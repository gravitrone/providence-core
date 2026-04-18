package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/gravitrone/providence-core/internal/engine/subagent"
)

// WriteAgentMemoryTool lets a subagent persist lessons into its per-type memory.
// It is intentionally registered only in the subagent tool path so the main
// agent cannot silently mutate user config.
//
// Writes require explicit permission because the tool is not read-only: the
// engine's permission chain sees a non-Allow default and falls through to the
// default "ask" decision, preserving the user approval step.
type WriteAgentMemoryTool struct {
	// AgentType identifies the memory bucket. Empty string falls back to "default".
	AgentType string
	// ProjectRoot is the base directory for project and local scope files.
	// Must be non-empty for project writes.
	ProjectRoot string
}

// NewWriteAgentMemoryTool creates a WriteAgentMemoryTool bound to the given
// subagent type and project root.
func NewWriteAgentMemoryTool(agentType, projectRoot string) *WriteAgentMemoryTool {
	return &WriteAgentMemoryTool{
		AgentType:   agentType,
		ProjectRoot: projectRoot,
	}
}

// NewSubagentToolRegistry returns a registry containing only the tools that
// should be exposed to subagents but not to the main agent. Keep this narrow:
// callers should merge it onto the core registry when spawning a subagent.
func NewSubagentToolRegistry(agentType, projectRoot string) *Registry {
	return NewRegistry(
		NewWriteAgentMemoryTool(agentType, projectRoot),
	)
}

// Name returns the tool identifier used by the engine and permission chain.
func (w *WriteAgentMemoryTool) Name() string { return "WriteAgentMemory" }

// Description returns the short schema description surfaced to the model.
func (w *WriteAgentMemoryTool) Description() string {
	return "Persist a lesson into this subagent type's memory (user or project scope). Local scope is read-only. Requires explicit user approval."
}

// ReadOnly returns false so the permission chain routes the invocation through
// the standard approval flow instead of auto-allowing it.
func (w *WriteAgentMemoryTool) ReadOnly() bool { return false }

// Prompt implements ToolPrompter with guidance on when and how to record memory.
func (w *WriteAgentMemoryTool) Prompt() string {
	return `Record a durable lesson for this subagent type so future invocations inherit it.

Scopes:
- "user": lessons the user wants every subagent of this type to remember across projects
- "project": project-specific context, committed to the repo
- "local": READ-ONLY from your side, the user edits this manually

Operations:
- "append" (default): adds a timestamped entry to the end of the file
- "replace": overwrites the entire scope file, use sparingly

Each scope file is capped at 50 KB. Appends that would exceed the cap truncate the oldest entries first.`
}

// InputSchema describes the tool's input JSON contract.
func (w *WriteAgentMemoryTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"scope": map[string]any{
				"type":        "string",
				"description": "Memory scope to write to. One of: user, project. The local scope is read-only.",
				"enum":        []string{"user", "project"},
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Lesson or context to record. Plain text or markdown.",
			},
			"operation": map[string]any{
				"type":        "string",
				"description": "append (default) or replace.",
				"enum":        []string{"append", "replace"},
			},
		},
		"required": []string{"scope", "content"},
	}
}

// Execute runs the memory write. The engine's permission chain is expected to
// have already approved the invocation; see the package doc for the contract.
func (w *WriteAgentMemoryTool) Execute(ctx context.Context, input map[string]any) ToolResult {
	scope := strings.TrimSpace(paramString(input, "scope", ""))
	content := paramString(input, "content", "")
	operation := strings.TrimSpace(paramString(input, "operation", string(subagent.OperationAppend)))

	if scope == "" {
		return ToolResult{Content: "missing required field: scope", IsError: true}
	}
	if strings.TrimSpace(content) == "" {
		return ToolResult{Content: "missing required field: content", IsError: true}
	}

	ms := subagent.MemoryScope(scope)
	op := subagent.Operation(operation)

	projectRoot := w.ProjectRoot
	if ms == subagent.MemoryScopeProject && projectRoot == "" {
		return ToolResult{Content: "project scope requires a configured project root", IsError: true}
	}

	if err := subagent.WriteAgentMemoryScope(ms, w.AgentType, projectRoot, content, op); err != nil {
		return ToolResult{Content: fmt.Sprintf("write agent memory: %v", err), IsError: true}
	}

	return ToolResult{Content: fmt.Sprintf("wrote %s scope for agent type %q", scope, effectiveAgentType(w.AgentType))}
}

// effectiveAgentType mirrors the normalization used by subagent.WriteAgentMemoryScope
// so the tool's success message matches the on-disk path.
func effectiveAgentType(agentType string) string {
	if strings.TrimSpace(agentType) == "" {
		return "default"
	}
	return agentType
}
