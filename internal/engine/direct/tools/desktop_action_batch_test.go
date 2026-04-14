package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/gravitrone/providence-core/internal/bridge/macos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock ---

type mockBatchBridge struct {
	actionBatchFn func(ctx context.Context, p macos.ActionBatchParams) (macos.ActionBatchResult, error)
}

func (m *mockBatchBridge) ActionBatch(ctx context.Context, p macos.ActionBatchParams) (macos.ActionBatchResult, error) {
	if m.actionBatchFn != nil {
		return m.actionBatchFn(ctx, p)
	}
	return macos.ActionBatchResult{}, nil
}

// --- DesktopActionBatch tests ---

func TestDesktopActionBatchName(t *testing.T) {
	tool := NewDesktopActionBatchTool(&mockBatchBridge{})
	assert.Equal(t, "DesktopActionBatch", tool.Name())
}

func TestDesktopActionBatchReadOnly(t *testing.T) {
	tool := NewDesktopActionBatchTool(&mockBatchBridge{})
	assert.False(t, tool.ReadOnly())
}

func TestDesktopActionBatchInputSchemaShape(t *testing.T) {
	tool := NewDesktopActionBatchTool(&mockBatchBridge{})
	schema := tool.InputSchema()
	require.Equal(t, "object", schema["type"])
	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, props, "actions")
	assert.Contains(t, props, "stop_on_error")
	assert.Contains(t, props, "screenshot_after")
	required, ok := schema["required"].([]string)
	require.True(t, ok)
	assert.Contains(t, required, "actions")
}

func TestDesktopActionBatchEmptyActionsIsError(t *testing.T) {
	tool := NewDesktopActionBatchTool(&mockBatchBridge{})

	result := tool.Execute(context.Background(), map[string]any{
		"actions": []any{},
	})
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "empty")
}

func TestDesktopActionBatchPassThroughActions(t *testing.T) {
	var gotParams macos.ActionBatchParams
	failIdx := 1
	expected := macos.ActionBatchResult{
		Completed: 1,
		FailedAt:  &failIdx,
		Actions: []macos.BatchActionResult{
			{Index: 0, Type: "click", OK: true, DurationMS: 5},
			{Index: 1, Type: "verify_ax", OK: false, Error: "element_not_found", DurationMS: 2},
		},
	}
	mock := &mockBatchBridge{
		actionBatchFn: func(_ context.Context, p macos.ActionBatchParams) (macos.ActionBatchResult, error) {
			gotParams = p
			return expected, nil
		},
	}
	tool := NewDesktopActionBatchTool(mock)

	result := tool.Execute(context.Background(), map[string]any{
		"actions": []any{
			map[string]any{"type": "click", "params": map[string]any{"x": 100, "y": 200}},
			map[string]any{"type": "verify_ax", "params": map[string]any{"expect": map[string]any{"role": "AXButton"}}},
		},
		"stop_on_error": true,
	})
	require.False(t, result.IsError, result.Content)

	require.Len(t, gotParams.Actions, 2)
	assert.Equal(t, "click", gotParams.Actions[0].Type)
	assert.Equal(t, "verify_ax", gotParams.Actions[1].Type)
	assert.True(t, gotParams.StopOnError)

	var decoded macos.ActionBatchResult
	require.NoError(t, json.Unmarshal([]byte(result.Content), &decoded))
	assert.Equal(t, 1, decoded.Completed)
	require.NotNil(t, decoded.FailedAt)
	assert.Equal(t, 1, *decoded.FailedAt)
	require.Len(t, decoded.Actions, 2)
}

func TestDesktopActionBatchScreenshotAfterParam(t *testing.T) {
	var gotParams macos.ActionBatchParams
	mock := &mockBatchBridge{
		actionBatchFn: func(_ context.Context, p macos.ActionBatchParams) (macos.ActionBatchResult, error) {
			gotParams = p
			return macos.ActionBatchResult{
				Completed:       1,
				FinalScreenshot: "/tmp/screen.png",
				Actions:         []macos.BatchActionResult{{Index: 0, Type: "type", OK: true}},
			}, nil
		},
	}
	tool := NewDesktopActionBatchTool(mock)

	result := tool.Execute(context.Background(), map[string]any{
		"actions":          []any{map[string]any{"type": "type", "params": map[string]any{"text": "hello"}}},
		"screenshot_after": true,
	})
	require.False(t, result.IsError, result.Content)
	assert.True(t, gotParams.ScreenshotAfter)

	var decoded macos.ActionBatchResult
	require.NoError(t, json.Unmarshal([]byte(result.Content), &decoded))
	assert.Equal(t, "/tmp/screen.png", decoded.FinalScreenshot)
}

func TestDesktopActionBatchBridgeErrorPropagates(t *testing.T) {
	mock := &mockBatchBridge{
		actionBatchFn: func(_ context.Context, _ macos.ActionBatchParams) (macos.ActionBatchResult, error) {
			return macos.ActionBatchResult{}, errors.New("action_batch: requires native bridge")
		},
	}
	tool := NewDesktopActionBatchTool(mock)

	result := tool.Execute(context.Background(), map[string]any{
		"actions": []any{map[string]any{"type": "key"}},
	})
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "action_batch failed")
}
