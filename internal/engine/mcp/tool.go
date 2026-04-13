package mcp

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/gravitrone/providence-core/internal/engine/direct/tools"
)

var invalidNameChars = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// NormalizeName replaces characters invalid for tool names with underscores.
// Matches CC's normalizeNameForMCP: ^[a-zA-Z0-9_-]{1,64}$
func NormalizeName(name string) string {
	return invalidNameChars.ReplaceAllString(name, "_")
}

// BuildToolName constructs the fully qualified MCP tool name.
// Format: mcp__{serverName}__{toolName} (same as CC).
func BuildToolName(serverName, toolName string) string {
	return "mcp__" + NormalizeName(serverName) + "__" + NormalizeName(toolName)
}

// ParseToolName extracts server and tool names from a qualified MCP tool name.
// Returns ("", "", false) if the name doesn't match the mcp__server__tool pattern.
func ParseToolName(qualified string) (serverName, toolName string, ok bool) {
	if !strings.HasPrefix(qualified, "mcp__") {
		return "", "", false
	}
	rest := qualified[len("mcp__"):]
	idx := strings.Index(rest, "__")
	if idx < 0 {
		return "", "", false
	}
	return rest[:idx], rest[idx+2:], true
}

// MCPTool wraps a single MCP server tool as a Providence Tool interface.
type MCPTool struct {
	serverName string
	def        ToolDef
	manager    *Manager
	qualified  string // cached "mcp__server__tool" name
}

// NewMCPTool creates a Tool wrapper for an MCP server tool.
func NewMCPTool(serverName string, def ToolDef, mgr *Manager) *MCPTool {
	return &MCPTool{
		serverName: serverName,
		def:        def,
		manager:    mgr,
		qualified:  BuildToolName(serverName, def.Name),
	}
}

// Name returns the fully qualified tool name: mcp__{server}__{tool}.
func (t *MCPTool) Name() string {
	return t.qualified
}

// Description returns the tool description from the MCP server.
func (t *MCPTool) Description() string {
	desc := t.def.Description
	if desc == "" {
		desc = fmt.Sprintf("MCP tool %s from server %s", t.def.Name, t.serverName)
	}
	return desc
}

// InputSchema returns the JSON Schema from the MCP server's tool definition.
func (t *MCPTool) InputSchema() map[string]any {
	if t.def.InputSchema != nil {
		return t.def.InputSchema
	}
	// Fallback: empty object schema.
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

// ReadOnly returns false; MCP tools may have side effects.
func (t *MCPTool) ReadOnly() bool {
	return false
}

// Execute calls the MCP tool through the manager.
func (t *MCPTool) Execute(_ context.Context, input map[string]any) tools.ToolResult {
	result, err := t.manager.CallTool(t.serverName, t.def.Name, input)
	if err != nil {
		return tools.ToolResult{
			Content: err.Error(),
			IsError: true,
		}
	}
	return tools.ToolResult{
		Content: result,
		IsError: false,
	}
}

// RegisterMCPTools creates MCPTool wrappers for all tools from the manager
// and registers them in the provided tool registry.
func RegisterMCPTools(mgr *Manager, registry *tools.Registry) int {
	allTools := mgr.GetAllTools()
	count := 0
	for serverName, defs := range allTools {
		for _, def := range defs {
			tool := NewMCPTool(serverName, def, mgr)
			registry.Register(tool)
			count++
		}
	}
	return count
}
