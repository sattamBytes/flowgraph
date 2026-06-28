// Package graph defines the canonical data model emitted by the analyzer.
//
// The JSON serialization of Graph is the project's canonical artifact: every
// other output (check, export, serve, mcp) is built from it. Keep this struct
// stable and self-contained — it must carry everything the lint rules and the
// dashboard need, so that `fg serve --graph graph.json` works with no source.
package graph

// Node kinds.
const (
	KindControlPlaneCaller = "ControlPlaneCaller"
	KindWorkflow           = "Workflow"
	KindActivity           = "Activity"
	KindSignal             = "Signal"
	KindQuery              = "Query"
	// General code-flow kinds (flowgraph).
	KindFunction     = "Function"      // a plain function or a resolved method
	KindInterface    = "InterfaceCall" // an interface method whose impl is unknown
	KindRESTEndpoint = "RESTEndpoint"  // an HTTP route entrypoint
	KindGRPCEndpoint = "GRPCEndpoint"  // a gRPC method entrypoint
)

// Edge kinds.
const (
	EdgeStartsWorkflow   = "STARTS_WORKFLOW"
	EdgeExecutesActivity = "EXECUTES_ACTIVITY"
	EdgeStartsChild      = "STARTS_CHILD"
	EdgeSignals          = "SIGNALS"
	// General code-flow edges (flowgraph).
	EdgeCalls   = "CALLS"   // function -> function
	EdgeHandles = "HANDLES" // entrypoint -> handler function
)

// Branch describes the nearest control-flow guard around a call site, so the UI
// can show "applyCoupon() runs only if req.Coupon != \"\"".
type Branch struct {
	Kind string `json:"kind"`           // if | else | case | for | select
	Cond string `json:"cond,omitempty"` // source text of the guard, truncated
}

// Resolution status of an edge.
const (
	Resolved   = "resolved"   // target found by func reference or registered name
	Unresolved = "unresolved" // computed/variable argument — cannot resolve statically
	Unknown    = "unknown"    // string literal that was never registered (typo/dead ref)
)

// Node is a vertex in the graph. Every node carries a source location.
//
// ponytail: child workflows are NOT separate nodes — a child workflow is the
// same registered workflow, just reached via a STARTS_CHILD edge. Splitting it
// would fragment blast-radius. The edge kind carries the "child" semantics.
type Node struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Name        string   `json:"name"`             // display / registered name
	Symbol      string   `json:"symbol,omitempty"` // pkgpath.FuncName when resolved
	File        string   `json:"file"`
	Line        int      `json:"line"`
	Service     string   `json:"service,omitempty"`     // package name, for filtering
	Package     string   `json:"package,omitempty"`     // full package path
	Namespace   string   `json:"namespace,omitempty"`   // Temporal namespace, "default" if unknown
	TaskQueues  []string `json:"taskQueues,omitempty"`  // queues a workflow/activity is REGISTERED on
	Registered  bool     `json:"registered,omitempty"`  // true if seen at a Register* site
	Started     bool     `json:"started,omitempty"`     // set during finalize: has an inbound start/exec edge
	HasListener bool     `json:"hasListener,omitempty"` // for Signal/Query: a handler exists
	// REST/gRPC entrypoint fields.
	Method        string `json:"method,omitempty"`        // HTTP verb / gRPC kind
	Path          string `json:"path,omitempty"`          // route path
	HandlerSymbol string `json:"handlerSymbol,omitempty"` // symbol of the handler func
	Entrypoint    bool   `json:"entrypoint,omitempty"`    // true for REST/gRPC roots
}

// Edge is a directed relationship. Every edge carries a resolution status plus
// the task queue and (when statically readable) timeout/retry metadata.
type Edge struct {
	From       string  `json:"from"`
	To         string  `json:"to"`
	Kind       string  `json:"kind"`
	Resolution string  `json:"resolution"`
	TaskQueue  string  `json:"taskQueue,omitempty"`  // queue used at the start site
	TargetName string  `json:"targetName,omitempty"` // the name/string referenced
	HasTimeout bool    `json:"hasTimeout,omitempty"` // EXECUTES_ACTIVITY: workflow set a timeout
	HasRetry   bool    `json:"hasRetry,omitempty"`   // EXECUTES_ACTIVITY: workflow set a retry policy
	Branch     *Branch `json:"branch,omitempty"`     // CALLS: the guard around the call site
	File       string  `json:"file"`
	Line       int     `json:"line"`
}

// Smell is a non-determinism finding located inside a workflow function. It is
// detected during analysis (it has no graph edge) and carried in the artifact
// so the `check` command can run rule 5 purely over the graph JSON.
type Smell struct {
	Kind   string `json:"kind"`   // e.g. "time.Now", "math/rand", "go-statement", "map-range", "network/db"
	Func   string `json:"func"`   // enclosing workflow function symbol
	Detail string `json:"detail"` // human-readable explanation
	File   string `json:"file"`
	Line   int    `json:"line"`
}

// Graph is the whole artifact.
type Graph struct {
	Nodes  []Node  `json:"nodes"`
	Edges  []Edge  `json:"edges"`
	Smells []Smell `json:"smells,omitempty"`
}

// NodeByID returns a pointer to the node with the given ID, or nil.
func (g *Graph) NodeByID(id string) *Node {
	for i := range g.Nodes {
		if g.Nodes[i].ID == id {
			return &g.Nodes[i]
		}
	}
	return nil
}
