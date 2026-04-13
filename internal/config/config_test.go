package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTestConfigFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

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
			Mode:           "both",
			ThresholdPct:   80,
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

func TestLoadMergedPriority(t *testing.T) {
	homeDir := t.TempDir()
	projectRoot := t.TempDir()
	t.Setenv("HOME", homeDir)

	writeTestConfigFile(t, filepath.Join(homeDir, ".providence", "config.toml"), `engine = "claude"
model = "user-model"
theme = "user-theme"
output_style = "verbose"
auto_title_enabled = true

[compact]
mode = "both"
trigger = "token"
threshold_pct = 70
turn_count = 12
keep_recent_pct = 25
rolling_tokens = 40000
fast_tier_model = "haiku"
circuit_breaker = 2
`)

	writeTestConfigFile(t, filepath.Join(projectRoot, ".providence", "config.toml"), `model = "project-model"
theme = "project-theme"
token_budget = 120000
dashboard_visible = true
tool_use_summary = true

[compact]
trigger = "pressure"
turn_count = 18
keep_recent_pct = 35
`)

	writeTestConfigFile(t, filepath.Join(projectRoot, ".providence", "config.local.toml"), `theme = "local-theme"
effort = "high"
openrouter_api_key = "local-key"

[compact]
threshold_pct = 90
rolling_tokens = 60000
circuit_breaker = 5
`)

	loaded := LoadMerged(projectRoot)

	assert.Equal(t, "claude", loaded.Engine)
	assert.Equal(t, "project-model", loaded.Model)
	assert.Equal(t, "local-theme", loaded.Theme)
	assert.Equal(t, "high", loaded.Effort)
	assert.Equal(t, "local-key", loaded.OpenRouterAPIKey)
	assert.Equal(t, 120000, loaded.TokenBudget)
	assert.True(t, loaded.AutoTitleEnabled)
	assert.True(t, loaded.ToolUseSummary)
	assert.True(t, loaded.DashboardVisible)
	assert.Equal(t, "verbose", loaded.OutputStyle)
	assert.Equal(t, "both", loaded.Compact.Mode)
	assert.Equal(t, "pressure", loaded.Compact.Trigger)
	assert.Equal(t, 90, loaded.Compact.ThresholdPct)
	assert.Equal(t, 18, loaded.Compact.TurnCount)
	assert.Equal(t, 35, loaded.Compact.KeepRecentPct)
	assert.Equal(t, 60000, loaded.Compact.RollingTokens)
	assert.Equal(t, "haiku", loaded.Compact.FastTierModel)
	assert.Equal(t, 5, loaded.Compact.CircuitBreaker)
}

func TestCompactConfigDefaults(t *testing.T) {
	compact := Defaults().Compact

	assert.Equal(t, "both", compact.Mode)
	assert.Equal(t, "hybrid", compact.Trigger)
	assert.Equal(t, 80, compact.ThresholdPct)
	assert.Equal(t, 20, compact.TurnCount)
	assert.Equal(t, 30, compact.KeepRecentPct)
	assert.Equal(t, 50000, compact.RollingTokens)
	assert.Equal(t, "haiku", compact.FastTierModel)
	assert.Equal(t, 3, compact.CircuitBreaker)
}

func TestConfigSaveRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	original := Config{
		Engine:           "direct",
		Model:            "gpt-5.4",
		Theme:            "flame",
		Effort:           "high",
		OpenRouterAPIKey: "openrouter-secret",
		TokenBudget:      123456,
		AutoTitleEnabled: true,
		ToolUseSummary:   true,
		DashboardVisible: true,
		BGAgentsEnabled:  true,
		OutputStyle:      "compact",
		Compact: CompactConfig{
			Mode:           "dynamic-rolling",
			Trigger:        "pressure",
			ThresholdPct:   88,
			TurnCount:      9,
			KeepRecentPct:  40,
			RollingTokens:  64000,
			FastTierModel:  "haiku",
			CircuitBreaker: 7,
		},
	}

	require.NoError(t, original.SaveTo(path))

	loaded := LoadFromTOML(path)
	assert.Equal(t, original, loaded)
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

func TestPermissionsConfigParse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `engine = "direct"

[permissions]
mode = "default"
allow = ["Read(*)", "Glob(*)", "Grep(*)"]
deny = ["Bash(rm -rf *)"]
ask = ["Bash(git push *)"]
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	c := LoadFromTOML(path)
	assert.Equal(t, "default", c.Permissions.Mode)
	assert.Equal(t, []string{"Read(*)", "Glob(*)", "Grep(*)"}, c.Permissions.Allow)
	assert.Equal(t, []string{"Bash(rm -rf *)"}, c.Permissions.Deny)
	assert.Equal(t, []string{"Bash(git push *)"}, c.Permissions.Ask)
}

func TestPermissionsConfigRules(t *testing.T) {
	pc := PermissionsConfig{
		Allow: []string{"Read(*)", "Glob(*)"},
		Deny:  []string{"Bash(rm -rf *)"},
		Ask:   []string{"Bash(git push *)"},
	}

	allowRules := pc.AllowRules("config")
	assert.Len(t, allowRules, 2)
	assert.Equal(t, "Read(*)", allowRules[0].Pattern)
	assert.Equal(t, "allow", allowRules[0].Behavior)
	assert.Equal(t, "config", allowRules[0].Source)

	denyRules := pc.DenyRules("config")
	assert.Len(t, denyRules, 1)
	assert.Equal(t, "Bash(rm -rf *)", denyRules[0].Pattern)

	askRules := pc.AskRules("config")
	assert.Len(t, askRules, 1)
	assert.Equal(t, "Bash(git push *)", askRules[0].Pattern)
}

func TestPermissionsConfigMerge(t *testing.T) {
	homeDir := t.TempDir()
	projectRoot := t.TempDir()
	t.Setenv("HOME", homeDir)

	writeTestConfigFile(t, filepath.Join(homeDir, ".providence", "config.toml"), `
[permissions]
mode = "default"
allow = ["Read(*)", "Glob(*)"]
deny = ["Bash(rm -rf *)"]
`)

	writeTestConfigFile(t, filepath.Join(projectRoot, ".providence", "config.toml"), `
[permissions]
mode = "acceptEdits"
allow = ["Write(*)", "Edit(*)"]
`)

	loaded := LoadMerged(projectRoot)
	assert.Equal(t, "acceptEdits", loaded.Permissions.Mode)
	// Project overrides should replace user-level allow list.
	assert.Equal(t, []string{"Write(*)", "Edit(*)"}, loaded.Permissions.Allow)
	// Deny from user-level should survive since project didn't override it.
	assert.Equal(t, []string{"Bash(rm -rf *)"}, loaded.Permissions.Deny)
}

func TestSaveCreatesDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "config.toml")

	c := Config{Engine: "claude"}
	require.NoError(t, c.SaveTo(path))

	loaded := LoadFromTOML(path)
	assert.Equal(t, "claude", loaded.Engine)
}
