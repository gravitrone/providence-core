package direct

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenCounterUsesCache(t *testing.T) {
	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("cache me")),
	}
	counter := newTokenCounter(tokenCounterConfig{
		enabled: true,
		apiKey:  "test-key",
		model:   "claude-sonnet-4-20250514",
	})

	var hits atomic.Int32
	redirectTokenCounterClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/messages/count_tokens", r.URL.Path)
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		assert.Equal(t, "test-key", r.Header.Get("x-api-key"))

		var req tokenCountRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "claude-sonnet-4-20250514", req.Model)
		require.Len(t, req.Messages, 1)
		assert.Equal(t, "cache me", req.Messages[0].Content[0].OfText.Text)

		w.Header().Set("content-type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(tokenCountResponse{InputTokens: 123}))
	}))

	first := counter.Count(context.Background(), messages)
	second := counter.Count(context.Background(), messages)

	assert.Equal(t, 123, first)
	assert.Equal(t, 123, second)
	assert.Equal(t, int32(1), hits.Load())
}

func TestTokenCounterFallsBackOnEndpointError(t *testing.T) {
	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("fallback me")),
	}
	counter := newTokenCounter(tokenCounterConfig{
		enabled: true,
		apiKey:  "test-key",
		model:   "claude-sonnet-4-20250514",
	})

	var hits atomic.Int32
	redirectTokenCounterClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))

	got := counter.Count(context.Background(), messages)

	assert.Equal(t, heuristicTokenCount(messages), got)
	assert.Equal(t, int32(1), hits.Load())
}

func TestTokenCounterHeuristicWhenDisabled(t *testing.T) {
	homeDir := t.TempDir()
	configDir := filepath.Join(homeDir, ".providence")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(`
model = "claude-sonnet-4-20250514"

[compact]
use_count_endpoint = false
`), 0o644))
	t.Setenv("HOME", homeDir)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("stay heuristic")),
	}

	var hits atomic.Int32
	redirectTokenCounterClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))

	counter := newDefaultTokenCounter()
	got := counter.Count(context.Background(), messages)

	assert.Equal(t, heuristicTokenCount(messages), got)
	assert.Zero(t, hits.Load())
}

func TestTokenCounterCtxCancel(t *testing.T) {
	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("cancel me")),
	}
	counter := newTokenCounter(tokenCounterConfig{
		enabled: true,
		apiKey:  "test-key",
		model:   "claude-sonnet-4-20250514",
	})

	var hits atomic.Int32
	redirectTokenCounterClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got := counter.Count(ctx, messages)

	assert.Equal(t, heuristicTokenCount(messages), got)
	assert.Zero(t, hits.Load())
}

func TestCurrentTokensUsesEndpointCounter(t *testing.T) {
	h := NewConversationHistory()
	h.counter = newTokenCounter(tokenCounterConfig{
		enabled: true,
		apiKey:  "test-key",
		model:   "claude-sonnet-4-20250514",
	})
	h.AddUser("count me precisely")

	var hits atomic.Int32
	redirectTokenCounterClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("content-type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(tokenCountResponse{InputTokens: 321}))
	}))

	assert.Equal(t, 321, h.CurrentTokens())
	assert.Equal(t, int32(1), hits.Load())
}

func redirectTokenCounterClient(t *testing.T, handler http.Handler) {
	t.Helper()

	server := newSandboxSafeServer(t, handler)
	if server == nil {
		return
	}

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	originalClient := providerHTTPClient
	providerHTTPClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			cloned := req.Clone(req.Context())
			cloned.URL.Scheme = serverURL.Scheme
			cloned.URL.Host = serverURL.Host
			return http.DefaultTransport.RoundTrip(cloned)
		}),
	}

	t.Cleanup(func() {
		providerHTTPClient = originalClient
		server.Close()
	})
}
