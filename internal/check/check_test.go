package check

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/sattamBytes/flowgraph/internal/analyze"
)

func sampleFindings(t *testing.T) []Finding {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "testdata", "sample"))
	if err != nil {
		t.Fatal(err)
	}
	g, err := analyze.Analyze(dir)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	return Check(g)
}

func byRule(fs []Finding) map[string][]Finding {
	m := map[string][]Finding{}
	for _, f := range fs {
		m[f.Rule] = append(m[f.Rule], f)
	}
	return m
}

// TestEveryRuleFiresExactlyOnce asserts each planted bug produces exactly one
// finding — the rule fires on the bug and stays quiet on the clean parts.
func TestEveryRuleFiresExactlyOnce(t *testing.T) {
	m := byRule(sampleFindings(t))
	for _, rule := range []string{
		"task-queue-mismatch", "unknown-name", "orphan",
		"signal-mismatch", "missing-timeout", "missing-retry",
	} {
		if got := len(m[rule]); got != 1 {
			t.Errorf("rule %q fired %d times, want 1: %+v", rule, got, m[rule])
		}
	}
	// ShippingWorkflow plants four distinct non-determinism smells.
	if got := len(m["non-determinism"]); got != 4 {
		t.Errorf("non-determinism fired %d times, want 4: %+v", got, m["non-determinism"])
	}
}

func TestTaskQueueMismatchIsError(t *testing.T) {
	m := byRule(sampleFindings(t))
	f := m["task-queue-mismatch"]
	if len(f) != 1 || f[0].Severity != SeverityError {
		t.Fatalf("task-queue-mismatch = %+v, want one error", f)
	}
	if !bytes.Contains([]byte(f[0].Message), []byte("payments")) {
		t.Errorf("message should name the wrong queue: %q", f[0].Message)
	}
}

func TestUnknownNameSuggestsCorrection(t *testing.T) {
	m := byRule(sampleFindings(t))
	f := m["unknown-name"]
	if len(f) != 1 || f[0].Severity != SeverityError {
		t.Fatalf("unknown-name = %+v, want one error", f)
	}
	if f[0].Suggestion != `did you mean "OrderWorkflow"?` {
		t.Errorf("suggestion = %q, want did-you-mean OrderWorkflow", f[0].Suggestion)
	}
}

func TestCleanPartsStayQuiet(t *testing.T) {
	// ChargeCard/SendEmail have timeout+retry and ship.v1 is started on its
	// registered queue — none of those should appear in findings.
	for _, f := range sampleFindings(t) {
		if f.Rule == "missing-timeout" && f.Message != "" {
			if got := f.Message; bytesContainsAny(got, "ChargeCard", "SendEmail") {
				t.Errorf("missing-timeout fired on a clean activity: %q", got)
			}
		}
		if f.Rule == "task-queue-mismatch" && bytesContainsAny(f.Message, "ship.v1") {
			t.Errorf("task-queue-mismatch fired on the clean ship.v1 start: %q", f.Message)
		}
	}
}

func TestExitCodes(t *testing.T) {
	if ExitCode(nil) != 0 {
		t.Error("no findings should exit 0")
	}
	warnOnly := []Finding{{Rule: "orphan", Severity: SeverityWarning}}
	if ExitCode(warnOnly) != 0 {
		t.Error("warnings-only should exit 0 (don't fail CI on warnings)")
	}
	withErr := []Finding{{Rule: "x", Severity: SeverityWarning}, {Rule: "y", Severity: SeverityError}}
	if ExitCode(withErr) != 1 {
		t.Error("any error should exit 1")
	}
}

func TestSuppression(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "x.go")
	os.WriteFile(file, []byte(
		"line1\n"+
			"call() //flowgraph:ignore unknown-name\n"+ // line 2: suppress only unknown-name
			"call() //flowgraph:ignore\n"+ // line 3: suppress everything
			"call() // no directive\n", // line 4: nothing
	), 0o644)

	findings := []Finding{
		{Rule: "unknown-name", File: file, Line: 2},        // suppressed (named)
		{Rule: "orphan", File: file, Line: 2},              // kept (different rule)
		{Rule: "task-queue-mismatch", File: file, Line: 3}, // suppressed (bare)
		{Rule: "unknown-name", File: file, Line: 4},        // kept (no directive)
	}
	got := FilterSuppressed(findings)
	if len(got) != 2 {
		t.Fatalf("after suppression got %d findings, want 2: %+v", len(got), got)
	}
	for _, f := range got {
		if f.Line == 3 {
			t.Error("bare //flowgraph:ignore should have suppressed line 3")
		}
		if f.Rule == "unknown-name" && f.Line == 2 {
			t.Error("named directive should have suppressed unknown-name on line 2")
		}
	}
}

func TestSuppressionDirectiveOnLineAbove(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "y.go")
	os.WriteFile(file, []byte("//flowgraph:ignore orphan\nregisterThing()\n"), 0o644)
	got := FilterSuppressed([]Finding{{Rule: "orphan", File: file, Line: 2}})
	if len(got) != 0 {
		t.Errorf("directive on the line above should suppress, got %+v", got)
	}
}

func TestPrintRendersAndCounts(t *testing.T) {
	var buf bytes.Buffer
	e, w := Print(&buf, []Finding{
		{Rule: "task-queue-mismatch", Severity: SeverityError, File: "a.go", Line: 1, Message: "boom"},
		{Rule: "orphan", Severity: SeverityWarning, File: "b.go", Line: 2, Message: "meh"},
	}, false)
	if e != 1 || w != 1 {
		t.Errorf("counts = %d errors %d warnings, want 1/1", e, w)
	}
	if !bytes.Contains(buf.Bytes(), []byte("a.go:1")) {
		t.Error("output should include file:line")
	}
}

func bytesContainsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if bytes.Contains([]byte(s), []byte(sub)) {
			return true
		}
	}
	return false
}
