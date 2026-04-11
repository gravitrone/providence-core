package direct

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/gravitrone/providence-core/internal/engine/compact"
)

var _ compact.Provider = (*openrouterCompactProvider)(nil)

type openrouterCompactProvider struct {
	history *[]openrouterHistoryEntry
	apiKey  string
	model   string
}

func newOpenRouterCompactProvider(history *[]openrouterHistoryEntry, apiKey string, model string) compact.Provider {
	return &openrouterCompactProvider{
		history: history,
		apiKey:  apiKey,
		model:   model,
	}
}

func (p *openrouterCompactProvider) Compress(context.Context, int) (int, error) {
	return 0, nil
}

func (p *openrouterCompactProvider) Serialize(int) (string, int, error) {
	items := append([]openrouterHistoryEntry(nil), (*p.history)...)
	cutIndex := findOpenRouterCompactionBoundary(items)
	if cutIndex <= 0 {
		return "", 0, nil
	}

	var transcript strings.Builder
	for i, item := range items[:cutIndex] {
		if i > 0 {
			transcript.WriteString("\n\n")
		}
		writeSerializedOpenRouterEntry(&transcript, item)
	}

	return transcript.String(), cutIndex, nil
}

func (p *openrouterCompactProvider) Replace(summary string, cutIndex int) error {
	items := *p.history
	if cutIndex <= 0 || cutIndex > len(items) {
		return fmt.Errorf("cut index out of range: %d", cutIndex)
	}

	replacement := openrouterHistoryEntry{
		Role:    "user",
		Content: "<context-summary>\n" + strings.TrimSpace(summary) + "\n</context-summary>",
	}
	tail := append([]openrouterHistoryEntry(nil), items[cutIndex:]...)
	*p.history = append([]openrouterHistoryEntry{replacement}, tail...)

	return nil
}

func (p *openrouterCompactProvider) OneShot(ctx context.Context, systemPrompt, input string) (string, error) {
	fastModel := engine.FastForProvider(engine.ProviderOpenRouter)
	if fastModel == "" {
		fastModel = "deepseek/deepseek-chat"
	}

	reqBody := struct {
		Model     string              `json:"model"`
		Stream    bool                `json:"stream"`
		Messages  []openrouterMessage `json:"messages"`
		MaxTokens int                 `json:"max_tokens"`
	}{
		Model:  fastModel,
		Stream: false,
		Messages: []openrouterMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: input},
		},
		MaxTokens: 4096,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal openrouter compact request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, OpenRouterEndpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("create openrouter compact request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("HTTP-Referer", "https://github.com/gravitrone/providence-core")
	req.Header.Set("X-Title", "Providence")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("openrouter compact request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("openrouter compact API error (%d): %s", resp.StatusCode, string(body))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("decode openrouter compact response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", nil
	}

	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}

func (p *openrouterCompactProvider) CurrentTokens() int {
	totalChars := 0
	for _, item := range *p.history {
		totalChars += len(item.Content)
		for _, toolCall := range item.ToolCalls {
			totalChars += len(toolCall.ID)
			totalChars += len(toolCall.Function.Name)
			totalChars += len(toolCall.Function.Arguments)
		}
	}
	return totalChars / 3
}

func (p *openrouterCompactProvider) ContextWindow() int {
	return engine.ContextWindowFor(p.model)
}

func findOpenRouterCompactionBoundary(items []openrouterHistoryEntry) int {
	if len(items) == 0 {
		return 0
	}

	cutIndex := len(items) * 70 / 100
	if cutIndex <= 0 {
		return 0
	}

	for cutIndex < len(items) && openRouterBoundaryOrphansToolOutput(items, cutIndex) {
		cutIndex++
	}

	return cutIndex
}

func openRouterBoundaryOrphansToolOutput(items []openrouterHistoryEntry, cutIndex int) bool {
	if cutIndex < 0 || cutIndex >= len(items) {
		return false
	}

	entry := items[cutIndex]
	if entry.Role != "tool" || entry.CallID == "" {
		return false
	}

	for i := cutIndex - 1; i >= 0; i-- {
		if items[i].Role != "assistant" {
			continue
		}
		for _, toolCall := range items[i].ToolCalls {
			if toolCall.ID == entry.CallID {
				return true
			}
		}
	}

	return false
}

func writeSerializedOpenRouterEntry(transcript *strings.Builder, entry openrouterHistoryEntry) {
	switch entry.Role {
	case "assistant":
		transcript.WriteString("ASSISTANT:\n")
		writeSerializedCompactText(transcript, entry.Content)
		for _, toolCall := range entry.ToolCalls {
			transcript.WriteString("[TOOL_CALL")
			if toolCall.ID != "" {
				transcript.WriteString(" id=")
				transcript.WriteString(toolCall.ID)
			}
			if toolCall.Function.Name != "" {
				transcript.WriteString(" name=")
				transcript.WriteString(toolCall.Function.Name)
			}
			if args := strings.TrimSpace(toolCall.Function.Arguments); args != "" {
				transcript.WriteString(" arguments=")
				transcript.WriteString(truncate(args, 400))
			}
			transcript.WriteString("]\n")
		}
	case "tool":
		transcript.WriteString("TOOL:\n")
		transcript.WriteString("[TOOL_RESULT")
		if entry.CallID != "" {
			transcript.WriteString(" id=")
			transcript.WriteString(entry.CallID)
		}
		if output := strings.TrimSpace(entry.Content); output != "" {
			transcript.WriteString(" content=")
			transcript.WriteString(truncate(output, 400))
		}
		transcript.WriteString("]\n")
	default:
		transcript.WriteString(strings.ToUpper(entry.Role))
		transcript.WriteString(":\n")
		writeSerializedCompactText(transcript, entry.Content)
	}
}
