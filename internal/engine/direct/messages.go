package direct

import (
	"encoding/base64"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"
)

// ImageData holds the data needed to include an image in a message.
type ImageData struct {
	MediaType string
	Data      []byte
}

// ConversationHistory manages the message history for a direct engine conversation.
// It is safe for concurrent access.
type ConversationHistory struct {
	messages []anthropic.MessageParam
	mu       sync.Mutex
}

// NewConversationHistory creates an empty conversation history.
func NewConversationHistory() *ConversationHistory {
	return &ConversationHistory{}
}

// AddUser appends a user text message to the history.
func (h *ConversationHistory) AddUser(text string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.messages = append(h.messages, anthropic.NewUserMessage(
		anthropic.NewTextBlock(text),
	))
}

// AddUserWithImages appends a user message with image content blocks followed by a text block.
func (h *ConversationHistory) AddUserWithImages(text string, images []ImageData) {
	h.mu.Lock()
	defer h.mu.Unlock()

	var blocks []anthropic.ContentBlockParamUnion
	for _, img := range images {
		blocks = append(blocks, anthropic.NewImageBlockBase64(
			img.MediaType,
			base64.StdEncoding.EncodeToString(img.Data),
		))
	}
	blocks = append(blocks, anthropic.NewTextBlock(text))
	h.messages = append(h.messages, anthropic.NewUserMessage(blocks...))
}

// AddAssistant appends an assistant message (from a completed API response) to the history.
func (h *ConversationHistory) AddAssistant(msg anthropic.Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.messages = append(h.messages, msg.ToParam())
}

// AddToolResults appends a user message containing tool result blocks.
func (h *ConversationHistory) AddToolResults(results []anthropic.ContentBlockParamUnion) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.messages = append(h.messages, anthropic.NewUserMessage(results...))
}

// Messages returns a copy of the current message list.
func (h *ConversationHistory) Messages() []anthropic.MessageParam {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]anthropic.MessageParam, len(h.messages))
	copy(out, h.messages)
	return out
}

// EstimateTokens gives a rough token estimate using the charCount * 4 / 3 heuristic.
func (h *ConversationHistory) EstimateTokens() int {
	h.mu.Lock()
	defer h.mu.Unlock()

	charCount := 0
	for _, msg := range h.messages {
		for _, block := range msg.Content {
			if block.OfText != nil {
				charCount += len(block.OfText.Text)
			}
			if block.OfToolResult != nil {
				for _, inner := range block.OfToolResult.Content {
					if inner.OfText != nil {
						charCount += len(inner.OfText.Text)
					}
				}
			}
		}
	}
	return charCount * 4 / 3
}
