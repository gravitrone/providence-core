package direct

import (
	"encoding/json"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/gravitrone/providence-core/internal/engine"
)

// translateStreamEvent converts an Anthropic SDK streaming event into an engine.ParsedEvent.
// Returns nil if the event should be skipped.
func translateStreamEvent(event anthropic.MessageStreamEventUnion) *engine.ParsedEvent {
	switch variant := event.AsAny().(type) {
	case anthropic.ContentBlockDeltaEvent:
		switch delta := variant.Delta.AsAny().(type) {
		case anthropic.TextDelta:
			return &engine.ParsedEvent{
				Type: "stream_event",
				Data: &engine.StreamEvent{
					Type: "stream_event",
					Event: engine.StreamEventData{
						Type:  "content_block_delta",
						Index: int(variant.Index),
						Delta: &engine.StreamDelta{
							Type: "text_delta",
							Text: delta.Text,
						},
					},
				},
			}
		case anthropic.InputJSONDelta:
			return &engine.ParsedEvent{
				Type: "tool_input_delta",
				Data: &engine.ToolInputDelta{
					Type:        "tool_input_delta",
					PartialJSON: delta.PartialJSON,
				},
			}
		default:
			return nil
		}

	default:
		return nil
	}
}

// extractToolCalls pulls tool use blocks from a completed message.
func extractToolCalls(msg anthropic.Message) []ToolCall {
	var calls []ToolCall
	for _, block := range msg.Content {
		if block.Type != "tool_use" {
			continue
		}
		var input map[string]any
		if len(block.Input) > 0 {
			_ = json.Unmarshal(block.Input, &input)
		}
		if input == nil {
			input = make(map[string]any)
		}
		calls = append(calls, ToolCall{
			ID:    block.ID,
			Name:  block.Name,
			Input: input,
		})
	}
	return calls
}
