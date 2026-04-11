package direct

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenRouterCompactBoundaryBasic(t *testing.T) {
	t.Parallel()

	history := []openrouterHistoryEntry{
		{Role: "user", Content: "user-0"},
		{Role: "assistant", Content: "assistant-1"},
		{
			Role:    "assistant",
			Content: "",
			ToolCalls: []openrouterToolCallMsg{{
				ID:   "call_1",
				Type: "function",
				Function: openrouterToolCallFuncMsg{
					Name:      "Read",
					Arguments: `{"path":"a.txt"}`,
				},
			}},
		},
		{Role: "tool", CallID: "call_1", Content: "tool-1"},
		{Role: "user", Content: "user-4"},
		{Role: "assistant", Content: "assistant-5"},
		{
			Role:    "assistant",
			Content: "",
			ToolCalls: []openrouterToolCallMsg{{
				ID:   "call_2",
				Type: "function",
				Function: openrouterToolCallFuncMsg{
					Name:      "Read",
					Arguments: `{"path":"b.txt"}`,
				},
			}},
		},
		{Role: "tool", CallID: "call_2", Content: "tool-2"},
		{Role: "user", Content: "user-8"},
		{Role: "assistant", Content: "assistant-9"},
	}

	assert.Equal(t, 8, findOpenRouterCompactionBoundary(history))
}

func TestOpenRouterCompactSerialize(t *testing.T) {
	t.Parallel()

	history := []openrouterHistoryEntry{
		{Role: "user", Content: "inspect src"},
		{
			Role:    "assistant",
			Content: "I will read a file",
			ToolCalls: []openrouterToolCallMsg{{
				ID:   "call_1",
				Type: "function",
				Function: openrouterToolCallFuncMsg{
					Name:      "Read",
					Arguments: `{"file_path":"main.go"}`,
				},
			}},
		},
		{Role: "tool", CallID: "call_1", Content: "package main"},
		{Role: "user", Content: "continue"},
		{Role: "assistant", Content: "done"},
	}

	provider := newOpenRouterCompactProvider(&history, "test-key", "anthropic/claude-sonnet-4-5")
	transcript, cutIndex, err := provider.Serialize(60000)

	require.NoError(t, err)
	assert.Equal(t, 3, cutIndex)
	assert.Contains(t, transcript, "USER:")
	assert.Contains(t, transcript, "ASSISTANT:")
	assert.Contains(t, transcript, "[TOOL_CALL")
	assert.Contains(t, transcript, "[TOOL_RESULT")
}

func TestOpenRouterCompactReplace(t *testing.T) {
	t.Parallel()

	history := []openrouterHistoryEntry{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "second"},
		{
			Role:    "assistant",
			Content: "",
			ToolCalls: []openrouterToolCallMsg{{
				ID:   "call_1",
				Type: "function",
				Function: openrouterToolCallFuncMsg{
					Name:      "Read",
					Arguments: `{"file_path":"main.go"}`,
				},
			}},
		},
		{Role: "tool", CallID: "call_1", Content: "package main"},
		{Role: "assistant", Content: "tail"},
	}

	provider := newOpenRouterCompactProvider(&history, "test-key", "anthropic/claude-sonnet-4-5")
	err := provider.Replace("compressed summary", 3)

	require.NoError(t, err)
	require.Len(t, history, 3)
	assert.Equal(t, "user", history[0].Role)
	assert.Contains(t, history[0].Content, "<context-summary>")
	assert.Equal(t, "tool", history[1].Role)
	assert.Equal(t, "assistant", history[2].Role)
}
