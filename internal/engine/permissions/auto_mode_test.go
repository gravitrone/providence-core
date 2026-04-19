package permissions

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAutoModeDisabledByDefault(t *testing.T) {
	am := NewAutoMode()
	assert.False(t, am.Enabled())
	assert.False(t, am.IsAutoApproved("Read", fileInput("/tmp/x")))
}

func TestAutoModeOnlyApprovesReadOnlyTools(t *testing.T) {
	am := NewAutoMode()
	am.SetAutoMode(true)

	tests := []struct {
		name  string
		tool  string
		input interface{}
		want  bool
	}{
		{"Read approved", "Read", fileInput("/tmp/x"), true},
		{"Glob approved", "Glob", nil, true},
		{"Grep approved", "Grep", nil, true},
		{"Write not approved", "Write", fileInput("/tmp/x"), false},
		{"Edit not approved", "Edit", fileInput("/tmp/x"), false},
		{"Bash not approved", "Bash", bashInput("ls"), false},
		{"mcp tool not approved", "mcp__github__pr", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, am.IsAutoApproved(tt.tool, tt.input))
		})
	}
}

func TestAutoModeSafetyPathsBlockApproval(t *testing.T) {
	am := NewAutoMode()
	am.SetAutoMode(true)

	// Read is allowlisted, but safety paths still require prompting.
	assert.False(t, am.IsAutoApproved("Read", fileInput("/home/user/.zshrc")))
	assert.False(t, am.IsAutoApproved("Read", fileInput("/project/.git/config")))
}

func TestAutoModeToggle(t *testing.T) {
	am := NewAutoMode()
	am.SetAutoMode(true)
	assert.True(t, am.Enabled())
	am.SetAutoMode(false)
	assert.False(t, am.Enabled())
	assert.False(t, am.IsAutoApproved("Read", fileInput("/tmp/x")))
}
