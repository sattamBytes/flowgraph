package check

import (
	"encoding/json"
	"fmt"
	"io"
)

// ANSI colors. Empty strings when color is disabled.
type palette struct{ red, yellow, dim, bold, reset string }

func newPalette(color bool) palette {
	if !color {
		return palette{}
	}
	return palette{red: "\033[31m", yellow: "\033[33m", dim: "\033[2m", bold: "\033[1m", reset: "\033[0m"}
}

// Print writes grouped, colored, human-friendly output and returns the count of
// errors and warnings.
func Print(w io.Writer, findings []Finding, color bool) (errors, warnings int) {
	p := newPalette(color)
	for _, f := range findings {
		if f.Severity == SeverityError {
			errors++
		} else {
			warnings++
		}
	}
	if len(findings) == 0 {
		fmt.Fprintf(w, "%s✓ no issues found%s\n", p.bold, p.reset)
		return
	}
	// errors first, then warnings; already sorted by file:line within Check.
	for _, sev := range []string{SeverityError, SeverityWarning} {
		for _, f := range findings {
			if f.Severity != sev {
				continue
			}
			icon, col := "✖", p.red
			if sev == SeverityWarning {
				icon, col = "⚠", p.yellow
			}
			fmt.Fprintf(w, "%s%s %s%s  %s%s:%d%s\n", col, icon, f.Rule, p.reset, p.dim, f.File, f.Line, p.reset)
			fmt.Fprintf(w, "    %s\n", f.Message)
			if f.Suggestion != "" {
				fmt.Fprintf(w, "    %s→ %s%s\n", p.dim, f.Suggestion, p.reset)
			}
		}
	}
	fmt.Fprintf(w, "\n%s%d error(s), %d warning(s)%s\n", p.bold, errors, warnings, p.reset)
	return
}

// PrintJSON writes findings as a JSON array for CI / editor consumption.
func PrintJSON(w io.Writer, findings []Finding) error {
	if findings == nil {
		findings = []Finding{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(findings)
}
