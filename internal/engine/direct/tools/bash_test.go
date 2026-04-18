package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gravitrone/providence-core/internal/engine/hooks"
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

func TestBashCwdPersistsAcrossCalls(t *testing.T) {
	root := t.TempDir()
	b := newTestBashTool(t, "persist-session", root)

	first := b.Execute(context.Background(), map[string]any{
		"command": "mkdir sub && cd sub",
	})

	require.False(t, first.IsError, first.Content)
	assert.NotContains(t, first.Content, cwdSentinelPrefix)
	assert.Contains(t, b.cwdStatePath(), "persist-session")
	assertSavedCwd(t, b, filepath.Join(root, "sub"))

	second := b.Execute(context.Background(), map[string]any{
		"command": "pwd",
	})

	require.False(t, second.IsError, second.Content)
	assert.Contains(t, second.Content, filepath.Join(root, "sub"))
	assert.NotContains(t, second.Content, cwdSentinelPrefix)

	failed := b.Execute(context.Background(), map[string]any{
		"command": "cd .. && exit 7",
	})

	require.True(t, failed.IsError)
	assert.Contains(t, failed.Content, "Exit code: 7")
	assertSavedCwd(t, b, filepath.Join(root, "sub"))

	third := b.Execute(context.Background(), map[string]any{
		"command": "pwd",
	})

	require.False(t, third.IsError, third.Content)
	assert.Contains(t, third.Content, filepath.Join(root, "sub"))
}

func TestBashCwdFallsBackWhenSavedDirMissing(t *testing.T) {
	root := t.TempDir()
	b := newTestBashTool(t, "missing-session", root)

	first := b.Execute(context.Background(), map[string]any{
		"command": "mkdir sub && cd sub",
	})

	require.False(t, first.IsError, first.Content)
	require.NoError(t, os.RemoveAll(filepath.Join(root, "sub")))

	second := b.Execute(context.Background(), map[string]any{
		"command": "pwd",
	})

	require.False(t, second.IsError, second.Content)
	assert.Contains(t, second.Content, root)
	assert.NotContains(t, second.Content, filepath.Join(root, "sub"))
	assertSavedCwd(t, b, root)
}

func TestBashCwdTempfileCleanedUp(t *testing.T) {
	root := t.TempDir()
	b := newTestBashTool(t, "cleanup-session", root)

	first := b.Execute(context.Background(), map[string]any{
		"command": "pwd",
	})

	require.False(t, first.IsError, first.Content)

	path := b.cwdStatePath()
	_, err := os.Stat(path)
	require.NoError(t, err)
	assert.Contains(t, path, "cleanup-session")

	require.NoError(t, b.Close())

	_, err = os.Stat(path)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestBash_FiresCwdChangedHook(t *testing.T) {
	root := t.TempDir()
	b := newTestBashTool(t, "hook-session", root)
	spy := &hookSpy{}
	b.SetHookEmitter(spy.record)

	res := b.Execute(context.Background(), map[string]any{
		"command": "mkdir sub && cd sub",
	})
	require.False(t, res.IsError, res.Content)

	events, inputs := spy.snapshot()
	require.Equal(t, []string{hooks.CwdChanged}, events)
	require.Len(t, inputs, 1)
	assert.Equal(t, "Bash", inputs[0].ToolName)
	assert.Equal(t, map[string]string{"cwd": filepath.Join(root, "sub")}, inputs[0].ToolInput)
}

func TestSandboxProfileIncludesAllowedNetwork(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	writeSandboxConfig(t, root, `[sandbox]
allow_network = ["localhost:3000", "api.myco.internal"]
`)

	b := newTestBashTool(t, "sandbox-network-session", root)
	path, err := b.ensureSandboxProfile()
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	profile := string(data)
	assert.Contains(t, profile, `(remote tcp "localhost:3000")`)
	assert.Contains(t, profile, `(remote udp "localhost:3000")`)
	assert.Contains(t, profile, `(remote tcp "api.myco.internal:*")`)
	assert.Contains(t, profile, sandboxResolverSocket)
}

func TestSandboxProfileIncludesAllowedWrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	writeSandboxConfig(t, root, `[sandbox]
allow_write = ["/tmp/cache", "~/.myapp/data"]
`)

	b := newTestBashTool(t, "sandbox-write-session", root)
	path, err := b.ensureSandboxProfile()
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	profile := string(data)
	assert.Contains(t, profile, `(allow file-write* (subpath "/tmp/cache"))`)
	assert.Contains(t, profile, `(allow file-write* (subpath "`+filepath.Join(home, ".myapp", "data")+`"))`)
}

func TestSandboxProfileRejectsWildcardNetwork(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	writeSandboxConfig(t, root, `[sandbox]
allow_network = ["*:*"]
`)

	b := newTestBashTool(t, "sandbox-reject-network", root)
	_, err := b.ensureSandboxProfile()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `sandbox.allow_network "*:*" is too broad`)
}

func TestSandboxProfileRejectsBareHomePath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	writeSandboxConfig(t, root, `[sandbox]
allow_write = ["~"]
`)

	b := newTestBashTool(t, "sandbox-reject-home", root)
	_, err := b.ensureSandboxProfile()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must target a subdirectory")
}

func TestSandboxProfileTempfileCleanedUp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	writeSandboxConfig(t, root, `[sandbox]
allow_network = ["localhost:3000"]
`)

	b := newTestBashTool(t, "sandbox-cleanup", root)
	path, err := b.ensureSandboxProfile()
	require.NoError(t, err)

	_, err = os.Stat(path)
	require.NoError(t, err)

	require.NoError(t, b.Close())

	_, err = os.Stat(path)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func newTestBashTool(t *testing.T, sessionID string, sessionRoot string) *BashTool {
	t.Helper()

	b := NewBashTool()
	b.SandboxDisabled = true
	b.sessionID = sessionID
	b.sessionRoot = sessionRoot
	t.Cleanup(func() {
		_ = b.Close()
	})

	return b
}

func assertSavedCwd(t *testing.T, b *BashTool, want string) {
	t.Helper()

	data, err := os.ReadFile(b.cwdStatePath())
	require.NoError(t, err)
	assert.Equal(t, want, strings.TrimSpace(string(data)))
}

func writeSandboxConfig(t *testing.T, root, content string) {
	t.Helper()

	path := filepath.Join(root, ".providence", "config.toml")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func TestBashToolHasTightCap(t *testing.T) {
	b := NewBashTool()

	provider, ok := any(b).(ResultCapProvider)
	require.True(t, ok)
	assert.Equal(t, bashToolResultSizeCap, provider.ResultSizeCap())
}
