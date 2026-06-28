package analyze

import (
	"go/ast"
	"go/printer"
	"go/types"
	"strings"

	"github.com/sattamBytes/flowgraph/internal/graph"
	"golang.org/x/tools/go/packages"
)

// pass3Calls builds the general function call graph: a node for every function
// defined in the analyzed packages, and CALLS edges between them annotated with
// the branch they sit under. Calls into the Temporal SDK are skipped here (the
// Temporal layer in pass2 already turned them into STARTS_WORKFLOW/… edges), so
// a call chain bridges naturally into Temporal edges through the shared function
// node. Interface/callback calls that cannot be resolved become dotted edges to
// an InterfaceCall node — surfaced, never guessed.
func (a *analyzer) pass3Calls() {
	a.buildLocalFuncs()
	for _, pkg := range a.pkgs {
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				fn, _ := pkg.TypesInfo.Defs[fd.Name].(*types.Func)
				if fn == nil {
					continue
				}
				from := a.symNode(graph.KindFunction, fn).ID // ensure node exists
				a.walkStmt(pkg, from, fd.Body, nil)
			}
		}
	}
}

// buildLocalFuncs records every function/method defined in the analyzed
// packages (not stdlib/deps), so the call graph only draws edges to first-party
// code rather than exploding into the standard library.
func (a *analyzer) buildLocalFuncs() {
	for _, pkg := range a.pkgs {
		for _, obj := range pkg.TypesInfo.Defs {
			if fn, ok := obj.(*types.Func); ok {
				a.localFuncs[fn] = true
			}
		}
	}
}

// recordCall resolves one call site and appends the appropriate edge.
func (a *analyzer) recordCall(pkg *packages.Package, from string, call *ast.CallExpr, br *graph.Branch) {
	info := pkg.TypesInfo
	// Temporal SDK calls are owned by pass2 — ignore here.
	if _, _, ok := sdkCall(info, call); ok {
		return
	}
	fn := calleeFunc(info, call)
	if fn == nil {
		return // e.g. a call through a func-typed variable we cannot name
	}
	f, l := a.pos(call.Pos())

	// Interface method call: resolvable target is unknown at compile time.
	if isInterfaceCall(info, call) {
		to := a.interfaceNode(fn, f, l)
		a.edges = append(a.edges, graph.Edge{
			From: from, To: to.ID, Kind: graph.EdgeCalls, Resolution: graph.Unresolved,
			TargetName: fn.Name(), Branch: br, File: f, Line: l,
		})
		return
	}

	// Concrete call into first-party code: resolved edge.
	if a.localFuncs[fn] {
		to := a.symNode(graph.KindFunction, fn)
		a.edges = append(a.edges, graph.Edge{
			From: from, To: to.ID, Kind: graph.EdgeCalls, Resolution: graph.Resolved,
			TargetName: fn.Name(), Branch: br, File: f, Line: l,
		})
		return
	}
	// Otherwise it's stdlib/dependency code — out of scope for the flow graph.
}

// isInterfaceCall reports whether call is a method invoked through an interface
// value (so the concrete implementation is not statically known).
func isInterfaceCall(info *types.Info, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	s := info.Selections[sel]
	if s == nil || s.Kind() != types.MethodVal {
		return false
	}
	_, isIface := s.Recv().Underlying().(*types.Interface)
	return isIface
}

// interfaceNode returns (creating if needed) the node for an interface method.
func (a *analyzer) interfaceNode(fn *types.Func, file string, line int) *graph.Node {
	id := "iface:" + funcSymbol(fn)
	return a.ensureNode(&graph.Node{
		ID: id, Kind: graph.KindInterface, Name: fn.Name() + " (interface)",
		Symbol: funcSymbol(fn), File: file, Line: line, Namespace: "default",
	})
}

// ---- statement walk with nearest-branch context ----

// walkStmt recurses through a statement, emitting CALLS for every call site with
// the nearest enclosing branch guard. br is the innermost guard (nil at top).
func (a *analyzer) walkStmt(pkg *packages.Package, from string, s ast.Stmt, br *graph.Branch) {
	switch st := s.(type) {
	case *ast.BlockStmt:
		for _, c := range st.List {
			a.walkStmt(pkg, from, c, br)
		}
	case *ast.IfStmt:
		if st.Init != nil {
			a.walkStmt(pkg, from, st.Init, br)
		}
		a.emitCalls(pkg, from, st.Cond, br)
		cond := a.exprText(st.Cond)
		a.walkStmt(pkg, from, st.Body, &graph.Branch{Kind: "if", Cond: cond})
		if st.Else != nil {
			a.walkStmt(pkg, from, st.Else, &graph.Branch{Kind: "else", Cond: cond})
		}
	case *ast.ForStmt:
		if st.Init != nil {
			a.walkStmt(pkg, from, st.Init, br)
		}
		nb := &graph.Branch{Kind: "for", Cond: a.exprText(st.Cond)}
		a.walkStmt(pkg, from, st.Body, nb)
	case *ast.RangeStmt:
		nb := &graph.Branch{Kind: "for", Cond: "range " + a.exprText(st.X)}
		a.walkStmt(pkg, from, st.Body, nb)
	case *ast.SwitchStmt:
		if st.Init != nil {
			a.walkStmt(pkg, from, st.Init, br)
		}
		a.emitCalls(pkg, from, st.Tag, br)
		for _, c := range st.Body.List {
			if cc, ok := c.(*ast.CaseClause); ok {
				nb := &graph.Branch{Kind: "case", Cond: a.caseText(cc.List)}
				for _, cs := range cc.Body {
					a.walkStmt(pkg, from, cs, nb)
				}
			}
		}
	case *ast.TypeSwitchStmt:
		for _, c := range st.Body.List {
			if cc, ok := c.(*ast.CaseClause); ok {
				nb := &graph.Branch{Kind: "case", Cond: a.caseText(cc.List)}
				for _, cs := range cc.Body {
					a.walkStmt(pkg, from, cs, nb)
				}
			}
		}
	case *ast.SelectStmt:
		for _, c := range st.Body.List {
			if cc, ok := c.(*ast.CommClause); ok {
				nb := &graph.Branch{Kind: "select"}
				for _, cs := range cc.Body {
					a.walkStmt(pkg, from, cs, nb)
				}
			}
		}
	case *ast.ExprStmt:
		a.emitCalls(pkg, from, st.X, br)
	case *ast.AssignStmt:
		for _, e := range st.Rhs {
			a.emitCalls(pkg, from, e, br)
		}
	case *ast.ReturnStmt:
		for _, e := range st.Results {
			a.emitCalls(pkg, from, e, br)
		}
	case *ast.DeferStmt:
		a.emitCalls(pkg, from, st.Call, br)
	case *ast.GoStmt:
		a.emitCalls(pkg, from, st.Call, br)
	case *ast.LabeledStmt:
		a.walkStmt(pkg, from, st.Stmt, br)
	default:
		// Best effort for anything not explicitly handled (DeclStmt, IncDec, …):
		// scan for calls under the current branch without descending into nested
		// control structures with the wrong guard.
		ast.Inspect(s, func(n ast.Node) bool {
			switch n.(type) {
			case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt,
				*ast.TypeSwitchStmt, *ast.SelectStmt:
				return false // handled by their own cases if reached directly
			case *ast.CallExpr:
				a.recordCall(pkg, from, n.(*ast.CallExpr), br)
			}
			return true
		})
	}
}

// emitCalls records every call within an expression under branch br.
func (a *analyzer) emitCalls(pkg *packages.Package, from string, e ast.Expr, br *graph.Branch) {
	if e == nil {
		return
	}
	ast.Inspect(e, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			a.recordCall(pkg, from, call, br)
		}
		return true
	})
}

func (a *analyzer) exprText(e ast.Expr) string {
	if e == nil {
		return ""
	}
	var b strings.Builder
	if err := printer.Fprint(&b, a.fset, e); err != nil {
		return ""
	}
	s := strings.Join(strings.Fields(b.String()), " ") // collapse whitespace
	if len(s) > 60 {
		s = s[:57] + "..."
	}
	return s
}

func (a *analyzer) caseText(exprs []ast.Expr) string {
	if len(exprs) == 0 {
		return "default"
	}
	parts := make([]string, 0, len(exprs))
	for _, e := range exprs {
		parts = append(parts, a.exprText(e))
	}
	return strings.Join(parts, ", ")
}
