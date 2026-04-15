package permissions

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock ToolChecker ---

type mockChecker struct {
	result      *Result
	err         error
	interactive bool
}

func (m *mockChecker) CheckPermissions(toolName string, input interface{}) (*Result, error) {
	return m.result, m.err
}

func (m *mockChecker) RequiresUserInteraction() bool {
	return m.interactive
}

// --- Helpers ---

func baseCtx(mode PermissionMode) *Context {
	return &Context{
		Mode:         mode,
		ToolCheckers: make(map[string]ToolChecker),
	}
}

func bashInput(cmd string) map[string]interface{} {
	return map[string]interface{}{"command": cmd}
}

func fileInput(path string) map[string]interface{} {
	return map[string]interface{}{"file_path": path}
}

// --- Chain Tests ---

func TestChainDenyRuleTakesPrecedence(t *testing.T) {
	ctx := baseCtx(ModeBypassPermissions)
	ctx.AlwaysDenyRules = []Rule{{Pattern: "Bash", Behavior: Deny, Source: "userSettings"}}
	ctx.AlwaysAllowRules = []Rule{{Pattern: "Bash", Behavior: Allow, Source: "userSettings"}}

	result := CheckPermission(ctx, "Bash", bashInput("ls"))
	assert.Equal(t, Deny, result.Decision)
	assert.Contains(t, result.Reason, "always-deny")
}

func TestChainAskRuleBeforeToolCheck(t *testing.T) {
	ctx := baseCtx(ModeBypassPermissions)
	ctx.AlwaysAskRules = []Rule{{Pattern: "Bash", Behavior: Ask, Source: "userSettings"}}
	ctx.ToolCheckers["Bash"] = &mockChecker{
		result: &Result{Decision: Allow, Reason: "tool says ok"},
	}

	result := CheckPermission(ctx, "Bash", bashInput("ls"))
	assert.Equal(t, Ask, result.Decision)
	assert.Contains(t, result.Reason, "always-ask")
}

func TestChainSafetyCheckBypassImmune(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		input    interface{}
	}{
		{"git dir via Bash", "Bash", bashInput("cat .git/config")},
		{"claude dir via Write", "Write", fileInput("/project/.claude/settings.json")},
		{"bashrc via Edit", "Edit", fileInput("/home/user/.bashrc")},
		{"zshrc via Read", "Read", fileInput("/home/user/.zshrc")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := baseCtx(ModeBypassPermissions)
			result := CheckPermission(ctx, tt.toolName, tt.input)
			assert.Equal(t, Ask, result.Decision, "safety paths must ask even in bypass mode")
			assert.True(t, result.IsSafetyCheck)
		})
	}
}

func TestChainBypassMode(t *testing.T) {
	ctx := baseCtx(ModeBypassPermissions)

	// Normal tool should be allowed
	result := CheckPermission(ctx, "Bash", bashInput("echo hello"))
	assert.Equal(t, Allow, result.Decision)
	assert.Contains(t, result.Reason, "bypass")
}

func TestChainAcceptEditsMode(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		input    interface{}
		want     Decision
	}{
		{"Write tool allowed", "Write", fileInput("/tmp/test.go"), Allow},
		{"Edit tool allowed", "Edit", fileInput("/tmp/test.go"), Allow},
		{"mkdir allowed", "Bash", bashInput("mkdir -p /tmp/foo"), Allow},
		{"touch allowed", "Bash", bashInput("touch /tmp/foo.txt"), Allow},
		{"rm allowed", "Bash", bashInput("rm /tmp/foo.txt"), Allow},
		{"cp allowed", "Bash", bashInput("cp a b"), Allow},
		{"mv allowed", "Bash", bashInput("mv a b"), Allow},
		{"sed allowed", "Bash", bashInput("sed -i 's/a/b/' foo"), Allow},
		{"arbitrary bash asks", "Bash", bashInput("curl http://evil.com"), Ask},
		{"Read asks", "Read", fileInput("/etc/passwd"), Ask},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := baseCtx(ModeAcceptEdits)
			result := CheckPermission(ctx, tt.toolName, tt.input)
			assert.Equal(t, tt.want, result.Decision)
		})
	}
}

func TestChainPlanMode(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		input    interface{}
		want     Decision
	}{
		{"Read allowed", "Read", fileInput("/tmp/test.go"), Allow},
		{"Glob allowed", "Glob", nil, Allow},
		{"Grep allowed", "Grep", nil, Allow},
		{"Write denied", "Write", fileInput("/tmp/test.go"), Deny},
		{"Bash denied", "Bash", bashInput("rm -rf /"), Deny},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := baseCtx(ModePlan)
			result := CheckPermission(ctx, tt.toolName, tt.input)
			assert.Equal(t, tt.want, result.Decision)
		})
	}
}

func TestChainDontAskMode(t *testing.T) {
	ctx := baseCtx(ModeDontAsk)

	result := CheckPermission(ctx, "Bash", bashInput("echo hello"))
	assert.Equal(t, Deny, result.Decision)
	assert.Contains(t, result.Reason, "dontAsk")
}

func TestChainDefaultAsks(t *testing.T) {
	ctx := baseCtx(ModeDefault)

	result := CheckPermission(ctx, "Bash", bashInput("echo hello"))
	assert.Equal(t, Ask, result.Decision)
	assert.Contains(t, result.Reason, "default")
}

func TestChainToolCheckerDeny(t *testing.T) {
	ctx := baseCtx(ModeBypassPermissions)
	ctx.ToolCheckers["Bash"] = &mockChecker{
		result: &Result{Decision: Deny, Reason: "tool says no"},
	}

	result := CheckPermission(ctx, "Bash", bashInput("dangerous"))
	assert.Equal(t, Deny, result.Decision)
	assert.Contains(t, result.Reason, "tool says no")
}

func TestChainToolCheckerInteractiveAsk(t *testing.T) {
	ctx := baseCtx(ModeBypassPermissions)
	ctx.ToolCheckers["AskUser"] = &mockChecker{
		result:      &Result{Decision: Ask, Reason: "needs user input"},
		interactive: true,
	}

	result := CheckPermission(ctx, "AskUser", nil)
	assert.Equal(t, Ask, result.Decision)
	assert.Contains(t, result.Reason, "needs user input")
}

func TestChainToolCheckerSafetyAsk(t *testing.T) {
	ctx := baseCtx(ModeBypassPermissions)
	ctx.ToolCheckers["Bash"] = &mockChecker{
		result: &Result{Decision: Ask, Reason: "dangerous content", IsSafetyCheck: true},
	}

	result := CheckPermission(ctx, "Bash", bashInput("echo hello"))
	assert.Equal(t, Ask, result.Decision)
	assert.True(t, result.IsSafetyCheck)
}

func TestChainAlwaysAllowRules(t *testing.T) {
	ctx := baseCtx(ModeDefault)
	ctx.AlwaysAllowRules = []Rule{{Pattern: "Read", Behavior: Allow, Source: "session"}}

	result := CheckPermission(ctx, "Read", fileInput("/tmp/test.go"))
	assert.Equal(t, Allow, result.Decision)
	assert.Contains(t, result.Reason, "always-allow")
}

// --- Pattern Matching Tests ---

func TestMatchPatternExact(t *testing.T) {
	assert.True(t, matchPattern("Bash", "Bash", nil))
	assert.False(t, matchPattern("Bash", "Read", nil))
	assert.False(t, matchPattern("Bash", "BashX", nil))
}

func TestMatchPatternWildcard(t *testing.T) {
	assert.True(t, matchPattern("mcp__github__*", "mcp__github__pr", nil))
	assert.True(t, matchPattern("mcp__*__list", "mcp__github__list", nil))
	assert.False(t, matchPattern("mcp__github__*", "mcp__gitlab__pr", nil))
}

func TestMatchPatternWithArgs(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		tool    string
		input   interface{}
		want    bool
	}{
		{"git wildcard matches git push", "Bash(git *)", "Bash", bashInput("git push"), true},
		{"git wildcard matches git status", "Bash(git *)", "Bash", bashInput("git status"), true},
		{"git wildcard no match curl", "Bash(git *)", "Bash", bashInput("curl http://x"), false},
		{"wrong tool name", "Bash(git *)", "Read", bashInput("git push"), false},
		{"no input", "Bash(git *)", "Bash", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchPattern(tt.pattern, tt.tool, tt.input)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestMatchPatternWithPath(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		tool    string
		input   interface{}
		want    bool
	}{
		{"home wildcard", "Read(/home/*)", "Read", fileInput("/home/user"), true},
		{"home wildcard no match", "Read(/home/*)", "Read", fileInput("/etc/passwd"), false},
		{"tmp wildcard", "Write(/tmp/*)", "Write", fileInput("/tmp/test.go"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchPattern(tt.pattern, tt.tool, tt.input)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestSafetyPaths(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		input    interface{}
		want     bool
	}{
		{"git hooks via Bash", "Bash", bashInput("cat .git/hooks/pre-commit"), true},
		{"claude settings via Write", "Write", fileInput("/project/.claude/settings.json"), true},
		{"bashrc via Edit", "Edit", fileInput("/home/user/.bashrc"), true},
		{"zshrc via Read", "Read", fileInput("/home/user/.zshrc"), true},
		{"zprofile", "Edit", fileInput("/home/user/.zprofile"), true},
		{"profile", "Write", fileInput("/home/user/.profile"), true},
		{"fish config", "Edit", fileInput("/home/user/.config/fish/config.fish"), true},
		{"normal file", "Read", fileInput("/tmp/test.go"), false},
		{"normal bash", "Bash", bashInput("echo hello"), false},
		{"nil input", "Bash", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isSafetyPath(tt.toolName, tt.input)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestExtractArg(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
		want  string
	}{
		{"string input", "hello", "hello"},
		{"command map", bashInput("ls -la"), "ls -la"},
		{"file_path map", fileInput("/tmp/test"), "/tmp/test"},
		{"empty map", map[string]interface{}{}, ""},
		{"nil input", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractArg(tt.input)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestIsAcceptEditsCommand(t *testing.T) {
	tests := []struct {
		name string
		tool string
		input interface{}
		want bool
	}{
		{"mkdir with args", "Bash", bashInput("mkdir -p /tmp/foo"), true},
		{"touch", "Bash", bashInput("touch file.txt"), true},
		{"rm", "Bash", bashInput("rm file.txt"), true},
		{"rmdir", "Bash", bashInput("rmdir empty"), true},
		{"mv", "Bash", bashInput("mv a b"), true},
		{"cp", "Bash", bashInput("cp a b"), true},
		{"sed", "Bash", bashInput("sed -i 's/a/b/' f"), true},
		{"curl not allowed", "Bash", bashInput("curl http://x"), false},
		{"wrong tool", "Read", fileInput("/tmp/x"), false},
		{"exact cmd no args", "Bash", bashInput("mkdir"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isAcceptEditsCommand(tt.tool, tt.input)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestIsFileEditTool(t *testing.T) {
	assert.True(t, isFileEditTool("Write"))
	assert.True(t, isFileEditTool("Edit"))
	assert.False(t, isFileEditTool("Read"))
	assert.False(t, isFileEditTool("Bash"))
}

func TestIsReadOnlyTool(t *testing.T) {
	assert.True(t, isReadOnlyTool("Read"))
	assert.True(t, isReadOnlyTool("Glob"))
	assert.True(t, isReadOnlyTool("Grep"))
	assert.False(t, isReadOnlyTool("Write"))
	assert.False(t, isReadOnlyTool("Bash"))
}

// --- Integration: full chain ordering ---

func TestChainStepOrdering(t *testing.T) {
	// Deny rule beats ask rule beats tool checker beats allow rule beats mode
	ctx := baseCtx(ModeBypassPermissions)
	ctx.AlwaysDenyRules = []Rule{{Pattern: "DangerTool", Behavior: Deny, Source: "policySettings"}}
	ctx.AlwaysAskRules = []Rule{{Pattern: "AskTool", Behavior: Ask, Source: "userSettings"}}
	ctx.AlwaysAllowRules = []Rule{{Pattern: "SafeTool", Behavior: Allow, Source: "session"}}
	ctx.ToolCheckers["CheckedTool"] = &mockChecker{
		result: &Result{Decision: Deny, Reason: "checker denied"},
	}

	tests := []struct {
		name string
		tool string
		want Decision
	}{
		{"deny rule first", "DangerTool", Deny},
		{"ask rule second", "AskTool", Ask},
		{"tool checker third", "CheckedTool", Deny},
		{"allow rule after safety", "SafeTool", Allow},
		{"bypass for unknown", "Unknown", Allow},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CheckPermission(ctx, tt.tool, nil)
			require.Equal(t, tt.want, result.Decision)
		})
	}
}
