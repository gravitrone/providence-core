package permissions

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMatchExact(t *testing.T) {
	assert.True(t, matchPattern("Bash", "Bash", nil))
	assert.False(t, matchPattern("Bash", "Read", nil))
}

func TestMatchWithGlob(t *testing.T) {
	assert.True(t, matchPattern("Bash(git *)", "Bash", bashInput("git push")))
	assert.True(t, matchPattern("Bash(git *)", "Bash", bashInput("git status")))
}

func TestMatchWithPath(t *testing.T) {
	assert.True(t, matchPattern("Read(/home/*)", "Read", fileInput("/home/user")))
	assert.True(t, matchPattern("Write(/tmp/*)", "Write", fileInput("/tmp/out.txt")))
}

func TestMatchNoMatch(t *testing.T) {
	assert.False(t, matchPattern("Bash(npm *)", "Bash", bashInput("git push")))
	assert.False(t, matchPattern("Read(/etc/*)", "Read", fileInput("/home/user")))
}

func TestIsSafetyPathGit(t *testing.T) {
	assert.True(t, isSafetyPath("Bash", bashInput("cat .git/hooks/pre-commit")))
	assert.True(t, isSafetyPath("Write", fileInput("/repo/.git/config")))
}

func TestIsSafetyPathClaude(t *testing.T) {
	assert.True(t, isSafetyPath("Write", fileInput("/project/.claude/settings.json")))
	assert.True(t, isSafetyPath("Edit", fileInput("/home/user/.claude/config.toml")))
}

func TestNormalPathNotSafety(t *testing.T) {
	assert.False(t, isSafetyPath("Read", fileInput("internal/main.go")))
	assert.False(t, isSafetyPath("Bash", bashInput("go build ./...")))
	assert.False(t, isSafetyPath("Write", fileInput("/tmp/output.txt")))
}

// TestMatchPatternEmptyEdges covers empty-pattern / empty-tool / empty-arg edges.
// matchPattern uses filepath.Match: "" only matches "".
func TestMatchPatternEmptyEdges(t *testing.T) {
	// empty pattern vs empty tool -> match
	assert.True(t, matchPattern("", "", nil))
	// empty pattern vs non-empty tool -> no match
	assert.False(t, matchPattern("", "Bash", nil))
	// non-empty plain pattern vs empty tool -> no match
	assert.False(t, matchPattern("Bash", "", nil))
	// parenthesized pattern with empty arg glob requires empty extracted arg
	assert.False(t, matchPattern("Bash()", "Bash", bashInput("anything")))
	assert.False(t, matchPattern("Bash()", "Bash", nil)) // extractArg returns "" -> short-circuit false
}

// TestMatchPatternCaseSensitive verifies matcher is case-sensitive for both
// tool names and arg globs (filepath.Match is case-sensitive on unix).
func TestMatchPatternCaseSensitive(t *testing.T) {
	assert.False(t, matchPattern("Bash", "bash", nil))
	assert.False(t, matchPattern("bash", "Bash", nil))
	assert.False(t, matchPattern("Read(/HOME/*)", "Read", fileInput("/home/user")))
	assert.True(t, matchPattern("Read(/home/*)", "Read", fileInput("/home/user")))
}

// TestMatchPatternNestedGlobs covers overlapping patterns like "Bash(npm:*)"
// vs "Bash(npm:install *)". Both should match their intended inputs; the
// matcher itself does not order patterns - matchesRules is first-match-wins
// in slice order, so callers supply precedence via list ordering.
func TestMatchPatternNestedGlobs(t *testing.T) {
	// broader pattern matches broader input
	assert.True(t, matchPattern("Bash(npm:*)", "Bash", bashInput("npm:install")))
	assert.True(t, matchPattern("Bash(npm:*)", "Bash", bashInput("npm:run")))
	// narrower pattern still matches its specific input
	assert.True(t, matchPattern("Bash(npm:install*)", "Bash", bashInput("npm:install foo")))
	// narrower does not match unrelated subcommand
	assert.False(t, matchPattern("Bash(npm:install*)", "Bash", bashInput("npm:run test")))

	// First-match-wins: whichever rule is first in the slice decides.
	rules := []Rule{
		{Pattern: "Bash(npm:*)", Behavior: Allow, Source: "userSettings"},
		{Pattern: "Bash(npm:install*)", Behavior: Allow, Source: "userSettings"},
	}
	assert.True(t, matchesRules(rules, "Bash", bashInput("npm:install foo")))
}

// TestMatchPatternMissingCloseParen guards the "(" without ")" early return.
func TestMatchPatternMissingCloseParen(t *testing.T) {
	assert.False(t, matchPattern("Bash(git *", "Bash", bashInput("git push")))
	assert.False(t, matchPattern("Bash(", "Bash", bashInput("anything")))
}

// TestChainDenyBeatsAllowAcrossSources verifies a deny rule from any source
// trumps an allow rule from any source (deny-wins invariant). Tests a few
// source permutations to ensure Source field does not affect precedence.
func TestChainDenyBeatsAllowAcrossSources(t *testing.T) {
	sourcePairs := []struct {
		denySrc, allowSrc string
	}{
		{"policySettings", "userSettings"},
		{"userSettings", "projectSettings"},
		{"projectSettings", "localSettings"},
		{"localSettings", "flagSettings"},
		{"flagSettings", "session"},
		{"session", "policySettings"},
	}
	for _, sp := range sourcePairs {
		t.Run(sp.denySrc+"_beats_"+sp.allowSrc, func(t *testing.T) {
			ctx := baseCtx(ModeDefault)
			ctx.AlwaysDenyRules = []Rule{{Pattern: "Bash", Behavior: Deny, Source: sp.denySrc}}
			ctx.AlwaysAllowRules = []Rule{{Pattern: "Bash", Behavior: Allow, Source: sp.allowSrc}}
			result := CheckPermission(ctx, "Bash", bashInput("ls"))
			assert.Equal(t, Deny, result.Decision)
		})
	}
}

// TestChainWildcardOverrideAcrossSources verifies wildcard patterns apply
// the same way regardless of Source, and a narrower deny rule mixed into
// the list still blocks an allow.
func TestChainWildcardOverrideAcrossSources(t *testing.T) {
	ctx := baseCtx(ModeDefault)
	ctx.AlwaysAllowRules = []Rule{
		{Pattern: "mcp__github__*", Behavior: Allow, Source: "userSettings"},
	}
	ctx.AlwaysDenyRules = []Rule{
		{Pattern: "mcp__github__delete_repo", Behavior: Deny, Source: "policySettings"},
	}

	// Narrow deny wins over wildcard allow.
	result := CheckPermission(ctx, "mcp__github__delete_repo", nil)
	assert.Equal(t, Deny, result.Decision)

	// Sibling tool under the wildcard is allowed.
	result = CheckPermission(ctx, "mcp__github__list_prs", nil)
	assert.Equal(t, Allow, result.Decision)
	assert.Contains(t, result.Reason, "always-allow")
}
