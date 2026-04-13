package direct

import (
	"encoding/json"
	"time"
)

// storeIface is the minimal store surface the engine needs for learnings.
// *store.Store satisfies this interface.
type storeIface interface {
	SaveSessionLearnings(id, learningsJSON string) error
}

// SessionLearnings captures mechanical facts from a completed session.
// Stored as JSON in sessions.learnings for future context recovery.
type SessionLearnings struct {
	SessionID   string        `json:"session_id"`
	Duration    string        `json:"duration"`
	ToolCalls   []ToolCallLog `json:"tool_calls,omitempty"`
	Errors      []string      `json:"errors,omitempty"`
	TurnCount   int           `json:"turn_count"`
}

// ToolCallLog is a single recorded tool invocation.
type ToolCallLog struct {
	Name   string `json:"name"`
	Target string `json:"target,omitempty"` // file path or query when applicable
}

// extractSessionLearnings pulls mechanical facts from the engine's conversation
// history. It does NOT call any LLM - it scans tool_use blocks only.
func (e *DirectEngine) extractSessionLearnings(startTime time.Time) SessionLearnings {
	l := SessionLearnings{
		SessionID: e.sessionID,
		Duration:  time.Since(startTime).Round(time.Second).String(),
	}

	msgs := e.history.Messages()
	for _, msg := range msgs {
		for _, block := range msg.Content {
			tu := block.OfToolUse
			if tu == nil {
				continue
			}
			entry := ToolCallLog{Name: tu.Name}
			// Extract file target from common fields.
			// Input is `any` - try as map first, then marshal/unmarshal.
			var inputMap map[string]any
			switch v := tu.Input.(type) {
			case map[string]any:
				inputMap = v
			default:
				// Re-encode and decode as a generic map.
				if data, err := json.Marshal(v); err == nil {
					_ = json.Unmarshal(data, &inputMap)
				}
			}
			for _, key := range []string{"file_path", "path", "pattern", "command", "query"} {
				if val, ok := inputMap[key]; ok {
					if s, ok := val.(string); ok && s != "" {
						entry.Target = s
						break
					}
				}
			}
			l.ToolCalls = append(l.ToolCalls, entry)
			l.TurnCount++
		}
	}

	return l
}

// saveSessionLearnings extracts and persists learnings for the current session.
// Silently ignores errors to avoid disrupting shutdown.
func (e *DirectEngine) saveSessionLearnings(st storeIface, startTime time.Time) {
	if st == nil {
		return
	}
	l := e.extractSessionLearnings(startTime)
	data, err := json.Marshal(l)
	if err != nil {
		return
	}
	_ = st.SaveSessionLearnings(e.sessionID, string(data))
}
