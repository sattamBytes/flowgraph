// Command fg is the flowgraph CLI: it statically maps how a Go app flows —
// from REST/Temporal entrypoints, through function calls and branches, and
// across Temporal's connect-by-name boundary into workflows and activities.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/sattamBytes/flowgraph/internal/analyze"
	"github.com/sattamBytes/flowgraph/internal/check"
	"github.com/sattamBytes/flowgraph/internal/export"
	"github.com/sattamBytes/flowgraph/internal/graph"
	"github.com/sattamBytes/flowgraph/internal/mcp"
	"github.com/sattamBytes/flowgraph/internal/serve"
	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   "fg",
		Short: "flowgraph: static code-flow analysis for Go (REST + Temporal aware)",
		Long: "fg statically maps how a Go application flows: from entrypoints (REST routes,\n" +
			"Temporal workflows) through function calls and the branches that guard them, and\n" +
			"across Temporal's connect-by-name boundary into workflows and activities.\n" +
			"It never executes your code.",
	}
	root.AddCommand(buildCmd(), checkCmd(), exportCmd(), serveCmd(), mcpCmd(), listCmd())
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}
}

func buildCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "build <path>",
		Short: "Emit the canonical JSON graph",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			g, err := analyze.Analyze(args[0])
			if err != nil {
				return err
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(g)
		},
	}
}

func checkCmd() *cobra.Command {
	var jsonOut bool
	c := &cobra.Command{
		Use:   "check <path>",
		Short: "Run lint rules; non-zero exit on errors (CI gate)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			g, err := analyze.Analyze(args[0])
			if err != nil {
				return err
			}
			findings := check.FilterSuppressed(check.Check(g))
			if jsonOut {
				if err := check.PrintJSON(os.Stdout, findings); err != nil {
					return err
				}
			} else {
				check.Print(os.Stdout, findings, useColor())
			}
			os.Exit(check.ExitCode(findings))
			return nil
		},
	}
	c.Flags().BoolVar(&jsonOut, "json", false, "machine-readable JSON output")
	return c
}

func exportCmd() *cobra.Command {
	var format string
	c := &cobra.Command{
		Use:   "export <path> --format mermaid|dot",
		Short: "Render a docs-friendly diagram",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			g, err := analyze.Analyze(args[0])
			if err != nil {
				return err
			}
			switch format {
			case "mermaid":
				fmt.Print(export.Mermaid(g))
			case "dot":
				fmt.Print(export.Dot(g))
			default:
				return fmt.Errorf("unknown --format %q (want mermaid or dot)", format)
			}
			return nil
		},
	}
	c.Flags().StringVar(&format, "format", "mermaid", "diagram format: mermaid or dot")
	return c
}

func serveCmd() *cobra.Command {
	var addr, graphFile string
	var open bool
	c := &cobra.Command{
		Use:   "serve [path]",
		Short: "Launch the interactive web dashboard",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			g, err := loadGraph(args, graphFile)
			if err != nil {
				return err
			}
			return serve.Serve(g, addr, open)
		},
	}
	c.Flags().StringVar(&addr, "addr", "localhost:8080", "listen address")
	c.Flags().StringVar(&graphFile, "graph", "", "use a prebuilt graph.json instead of analyzing source")
	c.Flags().BoolVar(&open, "open", false, "open the dashboard in a browser")
	return c
}

func mcpCmd() *cobra.Command {
	var graphFile string
	c := &cobra.Command{
		Use:   "mcp [path]",
		Short: "Start the MCP server (stdio) for AI agents",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			g, err := loadGraph(args, graphFile)
			if err != nil {
				return err
			}
			return mcp.Serve(g)
		},
	}
	c.Flags().StringVar(&graphFile, "graph", "", "use a prebuilt graph.json instead of analyzing source")
	return c
}

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list <path>",
		Short: "List detected entrypoints (REST/gRPC routes and workflows)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			g, err := analyze.Analyze(args[0])
			if err != nil {
				return err
			}
			var rest, wf []graph.Node
			for _, n := range g.Nodes {
				switch n.Kind {
				case graph.KindRESTEndpoint, graph.KindGRPCEndpoint:
					rest = append(rest, n)
				case graph.KindWorkflow:
					if n.Registered {
						wf = append(wf, n)
					}
				}
			}
			fmt.Printf("HTTP / gRPC entrypoints (%d):\n", len(rest))
			for _, n := range rest {
				fmt.Printf("  %-22s %s  (%s:%d)\n", n.Name, n.HandlerSymbol, n.File, n.Line)
			}
			fmt.Printf("\nWorkflows (%d):\n", len(wf))
			for _, n := range wf {
				fmt.Printf("  %-22s queues=%v  (%s:%d)\n", n.Name, n.TaskQueues, n.File, n.Line)
			}
			return nil
		},
	}
}

// loadGraph reads a prebuilt graph.json if given, else analyzes source at args[0].
func loadGraph(args []string, graphFile string) (*graph.Graph, error) {
	if graphFile != "" {
		b, err := os.ReadFile(graphFile)
		if err != nil {
			return nil, err
		}
		var g graph.Graph
		if err := json.Unmarshal(b, &g); err != nil {
			return nil, err
		}
		return &g, nil
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("provide a source path or --graph graph.json")
	}
	return analyze.Analyze(args[0])
}

// useColor reports whether to emit ANSI color: a terminal stdout and no NO_COLOR.
func useColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
