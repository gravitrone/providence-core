package tools

import (
	"context"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDesktopClickName(t *testing.T) {
	tool := NewDesktopClickTool(nil)
	assert.Equal(t, "DesktopClick", tool.Name())
}

func TestDesktopClickSchema(t *testing.T) {
	tool := NewDesktopClickTool(nil)
	schema := tool.InputSchema()
	props := schema["properties"].(map[string]any)
	assert.Contains(t, props, "x")
	assert.Contains(t, props, "y")
	assert.Contains(t, props, "action")
	assert.Equal(t, []string{"x", "y"}, schema["required"])
}

func TestDesktopClickNegativeCoords(t *testing.T) {
	tool := NewDesktopClickTool(nil)
	result := tool.Execute(context.Background(), map[string]any{"x": -1, "y": 100})
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "non-negative")
}

func TestDesktopClickMissingCoords(t *testing.T) {
	tool := NewDesktopClickTool(nil)
	result := tool.Execute(context.Background(), map[string]any{})
	assert.True(t, result.IsError)
}

func TestDesktopClickUnknownAction(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}
	tool := NewDesktopClickTool(nil)
	result := tool.Execute(context.Background(), map[string]any{"x": 100, "y": 100, "action": "triple_click"})
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "unknown action")
}

func TestDesktopClickNotReadOnly(t *testing.T) {
	tool := NewDesktopClickTool(nil)
	assert.False(t, tool.ReadOnly())
}
