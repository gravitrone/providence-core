package claude

import (
	"encoding/json"
	"fmt"

	"github.com/gravitrone/providence-core/internal/engine"
)

// --- Inbound Message Types ---

// UserMessage is the NDJSON envelope for a user turn sent to Claude's stdin.
type UserMessage struct {
	Type    string             `json:"type"`
	Message engine.MessageBody `json:"message"`
}

// PermissionResponse is the NDJSON response to a permission_request event.
type PermissionResponse struct {
	Type       string `json:"type"`
	QuestionID string `json:"question_id"`
	OptionID   string `json:"option_id"`
}

// ParseEvent does a two-pass unmarshal: first to get the event type,
// then into the concrete struct for that type.
// Returns (eventType, parsedStruct, error).
func ParseEvent(line []byte) (string, any, error) {
	var base engine.Event
	if err := json.Unmarshal(line, &base); err != nil {
		return "", nil, fmt.Errorf("parse event base: %w", err)
	}

	switch base.Type {
	case "system":
		if base.Subtype == "init" {
			var e engine.SystemInitEvent
			if err := json.Unmarshal(line, &e); err != nil {
				return base.Type, nil, fmt.Errorf("parse system_init: %w", err)
			}
			return base.Type, &e, nil
		}
		return base.Type, &base, nil

	case "stream_event":
		var e engine.StreamEvent
		if err := json.Unmarshal(line, &e); err != nil {
			return base.Type, nil, fmt.Errorf("parse stream_event: %w", err)
		}
		return base.Type, &e, nil

	case "assistant":
		var e engine.AssistantEvent
		if err := json.Unmarshal(line, &e); err != nil {
			return base.Type, nil, fmt.Errorf("parse assistant: %w", err)
		}
		return base.Type, &e, nil

	case "result":
		var e engine.ResultEvent
		if err := json.Unmarshal(line, &e); err != nil {
			return base.Type, nil, fmt.Errorf("parse result: %w", err)
		}
		return base.Type, &e, nil

	case "permission_request":
		var e engine.PermissionRequestEvent
		if err := json.Unmarshal(line, &e); err != nil {
			return base.Type, nil, fmt.Errorf("parse permission_request: %w", err)
		}
		return base.Type, &e, nil

	default:
		return base.Type, &base, nil
	}
}
