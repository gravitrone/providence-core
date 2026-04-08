package main

import (
	"bytes"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRootCommandExists(t *testing.T) {
	root := newRootCommand()
	require.NotNil(t, root)
	assert.Equal(t, "providence", root.Use)
}

func TestRootCommandShortDescription(t *testing.T) {
	root := newRootCommand()
	assert.NotEmpty(t, root.Short)
	assert.Contains(t, root.Long, "providence")
}

func TestRootCommandHasCompletionSubcommand(t *testing.T) {
	root := newRootCommand()
	require.Len(t, root.Commands(), 1, "root should have exactly one subcommand")
	assert.Equal(t, "completion [bash|zsh|fish]", root.Commands()[0].Use)
}

func TestRootCommandRunsWithoutError(t *testing.T) {
	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	// Override runBubbleTUI so it doesn't launch a real program.
	original := runBubbleTUI
	defer func() { runBubbleTUI = original }()
	runBubbleTUI = func(_ tea.Model) error { return nil }

	// When stdout is not a TTY (in tests), runTUI prints the banner and returns.
	err := root.Execute()
	assert.NoError(t, err)
}
