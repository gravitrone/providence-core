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

// TestBuiltinAgentImplementerLimits pins the Implementer contract: it
// must carry a bounded MaxTurns so plan-step execution cannot runaway,
// a non-empty SystemPrompt, and a tool set that includes write-capable
// tools (the agent explicitly implements code changes).
func TestBuiltinAgentImplementerLimits(t *testing.T) {
	t.Parallel()

	impl, ok := BuiltinAgents["Implementer"]
	require.True(t, ok, "Implementer must be a registered built-in agent")

	assert.Greater(t, impl.MaxTurns, 0, "MaxTurns must be bounded, not 0/unlimited")
	assert.LessOrEqual(t, impl.MaxTurns, 100, "MaxTurns must be a practical cap, not astronomical")
	assert.NotEmpty(t, impl.SystemPrompt, "SystemPrompt must be populated - the agent has nothing to follow otherwise")
	assert.Equal(t, []string{"*"}, impl.Tools, "Implementer writes code and must have the full tool set")
}

// TestBuiltinAgentSpecReviewerIsReadOnly verifies the Spec-Reviewer
// cannot mutate state. It uses DisallowedTools (denylist) rather than
// an Allow-style whitelist, so this test pins every write-capable tool
// the reviewer MUST NOT gain. If someone adds a new write tool to the
// registry, they must remember to deny it here as well.
func TestBuiltinAgentSpecReviewerIsReadOnly(t *testing.T) {
	t.Parallel()

	reviewer, ok := BuiltinAgents["Spec-Reviewer"]
	require.True(t, ok, "Spec-Reviewer must be a registered built-in agent")

	deniedTools := []string{"Edit", "Write", "NotebookEdit", "Agent"}
	for _, dt := range deniedTools {
		assert.Contains(t, reviewer.DisallowedTools, dt,
			"Spec-Reviewer must deny %q to stay strictly read-only", dt)
	}
	assert.Greater(t, reviewer.MaxTurns, 0, "MaxTurns must be bounded")
	assert.LessOrEqual(t, reviewer.MaxTurns, 50, "Review work should not sprawl")
}

// TestBuiltinAgentVerificationMaxTurnsBounded verifies the Verification
// agent has a concrete MaxTurns cap so background verification work
// cannot burn the API budget indefinitely if the agent gets stuck.
func TestBuiltinAgentVerificationMaxTurnsBounded(t *testing.T) {
	t.Parallel()

	ver, ok := BuiltinAgents["Verification"]
	require.True(t, ok)

	assert.Greater(t, ver.MaxTurns, 0, "MaxTurns must be bounded, not 0 (unlimited)")
	assert.LessOrEqual(t, ver.MaxTurns, 100, "MaxTurns must be a practical cap")
}
