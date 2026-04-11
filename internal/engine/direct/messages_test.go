package direct

import (
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConversationHistory_AddUser(t *testing.T) {
	h := NewConversationHistory()
	h.AddUser("hello")
	msgs := h.Messages()
	require.Len(t, msgs, 1)
	assert.Equal(t, anthropic.MessageParamRoleUser, msgs[0].Role)
	require.Len(t, msgs[0].Content, 1)
	assert.Equal(t, "hello", msgs[0].Content[0].OfText.Text)
}

func TestConversationHistory_AddToolResults(t *testing.T) {
	h := NewConversationHistory()
	h.AddToolResults([]anthropic.ContentBlockParamUnion{
		anthropic.NewToolResultBlock("tool_1", "result text", false),
	})
	msgs := h.Messages()
	require.Len(t, msgs, 1)
	assert.Equal(t, anthropic.MessageParamRoleUser, msgs[0].Role)
	require.Len(t, msgs[0].Content, 1)
	assert.NotNil(t, msgs[0].Content[0].OfToolResult)
	assert.Equal(t, "tool_1", msgs[0].Content[0].OfToolResult.ToolUseID)
}

func TestConversationHistory_MessagesReturnsCopy(t *testing.T) {
	h := NewConversationHistory()
	h.AddUser("one")
	msgs1 := h.Messages()
	h.AddUser("two")
	msgs2 := h.Messages()
	assert.Len(t, msgs1, 1, "original slice should not be affected")
	assert.Len(t, msgs2, 2)
}

func TestConversationHistory_EstimateTokens(t *testing.T) {
	h := NewConversationHistory()
	// 12 chars = "hello world!" -> 12 * 4 / 3 = 16
	h.AddUser("hello world!")
	assert.Equal(t, 16, h.EstimateTokens())
}

func TestConversationHistory_EstimateTokensEmpty(t *testing.T) {
	h := NewConversationHistory()
	assert.Equal(t, 0, h.EstimateTokens())
}

func TestCurrentTokensFallbackToEstimate(t *testing.T) {
	h := NewConversationHistory()
	h.AddUser("hello world!")

	assert.Equal(t, 16, h.CurrentTokens())
}

func TestCurrentTokensFromReported(t *testing.T) {
	h := NewConversationHistory()
	h.AddUser("hello world!")
	h.SetReportedTokens(11, 4)

	assert.Equal(t, 15, h.CurrentTokens())
}

func TestSetReportedTokens(t *testing.T) {
	h := NewConversationHistory()
	h.SetReportedTokens(12, 8)

	assert.Equal(t, 12, h.lastInputTokens)
	assert.Equal(t, 8, h.lastOutputTokens)
	assert.Equal(t, 20, h.lastReportedTokens)
}

func TestConversationHistory_AddAssistantText(t *testing.T) {
	h := NewConversationHistory()
	h.AddAssistantText("hi there")
	msgs := h.Messages()
	require.Len(t, msgs, 1)
	assert.Equal(t, anthropic.MessageParamRoleAssistant, msgs[0].Role)
	require.Len(t, msgs[0].Content, 1)
	assert.Equal(t, "hi there", msgs[0].Content[0].OfText.Text)
}

func TestCompressLongToolResults(t *testing.T) {
	longResult := strings.Repeat("x", 5000)
	shortResult := strings.Repeat("y", 300)

	h := NewConversationHistory()
	h.AddUser("user-0")
	h.AddToolResults([]anthropic.ContentBlockParamUnion{
		anthropic.NewToolResultBlock("tool_old_long", longResult, false),
	})
	h.AddToolResults([]anthropic.ContentBlockParamUnion{
		anthropic.NewToolResultBlock("tool_old_short", shortResult, false),
	})
	h.AddAssistantText("assistant-3")
	h.AddToolResults([]anthropic.ContentBlockParamUnion{
		anthropic.NewToolResultBlock("tool_recent_long", longResult, false),
	})
	h.AddUser("user-5")
	h.AddAssistantText("assistant-6")
	h.SetReportedTokens(12, 8)

	compressed := h.CompressLongToolResults(2000)
	require.Equal(t, 1, compressed)

	msgs := h.Messages()
	require.Len(t, msgs, 7)

	oldLong := msgs[1].Content[0].OfToolResult
	require.NotNil(t, oldLong)
	require.Len(t, oldLong.Content, 1)
	require.NotNil(t, oldLong.Content[0].OfText)
	assert.Equal(t, "[compressed: 5000 chars from tool_use_id=tool_old_long]", oldLong.Content[0].OfText.Text)

	oldShort := msgs[2].Content[0].OfToolResult
	require.NotNil(t, oldShort)
	require.Len(t, oldShort.Content, 1)
	require.NotNil(t, oldShort.Content[0].OfText)
	assert.Equal(t, shortResult, oldShort.Content[0].OfText.Text)

	recentLong := msgs[4].Content[0].OfToolResult
	require.NotNil(t, recentLong)
	require.Len(t, recentLong.Content, 1)
	require.NotNil(t, recentLong.Content[0].OfText)
	assert.Equal(t, longResult, recentLong.Content[0].OfText.Text)

	assert.Zero(t, h.lastInputTokens)
	assert.Zero(t, h.lastOutputTokens)
	assert.Zero(t, h.lastReportedTokens)
}
