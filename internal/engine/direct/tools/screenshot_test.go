package tools

import (
	"context"
	"runtime"
	"testing"

	"github.com/gravitrone/providence-core/internal/bridge/macos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScreenshotToolSchema(t *testing.T) {
	bridge := macos.New()
	tool := NewScreenshotTool(bridge)

	assert.Equal(t, "Screenshot", tool.Name())
	assert.True(t, tool.ReadOnly())

	schema := tool.InputSchema()
	require.NotNil(t, schema)

	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	_, hasRegion := props["region"]
	assert.True(t, hasRegion, "schema should have region property")
}

func TestScreenshotToolNonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("this test checks non-darwin behavior")
	}

	bridge := macos.New()
	tool := NewScreenshotTool(bridge)
	result := tool.Execute(context.Background(), map[string]any{})

	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "macOS")
}

func TestDesktopClickToolSchema(t *testing.T) {
	bridge := macos.New()
	tool := NewDesktopClickTool(bridge)

	assert.Equal(t, "DesktopClick", tool.Name())
	assert.False(t, tool.ReadOnly())

	schema := tool.InputSchema()
	require.NotNil(t, schema)

	required, ok := schema["required"].([]string)
	require.True(t, ok)
	assert.Contains(t, required, "x")
	assert.Contains(t, required, "y")
}

func TestDesktopClickToolValidation(t *testing.T) {
	bridge := macos.New()
	tool := NewDesktopClickTool(bridge)

	if runtime.GOOS != "darwin" {
		result := tool.Execute(context.Background(), map[string]any{"x": 100.0, "y": 200.0})
		assert.True(t, result.IsError)
		assert.Contains(t, result.Content, "macOS")
		return
	}

	// Missing coordinates
	result := tool.Execute(context.Background(), map[string]any{})
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "coordinates")
}

func TestDesktopTypeToolSchema(t *testing.T) {
	bridge := macos.New()
	tool := NewDesktopTypeTool(bridge)

	assert.Equal(t, "DesktopType", tool.Name())
	assert.False(t, tool.ReadOnly())

	schema := tool.InputSchema()
	require.NotNil(t, schema)

	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	_, hasText := props["text"]
	_, hasKey := props["key"]
	assert.True(t, hasText)
	assert.True(t, hasKey)
}

func TestDesktopTypeToolValidation(t *testing.T) {
	bridge := macos.New()
	tool := NewDesktopTypeTool(bridge)

	if runtime.GOOS != "darwin" {
		result := tool.Execute(context.Background(), map[string]any{"text": "hello"})
		assert.True(t, result.IsError)
		assert.Contains(t, result.Content, "macOS")
		return
	}

	// Neither text nor key
	result := tool.Execute(context.Background(), map[string]any{})
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "required")
}

func TestDesktopAppsToolSchema(t *testing.T) {
	bridge := macos.New()
	tool := NewDesktopAppsTool(bridge)

	assert.Equal(t, "DesktopApps", tool.Name())
	assert.True(t, tool.ReadOnly())

	schema := tool.InputSchema()
	require.NotNil(t, schema)

	required, ok := schema["required"].([]string)
	require.True(t, ok)
	assert.Contains(t, required, "action")
}

func TestDesktopAppsToolValidation(t *testing.T) {
	bridge := macos.New()
	tool := NewDesktopAppsTool(bridge)

	if runtime.GOOS != "darwin" {
		result := tool.Execute(context.Background(), map[string]any{"action": "list"})
		assert.True(t, result.IsError)
		assert.Contains(t, result.Content, "macOS")
		return
	}

	// Focus without app name
	result := tool.Execute(context.Background(), map[string]any{"action": "focus"})
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "app name")
}

func TestClipboardToolSchema(t *testing.T) {
	bridge := macos.New()
	tool := NewClipboardTool(bridge)

	assert.Equal(t, "Clipboard", tool.Name())
	assert.False(t, tool.ReadOnly())

	schema := tool.InputSchema()
	require.NotNil(t, schema)

	required, ok := schema["required"].([]string)
	require.True(t, ok)
	assert.Contains(t, required, "action")
}

func TestClipboardToolValidation(t *testing.T) {
	bridge := macos.New()
	tool := NewClipboardTool(bridge)

	if runtime.GOOS != "darwin" {
		result := tool.Execute(context.Background(), map[string]any{"action": "read"})
		assert.True(t, result.IsError)
		assert.Contains(t, result.Content, "macOS")
		return
	}

	// Write without text
	result := tool.Execute(context.Background(), map[string]any{"action": "write"})
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "text is required")
}
