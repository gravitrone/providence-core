package direct

import (
	"context"
	"testing"
	"time"

	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/gravitrone/providence-core/internal/engine/direct/tools"
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
func (t *permMockTool) Description() string                                        { return "" }
func (t *permMockTool) InputSchema() map[string]any                                { return nil }
func (t *permMockTool) ReadOnly() bool                                             { return t.readOnly }
func (t *permMockTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	return tools.ToolResult{}
}

func TestPermissionHandler_NeedsPermission(t *testing.T) {
	ph := NewPermissionHandler()
	// Read-only builtins (Read, Glob, Grep) are auto-allowed.
	assert.False(t, ph.NeedsPermission(&permMockTool{name: "Read", readOnly: true}))
	assert.False(t, ph.NeedsPermission(&permMockTool{name: "Glob", readOnly: true}))
	assert.False(t, ph.NeedsPermission(&permMockTool{name: "Grep", readOnly: true}))
	// Unknown/write tools require permission (Ask -> true).
	assert.True(t, ph.NeedsPermission(&permMockTool{name: "Bash", readOnly: false}))
	assert.True(t, ph.NeedsPermission(&permMockTool{name: "Write", readOnly: false}))
	assert.True(t, ph.NeedsPermission(&permMockTool{name: "test", readOnly: false}))
}

func TestPermissionHandler_RequestAndApprove(t *testing.T) {
	ph := NewPermissionHandler()
	events := make(chan engine.ParsedEvent, 10)

	var approved bool
	done := make(chan struct{})
	go func() {
		approved = ph.RequestPermission("tc_1", events, "write_file", map[string]any{"path": "/tmp"})
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
		assert.True(t, approved)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for RequestPermission to return")
	}
}

func TestPermissionHandler_RequestAndDeny(t *testing.T) {
	ph := NewPermissionHandler()
	events := make(chan engine.ParsedEvent, 10)

	var approved bool
	done := make(chan struct{})
	go func() {
		approved = ph.RequestPermission("tc_2", events, "delete_file", nil)
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
		assert.False(t, approved)
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}
