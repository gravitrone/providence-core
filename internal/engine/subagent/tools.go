package subagent

// AgentDisallowedTools are always blocked for subagents to prevent
// recursion and interaction with the parent session's control flow.
var AgentDisallowedTools = []string{
	"Task",
	"AskUserQuestion",
	"EnterPlanMode",
	"ExitPlanMode",
}

// FilterTools returns the effective tool list for an agent given the full
// set of available tools. If the agent's Tools list contains "*", all tools
// are included as a starting point. DisallowedTools from the agent type and
// AgentDisallowedTools are always removed unless the agent type explicitly
// includes them in its Tools list (non-wildcard).
func FilterTools(allTools []string, agentType AgentType) []string {
	// Build the base set.
	var base []string
	if containsWildcard(agentType.Tools) {
		base = make([]string, len(allTools))
		copy(base, allTools)
	} else {
		base = make([]string, len(agentType.Tools))
		copy(base, agentType.Tools)
	}

	// Build deny set from global disallowed + agent-specific disallowed.
	deny := make(map[string]struct{}, len(AgentDisallowedTools)+len(agentType.DisallowedTools))
	for _, t := range AgentDisallowedTools {
		deny[t] = struct{}{}
	}
	for _, t := range agentType.DisallowedTools {
		deny[t] = struct{}{}
	}

	// If agent explicitly listed tools (not wildcard), those override the
	// global deny list - the agent author knows what they're doing.
	if !containsWildcard(agentType.Tools) {
		explicit := make(map[string]struct{}, len(agentType.Tools))
		for _, t := range agentType.Tools {
			explicit[t] = struct{}{}
		}
		for name := range deny {
			if _, ok := explicit[name]; ok {
				delete(deny, name)
			}
		}
	}

	// Filter.
	result := make([]string, 0, len(base))
	for _, t := range base {
		if _, blocked := deny[t]; !blocked {
			result = append(result, t)
		}
	}

	return result
}

// containsWildcard checks if the tool list includes the "*" wildcard.
func containsWildcard(tools []string) bool {
	for _, t := range tools {
		if t == "*" {
			return true
		}
	}
	return false
}
