package subagent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveBuiltinAgent(t *testing.T) {
	tests := []struct {
		name     string
		wantDesc string
	}{
		{"general-purpose", "General-purpose agent for multi-step research and execution"},
		{"Explore", "Fast read-only codebase exploration agent"},
		{"Plan", "Software architect agent for designing implementation plans"},
		{"Verification", "Adversarial verification agent that checks work quality"},
		{"Code-Reviewer", "Code review agent that checks against project standards"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent, ok := ResolveAgentType(tt.name, nil)
			require.True(t, ok, "built-in agent %q should resolve", tt.name)
			assert.Equal(t, tt.name, agent.Name)
			assert.Equal(t, tt.wantDesc, agent.Description)
		})
	}
}

func TestResolveCustomOverride(t *testing.T) {
	custom := map[string]AgentType{
		"Explore": {
			Name:        "Explore",
			Description: "Custom explore override",
			Tools:       []string{"Read"},
			Model:       "slow",
		},
	}

	agent, ok := ResolveAgentType("Explore", custom)
	require.True(t, ok)
	assert.Equal(t, "Custom explore override", agent.Description)
	assert.Equal(t, "slow", agent.Model)
}

func TestResolveUnknown(t *testing.T) {
	_, ok := ResolveAgentType("nonexistent-agent", nil)
	assert.False(t, ok)
}

func TestAllBuiltinsHaveDescription(t *testing.T) {
	for name, agent := range BuiltinAgents {
		assert.NotEmpty(t, agent.Description, "builtin %q must have a description", name)
		assert.Equal(t, name, agent.Name, "builtin key must match Name field")
	}
}

func TestExploreTools(t *testing.T) {
	explore, ok := BuiltinAgents["Explore"]
	require.True(t, ok)
	assert.ElementsMatch(t, []string{"Read", "Glob", "Grep", "Bash"}, explore.Tools)
}

func TestPlanPermissionMode(t *testing.T) {
	plan, ok := BuiltinAgents["Plan"]
	require.True(t, ok)
	assert.Equal(t, "plan", plan.PermissionMode)
}

func TestVerificationBackground(t *testing.T) {
	verification, ok := BuiltinAgents["Verification"]
	require.True(t, ok)
	assert.True(t, verification.Background, "Verification agent should run in background")
}
