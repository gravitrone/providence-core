package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// modelCacheEntry is a single model from the OpenRouter catalog.
type modelCacheEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContextLen  int    `json:"context_length"`
	Description string `json:"description,omitempty"`
}

// modelCache is the on-disk cache format.
type modelCache struct {
	FetchedAt time.Time         `json:"fetched_at"`
	Models    []modelCacheEntry `json:"models"`
}

// modelCacheTTL is the cache validity duration.
const modelCacheTTL = 1 * time.Hour

// modelCachePath returns ~/.providence/model_cache.json.
func modelCachePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".providence", "model_cache.json")
}

// fetchModelCatalogMsg is the tea.Msg carrying the catalog result.
type fetchModelCatalogMsg struct {
	Models []modelCacheEntry
	Err    error
}

// fetchModelCatalog fetches available models from the OpenRouter API
// and caches the result. Returns cached data if still valid.
func fetchModelCatalog(apiKey string) ([]modelCacheEntry, error) {
	// Check cache first.
	cachePath := modelCachePath()
	if data, err := os.ReadFile(cachePath); err == nil {
		var cache modelCache
		if err := json.Unmarshal(data, &cache); err == nil {
			if time.Since(cache.FetchedAt) < modelCacheTTL {
				return cache.Models, nil
			}
		}
	}

	// Fetch from OpenRouter.
	req, err := http.NewRequest("GET", "https://openrouter.ai/api/v1/models", nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OpenRouter API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("OpenRouter API %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			ContextLength int    `json:"context_length"`
			Description   string `json:"description"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parse models: %w", err)
	}

	models := make([]modelCacheEntry, 0, len(result.Data))
	for _, m := range result.Data {
		models = append(models, modelCacheEntry{
			ID:          m.ID,
			Name:        m.Name,
			ContextLen:  m.ContextLength,
			Description: m.Description,
		})
	}

	// Write cache.
	cache := modelCache{
		FetchedAt: time.Now(),
		Models:    models,
	}
	cacheData, err := json.MarshalIndent(cache, "", "  ")
	if err == nil {
		_ = os.MkdirAll(filepath.Dir(cachePath), 0o755)
		_ = os.WriteFile(cachePath, cacheData, 0o644)
	}

	return models, nil
}

// formatModelCatalog formats the fetched model catalog for display.
func formatModelCatalog(models []modelCacheEntry) string {
	if len(models) == 0 {
		return "No models available from OpenRouter"
	}

	var b strings.Builder
	b.WriteString("OpenRouter Models\n\n")
	b.WriteString(fmt.Sprintf("  %-45s  %-8s  %s\n", "Model ID", "Context", "Name"))
	b.WriteString(fmt.Sprintf("  %-45s  %-8s  %s\n", strings.Repeat("-", 45), strings.Repeat("-", 8), strings.Repeat("-", 20)))

	// Show at most 30 models to avoid overwhelming the chat.
	limit := 30
	if len(models) < limit {
		limit = len(models)
	}
	for _, m := range models[:limit] {
		ctx := fmt.Sprintf("%dk", m.ContextLen/1000)
		name := m.Name
		if len(name) > 30 {
			name = name[:27] + "..."
		}
		b.WriteString(fmt.Sprintf("  %-45s  %-8s  %s\n", m.ID, ctx, name))
	}
	if len(models) > limit {
		b.WriteString(fmt.Sprintf("\n  ...and %d more models (cached at ~/.providence/model_cache.json)", len(models)-limit))
	}
	b.WriteString(fmt.Sprintf("\n\nTotal: %d models", len(models)))
	return b.String()
}
