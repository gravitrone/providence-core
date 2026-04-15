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

// TestCodexCompactEmptyNoOp verifies empty history is a no-op for both
// boundary computation and Serialize.
func TestCodexCompactEmptyNoOp(t *testing.T) {
	t.Parallel()

	history := []codexHistoryEntry{}
	assert.Equal(t, 0, findCodexCompactionBoundary(history))

	provider := newCodexCompactProvider(&history, "gpt-5.4")
	transcript, cutIdx, err := provider.Serialize(60000)
	require.NoError(t, err)
	assert.Equal(t, 0, cutIdx)
	assert.Equal(t, "", transcript)
}

// TestCodexReplacePreservesCallIDChain verifies Replace keeps the tail's
// function_call / tool CallID linkage intact after compaction (Codex-specific
// response chain invariant).
func TestCodexReplacePreservesCallIDChain(t *testing.T) {
	t.Parallel()

	history := []codexHistoryEntry{
		{Role: "user", Content: "drop"},
		{Role: "assistant", Content: "drop"},
		{Role: "function_call", CallID: "call_keep", FuncName: "Read", Content: `{"p":"x"}`},
		{Role: "tool", CallID: "call_keep", Content: "file body"},
		{Role: "assistant", Content: "follow up"},
	}

	provider := newCodexCompactProvider(&history, "gpt-5.4")
	require.NoError(t, provider.Replace("summary", 2))

	// After Replace: [summary, function_call(call_keep), tool(call_keep), assistant]
	require.Len(t, history, 4)
	assert.Equal(t, "user", history[0].Role)
	assert.Contains(t, history[0].Content, "<context-summary>")
	assert.Equal(t, "function_call", history[1].Role)
	assert.Equal(t, "call_keep", history[1].CallID)
	assert.Equal(t, "tool", history[2].Role)
	assert.Equal(t, "call_keep", history[2].CallID, "tool CallID must remain paired with its function_call")
	assert.Equal(t, "assistant", history[3].Role)
}

// TestCodexReplaceOutOfRangeRejected verifies bounds checking on cutIndex.
func TestCodexReplaceOutOfRangeRejected(t *testing.T) {
	t.Parallel()

	history := []codexHistoryEntry{
		{Role: "user", Content: "a"},
		{Role: "assistant", Content: "b"},
	}
	provider := newCodexCompactProvider(&history, "gpt-5.4")

	require.Error(t, provider.Replace("s", 0))
	require.Error(t, provider.Replace("s", 999))
	// Negative-valued-ish: 0 covered. History untouched.
	require.Len(t, history, 2)
}
