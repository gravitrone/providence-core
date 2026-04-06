package tools

import "context"

// WebSearchTool is a stub for searching the web.
type WebSearchTool struct{}

func (t *WebSearchTool) Name() string        { return "WebSearch" }
func (t *WebSearchTool) Description() string  { return "Search the web for a query." }
func (t *WebSearchTool) ReadOnly() bool       { return true }

func (t *WebSearchTool) InputSchema() map[string]any {
	return map[string]any{
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "The search query.",
			},
		},
		"required": []string{"query"},
	}
}

func (t *WebSearchTool) Execute(_ context.Context, _ map[string]any) ToolResult {
	return ToolResult{Content: "WebSearch not yet implemented", IsError: true}
}
