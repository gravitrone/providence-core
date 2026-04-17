package direct

import (
	"bufio"
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

func (p *codexCompactProvider) Serialize(keepRecentTokens int) (string, int, error) {
	items := append([]codexHistoryEntry(nil), (*p.history)...)
	cutIndex := findCodexCompactionBoundary(items, keepRecentTokens)
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

	// Codex (chatgpt.com/backend-api) requires Stream=true; non-streaming
	// requests fail with 400 "Stream must be set to true". We collect SSE
	// output_text deltas into a single summary string below.
	reqBody := codexRequest{
		Model:        fastModel,
		Store:        false,
		Stream:       true,
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

	resp, err := providerHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("codex compact request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("codex compact API error (%d): %s", resp.StatusCode, string(body))
	}

	summary, err := collectCodexOneShotText(ctx, resp.Body)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(summary), nil
}

// collectCodexOneShotText reads a Codex SSE stream and accumulates all
// response.output_text.delta events into a single string. It also handles
// terminal output_item.done events that carry the full text in case no
// per-token deltas were emitted. The reader stops on response.done /
// response.completed or end of stream.
func collectCodexOneShotText(ctx context.Context, body io.Reader) (string, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	var out strings.Builder
	var sawDelta bool

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return out.String(), ctx.Err()
		default:
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var base struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(data), &base); err != nil {
			continue
		}

		switch base.Type {
		case "response.output_text.delta":
			var delta struct {
				Delta string `json:"delta"`
			}
			if err := json.Unmarshal([]byte(data), &delta); err == nil && delta.Delta != "" {
				out.WriteString(delta.Delta)
				sawDelta = true
			}
		case "response.output_item.done":
			// Fallback: some servers may emit only the final item without per-token deltas.
			if sawDelta {
				continue
			}
			var item struct {
				Item struct {
					Type    string `json:"type"`
					Content []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"content"`
				} `json:"item"`
			}
			if err := json.Unmarshal([]byte(data), &item); err != nil {
				continue
			}
			if item.Item.Type != "message" {
				continue
			}
			for _, block := range item.Item.Content {
				if block.Type != "output_text" && block.Type != "text" {
					continue
				}
				out.WriteString(block.Text)
			}
		case "response.done", "response.completed":
			// Terminal event; let the loop drain naturally.
		}
	}

	if err := scanner.Err(); err != nil {
		return out.String(), fmt.Errorf("read codex compact stream: %w", err)
	}
	return out.String(), nil
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

func (p *codexCompactProvider) MaxOutputTokens() int {
	return engine.MaxOutputTokensFor(p.model)
}

// findCodexCompactionBoundary returns the index where the codex history
// should be cut so the tail starting there preserves at least
// keepRecentTokens worth of content. When keepRecentTokens is zero or
// negative, falls back to a fixed 70% message count cut. The returned index
// is advanced past any entry that would orphan a tool output from its
// originating function_call.
func findCodexCompactionBoundary(items []codexHistoryEntry, keepRecentTokens int) int {
	n := len(items)
	if n == 0 {
		return 0
	}

	var cutIndex int
	if keepRecentTokens <= 0 {
		cutIndex = n * 70 / 100
	} else {
		accumulated := 0
		for i := n - 1; i >= 0; i-- {
			accumulated += codexEntryEstimatedTokens(items[i])
			if accumulated >= keepRecentTokens {
				cutIndex = i
				break
			}
		}
	}
	if cutIndex <= 0 {
		return 0
	}

	for cutIndex < n && codexBoundaryOrphansToolOutput(items, cutIndex) {
		cutIndex++
	}

	return cutIndex
}

// codexEntryEstimatedTokens returns the char*4/3 token estimate for a
// single codex history entry, matching CurrentTokens' chars/3 heuristic up
// to the /3 vs *4/3 difference (kept consistent with the anthropic
// boundary helper so budget semantics line up across providers).
func codexEntryEstimatedTokens(e codexHistoryEntry) int {
	return (len(e.Content) + len(e.FuncName)) * 4 / 3
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
