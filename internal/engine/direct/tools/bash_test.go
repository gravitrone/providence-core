package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBash_Echo(t *testing.T) {
	b := NewBashTool()
	res := b.Execute(context.Background(), map[string]any{
		"command": "echo hello",
	})

	assert.False(t, res.IsError, res.Content)
	assert.Contains(t, res.Content, "hello")
	assert.Contains(t, res.Content, "Exit code: 0")
}

func TestBash_ExitCode(t *testing.T) {
	b := NewBashTool()
	res := b.Execute(context.Background(), map[string]any{
		"command": "exit 42",
	})

	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "Exit code: 42")
	require.NotNil(t, res.Metadata)
	assert.Equal(t, 42, res.Metadata["exit_code"])
}

func TestBash_StderrCaptured(t *testing.T) {
	b := NewBashTool()
	res := b.Execute(context.Background(), map[string]any{
		"command": "echo err >&2",
	})

	assert.False(t, res.IsError, res.Content)
	assert.Contains(t, res.Content, "err")
}

func TestBash_Timeout(t *testing.T) {
	b := NewBashTool()
	res := b.Execute(context.Background(), map[string]any{
		"command": "sleep 10",
		"timeout": float64(500), // 500ms
	})

	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "timed out")
}

func TestBash_Background(t *testing.T) {
	b := NewBashTool()
	res := b.Execute(context.Background(), map[string]any{
		"command":           "sleep 60",
		"run_in_background": true,
	})

	assert.False(t, res.IsError, res.Content)
	assert.Contains(t, res.Content, "PID")
	require.NotNil(t, res.Metadata)
	pid, ok := res.Metadata["pid"]
	assert.True(t, ok)
	assert.Greater(t, pid.(int), 0)
}

func TestBash_MissingCommand(t *testing.T) {
	b := NewBashTool()
	res := b.Execute(context.Background(), map[string]any{})

	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "command is required")
}

func TestBash_TimeoutClamped(t *testing.T) {
	b := NewBashTool()
	// Verify that an absurdly large timeout gets clamped to max.
	// We just check it doesn't crash; the command finishes fast.
	res := b.Execute(context.Background(), map[string]any{
		"command": "echo ok",
		"timeout": float64(999_999_999),
	})
	assert.False(t, res.IsError, res.Content)
	assert.Contains(t, res.Content, "ok")
}

func TestBash_LsTemp(t *testing.T) {
	dir := t.TempDir()
	b := NewBashTool()
	res := b.Execute(context.Background(), map[string]any{
		"command": "ls " + dir,
	})

	assert.False(t, res.IsError, res.Content)
	assert.Contains(t, res.Content, "Exit code: 0")
}
