package config

import (
	"encoding/json"
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

	Compact CompactConfig `toml:"compact" json:"compact,omitempty"`
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
