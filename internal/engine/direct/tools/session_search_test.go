package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionSearchFindsMessages(t *testing.T) {
	st := testStore(t)
	require.NoError(t, st.CreateSession("s1", "/tmp", "direct", "sonnet"))
	require.NoError(t, st.CreateSession("s2", "/tmp", "direct", "opus"))

	st.AddMessage("s1", "user", "build the providence core engine", "", "", "", "", "", 0, true)
	st.AddMessage("s1", "assistant", "starting the engine build now", "", "", "", "", "", 0, true)
	st.AddMessage("s2", "user", "fix the database migration", "", "", "", "", "", 0, true)

	tool := NewSessionSearchTool(st)
	res := tool.Execute(context.Background(), map[string]any{
		"query": "engine",
	})
	require.False(t, res.IsError, res.Content)

	var results []searchResultJSON
	require.NoError(t, json.Unmarshal([]byte(res.Content), &results))
	assert.Len(t, results, 2)

	// Both results should be from s1.
	for _, r := range results {
		assert.Equal(t, "s1", r.SessionID)
		assert.Contains(t, r.Snippet, "engine")
	}
}

func TestSessionSearchNoResults(t *testing.T) {
	st := testStore(t)
	require.NoError(t, st.CreateSession("s1", "/tmp", "direct", "sonnet"))
	st.AddMessage("s1", "user", "hello world", "", "", "", "", "", 0, true)

	tool := NewSessionSearchTool(st)
	res := tool.Execute(context.Background(), map[string]any{
		"query": "nonexistenttermxyz",
	})
	require.False(t, res.IsError, res.Content)
	assert.Equal(t, "[]", res.Content)
}

func TestSessionSearchNoStore(t *testing.T) {
	tool := NewSessionSearchTool(nil)
	res := tool.Execute(context.Background(), map[string]any{
		"query": "test",
	})
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "not available")
}

func TestSessionSearchEmptyQuery(t *testing.T) {
	st := testStore(t)
	tool := NewSessionSearchTool(st)
	res := tool.Execute(context.Background(), map[string]any{})
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "required")
}

func TestSessionSearchLimit(t *testing.T) {
	st := testStore(t)
	require.NoError(t, st.CreateSession("s1", "/tmp", "direct", "sonnet"))
	for range 20 {
		st.AddMessage("s1", "user", "the flame burns bright", "", "", "", "", "", 0, true)
	}

	tool := NewSessionSearchTool(st)
	res := tool.Execute(context.Background(), map[string]any{
		"query": "flame",
		"limit": float64(5),
	})
	require.False(t, res.IsError, res.Content)

	var results []searchResultJSON
	require.NoError(t, json.Unmarshal([]byte(res.Content), &results))
	assert.Len(t, results, 5)
}
