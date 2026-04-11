package direct

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gravitrone/providence-core/internal/auth"
	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/gravitrone/providence-core/internal/engine/compact"
)

var _ compact.Provider = (*codexCompactProvider)(nil)

type codexCompactProvider struct {
	history *[]codexHistoryEntry
	model   string
}

func newCodexCompactProvider(history *[]codexHistoryEntry, model string) compact.Provider {
	return &codexCompactProvider{
		history: history,
		model:   model,
	}
}

func (p *codexCompactProvider) Compress(context.Context, int) (int, error) {
	return 0, nil
}

func (p *codexCompactProvider) Serialize(int) (string, int, error) {
	items := append([]codexHistoryEntry(nil), (*p.history)...)
	cutIndex := findCodexCompactionBoundary(items)
	if cutIndex <= 0 {
		return "", 0, nil
	}

	var transcript strings.Builder
	for i, item := range items[:cutIndex] {
		if i > 0 {
			transcript.WriteString("\n\n")
		}
		writeSerializedCodexEntry(&transcript, item)
	}

	return transcript.String(), cutIndex, nil
}

func (p *codexCompactProvider) Replace(summary string, cutIndex int) error {
	items := *p.history
	if cutIndex <= 0 || cutIndex > len(items) {
		return fmt.Errorf("cut index out of range: %d", cutIndex)
	}

	replacement := codexHistoryEntry{
		Role:    "user",
		Content: "<context-summary>\n" + strings.TrimSpace(summary) + "\n</context-summary>",
	}
	tail := append([]codexHistoryEntry(nil), items[cutIndex:]...)
	*p.history = append([]codexHistoryEntry{replacement}, tail...)

	return nil
}

func (p *codexCompactProvider) OneShot(ctx context.Context, systemPrompt, input string) (string, error) {
	fastModel := engine.FastForProvider(engine.ProviderOpenAI)
	if fastModel == "" {
		fastModel = "gpt-5.4-mini"
	}

	tokens, err := auth.EnsureValidOpenAITokens()
	if err != nil {
		return "", fmt.Errorf("openai auth: %w", err)
	}

	inputMsg, err := json.Marshal(map[string]any{
		"role":    "user",
		"content": input,
	})
	if err != nil {
		return "", fmt.Errorf("marshal codex compact input: %w", err)
	}

	reqBody := codexRequest{
		Model:        fastModel,
		Store:        false,
		Stream:       false,
		Instructions: systemPrompt,
		Input:        []json.RawMessage{inputMsg},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal codex compact request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, auth.CodexEndpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("create codex compact request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	if tokens.AccountID != "" {
		req.Header.Set("X-OpenAI-Account-ID", tokens.AccountID)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("codex compact request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("codex compact API error (%d): %s", resp.StatusCode, string(body))
	}

	var parsed struct {
		OutputText string `json:"output_text"`
		Choices    []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text,omitempty"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("decode codex compact response: %w", err)
	}

	if text := strings.TrimSpace(parsed.OutputText); text != "" {
		return text, nil
	}
	if len(parsed.Choices) > 0 {
		if text := strings.TrimSpace(parsed.Choices[0].Message.Content); text != "" {
			return text, nil
		}
	}

	var summary strings.Builder
	for _, item := range parsed.Output {
		for _, block := range item.Content {
			if block.Type != "output_text" && block.Type != "text" {
				continue
			}
			text := strings.TrimSpace(block.Text)
			if text == "" {
				continue
			}
			if summary.Len() > 0 {
				summary.WriteString("\n")
			}
			summary.WriteString(text)
		}
	}

	return strings.TrimSpace(summary.String()), nil
}

func (p *codexCompactProvider) CurrentTokens() int {
	totalChars := 0
	for _, item := range *p.history {
		totalChars += len(item.Content)
		totalChars += len(item.FuncName)
	}
	return totalChars / 3
}

func (p *codexCompactProvider) ContextWindow() int {
	return engine.ContextWindowFor(p.model)
}

func findCodexCompactionBoundary(items []codexHistoryEntry) int {
	if len(items) == 0 {
		return 0
	}

	cutIndex := len(items) * 70 / 100
	if cutIndex <= 0 {
		return 0
	}

	for cutIndex < len(items) && codexBoundaryOrphansToolOutput(items, cutIndex) {
		cutIndex++
	}

	return cutIndex
}

func codexBoundaryOrphansToolOutput(items []codexHistoryEntry, cutIndex int) bool {
	if cutIndex < 0 || cutIndex >= len(items) {
		return false
	}

	entry := items[cutIndex]
	if entry.Role != "tool" || entry.CallID == "" {
		return false
	}

	for i := cutIndex - 1; i >= 0; i-- {
		if items[i].Role == "function_call" && items[i].CallID == entry.CallID {
			return true
		}
	}

	return false
}

func writeSerializedCodexEntry(transcript *strings.Builder, entry codexHistoryEntry) {
	switch entry.Role {
	case "assistant":
		transcript.WriteString("ASSISTANT:\n")
		writeSerializedCompactText(transcript, entry.Content)
	case "function_call":
		transcript.WriteString("ASSISTANT:\n")
		transcript.WriteString("[FUNCTION_CALL")
		if entry.FuncName != "" {
			transcript.WriteString(" name=")
			transcript.WriteString(entry.FuncName)
		}
		if entry.CallID != "" {
			transcript.WriteString(" id=")
			transcript.WriteString(entry.CallID)
		}
		if args := strings.TrimSpace(entry.Content); args != "" {
			transcript.WriteString(" arguments=")
			transcript.WriteString(truncate(args, 400))
		}
		transcript.WriteString("]\n")
	case "tool":
		transcript.WriteString("TOOL:\n")
		transcript.WriteString("[FUNCTION_CALL_OUTPUT")
		if entry.CallID != "" {
			transcript.WriteString(" id=")
			transcript.WriteString(entry.CallID)
		}
		if output := strings.TrimSpace(entry.Content); output != "" {
			transcript.WriteString(" output=")
			transcript.WriteString(truncate(output, 400))
		}
		transcript.WriteString("]\n")
	default:
		transcript.WriteString("USER:\n")
		writeSerializedCompactText(transcript, entry.Content)
	}
}

func writeSerializedCompactText(transcript *strings.Builder, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	transcript.WriteString(text)
	transcript.WriteString("\n")
}
