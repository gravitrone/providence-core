package subagent

import "fmt"

// AgentType defines a reusable agent configuration that can be instantiated
// as a subagent. Built-in types, custom user types, and background agents
// all share this shape.
type AgentType struct {
	Name            string   `yaml:"name"`
	Description     string   `yaml:"description"`
	Tools           []string `yaml:"tools"`
	DisallowedTools []string `yaml:"disallowedTools"`
	Model           string   `yaml:"model"`
	Engine          string   `yaml:"engine"`
	Effort          string   `yaml:"effort"`
	MaxTurns        int      `yaml:"maxTurns"`
	PermissionMode  string   `yaml:"permissionMode"`
	Background      bool     `yaml:"background"`
	Isolation       string   `yaml:"isolation"`
	SystemPrompt    string   `yaml:"-"`
	WorkDir         string   `yaml:"-"` // Override working directory (set by worktree isolation)
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
	Tools         string `json:"tools,omitempty"`
	MergeStrategy string `json:"merge_strategy,omitempty"`
	Isolation     string `json:"isolation,omitempty"` // "worktree", "docker", or empty (none)
}

// TaskResult is returned when an agent completes.
type TaskResult struct {
	AgentID        string `json:"agent_id"`
	Status         string `json:"status"`
	Result         string `json:"result"`
	TotalTokens    int    `json:"total_tokens"`
	ToolUses       int    `json:"tool_uses"`
	DurationMS     int64  `json:"duration_ms"`
	WorktreePath   string `json:"worktree_path,omitempty"`
	WorktreeBranch string `json:"worktree_branch,omitempty"`
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
