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
	Model         string                   `json:"model"`
	Messages      []openrouterMessage      `json:"messages"`
	Stream        bool                     `json:"stream"`
	StreamOptions *openrouterStreamOptions `json:"stream_options,omitempty"`
	Tools         []openrouterTool         `json:"tools,omitempty"`
}

// openrouterMessage is a single message in the OpenAI chat completions format.
// Tool calls use the `tool_calls` field on assistant messages, and tool results
// use role "tool" with `tool_call_id`.
type openrouterMessage struct {
	Role       string                  `json:"role"`
	Content    any                     `json:"content,omitempty"`
	Name       string                  `json:"name,omitempty"`
	ToolCallID string                  `json:"tool_call_id,omitempty"`
	ToolCalls  []openrouterToolCallMsg `json:"tool_calls,omitempty"`
}

type openrouterSystemContentBlock struct {
	Type         string                  `json:"type"`
	Text         string                  `json:"text"`
	CacheControl *openrouterCacheControl `json:"cache_control,omitempty"`
}

type openrouterCacheControl struct {
	Type string `json:"type"`
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

type openrouterStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
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

// buildOpenRouterTools converts the tool list to OpenAI function-calling format.
func buildOpenRouterTools(allTools []tools.Tool) []openrouterTool {
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
	return buildOpenRouterMessagesWithBlocks("", nil, system, history)
}

func buildOpenRouterMessagesForModel(model string, system string, history []openrouterHistoryEntry) []openrouterMessage {
	return buildOpenRouterMessagesWithBlocks(model, nil, system, history)
}

func buildOpenRouterMessagesWithBlocks(model string, blocks []engine.SystemBlock, system string, history []openrouterHistoryEntry) []openrouterMessage {
	msgs := make([]openrouterMessage, 0, len(history)+1)
	if system != "" || len(blocks) > 0 {
		msgs = append(msgs, openrouterMessage{
			Role:    "system",
			Content: buildOpenRouterSystemContentFromBlocks(model, blocks, system),
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

// compressOpenRouterToolResults replaces oversized older tool outputs with a stub.
func compressOpenRouterToolResults(msgs []openrouterMessage, minLen int) int {
	if len(msgs) <= 4 {
		return 0
	}

	compressed := 0
	for i := 0; i < len(msgs)-4; i++ {
		if msgs[i].Role != "tool" {
			continue
		}

		content, ok := msgs[i].Content.(string)
		if !ok {
			continue
		}
		if len(content) <= minLen {
			continue
		}

		msgs[i].Content = fmt.Sprintf(
			"[compressed: %d chars from tool_call_id=%s]",
			len(content),
			msgs[i].ToolCallID,
		)
		compressed++
	}

	return compressed
}

// buildOpenRouterSystemContentFromBlocks converts structured blocks to OpenRouter
// content array with cache control on the last cacheable block.
// For Anthropic models via OpenRouter, returns structured content blocks.
// For other models, returns the flat string.
func buildOpenRouterSystemContentFromBlocks(model string, blocks []engine.SystemBlock, flat string) any {
	if !strings.HasPrefix(model, "anthropic/") {
		return flat
	}

	if len(blocks) == 0 {
		if flat == "" {
			return flat
		}
		blocks = []engine.SystemBlock{{Text: flat, Cacheable: true}}
	}

	content := make([]openrouterSystemContentBlock, 0, len(blocks))
	lastCacheable := -1
	for _, block := range blocks {
		if block.Text == "" {
			continue
		}
		content = append(content, openrouterSystemContentBlock{
			Type: "text",
			Text: block.Text,
		})
		if block.Cacheable {
			lastCacheable = len(content) - 1
		}
	}
	if len(content) == 0 {
		return flat
	}
	if lastCacheable >= 0 {
		content[lastCacheable].CacheControl = &openrouterCacheControl{Type: "ephemeral"}
	}
	return content
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
			Model:         e.model,
			Messages:      buildOpenRouterMessagesWithBlocks(e.model, e.blocks, e.system, e.openrouterHistory),
			Stream:        true,
			StreamOptions: &openrouterStreamOptions{IncludeUsage: true},
			Tools:         buildOpenRouterTools(e.filteredTools()),
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

		resp, err := providerHTTPClient.Do(req)
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

		// Append assistant turn: single message with text + tool_calls per OpenAI contract.
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

		if len(toolCalls) == 0 {
			openrouterMsgs := buildOpenRouterMessages("", e.openrouterHistory)
			if compressOpenRouterToolResults(openrouterMsgs, 2000) > 0 {
				for i := range e.openrouterHistory {
					content, ok := openrouterMsgs[i].Content.(string)
					if ok && openrouterMsgs[i].Role == "tool" {
						e.openrouterHistory[i].Content = content
					}
				}
			}
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
				e.openrouterHistory = append(e.openrouterHistory, openrouterHistoryEntry{
					Role:    "tool",
					Content: "unknown tool: " + tc.Name,
					CallID:  tc.ID,
				})
				continue
			}

			if e.permissions.NeedsPermission(tool, input) {
				approved, err := e.permissions.RequestPermission(ctx, tc.ID, e.events, tc.Name, input)
				if err != nil {
					return
				}
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
		openrouterMsgs := buildOpenRouterMessages("", e.openrouterHistory)
		if compressOpenRouterToolResults(openrouterMsgs, 2000) > 0 {
			for i := range e.openrouterHistory {
				content, ok := openrouterMsgs[i].Content.(string)
				if ok && openrouterMsgs[i].Role == "tool" {
					e.openrouterHistory[i].Content = content
				}
			}
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
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			// Some providers send heartbeat comments or malformed lines;
			// skip rather than aborting the whole stream.
			continue
		}
		if len(chunk.Choices) == 0 && chunk.Usage != nil {
			e.emitUsageUpdate(chunk.Usage.PromptTokens, chunk.Usage.CompletionTokens, 0, 0)
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
