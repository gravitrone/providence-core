package permissions

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectShadowedRules_BasicCase(t *testing.T) {
	rules := []Rule{
		{Pattern: "Bash(git *)", Behavior: Allow, Source: "userSettings"},
		{Pattern: "Bash(git push)", Behavior: Ask, Source: "userSettings"},
	}

	found := DetectShadowedRules(rules)
	require.Len(t, found, 1)
	assert.Equal(t, "Bash(git push)", found[0].Shadowed.Pattern)
	assert.Equal(t, "Bash(git *)", found[0].ShadowedBy.Pattern)
	assert.Contains(t, found[0].Reason, "Bash(git *)")
}

func TestDetectShadowedRules_NoShadowReturnsEmpty(t *testing.T) {
	rules := []Rule{
		{Pattern: "Bash(git push)", Behavior: Ask, Source: "userSettings"},
		{Pattern: "Bash(git *)", Behavior: Allow, Source: "userSettings"},
		{Pattern: "Read(/etc/*)", Behavior: Deny, Source: "userSettings"},
	}

	found := DetectShadowedRules(rules)
	assert.Empty(t, found)
}

func TestDetectShadowedRules_DenyShadowsEverything(t *testing.T) {
	rules := []Rule{
		{Pattern: "Bash", Behavior: Deny, Source: "userSettings"},
		{Pattern: "Bash(git push)", Behavior: Allow, Source: "userSettings"},
	}

	found := DetectShadowedRules(rules)
	require.Len(t, found, 1)
	assert.Equal(t, Allow, found[0].Shadowed.Behavior)
	assert.Equal(t, Deny, found[0].ShadowedBy.Behavior)
}

func TestDetectShadowedRules_IdenticalPatterns(t *testing.T) {
	rules := []Rule{
		{Pattern: "Read", Behavior: Allow, Source: "userSettings"},
		{Pattern: "Read", Behavior: Allow, Source: "projectSettings"},
	}

	found := DetectShadowedRules(rules)
	require.Len(t, found, 1)
	assert.Equal(t, "projectSettings", found[0].Shadowed.Source)
}

func TestDetectShadowedRules_AllowDoesNotShadowDeny(t *testing.T) {
	rules := []Rule{
		{Pattern: "Bash(git *)", Behavior: Allow, Source: "userSettings"},
		{Pattern: "Bash(git push)", Behavior: Deny, Source: "userSettings"},
	}

	found := DetectShadowedRules(rules)
	assert.Empty(t, found, "a later deny is not shadowed by an earlier allow in our chain because deny rules run first")
}

func TestDetectShadowedRules_EmptyAndSingle(t *testing.T) {
	assert.Nil(t, DetectShadowedRules(nil))
	assert.Nil(t, DetectShadowedRules([]Rule{{Pattern: "Bash"}}))
}
