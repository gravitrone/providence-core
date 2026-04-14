package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// WebSearchTool searches the web using the Exa API.
type WebSearchTool struct{}

func (t *WebSearchTool) Name() string        { return "WebSearch" }
func (t *WebSearchTool) Description() string { return "Search the web using semantic search via Exa." }
func (t *WebSearchTool) ReadOnly() bool      { return true }

func (t *WebSearchTool) InputSchema() map[string]any {
	return map[string]any{
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "The search query.",
			},
			"num_results": map[string]any{
				"type":        "integer",
				"description": "Number of results to return (1-10, default 5).",
			},
		},
		"required": []string{"query"},
	}
}

const (
	exaTimeout    = 15 * time.Second
	exaMaxResults = 10
	exaDefaultNum = 5
	exaMaxChars   = 3000
)

// exaSearchURL is the Exa API endpoint. Protected by exaSearchURLMu for
// safe concurrent override in tests.
var (
	exaSearchURLMu sync.RWMutex
	exaSearchURL   = "https://api.exa.ai/search"
)

type exaRequest struct {
	Query      string      `json:"query"`
	NumResults int         `json:"num_results"`
	Contents   exaContents `json:"contents"`
}

type exaContents struct {
	Text exaTextOpts `json:"text"`
}

type exaTextOpts struct {
	MaxCharacters int `json:"max_characters"`
}

type exaResponse struct {
	Results []exaResult `json:"results"`
}

type exaResult struct {
	Title string  `json:"title"`
	URL   string  `json:"url"`
	Text  string  `json:"text"`
	Score float64 `json:"score"`
}

func (t *WebSearchTool) Execute(ctx context.Context, input map[string]any) ToolResult {
	query := paramString(input, "query", "")
	if query == "" {
		return ToolResult{Content: "query is required", IsError: true}
	}

	apiKey := os.Getenv("EXA_API_KEY")
	if apiKey == "" {
		return ToolResult{Content: "EXA_API_KEY not set. Get a free key at exa.ai", IsError: true}
	}

	numResults := paramInt(input, "num_results", exaDefaultNum)
	if numResults < 1 {
		numResults = 1
	}
	if numResults > exaMaxResults {
		numResults = exaMaxResults
	}

	reqBody := exaRequest{
		Query:      query,
		NumResults: numResults,
		Contents: exaContents{
			Text: exaTextOpts{MaxCharacters: exaMaxChars},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("failed to marshal request: %v", err), IsError: true}
	}

	ctx, cancel := context.WithTimeout(ctx, exaTimeout)
	defer cancel()

	exaSearchURLMu.RLock()
	searchURL := exaSearchURL
	exaSearchURLMu.RUnlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, searchURL, bytes.NewReader(body))
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("failed to create request: %v", err), IsError: true}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("search request failed: %v", err), IsError: true}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ToolResult{
			Content: fmt.Sprintf("Exa API returned status %d", resp.StatusCode),
			IsError: true,
		}
	}

	var exaResp exaResponse
	if err := json.NewDecoder(resp.Body).Decode(&exaResp); err != nil {
		return ToolResult{Content: fmt.Sprintf("failed to decode response: %v", err), IsError: true}
	}

	if len(exaResp.Results) == 0 {
		return ToolResult{Content: "No results found."}
	}

	return ToolResult{Content: formatExaResults(exaResp.Results)}
}

// formatExaResults renders search results as numbered plain text.
func formatExaResults(results []exaResult) string {
	var b strings.Builder
	for i, r := range results {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%d. %s\n   %s\n", i+1, r.Title, r.URL)
		text := strings.TrimSpace(r.Text)
		if text != "" {
			fmt.Fprintf(&b, "   %s\n", text)
		}
	}
	return b.String()
}
