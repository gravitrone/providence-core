package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDesktopTypeName(t *testing.T) {
	tool := NewDesktopTypeTool(nil)
	assert.Equal(t, "DesktopType", tool.Name())
}

func TestDesktopTypeSchema(t *testing.T) {
	tool := NewDesktopTypeTool(nil)
	schema := tool.InputSchema()
	props := schema["properties"].(map[string]any)
	assert.Contains(t, props, "text")
	assert.Contains(t, props, "key")
}

func TestDesktopTypeEmptyInput(t *testing.T) {
	tool := NewDesktopTypeTool(nil)
	result := tool.Execute(context.Background(), map[string]any{})
	assert.True(t, result.IsError)
}

func TestDesktopTypeNotReadOnly(t *testing.T) {
	tool := NewDesktopTypeTool(nil)
	assert.False(t, tool.ReadOnly())
}
