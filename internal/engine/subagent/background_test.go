package subagent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBackgroundAgentsRegistryDefaults pins structural invariants on every
// entry in the BackgroundAgents registry. Each entry must carry a
// non-empty Name, Description, SystemPrompt, and a TriggerOn value drawn
// from the supported enum (tool_use_turn, every_turn, on_demand). A new
// contributor adding an entry that forgets one of these will fail loudly
// here rather than crash at runtime when the scheduler reaches it.
func TestBackgroundAgentsRegistryDefaults(t *testing.T) {
	t.Parallel()

	allowedTriggers := map[string]bool{
		"tool_use_turn": true,
		"every_turn":    true,
		"on_demand":     true,
	}

	require.NotEmpty(t, BackgroundAgents, "registry must ship with at least one default agent")

	for key, agent := range BackgroundAgents {
		t.Run(key, func(t *testing.T) {
			assert.NotEmpty(t, agent.Name, "Name")
			assert.Equal(t, key, agent.Name, "registry key must match agent.Name")
			assert.NotEmpty(t, agent.Description, "Description")
			assert.NotEmpty(t, agent.SystemPrompt, "SystemPrompt")
			assert.True(t, allowedTriggers[agent.TriggerOn],
				"TriggerOn=%q must be one of tool_use_turn / every_turn / on_demand", agent.TriggerOn)
			assert.Greater(t, agent.MaxTurns, 0, "MaxTurns must be bounded, not 0 (runaway loop guard)")
		})
	}
}

// TestBackgroundAgentRedTeamAdvisorConfig pins the contract for the
// Red-Team-Advisor: tool_use_turn trigger, fire-and-forget, and a
// SystemPrompt that still mentions the drift / missed-requirements /
// repeat-errors duties. Those three phrases are the checks the scheduler
// expects the model to perform, so silently dropping any of them turns
// the agent into dead weight.
func TestBackgroundAgentRedTeamAdvisorConfig(t *testing.T) {
	t.Parallel()

	agent, ok := BackgroundAgents["Red-Team-Advisor"]
	require.True(t, ok, "Red-Team-Advisor must be registered")

	assert.Equal(t, "tool_use_turn", agent.TriggerOn)
	assert.True(t, agent.FireAndForget, "Red-Team-Advisor must be fire-and-forget so it does not block the main loop")
	lower := strings.ToLower(agent.SystemPrompt)
	assert.Contains(t, lower, "drift", "SystemPrompt must keep the drift check")
	assert.Contains(t, lower, "missed requirements", "SystemPrompt must keep the missed-requirements check")
	assert.Contains(t, lower, "repeat errors", "SystemPrompt must keep the repeat-errors check")
}

// TestBackgroundAgentSmartPreprocessorConfig pins the contract for the
// Smart-Pre-processor: every-turn trigger, blocking (NOT fire-and-forget)
// because the main model consumes its injected context, and a bounded
// tool set so preprocessing cannot runaway-research.
func TestBackgroundAgentSmartPreprocessorConfig(t *testing.T) {
	t.Parallel()

	agent, ok := BackgroundAgents["Smart-Pre-processor"]
	require.True(t, ok, "Smart-Pre-processor must be registered")

	assert.Equal(t, "every_turn", agent.TriggerOn)
	assert.False(t, agent.FireAndForget, "Smart-Pre-processor must be blocking - its output is injected into the next turn's context")
	assert.NotEmpty(t, agent.Tools, "Tools must be bounded so the preprocessor cannot runaway-research")
	assert.LessOrEqual(t, agent.MaxTurns, 5, "MaxTurns must cap preprocessing budget")
}
