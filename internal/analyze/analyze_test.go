package analyze_test

import (
	"path/filepath"
	"testing"

	"github.com/sattamBytes/flowgraph/internal/analyze"
	"github.com/sattamBytes/flowgraph/internal/graph"
)

func load(t *testing.T) *graph.Graph {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "testdata", "sample"))
	if err != nil {
		t.Fatal(err)
	}
	g, err := analyze.Analyze(dir)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	return g
}

func node(g *graph.Graph, kind, name string) *graph.Node {
	for i := range g.Nodes {
		if g.Nodes[i].Kind == kind && g.Nodes[i].Name == name {
			return &g.Nodes[i]
		}
	}
	return nil
}

func edgesTo(g *graph.Graph, kind string) []graph.Edge {
	var out []graph.Edge
	for _, e := range g.Edges {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func TestWorkflowRegisteredQueues(t *testing.T) {
	g := load(t)
	ow := node(g, graph.KindWorkflow, "OrderWorkflow")
	if ow == nil {
		t.Fatal("OrderWorkflow node missing")
	}
	if !ow.Registered || len(ow.TaskQueues) != 1 || ow.TaskQueues[0] != "orders" {
		t.Errorf("OrderWorkflow queues = %v registered=%v, want [orders] true", ow.TaskQueues, ow.Registered)
	}
}

func TestCustomNameResolves(t *testing.T) {
	g := load(t)
	// ShippingWorkflow registered under custom name "ship.v1"; node name is the
	// registered name and it must carry the function symbol (exact resolution).
	sw := node(g, graph.KindWorkflow, "ship.v1")
	if sw == nil {
		t.Fatal("ship.v1 node missing — custom RegisterWorkflowWithOptions name not mapped")
	}
	if sw.Symbol == "" || sw.TaskQueues[0] != "shipping" {
		t.Errorf("ship.v1 symbol=%q queues=%v, want resolved on shipping", sw.Symbol, sw.TaskQueues)
	}
}

func TestStringStartResolvesToSameNode(t *testing.T) {
	g := load(t)
	// "ship.v1" started by string literal must resolve to the registered symbol.
	var found bool
	for _, e := range edgesTo(g, graph.EdgeStartsWorkflow) {
		if e.TargetName == "ship.v1" {
			found = true
			if e.Resolution != graph.Resolved {
				t.Errorf("ship.v1 start resolution=%q, want resolved", e.Resolution)
			}
		}
	}
	if !found {
		t.Error("no STARTS_WORKFLOW edge for ship.v1")
	}
}

func TestUnknownNameSurfaced(t *testing.T) {
	g := load(t)
	var found bool
	for _, e := range edgesTo(g, graph.EdgeStartsWorkflow) {
		if e.TargetName == "OrderWorkfow" { // deliberate typo
			found = true
			if e.Resolution != graph.Unknown {
				t.Errorf("typo resolution=%q, want unknown", e.Resolution)
			}
		}
	}
	if !found {
		t.Error("typo'd workflow start not present")
	}
}

func TestUnresolvedEdgeLabeledNotFaked(t *testing.T) {
	g := load(t)
	var unresolved *graph.Edge
	for i, e := range g.Edges {
		if e.Resolution == graph.Unresolved && e.Kind == graph.EdgeStartsWorkflow {
			unresolved = &g.Edges[i]
		}
	}
	if unresolved == nil {
		t.Fatal("dynamic workflow start should produce an unresolved edge")
	}
	// Must NOT fake a target name, and the target node must be the (dynamic) sink.
	if unresolved.TargetName != "" {
		t.Errorf("unresolved edge invented a target name %q", unresolved.TargetName)
	}
	if tn := g.NodeByID(unresolved.To); tn == nil || tn.Name != "(dynamic)" {
		t.Errorf("unresolved edge target = %v, want a (dynamic) node", tn)
	}
}

func TestTaskQueueCarriedOnStart(t *testing.T) {
	g := load(t)
	for _, e := range edgesTo(g, graph.EdgeStartsWorkflow) {
		if e.Resolution == graph.Resolved && e.TargetName == "OrderWorkflow" {
			if e.TaskQueue != "payments" {
				t.Errorf("OrderWorkflow start queue = %q, want payments", e.TaskQueue)
			}
			return
		}
	}
	t.Error("resolved OrderWorkflow start edge not found")
}

func TestActivityTimeoutRetryFlags(t *testing.T) {
	g := load(t)
	want := map[string][2]bool{ // name -> {timeout, retry}
		"ChargeCard":    {true, true},
		"SendEmail":     {true, true},
		"GenerateLabel": {false, false},
	}
	for _, e := range edgesTo(g, graph.EdgeExecutesActivity) {
		w, ok := want[e.TargetName]
		if !ok {
			continue
		}
		if e.HasTimeout != w[0] || e.HasRetry != w[1] {
			t.Errorf("%s timeout=%v retry=%v, want %v", e.TargetName, e.HasTimeout, e.HasRetry, w)
		}
	}
}

func TestSignalListenerVsSender(t *testing.T) {
	g := load(t)
	if l := node(g, graph.KindSignal, "CancelOrder"); l == nil || !l.HasListener {
		t.Error("CancelOrder signal should have a listener")
	}
	if s := node(g, graph.KindSignal, "cancelOrder"); s == nil || s.HasListener {
		t.Error("cancelOrder signal (sender-only) should have NO listener")
	}
}

func TestNonDeterminismSmell(t *testing.T) {
	g := load(t)
	got := map[string]bool{}
	for _, s := range g.Smells {
		got[s.Kind] = true
	}
	for _, want := range []string{"time.Now", "map-range", "go-statement", "network/db"} {
		if !got[want] {
			t.Errorf("non-determinism smell %q not detected in ShippingWorkflow; got %v", want, got)
		}
	}
}

func TestOrphanHasNode(t *testing.T) {
	g := load(t)
	rc := node(g, graph.KindActivity, "RefundCard")
	if rc == nil || !rc.Registered || rc.Started {
		t.Errorf("RefundCard = %v, want registered & not started", rc)
	}
}

// callEdge finds a CALLS edge from a Function node to a node named toName.
func callEdge(g *graph.Graph, fromName, toName string) *graph.Edge {
	from := node(g, graph.KindFunction, fromName)
	if from == nil {
		return nil
	}
	for i := range g.Edges {
		e := &g.Edges[i]
		if e.Kind != graph.EdgeCalls || e.From != from.ID {
			continue
		}
		if t := g.NodeByID(e.To); t != nil && t.Name == toName {
			return e
		}
	}
	return nil
}

func TestCallGraphResolvedCall(t *testing.T) {
	g := load(t)
	e := callEdge(g, "Handle", "prepare")
	if e == nil {
		t.Fatal("expected resolved CALLS edge Handle -> prepare")
	}
	if e.Resolution != graph.Resolved {
		t.Errorf("Handle->prepare resolution = %q, want resolved", e.Resolution)
	}
	if e.Branch != nil {
		t.Errorf("Handle->prepare should have no branch guard, got %+v", e.Branch)
	}
}

func TestCallGraphInterfaceCallWithBranch(t *testing.T) {
	g := load(t)
	e := callEdge(g, "Handle", "Audit (interface)")
	if e == nil {
		t.Fatal("expected CALLS edge Handle -> Audit (interface)")
	}
	if e.Resolution != graph.Unresolved {
		t.Errorf("interface call resolution = %q, want unresolved (impl unknown)", e.Resolution)
	}
	if e.Branch == nil || e.Branch.Kind != "if" || e.Branch.Cond != `orderID == ""` {
		t.Errorf("interface call branch = %+v, want {if, orderID == \"\"}", e.Branch)
	}
	if n := node(g, graph.KindInterface, "Audit (interface)"); n == nil {
		t.Error("expected an InterfaceCall node for Audit")
	}
}

func TestCallBridgesIntoTemporal(t *testing.T) {
	g := load(t)
	// Handle (a plain Function) starts OrderWorkflow — the function node is shared
	// between the call graph and the Temporal layer, so the chain bridges through.
	h := node(g, graph.KindFunction, "Handle")
	if h == nil {
		t.Fatal("Handle node missing")
	}
	for _, e := range g.Edges {
		if e.Kind == graph.EdgeStartsWorkflow && e.From == h.ID {
			if tgt := g.NodeByID(e.To); tgt != nil && tgt.Name == "OrderWorkflow" {
				return // bridged
			}
		}
	}
	t.Error("Handle should bridge into OrderWorkflow via STARTS_WORKFLOW from the same function node")
}
