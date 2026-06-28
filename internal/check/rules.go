// Package check runs the lint rules over the canonical graph. Every rule
// operates purely on the graph artifact, so `check` works identically whether
// the graph came from source or from a prebuilt graph.json.
package check

import (
	"fmt"
	"sort"

	"github.com/sattamBytes/flowgraph/internal/graph"
)

// Severities.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// Finding is a single lint result.
type Finding struct {
	Rule       string `json:"rule"`
	Severity   string `json:"severity"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

// Check runs all rules and returns findings sorted by file:line.
func Check(g *graph.Graph) []Finding {
	var f []Finding
	f = append(f, ruleTaskQueueMismatch(g)...)
	f = append(f, ruleUnknownName(g)...)
	f = append(f, ruleOrphans(g)...)
	f = append(f, ruleSignalMismatch(g)...)
	f = append(f, ruleNonDeterminism(g)...)
	f = append(f, ruleMissingTimeoutRetry(g)...)
	sort.SliceStable(f, func(i, j int) bool {
		if f[i].File != f[j].File {
			return f[i].File < f[j].File
		}
		if f[i].Line != f[j].Line {
			return f[i].Line < f[j].Line
		}
		return f[i].Rule < f[j].Rule
	})
	return f
}

// Rule 1 (headline): a workflow started on queue X but registered on queue Y
// will hang forever.
func ruleTaskQueueMismatch(g *graph.Graph) []Finding {
	var out []Finding
	for _, e := range g.Edges {
		if e.Kind != graph.EdgeStartsWorkflow && e.Kind != graph.EdgeStartsChild {
			continue
		}
		if e.Resolution != graph.Resolved || e.TaskQueue == "" {
			continue
		}
		target := g.NodeByID(e.To)
		if target == nil || len(target.TaskQueues) == 0 {
			continue // unknown registration queue — can't prove a mismatch
		}
		if contains(target.TaskQueues, e.TaskQueue) {
			continue
		}
		out = append(out, Finding{
			Rule:     "task-queue-mismatch",
			Severity: SeverityError,
			File:     e.File, Line: e.Line,
			Message: fmt.Sprintf("%q is started on task queue %q but is only registered on %v — it will hang forever (no worker polls %q)",
				target.Name, e.TaskQueue, target.TaskQueues, e.TaskQueue),
			Suggestion: fmt.Sprintf("start it on %q, or register a worker for it on %q", target.TaskQueues[0], e.TaskQueue),
		})
	}
	return out
}

// Rule 2: a name referenced at a start/execute site that was never registered.
func ruleUnknownName(g *graph.Graph) []Finding {
	registered := registeredNames(g)
	var out []Finding
	for _, e := range g.Edges {
		if e.Resolution != graph.Unknown {
			continue
		}
		msg := fmt.Sprintf("%q is referenced here but no worker ever registers it (typo or dead reference)", e.TargetName)
		sug := "register it on a worker, or remove the reference"
		if did := closest(e.TargetName, registered); did != "" {
			sug = fmt.Sprintf("did you mean %q?", did)
		}
		out = append(out, Finding{
			Rule: "unknown-name", Severity: SeverityError,
			File: e.File, Line: e.Line, Message: msg, Suggestion: sug,
		})
	}
	return out
}

// Rule 3: registered but never started/executed.
func ruleOrphans(g *graph.Graph) []Finding {
	var out []Finding
	for _, n := range g.Nodes {
		if !n.Registered || n.Started {
			continue
		}
		if n.Kind != graph.KindWorkflow && n.Kind != graph.KindActivity {
			continue
		}
		verb := "started"
		if n.Kind == graph.KindActivity {
			verb = "executed"
		}
		out = append(out, Finding{
			Rule: "orphan", Severity: SeverityWarning,
			File: n.File, Line: n.Line,
			Message:    fmt.Sprintf("%s %q is registered but never %s anywhere", n.Kind, n.Name, verb),
			Suggestion: "remove it if it is dead, or wire up the caller",
		})
	}
	return out
}

// Rule 4: a signal/query is sent but no handler listens for that name.
func ruleSignalMismatch(g *graph.Graph) []Finding {
	var out []Finding
	for _, n := range g.Nodes {
		if n.Kind != graph.KindSignal && n.Kind != graph.KindQuery {
			continue
		}
		if n.HasListener {
			continue
		}
		// only flag if someone actually sends it
		send := senderEdge(g, n.ID)
		if send == nil {
			continue
		}
		out = append(out, Finding{
			Rule: "signal-mismatch", Severity: SeverityWarning,
			File: send.File, Line: send.Line,
			Message:    fmt.Sprintf("%s %q is sent but no workflow handles a %s with that name", n.Kind, n.Name, n.Kind),
			Suggestion: fmt.Sprintf("add a handler (workflow.GetSignalChannel / SetQueryHandler) for %q, or fix the name", n.Name),
		})
	}
	return out
}

// Rule 5: non-determinism smells collected during analysis.
func ruleNonDeterminism(g *graph.Graph) []Finding {
	var out []Finding
	for _, s := range g.Smells {
		out = append(out, Finding{
			Rule: "non-determinism", Severity: SeverityWarning,
			File: s.File, Line: s.Line,
			Message:    fmt.Sprintf("%s in workflow %s: %s", s.Kind, shortFunc(s.Func), s.Detail),
			Suggestion: s.Detail,
		})
	}
	return out
}

// Rule 6: an activity executed without a timeout (and/or retry policy).
func ruleMissingTimeoutRetry(g *graph.Graph) []Finding {
	seen := map[string]bool{}
	var out []Finding
	for _, e := range g.Edges {
		if e.Kind != graph.EdgeExecutesActivity {
			continue
		}
		key := fmt.Sprintf("%s:%d", e.File, e.Line)
		if seen[key] {
			continue
		}
		seen[key] = true
		target := g.NodeByID(e.To)
		name := e.TargetName
		if name == "" && target != nil {
			name = target.Name
		}
		if !e.HasTimeout {
			out = append(out, Finding{
				Rule: "missing-timeout", Severity: SeverityWarning,
				File: e.File, Line: e.Line,
				Message:    fmt.Sprintf("activity %q is executed with no StartToClose/ScheduleToClose timeout", name),
				Suggestion: "set workflow.WithActivityOptions(ctx, ActivityOptions{StartToCloseTimeout: ...})",
			})
		}
		if !e.HasRetry {
			out = append(out, Finding{
				Rule: "missing-retry", Severity: SeverityWarning,
				File: e.File, Line: e.Line,
				Message:    fmt.Sprintf("activity %q is executed with no RetryPolicy", name),
				Suggestion: "set a RetryPolicy in ActivityOptions (Temporal's default may not suit you)",
			})
		}
	}
	return out
}

// ExitCode returns non-zero if any error-severity finding exists, so `check`
// works as a CI gate out of the box.
func ExitCode(findings []Finding) int {
	for _, f := range findings {
		if f.Severity == SeverityError {
			return 1
		}
	}
	return 0
}

// ---- helpers ----

func senderEdge(g *graph.Graph, nodeID string) *graph.Edge {
	for i := range g.Edges {
		if g.Edges[i].To == nodeID && g.Edges[i].Kind == graph.EdgeSignals {
			return &g.Edges[i]
		}
	}
	return nil
}

func registeredNames(g *graph.Graph) []string {
	var names []string
	for _, n := range g.Nodes {
		if n.Registered {
			names = append(names, n.Name)
		}
	}
	return names
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
