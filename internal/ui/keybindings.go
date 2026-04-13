package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// KeybindingOverride defines key overrides for a specific context.
type KeybindingOverride struct {
	Context  string            `json:"context"`
	Bindings map[string]string `json:"bindings"`
}

// KeybindingsConfig holds all user-configured key overrides.
type KeybindingsConfig struct {
	Bindings []KeybindingOverride `json:"bindings"`
}

// LoadKeybindings reads keybindings from ~/.providence/keybindings.json.
// Returns nil config with nil error if the file does not exist.
func LoadKeybindings(homeDir string) (*KeybindingsConfig, error) {
	path := filepath.Join(homeDir, ".providence", "keybindings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var cfg KeybindingsConfig
	return &cfg, json.Unmarshal(data, &cfg)
}

// LookupBinding checks if a key has an override in the given context.
// Returns the remapped action string, or "" if no override exists.
func (kc *KeybindingsConfig) LookupBinding(ctx, key string) string {
	if kc == nil {
		return ""
	}
	for _, override := range kc.Bindings {
		if override.Context == ctx || override.Context == "*" {
			if action, ok := override.Bindings[key]; ok {
				return action
			}
		}
	}
	return ""
}
