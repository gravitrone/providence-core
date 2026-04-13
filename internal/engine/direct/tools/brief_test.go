package tools

import (
	"context"
	"testing"

	"github.com/gravitrone/providence-core/internal/engine/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBriefToolMetadata(t *testing.T) {
	b := NewBriefTool(nil)
	assert.Equal(t, "Brief", b.Name())
	assert.False(t, b.ReadOnly())
	assert.NotEmpty(t, b.Description())

	schema := b.InputSchema()
	require.NotNil(t, schema)
	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	_, hasMessage := props["message"]
	_, hasStatus := props["status"]
	assert.True(t, hasMessage)
	assert.True(t, hasStatus)
}

func TestBriefToolEmitsEvent(t *testing.T) {
	bus := session.NewBus()
	ch := bus.Subscribe(4)

	b := NewBriefTool(bus)
	result := b.Execute(context.Background(), map[string]any{
		"message": "build complete",
		"status":  "proactive",
	})

	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "delivered")

	// Verify event was published.
	select {
	case ev := <-ch:
		assert.Equal(t, "brief", ev.Type)
		data, ok := ev.Data.(map[string]string)
		require.True(t, ok)
		assert.Equal(t, "build complete", data["message"])
		assert.Equal(t, "proactive", data["status"])
	default:
		t.Fatal("expected event on bus, got none")
	}
}

func TestBriefToolNilBus(t *testing.T) {
	b := NewBriefTool(nil)
	result := b.Execute(context.Background(), map[string]any{
		"message": "test",
		"status":  "info",
	})

	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "delivered")
}

func TestBriefToolMissingMessage(t *testing.T) {
	b := NewBriefTool(nil)
	result := b.Execute(context.Background(), map[string]any{
		"status": "info",
	})

	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "message is required")
}

func TestBriefToolInvalidStatus(t *testing.T) {
	b := NewBriefTool(nil)
	result := b.Execute(context.Background(), map[string]any{
		"message": "test",
		"status":  "critical",
	})

	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid status")
}
