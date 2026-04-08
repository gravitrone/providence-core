package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config holds user preferences persisted to ~/.providence/config.json.
type Config struct {
	Engine string `json:"engine,omitempty"` // "claude" or "direct"
	Model  string `json:"model,omitempty"`  // model alias
	Theme  string `json:"theme,omitempty"`  // "flame" or "night"
}

// DefaultPath returns the default config file location.
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".providence", "config.json")
}

// Load reads config from DefaultPath. Returns empty Config on any error.
func Load() Config {
	return LoadFrom(DefaultPath())
}

// LoadFrom reads config from the given path. Returns empty Config on any error.
func LoadFrom(path string) Config {
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

// Save writes config to DefaultPath, creating ~/.providence/ if needed.
func (c Config) Save() error {
	return c.SaveTo(DefaultPath())
}

// SaveTo writes config to the given path, creating parent dirs if needed.
func (c Config) SaveTo(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
