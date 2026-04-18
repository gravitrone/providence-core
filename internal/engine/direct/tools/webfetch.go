package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	webFetchTimeout       = 60 * time.Second
	webFetchDomainTimeout = 10 * time.Second
	maxResponseBody       = 5 * 1024 * 1024 // 5 MiB
	maxResultContent      = 50_000          // chars kept inline after extraction
	maxRedirects          = 10
	webFetchUserAgent     = "Providence/0.3"
)

// Error taxonomy: callers can errors.Is the returned error from
// fetchPage so retry strategies can be intelligent. The tool-surface
// string includes the sentinel name so users reading the TUI get the
// category alongside the detail.
var (
	ErrDomainUnreachable = errors.New("domain unreachable")
	ErrTooManyRedirects  = errors.New("too many redirects")
	ErrCrossHostRedirect = errors.New("cross-host redirect refused")
	ErrBodyTooLarge      = errors.New("response body exceeds cap")
	ErrUnsupportedScheme = errors.New("unsupported URL scheme")
	ErrContentTypeBinary = errors.New("content type is binary; spilled to disk")
)

// webFetchDownloadsDir holds the directory where binary responses
// spill. Mutable so tests can redirect.
var (
	webFetchDownloadsDirOnce sync.Once
	webFetchDownloadsDir     string
	webFetchDownloadsDirMu   sync.RWMutex
)

// SetWebFetchDownloadsDir overrides the binary spill directory (for
// tests).
func SetWebFetchDownloadsDir(dir string) {
	webFetchDownloadsDirMu.Lock()
	defer webFetchDownloadsDirMu.Unlock()
	webFetchDownloadsDir = dir
}

func defaultWebFetchDownloadsDir() string {
	webFetchDownloadsDirOnce.Do(func() {
		webFetchDownloadsDirMu.Lock()
		defer webFetchDownloadsDirMu.Unlock()
		if webFetchDownloadsDir != "" {
			return
		}
		home, err := os.UserHomeDir()
		if err != nil {
			webFetchDownloadsDir = filepath.Join(os.TempDir(), "providence-webfetch-downloads")
			return
		}
		webFetchDownloadsDir = filepath.Join(home, ".providence", "webfetch-downloads")
	})
	webFetchDownloadsDirMu.RLock()
	defer webFetchDownloadsDirMu.RUnlock()
	return webFetchDownloadsDir
}

// WebFetchTool fetches a URL and extracts clean readable content from HTML.
type WebFetchTool struct{}

func (t *WebFetchTool) Name() string { return "WebFetch" }
func (t *WebFetchTool) Description() string {
	return "Fetch a web page by URL and extract clean readable text content. Strips HTML tags, scripts, styles, and navigation to return just the main text. Use the optional prompt parameter to specify what information to focus on."
}
func (t *WebFetchTool) ReadOnly() bool { return true }
func (t *WebFetchTool) ResultSizeCap() int {
	return webFetchResultSizeCap
}

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
			"no_cache": map[string]any{
				"type":        "boolean",
				"description": "Bypass the 15-minute LRU cache and force a fresh fetch.",
			},
		},
		"required": []string{"url"},
	}
}

func (t *WebFetchTool) Execute(ctx context.Context, input map[string]any) ToolResult {
	rawURL := paramString(input, "url", "")
	prompt := paramString(input, "prompt", "")
	noCache := paramBool(input, "no_cache", false)

	if rawURL == "" {
		return ToolResult{Content: "url is required", IsError: true}
	}

	fetchURL, err := normalizeURL(rawURL)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid url: %v", err), IsError: true}
	}

	// Serve from cache when fresh and the caller did not opt out.
	if !noCache {
		if entry, ok := webFetchCacheGet(fetchURL); ok {
			return buildResult(entry.body, entry.contentType, prompt, fetchURL, true)
		}
	}

	body, contentType, err := fetchPage(ctx, fetchURL)
	if err != nil {
		return formatFetchError(err)
	}

	// Binary content (image, pdf, archive, etc): spill to disk and
	// return a reference line instead of garbage text.
	if isBinaryContentType(contentType) {
		path, spillErr := spillBinaryBody(body, fetchURL, contentType)
		if spillErr != nil {
			return ToolResult{Content: fmt.Sprintf("%s: %v", ErrContentTypeBinary.Error(), spillErr), IsError: true}
		}
		return ToolResult{
			Content:  fmt.Sprintf("Binary content (%s) saved to %s", contentType, path),
			Metadata: map[string]any{"spill_path": path, "content_type": contentType},
		}
	}

	webFetchCachePut(fetchURL, body, contentType)
	return buildResult(body, contentType, prompt, fetchURL, false)
}

// buildResult extracts text from HTML, truncates to inline cap, and
// formats the response with an optional prompt banner.
func buildResult(body, contentType, prompt, fromURL string, fromCache bool) ToolResult {
	var content string
	if strings.Contains(contentType, "text/html") || strings.Contains(contentType, "application/xhtml") {
		content = extractTextFromHTML(body)
	} else {
		content = body
	}

	truncated := false
	if len(content) > maxResultContent {
		content = content[:maxResultContent]
		truncated = true
	}

	var result strings.Builder
	if prompt != "" {
		result.WriteString(fmt.Sprintf("<webfetch prompt=%q url=%q>\n", prompt, fromURL))
	}
	result.WriteString(content)
	if prompt != "" {
		result.WriteString("\n</webfetch>")
	}
	if truncated {
		result.WriteString("\n\n[content truncated at 50,000 characters]")
	}

	meta := map[string]any{"from_cache": fromCache, "content_type": contentType}
	return ToolResult{Content: result.String(), Metadata: meta}
}

// formatFetchError maps a sentinel error back to a user-facing tool
// result so the TUI can display a distinguishable category instead of
// a single opaque string.
func formatFetchError(err error) ToolResult {
	switch {
	case errors.Is(err, ErrDomainUnreachable):
		return ToolResult{Content: fmt.Sprintf("domain unreachable: %v", err), IsError: true}
	case errors.Is(err, ErrTooManyRedirects):
		return ToolResult{Content: fmt.Sprintf("too many redirects: %v", err), IsError: true}
	case errors.Is(err, ErrCrossHostRedirect):
		return ToolResult{Content: fmt.Sprintf("cross-host redirect refused: %v", err), IsError: true}
	case errors.Is(err, ErrBodyTooLarge):
		return ToolResult{Content: fmt.Sprintf("response body too large: %v", err), IsError: true}
	case errors.Is(err, ErrUnsupportedScheme):
		return ToolResult{Content: fmt.Sprintf("unsupported scheme: %v", err), IsError: true}
	default:
		return ToolResult{Content: fmt.Sprintf("fetch failed: %v", err), IsError: true}
	}
}

// normalizeURL validates and normalizes the URL, upgrading http to https.
func normalizeURL(rawURL string) (string, error) {
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
	if parsed.Scheme == "http" {
		parsed.Scheme = "https"
	}
	if parsed.Scheme != "https" {
		return "", fmt.Errorf("%w: %s", ErrUnsupportedScheme, parsed.Scheme)
	}
	return parsed.String(), nil
}

// buildHTTPClient returns a client whose CheckRedirect caps at
// maxRedirects and refuses redirects that cross to a different host
// (www-variant allowed so the common apex-to-www redirect works).
func buildHTTPClient() *http.Client {
	return &http.Client{
		Timeout: webFetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("%w: exceeded %d", ErrTooManyRedirects, maxRedirects)
			}
			if len(via) > 0 {
				if !sameHostOrWWWVariant(via[0].URL.Host, req.URL.Host) {
					return fmt.Errorf("%w: %s -> %s", ErrCrossHostRedirect, via[0].URL.Host, req.URL.Host)
				}
			}
			return nil
		},
	}
}

// sameHostOrWWWVariant returns true if a and b are the same host or
// differ only by the www. prefix.
func sameHostOrWWWVariant(a, b string) bool {
	if a == b {
		return true
	}
	if strings.TrimPrefix(a, "www.") == strings.TrimPrefix(b, "www.") {
		return true
	}
	return false
}

// fetchPage performs the HTTP GET with separate timeouts for the DNS
// preflight (10s) and the body fetch (60s), and returns distinguishable
// errors on every failure mode the tool surface cares about.
func fetchPage(ctx context.Context, fetchURL string) (string, string, error) {
	parsed, err := url.Parse(fetchURL)
	if err != nil {
		return "", "", err
	}

	// DNS preflight so unreachable domains fail fast.
	preCtx, preCancel := context.WithTimeout(ctx, webFetchDomainTimeout)
	defer preCancel()
	resolver := net.DefaultResolver
	if _, err := resolver.LookupHost(preCtx, parsed.Hostname()); err != nil {
		return "", "", fmt.Errorf("%w: %s: %v", ErrDomainUnreachable, parsed.Hostname(), err)
	}

	ctx, cancel := context.WithTimeout(ctx, webFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", webFetchUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.8")

	client := buildHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		// Redirect errors come back wrapped inside url.Error; unwrap so
		// errors.Is still works at the call site.
		var uerr *url.Error
		if errors.As(err, &uerr) && uerr.Err != nil {
			return "", "", uerr.Err
		}
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", "", fmt.Errorf("HTTP %d %s", resp.StatusCode, resp.Status)
	}

	limited := io.LimitReader(resp.Body, maxResponseBody+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return "", "", err
	}
	if len(data) > maxResponseBody {
		return "", "", fmt.Errorf("%w: > %d bytes", ErrBodyTooLarge, maxResponseBody)
	}

	return string(data), resp.Header.Get("Content-Type"), nil
}

// isBinaryContentType classifies a response as binary based on its
// Content-Type. Text, HTML, XML, JSON, JS, CSS stay inline.
func isBinaryContentType(ct string) bool {
	if ct == "" {
		return false
	}
	ct = strings.ToLower(ct)
	textualPrefixes := []string{
		"text/",
		"application/json",
		"application/xml",
		"application/xhtml",
		"application/javascript",
		"application/ecmascript",
		"application/ld+json",
		"application/x-ndjson",
	}
	for _, p := range textualPrefixes {
		if strings.HasPrefix(ct, p) {
			return false
		}
	}
	return true
}

// spillBinaryBody writes body to the downloads dir and returns the
// path. Extension derived from the Content-Type mime registry.
func spillBinaryBody(body, fromURL, contentType string) (string, error) {
	dir := defaultWebFetchDownloadsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	ext := ""
	if mediaType, _, err := mime.ParseMediaType(contentType); err == nil {
		exts, _ := mime.ExtensionsByType(mediaType)
		if len(exts) > 0 {
			ext = exts[0]
		}
	}
	if ext == "" {
		ext = ".bin"
	}

	host := ""
	if u, err := url.Parse(fromURL); err == nil {
		host = u.Hostname()
	}
	host = sanitiseForPath(host)
	if host == "" {
		host = "unknown"
	}

	path := filepath.Join(dir, fmt.Sprintf("%d-%s%s", time.Now().UnixMilli(), host, ext))
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// Pre-compiled regexes for HTML extraction.
var (
	reScript     = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	reStyle      = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	reComment    = regexp.MustCompile(`(?s)<!--.*?-->`)
	reNav        = regexp.MustCompile(`(?is)<nav[^>]*>.*?</nav>`)
	reHeader     = regexp.MustCompile(`(?is)<header[^>]*>.*?</header>`)
	reFooter     = regexp.MustCompile(`(?is)<footer[^>]*>.*?</footer>`)
	reAside      = regexp.MustCompile(`(?is)<aside[^>]*>.*?</aside>`)
	reBlockTag   = regexp.MustCompile(`(?i)<\s*/?\s*(br|p|div|h[1-6]|li|tr|blockquote|section|article|main)\b[^>]*>`)
	reAllTags    = regexp.MustCompile(`<[^>]+>`)
	reBlankLines = regexp.MustCompile(`\n{3,}`)
	reSpaces     = regexp.MustCompile(`[^\S\n]{2,}`)
)

// extractTextFromHTML strips HTML and returns clean readable text.
func extractTextFromHTML(html string) string {
	s := html
	s = reScript.ReplaceAllString(s, "")
	s = reStyle.ReplaceAllString(s, "")
	s = reComment.ReplaceAllString(s, "")
	s = reNav.ReplaceAllString(s, "")
	s = reHeader.ReplaceAllString(s, "")
	s = reFooter.ReplaceAllString(s, "")
	s = reAside.ReplaceAllString(s, "")
	s = reBlockTag.ReplaceAllString(s, "\n")
	s = reAllTags.ReplaceAllString(s, "")
	s = decodeHTMLEntities(s)
	s = reSpaces.ReplaceAllString(s, " ")
	s = reBlankLines.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
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
