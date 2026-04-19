package direct

import (
	"path/filepath"
	"testing"

	"github.com/gravitrone/providence-core/internal/engine/direct/tools"
	"github.com/gravitrone/providence-core/internal/engine/permissions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPermissionHandlerDenialHistoryIsRecorded(t *testing.T) {
	ph := NewPermissionHandlerWithConfig(
		nil, nil,
		nil,
		[]permissions.Rule{
			{Pattern: "Bash(git push *)", Behavior: permissions.Deny, Source: "userSettings"},
		},
		nil,
	)

	// Trigger a couple of denials.
	ph.Check(&tools.BashTool{}, map[string]any{"command": "git push --force"})
	ph.Check(&tools.BashTool{}, map[string]any{"command": "git push --force"})

	history := ph.DenialHistory()
	require.Len(t, history, 1)
	assert.Equal(t, 2, history[0].Count)
	assert.Equal(t, "Bash", history[0].Tool)
}

func TestPermissionHandlerAttachesExplanationOnDeny(t *testing.T) {
	ph := NewPermissionHandlerWithConfig(
		nil, nil,
		nil,
		[]permissions.Rule{
			{Pattern: "Bash(rm *)", Behavior: permissions.Deny, Source: "userSettings"},
		},
		nil,
	)

	result := ph.Check(&tools.BashTool{}, map[string]any{"command": "rm -rf home"})
	require.NotNil(t, result)
	assert.Equal(t, permissions.Deny, result.Decision)
	assert.Contains(t, result.Reason, "Bash(rm *)")
	assert.Contains(t, result.Reason, "rm -rf home")
}

func TestPermissionHandlerAutoModeApprovesReadOnly(t *testing.T) {
	ph := NewPermissionHandler()
	ph.SetAutoMode(true)
	assert.True(t, ph.AutoModeEnabled())

	// Read-only tool should be auto-approved without Ask.
	assert.False(t, ph.NeedsPermission(&permMockTool{name: "Read", readOnly: true}, map[string]any{"file_path": "/tmp/x"}))

	// Write stays behind the normal prompt.
	assert.True(t, ph.NeedsPermission(&permMockTool{name: "Write", readOnly: false}, map[string]any{"file_path": "/tmp/x"}))

	// Turning auto-mode off restores the default Ask for unknown tools.
	ph.SetAutoMode(false)
	assert.True(t, ph.NeedsPermission(&permMockTool{name: "Bash", readOnly: false}, map[string]any{"command": "ls"}))
}

func TestPermissionHandlerShadowedRulesSurfaced(t *testing.T) {
	ph := NewPermissionHandlerWithConfig(
		nil, nil,
		[]permissions.Rule{
			{Pattern: "Bash(git *)", Behavior: permissions.Allow, Source: "userSettings"},
			{Pattern: "Bash(git push)", Behavior: permissions.Allow, Source: "projectSettings"},
		},
		nil, nil,
	)

	shadowed := ph.ShadowedRules()
	require.NotEmpty(t, shadowed)
	assert.Equal(t, "Bash(git push)", shadowed[0].Shadowed.Pattern)
}

func TestPermissionHandlerPersistAndReload(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PROVIDENCE_PERMISSIONS_DIR", dir)

	project := filepath.Join(dir, "project-a")

	// Save via a fully configured handler.
	saver := NewPermissionHandlerWithConfig(
		[]permissions.Rule{{Pattern: "Bash(ls *)", Behavior: permissions.Allow, Source: "session"}},
		nil, nil, nil, nil,
	)
	require.NoError(t, saver.PersistRules(project))

	// Fresh handler reloads.
	loader := NewPermissionHandler()
	require.NoError(t, loader.LoadPersistedRules(project))

	// The loaded rule should auto-approve the matching command.
	assert.False(t, loader.NeedsPermission(&tools.BashTool{}, map[string]any{"command": "ls -la"}))
}
