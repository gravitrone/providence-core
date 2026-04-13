package hooks

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeShellTestScript(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hook.sh")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755))
	return path
}

func TestShellHookExitZero(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell tests require unix")
	}

	script := writeShellTestScript(t, `#!/bin/sh
echo '{"decision":"approve","reason":"shell ok"}'
exit 0
`)

	out, err := execShellHook(context.Background(), HookConfig{
		Command: script,
		Timeout: time.Second,
	}, HookInput{
		Event:    PreToolUse,
		ToolName: "Bash",
	})

	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, "approve", out.Decision)
	assert.Equal(t, "shell ok", out.Reason)
}

func TestShellHookExitTwo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell tests require unix")
	}

	script := writeShellTestScript(t, `#!/bin/sh
echo '{"decision":"block","reason":"blocked by hook"}'
echo 'policy denied' >&2
exit 2
`)

	out, err := execShellHook(context.Background(), HookConfig{
		Command: script,
		Timeout: time.Second,
	}, HookInput{
		Event:    PreToolUse,
		ToolName: "Write",
	})

	require.Error(t, err)
	require.NotNil(t, out)

	var blockErr *BlockingError
	require.ErrorAs(t, err, &blockErr)
	assert.Contains(t, blockErr.Message, "policy denied")
	assert.Equal(t, "block", out.Decision)
	assert.Equal(t, "blocked by hook", out.Reason)
}

func TestShellHookTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell tests require unix")
	}

	script := writeShellTestScript(t, `#!/bin/sh
sleep 1
`)

	out, err := execShellHook(context.Background(), HookConfig{
		Command: script,
		Timeout: 50 * time.Millisecond,
	}, HookInput{
		Event: PreToolUse,
	})

	require.Error(t, err)
	assert.Nil(t, out)
	assert.Contains(t, err.Error(), "timed out")
}
