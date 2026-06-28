package export_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/sattamBytes/flowgraph/internal/analyze"
	"github.com/sattamBytes/flowgraph/internal/export"
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

func TestMermaid(t *testing.T) {
	out := export.Mermaid(load(t))
	if !strings.HasPrefix(out, "flowchart LR") {
		t.Error("mermaid should start with flowchart LR")
	}
	if !strings.Contains(out, "OrderWorkflow") {
		t.Error("mermaid missing OrderWorkflow node")
	}
	// unresolved edges are dashed
	if !strings.Contains(out, "-.->") {
		t.Error("mermaid should dash the unresolved edge")
	}
}

func TestDot(t *testing.T) {
	out := export.Dot(load(t))
	if !strings.HasPrefix(out, "digraph temporal") {
		t.Error("dot should start with digraph temporal")
	}
	if !strings.Contains(out, "style=dashed") {
		t.Error("dot should dash the unresolved edge")
	}
	if strings.Count(out, "->") < 5 {
		t.Error("dot seems to be missing edges")
	}
}
