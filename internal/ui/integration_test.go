package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- TestThemeSwitchReactivity ---

func TestThemeSwitchReactivity(t *testing.T) {
	// Apply flame theme and verify primary color.
	ApplyTheme("flame")
	assert.Equal(t, FlameTheme, ActiveTheme)
	assert.Equal(t, c("#FFA600"), ColorPrimary, "flame primary should be amber")

	// Capture flame TabActiveStyle background.
	flameBg := TabActiveStyle.GetBackground()

	// Switch to night theme.
	ApplyTheme("night")
	assert.Equal(t, NightTheme, ActiveTheme)
	assert.Equal(t, c("#00DCFF"), ColorPrimary, "night primary should be cyan")

	// TabActiveStyle background should have changed.
	nightBg := TabActiveStyle.GetBackground()
	assert.NotEqual(t, flameBg, nightBg, "TabActiveStyle background should change on theme switch")

	// Switch back to flame to verify full round-trip.
	ApplyTheme("flame")
	assert.Equal(t, c("#FFA600"), ColorPrimary, "should be back to flame amber")
	restoredBg := TabActiveStyle.GetBackground()
	assert.Equal(t, flameBg, restoredBg, "TabActiveStyle background should restore to flame")
}

// --- TestSlashCommandRegistry ---

func TestSlashCommandRegistry(t *testing.T) {
	require.GreaterOrEqual(t, len(slashCommands), 25, "should have at least 25 slash commands")

	seen := make(map[string]bool, len(slashCommands))
	for _, cmd := range slashCommands {
		t.Run(cmd.Name, func(t *testing.T) {
			assert.NotEmpty(t, cmd.Name, "command name should not be empty")
			assert.NotEmpty(t, cmd.Desc, "command %q should have a description", cmd.Name)
			assert.True(t, cmd.Name[0] == '/', "command %q should start with /", cmd.Name)
		})

		// Check for duplicates.
		assert.False(t, seen[cmd.Name], "duplicate slash command: %q", cmd.Name)
		seen[cmd.Name] = true
	}
}
