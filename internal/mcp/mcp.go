// Package mcp exposes the graph over the Model Context Protocol so AI coding
// agents can query it. It consumes ONLY the graph.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sattamBytes/flowgraph/internal/graph"
)

type nodeRef struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	File string `json:"file"`
	Line int    `json:"line"`
}

type nodeQuery struct {
	Node string `json:"node" jsonschema:"name of a workflow, activity, signal, or query node"`
}

type nodeResult struct {
	Query string    `json:"query"`
	Nodes []nodeRef `json:"nodes"`
}

// edgeList wraps a slice so the MCP output schema is an object (the spec
// requires tool outputs to be objects, not bare arrays).
type edgeList struct {
	Edges []edgeRef `json:"edges"`
}

type edgeRef struct {
	Kind       string `json:"kind"`
	Resolution string `json:"resolution"`
	From       string `json:"from"`
	Target     string `json:"target"`
	File       string `json:"file"`
	Line       int    `json:"line"`
}

// Serve registers the query tools and runs an MCP server over stdio.
func Serve(g *graph.Graph) error {
	return newServer(g).Run(context.Background(), &mcp.StdioTransport{})
}

// newServer builds the MCP server with all query tools registered. Split out
// from Serve so tests can drive it over an in-memory transport.
func newServer(g *graph.Graph) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "flowgraph", Version: "0.1.0"}, nil)

	mcp.AddTool(s, &mcp.Tool{Name: "downstream",
		Description: "List everything a node triggers (transitive: activities, child workflows, signals)."},
		func(_ context.Context, _ *mcp.CallToolRequest, in nodeQuery) (*mcp.CallToolResult, nodeResult, error) {
			out := nodeResult{Query: in.Node, Nodes: refs(g, reachable(g, in.Node, true))}
			return text(out), out, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "upstream",
		Description: "List everything that triggers a node (transitive, toward control-plane callers)."},
		func(_ context.Context, _ *mcp.CallToolRequest, in nodeQuery) (*mcp.CallToolResult, nodeResult, error) {
			out := nodeResult{Query: in.Node, Nodes: refs(g, reachable(g, in.Node, false))}
			return text(out), out, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "who_starts",
		Description: "List the control-plane call sites that start a given workflow by name."},
		func(_ context.Context, _ *mcp.CallToolRequest, in nodeQuery) (*mcp.CallToolResult, nodeResult, error) {
			out := nodeResult{Query: in.Node, Nodes: whoStarts(g, in.Node)}
			return text(out), out, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "callees",
		Description: "List the functions a given function calls directly (one hop, CALLS edges)."},
		func(_ context.Context, _ *mcp.CallToolRequest, in nodeQuery) (*mcp.CallToolResult, nodeResult, error) {
			out := nodeResult{Query: in.Node, Nodes: neighbors(g, in.Node, true)}
			return text(out), out, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "callers",
		Description: "List the functions that call a given function directly (one hop, CALLS edges)."},
		func(_ context.Context, _ *mcp.CallToolRequest, in nodeQuery) (*mcp.CallToolResult, nodeResult, error) {
			out := nodeResult{Query: in.Node, Nodes: neighbors(g, in.Node, false)}
			return text(out), out, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "list_unresolved",
		Description: "List every edge whose target could not be resolved statically (dynamic names) or was never registered."},
		func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, edgeList, error) {
			out := edgeList{Edges: unresolved(g)}
			return text(out), out, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "get_graph",
		Description: "Return the full graph (nodes, edges, smells)."},
		func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, graph.Graph, error) {
			return text(*g), *g, nil
		})

	return s
}

func text(v any) *mcp.CallToolResult {
	b, _ := json.MarshalIndent(v, "", "  ")
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}
}

// reachable returns node IDs reachable from any node named `name`, following
// edges downstream (forward) or upstream (reverse).
func reachable(g *graph.Graph, name string, downstream bool) []string {
	seen := map[string]bool{}
	var stack []string
	for _, n := range g.Nodes {
		if n.Name == name {
			stack = append(stack, n.ID)
		}
	}
	var order []string
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, e := range g.Edges {
			var next string
			if downstream && e.From == id {
				next = e.To
			} else if !downstream && e.To == id {
				next = e.From
			} else {
				continue
			}
			if !seen[next] {
				seen[next] = true
				order = append(order, next)
				stack = append(stack, next)
			}
		}
	}
	return order
}

func refs(g *graph.Graph, ids []string) []nodeRef {
	out := []nodeRef{}
	for _, id := range ids {
		if n := g.NodeByID(id); n != nil {
			out = append(out, nodeRef{Name: n.Name, Kind: n.Kind, File: n.File, Line: n.Line})
		}
	}
	return out
}

// neighbors returns the direct CALLS callees (out=true) or callers (out=false)
// of every node named `name`.
func neighbors(g *graph.Graph, name string, out bool) []nodeRef {
	want := map[string]bool{}
	for _, n := range g.Nodes {
		if n.Name == name {
			want[n.ID] = true
		}
	}
	seen := map[string]bool{}
	res := []nodeRef{}
	for _, e := range g.Edges {
		if e.Kind != graph.EdgeCalls {
			continue
		}
		var other string
		if out && want[e.From] {
			other = e.To
		} else if !out && want[e.To] {
			other = e.From
		} else {
			continue
		}
		if seen[other] {
			continue
		}
		seen[other] = true
		if n := g.NodeByID(other); n != nil {
			res = append(res, nodeRef{Name: n.Name, Kind: n.Kind, File: n.File, Line: n.Line})
		}
	}
	return res
}

func whoStarts(g *graph.Graph, name string) []nodeRef {
	out := []nodeRef{}
	for _, e := range g.Edges {
		if e.Kind != graph.EdgeStartsWorkflow {
			continue
		}
		if t := g.NodeByID(e.To); t != nil && t.Name == name {
			if f := g.NodeByID(e.From); f != nil {
				out = append(out, nodeRef{Name: f.Name, Kind: f.Kind, File: e.File, Line: e.Line})
			}
		}
	}
	return out
}

func unresolved(g *graph.Graph) []edgeRef {
	out := []edgeRef{}
	for _, e := range g.Edges {
		if e.Resolution == graph.Resolved {
			continue
		}
		from := e.From
		if f := g.NodeByID(e.From); f != nil {
			from = f.Name
		}
		target := e.TargetName
		if target == "" {
			target = fmt.Sprintf("(dynamic @ %s:%d)", e.File, e.Line)
		}
		out = append(out, edgeRef{Kind: e.Kind, Resolution: e.Resolution, From: from, Target: target, File: e.File, Line: e.Line})
	}
	return out
}
