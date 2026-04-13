package permissions

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChainDenyRuleBlocks(t *testing.T) {
	ctx := baseCtx(ModeDefault)
	ctx.AlwaysDenyRules = []Rule{{Pattern: "Bash", Behavior: Deny, Source: "policySettings"}}

	result := CheckPermission(ctx, "Bash", bashInput("echo hi"))
	assert.Equal(t, Deny, result.Decision)
	assert.Contains(t, result.Reason, "always-deny")
	assert.Equal(t, "Bash", result.ToolName)
}

func TestChainAskRuleAsk(t *testing.T) {
	ctx := baseCtx(ModeBypassPermissions)
	ctx.AlwaysAskRules = []Rule{{Pattern: "Write", Behavior: Ask, Source: "userSettings"}}

	result := CheckPermission(ctx, "Write", fileInput("/tmp/foo.go"))
	assert.Equal(t, Ask, result.Decision)
	assert.Contains(t, result.Reason, "always-ask")
}

func TestChainSafetyPathAlwaysAsks(t *testing.T) {
	// Even in bypass mode, .git/hooks must trigger ASK
	ctx := baseCtx(ModeBypassPermissions)

	result := CheckPermission(ctx, "Bash", bashInput("cat .git/hooks/pre-commit"))
	assert.Equal(t, Ask, result.Decision)
	assert.True(t, result.IsSafetyCheck)
}

func TestChainBypassAllows(t *testing.T) {
	ctx := baseCtx(ModeBypassPermissions)

	// Non-safety path should be ALLOW in bypass
	result := CheckPermission(ctx, "Bash", bashInput("echo hello"))
	assert.Equal(t, Allow, result.Decision)
	assert.Contains(t, result.Reason, "bypass")
}

func TestChainAcceptEditsAllowsFSCommands(t *testing.T) {
	ctx := baseCtx(ModeAcceptEdits)

	cmds := []string{"mkdir -p /tmp/dir", "touch /tmp/f", "rm /tmp/f", "rmdir /tmp/dir", "mv a b", "cp a b", "sed -i '' f"}
	for _, cmd := range cmds {
		result := CheckPermission(ctx, "Bash", bashInput(cmd))
		assert.Equal(t, Allow, result.Decision, "command %q should be allowed in acceptEdits", cmd)
	}
}

func TestChainPlanDeniesWrites(t *testing.T) {
	ctx := baseCtx(ModePlan)

	for _, tool := range []string{"Write", "Edit"} {
		result := CheckPermission(ctx, tool, fileInput("/tmp/test.go"))
		assert.Equal(t, Deny, result.Decision, "%s should be denied in plan mode", tool)
		assert.Contains(t, result.Reason, "plan mode")
	}

	// Bash also denied
	result := CheckPermission(ctx, "Bash", bashInput("rm -rf /"))
	assert.Equal(t, Deny, result.Decision)
}

func TestChainDefaultAsksDeep(t *testing.T) {
	// No rules, default mode -> ASK for any tool
	ctx := baseCtx(ModeDefault)

	result := CheckPermission(ctx, "Bash", bashInput("ls"))
	assert.Equal(t, Ask, result.Decision)
	assert.Contains(t, result.Reason, "default")

	result = CheckPermission(ctx, "Write", fileInput("/tmp/x"))
	assert.Equal(t, Ask, result.Decision)
}

func TestChainDontAskDenies(t *testing.T) {
	ctx := baseCtx(ModeDontAsk)

	result := CheckPermission(ctx, "Bash", bashInput("echo test"))
	assert.Equal(t, Deny, result.Decision)
	assert.Contains(t, result.Reason, "dontAsk")
}
