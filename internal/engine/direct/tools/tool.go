package tools

import "context"

// ToolResult is the output of a tool execution.
type ToolResult struct {
	Content  string
	IsError  bool
	Metadata map[string]any // optional (e.g. base64 images)
}

// Tool is the interface all direct-engine tools must implement.
type Tool interface {
	Name() string
	Description() string
	InputSchema() map[string]any // JSON Schema as Go map
	ReadOnly() bool              // true = safe for parallel execution
	Execute(ctx context.Context, input map[string]any) ToolResult
}

// Registry holds a set of named tools.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry creates a registry from the given tools.
func NewRegistry(tools ...Tool) *Registry {
	r := &Registry{tools: make(map[string]Tool)}
	for _, t := range tools {
		r.tools[t.Name()] = t
	}
	return r
}

// Register adds a tool to the registry, overwriting any existing tool with the same name.
func (r *Registry) Register(t Tool) {
	r.tools[t.Name()] = t
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

// -- type-safe param helpers --

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
