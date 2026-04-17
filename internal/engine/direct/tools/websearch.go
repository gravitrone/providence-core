package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// WebSearchConfig defines per-session filtering and usage limits for WebSearchTool.
type WebSearchConfig struct {
	AllowedDomains []string
	BlockedDomains []string
	MaxUses        int
}

// WebSearchTool searches the web using the Exa API.
type WebSearchTool struct {
	Config WebSearchConfig

	mu       sync.Mutex
	uses     int
	seenURLs map[string]struct{}
}

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

	if err := t.reserveUse(); err != nil {
		return ToolResult{Content: err.Error(), IsError: true}
	}

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

	results := t.filterAndDedupeResults(exaResp.Results)
	if len(results) == 0 {
		return ToolResult{Content: "No results found."}
	}

	return ToolResult{Content: formatExaResults(results)}
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

func (t *WebSearchTool) reserveUse() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.Config.MaxUses > 0 && t.uses >= t.Config.MaxUses {
		return fmt.Errorf("websearch: max_uses %d exceeded for this session", t.Config.MaxUses)
	}

	t.uses++
	if t.seenURLs == nil {
		t.seenURLs = make(map[string]struct{})
	}

	return nil
}

func (t *WebSearchTool) filterAndDedupeResults(results []exaResult) []exaResult {
	allowed := normalizeConfiguredDomains(t.Config.AllowedDomains)
	blocked := normalizeConfiguredDomains(t.Config.BlockedDomains)

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.seenURLs == nil {
		t.seenURLs = make(map[string]struct{})
	}

	filtered := make([]exaResult, 0, len(results))
	for _, result := range results {
		if !shouldIncludeWebSearchResult(result.URL, allowed, blocked) {
			continue
		}

		key := strings.TrimSpace(result.URL)
		if _, ok := t.seenURLs[key]; ok {
			continue
		}

		t.seenURLs[key] = struct{}{}
		filtered = append(filtered, result)
	}

	return filtered
}

func shouldIncludeWebSearchResult(rawURL string, allowed map[string]struct{}, blocked map[string]struct{}) bool {
	if len(allowed) == 0 && len(blocked) == 0 {
		return true
	}

	domain := registeredDomainFromURL(rawURL)
	if domain != "" {
		if _, blockedMatch := blocked[domain]; blockedMatch {
			return false
		}
	}

	if len(allowed) == 0 {
		return true
	}

	if domain == "" {
		return false
	}

	_, allowedMatch := allowed[domain]
	return allowedMatch
}

func normalizeConfiguredDomains(domains []string) map[string]struct{} {
	if len(domains) == 0 {
		return nil
	}

	normalized := make(map[string]struct{}, len(domains))
	for _, domain := range domains {
		key := normalizeConfiguredDomain(domain)
		if key == "" {
			continue
		}
		normalized[key] = struct{}{}
	}

	return normalized
}

func normalizeConfiguredDomain(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	if strings.Contains(raw, "://") {
		return registeredDomainFromURL(raw)
	}

	return registeredDomainFromURL("https://" + raw)
}

// registeredDomainFromURL reduces hosts to their last two labels.
// This does not handle public suffix edge cases such as co.uk.
func registeredDomainFromURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}

	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "" {
		return ""
	}

	if ip := net.ParseIP(host); ip != nil {
		return host
	}

	labels := strings.Split(host, ".")
	if len(labels) <= 2 {
		return host
	}

	return strings.Join(labels[len(labels)-2:], ".")
}
