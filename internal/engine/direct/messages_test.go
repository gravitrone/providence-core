package direct

import (
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

func TestConversationHistory_AddAssistantText(t *testing.T) {
	h := NewConversationHistory()
	h.AddAssistantText("hi there")
	msgs := h.Messages()
	require.Len(t, msgs, 1)
	assert.Equal(t, anthropic.MessageParamRoleAssistant, msgs[0].Role)
	require.Len(t, msgs[0].Content, 1)
	assert.Equal(t, "hi there", msgs[0].Content[0].OfText.Text)
}
