package subagent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFilterToolsAll(t *testing.T) {
	allTools := []string{"Read", "Write", "Edit", "Bash", "Glob", "Grep", "Task", "AskUserQuestion"}

	agent := AgentType{
		Tools: []string{"*"},
	}

	result := FilterTools(allTools, agent)

	assert.Contains(t, result, "Read")
	assert.Contains(t, result, "Write")
	assert.Contains(t, result, "Bash")
	assert.NotContains(t, result, "Task", "Task should be removed by default disallowed list")
	assert.NotContains(t, result, "AskUserQuestion", "AskUserQuestion should be removed")
}

func TestFilterToolsSpecific(t *testing.T) {
	allTools := []string{"Read", "Write", "Edit", "Bash", "Glob", "Grep"}

	agent := AgentType{
		Tools: []string{"Read", "Grep"},
	}

	result := FilterTools(allTools, agent)

	assert.Equal(t, []string{"Read", "Grep"}, result)
}

func TestFilterToolsDisallowed(t *testing.T) {
	allTools := []string{"Read", "Write", "Task", "Bash", "EnterPlanMode", "ExitPlanMode"}

	agent := AgentType{
		Tools: []string{"*"},
	}

	result := FilterTools(allTools, agent)

	assert.NotContains(t, result, "Task")
	assert.NotContains(t, result, "EnterPlanMode")
	assert.NotContains(t, result, "ExitPlanMode")
	assert.Contains(t, result, "Read")
	assert.Contains(t, result, "Write")
	assert.Contains(t, result, "Bash")
}

func TestFilterToolsCustomDisallowed(t *testing.T) {
	allTools := []string{"Read", "Write", "Bash", "WebFetch"}

	agent := AgentType{
		Tools:           []string{"*"},
		DisallowedTools: []string{"WebFetch"},
	}

	result := FilterTools(allTools, agent)

	assert.NotContains(t, result, "WebFetch")
	assert.Contains(t, result, "Read")
}

func TestFilterToolsExplicitOverridesDeny(t *testing.T) {
	allTools := []string{"Read", "Task", "Bash"}

	// Agent explicitly requests Task - should be allowed since non-wildcard.
	agent := AgentType{
		Tools: []string{"Read", "Task"},
	}

	result := FilterTools(allTools, agent)

	assert.Contains(t, result, "Task", "explicit tool list should override global deny")
	assert.Contains(t, result, "Read")
}
