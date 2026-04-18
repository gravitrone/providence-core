package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/gravitrone/providence-core/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, s.Close())
	})
	return s
}

func TestSessionReadCurrentSession(t *testing.T) {
	st := testStore(t)
	require.NoError(t, st.CreateSession("s1", "/tmp", "direct", "sonnet"))
	_, err := st.AddMessage("s1", "user", "build the thing", "", "", "", "", "", 0, true)
	require.NoError(t, err)
	_, err = st.AddMessage("s1", "assistant", "on it", "", "", "", "", "", 0, true)
	require.NoError(t, err)

	tool := NewSessionReadTool(st, "s1")
	res := tool.Execute(context.Background(), map[string]any{})
	require.False(t, res.IsError, res.Content)

	var msgs []sessionMessage
	require.NoError(t, json.Unmarshal([]byte(res.Content), &msgs))
	assert.Len(t, msgs, 2)
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "build the thing", msgs[0].Content)
	assert.Equal(t, "assistant", msgs[1].Role)
}

func TestSessionReadPastSession(t *testing.T) {
	st := testStore(t)
	require.NoError(t, st.CreateSession("s1", "/tmp", "direct", "sonnet"))
	require.NoError(t, st.CreateSession("s2", "/tmp", "direct", "opus"))
	_, err := st.AddMessage("s1", "user", "old message", "", "", "", "", "", 0, true)
	require.NoError(t, err)
	_, err = st.AddMessage("s2", "user", "new message", "", "", "", "", "", 0, true)
	require.NoError(t, err)

	// Tool bound to s2, but we read s1 explicitly.
	tool := NewSessionReadTool(st, "s2")
	res := tool.Execute(context.Background(), map[string]any{
		"session_id": "s1",
	})
	require.False(t, res.IsError, res.Content)

	var msgs []sessionMessage
	require.NoError(t, json.Unmarshal([]byte(res.Content), &msgs))
	require.Len(t, msgs, 1)
	assert.Equal(t, "old message", msgs[0].Content)
}

func TestSessionReadOffsetAndLimit(t *testing.T) {
	st := testStore(t)
	require.NoError(t, st.CreateSession("s1", "/tmp", "direct", "sonnet"))
	for i := range 10 {
		_, err := st.AddMessage("s1", "user", "msg"+string(rune('0'+i)), "", "", "", "", "", 0, true)
		require.NoError(t, err)
	}

	tool := NewSessionReadTool(st, "s1")
	res := tool.Execute(context.Background(), map[string]any{
		"offset": float64(2),
		"limit":  float64(3),
	})
	require.False(t, res.IsError, res.Content)

	var msgs []sessionMessage
	require.NoError(t, json.Unmarshal([]byte(res.Content), &msgs))
	assert.Len(t, msgs, 3)
	assert.Equal(t, "msg2", msgs[0].Content)
}

func TestSessionReadNoStore(t *testing.T) {
	tool := NewSessionReadTool(nil, "s1")
	res := tool.Execute(context.Background(), map[string]any{})
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "not available")
}

func TestSessionReadNoSession(t *testing.T) {
	st := testStore(t)
	tool := NewSessionReadTool(st, "")
	res := tool.Execute(context.Background(), map[string]any{})
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "no session_id")
}
