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

// --- BridgeConfig Tests ---

func TestBridgeConfigDefaults(t *testing.T) {
	d := Defaults()
	b := d.Bridge
	assert.Equal(t, "auto", b.Mode)
	assert.Equal(t, 2, b.WarmStreamFPS)
	assert.Equal(t, 30, b.BurstStreamFPS)
	assert.True(t, b.ActionBatch)
	assert.True(t, b.ScreenDiffEnabled)
	assert.Equal(t, 12, b.AXMaxDepth)
	assert.Equal(t, 2000, b.AXMaxNodes)
	assert.Equal(t, 1500, b.SpawnTimeoutMS)
}

func TestBridgeConfigTOMLRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
[bridge]
mode = "swift"
swift_path = "/usr/local/bin/providence-mac-bridge"
warm_stream_fps = 5
burst_stream_fps = 30
action_batch = true
screen_diff_enabled = true
ax_max_depth = 15
ax_max_nodes = 3000
spawn_timeout_ms = 2000
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	c := LoadFromTOML(path)
	assert.Equal(t, "swift", c.Bridge.Mode)
	assert.Equal(t, "/usr/local/bin/providence-mac-bridge", c.Bridge.SwiftPath)
	assert.Equal(t, 5, c.Bridge.WarmStreamFPS)
	assert.Equal(t, 30, c.Bridge.BurstStreamFPS)
	assert.True(t, c.Bridge.ActionBatch)
	assert.True(t, c.Bridge.ScreenDiffEnabled)
	assert.Equal(t, 15, c.Bridge.AXMaxDepth)
	assert.Equal(t, 3000, c.Bridge.AXMaxNodes)
	assert.Equal(t, 2000, c.Bridge.SpawnTimeoutMS)
}

func TestBridgeConfigMerge(t *testing.T) {
	base := Config{
		Bridge: BridgeConfig{
			Mode:           "auto",
			WarmStreamFPS:  2,
			BurstStreamFPS: 30,
			AXMaxDepth:     12,
			AXMaxNodes:     2000,
			SpawnTimeoutMS: 1500,
		},
	}
	override := Config{
		Bridge: BridgeConfig{
			Mode:           "swift",
			WarmStreamFPS:  5,
			SpawnTimeoutMS: 3000,
		},
	}
	mergeConfig(&base, &override)
	assert.Equal(t, "swift", base.Bridge.Mode)
	assert.Equal(t, 5, base.Bridge.WarmStreamFPS)
	assert.Equal(t, 30, base.Bridge.BurstStreamFPS, "unset override should not clear base")
	assert.Equal(t, 3000, base.Bridge.SpawnTimeoutMS)
	assert.Equal(t, 2000, base.Bridge.AXMaxNodes, "unset override should not clear base")
}

func TestBridgeConfigValidation(t *testing.T) {
	tests := []struct {
		name      string
		cfg       BridgeConfig
		wantError bool
		errFrag   string
	}{
		{
			name: "valid auto mode",
			cfg:  BridgeConfig{Mode: "auto", WarmStreamFPS: 2, BurstStreamFPS: 30, AXMaxNodes: 2000, SpawnTimeoutMS: 1500},
		},
		{
			name: "valid swift mode",
			cfg:  BridgeConfig{Mode: "swift", SpawnTimeoutMS: 500},
		},
		{
			name: "valid shell mode",
			cfg:  BridgeConfig{Mode: "shell"},
		},
		{
			name:      "invalid mode",
			cfg:       BridgeConfig{Mode: "invalid"},
			wantError: true,
			errFrag:   "bridge.mode",
		},
		{
			name:      "warm fps out of range",
			cfg:       BridgeConfig{WarmStreamFPS: 61},
			wantError: true,
			errFrag:   "warm_stream_fps",
		},
		{
			name:      "burst fps out of range",
			cfg:       BridgeConfig{BurstStreamFPS: 99},
			wantError: true,
			errFrag:   "burst_stream_fps",
		},
		{
			name:      "ax_max_nodes too large",
			cfg:       BridgeConfig{AXMaxNodes: 10001},
			wantError: true,
			errFrag:   "ax_max_nodes",
		},
		{
			name:      "negative spawn timeout",
			cfg:       BridgeConfig{SpawnTimeoutMS: -1},
			wantError: true,
			errFrag:   "spawn_timeout_ms",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := Config{Bridge: tc.cfg}
			err := c.Validate()
			if tc.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errFrag)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestBridgeConfigEnvVarExpansion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	t.Setenv("BRIDGE_BIN", "/custom/bin/bridge")

	content := `
[bridge]
mode = "auto"
swift_path = "$BRIDGE_BIN"
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	c := LoadFromTOML(path)
	assert.Equal(t, "/custom/bin/bridge", c.Bridge.SwiftPath)
}

// --- OverlayConfig tests ---

func TestOverlayConfigDefaults(t *testing.T) {
	d := overlayDefaults()
	assert.False(t, d.Enable)
	assert.Equal(t, "system_reminder", d.ContextInjection)
	assert.Equal(t, 50000, d.DailyTokenBudget, "phase G: default daily budget 50000")
}

func TestOverlayConfigDailyBudgetValidation(t *testing.T) {
	cases := []struct {
		name    string
		budget  int
		wantErr bool
	}{
		{"zero disables gating", 0, false},
		{"positive default", 50000, false},
		{"small positive", 100, false},
		{"negative rejected", -1, true},
		{"large negative rejected", -99999, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{Overlay: OverlayConfig{DailyTokenBudget: tc.budget}}
			err := cfg.Validate()
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "daily_token_budget")
			} else {
				require.NoError(t, err)
			}
		})
	}
}



func TestOverlayConfigTOMLRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	original := Config{
		Overlay: OverlayConfig{
			Enable:           true,
			SocketPath:       "/tmp/overlay.sock",
			BinaryPath:       "/usr/local/bin/providence-overlay",
			AutoStart:        true,
			TTSEnabled:       false,
			ContextInjection: "synthetic_user",
		},
	}

	require.NoError(t, original.SaveTo(path))
	loaded := LoadFromTOML(path)

	assert.True(t, loaded.Overlay.Enable)
	assert.Equal(t, "/tmp/overlay.sock", loaded.Overlay.SocketPath)
	assert.Equal(t, "/usr/local/bin/providence-overlay", loaded.Overlay.BinaryPath)
	assert.True(t, loaded.Overlay.AutoStart)
	assert.False(t, loaded.Overlay.TTSEnabled)
	assert.Equal(t, "synthetic_user", loaded.Overlay.ContextInjection)
}

func TestOverlayConfigMerge(t *testing.T) {
	base := Config{
		Overlay: OverlayConfig{
			Enable:           false,
			ContextInjection: "system_reminder",
		},
	}
	override := Config{
		Overlay: OverlayConfig{
			Enable:           true,
			ContextInjection: "synthetic_user",
		},
	}
	mergeConfig(&base, &override)

	assert.True(t, base.Overlay.Enable)
	assert.Equal(t, "synthetic_user", base.Overlay.ContextInjection)
}

func TestOverlayConfigMergePreservesBase(t *testing.T) {
	base := Config{
		Overlay: OverlayConfig{
			Enable:     true,
			SocketPath: "/run/overlay.sock",
			BinaryPath: "/usr/bin/overlay",
			TTSEnabled: true,
		},
	}
	// Empty override should not overwrite base.
	override := Config{}
	mergeConfig(&base, &override)

	assert.True(t, base.Overlay.Enable)
	assert.Equal(t, "/run/overlay.sock", base.Overlay.SocketPath)
	assert.Equal(t, "/usr/bin/overlay", base.Overlay.BinaryPath)
	assert.True(t, base.Overlay.TTSEnabled)
}

func TestOverlayConfigValidation(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name:    "valid system_reminder",
			cfg:     Config{Overlay: OverlayConfig{ContextInjection: "system_reminder"}},
			wantErr: false,
		},
		{
			name:    "valid synthetic_user",
			cfg:     Config{Overlay: OverlayConfig{ContextInjection: "synthetic_user"}},
			wantErr: false,
		},
		{
			name:    "valid empty (uses default)",
			cfg:     Config{Overlay: OverlayConfig{ContextInjection: ""}},
			wantErr: false,
		},
		{
			name:    "invalid context_injection",
			cfg:     Config{Overlay: OverlayConfig{ContextInjection: "turbo"}},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "overlay")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestOverlayConfigEnvExpansion(t *testing.T) {
	t.Setenv("OVERLAY_SOCK", "/tmp/my-overlay.sock")
	t.Setenv("OVERLAY_BIN", "/usr/local/bin/overlay")

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `[overlay]
socket_path = "$OVERLAY_SOCK"
binary_path = "$OVERLAY_BIN"
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	c := LoadFromTOML(path)
	assert.Equal(t, "/tmp/my-overlay.sock", c.Overlay.SocketPath)
	assert.Equal(t, "/usr/local/bin/overlay", c.Overlay.BinaryPath)
}

func TestOverlayConfigJSONRoundtrip(t *testing.T) {
	cfg := OverlayConfig{
		Enable:           true,
		SocketPath:       "/tmp/overlay.sock",
		ContextInjection: "system_reminder",
	}

	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	var got OverlayConfig
	require.NoError(t, json.Unmarshal(data, &got))

	assert.Equal(t, cfg.Enable, got.Enable)
	assert.Equal(t, cfg.SocketPath, got.SocketPath)
	assert.Equal(t, cfg.ContextInjection, got.ContextInjection)
}
