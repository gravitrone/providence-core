package direct

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/gravitrone/providence-core/internal/engine/direct/tools"
)

// OpenRouterEndpoint is the chat completions endpoint for OpenRouter.
const OpenRouterEndpoint = "https://openrouter.ai/api/v1/chat/completions"

// openrouterRequest is the request body for OpenRouter's OpenAI-compatible API.
type openrouterRequest struct {
	Model    string              `json:"model"`
	Messages []openrouterMessage `json:"messages"`
	Stream   bool                `json:"stream"`
	Tools    []openrouterTool    `json:"tools,omitempty"`
}

// openrouterMessage is a single message in the OpenAI chat completions format.
// Tool calls use the `tool_calls` field on assistant messages, and tool results
// use role "tool" with `tool_call_id`.
type openrouterMessage struct {
	Role       string                  `json:"role"`
	Content    string                  `json:"content,omitempty"`
	Name       string                  `json:"name,omitempty"`
	ToolCallID string                  `json:"tool_call_id,omitempty"`
	ToolCalls  []openrouterToolCallMsg `json:"tool_calls,omitempty"`
}

// openrouterToolCallMsg is the assistant-side tool call representation.
type openrouterToolCallMsg struct {
	ID       string                    `json:"id"`
	Type     string                    `json:"type"`
	Function openrouterToolCallFuncMsg `json:"function"`
}

type openrouterToolCallFuncMsg struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// openrouterTool is an OpenAI function-calling tool definition.
type openrouterTool struct {
	Type     string                 `json:"type"`
	Function openrouterToolFunction `json:"function"`
}

type openrouterToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// openrouterHistoryEntry is a message in the OpenRouter conversation history.
// It mirrors how codexHistoryEntry tracks roles and tool-call metadata so that
// the full OpenAI chat completions contract (text + assistant tool_calls +
// tool results) can be reconstructed on each request.
type openrouterHistoryEntry struct {
	Role      string                  // user, assistant, tool
	Content   string                  // plain text content
	ToolCalls []openrouterToolCallMsg // assistant tool_calls (if any)
	CallID    string                  // tool_call_id for role=tool
}

// openrouterToolCall is an in-flight tool call being assembled from a stream.
type openrouterToolCall struct {
	Index   int
	ID      string
	Name    string
	RawArgs string
}

// buildOpenRouterTools converts the tool registry to OpenAI function-calling format.
func buildOpenRouterTools(registry *tools.Registry) []openrouterTool {
	allTools := registry.All()
	out := make([]openrouterTool, 0, len(allTools))
	for _, t := range allTools {
		schema := t.InputSchema()
		params := map[string]any{
			"type": "object",
		}
		if props, ok := schema["properties"]; ok {
			params["properties"] = props
		}
		if req, ok := schema["required"]; ok {
			params["required"] = req
		}
		out = append(out, openrouterTool{
			Type: "function",
			Function: openrouterToolFunction{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  params,
			},
		})
	}
	return out
}

// buildOpenRouterMessages converts internal history to OpenAI chat messages.
// A system message is prepended when non-empty.
func buildOpenRouterMessages(system string, history []openrouterHistoryEntry) []openrouterMessage {
	msgs := make([]openrouterMessage, 0, len(history)+1)
	if system != "" {
		msgs = append(msgs, openrouterMessage{
			Role:    "system",
			Content: system,
		})
	}
	for _, entry := range history {
		switch entry.Role {
		case "tool":
			msgs = append(msgs, openrouterMessage{
				Role:       "tool",
				ToolCallID: entry.CallID,
				Content:    entry.Content,
			})
		case "assistant":
			m := openrouterMessage{
				Role:    "assistant",
				Content: entry.Content,
			}
			if len(entry.ToolCalls) > 0 {
				m.ToolCalls = entry.ToolCalls
			}
			msgs = append(msgs, m)
		default:
			msgs = append(msgs, openrouterMessage{
				Role:    entry.Role,
				Content: entry.Content,
			})
		}
	}
	return msgs
}

// openrouterAgentLoop runs the agent loop using the OpenRouter API.
func (e *DirectEngine) openrouterAgentLoop(ctx context.Context) {
	defer e.emitResult()
	e.emitSystemInit()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if e.openrouterAPIKey == "" {
			e.emitError(fmt.Errorf("openrouter: missing API key"))
			return
		}

		reqBody := openrouterRequest{
			Model:    e.model,
			Messages: buildOpenRouterMessages(e.system, e.openrouterHistory),
			Stream:   true,
			Tools:    buildOpenRouterTools(e.registry),
		}

		bodyBytes, err := json.Marshal(reqBody)
		if err != nil {
			e.emitError(fmt.Errorf("marshal openrouter request: %w", err))
			return
		}

		req, err := http.NewRequestWithContext(ctx, "POST", OpenRouterEndpoint, bytes.NewReader(bodyBytes))
		if err != nil {
			e.emitError(fmt.Errorf("create openrouter request: %w", err))
			return
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+e.openrouterAPIKey)
		// Optional ranking headers per OpenRouter docs.
		req.Header.Set("HTTP-Referer", "https://github.com/gravitrone/providence-core")
		req.Header.Set("X-Title", "Providence")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			e.emitError(fmt.Errorf("openrouter request: %w", err))
			return
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			e.emitError(fmt.Errorf("openrouter API error (%d): %s", resp.StatusCode, string(body)))
			return
		}

		textParts, toolCalls, err := e.parseOpenRouterStream(ctx, resp.Body)
		resp.Body.Close()
		if err != nil {
			e.emitError(err)
			return
		}

		// Build assistant content parts for the UI event.
		var contentParts []engine.ContentPart
		fullText := strings.Join(textParts, "")
		if fullText != "" {
			contentParts = append(contentParts, engine.ContentPart{
				Type: "text",
				Text: fullText,
			})
		}
		for _, tc := range toolCalls {
			var input any
			_ = json.Unmarshal([]byte(tc.RawArgs), &input)
			contentParts = append(contentParts, engine.ContentPart{
				Type:  "tool_use",
				ID:    tc.ID,
				Name:  tc.Name,
				Input: input,
			})
		}

		e.events <- engine.ParsedEvent{
			Type: "assistant",
			Data: &engine.AssistantEvent{
				Type:    "assistant",
				Message: engine.AssistantMsg{Content: contentParts},
			},
		}

		// Append assistant turn to history (text + tool_calls in a single
		// message, matching OpenAI's chat completions contract).
		assistantEntry := openrouterHistoryEntry{
			Role:    "assistant",
			Content: fullText,
		}
		for _, tc := range toolCalls {
			args := tc.RawArgs
			if args == "" {
				args = "{}"
			}
			assistantEntry.ToolCalls = append(assistantEntry.ToolCalls, openrouterToolCallMsg{
				ID:   tc.ID,
				Type: "function",
				Function: openrouterToolCallFuncMsg{
					Name:      tc.Name,
					Arguments: args,
				},
			})
		}
		e.openrouterHistory = append(e.openrouterHistory, assistantEntry)

		// No tool calls -> turn is complete.
		if len(toolCalls) == 0 {
			return
		}

		// Execute tool calls, mirroring codex agent loop semantics.
		for _, tc := range toolCalls {
			var input map[string]any
			_ = json.Unmarshal([]byte(tc.RawArgs), &input)
			if input == nil {
				input = make(map[string]any)
			}

			tool, ok := e.registry.Get(tc.Name)
			if !ok {
				e.openrouterHistory = append(e.openrouterHistory, openrouterHistoryEntry{
					Role:    "tool",
					Content: "unknown tool: " + tc.Name,
					CallID:  tc.ID,
				})
				continue
			}

			if e.permissions.NeedsPermission(tool) {
				approved := e.permissions.RequestPermission(tc.ID, e.events, tc.Name, input)
				if !approved {
					e.openrouterHistory = append(e.openrouterHistory, openrouterHistoryEntry{
						Role:    "tool",
						Content: "permission denied",
						CallID:  tc.ID,
					})
					continue
				}
			}

			result := tool.Execute(ctx, input)

			e.events <- engine.ParsedEvent{
				Type: "tool_result",
				Data: &engine.ToolResultEvent{
					Type:       "tool_result",
					ToolCallID: tc.ID,
					ToolName:   tc.Name,
					Output:     result.Content,
					IsError:    result.IsError,
				},
			}

			e.openrouterHistory = append(e.openrouterHistory, openrouterHistoryEntry{
				Role:    "tool",
				Content: result.Content,
				CallID:  tc.ID,
			})
		}

		e.drainSteeredMessagesOpenRouter()
	}
}

// parseOpenRouterStream reads SSE events from an OpenAI-style chat completions
// stream. It returns the accumulated text deltas and any completed tool calls.
func (e *DirectEngine) parseOpenRouterStream(ctx context.Context, body io.Reader) ([]string, []openrouterToolCall, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	var textParts []string
	// Tool calls stream in fragments keyed by index; assemble them here.
	toolCallsByIndex := make(map[int]*openrouterToolCall)
	var toolCallOrder []int

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return textParts, collectOpenRouterToolCalls(toolCallsByIndex, toolCallOrder), ctx.Err()
		default:
		}

		line := scanner.Text()
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			// Some providers send heartbeat comments or malformed lines;
			// skip rather than aborting the whole stream.
			continue
		}

		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				textParts = append(textParts, choice.Delta.Content)
				e.events <- engine.ParsedEvent{
					Type: "stream_event",
					Data: &engine.StreamEvent{
						Type: "stream_event",
						Event: engine.StreamEventData{
							Type:  "content_block_delta",
							Index: 0,
							Delta: &engine.StreamDelta{
								Type: "text_delta",
								Text: choice.Delta.Content,
							},
						},
					},
				}
			}

			for _, tcDelta := range choice.Delta.ToolCalls {
				tc, ok := toolCallsByIndex[tcDelta.Index]
				if !ok {
					tc = &openrouterToolCall{Index: tcDelta.Index}
					toolCallsByIndex[tcDelta.Index] = tc
					toolCallOrder = append(toolCallOrder, tcDelta.Index)
				}
				if tcDelta.ID != "" {
					tc.ID = tcDelta.ID
				}
				if tcDelta.Function.Name != "" {
					tc.Name = tcDelta.Function.Name
				}
				if tcDelta.Function.Arguments != "" {
					tc.RawArgs += tcDelta.Function.Arguments
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return textParts, collectOpenRouterToolCalls(toolCallsByIndex, toolCallOrder), fmt.Errorf("read openrouter stream: %w", err)
	}
	return textParts, collectOpenRouterToolCalls(toolCallsByIndex, toolCallOrder), nil
}

// collectOpenRouterToolCalls flattens the index map into an ordered slice.
func collectOpenRouterToolCalls(byIndex map[int]*openrouterToolCall, order []int) []openrouterToolCall {
	out := make([]openrouterToolCall, 0, len(order))
	for _, idx := range order {
		if tc, ok := byIndex[idx]; ok {
			out = append(out, *tc)
		}
	}
	return out
}

// drainSteeredMessagesOpenRouter drains steered messages into openrouter history.
func (e *DirectEngine) drainSteeredMessagesOpenRouter() {
	e.steerMu.Lock()
	msgs := e.steered
	e.steered = nil
	e.steerMu.Unlock()

	for _, msg := range msgs {
		e.openrouterHistory = append(e.openrouterHistory, openrouterHistoryEntry{
			Role:    "user",
			Content: msg,
		})
	}
}
