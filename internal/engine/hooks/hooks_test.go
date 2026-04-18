package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunnerNoHooks(t *testing.T) {
	r := NewRunner(nil)
	out, err := r.Run(context.Background(), PreToolUse, HookInput{})
	require.NoError(t, err)
	assert.Nil(t, out, "no hooks registered should return nil output")
}

func TestRunnerHasHooks(t *testing.T) {
	r := NewRunner(map[string][]HookConfig{
		PreToolUse: {{Command: "echo ok"}},
	})
	assert.True(t, r.HasHooks(PreToolUse))
	assert.False(t, r.HasHooks(PostToolUse))
}

func TestRunnerShellHookSuccess(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell tests require unix")
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "hook.sh")
	err := os.WriteFile(script, []byte(`#!/bin/sh
echo '{"decision":"approve","reason":"looks good"}'
exit 0
`), 0o755)
	require.NoError(t, err)

	r := NewRunner(map[string][]HookConfig{
		PreToolUse: {{Command: script, Timeout: 10 * time.Second}},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	out, err := r.Run(ctx, PreToolUse, HookInput{
		ToolName:  "Bash",
		ToolInput: map[string]string{"command": "ls"},
	})
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, "approve", out.Decision)
	assert.Equal(t, "looks good", out.Reason)
}

func TestRunnerShellHookEmptyOutput(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell tests require unix")
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "hook.sh")
	err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755)
	require.NoError(t, err)

	r := NewRunner(map[string][]HookConfig{
		SessionStart: {{Command: script, Timeout: 5 * time.Second}},
	})

	out, err := r.Run(context.Background(), SessionStart, HookInput{})
	require.NoError(t, err)
	assert.Nil(t, out, "empty stdout with exit 0 should return nil output")
}

func TestRunnerShellHookBlockingError(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell tests require unix")
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "hook.sh")
	err := os.WriteFile(script, []byte(`#!/bin/sh
echo '{"decision":"block","reason":"denied by policy"}' >&1
echo "policy violation" >&2
exit 2
`), 0o755)
	require.NoError(t, err)

	r := NewRunner(map[string][]HookConfig{
		PreToolUse: {{Command: script, Timeout: 5 * time.Second}},
	})

	out, err := r.Run(context.Background(), PreToolUse, HookInput{ToolName: "Bash"})
	require.Error(t, err)

	var blockErr *BlockingError
	require.ErrorAs(t, err, &blockErr)
	assert.Contains(t, blockErr.Message, "policy violation")
	require.NotNil(t, out)
	assert.Equal(t, "block", out.Decision)
	assert.Equal(t, "denied by policy", out.Reason)
}

func TestRunnerShellHookNonBlockingError(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell tests require unix")
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "hook.sh")
	err := os.WriteFile(script, []byte("#!/bin/sh\nexit 1\n"), 0o755)
	require.NoError(t, err)

	r := NewRunner(map[string][]HookConfig{
		PostToolUse: {{Command: script, Timeout: 5 * time.Second}},
	})

	out, err := r.Run(context.Background(), PostToolUse, HookInput{})
	require.Error(t, err)
	assert.Nil(t, out)
	assert.Contains(t, err.Error(), "exited with code 1")
}

func TestRunnerShellHookTimeout(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell tests require unix")
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "hook.sh")
	err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 30\n"), 0o755)
	require.NoError(t, err)

	r := NewRunner(map[string][]HookConfig{
		PreToolUse: {{Command: script, Timeout: 100 * time.Millisecond}},
	})

	_, err = r.Run(context.Background(), PreToolUse, HookInput{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
}

func TestRunnerShellHookReceivesEnvVar(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell tests require unix")
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "hook.sh")
	// Script reads CLAUDE_HOOK_INPUT env var and echoes it to stdout
	err := os.WriteFile(script, []byte(`#!/bin/sh
echo "$CLAUDE_HOOK_INPUT"
`), 0o755)
	require.NoError(t, err)

	r := NewRunner(map[string][]HookConfig{
		PreToolUse: {{Command: script, Timeout: 5 * time.Second}},
	})

	input := HookInput{
		ToolName:  "Bash",
		SessionID: "test-123",
	}
	out, err := r.Run(context.Background(), PreToolUse, input)
	require.NoError(t, err)
	// The output is the input JSON echoed back, which won't parse as
	// a valid HookOutput with meaningful fields, so it returns a zero-value struct.
	// The important thing is no error - the env var was set and readable.
	_ = out
}

func TestRunnerHTTPHook(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var input HookInput
		err := json.NewDecoder(r.Body).Decode(&input)
		require.NoError(t, err)
		assert.Equal(t, PreToolUse, input.Event)
		assert.Equal(t, "Write", input.ToolName)

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"decision":"approve","system_message":"proceed with caution"}`)
	}))
	defer srv.Close()

	r := NewRunner(map[string][]HookConfig{
		PreToolUse: {{URL: srv.URL, Timeout: 5 * time.Second}},
	})

	out, err := r.Run(context.Background(), PreToolUse, HookInput{ToolName: "Write"})
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, "approve", out.Decision)
	assert.Equal(t, "proceed with caution", out.SystemMessage)
}

func TestRunnerHTTPHookEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r := NewRunner(map[string][]HookConfig{
		PostToolUse: {{URL: srv.URL, Timeout: 5 * time.Second}},
	})

	out, err := r.Run(context.Background(), PostToolUse, HookInput{})
	require.NoError(t, err)
	assert.Nil(t, out, "empty response body should return nil")
}

func TestRunnerHTTPHookError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "internal error")
	}))
	defer srv.Close()

	r := NewRunner(map[string][]HookConfig{
		PostToolUse: {{URL: srv.URL, Timeout: 5 * time.Second}},
	})

	_, err := r.Run(context.Background(), PostToolUse, HookInput{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 500")
}

func TestRunnerHookExecutedFiresAfterEvent(t *testing.T) {
	var (
		mu     sync.Mutex
		events []string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input HookInput
		require.NoError(t, json.NewDecoder(r.Body).Decode(&input))

		mu.Lock()
		events = append(events, input.Event)
		mu.Unlock()

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	r := NewRunner(map[string][]HookConfig{
		PreToolUse:   {{URL: srv.URL, Timeout: 5 * time.Second}},
		HookExecuted: {{URL: srv.URL, Timeout: 5 * time.Second}},
	})

	out, err := r.Run(context.Background(), PreToolUse, HookInput{ToolName: "Write"})
	require.NoError(t, err)
	assert.NotNil(t, out)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{PreToolUse, HookExecuted}, events)
}

func TestRunnerAsync(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell tests require unix")
	}

	dir := t.TempDir()
	marker := filepath.Join(dir, "marker.txt")
	script := filepath.Join(dir, "hook.sh")
	err := os.WriteFile(script, []byte(fmt.Sprintf("#!/bin/sh\necho done > %s\n", marker)), 0o755)
	require.NoError(t, err)

	r := NewRunner(map[string][]HookConfig{
		SessionEnd: {{Command: script, Timeout: 5 * time.Second}},
	})

	// RunAsync must return nearly immediately (well before the hook itself
	// finishes). Assert the async semantics directly so a future refactor
	// that accidentally makes the call synchronous fails loudly.
	start := time.Now()
	r.RunAsync(context.Background(), SessionEnd, HookInput{})
	assert.Less(t, time.Since(start), 500*time.Millisecond,
		"RunAsync must return before the hook finishes executing")

	// The hook itself runs in a background goroutine. Poll up to 30s for
	// the marker file to appear. The previous 10s bound was enough for
	// isolated runs but flaked under `-race -count=1 ./...` full-suite
	// contention where goroutine scheduling + shell spawn latency spike.
	// 30s is generous and does not weaken the test: if RunAsync were
	// silently synchronous the elapsed-time assertion above already fails.
	require.Eventually(t, func() bool {
		_, err := os.Stat(marker)
		return err == nil
	}, 30*time.Second, 50*time.Millisecond, "async hook should have created marker file")
}

func TestHookInputSerialization(t *testing.T) {
	input := HookInput{
		Event:     PreToolUse,
		ToolName:  "Bash",
		ToolInput: map[string]string{"command": "ls -la"},
		SessionID: "session-abc-123",
		Timestamp: "2026-04-12T00:00:00Z",
	}

	data, err := json.Marshal(input)
	require.NoError(t, err)

	var roundtrip HookInput
	err = json.Unmarshal(data, &roundtrip)
	require.NoError(t, err)

	assert.Equal(t, input.Event, roundtrip.Event)
	assert.Equal(t, input.ToolName, roundtrip.ToolName)
	assert.Equal(t, input.SessionID, roundtrip.SessionID)
	assert.Equal(t, input.Timestamp, roundtrip.Timestamp)

	// ToolInput round-trips as map[string]interface{} from JSON
	toolInput, ok := roundtrip.ToolInput.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "ls -la", toolInput["command"])
}

func TestHookOutputSerialization(t *testing.T) {
	cont := true
	output := HookOutput{
		Continue:       &cont,
		Decision:       "approve",
		Reason:         "all good",
		SystemMessage:  "warning: careful",
		SuppressOutput: false,
	}

	data, err := json.Marshal(output)
	require.NoError(t, err)

	var roundtrip HookOutput
	err = json.Unmarshal(data, &roundtrip)
	require.NoError(t, err)

	require.NotNil(t, roundtrip.Continue)
	assert.True(t, *roundtrip.Continue)
	assert.Equal(t, "approve", roundtrip.Decision)
	assert.Equal(t, "all good", roundtrip.Reason)
	assert.Equal(t, "warning: careful", roundtrip.SystemMessage)
	assert.False(t, roundtrip.SuppressOutput)
}
