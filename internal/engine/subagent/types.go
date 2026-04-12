package subagent

import "fmt"

// AgentType is a named agent configuration that defines how a subagent behaves.
type AgentType struct {
	Name            string
	Description     string
	SystemPrompt    string
	Tools           []string // tool allowlist ("*" = all)
	DisallowedTools []string
	Model           string // "inherit" or specific model
	Engine          string // "inherit" or specific engine
	MaxTurns        int    // 0 = unlimited
	Background      bool   // run async
	Isolation       string // "worktree" for git isolation
	PermissionMode  string
}

// TaskInput is the input schema for the Task/Agent tool.
type TaskInput struct {
	Description   string `json:"description"`
	Prompt        string `json:"prompt"`
	SubagentType  string `json:"subagent_type"`
	Model         string `json:"model,omitempty"`
	Engine        string `json:"engine,omitempty"`
	RunInBG       bool   `json:"run_in_background,omitempty"`
	Name          string `json:"name,omitempty"`
	Tools         string `json:"tools,omitempty"`          // comma-separated tool names
	MergeStrategy string `json:"merge_strategy,omitempty"` // auto|manual|vote (for /fork)
}

// TaskResult is returned when an agent completes.
type TaskResult struct {
	AgentID     string `json:"agent_id"`
	Status      string `json:"status"` // completed|failed|killed
	Result      string `json:"result"` // final text response
	TotalTokens int    `json:"total_tokens"`
	ToolUses    int    `json:"tool_uses"`
	DurationMS  int64  `json:"duration_ms"`
}

// TaskNotification is the XML-formatted notification for async agents (CC compat).
type TaskNotification struct {
	TaskID   string
	Status   string
	Summary  string
	Result   string
	Tokens   int
	ToolUses int
	Duration int64
}

// ToXML renders the notification as CC-compatible XML.
func (n TaskNotification) ToXML() string {
	return fmt.Sprintf(`<task-notification>
<task-id>%s</task-id>
<status>%s</status>
<summary>%s</summary>
<result>%s</result>
<usage><total_tokens>%d</total_tokens><tool_uses>%d</tool_uses><duration_ms>%d</duration_ms></usage>
</task-notification>`, n.TaskID, n.Status, n.Summary, n.Result, n.Tokens, n.ToolUses, n.Duration)
}

// --- Built-in Agent Types ---

// DefaultAgentType returns a general-purpose agent type for unrecognized subagent_type values.
func DefaultAgentType() AgentType {
	return AgentType{
		Name:           "default",
		Description:    "General-purpose worker agent",
		SystemPrompt:   StrippedAgentPrompt,
		Tools:          []string{"*"},
		Model:          "inherit",
		Engine:         "inherit",
		MaxTurns:       0,
		PermissionMode: "inherit",
	}
}

// BuiltinAgentTypes returns the set of named agent configurations shipped with Providence.
func BuiltinAgentTypes() map[string]AgentType {
	return map[string]AgentType{
		"code": {
			Name:           "code",
			Description:    "Code-focused worker with full tool access",
			SystemPrompt:   AntiRecursionPrompt + "\n\n" + StrippedAgentPrompt,
			Tools:          []string{"*"},
			Model:          "inherit",
			Engine:         "inherit",
			MaxTurns:       0,
			PermissionMode: "inherit",
		},
		"research": {
			Name:           "research",
			Description:    "Read-only research agent, no file writes",
			SystemPrompt:   AntiRecursionPrompt + "\n\nYou are a research agent. You may read files and search, but do NOT write or modify anything.",
			Tools:          []string{"Read", "Glob", "Grep", "WebFetch", "WebSearch", "Bash"},
			Model:          "inherit",
			Engine:         "inherit",
			MaxTurns:       0,
			PermissionMode: "inherit",
		},
		"review": {
			Name:           "review",
			Description:    "Code review agent, read-only",
			SystemPrompt:   AntiRecursionPrompt + "\n\nYou are a code review agent. Analyze code for bugs, style issues, and improvements. Do NOT modify files.",
			Tools:          []string{"Read", "Glob", "Grep", "Bash"},
			Model:          "inherit",
			Engine:         "inherit",
			MaxTurns:       0,
			PermissionMode: "inherit",
		},
	}
}
