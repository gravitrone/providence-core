package direct

import (
	"context"
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
	counter            *tokenCounter
	mu                 sync.Mutex
}

// NewConversationHistory creates an empty conversation history.
func NewConversationHistory() *ConversationHistory {
	return &ConversationHistory{
		counter: newDefaultTokenCounter(),
	}
}

// AddUser appends a user text message to the history.
func (h *ConversationHistory) AddUser(text string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.messages = append(h.messages, anthropic.NewUserMessage(
		anthropic.NewTextBlock(text),
	))
	h.invalidateReportedTokensLocked()
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
	h.invalidateReportedTokensLocked()
}

// AddAssistant appends an assistant message (from a completed API response) to the history.
func (h *ConversationHistory) AddAssistant(msg anthropic.Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.messages = append(h.messages, msg.ToParam())
	h.invalidateReportedTokensLocked()
}

// AddAssistantText appends a plain text assistant message (no tool calls).
// Used when restoring past sessions where only text content is preserved.
func (h *ConversationHistory) AddAssistantText(text string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.messages = append(h.messages, anthropic.NewAssistantMessage(
		anthropic.NewTextBlock(text),
	))
	h.invalidateReportedTokensLocked()
}

// RemoveLastAssistant removes the last message from history if it is an
// assistant message. Used when retrying the same request with escalated
// output tokens - the partial response should not remain in history.
func (h *ConversationHistory) RemoveLastAssistant() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.messages) > 0 && h.messages[len(h.messages)-1].Role == anthropic.MessageParamRoleAssistant {
		h.messages = h.messages[:len(h.messages)-1]
		h.invalidateReportedTokensLocked()
	}
}

// AddToolResults appends a user message containing tool result blocks.
func (h *ConversationHistory) AddToolResults(results []anthropic.ContentBlockParamUnion) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.messages = append(h.messages, anthropic.NewUserMessage(results...))
	h.invalidateReportedTokensLocked()
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
	if h.lastReportedTokens > 0 {
		total := h.lastReportedTokens
		h.mu.Unlock()
		return total
	}

	msgs := append([]anthropic.MessageParam(nil), h.messages...)
	counter := h.counter
	h.mu.Unlock()

	if counter == nil {
		return heuristicTokenCount(msgs)
	}

	return counter.Count(context.Background(), msgs)
}

// SetReportedTokens stores the latest provider-reported input and output totals.
func (h *ConversationHistory) SetReportedTokens(input, output int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.lastInputTokens = input
	h.lastOutputTokens = output
	h.lastReportedTokens = input + output
}

func (h *ConversationHistory) invalidateReportedTokensLocked() {
	h.lastReportedTokens = 0
	h.lastInputTokens = 0
	h.lastOutputTokens = 0
}

func (h *ConversationHistory) estimateTokensLocked() int {
	return heuristicTokenCount(h.messages)
}

// StripThinkingBlocks removes thinking and redacted thinking content blocks from
// all assistant messages in the history. Thinking blocks are model-bound and will
// cause 400 errors when sent to a different model during fallback.
func (h *ConversationHistory) StripThinkingBlocks() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for i := range h.messages {
		msg := &h.messages[i]
		if msg.Role != "assistant" {
			continue
		}
		filtered := msg.Content[:0]
		for _, block := range msg.Content {
			if block.OfThinking != nil || block.OfRedactedThinking != nil {
				continue
			}
			filtered = append(filtered, block)
		}
		msg.Content = filtered
	}

	h.invalidateReportedTokensLocked()
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
		h.invalidateReportedTokensLocked()
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
	h.invalidateReportedTokensLocked()

	return nil
}

// ReplaceAll atomically replaces the entire message history and resets
// reported token counters. Used after context collapse rewrites the slice.
func (h *ConversationHistory) ReplaceAll(msgs []anthropic.MessageParam) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.messages = msgs
	h.invalidateReportedTokensLocked()
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
