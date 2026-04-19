package permissions

import (
	"fmt"
	"strings"
)

// --- Risk Explainer ---

// explanationMaxChars caps the rendered explanation length. The UI layer
// renders these inline, so a short bound keeps transcripts readable.
const explanationMaxChars = 200

// ExplainDenial returns a short, human-readable reason that a tool invocation
// was denied. Despite the "llm" framing in the surrounding design docs, this
// function is deterministic text generation: it does not call any model and
// makes no network requests. The name is kept for parity with the reference
// harness; the implementation is a plain Go template.
//
// The output is always capped at 200 characters. If inputs are empty the
// function still returns a useful sentence so callers never have to special
// case missing fields.
func ExplainDenial(rule Rule, tool string, input interface{}) string {
	arg := strings.TrimSpace(extractArg(input))
	toolLabel := tool
	if toolLabel == "" {
		toolLabel = "unknown tool"
	}

	var msg string
	switch {
	case rule.Pattern == "" && arg == "":
		msg = fmt.Sprintf("denied %s: no matching allow rule.", toolLabel)
	case rule.Pattern == "":
		msg = fmt.Sprintf("denied %s: no matching allow rule. attempted: %s", toolLabel, arg)
	case arg == "":
		msg = fmt.Sprintf("denied because %s matches the %q rule.", toolLabel, rule.Pattern)
	default:
		msg = fmt.Sprintf(
			"denied because this %s call matches the %q rule. attempted: %s",
			toolLabel, rule.Pattern, arg,
		)
	}
	return truncate(msg, explanationMaxChars)
}

// truncate shortens s to max runes, appending an ellipsis marker when cut.
func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}
