package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	webFetchTimeout    = 30 * time.Second
	maxResponseBody    = 5 * 1024 * 1024 // 5MB
	maxResultContent   = 50_000          // chars
	webFetchUserAgent  = "Providence/0.3"
)

// WebFetchTool fetches a URL and extracts clean readable content from HTML.
type WebFetchTool struct{}

func (t *WebFetchTool) Name() string { return "WebFetch" }
func (t *WebFetchTool) Description() string {
	return "Fetch a web page by URL and extract clean readable text content. Strips HTML tags, scripts, styles, and navigation to return just the main text. Use the optional prompt parameter to specify what information to focus on."
}
func (t *WebFetchTool) ReadOnly() bool { return true }

func (t *WebFetchTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "The URL to fetch.",
			},
			"prompt": map[string]any{
				"type":        "string",
				"description": "Optional context for what to extract or focus on from the page.",
			},
		},
		"required": []string{"url"},
	}
}

func (t *WebFetchTool) Execute(ctx context.Context, input map[string]any) ToolResult {
	rawURL := paramString(input, "url", "")
	prompt := paramString(input, "prompt", "")

	if rawURL == "" {
		return ToolResult{Content: "url is required", IsError: true}
	}

	// Validate and normalize the URL.
	fetchURL, err := normalizeURL(rawURL)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid url: %v", err), IsError: true}
	}

	// Fetch the page.
	body, contentType, err := fetchPage(ctx, fetchURL)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("fetch failed: %v", err), IsError: true}
	}

	// Extract text content.
	var content string
	if strings.Contains(contentType, "text/html") || strings.Contains(contentType, "application/xhtml") {
		content = extractTextFromHTML(body)
	} else {
		// Plain text or other - use as-is.
		content = body
	}

	// Truncate if needed.
	truncated := false
	if len(content) > maxResultContent {
		content = content[:maxResultContent]
		truncated = true
	}

	// Build result.
	var result strings.Builder
	if prompt != "" {
		result.WriteString("Prompt: ")
		result.WriteString(prompt)
		result.WriteString("\n\nPage content:\n")
	}
	result.WriteString(content)
	if truncated {
		result.WriteString("\n\n[content truncated at 50,000 characters]")
	}

	return ToolResult{Content: result.String()}
}

// normalizeURL validates and normalizes the URL, upgrading http to https.
func normalizeURL(rawURL string) (string, error) {
	// Add scheme if missing.
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	if parsed.Host == "" {
		return "", fmt.Errorf("missing host")
	}

	// Auto-upgrade http to https.
	if parsed.Scheme == "http" {
		parsed.Scheme = "https"
	}

	if parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme: %s", parsed.Scheme)
	}

	return parsed.String(), nil
}

// fetchPage performs the HTTP GET and returns the body string and content type.
func fetchPage(ctx context.Context, fetchURL string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, webFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, nil)
	if err != nil {
		return "", "", err
	}

	req.Header.Set("User-Agent", webFetchUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.8")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", "", fmt.Errorf("HTTP %d %s", resp.StatusCode, resp.Status)
	}

	// Limit body read.
	limited := io.LimitReader(resp.Body, maxResponseBody)
	data, err := io.ReadAll(limited)
	if err != nil {
		return "", "", err
	}

	contentType := resp.Header.Get("Content-Type")
	return string(data), contentType, nil
}

// Pre-compiled regexes for HTML extraction.
var (
	reScript      = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	reStyle       = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	reComment     = regexp.MustCompile(`(?s)<!--.*?-->`)
	reNav         = regexp.MustCompile(`(?is)<nav[^>]*>.*?</nav>`)
	reHeader      = regexp.MustCompile(`(?is)<header[^>]*>.*?</header>`)
	reFooter      = regexp.MustCompile(`(?is)<footer[^>]*>.*?</footer>`)
	reAside       = regexp.MustCompile(`(?is)<aside[^>]*>.*?</aside>`)
	reBlockTag    = regexp.MustCompile(`(?i)<\s*/?\s*(br|p|div|h[1-6]|li|tr|blockquote|section|article|main)\b[^>]*>`)
	reAllTags     = regexp.MustCompile(`<[^>]+>`)
	reBlankLines  = regexp.MustCompile(`\n{3,}`)
	reSpaces      = regexp.MustCompile(`[^\S\n]{2,}`) // collapse runs of horizontal whitespace
)

// extractTextFromHTML strips HTML and returns clean readable text.
func extractTextFromHTML(html string) string {
	s := html

	// 1. Remove script and style blocks.
	s = reScript.ReplaceAllString(s, "")
	s = reStyle.ReplaceAllString(s, "")

	// 2. Remove HTML comments.
	s = reComment.ReplaceAllString(s, "")

	// 3. Remove nav, header, footer, aside blocks.
	s = reNav.ReplaceAllString(s, "")
	s = reHeader.ReplaceAllString(s, "")
	s = reFooter.ReplaceAllString(s, "")
	s = reAside.ReplaceAllString(s, "")

	// 4. Replace block-level tags with newlines.
	s = reBlockTag.ReplaceAllString(s, "\n")

	// 5. Strip all remaining HTML tags.
	s = reAllTags.ReplaceAllString(s, "")

	// 6. Decode common HTML entities.
	s = decodeHTMLEntities(s)

	// 7. Collapse horizontal whitespace.
	s = reSpaces.ReplaceAllString(s, " ")

	// 8. Collapse multiple blank lines into double newlines.
	s = reBlankLines.ReplaceAllString(s, "\n\n")

	// 9. Trim.
	s = strings.TrimSpace(s)

	return s
}

// decodeHTMLEntities handles the most common HTML entities.
func decodeHTMLEntities(s string) string {
	replacements := []struct{ old, new string }{
		{"&amp;", "&"},
		{"&lt;", "<"},
		{"&gt;", ">"},
		{"&quot;", "\""},
		{"&#39;", "'"},
		{"&#x27;", "'"},
		{"&apos;", "'"},
		{"&nbsp;", " "},
		{"&mdash;", "-"},
		{"&ndash;", "-"},
		{"&hellip;", "..."},
		{"&copy;", "(c)"},
		{"&reg;", "(R)"},
		{"&trade;", "(TM)"},
	}
	for _, r := range replacements {
		s = strings.ReplaceAll(s, r.old, r.new)
	}
	return s
}
