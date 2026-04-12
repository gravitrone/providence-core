// Package permissions implements the 7-step permission decision chain
// for tool execution approval. It mirrors the CC permission model with
// ordered rule evaluation, mode-based defaults, and bypass-immune safety checks.
package permissions

// PermissionMode controls the default tool approval behavior.
type PermissionMode string

const (
	// ModeDefault requires approval for all tool invocations.
	ModeDefault PermissionMode = "default"
	// ModeAcceptEdits auto-approves file edit tools and safe shell commands.
	ModeAcceptEdits PermissionMode = "acceptEdits"
	// ModeBypassPermissions skips all prompts except safety checks.
	ModeBypassPermissions PermissionMode = "bypassPermissions"
	// ModePlan allows read-only tools, denies all writes.
	ModePlan PermissionMode = "plan"
	// ModeDontAsk silently denies when uncertain instead of prompting.
	ModeDontAsk PermissionMode = "dontAsk"
)

// Decision is the result of a permission check.
type Decision string

const (
	// Allow permits the tool to execute.
	Allow Decision = "allow"
	// Deny blocks the tool from executing.
	Deny Decision = "deny"
	// Ask requires user confirmation before executing.
	Ask Decision = "ask"
)

// Rule defines a permission rule (allow/deny/ask) for a tool pattern.
type Rule struct {
	// Pattern matches tool invocations. Examples:
	//   "Bash"              - exact tool name
	//   "Bash(git *)"       - tool with argument glob
	//   "Read(/home/*)"     - tool with path glob
	//   "mcp__github__*"    - wildcard tool name
	Pattern  string
	Behavior Decision
	Source   string // "userSettings", "projectSettings", "localSettings", "flagSettings", "policySettings", "session"
}

// Result is the output of CheckPermission.
type Result struct {
	Decision      Decision
	Reason        string
	ToolName      string
	UpdatedInput  interface{}
	IsSafetyCheck bool // .git/, .claude/, shell configs - bypass-immune
}

// ToolChecker is per-tool permission logic.
type ToolChecker interface {
	CheckPermissions(toolName string, input interface{}) (*Result, error)
	RequiresUserInteraction() bool
}

// Context holds the permission evaluation context.
type Context struct {
	Mode             PermissionMode
	AlwaysAllowRules []Rule
	AlwaysDenyRules  []Rule
	AlwaysAskRules   []Rule
	ToolCheckers     map[string]ToolChecker
}

// AcceptEditsAllowedCommands are auto-approved in acceptEdits mode.
var AcceptEditsAllowedCommands = []string{"mkdir", "touch", "rm", "rmdir", "mv", "cp", "sed"}
