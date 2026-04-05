package claude

import (
	"encoding/json"
	"fmt"
)

// Inbound messages (sent to Claude's stdin)

type UserMessage struct {
	Type    string      `json:"type"`
	Message MessageBody `json:"message"`
}

type MessageBody struct {
	Role    string        `json:"role"`
	Content []ContentPart `json:"content"`
}

type ContentPart struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Input any    `json:"input,omitempty"`
}

type PermissionResponse struct {
	Type       string `json:"type"`
	QuestionID string `json:"question_id"`
	OptionID   string `json:"option_id"`
}

// Outbound events (received from Claude's stdout)

type Event struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`
}

type SystemInitEvent struct {
	Type      string   `json:"type"`
	Subtype   string   `json:"subtype"`
	SessionID string   `json:"session_id"`
	Tools     []string `json:"tools"`
	Model     string   `json:"model"`
}

type StreamEvent struct {
	Type  string          `json:"type"`
	Event StreamEventData `json:"event"`
}

type StreamEventData struct {
	Type  string       `json:"type"`
	Index int          `json:"index"`
	Delta *StreamDelta `json:"delta,omitempty"`
}

type StreamDelta struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type AssistantEvent struct {
	Type    string       `json:"type"`
	Message AssistantMsg `json:"message"`
}

type AssistantMsg struct {
	Content []ContentPart `json:"content"`
}

type ResultEvent struct {
	Type         string  `json:"type"`
	Subtype      string  `json:"subtype"`
	Result       string  `json:"result"`
	SessionID    string  `json:"session_id"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	IsError      bool    `json:"is_error"`
}

type PermissionRequestEvent struct {
	Type       string             `json:"type"`
	Tool       PermissionTool     `json:"tool"`
	QuestionID string             `json:"question_id"`
	Options    []PermissionOption `json:"options"`
}

type PermissionTool struct {
	Name  string `json:"name"`
	Input any    `json:"input"`
}

type PermissionOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// ParseEvent does a two-pass unmarshal: first to get the event type,
// then into the concrete struct for that type.
// Returns (eventType, parsedStruct, error).
func ParseEvent(line []byte) (string, any, error) {
	var base Event
	if err := json.Unmarshal(line, &base); err != nil {
		return "", nil, fmt.Errorf("parse event base: %w", err)
	}

	switch base.Type {
	case "system":
		if base.Subtype == "init" {
			var e SystemInitEvent
			if err := json.Unmarshal(line, &e); err != nil {
				return base.Type, nil, fmt.Errorf("parse system_init: %w", err)
			}
			return base.Type, &e, nil
		}
		return base.Type, &base, nil

	case "stream_event":
		var e StreamEvent
		if err := json.Unmarshal(line, &e); err != nil {
			return base.Type, nil, fmt.Errorf("parse stream_event: %w", err)
		}
		return base.Type, &e, nil

	case "assistant":
		var e AssistantEvent
		if err := json.Unmarshal(line, &e); err != nil {
			return base.Type, nil, fmt.Errorf("parse assistant: %w", err)
		}
		return base.Type, &e, nil

	case "result":
		var e ResultEvent
		if err := json.Unmarshal(line, &e); err != nil {
			return base.Type, nil, fmt.Errorf("parse result: %w", err)
		}
		return base.Type, &e, nil

	case "permission_request":
		var e PermissionRequestEvent
		if err := json.Unmarshal(line, &e); err != nil {
			return base.Type, nil, fmt.Errorf("parse permission_request: %w", err)
		}
		return base.Type, &e, nil

	default:
		return base.Type, &base, nil
	}
}
