// Command tcg is the temporal-code-graph CLI: it statically analyzes Temporal
// Go projects and reconnects the control plane to workflows/activities by name.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/sattamBytes/temporal-code-graph/internal/analyze"
	"github.com/sattamBytes/temporal-code-graph/internal/check"
	"github.com/sattamBytes/temporal-code-graph/internal/export"
	"github.com/sattamBytes/temporal-code-graph/internal/graph"
	"github.com/sattamBytes/temporal-code-graph/internal/mcp"
	"github.com/sattamBytes/temporal-code-graph/internal/serve"
	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   "tcg",
		Short: "temporal-code-graph: static analysis for Temporal Go projects",
		Long: "tcg statically connects the control plane (code that starts workflows by NAME)\n" +
			"to the workflows, activities, child workflows, and signals they use, then lints\n" +
			"for common Temporal bugs. It never executes your code.",
	}
	root.AddCommand(buildCmd(), checkCmd(), exportCmd(), serveCmd(), mcpCmd())
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
