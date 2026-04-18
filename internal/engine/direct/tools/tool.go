package tools

import (
	"context"
	"strings"

	"github.com/gravitrone/providence-core/internal/engine/hooks"
)

// ToolResult is the output of a tool execution.
type ToolResult struct {
	Content  string
	IsError  bool
	Metadata map[string]any // optional (e.g. base64 images)

	// ContextModifier is an optional function the tool can return to mutate
	// engine state after execution (e.g. update file read cache, change
	// permission mode). Called in the engine's tool execution loop after the
	// result is recorded. May be nil.
	ContextModifier func()
}

// HookEmitter dispatches lifecycle hook events from tool implementations.
type HookEmitter func(event string, input hooks.HookInput)

// Tool is the interface all direct-engine tools must implement.
type Tool interface {
	Name() string
	Description() string
	InputSchema() map[string]any // JSON Schema as Go map
	ReadOnly() bool              // true = safe for parallel execution
	Execute(ctx context.Context, input map[string]any) ToolResult
}

// ToolPrompter is an optional interface tools can implement to provide
// detailed guidance text injected into the system prompt alongside the tool schema.
// Modeled after CC's per-tool prompt system.
type ToolPrompter interface {
	Prompt() string
}

// ResultCapProvider is an optional interface tools can implement to override
// the default result-size cap applied after execution.
type ResultCapProvider interface {
	ResultSizeCap() int
}

// Registry holds a set of named tools.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry creates a registry from the given tools.
func NewRegistry(tools ...Tool) *Registry {
	r := &Registry{tools: make(map[string]Tool)}
	for _, t := range tools {
		r.tools[t.Name()] = wrapToolWithResultCap(t)
	}
	return r
}

// Register adds a tool to the registry, overwriting any existing tool with the same name.
func (r *Registry) Register(t Tool) {
	r.tools[t.Name()] = wrapToolWithResultCap(t)
}

// Get returns a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// All returns every registered tool.
func (r *Registry) All() []Tool {
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

// CollectToolPrompts iterates all registered tools and concatenates the Prompt()
// output from those implementing ToolPrompter. The result is a single string
// suitable for injection into the system prompt.
func CollectToolPrompts(reg *Registry) string {
	var parts []string
	for _, t := range reg.All() {
		if p, ok := unwrapTool(t).(ToolPrompter); ok {
			if text := p.Prompt(); text != "" {
				parts = append(parts, "## "+t.Name()+"\n\n"+text)
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "# Tool Guidance\n\n" + strings.Join(parts, "\n\n")
}

// DefaultToolPrompts returns the collected tool prompt text using a minimal
// set of core tools. This is safe to call without runtime dependencies
// (no FileState, no subagent.Runner, no macos.Bridge needed) because Prompt()
// methods only return static text.
func DefaultToolPrompts() string {
	fs := NewFileState()
	prompters := []Tool{
		NewReadTool(fs),
		NewWriteTool(fs),
		NewEditTool(fs),
		&BashTool{},
		&GlobTool{},
		&GrepTool{},
		NewTodoWriteTool(),
		NewAskUserQuestionTool(nil),
		NewSkillTool(),
		SleepTool{},
	}
	reg := NewRegistry(prompters...)
	return CollectToolPrompts(reg)
}

// --- Type-Safe Param Helpers ---

func paramString(input map[string]any, key string, fallback string) string {
	v, ok := input[key]
	if !ok {
		return fallback
	}
	s, ok := v.(string)
	if !ok {
		return fallback
	}
	return s
}

func paramInt(input map[string]any, key string, fallback int) int {
	v, ok := input[key]
	if !ok {
		return fallback
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return fallback
	}
}

func paramFloat(input map[string]any, key string, fallback float64) float64 {
	v, ok := input[key]
	if !ok {
		return fallback
	}
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return fallback
	}
}

func paramBool(input map[string]any, key string, fallback bool) bool {
	v, ok := input[key]
	if !ok {
		return fallback
	}
	b, ok := v.(bool)
	if !ok {
		return fallback
	}
	return b
}
