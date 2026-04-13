package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStructuredOutputMetadata(t *testing.T) {
	s := NewStructuredOutputTool()
	assert.Equal(t, "StructuredOutput", s.Name())
	assert.False(t, s.ReadOnly())
	assert.NotEmpty(t, s.Description())

	schema := s.InputSchema()
	require.NotNil(t, schema)
	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	_, hasSchema := props["schema"]
	_, hasData := props["data"]
	assert.True(t, hasSchema)
	assert.True(t, hasData)
}

func TestStructuredOutputValid(t *testing.T) {
	s := NewStructuredOutputTool()

	result := s.Execute(context.Background(), map[string]any{
		"schema": map[string]any{
			"type": "object",
			"required": []any{"name", "version"},
		},
		"data": map[string]any{
			"name":    "providence",
			"version": "1.0.0",
		},
	})

	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "stored")
	assert.NotNil(t, result.Metadata)
	assert.NotNil(t, result.Metadata["structured_output"])

	// Verify stored result.
	last := s.LastResult()
	require.NotNil(t, last)
	assert.Equal(t, "providence", last["name"])
}

func TestStructuredOutputMissingRequired(t *testing.T) {
	s := NewStructuredOutputTool()

	result := s.Execute(context.Background(), map[string]any{
		"schema": map[string]any{
			"type": "object",
			"required": []any{"name", "version"},
		},
		"data": map[string]any{
			"name": "providence",
		},
	})

	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "missing required field: version")
}

func TestStructuredOutputNoSchema(t *testing.T) {
	s := NewStructuredOutputTool()

	result := s.Execute(context.Background(), map[string]any{
		"data": map[string]any{"foo": "bar"},
	})

	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "schema is required")
}

func TestStructuredOutputNoData(t *testing.T) {
	s := NewStructuredOutputTool()

	result := s.Execute(context.Background(), map[string]any{
		"schema": map[string]any{"type": "object"},
	})

	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "data is required")
}

func TestStructuredOutputNoRequired(t *testing.T) {
	s := NewStructuredOutputTool()

	result := s.Execute(context.Background(), map[string]any{
		"schema": map[string]any{
			"type": "object",
		},
		"data": map[string]any{
			"anything": "goes",
		},
	})

	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "stored")
}

func TestStructuredOutputLastResultNil(t *testing.T) {
	s := NewStructuredOutputTool()
	assert.Nil(t, s.LastResult())
}

func TestStructuredOutputOverwrites(t *testing.T) {
	s := NewStructuredOutputTool()

	s.Execute(context.Background(), map[string]any{
		"schema": map[string]any{"type": "object"},
		"data":   map[string]any{"v": "1"},
	})
	s.Execute(context.Background(), map[string]any{
		"schema": map[string]any{"type": "object"},
		"data":   map[string]any{"v": "2"},
	})

	last := s.LastResult()
	require.NotNil(t, last)
	assert.Equal(t, "2", last["v"])
}
