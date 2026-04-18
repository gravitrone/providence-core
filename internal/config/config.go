package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/gravitrone/providence-core/internal/auth"
)

// BridgeConfig configures the macOS native bridge (providence-mac-bridge) and
// its CU tool behavior.
type BridgeConfig struct {
	Mode              string `toml:"mode" json:"mode,omitempty"`                               // auto|swift|shell; default auto
	SwiftPath         string `toml:"swift_path" json:"swift_path,omitempty"`                   // override binary lookup
	WarmStreamFPS     int    `toml:"warm_stream_fps" json:"warm_stream_fps,omitempty"`         // default 2
	BurstStreamFPS    int    `toml:"burst_stream_fps" json:"burst_stream_fps,omitempty"`       // default 30
	ActionBatch       bool   `toml:"action_batch" json:"action_batch,omitempty"`               // default true
	ScreenDiffEnabled bool   `toml:"screen_diff_enabled" json:"screen_diff_enabled,omitempty"` // default true
	AXMaxDepth        int    `toml:"ax_max_depth" json:"ax_max_depth,omitempty"`               // default 12
	AXMaxNodes        int    `toml:"ax_max_nodes" json:"ax_max_nodes,omitempty"`               // default 2000
	SpawnTimeoutMS    int    `toml:"spawn_timeout_ms" json:"spawn_timeout_ms,omitempty"`       // default 1500
}

// OverlayConfig configures the providence-overlay companion process.
type OverlayConfig struct {
	Enable     bool   `toml:"enable" json:"enable,omitempty"`
	SocketPath string `toml:"socket_path" json:"socket_path,omitempty"`
	BinaryPath string `toml:"binary_path" json:"binary_path,omitempty"`
	AutoStart  bool   `toml:"auto_start" json:"auto_start,omitempty"`
	// Spawn controls whether /overlay start forks the overlay subprocess.
	// Default true. Set false to only run the UDS server and let the user
	// launch the overlay manually from a fresh shell (works around macOS
	// TCC "responsible process" attribution that hangs ScreenCaptureKit
	// when the overlay is spawned from providence).
	Spawn            *bool  `toml:"spawn,omitempty" json:"spawn,omitempty"`
	TTSEnabled       bool   `toml:"tts_enabled" json:"tts_enabled,omitempty"`
	ContextInjection string `toml:"context_injection" json:"context_injection,omitempty"` // legacy: kept for forward-compat config files

	// Daily token budget breaker. When > 0, the overlay bridge skips
	// injections once this many tokens have been recorded today. 0 disables
	// gating. Default 50000. Must be >= 0.
	DailyTokenBudget int `toml:"daily_token_budget" json:"daily_token_budget,omitempty"`
}

// SandboxConfig controls extra sandbox-exec allowances.
//
// This config is security-sensitive. A malicious config file can relax the
// sandbox and broaden filesystem or network access for bash commands.
type SandboxConfig struct {
	AllowNetwork []string `toml:"allow_network" json:"allow_network,omitempty"`
	AllowWrite   []string `toml:"allow_write" json:"allow_write,omitempty"`
}

// SpawnEnabled returns true if the overlay subprocess should be spawned
// automatically. Defaults to true for backwards compatibility.
func (c *OverlayConfig) SpawnEnabled() bool {
	if c.Spawn == nil {
		return true
	}
	return *c.Spawn
}

// Config holds user preferences persisted to ~/.providence/config.toml.
type Config struct {
	Engine           string   `toml:"engine" json:"engine,omitempty"`
	Model            string   `toml:"model" json:"model,omitempty"`
	Theme            string   `toml:"theme" json:"theme,omitempty"`
	Effort           string   `toml:"effort" json:"effort,omitempty"`
	APIKeyHelper     string   `toml:"api_key_helper" json:"api_key_helper,omitempty"`
	OpenRouterAPIKey string   `toml:"openrouter_api_key" json:"openrouter_api_key,omitempty"`
	TokenBudget      int      `toml:"token_budget" json:"token_budget,omitempty"`
	AutoTitleEnabled bool     `toml:"auto_title_enabled" json:"auto_title_enabled,omitempty"`
	ToolUseSummary   bool     `toml:"tool_use_summary" json:"tool_use_summary,omitempty"`
	DashboardVisible *bool    `toml:"dashboard_visible,omitempty" json:"dashboard_visible,omitempty"`
	BGAgentsEnabled  bool     `toml:"bg_agents_enabled" json:"bg_agents_enabled,omitempty"`
	OutputStyle      string   `toml:"output_style" json:"output_style,omitempty"`
	Persona          string   `toml:"persona" json:"persona,omitempty"`
	SpinnerVerbs     []string `toml:"spinner_verbs" json:"spinner_verbs,omitempty"`

	Bridge      BridgeConfig      `toml:"bridge" json:"bridge,omitempty"`
	Compact     CompactConfig     `toml:"compact" json:"compact,omitempty"`
	Hooks       HooksConfig       `toml:"hooks" json:"hooks,omitempty"`
	Permissions PermissionsConfig `toml:"permissions" json:"permissions,omitempty"`
	Overlay     OverlayConfig     `toml:"overlay" json:"overlay,omitempty"`
	Sandbox     SandboxConfig     `toml:"sandbox" json:"sandbox,omitempty"`
	Session     SessionConfig     `toml:"session" json:"session,omitempty"`
}

// SessionConfig holds per-session settings including the LLM-generated
// session memory feature.
type SessionConfig struct {
	// MemoryEnabled toggles the llm-generated session memory writer. When
	// false, no memory files are created and the compactor falls straight
	// through to raw-history compression. Default true.
	MemoryEnabled *bool `toml:"memory_enabled" json:"memory_enabled,omitempty"`
	// MemoryTurnInterval is the number of completed turns between memory
	// writes. A fork subagent dispatches on every Nth completed turn. Must
	// be positive; zero or negative falls back to the default.
	MemoryTurnInterval int `toml:"memory_turn_interval" json:"memory_turn_interval,omitempty"`
}

// PermissionsConfig holds permission rules from config files.
// TOML example:
//
//	[permissions]
//	mode = "default"
//	allow = ["Read(*)", "Glob(*)", "Grep(*)"]
//	deny = ["Bash(rm -rf *)"]
//	ask = ["Bash(git push *)"]
type PermissionsConfig struct {
	Mode  string   `toml:"mode" json:"mode,omitempty"`   // default, acceptEdits, bypassPermissions, plan, dontAsk
	Allow []string `toml:"allow" json:"allow,omitempty"` // patterns that auto-allow
	Deny  []string `toml:"deny" json:"deny,omitempty"`   // patterns that always deny
	Ask   []string `toml:"ask" json:"ask,omitempty"`     // patterns that always ask
}

// HookEntry defines a single hook - either a shell command or HTTP endpoint.
type HookEntry struct {
	Command string `toml:"command" json:"command,omitempty"`
	URL     string `toml:"url" json:"url,omitempty"`
	Timeout int    `toml:"timeout" json:"timeout,omitempty"` // milliseconds
}

// HooksConfig maps event names to lists of hook entries.
// TOML example:
//
//	[hooks]
//	[hooks.PreToolUse]
//	  [[hooks.PreToolUse.hooks]]
//	  command = "echo pre-tool"
type HooksConfig struct {
	PreToolUse         []HookEntry `toml:"PreToolUse" json:"PreToolUse,omitempty"`
	PostToolUse        []HookEntry `toml:"PostToolUse" json:"PostToolUse,omitempty"`
	PostToolUseFailure []HookEntry `toml:"PostToolUseFailure" json:"PostToolUseFailure,omitempty"`
	Stop               []HookEntry `toml:"Stop" json:"Stop,omitempty"`
	SessionStart       []HookEntry `toml:"SessionStart" json:"SessionStart,omitempty"`
	SessionEnd         []HookEntry `toml:"SessionEnd" json:"SessionEnd,omitempty"`
	PreCompact         []HookEntry `toml:"PreCompact" json:"PreCompact,omitempty"`
	PostCompact        []HookEntry `toml:"PostCompact" json:"PostCompact,omitempty"`
	PermissionDenied   []HookEntry `toml:"PermissionDenied" json:"PermissionDenied,omitempty"`
	SubagentStart      []HookEntry `toml:"SubagentStart" json:"SubagentStart,omitempty"`
	SubagentStop       []HookEntry `toml:"SubagentStop" json:"SubagentStop,omitempty"`
	UserPromptSubmit   []HookEntry `toml:"UserPromptSubmit" json:"UserPromptSubmit,omitempty"`
}

// ToMap converts the typed HooksConfig into a map[string][]HookEntry for the Runner.
func (h *HooksConfig) ToMap() map[string][]HookEntry {
	m := make(map[string][]HookEntry)
	if len(h.PreToolUse) > 0 {
		m["PreToolUse"] = h.PreToolUse
	}
	if len(h.PostToolUse) > 0 {
		m["PostToolUse"] = h.PostToolUse
	}
	if len(h.PostToolUseFailure) > 0 {
		m["PostToolUseFailure"] = h.PostToolUseFailure
	}
	if len(h.Stop) > 0 {
		m["Stop"] = h.Stop
	}
	if len(h.SessionStart) > 0 {
		m["SessionStart"] = h.SessionStart
	}
	if len(h.SessionEnd) > 0 {
		m["SessionEnd"] = h.SessionEnd
	}
	if len(h.PreCompact) > 0 {
		m["PreCompact"] = h.PreCompact
	}
	if len(h.PostCompact) > 0 {
		m["PostCompact"] = h.PostCompact
	}
	if len(h.PermissionDenied) > 0 {
		m["PermissionDenied"] = h.PermissionDenied
	}
	if len(h.SubagentStart) > 0 {
		m["SubagentStart"] = h.SubagentStart
	}
	if len(h.SubagentStop) > 0 {
		m["SubagentStop"] = h.SubagentStop
	}
	if len(h.UserPromptSubmit) > 0 {
		m["UserPromptSubmit"] = h.UserPromptSubmit
	}
	return m
}

// CompactConfig holds compaction-related settings.
type CompactConfig struct {
	Mode           string `toml:"mode" json:"mode,omitempty"`       // cc-tail-replace | dynamic-rolling | both | off
	Trigger        string `toml:"trigger" json:"trigger,omitempty"` // token | turn | pressure | hybrid
	ThresholdPct   int    `toml:"threshold_pct" json:"threshold_pct,omitempty"`
	TurnCount      int    `toml:"turn_count" json:"turn_count,omitempty"`
	KeepRecentPct  int    `toml:"keep_recent_pct" json:"keep_recent_pct,omitempty"`
	RollingTokens  int    `toml:"rolling_tokens" json:"rolling_tokens,omitempty"`
	FastTierModel  string `toml:"fast_tier_model" json:"fast_tier_model,omitempty"`
	CircuitBreaker int    `toml:"circuit_breaker" json:"circuit_breaker,omitempty"`
	// CacheTTL controls Anthropic cache_control TTL.
	// Use "1h" only on Claude subscriber accounts; Anthropic rejects it otherwise.
	CacheTTL string `toml:"cache_ttl" json:"cache_ttl,omitempty"`
}

// DefaultTOMLPath returns the default TOML config file location.
func DefaultTOMLPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".providence", "config.toml")
}

// DefaultPath returns the default config file location (TOML).
func DefaultPath() string {
	return DefaultTOMLPath()
}

// defaultJSONPath returns the legacy JSON config path.
func defaultJSONPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".providence", "config.json")
}

// overlayDefaults returns the default OverlayConfig.
func overlayDefaults() OverlayConfig {
	return OverlayConfig{
		Enable:           false,
		ContextInjection: "system_reminder",
		DailyTokenBudget: 50000,
	}
}

func boolPtr(value bool) *bool {
	return &value
}

func optionalBoolValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func dashboardVisibleValue(value *bool) bool {
	return optionalBoolValue(value, true)
}

// Defaults returns a Config with sensible default values.
func Defaults() Config {
	return Config{
		Engine:           "claude",
		Theme:            "flame",
		DashboardVisible: boolPtr(true),
		Overlay:          overlayDefaults(),
		Bridge: BridgeConfig{
			Mode:              "auto",
			WarmStreamFPS:     2,
			BurstStreamFPS:    30,
			ActionBatch:       true,
			ScreenDiffEnabled: true,
			AXMaxDepth:        12,
			AXMaxNodes:        2000,
			SpawnTimeoutMS:    1500,
		},
		Compact: CompactConfig{
			Mode:           "both",
			Trigger:        "hybrid",
			ThresholdPct:   80,
			TurnCount:      20,
			KeepRecentPct:  30,
			RollingTokens:  50000,
			FastTierModel:  "haiku",
			CircuitBreaker: 3,
			CacheTTL:       "5m",
		},
	}
}

// Load reads config from the default path with JSON migration fallback.
func Load() Config {
	tomlPath := DefaultTOMLPath()
	jsonPath := defaultJSONPath()
	cfg := loadWithMigration(tomlPath, jsonPath)
	applyAPIKeyHelper(&cfg)
	return cfg
}

// loadWithMigration tries TOML first, falls back to JSON with auto-migration.
func loadWithMigration(tomlPath, jsonPath string) Config {
	cfg := Defaults()

	// Try TOML first.
	if tomlCfg, err := loadTOMLFile(tomlPath); err == nil {
		mergeConfig(&cfg, &tomlCfg)
		return cfg
	} else if !errors.Is(err, os.ErrNotExist) {
		reportConfigLoadError(err)
		return cfg
	}

	// Try JSON migration.
	if jsonCfg, err := loadJSONFile(jsonPath); err == nil {
		mergeConfig(&cfg, &jsonCfg)
		// Migrate: write TOML, best-effort.
		_ = cfg.SaveTo(tomlPath)
		return cfg
	} else if !errors.Is(err, os.ErrNotExist) {
		reportConfigLoadError(err)
	}

	return cfg
}

// LoadFrom reads config from a TOML file. Returns empty Config on any error.
func LoadFrom(path string) Config {
	return LoadFromTOML(path)
}

// expandEnvVars replaces ${VAR} and $VAR patterns in s with environment values.
func expandEnvVars(s string) string {
	return os.ExpandEnv(s)
}

// expandConfigEnvVars applies env var expansion to all string fields in a Config.
func expandConfigEnvVars(c *Config) {
	c.Engine = expandEnvVars(c.Engine)
	c.Model = expandEnvVars(c.Model)
	c.Theme = expandEnvVars(c.Theme)
	c.Effort = expandEnvVars(c.Effort)
	c.APIKeyHelper = expandEnvVars(c.APIKeyHelper)
	c.OpenRouterAPIKey = expandEnvVars(c.OpenRouterAPIKey)
	c.OutputStyle = expandEnvVars(c.OutputStyle)
	c.Persona = expandEnvVars(c.Persona)
	c.Bridge.Mode = expandEnvVars(c.Bridge.Mode)
	c.Bridge.SwiftPath = expandEnvVars(c.Bridge.SwiftPath)
	c.Overlay.SocketPath = expandEnvVars(c.Overlay.SocketPath)
	c.Overlay.BinaryPath = expandEnvVars(c.Overlay.BinaryPath)
	c.Overlay.ContextInjection = expandEnvVars(c.Overlay.ContextInjection)
	c.Compact.Mode = expandEnvVars(c.Compact.Mode)
	c.Compact.Trigger = expandEnvVars(c.Compact.Trigger)
	c.Compact.FastTierModel = expandEnvVars(c.Compact.FastTierModel)
	c.Compact.CacheTTL = expandEnvVars(c.Compact.CacheTTL)
	c.Permissions.Mode = expandEnvVars(c.Permissions.Mode)
	for i := range c.Permissions.Allow {
		c.Permissions.Allow[i] = expandEnvVars(c.Permissions.Allow[i])
	}
	for i := range c.Permissions.Deny {
		c.Permissions.Deny[i] = expandEnvVars(c.Permissions.Deny[i])
	}
	for i := range c.Permissions.Ask {
		c.Permissions.Ask[i] = expandEnvVars(c.Permissions.Ask[i])
	}
	for i := range c.Sandbox.AllowNetwork {
		c.Sandbox.AllowNetwork[i] = expandEnvVars(c.Sandbox.AllowNetwork[i])
	}
	for i := range c.Sandbox.AllowWrite {
		c.Sandbox.AllowWrite[i] = expandEnvVars(c.Sandbox.AllowWrite[i])
	}
}

// LoadFromTOML reads config from a TOML file. Returns empty Config on any error.
// Environment variables (${VAR} or $VAR) in string fields are expanded after loading.
func LoadFromTOML(path string) Config {
	c, err := loadTOMLFile(path)
	if err != nil {
		return Config{}
	}
	return c
}

// loadJSONFile reads config from a JSON file.
func loadJSONFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read json config %s: %w", path, err)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("decode json config %s: %w", path, err)
	}
	expandConfigEnvVars(&c)
	return c, nil
}

func loadTOMLFile(path string) (Config, error) {
	var c Config
	if _, err := toml.DecodeFile(path, &c); err != nil {
		return Config{}, fmt.Errorf("decode toml config %s: %w", path, err)
	}
	expandConfigEnvVars(&c)
	return c, nil
}

func reportConfigLoadError(err error) {
	fmt.Fprintf(os.Stderr, "%v\n", err)
}

// managedSettingsPath returns the macOS enterprise managed-settings path.
// On other platforms, returns an empty string (feature disabled).
func managedSettingsPath() string {
	return "/Library/Application Support/Providence/managed-settings.toml"
}

// loadManagedSettings attempts to load enterprise managed settings from the
// well-known system path. Returns empty Config if file doesn't exist or
// platform doesn't support it. This is a stub - enterprise enforcement logic
// is not implemented.
func loadManagedSettings() Config {
	path := managedSettingsPath()
	if path == "" {
		return Config{}
	}
	if _, err := os.Stat(path); err != nil {
		return Config{} // not present, not an error
	}
	return LoadFromTOML(path)
}

// LoadMerged loads config with 5-level merge: user global -> project -> local -> (flags + policy at runtime).
// projectRoot is the working directory or project root where .providence/ may live.
// Enterprise managed-settings.toml (if present) is applied last as highest priority.
func LoadMerged(projectRoot string) Config {
	home, _ := os.UserHomeDir()

	// Level 1: user global
	cfg := loadWithMigration(DefaultTOMLPath(), defaultJSONPath())

	// Level 2: project (committed)
	projectCfg := LoadFromTOML(filepath.Join(projectRoot, ".providence", "config.toml"))
	mergeConfig(&cfg, &projectCfg)

	// Level 3: local (gitignored)
	localCfg := LoadFromTOML(filepath.Join(projectRoot, ".providence", "config.local.toml"))
	mergeConfig(&cfg, &localCfg)

	// Level 4: .claude/ compat path
	claudeCfg := LoadFromTOML(filepath.Join(projectRoot, ".claude", "config.toml"))
	mergeConfig(&cfg, &claudeCfg)

	// Level 5 (highest priority): enterprise managed settings - overrides everything.
	managedCfg := loadManagedSettings()
	mergeConfig(&cfg, &managedCfg)

	// CLI flags handled at runtime by caller.
	_ = home
	applyAPIKeyHelper(&cfg)
	return cfg
}

func applyAPIKeyHelper(c *Config) {
	helperCmd := strings.TrimSpace(c.APIKeyHelper)
	if helperCmd == "" {
		return
	}

	key, err := auth.ResolveAPIKeyViaHelper(context.Background(), helperCmd)
	if err != nil {
		log.Printf("api key helper failed: %q", helperCmd)
		return
	}
	if key == "" {
		return
	}

	_ = os.Setenv("ANTHROPIC_API_KEY", key)
}

// mergeConfig overlays non-zero fields from override onto base.
func mergeConfig(base, override *Config) {
	if override.Engine != "" {
		base.Engine = override.Engine
	}
	if override.Model != "" {
		base.Model = override.Model
	}
	if override.Theme != "" {
		base.Theme = override.Theme
	}
	if override.Effort != "" {
		base.Effort = override.Effort
	}
	if override.APIKeyHelper != "" {
		base.APIKeyHelper = override.APIKeyHelper
	}
	if override.OpenRouterAPIKey != "" {
		base.OpenRouterAPIKey = override.OpenRouterAPIKey
	}
	if override.TokenBudget != 0 {
		base.TokenBudget = override.TokenBudget
	}
	if override.AutoTitleEnabled {
		base.AutoTitleEnabled = true
	}
	if override.ToolUseSummary {
		base.ToolUseSummary = true
	}
	if override.DashboardVisible != nil {
		base.DashboardVisible = boolPtr(*override.DashboardVisible)
	}
	if override.OutputStyle != "" {
		base.OutputStyle = override.OutputStyle
	}
	if override.Persona != "" {
		base.Persona = override.Persona
	}
	if len(override.SpinnerVerbs) > 0 {
		base.SpinnerVerbs = override.SpinnerVerbs
	}
	// Compact: merge non-zero fields
	if override.Compact.Mode != "" {
		base.Compact.Mode = override.Compact.Mode
	}
	if override.Compact.Trigger != "" {
		base.Compact.Trigger = override.Compact.Trigger
	}
	if override.Compact.ThresholdPct != 0 {
		base.Compact.ThresholdPct = override.Compact.ThresholdPct
	}
	if override.Compact.TurnCount != 0 {
		base.Compact.TurnCount = override.Compact.TurnCount
	}
	if override.Compact.KeepRecentPct != 0 {
		base.Compact.KeepRecentPct = override.Compact.KeepRecentPct
	}
	if override.Compact.RollingTokens != 0 {
		base.Compact.RollingTokens = override.Compact.RollingTokens
	}
	if override.Compact.FastTierModel != "" {
		base.Compact.FastTierModel = override.Compact.FastTierModel
	}
	if override.Compact.CircuitBreaker != 0 {
		base.Compact.CircuitBreaker = override.Compact.CircuitBreaker
	}
	if override.Compact.CacheTTL != "" {
		base.Compact.CacheTTL = override.Compact.CacheTTL
	}
	// Bridge: merge non-zero/non-empty fields.
	if override.Bridge.Mode != "" {
		base.Bridge.Mode = override.Bridge.Mode
	}
	if override.Bridge.SwiftPath != "" {
		base.Bridge.SwiftPath = override.Bridge.SwiftPath
	}
	if override.Bridge.WarmStreamFPS != 0 {
		base.Bridge.WarmStreamFPS = override.Bridge.WarmStreamFPS
	}
	if override.Bridge.BurstStreamFPS != 0 {
		base.Bridge.BurstStreamFPS = override.Bridge.BurstStreamFPS
	}
	if override.Bridge.ActionBatch {
		base.Bridge.ActionBatch = true
	}
	if override.Bridge.ScreenDiffEnabled {
		base.Bridge.ScreenDiffEnabled = true
	}
	if override.Bridge.AXMaxDepth != 0 {
		base.Bridge.AXMaxDepth = override.Bridge.AXMaxDepth
	}
	if override.Bridge.AXMaxNodes != 0 {
		base.Bridge.AXMaxNodes = override.Bridge.AXMaxNodes
	}
	if override.Bridge.SpawnTimeoutMS != 0 {
		base.Bridge.SpawnTimeoutMS = override.Bridge.SpawnTimeoutMS
	}

	// Permissions: merge non-empty fields.
	if override.Permissions.Mode != "" {
		base.Permissions.Mode = override.Permissions.Mode
	}
	if len(override.Permissions.Allow) > 0 {
		base.Permissions.Allow = override.Permissions.Allow
	}
	if len(override.Permissions.Deny) > 0 {
		base.Permissions.Deny = override.Permissions.Deny
	}
	if len(override.Permissions.Ask) > 0 {
		base.Permissions.Ask = override.Permissions.Ask
	}

	// Overlay: merge non-zero/non-false fields.
	if override.Overlay.Enable {
		base.Overlay.Enable = true
	}
	if override.Overlay.SocketPath != "" {
		base.Overlay.SocketPath = override.Overlay.SocketPath
	}
	if override.Overlay.BinaryPath != "" {
		base.Overlay.BinaryPath = override.Overlay.BinaryPath
	}
	if override.Overlay.AutoStart {
		base.Overlay.AutoStart = true
	}
	if override.Overlay.TTSEnabled {
		base.Overlay.TTSEnabled = true
	}
	if override.Overlay.ContextInjection != "" {
		base.Overlay.ContextInjection = override.Overlay.ContextInjection
	}
	if override.Overlay.DailyTokenBudget != 0 {
		base.Overlay.DailyTokenBudget = override.Overlay.DailyTokenBudget
	}

	// Sandbox: merge non-empty lists.
	if len(override.Sandbox.AllowNetwork) > 0 {
		base.Sandbox.AllowNetwork = override.Sandbox.AllowNetwork
	}
	if len(override.Sandbox.AllowWrite) > 0 {
		base.Sandbox.AllowWrite = override.Sandbox.AllowWrite
	}

	// Hooks: override replaces entire event lists (not additive).
	overrideHooks := override.Hooks.ToMap()
	for event, entries := range overrideHooks {
		switch event {
		case "PreToolUse":
			base.Hooks.PreToolUse = entries
		case "PostToolUse":
			base.Hooks.PostToolUse = entries
		case "PostToolUseFailure":
			base.Hooks.PostToolUseFailure = entries
		case "Stop":
			base.Hooks.Stop = entries
		case "SessionStart":
			base.Hooks.SessionStart = entries
		case "SessionEnd":
			base.Hooks.SessionEnd = entries
		case "PreCompact":
			base.Hooks.PreCompact = entries
		case "PostCompact":
			base.Hooks.PostCompact = entries
		case "PermissionDenied":
			base.Hooks.PermissionDenied = entries
		case "SubagentStart":
			base.Hooks.SubagentStart = entries
		case "SubagentStop":
			base.Hooks.SubagentStop = entries
		case "UserPromptSubmit":
			base.Hooks.UserPromptSubmit = entries
		}
	}

	// Session: merge memory settings. A *bool override replaces base only when
	// explicitly set, so an unset override never flips an already-configured
	// base value to its zero default.
	if override.Session.MemoryEnabled != nil {
		base.Session.MemoryEnabled = boolPtr(*override.Session.MemoryEnabled)
	}
	if override.Session.MemoryTurnInterval > 0 {
		base.Session.MemoryTurnInterval = override.Session.MemoryTurnInterval
	}
}

// ClaudeSettings represents .claude/settings.json for CC compatibility.
type ClaudeSettings struct {
	AllowedTools    []string          `json:"allowedTools"`
	DisallowedTools []string          `json:"disallowedTools"`
	Env             map[string]string `json:"env"`
	Hooks           map[string]any    `json:"hooks"`
}

// LoadClaudeSettings reads .claude/settings.json from the project directory.
// Returns nil with no error if the file doesn't exist.
func LoadClaudeSettings(projectDir string) (*ClaudeSettings, error) {
	path := filepath.Join(projectDir, ".claude", "settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var settings ClaudeSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("parse .claude/settings.json: %w", err)
	}

	// Apply env vars to process environment.
	for k, v := range settings.Env {
		os.Setenv(k, v)
	}

	return &settings, nil
}

// ParseHooks converts the raw .claude/settings.json hooks map into typed HooksConfig.
// CC format: {"hooks": {"PreToolUse": [{"command": "echo hi"}]}}
func (s *ClaudeSettings) ParseHooks() HooksConfig {
	if len(s.Hooks) == 0 {
		return HooksConfig{}
	}

	var cfg HooksConfig
	for event, raw := range s.Hooks {
		entries := parseHookEntries(raw)
		if len(entries) == 0 {
			continue
		}
		switch event {
		case "PreToolUse":
			cfg.PreToolUse = entries
		case "PostToolUse":
			cfg.PostToolUse = entries
		case "PostToolUseFailure":
			cfg.PostToolUseFailure = entries
		case "Stop":
			cfg.Stop = entries
		case "SessionStart":
			cfg.SessionStart = entries
		case "SessionEnd":
			cfg.SessionEnd = entries
		case "PreCompact":
			cfg.PreCompact = entries
		case "PostCompact":
			cfg.PostCompact = entries
		case "PermissionDenied":
			cfg.PermissionDenied = entries
		case "SubagentStart":
			cfg.SubagentStart = entries
		case "SubagentStop":
			cfg.SubagentStop = entries
		case "UserPromptSubmit":
			cfg.UserPromptSubmit = entries
		}
	}
	return cfg
}

// parseHookEntries converts a raw JSON value (expected to be an array of objects)
// into typed HookEntry slices. Handles both array and single-object forms.
func parseHookEntries(raw any) []HookEntry {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}

	// Try array first.
	var entries []HookEntry
	if err := json.Unmarshal(data, &entries); err == nil {
		return entries
	}

	// Try single object.
	var single HookEntry
	if err := json.Unmarshal(data, &single); err == nil && (single.Command != "" || single.URL != "") {
		return []HookEntry{single}
	}

	return nil
}

// PermissionRule is a structured permission rule parsed from config patterns.
type PermissionRule struct {
	Pattern  string
	Behavior string // "allow", "deny", "ask"
	Source   string
}

// AllowRules returns the allow patterns as structured rules.
func (p *PermissionsConfig) AllowRules(source string) []PermissionRule {
	rules := make([]PermissionRule, len(p.Allow))
	for i, pattern := range p.Allow {
		rules[i] = PermissionRule{Pattern: pattern, Behavior: "allow", Source: source}
	}
	return rules
}

// DenyRules returns the deny patterns as structured rules.
func (p *PermissionsConfig) DenyRules(source string) []PermissionRule {
	rules := make([]PermissionRule, len(p.Deny))
	for i, pattern := range p.Deny {
		rules[i] = PermissionRule{Pattern: pattern, Behavior: "deny", Source: source}
	}
	return rules
}

// AskRules returns the ask patterns as structured rules.
func (p *PermissionsConfig) AskRules(source string) []PermissionRule {
	rules := make([]PermissionRule, len(p.Ask))
	for i, pattern := range p.Ask {
		rules[i] = PermissionRule{Pattern: pattern, Behavior: "ask", Source: source}
	}
	return rules
}

// Validate checks that config fields are within allowed values.
// Returns a combined error with all violations found, or nil if valid.
func (c *Config) Validate() error {
	var errs []string

	validEngines := map[string]bool{
		"":               true, // empty = use default
		"direct":         true,
		"claude":         true,
		"codex_headless": true,
		"opencode":       true,
	}
	if !validEngines[c.Engine] {
		errs = append(errs, fmt.Sprintf("engine %q is not valid (allowed: direct, claude, codex_headless, opencode)", c.Engine))
	}

	if c.Model == "" && c.Engine == "direct" {
		errs = append(errs, "model must not be empty when engine is direct")
	}

	validCompactModes := map[string]bool{
		"":                true, // empty = use default
		"cc-tail-replace": true,
		"dynamic-rolling": true,
		"both":            true,
		"off":             true,
	}
	if !validCompactModes[c.Compact.Mode] {
		errs = append(errs, fmt.Sprintf("compact.mode %q is not valid (allowed: cc-tail-replace, dynamic-rolling, both, off)", c.Compact.Mode))
	}
	validCompactCacheTTLs := map[string]bool{
		"":   true,
		"5m": true,
		"1h": true,
	}
	if !validCompactCacheTTLs[c.Compact.CacheTTL] {
		errs = append(errs, fmt.Sprintf("compact.cache_ttl %q is not valid (allowed: 5m, 1h)", c.Compact.CacheTTL))
	}

	validEffort := map[string]bool{
		"":       true, // empty = use default
		"low":    true,
		"medium": true,
		"high":   true,
	}
	if !validEffort[c.Effort] {
		errs = append(errs, fmt.Sprintf("effort %q is not valid (allowed: low, medium, high)", c.Effort))
	}

	validBridgeModes := map[string]bool{
		"":      true,
		"auto":  true,
		"swift": true,
		"shell": true,
	}
	if !validBridgeModes[c.Bridge.Mode] {
		errs = append(errs, fmt.Sprintf("bridge.mode %q is not valid (allowed: auto, swift, shell)", c.Bridge.Mode))
	}
	if c.Bridge.WarmStreamFPS < 0 || c.Bridge.WarmStreamFPS > 60 {
		errs = append(errs, fmt.Sprintf("bridge.warm_stream_fps %d out of range (0-60)", c.Bridge.WarmStreamFPS))
	}
	if c.Bridge.BurstStreamFPS < 0 || c.Bridge.BurstStreamFPS > 60 {
		errs = append(errs, fmt.Sprintf("bridge.burst_stream_fps %d out of range (0-60)", c.Bridge.BurstStreamFPS))
	}
	if c.Bridge.AXMaxNodes < 0 || c.Bridge.AXMaxNodes > 10000 {
		errs = append(errs, fmt.Sprintf("bridge.ax_max_nodes %d out of range (0-10000)", c.Bridge.AXMaxNodes))
	}
	if c.Bridge.SpawnTimeoutMS < 0 {
		errs = append(errs, fmt.Sprintf("bridge.spawn_timeout_ms %d must be > 0", c.Bridge.SpawnTimeoutMS))
	}

	validContextInjection := map[string]bool{
		"":                true, // empty = use default
		"system_reminder": true,
		"synthetic_user":  true, // legacy: still accepted but no longer fires direct sends
	}
	if !validContextInjection[c.Overlay.ContextInjection] {
		errs = append(errs, fmt.Sprintf("overlay.context_injection %q is not valid (allowed: system_reminder, synthetic_user)", c.Overlay.ContextInjection))
	}
	if c.Overlay.DailyTokenBudget < 0 {
		errs = append(errs, fmt.Sprintf("overlay.daily_token_budget %d must be >= 0 (0 disables)", c.Overlay.DailyTokenBudget))
	}
	if _, err := NormalizeSandboxConfig(c.Sandbox); err != nil {
		errs = append(errs, err.Error())
	}

	if len(errs) > 0 {
		return fmt.Errorf("config validation errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// NormalizeSandboxConfig validates and normalizes sandbox config values.
func NormalizeSandboxConfig(cfg SandboxConfig) (SandboxConfig, error) {
	var normalized SandboxConfig
	var errs []string

	for _, entry := range cfg.AllowNetwork {
		value, err := normalizeSandboxNetworkEntry(entry)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		normalized.AllowNetwork = append(normalized.AllowNetwork, value)
	}

	for _, entry := range cfg.AllowWrite {
		value, err := normalizeSandboxWritePath(entry)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		normalized.AllowWrite = append(normalized.AllowWrite, value)
	}

	if len(errs) > 0 {
		return SandboxConfig{}, fmt.Errorf("sandbox config invalid: %s", strings.Join(errs, "; "))
	}

	return normalized, nil
}

func normalizeSandboxNetworkEntry(entry string) (string, error) {
	value := strings.TrimSpace(expandEnvVars(entry))
	if value == "" {
		return "", fmt.Errorf("sandbox.allow_network entry must not be empty")
	}
	if value == "*:*" || value == "0.0.0.0:*" {
		return "", fmt.Errorf("sandbox.allow_network %q is too broad", entry)
	}
	if strings.Contains(value, "://") {
		return "", fmt.Errorf("sandbox.allow_network %q must be host or host:port", entry)
	}

	host := value
	port := ""
	if strings.Contains(value, ":") {
		parsedHost, parsedPort, err := net.SplitHostPort(value)
		if err != nil {
			return "", fmt.Errorf("sandbox.allow_network %q must be host or host:port", entry)
		}
		host = parsedHost
		port = parsedPort
	}

	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" {
		return "", fmt.Errorf("sandbox.allow_network %q must include a host", entry)
	}
	if host == "*" || host == "0.0.0.0" {
		return "", fmt.Errorf("sandbox.allow_network %q is too broad", entry)
	}
	if strings.Contains(host, "*") {
		return "", fmt.Errorf("sandbox.allow_network %q must not include wildcard hosts", entry)
	}

	if port == "" {
		return host + ":*", nil
	}
	if port == "*" {
		return "", fmt.Errorf("sandbox.allow_network %q must not include wildcard ports", entry)
	}

	portNum, err := strconv.Atoi(port)
	if err != nil || portNum < 1 || portNum > 65535 {
		return "", fmt.Errorf("sandbox.allow_network %q must use a port between 1 and 65535", entry)
	}

	return net.JoinHostPort(host, port), nil
}

func normalizeSandboxWritePath(entry string) (string, error) {
	value := strings.TrimSpace(expandEnvVars(entry))
	if value == "" {
		return "", fmt.Errorf("sandbox.allow_write entry must not be empty")
	}

	expanded, err := expandSandboxHome(value)
	if err != nil {
		return "", err
	}
	cleaned := filepath.Clean(expanded)
	if !filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("sandbox.allow_write %q must be an absolute path or ~/subdir", entry)
	}
	if cleaned == string(filepath.Separator) {
		return "", fmt.Errorf("sandbox.allow_write %q must not allow filesystem root", entry)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory for sandbox.allow_write %q: %w", entry, err)
	}
	if filepath.Clean(home) == cleaned {
		return "", fmt.Errorf("sandbox.allow_write %q must target a subdirectory, not $HOME itself", entry)
	}

	return cleaned, nil
}

func expandSandboxHome(path string) (string, error) {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory for sandbox.allow_write %q: %w", path, err)
		}
		return home, nil
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory for sandbox.allow_write %q: %w", path, err)
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
	}
	return path, nil
}

// Save writes config to DefaultPath as TOML, creating ~/.providence/ if needed.
func (c Config) Save() error {
	return c.SaveTo(DefaultPath())
}

// SaveTo writes config to the given path as TOML, creating parent dirs if needed.
func (c Config) SaveTo(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	enc := toml.NewEncoder(f)
	encErr := enc.Encode(c)
	if closeErr := f.Close(); closeErr != nil && encErr == nil {
		return closeErr
	}
	return encErr
}
