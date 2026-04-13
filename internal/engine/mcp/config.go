package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ServerConfig holds the configuration for a single MCP server.
type ServerConfig struct {
	Name    string
	Type    string // "stdio" (only supported transport for v1)
	Command string
	Args    []string
	Env     map[string]string
}

// mcpJSONFile is the on-disk format for .mcp.json / ~/.providence/mcp.json.
type mcpJSONFile struct {
	MCPServers map[string]mcpServerEntry `json:"mcpServers"`
}

type mcpServerEntry struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

// LoadMCPConfig reads MCP server configs from project-local .mcp.json and
// user-level ~/.providence/mcp.json. Project-local configs take precedence
// when server names collide.
func LoadMCPConfig(projectDir, homeDir string) ([]ServerConfig, error) {
	servers := make(map[string]ServerConfig)

	// User-level config (lower priority).
	userPath := filepath.Join(homeDir, ".providence", "mcp.json")
	if err := loadConfigFile(userPath, servers); err != nil {
		return nil, fmt.Errorf("user mcp config: %w", err)
	}

	// Project-level config (higher priority, overwrites user-level).
	projectPath := filepath.Join(projectDir, ".mcp.json")
	if err := loadConfigFile(projectPath, servers); err != nil {
		return nil, fmt.Errorf("project mcp config: %w", err)
	}

	out := make([]ServerConfig, 0, len(servers))
	for _, cfg := range servers {
		out = append(out, cfg)
	}
	return out, nil
}

func loadConfigFile(path string, dst map[string]ServerConfig) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // not an error, file is optional
		}
		return err
	}

	var f mcpJSONFile
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}

	for name, entry := range f.MCPServers {
		transport := entry.Type
		if transport == "" {
			transport = "stdio" // default, matches CC behavior
		}
		if transport != "stdio" {
			continue // v1: only stdio supported
		}
		if entry.Command == "" {
			continue // skip entries without a command
		}
		dst[name] = ServerConfig{
			Name:    name,
			Type:    transport,
			Command: entry.Command,
			Args:    entry.Args,
			Env:     entry.Env,
		}
	}
	return nil
}
