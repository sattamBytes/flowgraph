package analyze

import (
	"go/ast"
	"go/types"
	"strings"

	"github.com/sattamBytes/flowgraph/internal/graph"
)

// Entrypoint is a door into the app discovered by a resolver: an HTTP route or a
// gRPC method bound to a handler function.
type Entrypoint struct {
	Kind    string // graph.KindRESTEndpoint / KindGRPCEndpoint
	Method  string // HTTP verb (may be "" for "any")
	Path    string // route path
	Handler *types.Func
}

// EntrypointResolver recognizes a framework's route-registration call. Each
// framework is a small self-contained resolver matched by the RESOLVED package
// path of the called function (never by method-name text alone).
type EntrypointResolver interface {
	Name() string
	Resolve(info *types.Info, call *ast.CallExpr) (*Entrypoint, bool)
}

// resolvers is the registered set. Add a framework by appending one here.
var resolvers = []EntrypointResolver{
	netHTTPResolver{},
	// chi: title-case verb methods, handler is the 2nd arg.
	verbResolver{name: "chi", pkgPrefix: "github.com/go-chi/chi", verbs: titleVerbs, handlerLast: false},
	// gin: upper-case verb methods, variadic handlers — the real handler is last.
	verbResolver{name: "gin", pkgPrefix: "github.com/gin-gonic/gin", verbs: upperVerbs, handlerLast: true},
	// echo: upper-case verb methods, handler is the 2nd arg (middleware follows).
	verbResolver{name: "echo", pkgPrefix: "github.com/labstack/echo", verbs: upperVerbs, handlerLast: false},
}

// pass4Entrypoints discovers entrypoints and links them to their handlers.
func (a *analyzer) pass4Entrypoints() {
	for _, pkg := range a.pkgs {
		info := pkg.TypesInfo
		for _, file := range pkg.Syntax {
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				for _, r := range resolvers {
					if ep, ok := r.Resolve(info, call); ok {
						a.addEntrypoint(call, ep)
						break
					}
				}
				return true
			})
		}
	}
}

func (a *analyzer) addEntrypoint(call *ast.CallExpr, ep *Entrypoint) {
	file, line := a.pos(call.Pos())
	label := strings.TrimSpace(ep.Method + " " + ep.Path)
	id := "rest:" + label
	n := a.ensureNode(&graph.Node{
		ID: id, Kind: ep.Kind, Name: label, Method: ep.Method, Path: ep.Path,
		File: file, Line: line, Namespace: "default", Entrypoint: true,
	})
	if ep.Handler != nil {
		h := a.symNode(graph.KindFunction, ep.Handler)
		n.HandlerSymbol = h.Symbol
		a.edges = append(a.edges, graph.Edge{
			From: id, To: h.ID, Kind: graph.EdgeHandles, Resolution: graph.Resolved,
			TargetName: ep.Handler.Name(), File: file, Line: line,
		})
	}
}

// splitPattern separates an optional leading HTTP method from a route pattern,
// supporting Go 1.22+ ServeMux patterns like "POST /orders".
func splitPattern(pat string) (method, path string) {
	pat = strings.TrimSpace(pat)
	if i := strings.IndexByte(pat, ' '); i > 0 {
		m := pat[:i]
		if isHTTPMethod(m) {
			return m, strings.TrimSpace(pat[i+1:])
		}
	}
	return "", pat
}

func isHTTPMethod(s string) bool {
	switch s {
	case "GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS", "CONNECT", "TRACE":
		return true
	}
	return false
}

// ---- net/http resolver ----

type netHTTPResolver struct{}

func (netHTTPResolver) Name() string { return "net/http" }

func (netHTTPResolver) Resolve(info *types.Info, call *ast.CallExpr) (*Entrypoint, bool) {
	fn := calleeFunc(info, call)
	if fn == nil || fn.Pkg() == nil || fn.Pkg().Path() != "net/http" || fn.Name() != "HandleFunc" {
		return nil, false
	}
	// http.HandleFunc(pattern, handler) and (*ServeMux).HandleFunc(pattern, handler)
	pat, ok := stringConst(info, arg(call, 0))
	if !ok {
		return nil, false
	}
	method, path := splitPattern(pat)
	return &Entrypoint{
		Kind: graph.KindRESTEndpoint, Method: method, Path: path,
		Handler: funcRef(info, arg(call, 1)),
	}, true
}

// ---- generic verb-method resolver (chi, gin, echo, …) ----

var titleVerbs = map[string]string{
	"Get": "GET", "Post": "POST", "Put": "PUT", "Delete": "DELETE",
	"Patch": "PATCH", "Head": "HEAD", "Options": "OPTIONS", "Connect": "CONNECT", "Trace": "TRACE",
}

var upperVerbs = map[string]string{
	"GET": "GET", "POST": "POST", "PUT": "PUT", "DELETE": "DELETE",
	"PATCH": "PATCH", "HEAD": "HEAD", "OPTIONS": "OPTIONS", "CONNECT": "CONNECT", "TRACE": "TRACE",
}

// verbResolver handles routers whose registration is `router.<Verb>(path, handler)`.
// handlerLast picks the final argument as the handler (gin's variadic handlers)
// rather than the second (chi/echo, where middleware follows the handler).
type verbResolver struct {
	name        string
	pkgPrefix   string
	verbs       map[string]string
	handlerLast bool
}

func (v verbResolver) Name() string { return v.name }

func (v verbResolver) Resolve(info *types.Info, call *ast.CallExpr) (*Entrypoint, bool) {
	fn := calleeFunc(info, call)
	if fn == nil || fn.Pkg() == nil || !strings.HasPrefix(fn.Pkg().Path(), v.pkgPrefix) {
		return nil, false
	}
	method, ok := v.verbs[fn.Name()]
	if !ok {
		return nil, false
	}
	path, ok := stringConst(info, arg(call, 0))
	if !ok {
		return nil, false
	}
	handlerArg := arg(call, 1)
	if v.handlerLast && len(call.Args) > 1 {
		handlerArg = call.Args[len(call.Args)-1]
	}
	return &Entrypoint{
		Kind: graph.KindRESTEndpoint, Method: method, Path: path,
		Handler: funcRef(info, handlerArg),
	}, true
}
