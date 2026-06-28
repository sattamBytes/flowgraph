// Package export renders the canonical graph into docs-friendly diagrams.
package export

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sattamBytes/temporal-code-graph/internal/graph"
)

// ids assigns each node a stable, syntax-safe short id (n0, n1, ...).
func ids(g *graph.Graph) map[string]string {
	keys := make([]string, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		keys = append(keys, n.ID)
	}
	sort.Strings(keys)
	m := make(map[string]string, len(keys))
	for i, k := range keys {
		m[k] = fmt.Sprintf("n%d", i)
	}
	return m
}

func label(n graph.Node) string {
	if len(n.TaskQueues) > 0 {
		return fmt.Sprintf("%s\n[%s]", n.Name, strings.Join(n.TaskQueues, ","))
	}
	return n.Name
}

// Mermaid renders a Mermaid flowchart. Unresolved edges are dashed.
func Mermaid(g *graph.Graph) string {
	id := ids(g)
	var b strings.Builder
	b.WriteString("flowchart LR\n")
	for _, n := range g.Nodes {
		shape := nodeShape(n.Kind, label(n))
		fmt.Fprintf(&b, "  %s%s:::%s\n", id[n.ID], shape, strings.ToLower(n.Kind))
	}
	for _, e := range g.Edges {
		arrow := "-->"
		if e.Resolution != graph.Resolved {
			arrow = "-.->"
		}
		lbl := e.Kind
		if e.TaskQueue != "" {
			lbl += " @" + e.TaskQueue
		}
		if e.Resolution != graph.Resolved {
			lbl += " (" + e.Resolution + ")"
		}
		fmt.Fprintf(&b, "  %s %s|%q| %s\n", id[e.From], arrow, lbl, id[e.To])
	}
	b.WriteString("  classDef workflow fill:#dbeafe,stroke:#2563eb;\n")
	b.WriteString("  classDef activity fill:#dcfce7,stroke:#16a34a;\n")
	b.WriteString("  classDef controlplanecaller fill:#fef9c3,stroke:#ca8a04;\n")
	b.WriteString("  classDef signal fill:#fae8ff,stroke:#a21caf;\n")
	b.WriteString("  classDef query fill:#ffedd5,stroke:#ea580c;\n")
	return b.String()
}

func nodeShape(kind, lbl string) string {
	lbl = strings.ReplaceAll(lbl, "\n", "<br/>")
	switch kind {
	case graph.KindWorkflow:
		return fmt.Sprintf("[%q]", lbl)
	case graph.KindActivity:
		return fmt.Sprintf("([%q])", lbl)
	case graph.KindControlPlaneCaller:
		return fmt.Sprintf("[/%q/]", lbl)
	default: // signal, query
		return fmt.Sprintf("{{%q}}", lbl)
	}
}

// Dot renders Graphviz DOT. Unresolved edges are dashed.
func Dot(g *graph.Graph) string {
	id := ids(g)
	var b strings.Builder
	b.WriteString("digraph temporal {\n  rankdir=LR;\n  node [shape=box,style=rounded];\n")
	for _, n := range g.Nodes {
		lbl := strings.ReplaceAll(label(n), "\n", "\\n")
		fmt.Fprintf(&b, "  %s [label=%q,fillcolor=%q,style=\"rounded,filled\"];\n", id[n.ID], lbl, dotColor(n.Kind))
	}
	for _, e := range g.Edges {
		style := "solid"
		if e.Resolution != graph.Resolved {
			style = "dashed"
		}
		lbl := e.Kind
		if e.TaskQueue != "" {
			lbl += " @" + e.TaskQueue
		}
		fmt.Fprintf(&b, "  %s -> %s [label=%q,style=%s];\n", id[e.From], id[e.To], lbl, style)
	}
	b.WriteString("}\n")
	return b.String()
}

func dotColor(kind string) string {
	switch kind {
	case graph.KindWorkflow:
		return "#dbeafe"
	case graph.KindActivity:
		return "#dcfce7"
	case graph.KindControlPlaneCaller:
		return "#fef9c3"
	case graph.KindSignal:
		return "#fae8ff"
	case graph.KindQuery:
		return "#ffedd5"
	}
	return "#eeeeee"
}
