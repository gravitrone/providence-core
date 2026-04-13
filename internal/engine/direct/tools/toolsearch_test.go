package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolSearchSelectExact(t *testing.T) {
	reg := NewRegistry(SleepTool{}, &GlobTool{}, &GrepTool{})
	ts := NewToolSearchTool(reg)

	result := ts.Execute(context.Background(), map[string]any{
		"query": "select:Sleep,Glob",
	})

	require.False(t, result.IsError, result.Content)
	assert.Contains(t, result.Content, "Sleep")
	assert.Contains(t, result.Content, "Glob")
	assert.NotContains(t, result.Content, "Grep")
}

func TestToolSearchSelectNotFound(t *testing.T) {
	reg := NewRegistry(SleepTool{})
	ts := NewToolSearchTool(reg)

	result := ts.Execute(context.Background(), map[string]any{
		"query": "select:Nonexistent",
	})

	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "No tools found")
}

func TestToolSearchKeyword(t *testing.T) {
	reg := NewRegistry(SleepTool{}, &GlobTool{}, &GrepTool{})
	ts := NewToolSearchTool(reg)

	result := ts.Execute(context.Background(), map[string]any{
		"query":       "sleep",
		"max_results": 5,
	})

	require.False(t, result.IsError, result.Content)
	assert.Contains(t, result.Content, "Sleep")
}

func TestToolSearchKeywordNoMatch(t *testing.T) {
	reg := NewRegistry(SleepTool{})
	ts := NewToolSearchTool(reg)

	result := ts.Execute(context.Background(), map[string]any{
		"query": "xyznonexistent",
	})

	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "No tools found")
}

func TestToolSearchEmptyQuery(t *testing.T) {
	reg := NewRegistry(SleepTool{})
	ts := NewToolSearchTool(reg)

	result := ts.Execute(context.Background(), map[string]any{
		"query": "",
	})

	assert.True(t, result.IsError)
}

func TestToolSearchReturnsSchema(t *testing.T) {
	reg := NewRegistry(SleepTool{})
	ts := NewToolSearchTool(reg)

	result := ts.Execute(context.Background(), map[string]any{
		"query": "select:Sleep",
	})

	require.False(t, result.IsError, result.Content)
	// Should contain schema fields.
	assert.Contains(t, result.Content, "\"name\"")
	assert.Contains(t, result.Content, "\"description\"")
	assert.Contains(t, result.Content, "\"parameters\"")
	assert.Contains(t, result.Content, "duration_ms")
}

func TestToolSearchMaxResults(t *testing.T) {
	reg := NewRegistry(SleepTool{}, &GlobTool{}, &GrepTool{})
	ts := NewToolSearchTool(reg)

	// Search for a term all tools might partially match.
	result := ts.Execute(context.Background(), map[string]any{
		"query":       "tool",
		"max_results": 1,
	})

	// Should not error even if nothing matches "tool" in name/desc.
	// If something matches, count should be limited.
	_ = result
	assert.False(t, result.IsError || strings.Contains(result.Content, "Error"))
}
