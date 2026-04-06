package tools

import "context"

// WebFetchTool is a stub for fetching web page content.
type WebFetchTool struct{}

func (t *WebFetchTool) Name() string        { return "WebFetch" }
func (t *WebFetchTool) Description() string  { return "Fetch the content of a web page by URL." }
func (t *WebFetchTool) ReadOnly() bool       { return true }

func (t *WebFetchTool) InputSchema() map[string]any {
	return map[string]any{
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "The URL to fetch.",
			},
		},
		"required": []string{"url"},
	}
}

func (t *WebFetchTool) Execute(_ context.Context, _ map[string]any) ToolResult {
	return ToolResult{Content: "WebFetch not yet implemented", IsError: true}
}
