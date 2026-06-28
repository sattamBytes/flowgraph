package check

import (
	"os"
	"strings"
)

// suppressMarker is the inline directive. Place it on the offending line or the
// line directly above it:
//
//	client.ExecuteWorkflow(ctx, o, name) //tcg:ignore unknown-name
//
// With no rule names it suppresses every rule on that line; with one or more
// rule names it suppresses only those.
const suppressMarker = "tcg:ignore"

// FilterSuppressed removes findings silenced by an inline //tcg:ignore comment.
// It reads the source files directly, so suppression needs no state in the graph.
func FilterSuppressed(findings []Finding) []Finding {
	cache := map[string][]string{}
	out := findings[:0:0]
	for _, f := range findings {
		if !suppressed(cache, f) {
			out = append(out, f)
		}
	}
	return out
}

func suppressed(cache map[string][]string, f Finding) bool {
	lines, ok := cache[f.File]
	if !ok {
		b, err := os.ReadFile(f.File)
		if err == nil {
			lines = strings.Split(string(b), "\n")
		}
		cache[f.File] = lines
	}
	// Same line: a trailing or whole-line directive suppresses this finding.
	if f.Line >= 1 && f.Line <= len(lines) && directiveMatches(lines[f.Line-1], f.Rule) {
		return true
	}
	// Line above: only a comment-ONLY line suppresses the line below it, so a
	// trailing directive on a code line does not bleed onto the next statement.
	if above := f.Line - 1; above >= 1 && above <= len(lines) {
		if line := lines[above-1]; commentOnly(line) && directiveMatches(line, f.Rule) {
			return true
		}
	}
	return false
}

func commentOnly(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "//")
}

func directiveMatches(line, rule string) bool {
	i := strings.Index(line, suppressMarker)
	if i < 0 {
		return false
	}
	rest := strings.TrimSpace(line[i+len(suppressMarker):])
	if rest == "" {
		return true // bare marker suppresses all rules on the line
	}
	for _, tok := range strings.Fields(rest) {
		if tok == rule {
			return true
		}
	}
	return false
}
