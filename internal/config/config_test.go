package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `engine = "direct"
model = "opus"
theme = "night"
effort = "high"
token_budget = 100000
auto_title_enabled = true
dashboard_visible = true

[compact]
mode = "both"
trigger = "hybrid"
threshold_pct = 80
turn_count = 20
keep_recent_pct = 30
rolling_tokens = 50000
fast_tier_model = "haiku"
circuit_breaker = 3
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	c := LoadFromTOML(path)
	assert.Equal(t, "direct", c.Engine)
	assert.Equal(t, "opus", c.Model)
	assert.Equal(t, "night", c.Theme)
	assert.Equal(t, "high", c.Effort)
	assert.Equal(t, 100000, c.TokenBudget)
	assert.True(t, c.AutoTitleEnabled)
	assert.True(t, c.DashboardVisible)
	assert.Equal(t, "both", c.Compact.Mode)
	assert.Equal(t, "hybrid", c.Compact.Trigger)
	assert.Equal(t, 80, c.Compact.ThresholdPct)
	assert.Equal(t, 20, c.Compact.TurnCount)
	assert.Equal(t, 30, c.Compact.KeepRecentPct)
	assert.Equal(t, 50000, c.Compact.RollingTokens)
	assert.Equal(t, "haiku", c.Compact.FastTierModel)
	assert.Equal(t, 3, c.Compact.CircuitBreaker)
}

func TestLoadSaveRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	original := Config{
		Engine: "direct",
		Model:  "opus",
		Theme:  "night",
		Compact: CompactConfig{
			Mode:          "both",
			ThresholdPct:  80,
			CircuitBreaker: 3,
		},
	}

	require.NoError(t, original.SaveTo(path))

	loaded := LoadFromTOML(path)
	assert.Equal(t, original.Engine, loaded.Engine)
	assert.Equal(t, original.Model, loaded.Model)
	assert.Equal(t, original.Theme, loaded.Theme)
	assert.Equal(t, original.Compact.Mode, loaded.Compact.Mode)
	assert.Equal(t, original.Compact.ThresholdPct, loaded.Compact.ThresholdPct)
	assert.Equal(t, original.Compact.CircuitBreaker, loaded.Compact.CircuitBreaker)
}

func TestMigrateFromJSON(t *testing.T) {
	dir := t.TempDir()
	tomlPath := filepath.Join(dir, "config.toml")
	jsonPath := filepath.Join(dir, "config.json")

	// Write a legacy JSON config.
	legacy := Config{
		Engine: "claude",
		Model:  "sonnet",
		Theme:  "flame",
	}
	data, err := json.MarshalIndent(legacy, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(jsonPath, data, 0o644))

	// loadWithMigration should read JSON and write TOML.
	c := loadWithMigration(tomlPath, jsonPath)
	assert.Equal(t, "claude", c.Engine)
	assert.Equal(t, "sonnet", c.Model)
	assert.Equal(t, "flame", c.Theme)

	// TOML file should now exist.
	_, err = os.Stat(tomlPath)
	assert.NoError(t, err, "migration should create TOML file")

	// Second load should use TOML directly.
	c2 := loadWithMigration(tomlPath, jsonPath)
	assert.Equal(t, "claude", c2.Engine)
}

func TestDefaultValues(t *testing.T) {
	d := Defaults()
	assert.Equal(t, "claude", d.Engine)
	assert.Equal(t, "flame", d.Theme)
	assert.True(t, d.DashboardVisible)
	assert.Equal(t, "both", d.Compact.Mode)
	assert.Equal(t, "hybrid", d.Compact.Trigger)
	assert.Equal(t, 80, d.Compact.ThresholdPct)
	assert.Equal(t, 20, d.Compact.TurnCount)
	assert.Equal(t, 3, d.Compact.CircuitBreaker)
}

func TestLoadMissingFile(t *testing.T) {
	c := LoadFromTOML("/nonexistent/path/config.toml")
	assert.Equal(t, "", c.Engine)
	assert.Equal(t, "", c.Model)
}

func TestLoadCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("not valid toml [[["), 0o644))

	c := LoadFromTOML(path)
	assert.Equal(t, "", c.Engine)
}

func TestSaveCreatesDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "config.toml")

	c := Config{Engine: "claude"}
	require.NoError(t, c.SaveTo(path))

	loaded := LoadFromTOML(path)
	assert.Equal(t, "claude", loaded.Engine)
}
