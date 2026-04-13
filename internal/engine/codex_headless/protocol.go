package codex_headless

import (
	"encoding/json"
	"fmt"

	"github.com/gravitrone/providence-core/internal/engine"
)

// --- Codex JSONL Event Types ---

// codexBaseEvent is the envelope for all codex exec --json events.
type codexBaseEvent struct {
	Type string `json:"type"`
}

// codexItemCompletedEvent wraps an item.completed payload.
type codexItemCompletedEvent struct {
	Type string    `json:"type"`
	Item codexItem `json:"item"`
}

// codexItem is the polymorphic item body inside item.completed events.
type codexItem struct {
	Type string `json:"type"`
	// agent_message fields
	Text string `json:"text,omitempty"`
	// command_execution fields
	Command          string `json:"command,omitempty"`
	ExitCode         *int   `json:"exit_code,omitempty"`
	AggregatedOutput string `json:"aggregated_output,omitempty"`
	// file_change fields
	Changes json.RawMessage `json:"changes,omitempty"`
}

// codexTurnCompletedEvent wraps a turn.completed payload with usage.
type codexTurnCompletedEvent struct {
	Type  string     `json:"type"`
	Usage codexUsage `json:"usage"`
}

// codexUsage carries token counts from a completed turn.
type codexUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// codexErrorEvent wraps an error payload.
type codexErrorEvent struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// --- Event Mapping ---

// parseCodexEvent maps a single JSONL line from codex exec --json into a
// Providence ParsedEvent. Unknown event types are silently skipped (returns
// a zero ParsedEvent with empty Type).
func parseCodexEvent(line []byte) (engine.ParsedEvent, error) {
	var base codexBaseEvent
	if err := json.Unmarshal(line, &base); err != nil {
		return engine.ParsedEvent{}, fmt.Errorf("parse codex event base: %w", err)
	}

	switch base.Type {
	case "item.completed":
		return parseItemCompleted(line)

	case "turn.completed":
		return parseTurnCompleted(line)

	case "error", "turn.failed":
		return parseError(line, base.Type)

	default:
		// thread.started, turn.started, etc. - no Providence equivalent.
		return engine.ParsedEvent{}, nil
	}
}

// parseItemCompleted maps item.completed events to assistant or tool_use events.
func parseItemCompleted(line []byte) (engine.ParsedEvent, error) {
	var ev codexItemCompletedEvent
	if err := json.Unmarshal(line, &ev); err != nil {
		return engine.ParsedEvent{}, fmt.Errorf("parse item.completed: %w", err)
	}

	switch ev.Item.Type {
	case "agent_message":
		return engine.ParsedEvent{
			Type: "assistant",
			Data: &engine.AssistantEvent{
				Type: "assistant",
				Message: engine.AssistantMsg{
					Content: []engine.ContentPart{
						{Type: "text", Text: ev.Item.Text},
					},
				},
			},
			Raw: string(line),
		}, nil

	case "command_execution":
		exitCode := 0
		if ev.Item.ExitCode != nil {
			exitCode = *ev.Item.ExitCode
		}
		return engine.ParsedEvent{
			Type: "tool_result",
			Data: &engine.ToolResultEvent{
				Type:     "tool_result",
				ToolName: "command_execution",
				Output:   fmt.Sprintf("$ %s\n(exit %d)\n%s", ev.Item.Command, exitCode, ev.Item.AggregatedOutput),
				IsError:  exitCode != 0,
			},
			Raw: string(line),
		}, nil

	case "file_change":
		return engine.ParsedEvent{
			Type: "tool_result",
			Data: &engine.ToolResultEvent{
				Type:     "tool_result",
				ToolName: "file_change",
				Output:   string(ev.Item.Changes),
			},
			Raw: string(line),
		}, nil

	default:
		return engine.ParsedEvent{}, nil
	}
}

// parseTurnCompleted maps turn.completed to a result event.
func parseTurnCompleted(line []byte) (engine.ParsedEvent, error) {
	var ev codexTurnCompletedEvent
	if err := json.Unmarshal(line, &ev); err != nil {
		return engine.ParsedEvent{}, fmt.Errorf("parse turn.completed: %w", err)
	}

	return engine.ParsedEvent{
		Type: "result",
		Data: &engine.ResultEvent{
			Type:    "result",
			Subtype: "success",
			Result:  "Turn completed",
		},
		Raw: string(line),
	}, nil
}

// parseError maps error and turn.failed events to error result events.
func parseError(line []byte, evType string) (engine.ParsedEvent, error) {
	var ev codexErrorEvent
	if err := json.Unmarshal(line, &ev); err != nil {
		return engine.ParsedEvent{}, fmt.Errorf("parse %s: %w", evType, err)
	}

	msg := ev.Message
	if msg == "" {
		msg = evType
	}

	return engine.ParsedEvent{
		Type: "result",
		Data: &engine.ResultEvent{
			Type:    "result",
			Subtype: "error",
			Result:  msg,
			IsError: true,
		},
		Raw: string(line),
	}, nil
}
