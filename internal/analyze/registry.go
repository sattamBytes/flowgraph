package analyze

import (
	"go/ast"
	"go/types"
)

// pass1Registry builds name -> symbol mappings and records task queues.
//
// It runs in two sub-walks: first collect every worker.New(...) so we know each
// worker variable's task queue, then collect Register* calls and attach the
// queue of the worker they were registered on.
func (a *analyzer) pass1Registry() {
	a.collectWorkers()
	a.collectRegistrations()
}

func (a *analyzer) collectWorkers() {
	for _, pkg := range a.pkgs {
		info := pkg.TypesInfo
		for _, file := range pkg.Syntax {
			ast.Inspect(file, func(n ast.Node) bool {
				as, ok := n.(*ast.AssignStmt)
				if !ok {
					return true
				}
				for i, rhs := range as.Rhs {
					call, ok := rhs.(*ast.CallExpr)
					if !ok {
						continue
					}
					p, name, ok := sdkCall(info, call)
					if !ok || p != sdkWorker || name != "New" {
						continue
					}
					// worker.New(client, taskQueue, options)
					queue := ""
					if q, ok := stringConst(info, arg(call, 1)); ok {
						queue = q
					}
					if i >= len(as.Lhs) {
						continue
					}
					if v := varObject(info, as.Lhs[i]); v != nil {
						a.workerQueues[v] = appendUnique(a.workerQueues[v], queue)
					}
				}
				return true
			})
		}
	}
}

func (a *analyzer) collectRegistrations() {
	for _, pkg := range a.pkgs {
		info := pkg.TypesInfo
		for _, file := range pkg.Syntax {
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				p, name, ok := sdkCall(info, call)
				if !ok || p != sdkWorker {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				queues := a.workerQueuesFor(info, sel.X)
				switch name {
				case "RegisterWorkflow":
					a.registerPlain(info, call, queues, true)
				case "RegisterWorkflowWithOptions":
					a.registerWithOptions(info, call, queues, true)
				case "RegisterActivity":
					a.registerPlain(info, call, queues, false)
				case "RegisterActivityWithOptions":
					a.registerWithOptions(info, call, queues, false)
				}
				return true
			})
		}
	}
}

// registerPlain handles RegisterWorkflow(Fn) / RegisterActivity(Fn): the
// registered name is the function name.
func (a *analyzer) registerPlain(info *types.Info, call *ast.CallExpr, queues []string, isWorkflow bool) {
	fn := funcRef(info, arg(call, 0))
	if fn == nil {
		return
	}
	a.record(fn, fn.Name(), queues, call, isWorkflow)
}

// registerWithOptions handles Register*WithOptions(Fn, RegisterOptions{Name:"x"}).
// If Name is absent the registered name falls back to the function name.
func (a *analyzer) registerWithOptions(info *types.Info, call *ast.CallExpr, queues []string, isWorkflow bool) {
	fn := funcRef(info, arg(call, 0))
	if fn == nil {
		return
	}
	name := fn.Name()
	if cl := compositeLit(nil, arg(call, 1)); cl != nil {
		if v := litFieldValue(cl, "Name"); v != nil {
			if s, ok := stringConst(info, v); ok && s != "" {
				name = s
			}
		}
	}
	a.record(fn, name, queues, call, isWorkflow)
}

func (a *analyzer) record(fn *types.Func, name string, queues []string, call *ast.CallExpr, isWorkflow bool) {
	file, line := a.pos(call.Pos())
	a.symRegName[fn] = name
	for _, q := range queues {
		a.symQueues[fn] = appendUnique(a.symQueues[fn], q)
	}
	e := &regEntry{sym: fn, name: name, queues: queues, file: file, line: line}
	if isWorkflow {
		a.wfReg[name] = e
	} else {
		a.actReg[name] = e
	}
}

// workerQueuesFor returns the registered task queues for the worker that recv
// refers to.
func (a *analyzer) workerQueuesFor(info *types.Info, recv ast.Expr) []string {
	id, ok := recv.(*ast.Ident)
	if !ok {
		return nil
	}
	if v, ok := info.Uses[id].(*types.Var); ok {
		return a.workerQueues[v]
	}
	return nil
}

func varObject(info *types.Info, expr ast.Expr) *types.Var {
	id, ok := expr.(*ast.Ident)
	if !ok {
		return nil
	}
	if v, ok := info.Defs[id].(*types.Var); ok {
		return v
	}
	if v, ok := info.Uses[id].(*types.Var); ok {
		return v
	}
	return nil
}

func appendUnique(s []string, v string) []string {
	if v == "" {
		return s
	}
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}
