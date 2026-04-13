package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDesktopAppsName(t *testing.T) {
	tool := NewDesktopAppsTool(nil)
	assert.Equal(t, "DesktopApps", tool.Name())
}

func TestDesktopAppsSchema(t *testing.T) {
	tool := NewDesktopAppsTool(nil)
	schema := tool.InputSchema()
	props := schema["properties"].(map[string]any)
	assert.Contains(t, props, "action")
}

func TestDesktopAppsInvalidAction(t *testing.T) {
	tool := NewDesktopAppsTool(nil)
	result := tool.Execute(context.Background(), map[string]any{"action": "destroy"})
	assert.True(t, result.IsError)
}

func TestDesktopAppsListAction(t *testing.T) {
	tool := NewDesktopAppsTool(nil)
	schema := tool.InputSchema()
	actionProp := schema["properties"].(map[string]any)["action"].(map[string]any)
	enum := actionProp["enum"].([]string)
	assert.Contains(t, enum, "list")
	assert.Contains(t, enum, "focus")
	assert.Contains(t, enum, "launch")
}
