package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigToolMetadata(t *testing.T) {
	c := NewConfigTool()
	assert.Equal(t, "Config", c.Name())
	assert.False(t, c.ReadOnly())
	assert.NotEmpty(t, c.Description())

	schema := c.InputSchema()
	require.NotNil(t, schema)
	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	_, hasAction := props["action"]
	_, hasKey := props["key"]
	assert.True(t, hasAction)
	assert.True(t, hasKey)
}

func TestConfigToolSetAndGet(t *testing.T) {
	c := NewConfigTool()

	// Set a value.
	result := c.Execute(context.Background(), map[string]any{
		"action": "set",
		"key":    "model",
		"value":  "claude-opus-4",
	})
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "updated")

	// Get it back.
	result = c.Execute(context.Background(), map[string]any{
		"action": "get",
		"key":    "model",
	})
	assert.False(t, result.IsError)

	var resp map[string]string
	err := json.Unmarshal([]byte(result.Content), &resp)
	require.NoError(t, err)
	assert.Equal(t, "claude-opus-4", resp["value"])
}

func TestConfigToolGetUnset(t *testing.T) {
	c := NewConfigTool()

	result := c.Execute(context.Background(), map[string]any{
		"action": "get",
		"key":    "theme",
	})
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "theme")
}

func TestConfigToolUnsupportedKey(t *testing.T) {
	c := NewConfigTool()

	result := c.Execute(context.Background(), map[string]any{
		"action": "get",
		"key":    "secret_password",
	})
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "unsupported key")
}

func TestConfigToolSetMissingValue(t *testing.T) {
	c := NewConfigTool()

	result := c.Execute(context.Background(), map[string]any{
		"action": "set",
		"key":    "theme",
	})
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "value is required")
}

func TestConfigToolInvalidAction(t *testing.T) {
	c := NewConfigTool()

	result := c.Execute(context.Background(), map[string]any{
		"action": "delete",
		"key":    "model",
	})
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid action")
}

func TestConfigToolProgrammaticAccess(t *testing.T) {
	c := NewConfigTool()

	c.SetValue("effort", "high")
	val, ok := c.GetValue("effort")
	assert.True(t, ok)
	assert.Equal(t, "high", val)

	_, ok = c.GetValue("missing")
	assert.False(t, ok)
}
