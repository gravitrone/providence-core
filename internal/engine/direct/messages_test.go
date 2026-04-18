package direct

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requireImageBlock(t *testing.T, block anthropic.ContentBlockParamUnion, want ImageData) {
	t.Helper()

	require.NotNil(t, block.OfImage)
	require.Nil(t, block.OfText)
	require.NotNil(t, block.OfImage.Source.OfBase64)
	assert.Equal(t, want.MediaType, string(block.OfImage.Source.OfBase64.MediaType))
	assert.Equal(t, base64.StdEncoding.EncodeToString(want.Data), block.OfImage.Source.OfBase64.Data)
}

func requireTextBlock(t *testing.T, block anthropic.ContentBlockParamUnion, want string) {
	t.Helper()

	require.NotNil(t, block.OfText)
	require.Nil(t, block.OfImage)
	assert.Equal(t, want, block.OfText.Text)
}

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
	h.counter = newTokenCounter(tokenCounterConfig{})
	h.AddUser("hello world!")

	assert.Equal(t, 16, h.CurrentTokens())
}

func TestCurrentTokensFromReported(t *testing.T) {
	h := NewConversationHistory()
	h.AddUser("hello world!")
	h.SetReportedTokens(11, 4)

	assert.Equal(t, 15, h.CurrentTokens())
}

func TestCurrentTokensInvalidatesAfterAppend(t *testing.T) {
	h := NewConversationHistory()
	h.counter = newTokenCounter(tokenCounterConfig{})
	h.AddUser("hello world!")
	h.SetReportedTokens(16, 0)
	require.Equal(t, 16, h.CurrentTokens())

	h.AddUser("more text")

	current := h.CurrentTokens()
	assert.Equal(t, 28, current)
	assert.Greater(t, current, 16)
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

func TestAddUserWithImages_ZeroImages(t *testing.T) {
	withImages := NewConversationHistory()
	withImages.AddUserWithImages("hi", nil)

	withText := NewConversationHistory()
	withText.AddUser("hi")

	withImagesMessages := withImages.Messages()
	withTextMessages := withText.Messages()

	require.Len(t, withImagesMessages, 1)
	require.Len(t, withTextMessages, 1)
	assert.Equal(t, anthropic.MessageParamRoleUser, withImagesMessages[0].Role)
	assert.Equal(t, withTextMessages[0].Role, withImagesMessages[0].Role)
	require.Len(t, withImagesMessages[0].Content, 1)
	requireTextBlock(t, withImagesMessages[0].Content[0], "hi")
	assert.Equal(t, withTextMessages[0].Content[0].OfText.Text, withImagesMessages[0].Content[0].OfText.Text)
}

func TestAddUserWithImages_SingleImage(t *testing.T) {
	h := NewConversationHistory()
	image := ImageData{MediaType: "image/png", Data: []byte{1, 2, 3}}

	h.AddUserWithImages("hi", []ImageData{image})

	msgs := h.Messages()
	require.Len(t, msgs, 1)
	assert.Equal(t, anthropic.MessageParamRoleUser, msgs[0].Role)
	require.Len(t, msgs[0].Content, 2)
	requireImageBlock(t, msgs[0].Content[0], image)
	requireTextBlock(t, msgs[0].Content[1], "hi")
}

func TestAddUserWithImages_MultipleImages(t *testing.T) {
	h := NewConversationHistory()
	images := []ImageData{
		{MediaType: "image/png", Data: []byte{1}},
		{MediaType: "image/png", Data: []byte{2}},
		{MediaType: "image/png", Data: []byte{3}},
	}

	h.AddUserWithImages("describe", images)

	msgs := h.Messages()
	require.Len(t, msgs, 1)
	require.Len(t, msgs[0].Content, 4)
	requireImageBlock(t, msgs[0].Content[0], images[0])
	requireImageBlock(t, msgs[0].Content[1], images[1])
	requireImageBlock(t, msgs[0].Content[2], images[2])
	requireTextBlock(t, msgs[0].Content[3], "describe")
}

func TestAddUserWithImages_EmptyText(t *testing.T) {
	h := NewConversationHistory()
	image := ImageData{MediaType: "image/png", Data: []byte{9, 8, 7}}

	h.AddUserWithImages("", []ImageData{image})

	msgs := h.Messages()
	require.Len(t, msgs, 1)
	require.Len(t, msgs[0].Content, 2)
	requireImageBlock(t, msgs[0].Content[0], image)
	requireTextBlock(t, msgs[0].Content[1], "")
}

func TestAddUserWithImages_NonPNGMediaType(t *testing.T) {
	h := NewConversationHistory()
	image := ImageData{MediaType: "image/jpeg", Data: []byte{4, 5, 6}}

	h.AddUserWithImages("jpeg", []ImageData{image})

	msgs := h.Messages()
	require.Len(t, msgs, 1)
	require.Len(t, msgs[0].Content, 2)
	requireImageBlock(t, msgs[0].Content[0], image)
	requireTextBlock(t, msgs[0].Content[1], "jpeg")
}

func TestAddUserWithImages_LargePayload(t *testing.T) {
	h := NewConversationHistory()
	payload := make([]byte, 1<<20)
	for i := range payload {
		payload[i] = byte(i % 251)
	}

	h.AddUserWithImages("large", []ImageData{{MediaType: "image/png", Data: payload}})

	msgs := h.Messages()
	require.Len(t, msgs, 1)
	require.Len(t, msgs[0].Content, 2)
	block := msgs[0].Content[0]
	require.NotNil(t, block.OfImage)
	require.NotNil(t, block.OfImage.Source.OfBase64)
	assert.Equal(t, base64.StdEncoding.EncodedLen(len(payload)), len(block.OfImage.Source.OfBase64.Data))

	decoded, err := base64.StdEncoding.DecodeString(block.OfImage.Source.OfBase64.Data)
	require.NoError(t, err)
	assert.Equal(t, payload, decoded)
	requireTextBlock(t, msgs[0].Content[1], "large")
}

func TestAddUserWithImages_HistoryGrowsBy_One_Per_Call(t *testing.T) {
	h := NewConversationHistory()

	h.AddUserWithImages("first", nil)
	h.AddUserWithImages("second", []ImageData{{MediaType: "image/png", Data: []byte{1}}})
	h.AddUserWithImages("third", []ImageData{
		{MediaType: "image/png", Data: []byte{2}},
		{MediaType: "image/jpeg", Data: []byte{3}},
	})

	msgs := h.Messages()
	require.Len(t, msgs, 3)
	assert.Equal(t, anthropic.MessageParamRoleUser, msgs[0].Role)
	assert.Equal(t, anthropic.MessageParamRoleUser, msgs[1].Role)
	assert.Equal(t, anthropic.MessageParamRoleUser, msgs[2].Role)
	requireTextBlock(t, msgs[0].Content[0], "first")
	requireTextBlock(t, msgs[1].Content[1], "second")
	requireTextBlock(t, msgs[2].Content[2], "third")
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
