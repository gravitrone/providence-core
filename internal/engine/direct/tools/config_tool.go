package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// supportedConfigKeys defines which runtime settings can be read/written.
var supportedConfigKeys = map[string]bool{
	"model":              true,
	"theme":              true,
	"effort":             true,
	"engine":             true,
	"auto_title_enabled": true,
	"dashboard_visible":  true,
}

// ConfigTool reads and writes runtime configuration settings.
type ConfigTool struct {
	mu     sync.RWMutex
	values map[string]string
}

// NewConfigTool creates a ConfigTool with empty initial state.
func NewConfigTool() *ConfigTool {
	return &ConfigTool{values: make(map[string]string)}
}

func (c *ConfigTool) Name() string { return "Config" }
func (c *ConfigTool) Description() string {
	return "Get or set runtime configuration settings. Supported keys: model, theme, effort, engine, auto_title_enabled, dashboard_visible."
}
func (c *ConfigTool) ReadOnly() bool { return false }

func (c *ConfigTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"get", "set"},
				"description": "Whether to read or write the setting",
			},
			"key": map[string]any{
				"type":        "string",
				"description": "The config key to get or set",
			},
			"value": map[string]any{
				"type":        "string",
				"description": "The value to set (required for action=set, ignored for get)",
			},
		},
		"required": []string{"action", "key"},
	}
}

// SetValue sets a config value programmatically (used by the engine to seed initial state).
func (c *ConfigTool) SetValue(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[key] = value
}

// GetValue reads a config value programmatically.
func (c *ConfigTool) GetValue(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.values[key]
	return v, ok
}

// Execute handles get/set actions on runtime config.
func (c *ConfigTool) Execute(_ context.Context, input map[string]any) ToolResult {
	action := paramString(input, "action", "")
	key := paramString(input, "key", "")

	if action == "" || key == "" {
		return ToolResult{Content: "action and key are required", IsError: true}
	}

	if !supportedConfigKeys[key] {
		keys := make([]string, 0, len(supportedConfigKeys))
		for k := range supportedConfigKeys {
			keys = append(keys, k)
		}
		return ToolResult{
			Content: fmt.Sprintf("unsupported key %q: supported keys are %v", key, keys),
			IsError: true,
		}
	}

	switch action {
	case "get":
		c.mu.RLock()
		val, ok := c.values[key]
		c.mu.RUnlock()

		if !ok {
			resp, _ := json.Marshal(map[string]any{
				"key":   key,
				"value": nil,
				"set":   false,
			})
			return ToolResult{Content: string(resp)}
		}

		resp, _ := json.Marshal(map[string]string{
			"key":   key,
			"value": val,
		})
		return ToolResult{Content: string(resp)}

	case "set":
		value := paramString(input, "value", "")
		if value == "" {
			return ToolResult{Content: "value is required for set action", IsError: true}
		}

		c.mu.Lock()
		c.values[key] = value
		c.mu.Unlock()

		resp, _ := json.Marshal(map[string]string{
			"key":   key,
			"value": value,
			"status": "updated",
		})
		return ToolResult{Content: string(resp)}

	default:
		return ToolResult{
			Content: fmt.Sprintf("invalid action %q: must be get or set", action),
			IsError: true,
		}
	}
}
