package tools

import (
	"context"
	"encoding/json"
	"runtime"
	"testing"

	"github.com/gravitrone/providence-core/internal/bridge/macos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDesktopReadScreenName(t *testing.T) {
	tool := NewDesktopReadScreenTool(&mockAXBridge{})
	assert.Equal(t, "DesktopReadScreen", tool.Name())
}

func TestDesktopReadScreenReadOnly(t *testing.T) {
	tool := NewDesktopReadScreenTool(&mockAXBridge{})
	assert.True(t, tool.ReadOnly())
}

func TestDesktopReadScreenFormatFlatReturnsFlatField(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}

	mock := &mockAXBridge{
		axTreeFn: func(_ context.Context, p macos.AXTreeParams) (macos.AXTreeResult, error) {
			assert.Equal(t, "flat", p.Format)
			return macos.AXTreeResult{Flat: "AXWindow > AXButton [OK]"}, nil
		},
	}

	tool := NewDesktopReadScreenTool(mock)
	result := tool.Execute(context.Background(), map[string]any{})

	require.False(t, result.IsError, result.Content)
	assert.Equal(t, "AXWindow > AXButton [OK]", result.Content)
}

func TestDesktopReadScreenFormatJSONReturnsRootJSON(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}

	root := &macos.AXNode{ID: "root-1", Role: "AXWindow"}
	mock := &mockAXBridge{
		axTreeFn: func(_ context.Context, p macos.AXTreeParams) (macos.AXTreeResult, error) {
			assert.Equal(t, "json", p.Format)
			return macos.AXTreeResult{Root: root}, nil
		},
	}

	tool := NewDesktopReadScreenTool(mock)
	result := tool.Execute(context.Background(), map[string]any{"format": "json"})

	require.False(t, result.IsError, result.Content)

	var node macos.AXNode
	require.NoError(t, json.Unmarshal([]byte(result.Content), &node))
	assert.Equal(t, "root-1", node.ID)
	assert.Equal(t, "AXWindow", node.Role)
}

func TestDesktopReadScreenDefaultParams(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}

	var capturedParams macos.AXTreeParams
	mock := &mockAXBridge{
		axTreeFn: func(_ context.Context, p macos.AXTreeParams) (macos.AXTreeResult, error) {
			capturedParams = p
			return macos.AXTreeResult{Flat: "ok"}, nil
		},
	}

	tool := NewDesktopReadScreenTool(mock)
	result := tool.Execute(context.Background(), map[string]any{})

	require.False(t, result.IsError, result.Content)
	assert.Equal(t, 12, capturedParams.MaxDepth)
	assert.Equal(t, 2000, capturedParams.MaxNodes)
	assert.False(t, capturedParams.IncludeInvisible)
	assert.Equal(t, "flat", capturedParams.Format)
}
