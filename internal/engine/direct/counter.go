package direct

import (
	"bytes"
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/gravitrone/providence-core/internal/config"
)

const (
	anthropicCountTokensURL = "https://api.anthropic.com/v1/messages/count_tokens"
	tokenCountCacheSize     = 256
)

type tokenCounter struct {
	enabled bool
	apiKey  string
	model   string
	cache   *tokenCountCache
}

type tokenCounterConfig struct {
	enabled bool
	apiKey  string
	model   string
}

type tokenCounterFileConfig struct {
	Model   string                        `toml:"model"`
	Compact tokenCounterCompactFileConfig `toml:"compact"`
}

type tokenCounterCompactFileConfig struct {
	UseCountEndpoint *bool `toml:"use_count_endpoint"`
}

type tokenCountRequest struct {
	Model    string                   `json:"model"`
	Messages []anthropic.MessageParam `json:"messages"`
}

type tokenCountResponse struct {
	InputTokens int `json:"input_tokens"`
}

type tokenCountCache struct {
	capacity int
	order    *list.List
	entries  map[string]*list.Element
	mu       sync.Mutex
}

type tokenCountCacheEntry struct {
	key   string
	count int
}

func newDefaultTokenCounter() *tokenCounter {
	cfg := tokenCounterConfig{
		enabled: true,
		apiKey:  strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")),
		model:   string(anthropic.ModelClaudeSonnet4_20250514),
	}

	fileCfg, err := loadTokenCounterFileConfig()
	if err == nil {
		if fileCfg.Model != "" {
			cfg.model = fileCfg.Model
		}
		if fileCfg.Compact.UseCountEndpoint != nil {
			cfg.enabled = *fileCfg.Compact.UseCountEndpoint
		}
	}

	return newTokenCounter(cfg)
}

func newTokenCounter(cfg tokenCounterConfig) *tokenCounter {
	model := strings.TrimSpace(cfg.model)
	if model == "" {
		model = string(anthropic.ModelClaudeSonnet4_20250514)
	}

	return &tokenCounter{
		enabled: cfg.enabled && supportsAnthropicCountEndpoint(model),
		apiKey:  strings.TrimSpace(cfg.apiKey),
		model:   model,
		cache:   newTokenCountCache(tokenCountCacheSize),
	}
}

func loadTokenCounterFileConfig() (tokenCounterFileConfig, error) {
	path := config.DefaultPath()
	if path == "" {
		return tokenCounterFileConfig{}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return tokenCounterFileConfig{}, nil
		}
		return tokenCounterFileConfig{}, fmt.Errorf("read token counter config: %w", err)
	}

	var cfg tokenCounterFileConfig
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return tokenCounterFileConfig{}, fmt.Errorf("decode token counter config: %w", err)
	}

	return cfg, nil
}

func supportsAnthropicCountEndpoint(model string) bool {
	normalized := strings.ToLower(strings.TrimSpace(model))
	return normalized != "" &&
		strings.HasPrefix(normalized, "claude") &&
		!strings.Contains(normalized, "/")
}

func heuristicTokenCount(messages []anthropic.MessageParam) int {
	charCount := 0
	for _, msg := range messages {
		for _, block := range msg.Content {
			if block.OfText != nil {
				charCount += len(block.OfText.Text)
			}
			if block.OfToolResult != nil {
				for _, inner := range block.OfToolResult.Content {
					if inner.OfText != nil {
						charCount += len(inner.OfText.Text)
					}
				}
			}
		}
	}

	return charCount * 4 / 3
}

func newTokenCountCache(capacity int) *tokenCountCache {
	return &tokenCountCache{
		capacity: capacity,
		order:    list.New(),
		entries:  make(map[string]*list.Element, capacity),
	}
}

func (c *tokenCountCache) Get(key string) (int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	element, ok := c.entries[key]
	if !ok {
		return 0, false
	}

	c.order.MoveToFront(element)
	entry := element.Value.(tokenCountCacheEntry)
	return entry.count, true
}

func (c *tokenCountCache) Add(key string, count int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if element, ok := c.entries[key]; ok {
		element.Value = tokenCountCacheEntry{key: key, count: count}
		c.order.MoveToFront(element)
		return
	}

	element := c.order.PushFront(tokenCountCacheEntry{key: key, count: count})
	c.entries[key] = element
	if c.order.Len() <= c.capacity {
		return
	}

	tail := c.order.Back()
	if tail == nil {
		return
	}

	entry := tail.Value.(tokenCountCacheEntry)
	delete(c.entries, entry.key)
	c.order.Remove(tail)
}

func (c *tokenCounter) Count(ctx context.Context, messages []anthropic.MessageParam) int {
	if len(messages) == 0 {
		return 0
	}

	heuristic := heuristicTokenCount(messages)
	if c == nil || !c.enabled || c.apiKey == "" {
		return heuristic
	}

	select {
	case <-ctx.Done():
		return heuristic
	default:
	}

	cacheKey, err := hashMessages(messages)
	if err != nil {
		return heuristic
	}

	if count, ok := c.cache.Get(cacheKey); ok {
		return count
	}

	count, err := c.fetch(ctx, messages)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("warning: count tokens endpoint failed, using heuristic: %v", err)
		}
		return heuristic
	}

	c.cache.Add(cacheKey, count)
	return count
}

func hashMessages(messages []anthropic.MessageParam) (string, error) {
	payload, err := json.Marshal(messages)
	if err != nil {
		return "", fmt.Errorf("marshal messages for token hash: %w", err)
	}

	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func (c *tokenCounter) fetch(ctx context.Context, messages []anthropic.MessageParam) (int, error) {
	payload, err := json.Marshal(tokenCountRequest{
		Model:    c.model,
		Messages: messages,
	})
	if err != nil {
		return 0, fmt.Errorf("marshal count tokens request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicCountTokensURL, bytes.NewReader(payload))
	if err != nil {
		return 0, fmt.Errorf("build count tokens request: %w", err)
	}

	req.Header.Set("content-type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("x-api-key", c.apiKey)

	resp, err := providerHTTPClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, fmt.Errorf("post count tokens request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if text := strings.TrimSpace(string(body)); text != "" {
			return 0, fmt.Errorf("count tokens endpoint returned %s: %s", resp.Status, text)
		}
		return 0, fmt.Errorf("count tokens endpoint returned %s", resp.Status)
	}

	var result tokenCountResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode count tokens response: %w", err)
	}

	return result.InputTokens, nil
}
