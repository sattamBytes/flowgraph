package mcp

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sattamBytes/flowgraph/internal/analyze"
	"github.com/sattamBytes/flowgraph/internal/graph"
)

func load(t *testing.T) *graph.Graph {
	t.Helper()
	dir, _ := filepath.Abs(filepath.Join("..", "..", "testdata", "sample"))
	g, err := analyze.Analyze(dir)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func names(rs []nodeRef) map[string]bool {
	m := map[string]bool{}
	for _, r := range rs {
		m[r.Name] = true
	}
	return m
}

func TestDownstream(t *testing.T) {
	g := load(t)
	got := names(refs(g, reachable(g, "OrderWorkflow", true)))
	for _, want := range []string{"ChargeCard", "SendEmail"} {
		if !got[want] {
			t.Errorf("downstream(OrderWorkflow) missing %q; got %v", want, got)
		}
	}
}

func TestUpstream(t *testing.T) {
	g := load(t)
	got := names(refs(g, reachable(g, "ChargeCard", false)))
	for _, want := range []string{"OrderWorkflow", "StartOrder"} {
		if !got[want] {
			t.Errorf("upstream(ChargeCard) missing %q; got %v", want, got)
		}
	}
}

func TestWhoStarts(t *testing.T) {
	g := load(t)
	got := names(whoStarts(g, "OrderWorkflow"))
	if !got["StartOrder"] {
		t.Errorf("who_starts(OrderWorkflow) = %v, want StartOrder", got)
	}
}

func TestListUnresolved(t *testing.T) {
	g := load(t)
	u := unresolved(g)
	var hasDynamic, hasTypo bool
	for _, e := range u {
		if e.Resolution == graph.Unresolved {
			hasDynamic = true
		}
		if e.Resolution == graph.Unknown && e.Target == "OrderWorkfow" {
			hasTypo = true
		}
	}
	if !hasDynamic {
		t.Error("list_unresolved should include the dynamic (unresolved) start")
	}
	if !hasTypo {
		t.Error("list_unresolved should include the unknown typo'd name")
	}
}

func TestCalleesAndCallers(t *testing.T) {
	g := load(t)
	if !names(neighbors(g, "Handle", true))["prepare"] {
		t.Error("callees(Handle) should include prepare")
	}
	if !names(neighbors(g, "prepare", false))["Handle"] {
		t.Error("callers(prepare) should include Handle")
	}
}

// TestRoundTrip drives the real MCP server over an in-memory transport, the way
// an AI agent would, and checks a tool call returns the expected answer.
func TestRoundTrip(t *testing.T) {
	g := load(t)
	ctx := context.Background()
	clientT, serverT := mcpsdk.NewInMemoryTransports()

	srv := newServer(g)
	go func() { _ = srv.Run(ctx, serverT) }()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "who_starts",
		Arguments: map[string]any{"node": "OrderWorkflow"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var text string
	for _, c := range res.Content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			text += tc.Text
		}
	}
	if !strings.Contains(text, "StartOrder") {
		t.Errorf("who_starts(OrderWorkflow) returned %q, want it to mention StartOrder", text)
	}
}
