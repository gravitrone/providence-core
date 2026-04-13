package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClipboardName(t *testing.T) {
	tool := NewClipboardTool(nil)
	assert.Equal(t, "Clipboard", tool.Name())
}

func TestClipboardSchema(t *testing.T) {
	tool := NewClipboardTool(nil)
	schema := tool.InputSchema()
	props := schema["properties"].(map[string]any)
	assert.Contains(t, props, "action")
}

func TestClipboardInvalidAction(t *testing.T) {
	tool := NewClipboardTool(nil)
	result := tool.Execute(context.Background(), map[string]any{"action": "delete"})
	assert.True(t, result.IsError)
}

func TestClipboardWriteRequiresText(t *testing.T) {
	tool := NewClipboardTool(nil)
	result := tool.Execute(context.Background(), map[string]any{"action": "write"})
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "text")
}
