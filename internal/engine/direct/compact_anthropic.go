package direct

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/gravitrone/providence-core/internal/engine/compact"
)

var _ compact.Provider = (*anthropicCompactProvider)(nil)

type anthropicCompactProvider struct {
	history *ConversationHistory
	client  anthropic.Client
	model   string
}

func newAnthropicCompactProvider(h *ConversationHistory, client anthropic.Client, model string) compact.Provider {
	return &anthropicCompactProvider{
		history: h,
		client:  client,
		model:   model,
	}
}

func (p *anthropicCompactProvider) Compress(context.Context, int) (int, error) {
	return 0, nil
}

func (p *anthropicCompactProvider) Serialize(keepRecentTokens int) (string, int, error) {
	msgs := p.history.Messages()
	cutIndex := findSafeCompactionBoundary(msgs, keepRecentTokens)
	if cutIndex <= 0 {
		return "", 0, nil
	}

	prefix := p.history.MessagesBefore(cutIndex)
	var transcript strings.Builder
	for i, msg := range prefix {
		if i > 0 {
			transcript.WriteString("\n\n")
		}
		writeSerializedMessage(&transcript, msg)
	}

	return transcript.String(), cutIndex, nil
}

func (p *anthropicCompactProvider) Replace(summary string, cutIndex int) error {
	replacement := anthropic.NewUserMessage(
		anthropic.NewTextBlock(
			"<context-summary>\n" + strings.TrimSpace(summary) + "\n</context-summary>",
		),
	)
	return p.history.ReplaceTail(replacement, cutIndex)
}

func (p *anthropicCompactProvider) OneShot(ctx context.Context, systemPrompt, input string) (string, error) {
	model := engine.FastForProvider(engine.ProviderAnthropic)
	if model == "" {
		model = p.model
	}

	msg, err := p.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: 4096,
		System: []anthropic.TextBlockParam{{
			Text: systemPrompt,
		}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(input)),
		},
	})
	if err != nil {
		return "", err
	}
	if msg == nil {
		return "", nil
	}

	var summary strings.Builder
	for _, block := range msg.Content {
		if block.Type != "text" {
			continue
		}
		if summary.Len() > 0 {
			summary.WriteString("\n")
		}
		summary.WriteString(block.Text)
	}

	return strings.TrimSpace(summary.String()), nil
}

func (p *anthropicCompactProvider) CurrentTokens() int {
	return p.history.CurrentTokens()
}

func (p *anthropicCompactProvider) ContextWindow() int {
	return engine.ContextWindowFor(p.model)
}

func (p *anthropicCompactProvider) MaxOutputTokens() int {
	return engine.MaxOutputTokensFor(p.model)
}

// findSafeCompactionBoundary returns the index where the history should be
// cut so that the tail starting at the returned index preserves at least
// keepRecentTokens worth of estimated content (chars * 4 / 3). When
// keepRecentTokens is zero or negative, falls back to a fixed 70% message
// count cut (legacy behaviour retained for callers that do not supply a
// budget). The returned index is advanced past any message that opens with
// a tool_result block so the preserved tail never orphans a tool_result
// from its tool_use.
func findSafeCompactionBoundary(msgs []anthropic.MessageParam, keepRecentTokens int) int {
	n := len(msgs)
	if n == 0 {
		return 0
	}

	var cutIndex int
	if keepRecentTokens <= 0 {
		cutIndex = n * 70 / 100
	} else {
		accumulated := 0
		for i := n - 1; i >= 0; i-- {
			accumulated += messageEstimatedTokens(msgs[i])
			if accumulated >= keepRecentTokens {
				cutIndex = i
				break
			}
		}
	}
	if cutIndex <= 0 {
		return 0
	}

	for cutIndex < n && messageHasToolResult(msgs[cutIndex]) {
		cutIndex++
	}

	return cutIndex
}

// messageEstimatedTokens returns the char*4/3 token estimate for a single
// message. Mirrors the heuristic used by ConversationHistory.EstimateTokens
// so the compactor stays consistent with the rest of the package.
func messageEstimatedTokens(m anthropic.MessageParam) int {
	chars := 0
	for _, block := range m.Content {
		if block.OfText != nil {
			chars += len(block.OfText.Text)
		}
		if block.OfToolResult != nil {
			for _, inner := range block.OfToolResult.Content {
				if inner.OfText != nil {
					chars += len(inner.OfText.Text)
				}
			}
		}
	}
	return chars * 4 / 3
}

func messageHasToolResult(m anthropic.MessageParam) bool {
	for _, block := range m.Content {
		if block.OfToolResult != nil {
			return true
		}
	}
	return false
}

func writeSerializedMessage(transcript *strings.Builder, msg anthropic.MessageParam) {
	if msg.Role == anthropic.MessageParamRoleAssistant {
		transcript.WriteString("ASSISTANT")
	} else {
		transcript.WriteString("USER")
	}
	transcript.WriteString(":\n")

	for _, block := range msg.Content {
		writeSerializedBlock(transcript, block)
	}
}

func writeSerializedBlock(transcript *strings.Builder, block anthropic.ContentBlockParamUnion) {
	switch {
	case block.OfText != nil:
		text := strings.TrimSpace(block.OfText.Text)
		if text == "" {
			return
		}
		transcript.WriteString(text)
		transcript.WriteString("\n")
	case block.OfToolUse != nil:
		transcript.WriteString("[TOOL_USE name=")
		transcript.WriteString(block.OfToolUse.Name)
		if block.OfToolUse.ID != "" {
			transcript.WriteString(" id=")
			transcript.WriteString(block.OfToolUse.ID)
		}
		if payload := renderCompactJSON(block.OfToolUse.Input); payload != "" {
			transcript.WriteString(" input=")
			transcript.WriteString(truncate(payload, 400))
		}
		transcript.WriteString("]\n")
	case block.OfToolResult != nil:
		transcript.WriteString("[TOOL_RESULT id=")
		transcript.WriteString(block.OfToolResult.ToolUseID)
		if block.OfToolResult.IsError.Valid() && block.OfToolResult.IsError.Value {
			transcript.WriteString(" error=true")
		}
		if content := strings.TrimSpace(renderToolResultText(block.OfToolResult.Content)); content != "" {
			transcript.WriteString(" content=")
			transcript.WriteString(truncate(content, 400))
		}
		transcript.WriteString("]\n")
	case block.OfImage != nil:
		transcript.WriteString("[IMAGE]\n")
	case block.OfDocument != nil:
		transcript.WriteString("[DOCUMENT]\n")
	case block.OfThinking != nil:
		transcript.WriteString("[THINKING]\n")
	case block.OfRedactedThinking != nil:
		transcript.WriteString("[REDACTED_THINKING]\n")
	case block.OfServerToolUse != nil:
		transcript.WriteString("[SERVER_TOOL_USE name=")
		transcript.WriteString(string(block.OfServerToolUse.Name))
		transcript.WriteString("]\n")
	default:
		transcript.WriteString("[UNSUPPORTED_BLOCK]\n")
	}
}

func renderToolResultText(content []anthropic.ToolResultBlockParamContentUnion) string {
	var rendered []string
	for _, block := range content {
		switch {
		case block.OfText != nil:
			text := strings.TrimSpace(block.OfText.Text)
			if text != "" {
				rendered = append(rendered, text)
			}
		case block.OfImage != nil:
			rendered = append(rendered, "[IMAGE]")
		case block.OfDocument != nil:
			rendered = append(rendered, "[DOCUMENT]")
		case block.OfSearchResult != nil:
			rendered = append(rendered, "[SEARCH_RESULT]")
		case block.OfToolReference != nil:
			rendered = append(rendered, "[TOOL_REFERENCE]")
		}
	}
	return strings.Join(rendered, " ")
}

func renderCompactJSON(v any) string {
	if v == nil {
		return ""
	}

	encoded, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(encoded)
}
