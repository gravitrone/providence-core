package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelCacheRoundtrip(t *testing.T) {
	models := []modelCacheEntry{
		{ID: "openai/gpt-4", Name: "GPT-4", ContextLen: 128000, Description: "OpenAI flagship"},
		{ID: "anthropic/claude-3.5-sonnet", Name: "Claude 3.5 Sonnet", ContextLen: 200000},
	}
	cache := modelCache{
		FetchedAt: time.Now(),
		Models:    models,
	}

	data, err := json.Marshal(cache)
	require.NoError(t, err)

	var decoded modelCache
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Len(t, decoded.Models, 2)
	assert.Equal(t, "openai/gpt-4", decoded.Models[0].ID)
	assert.Equal(t, "GPT-4", decoded.Models[0].Name)
	assert.Equal(t, 128000, decoded.Models[0].ContextLen)
	assert.Equal(t, "OpenAI flagship", decoded.Models[0].Description)
}

func TestModelCacheLoadFromDisk(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "model_cache.json")

	models := []modelCacheEntry{
		{ID: "test/model-a", Name: "Model A", ContextLen: 32000},
	}
	cache := modelCache{
		FetchedAt: time.Now(),
		Models:    models,
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(cachePath, data, 0o644))

	// Read it back.
	readData, err := os.ReadFile(cachePath)
	require.NoError(t, err)

	var loaded modelCache
	require.NoError(t, json.Unmarshal(readData, &loaded))
	assert.Len(t, loaded.Models, 1)
	assert.Equal(t, "test/model-a", loaded.Models[0].ID)
}

func TestModelCacheTTLFresh(t *testing.T) {
	cache := modelCache{
		FetchedAt: time.Now(),
		Models:    []modelCacheEntry{{ID: "x"}},
	}
	elapsed := time.Since(cache.FetchedAt)
	assert.True(t, elapsed < modelCacheTTL, "freshly created cache should not be stale")
}

func TestModelCacheTTLStale(t *testing.T) {
	cache := modelCache{
		FetchedAt: time.Now().Add(-2 * time.Hour),
		Models:    []modelCacheEntry{{ID: "x"}},
	}
	elapsed := time.Since(cache.FetchedAt)
	assert.True(t, elapsed >= modelCacheTTL, "cache from 2 hours ago should be stale")
}

func TestModelCacheTTLEdge(t *testing.T) {
	// Exactly at the boundary.
	cache := modelCache{
		FetchedAt: time.Now().Add(-modelCacheTTL),
	}
	elapsed := time.Since(cache.FetchedAt)
	assert.True(t, elapsed >= modelCacheTTL, "cache at exact TTL should be stale")
}

func TestModelCacheMissingFileGraceful(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent", "cache.json")
	_, err := os.ReadFile(path)
	assert.Error(t, err, "reading nonexistent cache file should error")
	assert.True(t, os.IsNotExist(err))
}

func TestModelCacheInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0o644))

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var cache modelCache
	err = json.Unmarshal(data, &cache)
	assert.Error(t, err, "invalid JSON should fail to unmarshal")
}

func TestFormatModelCatalogEmpty(t *testing.T) {
	out := formatModelCatalog(nil)
	assert.Equal(t, "No models available from OpenRouter", out)
}

func TestFormatModelCatalogEmptySlice(t *testing.T) {
	out := formatModelCatalog([]modelCacheEntry{})
	assert.Equal(t, "No models available from OpenRouter", out)
}

func TestFormatModelCatalogSingleModel(t *testing.T) {
	models := []modelCacheEntry{
		{ID: "openai/gpt-4o", Name: "GPT-4o", ContextLen: 128000},
	}
	out := formatModelCatalog(models)

	assert.Contains(t, out, "OpenRouter Models")
	assert.Contains(t, out, "openai/gpt-4o")
	assert.Contains(t, out, "GPT-4o")
	assert.Contains(t, out, "128k")
	assert.Contains(t, out, "Total: 1 models")
}

func TestFormatModelCatalogTruncatesLongName(t *testing.T) {
	longName := "This Is An Extremely Long Model Name That Exceeds Thirty Characters"
	models := []modelCacheEntry{
		{ID: "test/long", Name: longName, ContextLen: 8000},
	}
	out := formatModelCatalog(models)

	// Name should be truncated to 27 chars + "..."
	assert.Contains(t, out, "...")
	assert.NotContains(t, out, longName)
}

func TestFormatModelCatalogLimitsTo30(t *testing.T) {
	models := make([]modelCacheEntry, 50)
	for i := range models {
		models[i] = modelCacheEntry{
			ID:         "test/model-" + string(rune('a'+i%26)),
			Name:       "Model",
			ContextLen: 4000,
		}
	}
	out := formatModelCatalog(models)

	assert.Contains(t, out, "...and 20 more models")
	assert.Contains(t, out, "Total: 50 models")
}

func TestFormatModelCatalogExact30(t *testing.T) {
	models := make([]modelCacheEntry, 30)
	for i := range models {
		models[i] = modelCacheEntry{
			ID:         "test/m",
			Name:       "M",
			ContextLen: 1000,
		}
	}
	out := formatModelCatalog(models)

	assert.NotContains(t, out, "...and")
	assert.Contains(t, out, "Total: 30 models")
}

func TestFormatModelCatalogContextFormatting(t *testing.T) {
	models := []modelCacheEntry{
		{ID: "a", Name: "A", ContextLen: 200000},
		{ID: "b", Name: "B", ContextLen: 4096},
		{ID: "c", Name: "C", ContextLen: 500},
	}
	out := formatModelCatalog(models)

	assert.Contains(t, out, "200k")
	assert.Contains(t, out, "4k")
	assert.Contains(t, out, "0k") // 500/1000 rounds down to 0
}

func TestModelCacheEntryOmitsEmptyDescription(t *testing.T) {
	entry := modelCacheEntry{ID: "test/x", Name: "X", ContextLen: 1000}
	data, err := json.Marshal(entry)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "description")

	entryWithDesc := modelCacheEntry{ID: "test/y", Name: "Y", ContextLen: 1000, Description: "has one"}
	data, err = json.Marshal(entryWithDesc)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"description":"has one"`)
}
