package direct

import (
	"context"
	"testing"
	"time"

	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/gravitrone/providence-core/internal/engine/direct/tools"
	"github.com/gravitrone/providence-core/internal/engine/hooks"
	"github.com/gravitrone/providence-core/internal/engine/permissions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type permMockTool struct {
	name     string
	readOnly bool
}

func (t *permMockTool) Name() string {
	if t.name != "" {
		return t.name
	}
	return "test"
}
func (t *permMockTool) Description() string         { return "" }
func (t *permMockTool) InputSchema() map[string]any { return nil }
func (t *permMockTool) ReadOnly() bool              { return t.readOnly }
func (t *permMockTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	return tools.ToolResult{}
}

func TestPermissionHandler_NeedsPermission(t *testing.T) {
	ph := NewPermissionHandler()
	// Read-only builtins (Read, Glob, Grep) are auto-allowed.
	assert.False(t, ph.NeedsPermission(&permMockTool{name: "Read", readOnly: true}, nil))
	assert.False(t, ph.NeedsPermission(&permMockTool{name: "Glob", readOnly: true}, nil))
	assert.False(t, ph.NeedsPermission(&permMockTool{name: "Grep", readOnly: true}, nil))
	// Unknown/write tools require permission (Ask -> true).
	assert.True(t, ph.NeedsPermission(&permMockTool{name: "Bash", readOnly: false}, nil))
	assert.True(t, ph.NeedsPermission(&permMockTool{name: "Write", readOnly: false}, nil))
	assert.True(t, ph.NeedsPermission(&permMockTool{name: "test", readOnly: false}, nil))
}

func TestPermissionHandler_RequestAndApprove(t *testing.T) {
	ph := NewPermissionHandler()
	events := make(chan engine.ParsedEvent, 10)

	var approved bool
	var err error
	done := make(chan struct{})
	go func() {
		approved, err = ph.RequestPermission(context.Background(), "tc_1", events, "write_file", map[string]any{"path": "/tmp"})
		close(done)
	}()

	// Wait for permission event.
	select {
	case pe := <-events:
		require.Equal(t, "permission_request", pe.Type)
		pre, ok := pe.Data.(*engine.PermissionRequestEvent)
		require.True(t, ok)
		assert.Equal(t, "write_file", pre.Tool.Name)
		assert.Len(t, pre.Options, 2)
		// Approve it.
		ph.Respond(pre.QuestionID, true)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for permission event")
	}

	select {
	case <-done:
		require.NoError(t, err)
		assert.True(t, approved)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for RequestPermission to return")
	}
}

func TestPermissionHandler_RequestAndDeny(t *testing.T) {
	ph := NewPermissionHandler()
	events := make(chan engine.ParsedEvent, 10)

	var approved bool
	var err error
	done := make(chan struct{})
	go func() {
		approved, err = ph.RequestPermission(context.Background(), "tc_2", events, "delete_file", nil)
		close(done)
	}()

	select {
	case pe := <-events:
		pre := pe.Data.(*engine.PermissionRequestEvent)
		ph.Respond(pre.QuestionID, false)
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}

	select {
	case <-done:
		require.NoError(t, err)
		assert.False(t, approved)
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestNeedsPermissionArgumentPatternMatches(t *testing.T) {
	ph := NewPermissionHandlerWithConfig(
		nil,
		nil,
		nil,
		[]permissions.Rule{
			{Pattern: "Bash(git push *)", Behavior: permissions.Deny, Source: "userSettings"},
		},
		nil,
	)

	input := map[string]any{"command": "git push --force"}

	assert.False(t, ph.NeedsPermission(&tools.BashTool{}, input))
}

func TestNeedsPermissionArgumentPatternNoMatch(t *testing.T) {
	ph := NewPermissionHandlerWithConfig(
		nil,
		nil,
		nil,
		[]permissions.Rule{
			{Pattern: "Bash(git push *)", Behavior: permissions.Deny, Source: "userSettings"},
		},
		nil,
	)

	input := map[string]any{"command": "ls"}

	assert.True(t, ph.NeedsPermission(&tools.BashTool{}, input))
}

func TestNeedsPermissionSafetyPathFires(t *testing.T) {
	ph := NewPermissionHandler()

	input := map[string]any{"command": "cat .git/hooks/pre-commit"}

	assert.True(t, ph.NeedsPermission(&tools.BashTool{}, input))
}

func TestNeedsPermissionInterruptedRequestReturnsCancelled(t *testing.T) {
	ph := NewPermissionHandler()
	events := make(chan engine.ParsedEvent, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type permissionRequestResult struct {
		approved bool
		err      error
	}

	resultCh := make(chan permissionRequestResult, 1)
	go func() {
		approved, err := ph.RequestPermission(ctx, "tc_3", events, "write_file", map[string]any{"path": "/tmp"})
		resultCh <- permissionRequestResult{approved: approved, err: err}
	}()

	var questionID string
	select {
	case pe := <-events:
		require.Equal(t, "permission_request", pe.Type)
		pre, ok := pe.Data.(*engine.PermissionRequestEvent)
		require.True(t, ok)
		questionID = pre.QuestionID
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for permission event")
	}

	cancel()

	select {
	case result := <-resultCh:
		assert.False(t, result.approved)
		require.ErrorIs(t, result.err, context.Canceled)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for RequestPermission to return after cancellation")
	}

	ph.mu.Lock()
	_, ok := ph.pending[questionID]
	ph.mu.Unlock()
	assert.False(t, ok)
}

func TestPermissionHandler_CheckFiresPermissionDeniedHook(t *testing.T) {
	ph := NewPermissionHandler()
	ph.SetMode("deny")

	var recordedEvent string
	var recordedInput hooks.HookInput
	ph.SetHookEmitter(func(event string, input hooks.HookInput) {
		recordedEvent = event
		recordedInput = input
	})

	result := ph.Check(&permMockTool{name: "Write", readOnly: false}, map[string]any{"file_path": "/tmp/blocked"})
	require.NotNil(t, result)
	assert.Equal(t, permissions.Deny, result.Decision)
	assert.Equal(t, hooks.PermissionDenied, recordedEvent)
	assert.Equal(t, "Write", recordedInput.ToolName)
}

func TestPermissionHandler_RequestPermissionFiresPermissionGrantedHook(t *testing.T) {
	ph := NewPermissionHandler()
	events := make(chan engine.ParsedEvent, 10)

	var recordedEvent string
	var recordedInput hooks.HookInput
	ph.SetHookEmitter(func(event string, input hooks.HookInput) {
		recordedEvent = event
		recordedInput = input
	})

	type requestResult struct {
		approved bool
		err      error
	}

	resultCh := make(chan requestResult, 1)
	done := make(chan struct{})
	go func() {
		approved, err := ph.RequestPermission(context.Background(), "tc_grant", events, "Write", map[string]any{"file_path": "/tmp/allowed"})
		resultCh <- requestResult{approved: approved, err: err}
		close(done)
	}()

	select {
	case pe := <-events:
		pre, ok := pe.Data.(*engine.PermissionRequestEvent)
		require.True(t, ok)
		ph.Respond(pre.QuestionID, true)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for permission request")
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for approval result")
	}

	result := <-resultCh
	require.NoError(t, result.err)
	assert.True(t, result.approved)

	assert.Equal(t, hooks.PermissionGranted, recordedEvent)
	assert.Equal(t, "Write", recordedInput.ToolName)
}
