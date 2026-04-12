package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gravitrone/providence-core/internal/store"
)

// SessionReadTool lets the model read messages from current or past sessions.
type SessionReadTool struct {
	store            *store.Store
	currentSessionID string
}

// NewSessionReadTool creates a SessionReadTool bound to the given store.
func NewSessionReadTool(st *store.Store, currentSessionID string) *SessionReadTool {
	return &SessionReadTool{store: st, currentSessionID: currentSessionID}
}

// SetCurrentSession updates the active session ID (called when a new session starts).
func (s *SessionReadTool) SetCurrentSession(id string) {
	s.currentSessionID = id
}

func (s *SessionReadTool) Name() string        { return "SessionRead" }
func (s *SessionReadTool) Description() string { return "Read messages from the current or a past session." }
func (s *SessionReadTool) ReadOnly() bool      { return true }

func (s *SessionReadTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Session ID to read. Empty for current session.",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "Number of messages to skip from the start (default 0).",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum messages to return (default 50).",
			},
		},
	}
}

type sessionMessage struct {
	Role      string `json:"role"`
	Content   string `json:"content,omitempty"`
	ToolName  string `json:"tool_name,omitempty"`
	CreatedAt string `json:"created_at"`
}

func (s *SessionReadTool) Execute(_ context.Context, input map[string]any) ToolResult {
	if s.store == nil {
		return ToolResult{Content: "session store not available", IsError: true}
	}

	sid := paramString(input, "session_id", "")
	if sid == "" {
		sid = s.currentSessionID
	}
	if sid == "" {
		return ToolResult{Content: "no session_id provided and no active session", IsError: true}
	}

	offset := paramInt(input, "offset", 0)
	limit := paramInt(input, "limit", 50)
	if limit < 1 {
		limit = 50
	}

	msgs, err := s.store.GetMessages(sid)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("failed to read messages: %v", err), IsError: true}
	}

	// Apply offset.
	if offset > 0 && offset < len(msgs) {
		msgs = msgs[offset:]
	} else if offset >= len(msgs) {
		msgs = nil
	}

	// Apply limit.
	if limit < len(msgs) {
		msgs = msgs[:limit]
	}

	out := make([]sessionMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, sessionMessage{
			Role:      m.Role,
			Content:   m.Content,
			ToolName:  m.ToolName,
			CreatedAt: m.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	data, err := json.Marshal(out)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("json marshal error: %v", err), IsError: true}
	}
	return ToolResult{Content: string(data)}
}
