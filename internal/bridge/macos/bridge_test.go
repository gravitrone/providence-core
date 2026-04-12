package macos

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBridgeIsAvailable(t *testing.T) {
	b := New()
	if runtime.GOOS == "darwin" {
		assert.True(t, b.IsAvailable())
	} else {
		assert.False(t, b.IsAvailable())
	}
}

func TestBuildKeystrokeScript_SimpleKey(t *testing.T) {
	script, err := buildKeystrokeScript("return")
	require.NoError(t, err)
	assert.Contains(t, script, "key code 36")
}

func TestBuildKeystrokeScript_ModifierCombo(t *testing.T) {
	tests := []struct {
		input    string
		contains []string
	}{
		{
			input:    "command+v",
			contains: []string{`keystroke "v"`, "command down"},
		},
		{
			input:    "ctrl+shift+a",
			contains: []string{`keystroke "a"`, "control down", "shift down"},
		},
		{
			input:    "alt+tab",
			contains: []string{"key code 48", "option down"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			script, err := buildKeystrokeScript(tc.input)
			require.NoError(t, err)
			for _, c := range tc.contains {
				assert.Contains(t, script, c)
			}
		})
	}
}

func TestBuildKeystrokeScript_EmptyCombo(t *testing.T) {
	_, err := buildKeystrokeScript("")
	assert.Error(t, err)
}

func TestBuildKeystrokeScript_ModifiersOnly(t *testing.T) {
	_, err := buildKeystrokeScript("command+shift")
	assert.Error(t, err)
}

func TestClipboardRoundtrip(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("clipboard test only runs on macOS")
	}

	b := New()
	ctx := t.Context()

	testText := "providence-bridge-test-" + t.Name()
	err := b.ClipboardWrite(ctx, testText)
	require.NoError(t, err)

	got, err := b.ClipboardRead(ctx)
	require.NoError(t, err)
	assert.Equal(t, testText, got)
}

func TestListApps(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("list apps test only runs on macOS")
	}

	b := New()
	ctx := t.Context()

	apps, err := b.ListApps(ctx)
	if err != nil {
		// System Events may require accessibility permissions in CI or sandboxed envs
		t.Skipf("list apps not available (likely missing accessibility permissions): %v", err)
	}
	assert.NotEmpty(t, apps, "should have at least one running app")

	// Finder is always running on macOS
	found := false
	for _, app := range apps {
		if app.Name == "Finder" {
			found = true
			break
		}
	}
	assert.True(t, found, "Finder should be in running apps")
}
