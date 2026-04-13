package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ToolSearchTool lets the model discover tools not in the initial prompt.
// For v1 it returns all registered tool schemas. Future versions will support
// actual deferral where only a subset of tools are loaded initially.
type ToolSearchTool struct {
	registry *Registry
}

// NewToolSearchTool creates a ToolSearchTool backed by the given registry.
func NewToolSearchTool(reg *Registry) *ToolSearchTool {
	return &ToolSearchTool{registry: reg}
}

func (t *ToolSearchTool) Name() string { return "ToolSearch" }
func (t *ToolSearchTool) Description() string {
	return "Fetches full schema definitions for deferred tools so they can be called. Use \"select:Name1,Name2\" for exact lookup or keywords for fuzzy search."
}
func (t *ToolSearchTool) ReadOnly() bool { return true }

func (t *ToolSearchTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Query to find tools. Use \"select:<name>[,<name>...]\" for direct selection, or keywords to search.",
			},
			"max_results": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results to return (default: 5)",
				"default":     5,
			},
		},
		"required": []string{"query"},
	}
}

// Prompt implements ToolPrompter.
func (t *ToolSearchTool) Prompt() string {
	return `Fetches full schema definitions for deferred tools so they can be called.

Query forms:
- "select:Read,Edit,Grep" - fetch these exact tools by name
- "notebook jupyter" - keyword search, up to max_results best matches`
}

// Execute searches the registry and returns matching tool schemas.
func (t *ToolSearchTool) Execute(ctx context.Context, input map[string]any) ToolResult {
	query := paramString(input, "query", "")
	if query == "" {
		return ToolResult{Content: "Error: query parameter is required", IsError: true}
	}

	maxResults := paramInt(input, "max_results", 5)
	if maxResults < 1 {
		maxResults = 1
	}
	if maxResults > 50 {
		maxResults = 50
	}

	var matched []Tool

	if strings.HasPrefix(query, "select:") {
		// Direct selection: "select:Read,Edit,Grep"
		names := strings.Split(query[len("select:"):], ",")
		for _, name := range names {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if tool, ok := t.registry.Get(name); ok {
				matched = append(matched, tool)
			}
		}
		if len(matched) == 0 {
			return ToolResult{Content: fmt.Sprintf("No tools found matching: %s", query)}
		}
	} else {
		// Keyword search: match tools whose name or description contains query terms.
		terms := strings.Fields(strings.ToLower(query))
		all := t.registry.All()

		type scored struct {
			tool  Tool
			score int
		}
		var results []scored

		for _, tool := range all {
			name := strings.ToLower(tool.Name())
			desc := strings.ToLower(tool.Description())
			score := 0
			for _, term := range terms {
				if strings.Contains(name, term) {
					score += 10 // Name match is stronger
				}
				if strings.Contains(desc, term) {
					score += 1
				}
			}
			if score > 0 {
				results = append(results, scored{tool: tool, score: score})
			}
		}

		// Sort by score descending (simple insertion sort for small N).
		for i := 1; i < len(results); i++ {
			for j := i; j > 0 && results[j].score > results[j-1].score; j-- {
				results[j], results[j-1] = results[j-1], results[j]
			}
		}

		for i, r := range results {
			if i >= maxResults {
				break
			}
			matched = append(matched, r.tool)
		}

		if len(matched) == 0 {
			return ToolResult{Content: fmt.Sprintf("No tools found matching keywords: %s", query)}
		}
	}

	// Build schema output.
	var schemas []map[string]any
	for _, tool := range matched {
		schema := map[string]any{
			"name":        tool.Name(),
			"description": tool.Description(),
			"parameters":  tool.InputSchema(),
			"read_only":   tool.ReadOnly(),
		}
		schemas = append(schemas, schema)
	}

	data, err := json.MarshalIndent(schemas, "", "  ")
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("Error serializing schemas: %v", err), IsError: true}
	}

	return ToolResult{Content: string(data)}
}
