//go:build darwin

package macos

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseKeyCombo(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantKey       string
		wantModifiers []string
		wantCode      int
		wantErr       bool
	}{
		{
			name:          "cmd copy",
			input:         "cmd+c",
			wantKey:       "c",
			wantModifiers: []string{"cmd"},
			wantCode:      8,
		},
		{
			name:          "cmd shift redo",
			input:         "cmd+shift+z",
			wantKey:       "z",
			wantModifiers: []string{"cmd", "shift"},
			wantCode:      6,
		},
		{
			name:          "ctrl alt t",
			input:         "ctrl+alt+t",
			wantKey:       "t",
			wantModifiers: []string{"control", "option"},
			wantCode:      17,
		},
		{
			name:          "command alias",
			input:         "command+v",
			wantKey:       "v",
			wantModifiers: []string{"cmd"},
			wantCode:      9,
		},
		{
			name:     "return",
			input:    "return",
			wantKey:  "return",
			wantCode: 36,
		},
		{
			name:     "function key",
			input:    "F5",
			wantKey:  "f5",
			wantCode: 96,
		},
		{
			name:          "shift tab",
			input:         "shift+tab",
			wantKey:       "tab",
			wantModifiers: []string{"shift"},
			wantCode:      48,
		},
		{
			name:          "trim and lowercase",
			input:         "  ctrl + shift + A  ",
			wantKey:       "a",
			wantModifiers: []string{"control", "shift"},
			wantCode:      0,
		},
		{
			name:          "option left",
			input:         "option+left",
			wantKey:       "left",
			wantModifiers: []string{"option"},
			wantCode:      123,
		},
		{
			name:     "delete",
			input:    "delete",
			wantKey:  "delete",
			wantCode: 51,
		},
		{
			name:          "unknown key",
			input:         "cmd+unknown",
			wantKey:       "unknown",
			wantModifiers: []string{"cmd"},
			wantCode:      -1,
		},
		{
			name:          "space",
			input:         "alt+space",
			wantKey:       "space",
			wantModifiers: []string{"option"},
			wantCode:      49,
		},
		{
			name:          "canonical modifier order",
			input:         "control+command+shift+option+1",
			wantKey:       "1",
			wantModifiers: []string{"cmd", "control", "option", "shift"},
			wantCode:      18,
		},
		{
			name:     "enter alias",
			input:    "enter",
			wantKey:  "enter",
			wantCode: 36,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "modifier only",
			input:   "command+shift",
			wantErr: true,
		},
		{
			name:    "trailing plus",
			input:   "cmd+",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			combo, err := ParseKeyCombo(tc.input)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.wantKey, combo.Key)
			assert.Equal(t, tc.wantModifiers, combo.Modifiers)
			assert.Equal(t, tc.wantCode, combo.VirtualCode)
		})
	}
}

func TestBuildKeystrokeScriptMatchesOldBehavior(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "return",
			expected: `tell application "System Events" to key code 36`,
		},
		{
			input:    "cmd+c",
			expected: `tell application "System Events" to keystroke "c" using {command down}`,
		},
		{
			input:    "cmd+shift+z",
			expected: `tell application "System Events" to keystroke "z" using {command down, shift down}`,
		},
		{
			input:    "ctrl+alt+t",
			expected: `tell application "System Events" to keystroke "t" using {control down, option down}`,
		},
		{
			input:    "shift+tab",
			expected: `tell application "System Events" to key code 48 using {shift down}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			script, err := buildKeystrokeScript(tc.input)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, script)
		})
	}
}
