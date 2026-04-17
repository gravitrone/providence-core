package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebSearchMissingQuery(t *testing.T) {
	tool := &WebSearchTool{}
	res := tool.Execute(context.Background(), map[string]any{})
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "query is required")
}

func TestWebSearchEmptyQuery(t *testing.T) {
	tool := &WebSearchTool{}
	res := tool.Execute(context.Background(), map[string]any{"query": ""})
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "query is required")
}

func TestWebSearchMissingAPIKey(t *testing.T) {
	t.Setenv("EXA_API_KEY", "")
	tool := &WebSearchTool{}
	res := tool.Execute(context.Background(), map[string]any{"query": "test"})
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "EXA_API_KEY not set")
	assert.Contains(t, res.Content, "exa.ai")
}

func TestWebSearchAllowedDomainsFilter(t *testing.T) {
	tool := newWebSearchToolForResponses(t, WebSearchConfig{
		AllowedDomains: []string{"example.com"},
	}, exaResponse{
		Results: []exaResult{
			{Title: "Allowed", URL: "https://blog.example.com/post", Text: "keep"},
			{Title: "Blocked", URL: "https://other.com/post", Text: "drop"},
		},
	})

	res := tool.Execute(context.Background(), map[string]any{"query": "test"})

	assert.False(t, res.IsError)
	assert.Contains(t, res.Content, "Allowed")
	assert.Contains(t, res.Content, "https://blog.example.com/post")
	assert.NotContains(t, res.Content, "Blocked")
	assert.NotContains(t, res.Content, "https://other.com/post")
}

func TestWebSearchBlockedDomainsFilter(t *testing.T) {
	tool := newWebSearchToolForResponses(t, WebSearchConfig{
		BlockedDomains: []string{"example.com"},
	}, exaResponse{
		Results: []exaResult{
			{Title: "Blocked", URL: "https://www.example.com/post", Text: "drop"},
			{Title: "Allowed", URL: "https://keep.com/post", Text: "keep"},
		},
	})

	res := tool.Execute(context.Background(), map[string]any{"query": "test"})

	assert.False(t, res.IsError)
	assert.Contains(t, res.Content, "Allowed")
	assert.Contains(t, res.Content, "https://keep.com/post")
	assert.NotContains(t, res.Content, "Blocked")
	assert.NotContains(t, res.Content, "https://www.example.com/post")
}

func TestWebSearchBlockedWinsOverAllowed(t *testing.T) {
	tool := newWebSearchToolForResponses(t, WebSearchConfig{
		AllowedDomains: []string{"example.com"},
		BlockedDomains: []string{"example.com"},
	}, exaResponse{
		Results: []exaResult{
			{Title: "Blocked", URL: "https://docs.example.com/post", Text: "drop"},
		},
	})

	res := tool.Execute(context.Background(), map[string]any{"query": "test"})

	assert.False(t, res.IsError)
	assert.Equal(t, "No results found.", res.Content)
}

func TestWebSearchMaxUsesExceeded(t *testing.T) {
	var requestCount atomic.Int32
	tool := newWebSearchToolWithHandler(t, WebSearchConfig{
		MaxUses: 1,
	}, func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(exaResponse{
			Results: []exaResult{{Title: "Allowed", URL: "https://example.com/post", Text: "keep"}},
		}))
	})

	first := tool.Execute(context.Background(), map[string]any{"query": "test"})
	second := tool.Execute(context.Background(), map[string]any{"query": "test"})

	assert.False(t, first.IsError)
	assert.True(t, second.IsError)
	assert.Equal(t, "websearch: max_uses 1 exceeded for this session", second.Content)
	assert.EqualValues(t, 1, requestCount.Load())
}

func TestWebSearchDedupesResults(t *testing.T) {
	tool := newWebSearchToolForResponses(t, WebSearchConfig{},
		exaResponse{
			Results: []exaResult{
				{Title: "First", URL: "https://example.com/shared", Text: "keep"},
			},
		},
		exaResponse{
			Results: []exaResult{
				{Title: "Duplicate", URL: "https://example.com/shared", Text: "drop"},
				{Title: "Second", URL: "https://example.com/new", Text: "keep"},
			},
		},
	)

	first := tool.Execute(context.Background(), map[string]any{"query": "test"})
	second := tool.Execute(context.Background(), map[string]any{"query": "test"})

	assert.False(t, first.IsError)
	assert.Contains(t, first.Content, "First")
	assert.Contains(t, first.Content, "https://example.com/shared")

	assert.False(t, second.IsError)
	assert.Contains(t, second.Content, "Second")
	assert.Contains(t, second.Content, "https://example.com/new")
	assert.NotContains(t, second.Content, "Duplicate")
	assert.NotContains(t, second.Content, "https://example.com/shared")
}

func TestWebSearchFormatResults(t *testing.T) {
	results := []exaResult{
		{Title: "Go Programming", URL: "https://go.dev", Text: "The Go programming language.", Score: 0.95},
		{Title: "Rust Lang", URL: "https://rust-lang.org", Text: "A systems programming language.", Score: 0.88},
	}

	out := formatExaResults(results)

	assert.Contains(t, out, "1. Go Programming")
	assert.Contains(t, out, "   https://go.dev")
	assert.Contains(t, out, "   The Go programming language.")
	assert.Contains(t, out, "2. Rust Lang")
	assert.Contains(t, out, "   https://rust-lang.org")
	assert.Contains(t, out, "   A systems programming language.")
}

func TestWebSearchFormatResultsEmptyText(t *testing.T) {
	results := []exaResult{
		{Title: "No Content", URL: "https://example.com", Text: "", Score: 0.5},
	}

	out := formatExaResults(results)
	assert.Contains(t, out, "1. No Content")
	assert.Contains(t, out, "   https://example.com")
	// should not have a trailing content line
	lines := 0
	for _, line := range splitLines(out) {
		if line != "" {
			lines++
		}
	}
	assert.Equal(t, 2, lines) // title + url only
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func TestWebSearchNumResultsClamped(t *testing.T) {
	// spin up a mock server to verify num_results clamping
	var receivedReq exaRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedReq)
		resp := exaResponse{Results: []exaResult{{Title: "Result", URL: "https://example.com", Text: "content"}}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	setExaURL(t, srv.URL)

	t.Setenv("EXA_API_KEY", "test-key")
	tool := &WebSearchTool{}

	// num_results > 10 should be clamped to 10
	res := tool.Execute(context.Background(), map[string]any{
		"query":       "test",
		"num_results": float64(50),
	})
	assert.False(t, res.IsError)
	assert.Equal(t, 10, receivedReq.NumResults)

	// num_results < 1 should be clamped to 1
	res = tool.Execute(context.Background(), map[string]any{
		"query":       "test",
		"num_results": float64(0),
	})
	assert.False(t, res.IsError)
	assert.Equal(t, 1, receivedReq.NumResults)
}

func TestWebSearchMockAPICall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// verify headers
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "test-api-key", r.Header.Get("x-api-key"))
		assert.Equal(t, http.MethodPost, r.Method)

		var req exaRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "golang concurrency", req.Query)
		assert.Equal(t, 3, req.NumResults)

		resp := exaResponse{
			Results: []exaResult{
				{Title: "Go Concurrency", URL: "https://go.dev/conc", Text: "Goroutines and channels.", Score: 0.97},
				{Title: "Concurrency Patterns", URL: "https://example.com/patterns", Text: "Common patterns.", Score: 0.91},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	setExaURL(t, srv.URL)
	t.Setenv("EXA_API_KEY", "test-api-key")

	tool := &WebSearchTool{}
	res := tool.Execute(context.Background(), map[string]any{
		"query":       "golang concurrency",
		"num_results": float64(3),
	})

	assert.False(t, res.IsError)
	assert.Contains(t, res.Content, "1. Go Concurrency")
	assert.Contains(t, res.Content, "https://go.dev/conc")
	assert.Contains(t, res.Content, "2. Concurrency Patterns")
}

func TestWebSearchNoResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := exaResponse{Results: []exaResult{}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	setExaURL(t, srv.URL)
	t.Setenv("EXA_API_KEY", "test-key")

	tool := &WebSearchTool{}
	res := tool.Execute(context.Background(), map[string]any{"query": "obscure query"})
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content, "No results found")
}

func TestWebSearchAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	setExaURL(t, srv.URL)
	t.Setenv("EXA_API_KEY", "test-key")

	tool := &WebSearchTool{}
	res := tool.Execute(context.Background(), map[string]any{"query": "test"})
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "429")
}

func TestWebSearchReadOnly(t *testing.T) {
	tool := &WebSearchTool{}
	assert.True(t, tool.ReadOnly())
}

func TestWebSearchName(t *testing.T) {
	tool := &WebSearchTool{}
	assert.Equal(t, "WebSearch", tool.Name())
}

func newWebSearchToolForResponses(t *testing.T, cfg WebSearchConfig, responses ...exaResponse) *WebSearchTool {
	t.Helper()

	var (
		mu    sync.Mutex
		index int
	)

	return newWebSearchToolWithHandler(t, cfg, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		require.Less(t, index, len(responses), "unexpected extra request")
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(responses[index]))
		index++
	})
}

func newWebSearchToolWithHandler(t *testing.T, cfg WebSearchConfig, handler http.HandlerFunc) *WebSearchTool {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	setExaURL(t, srv.URL)
	t.Setenv("EXA_API_KEY", "test-key")

	return &WebSearchTool{Config: cfg}
}

// setExaURL overrides the Exa search endpoint for this test.
func setExaURL(t *testing.T, url string) {
	t.Helper()
	exaSearchURLMu.Lock()
	orig := exaSearchURL
	exaSearchURL = url
	exaSearchURLMu.Unlock()
	t.Cleanup(func() {
		exaSearchURLMu.Lock()
		exaSearchURL = orig
		exaSearchURLMu.Unlock()
	})
}
