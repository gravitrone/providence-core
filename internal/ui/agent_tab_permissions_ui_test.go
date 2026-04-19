package ui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gravitrone/providence-core/internal/config"
)

// TestSlashPermissionsShowsDepthSections verifies the three new sections
// (shadowed rules, recent denials, auto-mode) get rendered only when they
// have content, and that the auto-mode line is always present.
func TestSlashPermissionsShowsDepthSections(t *testing.T) {
	t.Run("empty state renders only auto-mode", func(t *testing.T) {
		at := NewAgentTab("", config.Config{}, nil, nil)
		var out strings.Builder
		at.renderPermissionDepthSections(&out)
		got := out.String()
		assert.NotContains(t, got, "Shadowed rules:", "no config rules means no shadow section")
		assert.NotContains(t, got, "Recent denials", "no denials means no denial section")
		assert.Contains(t, got, "Auto-mode: disabled", "auto-mode always renders its state line")
	})

	t.Run("shadow section shows when rules shadow each other", func(t *testing.T) {
		cfg := config.Config{
			Permissions: config.PermissionsConfig{
				Allow: []string{"Bash", "Bash(git push)"},
			},
		}
		at := NewAgentTab("", cfg, nil, nil)
		var out strings.Builder
		at.renderPermissionDepthSections(&out)
		got := out.String()
		assert.Contains(t, got, "Shadowed rules:", "shadow section should be present")
		assert.Contains(t, got, "Bash(git push)", "shadow section lists the unreachable pattern")
	})

	t.Run("denials section shows after recordUIDenial", func(t *testing.T) {
		at := NewAgentTab("", config.Config{}, nil, nil)
		at.recordUIDenial("Bash", map[string]any{"command": "rm -rf /"})
		at.recordUIDenial("Bash", map[string]any{"command": "rm -rf /"}) // same key, count bumps
		at.recordUIDenial("Write", map[string]any{"file_path": "/etc/passwd"})

		var out strings.Builder
		at.renderPermissionDepthSections(&out)
		got := out.String()
		assert.Contains(t, got, "Recent denials")
		assert.Contains(t, got, "Bash")
		assert.Contains(t, got, "Write")
		assert.Contains(t, got, "x2", "repeat denial of the same key should bump the count")
	})

	t.Run("auto-mode toggle flips the label", func(t *testing.T) {
		at := NewAgentTab("", config.Config{}, nil, nil)
		at.autoModeEnabled = true
		var out strings.Builder
		at.renderPermissionDepthSections(&out)
		assert.Contains(t, out.String(), "Auto-mode: enabled")
	})
}

// TestSlashPermissionsAutomodeSubcommand exercises the automode subcommand so
// the UI-side toggle mirrors the engine-side auto-mode accessor.
func TestSlashPermissionsAutomodeSubcommand(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil, nil)
	require.False(t, at.autoModeEnabled, "fresh tab starts with auto-mode off")

	handled, _ := at.handleSlashCommand("/permissions automode on")
	require.True(t, handled)
	assert.True(t, at.autoModeEnabled, "automode on must flip the flag")

	handled, _ = at.handleSlashCommand("/permissions automode off")
	require.True(t, handled)
	assert.False(t, at.autoModeEnabled, "automode off must clear the flag")

	handled, _ = at.handleSlashCommand("/permissions automode")
	require.True(t, handled)
	assert.True(t, at.autoModeEnabled, "automode with no arg must toggle from off to on")
}
