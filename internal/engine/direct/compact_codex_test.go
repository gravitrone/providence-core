package direct

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodexCompactBoundaryBasic(t *testing.T) {
	t.Parallel()

	history := []codexHistoryEntry{
		{Role: "user", Content: "user-0"},
		{Role: "assistant", Content: "assistant-1"},
		{Role: "function_call", CallID: "call_1", FuncName: "Read", Content: `{"path":"a.txt"}`},
		{Role: "tool", CallID: "call_1", Content: "tool-1"},
		{Role: "user", Content: "user-4"},
		{Role: "assistant", Content: "assistant-5"},
		{Role: "function_call", CallID: "call_2", FuncName: "Read", Content: `{"path":"b.txt"}`},
		{Role: "tool", CallID: "call_2", Content: "tool-2"},
		{Role: "user", Content: "user-8"},
		{Role: "assistant", Content: "assistant-9"},
	}

	assert.Equal(t, 8, findCodexCompactionBoundary(history))
}

func TestCodexCompactSerialize(t *testing.T) {
	t.Parallel()

	history := []codexHistoryEntry{
		{Role: "user", Content: "inspect src"},
		{Role: "assistant", Content: "I will read a file"},
		{Role: "function_call", CallID: "call_1", FuncName: "Read", Content: `{"file_path":"main.go"}`},
		{Role: "tool", CallID: "call_1", Content: "package main"},
		{Role: "user", Content: "continue"},
		{Role: "assistant", Content: "done"},
	}

	provider := newCodexCompactProvider(&history, "gpt-5.4")
	transcript, cutIndex, err := provider.Serialize(60000)

	require.NoError(t, err)
	assert.Equal(t, 4, cutIndex)
	assert.Contains(t, transcript, "USER:")
	assert.Contains(t, transcript, "ASSISTANT:")
	assert.Contains(t, transcript, "[FUNCTION_CALL")
	assert.Contains(t, transcript, "[FUNCTION_CALL_OUTPUT")
}

func TestCodexCompactReplace(t *testing.T) {
	t.Parallel()

	history := []codexHistoryEntry{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "second"},
		{Role: "function_call", CallID: "call_1", FuncName: "Read", Content: `{"file_path":"main.go"}`},
		{Role: "tool", CallID: "call_1", Content: "package main"},
		{Role: "assistant", Content: "tail"},
	}

	provider := newCodexCompactProvider(&history, "gpt-5.4")
	err := provider.Replace("compressed summary", 3)

	require.NoError(t, err)
	require.Len(t, history, 3)
	assert.Equal(t, "user", history[0].Role)
	assert.Contains(t, history[0].Content, "<context-summary>")
	assert.Equal(t, "tool", history[1].Role)
	assert.Equal(t, "assistant", history[2].Role)
}
