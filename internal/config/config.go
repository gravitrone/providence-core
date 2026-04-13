package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config holds user preferences persisted to ~/.providence/config.toml.
type Config struct {
	Engine           string `toml:"engine" json:"engine,omitempty"`
	Model            string `toml:"model" json:"model,omitempty"`
	Theme            string `toml:"theme" json:"theme,omitempty"`
	Effort           string `toml:"effort" json:"effort,omitempty"`
	OpenRouterAPIKey string `toml:"openrouter_api_key" json:"openrouter_api_key,omitempty"`
	TokenBudget      int    `toml:"token_budget" json:"token_budget,omitempty"`
	AutoTitleEnabled bool   `toml:"auto_title_enabled" json:"auto_title_enabled,omitempty"`
	ToolUseSummary   bool   `toml:"tool_use_summary" json:"tool_use_summary,omitempty"`
	DashboardVisible bool   `toml:"dashboard_visible" json:"dashboard_visible,omitempty"`
	BGAgentsEnabled  bool   `toml:"bg_agents_enabled" json:"bg_agents_enabled,omitempty"`
	OutputStyle      string   `toml:"output_style" json:"output_style,omitempty"`
	SpinnerVerbs     []string `toml:"spinner_verbs" json:"spinner_verbs,omitempty"`

	Compact     CompactConfig     `toml:"compact" json:"compact,omitempty"`
	Hooks       HooksConfig       `toml:"hooks" json:"hooks,omitempty"`
	Permissions PermissionsConfig `toml:"permissions" json:"permissions,omitempty"`
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
	Allow []string `toml:"allow" json:"allow,omitempty"`  // patterns that auto-allow
	Deny  []string `toml:"deny" json:"deny,omitempty"`    // patterns that always deny
	Ask   []string `toml:"ask" json:"ask,omitempty"`      // patterns that always ask
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
	Mode          string `toml:"mode" json:"mode,omitempty"`             // cc-tail-replace | dynamic-rolling | both | off
	Trigger       string `toml:"trigger" json:"trigger,omitempty"`       // token | turn | pressure | hybrid
	ThresholdPct  int    `toml:"threshold_pct" json:"threshold_pct,omitempty"`
	TurnCount     int    `toml:"turn_count" json:"turn_count,omitempty"`
	KeepRecentPct int    `toml:"keep_recent_pct" json:"keep_recent_pct,omitempty"`
	RollingTokens int    `toml:"rolling_tokens" json:"rolling_tokens,omitempty"`
	FastTierModel string `toml:"fast_tier_model" json:"fast_tier_model,omitempty"`
	CircuitBreaker int   `toml:"circuit_breaker" json:"circuit_breaker,omitempty"`
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

// Defaults returns a Config with sensible default values.
func Defaults() Config {
	return Config{
		Engine:           "claude",
		Theme:            "flame",
		DashboardVisible: true,
		Compact: CompactConfig{
			Mode:          "both",
			Trigger:       "hybrid",
			ThresholdPct:  80,
			TurnCount:     20,
			KeepRecentPct: 30,
			RollingTokens: 50000,
			FastTierModel: "haiku",
			CircuitBreaker: 3,
		},
	}
}

// Load reads config from the default path with JSON migration fallback.
func Load() Config {
	tomlPath := DefaultTOMLPath()
	jsonPath := defaultJSONPath()
	return loadWithMigration(tomlPath, jsonPath)
}

// loadWithMigration tries TOML first, falls back to JSON with auto-migration.
func loadWithMigration(tomlPath, jsonPath string) Config {
	// Try TOML first.
	if _, err := os.Stat(tomlPath); err == nil {
		return LoadFromTOML(tomlPath)
	}

	// Try JSON migration.
	if _, err := os.Stat(jsonPath); err == nil {
		c := loadFromJSON(jsonPath)
		// Migrate: write TOML, best-effort.
		_ = c.SaveTo(tomlPath)
		return c
	}

	return Config{}
}

// LoadFrom reads config from a TOML file. Returns empty Config on any error.
func LoadFrom(path string) Config {
	return LoadFromTOML(path)
}

// LoadFromTOML reads config from a TOML file. Returns empty Config on any error.
func LoadFromTOML(path string) Config {
	var c Config
	if _, err := toml.DecodeFile(path, &c); err != nil {
		return Config{}
	}
	return c
}

// loadFromJSON reads config from a JSON file. Returns empty Config on any error.
func loadFromJSON(path string) Config {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}
	}
	return c
}

// LoadMerged loads config with 5-level merge: user global -> project -> local -> (flags + policy at runtime).
// projectRoot is the working directory or project root where .providence/ may live.
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

	// Level 5: CLI flags + policy handled at runtime by caller
	_ = home
	return cfg
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
	if override.DashboardVisible {
		base.DashboardVisible = true
	}
	if override.OutputStyle != "" {
		base.OutputStyle = override.OutputStyle
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
