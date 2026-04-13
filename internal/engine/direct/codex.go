package direct

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gravitrone/providence-core/internal/auth"
	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/gravitrone/providence-core/internal/engine/direct/tools"
)

// codexRequest is the request body for the Codex API.
type codexRequest struct {
	Model        string            `json:"model"`
	Store        bool              `json:"store"`
	Stream       bool              `json:"stream"`
	Instructions string            `json:"instructions"`
	Input        []json.RawMessage `json:"input"`
	ToolChoice   string            `json:"tool_choice,omitempty"`
	Tools        []codexTool       `json:"tools,omitempty"`
}

type codexMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type codexTool struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// codexSSEEvent represents a parsed SSE event from the Codex stream.
type codexSSEEvent struct {
	Type string
	Data json.RawMessage
}

// codexResponseDelta is a text delta from the Codex SSE stream.
type codexResponseDelta struct {
	Type   string `json:"type"`
	ItemID string `json:"item_id"`
	Delta  string `json:"delta"`
}

// codexResponseItem is a completed item from the stream.
type codexResponseItem struct {
	Type string          `json:"type"`
	Item json.RawMessage `json:"item"`
}

// codexItem represents a completed output item.
type codexItem struct {
	Type      string         `json:"type"`
	ID        string         `json:"id"`
	Content   []codexContent `json:"content,omitempty"`
	Name      string         `json:"name,omitempty"`
	CallID    string         `json:"call_id,omitempty"`
	Arguments string         `json:"arguments,omitempty"`
	Output    string         `json:"output,omitempty"`
	Status    string         `json:"status,omitempty"`
}

type codexContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// codexDone is the response.done or response.completed event.
type codexDone struct {
	Type     string `json:"type"`
	Response struct {
		Status string         `json:"status"`
		Usage  map[string]any `json:"usage"`
	} `json:"response"`
}

// buildCodexTools converts the tool registry to Codex tool format.
func buildCodexTools(registry *tools.Registry) []codexTool {
	allTools := registry.All()
	out := make([]codexTool, 0, len(allTools))
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
		out = append(out, codexTool{
			Type:        "function",
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  params,
		})
	}
	return out
}

// buildCodexMessages converts the internal message history to codex format.
// Tool results use the function_call_output type with call_id.
func buildCodexMessages(history []codexHistoryEntry) []json.RawMessage {
	var msgs []json.RawMessage
	for _, entry := range history {
		switch entry.Role {
		case "function_call":
			msg := map[string]any{
				"type":      "function_call",
				"call_id":   entry.CallID,
				"name":      entry.FuncName,
				"arguments": entry.Content,
			}
			b, _ := json.Marshal(msg)
			msgs = append(msgs, b)
		case "tool":
			msg := map[string]any{
				"type":    "function_call_output",
				"call_id": entry.CallID,
				"output":  entry.Content,
			}
			b, _ := json.Marshal(msg)
			msgs = append(msgs, b)
		default:
			msg := map[string]any{
				"role":    entry.Role,
				"content": entry.Content,
			}
			b, _ := json.Marshal(msg)
			msgs = append(msgs, b)
		}
	}
	return msgs
}

// codexHistoryEntry is a message in the codex conversation history.
type codexHistoryEntry struct {
	Role     string
	Content  string
	CallID   string // for function_call and function_call_output
	FuncName string // for function_call items
}

// compressCodexToolResults replaces oversized older tool outputs with a stub.
func compressCodexToolResults(items []codexHistoryEntry, minLen int) int {
	if len(items) <= 4 {
		return 0
	}

	compressed := 0
	for i := 0; i < len(items)-4; i++ {
		if items[i].Role != "tool" {
			continue
		}
		if len(items[i].Content) <= minLen {
			continue
		}

		items[i].Content = fmt.Sprintf(
			"[compressed: %d chars from call_id=%s]",
			len(items[i].Content),
			items[i].CallID,
		)
		compressed++
	}

	return compressed
}

// codexAgentLoop runs the agent loop using the Codex API.
func (e *DirectEngine) codexAgentLoop(ctx context.Context) {
	defer e.emitResult()
	e.emitSystemInit()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		tokens, err := auth.EnsureValidOpenAITokens()
		if err != nil {
			e.emitError(fmt.Errorf("openai auth: %w", err))
			return
		}

		// OpenAI Codex does not support prompt caching via cache_control; system prompt is plain text.
		reqBody := codexRequest{
			Model:        e.model,
			Store:        false,
			Stream:       true,
			Instructions: e.system,
			Input:        buildCodexMessages(e.codexHistory),
			ToolChoice:   "auto",
			Tools:        buildCodexTools(e.registry),
		}

		bodyBytes, err := json.Marshal(reqBody)
		if err != nil {
			e.emitError(fmt.Errorf("marshal codex request: %w", err))
			return
		}

		req, err := http.NewRequestWithContext(ctx, "POST", auth.CodexEndpoint, bytes.NewReader(bodyBytes))
		if err != nil {
			e.emitError(fmt.Errorf("create codex request: %w", err))
			return
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
		if tokens.AccountID != "" {
			req.Header.Set("X-OpenAI-Account-ID", tokens.AccountID)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			e.emitError(fmt.Errorf("codex request: %w", err))
			return
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			e.emitError(fmt.Errorf("codex API error (%d): %s", resp.StatusCode, string(body)))
			return
		}

		textParts, toolCalls, err := e.parseCodexStream(ctx, resp.Body)
		resp.Body.Close()
		if err != nil {
			e.emitError(err)
			return
		}

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

		if fullText != "" {
			e.codexHistory = append(e.codexHistory, codexHistoryEntry{
				Role:    "assistant",
				Content: fullText,
			})
		}
		// Append function_call items so Codex can match call_ids on the next turn.
		for _, tc := range toolCalls {
			e.codexHistory = append(e.codexHistory, codexHistoryEntry{
				Role:     "function_call",
				Content:  tc.RawArgs,
				CallID:   tc.ID,
				FuncName: tc.Name,
			})
		}

		if len(toolCalls) == 0 {
			compressCodexToolResults(e.codexHistory, 2000)
			return
		}

		for _, tc := range toolCalls {
			var input map[string]any
			_ = json.Unmarshal([]byte(tc.RawArgs), &input)
			if input == nil {
				input = make(map[string]any)
			}

			tool, ok := e.registry.Get(tc.Name)
			if !ok {
				e.codexHistory = append(e.codexHistory, codexHistoryEntry{
					Role:    "tool",
					Content: "unknown tool: " + tc.Name,
					CallID:  tc.ID,
				})
				continue
			}

			if e.permissions.NeedsPermission(tool) {
				approved := e.permissions.RequestPermission(tc.ID, e.events, tc.Name, input)
				if !approved {
					e.codexHistory = append(e.codexHistory, codexHistoryEntry{
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

			e.codexHistory = append(e.codexHistory, codexHistoryEntry{
				Role:    "tool",
				Content: result.Content,
				CallID:  tc.ID,
			})
		}
		compressCodexToolResults(e.codexHistory, 2000)

		e.drainSteeredMessagesCodex()
	}
}

// codexToolCall represents a parsed tool call from the Codex stream.
type codexToolCall struct {
	ID      string
	Name    string
	RawArgs string
}

// parseCodexStream reads SSE events from the Codex response body.
// Returns accumulated text parts and any tool calls.
func (e *DirectEngine) parseCodexStream(ctx context.Context, body io.Reader) ([]string, []codexToolCall, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	var textParts []string
	var toolCalls []codexToolCall
	toolCallArgs := make(map[string]*codexToolCall)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return textParts, toolCalls, ctx.Err()
		default:
		}

		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var baseEvent struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(data), &baseEvent); err != nil {
			continue
		}

		switch baseEvent.Type {
		case "response.output_text.delta":
			var delta struct {
				Delta  string `json:"delta"`
				ItemID string `json:"item_id"`
			}
			if err := json.Unmarshal([]byte(data), &delta); err == nil && delta.Delta != "" {
				textParts = append(textParts, delta.Delta)
				e.events <- engine.ParsedEvent{
					Type: "stream_event",
					Data: &engine.StreamEvent{
						Type: "stream_event",
						Event: engine.StreamEventData{
							Type:  "content_block_delta",
							Index: 0,
							Delta: &engine.StreamDelta{
								Type: "text_delta",
								Text: delta.Delta,
							},
						},
					},
				}
			}

		case "response.function_call_arguments.delta":
			var delta struct {
				ItemID string `json:"item_id"`
				Delta  string `json:"delta"`
			}
			if err := json.Unmarshal([]byte(data), &delta); err == nil {
				tc, ok := toolCallArgs[delta.ItemID]
				if !ok {
					tc = &codexToolCall{ID: delta.ItemID}
					toolCallArgs[delta.ItemID] = tc
				}
				tc.RawArgs += delta.Delta
			}

		case "response.output_item.added":
			var item struct {
				Item struct {
					Type   string `json:"type"`
					ID     string `json:"id"`
					Name   string `json:"name"`
					CallID string `json:"call_id"`
				} `json:"item"`
			}
			if err := json.Unmarshal([]byte(data), &item); err == nil {
				if item.Item.Type == "function_call" {
					tc := &codexToolCall{
						ID:   item.Item.CallID,
						Name: item.Item.Name,
					}
					toolCallArgs[item.Item.ID] = tc
				}
			}

		case "response.output_item.done":
			var item struct {
				Item struct {
					Type      string `json:"type"`
					ID        string `json:"id"`
					CallID    string `json:"call_id"`
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"item"`
			}
			if err := json.Unmarshal([]byte(data), &item); err == nil {
				if item.Item.Type == "function_call" {
					tc, ok := toolCallArgs[item.Item.ID]
					if ok {
						if tc.Name == "" {
							tc.Name = item.Item.Name
						}
						if tc.ID == "" {
							tc.ID = item.Item.CallID
						}
						// Use the final arguments if we have them.
						if item.Item.Arguments != "" {
							tc.RawArgs = item.Item.Arguments
						}
						toolCalls = append(toolCalls, *tc)
					} else {
						toolCalls = append(toolCalls, codexToolCall{
							ID:      item.Item.CallID,
							Name:    item.Item.Name,
							RawArgs: item.Item.Arguments,
						})
					}
				}
			}

		case "response.done", "response.completed":
			var done codexDone
			if err := json.Unmarshal([]byte(data), &done); err == nil {
				if inputTokens, outputTokens, ok := extractCodexUsage(done); ok {
					e.emitUsageUpdate(inputTokens, outputTokens, 0, 0)
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return textParts, toolCalls, fmt.Errorf("read codex stream: %w", err)
	}
	return textParts, toolCalls, nil
}

func extractCodexUsage(done codexDone) (int, int, bool) {
	if done.Response.Usage == nil {
		return 0, 0, false
	}

	inputTokens, inputOK := parseCodexTokenCount(done.Response.Usage["input_tokens"])
	outputTokens, outputOK := parseCodexTokenCount(done.Response.Usage["output_tokens"])
	return inputTokens, outputTokens, inputOK || outputOK
}

func parseCodexTokenCount(value any) (int, bool) {
	switch v := value.(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0, false
		}
		return int(n), true
	case string:
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

// drainSteeredMessagesCodex drains steered messages into the codex history.
func (e *DirectEngine) drainSteeredMessagesCodex() {
	e.steerMu.Lock()
	msgs := e.steered
	e.steered = nil
	e.steerMu.Unlock()

	for _, msg := range msgs {
		e.codexHistory = append(e.codexHistory, codexHistoryEntry{
			Role:    "user",
			Content: msg,
		})
	}
}

// truncate shortens a string to maxLen, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
