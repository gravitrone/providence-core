package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gravitrone/providence-core/internal/store"
)

// SessionSearchTool lets the model search across all sessions using FTS5.
type SessionSearchTool struct {
	store *store.Store
}

// NewSessionSearchTool creates a SessionSearchTool bound to the given store.
func NewSessionSearchTool(st *store.Store) *SessionSearchTool {
	return &SessionSearchTool{store: st}
}

func (s *SessionSearchTool) Name() string        { return "SessionSearch" }
func (s *SessionSearchTool) Description() string { return "Search across all sessions for matching messages." }
func (s *SessionSearchTool) ReadOnly() bool      { return true }

func (s *SessionSearchTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query (FTS5 syntax supported).",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum results to return (default 10).",
			},
		},
		"required": []string{"query"},
	}
}

type searchResultJSON struct {
	SessionID string `json:"session_id"`
	Role      string `json:"role"`
	Snippet   string `json:"snippet"`
	CreatedAt string `json:"created_at"`
}

func (s *SessionSearchTool) Execute(_ context.Context, input map[string]any) ToolResult {
	if s.store == nil {
		return ToolResult{Content: "session store not available", IsError: true}
	}

	query := paramString(input, "query", "")
	if query == "" {
		return ToolResult{Content: "query is required", IsError: true}
	}

	limit := paramInt(input, "limit", 10)
	if limit < 1 {
		limit = 10
	}

	results, err := s.store.SearchMessages(query, limit)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("search failed: %v", err), IsError: true}
	}

	if len(results) == 0 {
		return ToolResult{Content: "[]"}
	}

	out := make([]searchResultJSON, 0, len(results))
	for _, r := range results {
		out = append(out, searchResultJSON{
			SessionID: r.SessionID,
			Role:      r.Role,
			Snippet:   r.Snippet,
			CreatedAt: r.CreatedAt,
		})
	}

	data, err := json.Marshal(out)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("json marshal error: %v", err), IsError: true}
	}
	return ToolResult{Content: string(data)}
}
