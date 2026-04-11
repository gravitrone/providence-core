package direct

import (
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/require"
)

func TestFindSafeCompactionBoundaryEmpty(t *testing.T) {
	t.Parallel()

	require.Zero(t, findSafeCompactionBoundary(nil))
}

func TestFindSafeCompactionBoundaryBasic(t *testing.T) {
	t.Parallel()

	h := NewConversationHistory()
	for i := range 10 {
		h.AddUser("user message " + truncate("abcdefghij", i+1))
	}

	msgs := h.Messages()
	idx := findSafeCompactionBoundary(msgs)

	require.Greater(t, idx, 0)
	require.Less(t, idx, len(msgs))
}

func TestMessageHasToolResultDetection(t *testing.T) {
	t.Parallel()

	require.False(t, messageHasToolResult(anthropic.NewUserMessage(anthropic.NewTextBlock("plain text"))))
	require.True(t, messageHasToolResult(anthropic.NewUserMessage(
		anthropic.NewToolResultBlock("tool_1", "tool output", false),
	)))
}

func TestAnthropicProviderSerialize(t *testing.T) {
	t.Parallel()

	h := NewConversationHistory()
	for i := range 10 {
		h.AddUser("message " + truncate("abcdefghij", i+1))
	}

	provider := newAnthropicCompactProvider(h, anthropic.Client{}, "claude-sonnet-4-6")
	transcript, cutIdx, err := provider.Serialize(60000)

	require.NoError(t, err)
	require.Greater(t, cutIdx, 0)
	require.Contains(t, transcript, "USER")
}

func TestAnthropicProviderReplace(t *testing.T) {
	t.Parallel()

	h := NewConversationHistory()
	for i := range 6 {
		h.AddUser("message " + truncate("abcdefghij", i+1))
	}

	provider := newAnthropicCompactProvider(h, anthropic.Client{}, "claude-sonnet-4-6")
	err := provider.Replace("compressed summary", 4)
	require.NoError(t, err)

	msgs := h.Messages()
	require.Len(t, msgs, 3)
	require.Len(t, msgs[0].Content, 1)
	require.NotNil(t, msgs[0].Content[0].OfText)
	require.True(t, strings.Contains(msgs[0].Content[0].OfText.Text, "<context-summary>"))
}

func TestTruncate(t *testing.T) {
	t.Parallel()

	require.Equal(t, "abc", truncate("abc", 10))
	require.Equal(t, "abcde...", truncate("abcdef", 5))
}
