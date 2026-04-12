package subagent

// BuiltinAgents is the registry of built-in agent types available out of the
// box. Keys are the canonical names used in /fork and Task tool invocations.
var BuiltinAgents = map[string]AgentType{
	"general-purpose": {
		Name:        "general-purpose",
		Description: "General-purpose agent for multi-step research and execution",
		Tools:       []string{"*"},
		Model:       "inherit",
		MaxTurns:    0, // unlimited
	},
	"Explore": {
		Name:        "Explore",
		Description: "Fast read-only codebase exploration agent",
		Tools:       []string{"Read", "Glob", "Grep", "Bash"},
		Model:       "fast",
		MaxTurns:    50,
		SystemPrompt: "You are an Explore agent. Search the codebase efficiently. " +
			"Read files, grep for patterns, glob for structure. Report what you find concisely. " +
			"Do NOT modify any files.",
	},
	"Plan": {
		Name:        "Plan",
		Description: "Software architect agent for designing implementation plans",
		Tools:       []string{"Read", "Glob", "Grep"},
		Model:       "inherit",
		MaxTurns:    30,
		PermissionMode: "plan",
		SystemPrompt: "You are a Plan agent. Design implementation plans. " +
			"Read code to understand architecture. Output a detailed step-by-step plan " +
			"with file paths and code sketches. Do NOT execute - only plan.",
	},
	"Verification": {
		Name:        "Verification",
		Description: "Adversarial verification agent that checks work quality",
		Tools:       []string{"Read", "Glob", "Grep", "Bash"},
		Model:       "inherit",
		MaxTurns:    30,
		Background:  true,
		SystemPrompt: "You are a Verification agent. Your job is adversarial review. " +
			"Check the work against requirements. Run tests. Look for bugs, edge cases, " +
			"security issues. Report: PASS / FAIL / PARTIAL with specific findings.",
	},
	"Code-Reviewer": {
		Name:        "Code-Reviewer",
		Description: "Code review agent that checks against project standards",
		Tools:       []string{"Read", "Glob", "Grep", "Bash"},
		Model:       "inherit",
		MaxTurns:    20,
		SystemPrompt: "You are a Code-Reviewer agent. Review recent changes against " +
			"project coding standards (see AGENTS.md). Check: naming conventions, error handling, " +
			"test coverage, documentation. Report issues with file:line references.",
	},
}

// ResolveAgentType looks up an agent type by name. Custom agents take
// priority over built-ins so projects can override default behavior.
func ResolveAgentType(name string, customAgents map[string]AgentType) (AgentType, bool) {
	if agent, ok := customAgents[name]; ok {
		return agent, true
	}
	if agent, ok := BuiltinAgents[name]; ok {
		return agent, true
	}
	return AgentType{}, false
}
