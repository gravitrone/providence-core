package direct

import (
	"encoding/base64"
	"fmt"
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
	messages           []anthropic.MessageParam
	lastReportedTokens int
	lastInputTokens    int
	lastOutputTokens   int
	mu                 sync.Mutex
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

// AddAssistantText appends a plain text assistant message (no tool calls).
// Used when restoring past sessions where only text content is preserved.
func (h *ConversationHistory) AddAssistantText(text string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.messages = append(h.messages, anthropic.NewAssistantMessage(
		anthropic.NewTextBlock(text),
	))
}

// RemoveLastAssistant removes the last message from history if it is an
// assistant message. Used when retrying the same request with escalated
// output tokens - the partial response should not remain in history.
func (h *ConversationHistory) RemoveLastAssistant() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.messages) > 0 && h.messages[len(h.messages)-1].Role == anthropic.MessageParamRoleAssistant {
		h.messages = h.messages[:len(h.messages)-1]
	}
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

	return h.estimateTokensLocked()
}

// CurrentTokens returns the last provider-reported total when available,
// falling back to a rough estimate from the current message history.
func (h *ConversationHistory) CurrentTokens() int {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.lastReportedTokens > 0 {
		return h.lastReportedTokens
	}
	return h.estimateTokensLocked()
}

// SetReportedTokens stores the latest provider-reported input and output totals.
func (h *ConversationHistory) SetReportedTokens(input, output int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.lastInputTokens = input
	h.lastOutputTokens = output
	h.lastReportedTokens = input + output
}

func (h *ConversationHistory) estimateTokensLocked() int {
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

// --- W5 microcompact ---

// CompressLongToolResults replaces oversized older tool_result blocks with a stub.
func (h *ConversationHistory) CompressLongToolResults(minLen int) int {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.messages) <= 4 {
		return 0
	}

	compressed := 0
	for i := 0; i < len(h.messages)-4; i++ {
		for j := range h.messages[i].Content {
			toolResult := h.messages[i].Content[j].OfToolResult
			if toolResult == nil {
				continue
			}

			totalLen := 0
			for _, inner := range toolResult.Content {
				if inner.OfText != nil {
					totalLen += len(inner.OfText.Text)
				}
			}
			if totalLen <= minLen {
				continue
			}

			toolResult.Content = []anthropic.ToolResultBlockParamContentUnion{{
				OfText: &anthropic.TextBlockParam{
					Text: fmt.Sprintf(
						"[compressed: %d chars from tool_use_id=%s]",
						totalLen,
						toolResult.ToolUseID,
					),
				},
			}}
			compressed++
		}
	}

	if compressed > 0 {
		h.lastReportedTokens = 0
		h.lastInputTokens = 0
		h.lastOutputTokens = 0
	}

	return compressed
}

// --- W4 compaction support ---

// ReplaceTail replaces the compacted prefix before cutIndex with a single
// replacement message while preserving the recent tail starting at cutIndex.
func (h *ConversationHistory) ReplaceTail(replacement anthropic.MessageParam, cutIndex int) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if cutIndex <= 0 || cutIndex > len(h.messages) {
		return fmt.Errorf("cut index out of range: %d", cutIndex)
	}

	tail := append([]anthropic.MessageParam(nil), h.messages[cutIndex:]...)
	h.messages = append([]anthropic.MessageParam{replacement}, tail...)
	h.lastReportedTokens = 0
	h.lastInputTokens = 0
	h.lastOutputTokens = 0

	return nil
}

// MessagesBefore returns a copy of the messages before idx.
func (h *ConversationHistory) MessagesBefore(idx int) []anthropic.MessageParam {
	h.mu.Lock()
	defer h.mu.Unlock()

	if idx <= 0 {
		return nil
	}
	if idx > len(h.messages) {
		idx = len(h.messages)
	}

	out := make([]anthropic.MessageParam, idx)
	copy(out, h.messages[:idx])
	return out
}
