package direct

import (
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/require"
)

func TestFindSafeCompactionBoundaryEmpty(t *testing.T) {
	t.Parallel()

	require.Zero(t, findSafeCompactionBoundary(nil, 0))
}

func TestFindSafeCompactionBoundaryBasic(t *testing.T) {
	t.Parallel()

	h := NewConversationHistory()
	for i := range 10 {
		h.AddUser("user message " + truncate("abcdefghij", i+1))
	}

	msgs := h.Messages()
	idx := findSafeCompactionBoundary(msgs, 0)

	require.Greater(t, idx, 0)
	require.Less(t, idx, len(msgs))
}

// TestFindSafeCompactionBoundaryTokenBudget verifies the keepRecentTokens
// arg drives a token-accumulated tail cut instead of the 70% fallback. With
// a budget smaller than a single message's estimate the cut lands on the
// last index; with a budget larger than the whole history the cut is zero.
func TestFindSafeCompactionBoundaryTokenBudget(t *testing.T) {
	t.Parallel()

	h := NewConversationHistory()
	for i := 0; i < 6; i++ {
		h.AddUser(strings.Repeat("x", 120))
	}
	msgs := h.Messages()

	// Each msg ~120 chars -> ~160 estimated tokens. Budget=1 means the
	// very last message alone satisfies the floor so cut sits at n-1.
	require.Equal(t, len(msgs)-1, findSafeCompactionBoundary(msgs, 1))

	// Budget larger than all message tokens combined -> nothing to compact.
	require.Zero(t, findSafeCompactionBoundary(msgs, 100000))
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
	// budget=0 -> legacy 70% cut path so the assertion remains stable
	// independent of per-message token estimates.
	transcript, cutIdx, err := provider.Serialize(0)

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

// TestAnthropicSerializeEmptyNoOp verifies Serialize on empty history is a
// no-op (empty transcript, zero cut index, no error).
func TestAnthropicSerializeEmptyNoOp(t *testing.T) {
	t.Parallel()

	h := NewConversationHistory()
	provider := newAnthropicCompactProvider(h, anthropic.Client{}, "claude-sonnet-4-6")

	transcript, cutIdx, err := provider.Serialize(60000)
	require.NoError(t, err)
	require.Equal(t, 0, cutIdx)
	require.Equal(t, "", transcript)
}

// TestAnthropicBoundaryAvoidsToolResultOrphan verifies the boundary advances
// past messages carrying tool_result blocks so a tool_result is never severed
// from its originating tool_use in the tail.
func TestAnthropicBoundaryAvoidsToolResultOrphan(t *testing.T) {
	t.Parallel()

	// Directly build a 10-msg slice with a tool_result at the 70% boundary
	// (index 7) to exercise the advance-past-tool-result branch.
	crafted := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("u0")),
		anthropic.NewUserMessage(anthropic.NewTextBlock("u1")),
		anthropic.NewUserMessage(anthropic.NewTextBlock("u2")),
		anthropic.NewUserMessage(anthropic.NewTextBlock("u3")),
		anthropic.NewUserMessage(anthropic.NewTextBlock("u4")),
		anthropic.NewUserMessage(anthropic.NewTextBlock("u5")),
		anthropic.NewUserMessage(anthropic.NewTextBlock("u6")),
		anthropic.NewUserMessage(anthropic.NewToolResultBlock("t1", "tool result", false)),
		anthropic.NewUserMessage(anthropic.NewTextBlock("u8")),
		anthropic.NewUserMessage(anthropic.NewTextBlock("u9")),
	}
	idx := findSafeCompactionBoundary(crafted, 0)
	// 10 * 70 / 100 = 7, but index 7 is a tool_result -> must advance to 8.
	require.Equal(t, 8, idx)
	require.False(t, messageHasToolResult(crafted[idx]))
}

// TestAnthropicReplaceOutOfRangeRejected verifies Replace returns an error
// for an invalid cut index (preserves history-integrity invariant).
func TestAnthropicReplaceOutOfRangeRejected(t *testing.T) {
	t.Parallel()

	h := NewConversationHistory()
	for i := 0; i < 3; i++ {
		h.AddUser("m" + truncate("abc", i+1))
	}
	provider := newAnthropicCompactProvider(h, anthropic.Client{}, "claude-sonnet-4-6")

	// Zero index: rejected.
	require.Error(t, provider.Replace("summary", 0))
	// Past end: rejected.
	require.Error(t, provider.Replace("summary", 999))
	// History untouched.
	require.Len(t, h.Messages(), 3)
}
