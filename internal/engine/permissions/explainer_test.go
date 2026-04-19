package permissions

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

func TestExplainDenialIncludesRuleAndInput(t *testing.T) {
	rule := Rule{Pattern: "Bash(rm -rf *)", Behavior: Deny, Source: "policySettings"}
	out := ExplainDenial(rule, "Bash", bashInput("rm -rf ~/.config"))

	assert.Contains(t, out, "Bash")
	assert.Contains(t, out, "Bash(rm -rf *)")
	assert.Contains(t, out, "rm -rf ~/.config")
}

func TestExplainDenialCapAt200Chars(t *testing.T) {
	longPattern := "Bash(" + strings.Repeat("x", 500) + ")"
	rule := Rule{Pattern: longPattern, Behavior: Deny}
	out := ExplainDenial(rule, "Bash", bashInput(strings.Repeat("y", 500)))

	assert.LessOrEqual(t, utf8.RuneCountInString(out), 200)
}

func TestExplainDenialEmptyInputsStillReadable(t *testing.T) {
	out := ExplainDenial(Rule{}, "", nil)
	assert.NotEmpty(t, out)
	assert.Contains(t, out, "unknown tool")
}

func TestExplainDenialNoPatternStillReports(t *testing.T) {
	out := ExplainDenial(Rule{}, "Bash", bashInput("rm -rf /"))
	assert.Contains(t, out, "Bash")
	assert.Contains(t, out, "rm -rf /")
}

func TestExplainDenialNoInputUsesPatternOnly(t *testing.T) {
	rule := Rule{Pattern: "Write", Behavior: Deny}
	out := ExplainDenial(rule, "Write", nil)
	assert.Contains(t, out, "Write")
	assert.Contains(t, out, `"Write"`)
}
