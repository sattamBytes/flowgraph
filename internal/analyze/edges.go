package analyze

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"github.com/sattamBytes/flowgraph/internal/graph"
	"golang.org/x/tools/go/packages"
)

// pass2Edges walks every function, resolves invocation sites into graph edges,
// and collects non-determinism smells inside workflow functions.
func (a *analyzer) pass2Edges() {
	for _, pkg := range a.pkgs {
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				a.processFunc(pkg, fd)
			}
		}
	}
}

func (a *analyzer) processFunc(pkg *packages.Package, fd *ast.FuncDecl) {
	info := pkg.TypesInfo
	funcObj, _ := info.Defs[fd.Name].(*types.Func)
	if funcObj == nil {
		return
	}
	isWf := isWorkflowFunc(funcObj)

	// Pre-scan ctx-option helpers so EXECUTES_ACTIVITY/STARTS_CHILD edges can
	// carry timeout/retry/queue metadata set elsewhere in the same function.
	actTimeout, actRetry, childTQ := a.scanFuncOptions(info, fd)

	// The enclosing function is ONE unified node (see symNode): a Workflow if its
	// first param is workflow.Context, otherwise a plain Function. A function that
	// starts a workflow is just a Function with an outgoing STARTS_WORKFLOW edge —
	// no separate ControlPlaneCaller node — so call chains bridge into Temporal.
	selfKind := graph.KindFunction
	if isWf {
		selfKind = graph.KindWorkflow
	}
	self := func() string {
		return a.symNode(selfKind, funcObj).ID
	}

	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.GoStmt:
			if isWf {
				a.addSmell("go-statement", funcObj, "`go` statement spawns a goroutine — use workflow.Go for deterministic concurrency", node.Pos())
			}
		case *ast.RangeStmt:
			if isWf && node.X != nil {
				if tv, ok := info.Types[node.X]; ok {
					if _, isMap := tv.Type.Underlying().(*types.Map); isMap {
						a.addSmell("map-range", funcObj, "ranging over a map has nondeterministic order — sort the keys first", node.Pos())
					}
				}
			}
		case *ast.CallExpr:
			if a.handleSDKCall(pkg, fd, self, actTimeout, actRetry, childTQ, node) {
				return true
			}
			if isWf {
				a.maybeSmellCall(info, funcObj, node)
			}
		}
		return true
	})
}

// handleSDKCall builds an edge for a Temporal SDK invocation. Returns true if
// the call was an SDK call (handled), false otherwise.
func (a *analyzer) handleSDKCall(pkg *packages.Package, fd *ast.FuncDecl, self func() string, actTimeout, actRetry bool, childTQ string, call *ast.CallExpr) bool {
	info := pkg.TypesInfo
	p, name, ok := sdkCall(info, call)
	if !ok {
		return false
	}
	switch p {
	case sdkClient:
		switch name {
		case "ExecuteWorkflow":
			tq, _ := a.litFieldString(info, fd, arg(call, 1), "TaskQueue")
			a.startWorkflowEdge(info, fd, self(), arg(call, 2), tq, graph.EdgeStartsWorkflow, call)
		case "SignalWorkflow":
			a.signalEdge(info, self(), arg(call, 3), call, false)
		case "SignalWithStartWorkflow":
			a.signalEdge(info, self(), arg(call, 2), call, false)
			tq, _ := a.litFieldString(info, fd, arg(call, 4), "TaskQueue")
			a.startWorkflowEdge(info, fd, self(), arg(call, 5), tq, graph.EdgeStartsWorkflow, call)
		case "QueryWorkflow":
			a.signalEdge(info, self(), arg(call, 3), call, true)
		}
	case sdkWorkflow:
		switch name {
		case "ExecuteActivity", "ExecuteLocalActivity":
			a.activityEdge(info, fd, self(), arg(call, 1), actTimeout, actRetry, call)
		case "ExecuteChildWorkflow":
			a.startWorkflowEdge(info, fd, self(), arg(call, 1), childTQ, graph.EdgeStartsChild, call)
		case "SignalExternalWorkflow":
			a.signalEdge(info, self(), arg(call, 3), call, false)
		case "GetSignalChannel":
			a.listenerEdge(info, self(), arg(call, 1), call, false)
		case "SetQueryHandler":
			a.listenerEdge(info, self(), arg(call, 1), call, true)
		}
	}
	return true
}

func (a *analyzer) startWorkflowEdge(info *types.Info, fd *ast.FuncDecl, from string, target ast.Expr, tq, kind string, call *ast.CallExpr) {
	toID, res, tn := a.resolveTarget(graph.KindWorkflow, info, fd, target)
	f, l := a.pos(call.Pos())
	a.edges = append(a.edges, graph.Edge{
		From: from, To: toID, Kind: kind, Resolution: res,
		TaskQueue: tq, TargetName: tn, File: f, Line: l,
	})
}

func (a *analyzer) activityEdge(info *types.Info, fd *ast.FuncDecl, from string, target ast.Expr, hasTimeout, hasRetry bool, call *ast.CallExpr) {
	toID, res, tn := a.resolveTarget(graph.KindActivity, info, fd, target)
	f, l := a.pos(call.Pos())
	a.edges = append(a.edges, graph.Edge{
		From: from, To: toID, Kind: graph.EdgeExecutesActivity, Resolution: res,
		TargetName: tn, HasTimeout: hasTimeout, HasRetry: hasRetry, File: f, Line: l,
	})
}

// signalEdge builds a sender edge: caller -> Signal/Query node.
func (a *analyzer) signalEdge(info *types.Info, from string, nameExpr ast.Expr, call *ast.CallExpr, isQuery bool) {
	kind := graph.KindSignal
	if isQuery {
		kind = graph.KindQuery
	}
	f, l := a.pos(call.Pos())
	if s, ok := stringConst(info, nameExpr); ok {
		n := a.sigNode(kind, s, f, l)
		a.edges = append(a.edges, graph.Edge{From: from, To: n.ID, Kind: graph.EdgeSignals, Resolution: graph.Resolved, TargetName: s, File: f, Line: l})
		return
	}
	n := a.unresolvedNode(kind, f, l)
	a.edges = append(a.edges, graph.Edge{From: from, To: n.ID, Kind: graph.EdgeSignals, Resolution: graph.Unresolved, File: f, Line: l})
}

// listenerEdge builds a listener edge: Signal/Query node -> handling workflow,
// and marks the node as having a listener.
func (a *analyzer) listenerEdge(info *types.Info, workflow string, nameExpr ast.Expr, call *ast.CallExpr, isQuery bool) {
	kind := graph.KindSignal
	if isQuery {
		kind = graph.KindQuery
	}
	f, l := a.pos(call.Pos())
	s, ok := stringConst(info, nameExpr)
	if !ok {
		return // dynamic listener name — nothing to match against
	}
	n := a.sigNode(kind, s, f, l)
	n.HasListener = true
	a.edges = append(a.edges, graph.Edge{From: n.ID, To: workflow, Kind: graph.EdgeSignals, Resolution: graph.Resolved, TargetName: s, File: f, Line: l})
}

// resolveTarget resolves a workflow/activity argument to a node, returning the
// node ID, resolution status, and the referenced name (if any).
func (a *analyzer) resolveTarget(kind string, info *types.Info, fd *ast.FuncDecl, target ast.Expr) (string, string, string) {
	if target == nil {
		f, l := a.posOf(fd)
		n := a.unresolvedNode(kind, f, l)
		return n.ID, graph.Unresolved, ""
	}
	if fn := funcRef(info, target); fn != nil {
		n := a.symNode(kind, fn)
		return n.ID, graph.Resolved, n.Name
	}
	if s, ok := stringConst(info, target); ok {
		var entry *regEntry
		if kind == graph.KindWorkflow {
			entry = a.wfReg[s]
		} else {
			entry = a.actReg[s]
		}
		if entry != nil {
			n := a.symNode(kind, entry.sym)
			return n.ID, graph.Resolved, s
		}
		f, l := a.pos(target.Pos())
		n := a.nameNode(kind, s, f, l)
		return n.ID, graph.Unknown, s
	}
	f, l := a.pos(target.Pos())
	n := a.unresolvedNode(kind, f, l)
	return n.ID, graph.Unresolved, ""
}

func (a *analyzer) posOf(fd *ast.FuncDecl) (string, int) {
	return a.pos(fd.Pos())
}

func (a *analyzer) scanFuncOptions(info *types.Info, fd *ast.FuncDecl) (actTimeout, actRetry bool, childTQ string) {
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		p, name, ok := sdkCall(info, call)
		if !ok || p != sdkWorkflow {
			return true
		}
		switch name {
		case "WithActivityOptions", "WithLocalActivityOptions":
			if litFieldPresent(fd, arg(call, 1), "StartToCloseTimeout", "ScheduleToCloseTimeout", "ScheduleToStartTimeout") {
				actTimeout = true
			}
			if litFieldPresent(fd, arg(call, 1), "RetryPolicy") {
				actRetry = true
			}
		case "WithChildOptions":
			if tq, ok := a.litFieldString(info, fd, arg(call, 1), "TaskQueue"); ok {
				childTQ = tq
			}
		}
		return true
	})
	return
}

// maybeSmellCall flags nondeterministic library calls inside a workflow.
func (a *analyzer) maybeSmellCall(info *types.Info, funcObj *types.Func, call *ast.CallExpr) {
	fn := calleeFunc(info, call)
	if fn == nil || fn.Pkg() == nil {
		return
	}
	p := fn.Pkg().Path()
	switch {
	case p == "time" && fn.Name() == "Now":
		a.addSmell("time.Now", funcObj, "time.Now() is nondeterministic in workflows — use workflow.Now(ctx)", call.Pos())
	case p == "math/rand" || strings.HasPrefix(p, "math/rand/"):
		a.addSmell("math/rand", funcObj, "random numbers are nondeterministic — use a side effect or a seed from workflow state", call.Pos())
	case p == "net/http" || strings.HasPrefix(p, "database/sql") || p == "net":
		a.addSmell("network/db", funcObj, "direct network/DB I/O is forbidden in workflows — move it into an activity", call.Pos())
	}
}

func (a *analyzer) addSmell(kind string, funcObj *types.Func, detail string, pos token.Pos) {
	f, l := a.pos(pos)
	a.smells = append(a.smells, graph.Smell{
		Kind: kind, Func: funcSymbol(funcObj), Detail: detail, File: f, Line: l,
	})
}
