// Package analyze implements the two-pass static analysis that reconnects
// Temporal's "connect by name" model into a graph.
//
// Pass 1 (registry.go) builds name -> Go function symbol mappings from
// Register* call sites and records each worker's task queue. Pass 2 (edges.go)
// finds invocation sites (ExecuteWorkflow/Activity/ChildWorkflow, signals) and
// resolves each target to a function reference, a registered name, or marks it
// unresolved. SDK calls are identified by the RESOLVED package path of the
// callee — never by method-name text — so a user's own ExecuteWorkflow helper
// does not cause false positives.
package analyze

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"os"
	"sort"
	"strings"

	"github.com/sattamBytes/flowgraph/internal/graph"
	"golang.org/x/tools/go/packages"
)

// Temporal SDK package paths. We match on these resolved paths, not text.
const (
	sdkRoot     = "go.temporal.io/sdk"
	sdkClient   = sdkRoot + "/client"
	sdkWorkflow = sdkRoot + "/workflow"
	sdkWorker   = sdkRoot + "/worker"
)

type regEntry struct {
	sym    *types.Func
	name   string
	queues []string
	file   string
	line   int
}

type analyzer struct {
	fset *token.FileSet
	pkgs []*packages.Package

	// pass 1
	workerQueues map[*types.Var][]string  // worker var -> task queue(s)
	wfReg        map[string]*regEntry     // registered name -> workflow entry
	actReg       map[string]*regEntry     // registered name -> activity entry
	symRegName   map[*types.Func]string   // symbol -> registered name (any kind)
	symQueues    map[*types.Func][]string // symbol -> registered queues

	// pass 2 / output
	nodes  map[string]*graph.Node
	edges  []graph.Edge
	smells []graph.Smell

	// pass 3 (call graph): functions defined in the analyzed packages
	localFuncs map[*types.Func]bool
}

func newAnalyzer() *analyzer {
	return &analyzer{
		workerQueues: map[*types.Var][]string{},
		wfReg:        map[string]*regEntry{},
		actReg:       map[string]*regEntry{},
		symRegName:   map[*types.Func]string{},
		symQueues:    map[*types.Func][]string{},
		nodes:        map[string]*graph.Node{},
		localFuncs:   map[*types.Func]bool{},
	}
}

// Analyze loads the Go packages rooted at path and returns the canonical graph.
// It never executes user code. Type errors degrade gracefully: unresolved
// targets are surfaced as unresolved edges, never guessed.
func Analyze(path string) (*graph.Graph, error) {
	a := newAnalyzer()
	if err := a.load(path); err != nil {
		return nil, err
	}
	a.pass1Registry()
	a.pass2Edges()
	a.ensureRegisteredNodes()
	a.pass3Calls()
	return a.finalize(), nil
}

func (a *analyzer) load(path string) error {
	a.fset = token.NewFileSet()
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedImports | packages.NeedDeps | packages.NeedTypes |
			packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedModule,
		Fset: a.fset,
	}
	patterns := []string{path}
	base := strings.TrimSuffix(strings.TrimSuffix(path, "..."), "/")
	if base == "" {
		base = "."
	}
	if fi, err := os.Stat(base); err == nil && fi.IsDir() {
		cfg.Dir = base
		patterns = []string{"./..."}
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return fmt.Errorf("loading packages: %w", err)
	}
	if len(pkgs) == 0 {
		return fmt.Errorf("no Go packages found at %q", path)
	}
	a.pkgs = pkgs
	return nil
}

// ---- generic AST/type helpers ----

func (a *analyzer) pos(p token.Pos) (string, int) {
	pp := a.fset.Position(p)
	return pp.Filename, pp.Line
}

func funcSymbol(fn *types.Func) string {
	if fn == nil {
		return ""
	}
	if fn.Pkg() == nil {
		return fn.Name()
	}
	return fn.Pkg().Path() + "." + fn.Name()
}

// calleeFunc resolves a call expression to the *types.Func being called, for
// both method calls (x.Foo()) and package-qualified calls (pkg.Foo()).
func calleeFunc(info *types.Info, call *ast.CallExpr) *types.Func {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		// bare Foo() — resolve the ident
		if id, ok := call.Fun.(*ast.Ident); ok {
			if fn, ok := info.Uses[id].(*types.Func); ok {
				return fn
			}
		}
		return nil
	}
	if s := info.Selections[sel]; s != nil {
		if m, ok := s.Obj().(*types.Func); ok {
			return m
		}
	}
	if fn, ok := info.Uses[sel.Sel].(*types.Func); ok {
		return fn
	}
	return nil
}

// sdkCall returns the SDK package path and method name if call targets the
// Temporal SDK, else ("", "", false).
func sdkCall(info *types.Info, call *ast.CallExpr) (pkgPath, name string, ok bool) {
	fn := calleeFunc(info, call)
	if fn == nil || fn.Pkg() == nil {
		return "", "", false
	}
	p := fn.Pkg().Path()
	if !strings.HasPrefix(p, sdkRoot) {
		return "", "", false
	}
	return p, fn.Name(), true
}

// funcRef resolves an expression that is a reference to a function (passed as a
// value, e.g. OrderWorkflow or a.ChargeCard) to its *types.Func.
func funcRef(info *types.Info, expr ast.Expr) *types.Func {
	switch e := expr.(type) {
	case *ast.Ident:
		if fn, ok := info.Uses[e].(*types.Func); ok {
			return fn
		}
		if fn, ok := info.Defs[e].(*types.Func); ok {
			return fn
		}
	case *ast.SelectorExpr:
		if s := info.Selections[e]; s != nil {
			if m, ok := s.Obj().(*types.Func); ok {
				return m
			}
		}
		if fn, ok := info.Uses[e.Sel].(*types.Func); ok {
			return fn
		}
	}
	return nil
}

// stringConst returns the constant string value of expr if it is one. go/types
// folds constants, so this also resolves named string constants, not just
// literals.
func stringConst(info *types.Info, expr ast.Expr) (string, bool) {
	tv, ok := info.Types[expr]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(tv.Value), true
}

// arg returns call.Args[i] or nil.
func arg(call *ast.CallExpr, i int) ast.Expr {
	if i < len(call.Args) {
		return call.Args[i]
	}
	return nil
}

// compositeLit unwraps expr to a composite literal: a literal, &literal, or an
// ident assigned a literal within enclosing.
func compositeLit(enclosing *ast.FuncDecl, expr ast.Expr) *ast.CompositeLit {
	switch e := expr.(type) {
	case *ast.CompositeLit:
		return e
	case *ast.UnaryExpr:
		if e.Op == token.AND {
			return compositeLit(enclosing, e.X)
		}
	case *ast.Ident:
		// best-effort: find `name := SomeStruct{...}` in the enclosing func.
		// ponytail: first assignment wins; full data-flow is out of scope.
		var found *ast.CompositeLit
		if enclosing != nil && enclosing.Body != nil {
			ast.Inspect(enclosing.Body, func(n ast.Node) bool {
				if found != nil {
					return false
				}
				as, ok := n.(*ast.AssignStmt)
				if !ok {
					return true
				}
				for i, lhs := range as.Lhs {
					if id, ok := lhs.(*ast.Ident); ok && id.Name == e.Name && i < len(as.Rhs) {
						if cl := compositeLit(enclosing, as.Rhs[i]); cl != nil {
							found = cl
						}
					}
				}
				return true
			})
		}
		return found
	}
	return nil
}

// litFieldValue returns the value expr for the named field of a composite lit.
func litFieldValue(cl *ast.CompositeLit, field string) ast.Expr {
	if cl == nil {
		return nil
	}
	for _, el := range cl.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			if id, ok := kv.Key.(*ast.Ident); ok && id.Name == field {
				return kv.Value
			}
		}
	}
	return nil
}

// litFieldString reads a string field from an options composite literal.
func (a *analyzer) litFieldString(info *types.Info, enclosing *ast.FuncDecl, optsArg ast.Expr, field string) (string, bool) {
	cl := compositeLit(enclosing, optsArg)
	v := litFieldValue(cl, field)
	if v == nil {
		return "", false
	}
	return stringConst(info, v)
}

// litFieldPresent reports whether any of the given fields is set in the options
// literal (used for timeout/retry presence checks).
func litFieldPresent(enclosing *ast.FuncDecl, optsArg ast.Expr, fields ...string) bool {
	cl := compositeLit(enclosing, optsArg)
	for _, f := range fields {
		if litFieldValue(cl, f) != nil {
			return true
		}
	}
	return false
}

// isWorkflowFunc reports whether fn's first parameter is workflow.Context.
func isWorkflowFunc(fn *types.Func) bool {
	if fn == nil {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Params().Len() == 0 {
		return false
	}
	named, ok := sig.Params().At(0).Type().(*types.Named)
	if !ok {
		return false
	}
	o := named.Obj()
	return o.Pkg() != nil && o.Pkg().Path() == sdkWorkflow && o.Name() == "Context"
}

// ---- node helpers ----

func (a *analyzer) ensureNode(n *graph.Node) *graph.Node {
	if ex, ok := a.nodes[n.ID]; ok {
		return ex
	}
	a.nodes[n.ID] = n
	return n
}

// kindRank orders node kinds so a richer role wins when the same Go function is
// seen in multiple roles (a workflow that is also reached as a plain call stays
// a Workflow, not downgraded to Function).
func kindRank(kind string) int {
	switch kind {
	case graph.KindWorkflow, graph.KindActivity:
		return 3
	case graph.KindRESTEndpoint, graph.KindGRPCEndpoint:
		return 2
	default: // Function, etc.
		return 1
	}
}

// symNode returns the single unified node for a Go function, keyed by symbol
// regardless of role. A function is ONE node whether it is a plain function, a
// workflow, an activity, or an entrypoint — this is what lets a call chain bridge
// seamlessly into Temporal edges. The kind is upgraded but never downgraded.
func (a *analyzer) symNode(kind string, fn *types.Func) *graph.Node {
	id := "fn:" + funcSymbol(fn)
	if ex, ok := a.nodes[id]; ok {
		if kindRank(kind) > kindRank(ex.Kind) {
			ex.Kind = kind
		}
		// Fill registration metadata if it became known after creation.
		if len(ex.TaskQueues) == 0 && len(a.symQueues[fn]) > 0 {
			ex.TaskQueues = a.symQueues[fn]
		}
		if !ex.Registered && a.symRegName[fn] != "" {
			ex.Registered = true
		}
		return ex
	}
	file, line := a.pos(fn.Pos())
	name := fn.Name()
	if rn, ok := a.symRegName[fn]; ok {
		name = rn
	}
	svc, pkgPath := "", ""
	if fn.Pkg() != nil {
		svc = fn.Pkg().Name()
		pkgPath = fn.Pkg().Path()
	}
	n := &graph.Node{
		ID: id, Kind: kind, Name: name, Symbol: funcSymbol(fn),
		File: file, Line: line, Service: svc, Package: pkgPath, Namespace: "default",
		TaskQueues: a.symQueues[fn], Registered: a.symRegName[fn] != "",
	}
	a.nodes[id] = n
	return n
}

func (a *analyzer) nameNode(kind, name string, file string, line int) *graph.Node {
	prefix := "wf:name:"
	if kind == graph.KindActivity {
		prefix = "act:name:"
	}
	id := prefix + name
	return a.ensureNode(&graph.Node{
		ID: id, Kind: kind, Name: name, File: file, Line: line, Namespace: "default",
	})
}

func (a *analyzer) unresolvedNode(kind, file string, line int) *graph.Node {
	id := fmt.Sprintf("unresolved:%s:%d", file, line)
	return a.ensureNode(&graph.Node{
		ID: id, Kind: kind, Name: "(dynamic)", File: file, Line: line, Namespace: "default",
	})
}

func (a *analyzer) sigNode(kind, name string, file string, line int) *graph.Node {
	prefix := "sig:"
	if kind == graph.KindQuery {
		prefix = "qry:"
	}
	id := prefix + name
	return a.ensureNode(&graph.Node{
		ID: id, Kind: kind, Name: name, File: file, Line: line, Namespace: "default",
	})
}

// ensureRegisteredNodes materializes a node for every registered workflow and
// activity, even those never started/executed — otherwise orphans (registered
// but never used) would have no node and the orphan rule could not see them.
func (a *analyzer) ensureRegisteredNodes() {
	for _, e := range a.wfReg {
		a.symNode(graph.KindWorkflow, e.sym)
	}
	for _, e := range a.actReg {
		a.symNode(graph.KindActivity, e.sym)
	}
}

// finalize converts the maps into a deterministic, sorted Graph and marks
// which target nodes were actually started/executed.
func (a *analyzer) finalize() *graph.Graph {
	g := &graph.Graph{Smells: a.smells}
	started := map[string]bool{}
	for _, e := range a.edges {
		switch e.Kind {
		case graph.EdgeStartsWorkflow, graph.EdgeExecutesActivity, graph.EdgeStartsChild:
			started[e.To] = true
		}
	}
	for id := range a.nodes {
		if started[id] {
			a.nodes[id].Started = true
		}
	}
	ids := make([]string, 0, len(a.nodes))
	for id := range a.nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		g.Nodes = append(g.Nodes, *a.nodes[id])
	}
	g.Edges = append(g.Edges, a.edges...)
	sort.Slice(g.Edges, func(i, j int) bool {
		ei, ej := g.Edges[i], g.Edges[j]
		if ei.From != ej.From {
			return ei.From < ej.From
		}
		if ei.To != ej.To {
			return ei.To < ej.To
		}
		if ei.Kind != ej.Kind {
			return ei.Kind < ej.Kind
		}
		return ei.Line < ej.Line
	})
	sort.Slice(g.Smells, func(i, j int) bool {
		if g.Smells[i].File != g.Smells[j].File {
			return g.Smells[i].File < g.Smells[j].File
		}
		return g.Smells[i].Line < g.Smells[j].Line
	})
	return g
}
