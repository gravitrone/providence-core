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

func TestToolSearchSelectMultipleExact(t *testing.T) {
	reg := NewRegistry(SleepTool{}, &GlobTool{}, &GrepTool{})
	ts := NewToolSearchTool(reg)

	result := ts.Execute(context.Background(), map[string]any{
		"query": "select:Sleep,Glob,Grep",
	})

	require.False(t, result.IsError, result.Content)
	assert.Contains(t, result.Content, "Sleep")
	assert.Contains(t, result.Content, "Glob")
	assert.Contains(t, result.Content, "Grep")
}

func TestToolSearchSelectWithWhitespace(t *testing.T) {
	reg := NewRegistry(SleepTool{}, &GlobTool{})
	ts := NewToolSearchTool(reg)

	// Names with extra spaces should still work.
	result := ts.Execute(context.Background(), map[string]any{
		"query": "select: Sleep , Glob ",
	})

	require.False(t, result.IsError, result.Content)
	assert.Contains(t, result.Content, "Sleep")
	assert.Contains(t, result.Content, "Glob")
}

func TestToolSearchKeywordScoresNameHigherThanDesc(t *testing.T) {
	reg := NewRegistry(SleepTool{}, &GlobTool{}, &GrepTool{})
	ts := NewToolSearchTool(reg)

	// "glob" exactly matches the tool name - should be first result.
	result := ts.Execute(context.Background(), map[string]any{
		"query":       "glob",
		"max_results": 1,
	})

	require.False(t, result.IsError, result.Content)
	assert.Contains(t, result.Content, "Glob")
}

func TestToolSearchMaxResultsClamped(t *testing.T) {
	reg := NewRegistry(SleepTool{}, &GlobTool{}, &GrepTool{})
	ts := NewToolSearchTool(reg)

	// max_results < 1 should be clamped to 1.
	result := ts.Execute(context.Background(), map[string]any{
		"query":       "select:Sleep",
		"max_results": 0,
	})
	// select: mode doesn't use max_results, but this should still not error.
	require.False(t, result.IsError, result.Content)
}

func TestToolSearchInvalidQueryNoParam(t *testing.T) {
	reg := NewRegistry(SleepTool{})
	ts := NewToolSearchTool(reg)

	// Missing query parameter entirely.
	result := ts.Execute(context.Background(), map[string]any{})
	assert.True(t, result.IsError)
}

func TestToolSearchReadOnlyField(t *testing.T) {
	reg := NewRegistry(SleepTool{})
	ts := NewToolSearchTool(reg)

	result := ts.Execute(context.Background(), map[string]any{
		"query": "select:Sleep",
	})

	require.False(t, result.IsError, result.Content)
	assert.Contains(t, result.Content, "\"read_only\"")
}

func TestToolSearchSelectPartialNotFoundReturnsFound(t *testing.T) {
	reg := NewRegistry(SleepTool{}, &GlobTool{})
	ts := NewToolSearchTool(reg)

	// One valid, one invalid name.
	result := ts.Execute(context.Background(), map[string]any{
		"query": "select:Sleep,DoesNotExist",
	})

	// Should return the one that was found, not error.
	require.False(t, result.IsError, result.Content)
	assert.Contains(t, result.Content, "Sleep")
	assert.NotContains(t, result.Content, "DoesNotExist")
}

func TestToolSearchMultiWordKeyword(t *testing.T) {
	reg := NewRegistry(SleepTool{}, &GlobTool{}, &GrepTool{})
	ts := NewToolSearchTool(reg)

	// Multi-word queries should match tools containing any of the terms.
	result := ts.Execute(context.Background(), map[string]any{
		"query":       "sleep file",
		"max_results": 10,
	})

	// Sleep should match by name. File-related tools might match by description.
	require.False(t, result.IsError, result.Content)
	assert.Contains(t, result.Content, "Sleep")
}
