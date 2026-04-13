package mcp

import (
	"fmt"
	"strings"
	"sync"
)

// Manager holds all connected MCP server clients and provides a unified
// interface for tool discovery and invocation.
type Manager struct {
	mu      sync.RWMutex
	clients map[string]*Client
}

// NewManager creates an empty MCP Manager.
func NewManager() *Manager {
	return &Manager{
		clients: make(map[string]*Client),
	}
}

// ConnectAll spawns and initializes all configured MCP servers.
// Servers that fail to connect are logged and skipped (non-fatal).
func (m *Manager) ConnectAll(configs []ServerConfig) error {
	var errs []string

	for _, cfg := range configs {
		if cfg.Type != "stdio" {
			continue // v1: only stdio
		}

		client, err := NewStdioClient(cfg)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: spawn failed: %v", cfg.Name, err))
			continue
		}

		if err := client.Initialize(); err != nil {
			client.Close()
			errs = append(errs, fmt.Sprintf("%s: init failed: %v", cfg.Name, err))
			continue
		}

		if _, err := client.ListTools(); err != nil {
			client.Close()
			errs = append(errs, fmt.Sprintf("%s: list tools failed: %v", cfg.Name, err))
			continue
		}

		m.mu.Lock()
		m.clients[cfg.Name] = client
		m.mu.Unlock()
	}

	if len(errs) > 0 {
		return fmt.Errorf("mcp connection errors:\n%s", strings.Join(errs, "\n"))
	}
	return nil
}

// GetAllTools returns ToolDef entries from all connected servers.
// Each ToolDef has the original (non-prefixed) name from the server.
func (m *Manager) GetAllTools() map[string][]ToolDef {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string][]ToolDef, len(m.clients))
	for name, client := range m.clients {
		result[name] = client.tools
	}
	return result
}

// CallTool invokes a tool on the specified MCP server.
func (m *Manager) CallTool(serverName, toolName string, args map[string]any) (string, error) {
	m.mu.RLock()
	client, ok := m.clients[serverName]
	m.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("MCP server %q not connected", serverName)
	}
	return client.CallTool(toolName, args)
}

// GetInstructions concatenates instructions from all connected servers.
func (m *Manager) GetInstructions() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var parts []string
	for name, client := range m.clients {
		if inst := client.GetInstructions(); inst != "" {
			parts = append(parts, fmt.Sprintf("## %s\n%s", name, inst))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "# MCP Server Instructions\n\n" + strings.Join(parts, "\n\n")
}

// ServerCount returns the number of connected servers.
func (m *Manager) ServerCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.clients)
}

// RefreshTools re-queries all connected MCP servers for their current tool lists.
// This picks up newly-connected servers or tools that appeared mid-conversation.
// Errors are silently ignored - stale tool lists are better than crashes.
func (m *Manager) RefreshTools() {
	m.mu.RLock()
	clients := make(map[string]*Client, len(m.clients))
	for k, v := range m.clients {
		clients[k] = v
	}
	m.mu.RUnlock()

	for _, client := range clients {
		// Re-list tools from each server, updating the client's cached tool list.
		_, _ = client.ListTools()
	}
}

// CloseAll shuts down all connected MCP server subprocesses.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, client := range m.clients {
		client.Close()
	}
	m.clients = make(map[string]*Client)
}
