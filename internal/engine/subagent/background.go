package subagent

// BackgroundAgentType defines agents that run continuously watching the
// session. They are triggered automatically based on conversation events.
type BackgroundAgentType struct {
	AgentType
	TriggerOn     string // "tool_use_turn", "every_turn", "on_demand"
	FireAndForget bool   // true = don't wait for result before continuing
}

// BackgroundAgents is the registry of built-in background agent types.
var BackgroundAgents = map[string]BackgroundAgentType{
	"Red-Team-Advisor": {
		AgentType: AgentType{
			Name:        "Red-Team-Advisor",
			Description: "Watches for drift, missed requirements, repeat errors",
			Tools:       []string{},
			Model:       "fast",
			MaxTurns:    1,
			SystemPrompt: "You are a Red Team Advisor. Review the last assistant response and tool calls. " +
				"Check for: drift from original task, missed requirements, repeat errors (same error 3+ times), " +
				"stale file assumptions, unnecessary complexity. " +
				"If issues found: output a <system-reminder> block with the issue. " +
				`If no issues: output "LGTM" (nothing injected).`,
		},
		TriggerOn:     "tool_use_turn",
		FireAndForget: true,
	},
	"Smart-Pre-processor": {
		AgentType: AgentType{
			Name:        "Smart-Pre-processor",
			Description: "Enriches user message with relevant file snippets",
			Tools:       []string{"Read", "Grep", "Glob"},
			Model:       "fast",
			MaxTurns:    3,
			SystemPrompt: "You are a Smart Pre-processor. Given the user's message, find 1-3 relevant code snippets " +
				"from the project that would help the main model answer better. Output them as context. " +
				"Be fast - max 3 tool calls. Don't over-research.",
		},
		TriggerOn:     "every_turn",
		FireAndForget: false, // wait for result, inject into context
	},
}
