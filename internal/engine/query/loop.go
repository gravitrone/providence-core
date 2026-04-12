package query

import (
	"context"
	"fmt"
	"strings"
)

// Terminal is the loop completion reason.
type Terminal struct {
	Reason    string // "completed", "max_turns", "blocking_limit", "prompt_too_long", "cancelled", "error"
	TurnCount int
	Err       error
}

// LoopEvent is emitted by the query loop to the caller.
type LoopEvent struct {
	Type string // "text_delta", "tool_start", "tool_result", "usage", "error"
	Data interface{}
}

// TextDeltaData carries a text chunk from the model.
type TextDeltaData struct {
	Text string
}

// ToolStartData is emitted when a tool invocation begins.
type ToolStartData struct {
	ToolUseID string
	Name      string
	Input     string
}

// ToolResultData is emitted when a tool finishes.
type ToolResultData struct {
	ToolUseID string
	Name      string
	Content   string
	Err       error
}

// UsageData carries token usage from a provider response.
type UsageData struct {
	InputTokens  int
	OutputTokens int
}

// QueryLoop is the single agent loop. Every engine delegates here.
// It handles: streaming, tool execution, compaction triggers, error recovery.
// Returns an events channel for real-time updates and a done channel for the
// terminal result.
func QueryLoop(ctx context.Context, deps *Deps, initialState *State) (<-chan LoopEvent, <-chan Terminal) {
	events := make(chan LoopEvent, 100)
	done := make(chan Terminal, 1)

	go func() {
		defer close(events)
		defer close(done)

		state := initialState
		if state == nil {
			state = &State{}
		}

		for {
			select {
			case <-ctx.Done():
				done <- Terminal{Reason: "cancelled", TurnCount: state.TurnCount, Err: ctx.Err()}
				return
			default:
			}

			// Check max turns.
			if deps.MaxTurns > 0 && state.TurnCount >= deps.MaxTurns {
				done <- Terminal{Reason: "max_turns", TurnCount: state.TurnCount}
				return
			}

			// Pre-call: trigger auto-compact if needed.
			if deps.Compact != nil {
				deps.Compact.TriggerIfNeeded(ctx)
			}

			// Stream from provider.
			tools := make([]ToolDef, 0)
			if deps.Tools != nil {
				tools = deps.Tools.ListTools()
			}

			streamCh, err := deps.Provider.Stream(ctx, state.Messages, tools, deps.SystemPrompt)
			if err != nil {
				emit(events, LoopEvent{Type: "error", Data: err})
				done <- Terminal{Reason: "error", TurnCount: state.TurnCount, Err: fmt.Errorf("provider stream failed: %w", err)}
				return
			}

			// Accumulate the response.
			var assistantText strings.Builder
			var pendingTools []ToolCall
			var stopReason string

			for ev := range streamCh {
				select {
				case <-ctx.Done():
					done <- Terminal{Reason: "cancelled", TurnCount: state.TurnCount, Err: ctx.Err()}
					return
				default:
				}

				switch ev.Type {
				case "text_delta":
					assistantText.WriteString(ev.Text)
					emit(events, LoopEvent{Type: "text_delta", Data: TextDeltaData{Text: ev.Text}})

				case "tool_use_start":
					// Tool call detected - will be completed on tool_use_stop.

				case "tool_use_stop":
					pendingTools = append(pendingTools, ToolCall{
						ID:    ev.ToolUseID,
						Name:  ev.ToolName,
						Input: ev.ToolInput,
					})

				case "message_complete":
					stopReason = ev.StopReason
					if ev.InputTokens > 0 || ev.OutputTokens > 0 {
						emit(events, LoopEvent{Type: "usage", Data: UsageData{
							InputTokens:  ev.InputTokens,
							OutputTokens: ev.OutputTokens,
						}})
					}

				case "error":
					emit(events, LoopEvent{Type: "error", Data: ev.Error})
					done <- Terminal{Reason: "error", TurnCount: state.TurnCount, Err: ev.Error}
					return
				}
			}

			// Check cancellation after stream consumption.
			select {
			case <-ctx.Done():
				done <- Terminal{Reason: "cancelled", TurnCount: state.TurnCount, Err: ctx.Err()}
				return
			default:
			}

			// Append assistant message.
			assistantMsg := Message{
				Role:      "assistant",
				Content:   assistantText.String(),
				ToolCalls: pendingTools,
			}
			state.Messages = append(state.Messages, assistantMsg)
			state.TurnCount++

			// If no tool calls, we're done.
			if len(pendingTools) == 0 {
				done <- Terminal{Reason: "completed", TurnCount: state.TurnCount}
				return
			}

			// Handle end_turn with pending tools (shouldn't happen but be safe).
			if stopReason == "end_turn" && len(pendingTools) > 0 {
				done <- Terminal{Reason: "completed", TurnCount: state.TurnCount}
				return
			}

			// Execute tools.
			executor := NewStreamingToolExecutor(deps.Tools)
			for _, tc := range pendingTools {
				// Permission check.
				if deps.Permissions != nil {
					allowed, permErr := deps.Permissions.Check(tc.Name, tc.Input)
					if permErr != nil || !allowed {
						result := Message{
							Role:       "tool_result",
							ToolCallID: tc.ID,
							ToolName:   tc.Name,
							Content:    "permission denied",
						}
						state.Messages = append(state.Messages, result)
						emit(events, LoopEvent{Type: "tool_result", Data: ToolResultData{
							ToolUseID: tc.ID,
							Name:      tc.Name,
							Content:   "permission denied",
							Err:       permErr,
						}})
						continue
					}
				}

				emit(events, LoopEvent{Type: "tool_start", Data: ToolStartData{
					ToolUseID: tc.ID,
					Name:      tc.Name,
					Input:     tc.Input,
				}})
				executor.AddTool(tc.ID, tc.Name, tc.Input)
			}

			// Wait for all tool results.
			results := executor.GetRemainingResults()
			for _, r := range results {
				resultMsg := Message{
					Role:       "tool_result",
					ToolCallID: r.ToolUseID,
					ToolName:   r.Name,
					Content:    r.Content,
				}
				state.Messages = append(state.Messages, resultMsg)
				emit(events, LoopEvent{Type: "tool_result", Data: ToolResultData{
					ToolUseID: r.ToolUseID,
					Name:      r.Name,
					Content:   r.Content,
					Err:       r.Error,
				}})
			}

			// Run post-tool hooks.
			if deps.Hooks != nil {
				_ = deps.Hooks.Run(ctx, "post_tool", results)
			}

			// Loop back for next turn.
		}
	}()

	return events, done
}

// emit sends a LoopEvent to the channel, dropping if full.
func emit(ch chan<- LoopEvent, ev LoopEvent) {
	select {
	case ch <- ev:
	default:
		// Drop event if channel full - caller is too slow.
	}
}
