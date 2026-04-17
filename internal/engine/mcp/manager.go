package mcp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// maxConsecutiveErrors is the number of consecutive CallTool failures before
// the manager attempts to reconnect the MCP server.
const maxConsecutiveErrors = 3

// maxReconnectAttempts caps how many times the manager will try to reconnect
// a single MCP server before giving up.
const maxReconnectAttempts = 5

// Manager holds all connected MCP server clients and provides a unified
// interface for tool discovery and invocation.
type Manager struct {
	mu      sync.RWMutex
	clients map[string]*Client
	configs map[string]ServerConfig

	instructionCache map[string]string
	turnAttachments  map[string]TurnAttachment

	// Per-client error tracking for auto-reconnect.
	consecutiveErrors map[string]int
	reconnectAttempts map[string]int
}

// TurnAttachment is deferred MCP context that should be injected on the next turn.
type TurnAttachment struct {
	ServerName string
	Content    string
}

// NewManager creates an empty MCP Manager.
func NewManager() *Manager {
	return &Manager{
		clients:           make(map[string]*Client),
		configs:           make(map[string]ServerConfig),
		instructionCache:  make(map[string]string),
		turnAttachments:   make(map[string]TurnAttachment),
		consecutiveErrors: make(map[string]int),
		reconnectAttempts: make(map[string]int),
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
		m.bindNotificationHandler(cfg.Name, client)

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
		m.configs[cfg.Name] = cfg
		m.setInstructionCacheLocked(cfg.Name, client.GetInstructions())
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
		result[name] = client.GetTools()
	}
	return result
}

// CallTool invokes a tool on the specified MCP server. After 3 consecutive
// errors, automatically attempts to reconnect the server (up to 5 times).
func (m *Manager) CallTool(serverName, toolName string, args map[string]any) (string, error) {
	m.mu.RLock()
	client, ok := m.clients[serverName]
	m.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("MCP server %q not connected", serverName)
	}

	result, err := client.CallTool(toolName, args)
	if err != nil {
		m.mu.Lock()
		m.consecutiveErrors[serverName]++
		errCount := m.consecutiveErrors[serverName]
		m.mu.Unlock()

		// Auto-reconnect after threshold consecutive failures.
		if errCount >= maxConsecutiveErrors {
			if reconnErr := m.Reconnect(serverName); reconnErr == nil {
				// Retry the call once on the fresh connection.
				return m.retryCall(serverName, toolName, args)
			}
		}
		return result, err
	}

	// Success: reset error counter.
	m.mu.Lock()
	m.consecutiveErrors[serverName] = 0
	m.mu.Unlock()

	return result, nil
}

// retryCall performs a single retry of CallTool on a freshly reconnected server.
func (m *Manager) retryCall(serverName, toolName string, args map[string]any) (string, error) {
	m.mu.RLock()
	client, ok := m.clients[serverName]
	m.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("MCP server %q not connected after reconnect", serverName)
	}
	return client.CallTool(toolName, args)
}

// Reconnect tears down and re-initializes the named MCP server connection.
// Returns an error if the server config is unknown or reconnect attempts are
// exhausted (capped at 5).
func (m *Manager) Reconnect(name string) error {
	m.mu.Lock()
	cfg, hasCfg := m.configs[name]
	attempts := m.reconnectAttempts[name]
	if attempts >= maxReconnectAttempts {
		m.mu.Unlock()
		return fmt.Errorf("MCP server %q: reconnect attempts exhausted (%d/%d)", name, attempts, maxReconnectAttempts)
	}
	m.reconnectAttempts[name] = attempts + 1
	m.mu.Unlock()

	if !hasCfg {
		return fmt.Errorf("MCP server %q: no config available for reconnect", name)
	}

	// Close the existing client if present.
	m.mu.RLock()
	oldClient, hasOld := m.clients[name]
	m.mu.RUnlock()
	if hasOld {
		oldClient.Close()
	}

	// Spawn and initialize a fresh client.
	client, err := NewStdioClient(cfg)
	if err != nil {
		return fmt.Errorf("MCP server %q reconnect spawn: %w", name, err)
	}
	m.bindNotificationHandler(name, client)

	if err := client.Initialize(); err != nil {
		client.Close()
		return fmt.Errorf("MCP server %q reconnect init: %w", name, err)
	}

	if _, err := client.ListTools(); err != nil {
		client.Close()
		return fmt.Errorf("MCP server %q reconnect list tools: %w", name, err)
	}

	m.mu.Lock()
	m.clients[name] = client
	m.setInstructionCacheLocked(name, client.GetInstructions())
	m.consecutiveErrors[name] = 0
	m.mu.Unlock()

	return nil
}

// GetInstructions concatenates instructions from all connected servers.
func (m *Manager) GetInstructions() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var parts []string
	names := make([]string, 0, len(m.instructionCache))
	for name := range m.instructionCache {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if inst := m.instructionCache[name]; inst != "" {
			parts = append(parts, fmt.Sprintf("## %s\n%s", name, inst))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "# MCP Server Instructions\n\n" + strings.Join(parts, "\n\n")
}

// TakeTurnAttachments returns and clears the queued MCP attachments for the next turn.
func (m *Manager) TakeTurnAttachments() []TurnAttachment {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.turnAttachments) == 0 {
		return nil
	}

	names := make([]string, 0, len(m.turnAttachments))
	for name := range m.turnAttachments {
		names = append(names, name)
	}
	sort.Strings(names)

	attachments := make([]TurnAttachment, 0, len(names))
	for _, name := range names {
		attachments = append(attachments, m.turnAttachments[name])
		delete(m.turnAttachments, name)
	}
	return attachments
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
	names := make([]string, 0, len(m.clients))
	for name := range m.clients {
		names = append(names, name)
	}
	m.mu.RUnlock()

	for _, name := range names {
		m.refreshToolsForServer(name)
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
	m.instructionCache = make(map[string]string)
	m.turnAttachments = make(map[string]TurnAttachment)
}

func (m *Manager) bindNotificationHandler(serverName string, client *Client) {
	client.SetNotificationHandler(func(method string, params json.RawMessage) {
		m.handleNotification(serverName, method, params)
	})
}

func (m *Manager) handleNotification(serverName, method string, params json.RawMessage) {
	switch {
	case method == "notifications/tools/list_changed":
		go m.refreshToolsForServer(serverName)
	case strings.HasPrefix(method, "notifications/instructions/"):
		text, ok := extractInstructionText(params)
		if !ok {
			return
		}
		m.mu.Lock()
		m.instructionCache[serverName] = text
		m.turnAttachments[serverName] = TurnAttachment{
			ServerName: serverName,
			Content:    buildInstructionAttachment(serverName, text),
		}
		m.mu.Unlock()
	}
}

func (m *Manager) refreshToolsForServer(name string) {
	m.mu.RLock()
	client, ok := m.clients[name]
	m.mu.RUnlock()
	if !ok {
		return
	}

	_, _ = client.ListTools()
}

func (m *Manager) setInstructionCacheLocked(name, instructions string) {
	if instructions == "" {
		delete(m.instructionCache, name)
		return
	}
	m.instructionCache[name] = instructions
}

func buildInstructionAttachment(serverName, instructions string) string {
	return fmt.Sprintf(
		"<system-reminder source=\"mcp\" server=%q>\nMCP server instructions updated.\n\n%s\n</system-reminder>",
		serverName,
		instructions,
	)
}

func extractInstructionText(params json.RawMessage) (string, bool) {
	var direct string
	if err := json.Unmarshal(params, &direct); err == nil && direct != "" {
		return direct, true
	}

	var payload map[string]any
	if err := json.Unmarshal(params, &payload); err != nil {
		return "", false
	}

	for _, key := range []string{"instructions", "instruction", "text", "content", "value", "delta"} {
		if value, ok := firstInstructionString(payload[key]); ok {
			return value, true
		}
	}

	return "", false
}

func firstInstructionString(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, typed != ""
	case map[string]any:
		for _, key := range []string{"text", "content", "value"} {
			if nested, ok := typed[key].(string); ok && nested != "" {
				return nested, true
			}
		}
	}
	return "", false
}
