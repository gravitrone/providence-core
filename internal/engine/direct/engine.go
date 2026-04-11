package direct

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/google/uuid"
	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/gravitrone/providence-core/internal/engine/direct/tools"
)

func init() {
	engine.RegisterFactory(engine.EngineTypeDirect, func(cfg engine.EngineConfig) (engine.Engine, error) {
		return NewDirectEngine(cfg)
	})
}

// DirectEngine implements engine.Engine using the Anthropic Messages API directly.
type DirectEngine struct {
	client      anthropic.Client
	model       string
	system      string
	events      chan engine.ParsedEvent
	history     *ConversationHistory
	registry    *tools.Registry
	permissions *PermissionHandler
	workDir     string
	sessionID   string

	mu     sync.Mutex
	status engine.SessionStatus
	cancel context.CancelFunc
	ctx    context.Context

	// Mid-turn steering: extra user messages injected between turns.
	steered []string
	steerMu sync.Mutex

	// Pending images to include with the next user message.
	pendingImages []ImageData

	// Codex mode: use OpenAI Codex API instead of Anthropic.
	codexMode    bool
	codexHistory []codexHistoryEntry

	// OpenRouter mode: use OpenRouter OpenAI-compatible API.
	openrouterMode    bool
	openrouterAPIKey  string
	openrouterHistory []openrouterHistoryEntry
}

// NewDirectEngine creates a DirectEngine from the given config.
func NewDirectEngine(cfg engine.EngineConfig) (*DirectEngine, error) {
	isCodex := cfg.Provider == "openai"
	isOpenRouter := cfg.Provider == "openrouter"

	// Resolve OpenRouter API key: explicit cfg field > env var.
	openrouterKey := cfg.OpenRouterAPIKey
	if isOpenRouter && openrouterKey == "" {
		openrouterKey = os.Getenv("OPENROUTER_API_KEY")
	}
	if isOpenRouter && openrouterKey == "" {
		return nil, fmt.Errorf("openrouter provider requires OPENROUTER_API_KEY env var or OpenRouterAPIKey config")
	}

	var client anthropic.Client
	if !isCodex && !isOpenRouter {
		opts := []option.RequestOption{}
		if cfg.APIKey != "" {
			opts = append(opts, option.WithAPIKey(cfg.APIKey))
		}
		client = anthropic.NewClient(opts...)
	}

	model := cfg.Model
	if model == "" {
		switch {
		case isCodex:
			model = "gpt-5.4"
		case isOpenRouter:
			model = "anthropic/claude-sonnet-4-5"
		default:
			model = string(anthropic.ModelClaudeSonnet4_20250514)
		}
	}

	// Build tool registry with all built-in tools.
	fs := tools.NewFileState()
	registry := tools.NewRegistry(
		tools.NewReadTool(fs),
		tools.NewWriteTool(fs),
		tools.NewEditTool(fs),
		&tools.BashTool{},
		&tools.GlobTool{},
		&tools.GrepTool{},
		&tools.WebFetchTool{},
		&tools.WebSearchTool{},
	)

	ctx, cancel := context.WithCancel(context.Background())

	return &DirectEngine{
		client:           client,
		model:            model,
		system:           cfg.SystemPrompt,
		events:           make(chan engine.ParsedEvent, 64),
		history:          NewConversationHistory(),
		registry:         registry,
		permissions:      NewPermissionHandler(),
		workDir:          cfg.WorkDir,
		sessionID:        uuid.New().String(),
		status:           engine.StatusIdle,
		ctx:              ctx,
		cancel:           cancel,
		codexMode:        isCodex,
		openrouterMode:   isOpenRouter,
		openrouterAPIKey: openrouterKey,
	}, nil
}

// SetRegistry replaces the tool registry (for use before first Send).
func (e *DirectEngine) SetRegistry(r *tools.Registry) {
	e.registry = r
}

// SetPendingImages stores images to include with the next user message.
// Images are consumed on the next Send call.
func (e *DirectEngine) SetPendingImages(images []ImageData) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pendingImages = images
}

// Send sends a user message to the AI and starts the agent loop.
func (e *DirectEngine) Send(text string) error {
	e.mu.Lock()
	if e.status == engine.StatusRunning {
		e.mu.Unlock()
		return fmt.Errorf("engine is already running")
	}
	e.status = engine.StatusRunning
	// Reset context for this turn.
	e.ctx, e.cancel = context.WithCancel(context.Background())
	// Consume pending images.
	images := e.pendingImages
	e.pendingImages = nil
	e.mu.Unlock()

	if e.codexMode {
		e.codexHistory = append(e.codexHistory, codexHistoryEntry{
			Role:    "user",
			Content: text,
		})
		go e.codexAgentLoop(e.ctx)
	} else if e.openrouterMode {
		e.openrouterHistory = append(e.openrouterHistory, openrouterHistoryEntry{
			Role:    "user",
			Content: text,
		})
		go e.openrouterAgentLoop(e.ctx)
	} else {
		if len(images) > 0 {
			e.history.AddUserWithImages(text, images)
		} else {
			e.history.AddUser(text)
		}
		go e.agentLoop(e.ctx)
	}
	return nil
}

// Events returns the read-only event channel.
func (e *DirectEngine) Events() <-chan engine.ParsedEvent {
	return e.events
}

// RespondPermission resolves a pending permission request.
func (e *DirectEngine) RespondPermission(questionID, optionID string) error {
	approved := optionID == "allow"
	e.permissions.Respond(questionID, approved)
	return nil
}

// Interrupt aborts the current API call and tool executions.
// The engine remains usable for the next Send call.
func (e *DirectEngine) Interrupt() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cancel != nil {
		e.cancel()
	}
}

// Cancel aborts the current operation and closes the events channel.
func (e *DirectEngine) Cancel() {
	e.Interrupt()
	// Give the agent loop a moment to finish, then close events.
	// The agent loop's defer will emit the result event before we close.
}

// Close cleanly shuts down the engine and closes the events channel.
func (e *DirectEngine) Close() {
	e.Interrupt()
}

// Status returns the current engine status (thread-safe).
func (e *DirectEngine) Status() engine.SessionStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.status
}

// RestoreHistory replaces the engine's conversation history with the given
// restored messages. User and assistant turns are restored directly, while
// persisted tool rows are synthesized into assistant text so resumed sessions
// retain prior tool outcomes without replaying provider-specific tool blocks.
func (e *DirectEngine) RestoreHistory(messages []engine.RestoredMessage) error {
	e.history = NewConversationHistory()
	// Also reset codex/openrouter histories so all modes stay consistent.
	e.codexHistory = nil
	e.openrouterHistory = nil
	for _, m := range messages {
		switch m.Role {
		case "user":
			e.restoreUserText(m.Content)
		case "assistant":
			e.restoreAssistantText(m.Content)
		case "tool":
			e.restoreAssistantText(formatRestoredToolMessage(m))
		default:
			// Skip system/permission and other non-conversation roles.
		}
	}
	return nil
}

// restoreUserText adds a user text message to all active history modes.
func (e *DirectEngine) restoreUserText(text string) {
	if text == "" {
		return
	}

	e.history.AddUser(text)
	if e.codexMode {
		e.codexHistory = append(e.codexHistory, codexHistoryEntry{
			Role:    "user",
			Content: text,
		})
	}
	if e.openrouterMode {
		e.openrouterHistory = append(e.openrouterHistory, openrouterHistoryEntry{
			Role:    "user",
			Content: text,
		})
	}
}

// restoreAssistantText adds an assistant text message to all active history modes.
func (e *DirectEngine) restoreAssistantText(text string) {
	if text == "" {
		return
	}

	e.history.AddAssistantText(text)
	if e.codexMode {
		e.codexHistory = append(e.codexHistory, codexHistoryEntry{
			Role:    "assistant",
			Content: text,
		})
	}
	if e.openrouterMode {
		e.openrouterHistory = append(e.openrouterHistory, openrouterHistoryEntry{
			Role:    "assistant",
			Content: text,
		})
	}
}

// formatRestoredToolMessage formats a persisted tool message for display in restored history.
func formatRestoredToolMessage(message engine.RestoredMessage) string {
	toolName := strings.TrimSpace(message.ToolName)
	if toolName == "" {
		toolName = "Tool"
	}
	if message.ToolInput != "" {
		toolName = fmt.Sprintf("%s(%s)", toolName, message.ToolInput)
	}
	return fmt.Sprintf("[Tool: %s -> %s]", toolName, message.Content)
}

// agentLoop is the core loop: call API, stream response, execute tools, repeat.
func (e *DirectEngine) agentLoop(ctx context.Context) {
	defer e.emitResult()
	e.emitSystemInit()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Build tool params.
		toolParams := e.toolParams()

		// Call Messages API with streaming.
		stream := e.client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
			Model:     anthropic.Model(e.model),
			MaxTokens: 16384,
			System:    e.systemBlocks(),
			Messages:  e.history.Messages(),
			Tools:     toolParams,
		})

		// Consume stream, emit text deltas, accumulate message.
		var accumulated anthropic.Message
		for stream.Next() {
			event := stream.Current()
			_ = accumulated.Accumulate(event)
			if pe := translateStreamEvent(event); pe != nil {
				e.events <- *pe
			}
		}
		if err := stream.Err(); err != nil {
			e.emitError(err)
			return
		}

		// Add assistant message to history.
		e.history.AddAssistant(accumulated)

		// Emit full assistant event.
		e.emitAssistant(accumulated)

		// If no tool use, we're done.
		if accumulated.StopReason != anthropic.StopReasonToolUse {
			return
		}

		// Execute tools.
		toolCalls := extractToolCalls(accumulated)
		queue := NewStreamingToolQueue(e.registry)
		for _, tc := range toolCalls {
			tool, ok := e.registry.Get(tc.Name)
			if !ok {
				// Unknown tool - add error result directly.
				queue.mu.Lock()
				queue.results = append(queue.results, ToolCallResult{
					ToolCall: tc,
					Result:   tools.ToolResult{Content: "unknown tool: " + tc.Name, IsError: true},
				})
				queue.mu.Unlock()
				continue
			}
			if e.permissions.NeedsPermission(tool) {
				approved := e.permissions.RequestPermission(tc.ID, e.events, tc.Name, tc.Input)
				if !approved {
					queue.mu.Lock()
					queue.results = append(queue.results, ToolCallResult{
						ToolCall: tc,
						Result:   tools.ToolResult{Content: "permission denied", IsError: true},
					})
					queue.mu.Unlock()
					continue
				}
			}
			queue.Submit(ctx, tc)
		}
		queue.Wait()

		// Collect results, emit tool_result events, add to history.
		var resultBlocks []anthropic.ContentBlockParamUnion
		for _, r := range queue.Results() {
			e.events <- engine.ParsedEvent{
				Type: "tool_result",
				Data: &engine.ToolResultEvent{
					Type:       "tool_result",
					ToolCallID: r.ID,
					ToolName:   r.Name,
					Output:     r.Result.Content,
					IsError:    r.Result.IsError,
				},
			}
			resultBlocks = append(resultBlocks, anthropic.NewToolResultBlock(
				r.ID, r.Result.Content, r.Result.IsError,
			))
		}
		e.history.AddToolResults(resultBlocks)

		// Check for steered messages.
		e.drainSteeredMessages()
	}
}

// emitSystemInit sends the initial system event.
func (e *DirectEngine) emitSystemInit() {
	toolNames := make([]string, 0)
	for _, t := range e.registry.All() {
		toolNames = append(toolNames, t.Name())
	}
	e.events <- engine.ParsedEvent{
		Type: "system",
		Data: &engine.SystemInitEvent{
			Type:      "system",
			Subtype:   "init",
			SessionID: e.sessionID,
			Tools:     toolNames,
			Model:     e.model,
		},
	}
}

// emitAssistant converts a completed Message into an AssistantEvent.
func (e *DirectEngine) emitAssistant(msg anthropic.Message) {
	parts := make([]engine.ContentPart, 0, len(msg.Content))
	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			parts = append(parts, engine.ContentPart{
				Type: "text",
				Text: block.Text,
			})
		case "tool_use":
			var input any
			if len(block.Input) > 0 {
				_ = json.Unmarshal(block.Input, &input)
			}
			parts = append(parts, engine.ContentPart{
				Type:  "tool_use",
				ID:    block.ID,
				Name:  block.Name,
				Input: input,
			})
		}
	}
	e.events <- engine.ParsedEvent{
		Type: "assistant",
		Data: &engine.AssistantEvent{
			Type:    "assistant",
			Message: engine.AssistantMsg{Content: parts},
		},
	}
}

// emitResult sends the final result event.
func (e *DirectEngine) emitResult() {
	e.mu.Lock()
	wasRunning := e.status == engine.StatusRunning
	if wasRunning {
		e.status = engine.StatusCompleted
	}
	e.mu.Unlock()

	e.events <- engine.ParsedEvent{
		Type: "result",
		Data: &engine.ResultEvent{
			Type:      "result",
			Subtype:   "success",
			SessionID: e.sessionID,
		},
	}
}

// emitError sends a result event with IsError=true.
func (e *DirectEngine) emitError(err error) {
	e.mu.Lock()
	e.status = engine.StatusFailed
	e.mu.Unlock()

	e.events <- engine.ParsedEvent{
		Type: "result",
		Data: &engine.ResultEvent{
			Type:      "result",
			Subtype:   "error",
			Result:    err.Error(),
			SessionID: e.sessionID,
			IsError:   true,
		},
	}
}

// toolParams converts the registry into Anthropic SDK tool params.
func (e *DirectEngine) toolParams() []anthropic.ToolUnionParam {
	allTools := e.registry.All()
	if len(allTools) == 0 {
		return nil
	}
	params := make([]anthropic.ToolUnionParam, 0, len(allTools))
	for _, t := range allTools {
		schema := t.InputSchema()
		tp := anthropic.ToolParam{
			Name:        t.Name(),
			Description: anthropic.String(t.Description()),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: schema["properties"],
			},
		}
		if req, ok := schema["required"].([]string); ok {
			tp.InputSchema.Required = req
		}
		params = append(params, anthropic.ToolUnionParam{OfTool: &tp})
	}
	return params
}

// systemBlocks returns the system prompt as TextBlockParam slice.
func (e *DirectEngine) systemBlocks() []anthropic.TextBlockParam {
	if e.system == "" {
		return nil
	}

	systemPrompt := e.system
	blocks := engine.BuildSystemBlocks(nil)
	if systemPrompt != engine.BuildSystemPrompt(nil) {
		blocks = []engine.SystemBlock{{
			Text:      systemPrompt,
			Cacheable: true,
		}}
	}

	params := make([]anthropic.TextBlockParam, 0, len(blocks))
	lastCacheable := -1
	for _, block := range blocks {
		if block.Text == "" {
			continue
		}
		params = append(params, anthropic.TextBlockParam{Text: block.Text})
		if block.Cacheable {
			lastCacheable = len(params) - 1
		}
	}
	if lastCacheable >= 0 {
		params[lastCacheable].CacheControl = anthropic.NewCacheControlEphemeralParam()
	}
	return params
}

// drainSteeredMessages checks for mid-turn steering messages and adds them as user messages.
func (e *DirectEngine) drainSteeredMessages() {
	e.steerMu.Lock()
	msgs := e.steered
	e.steered = nil
	e.steerMu.Unlock()

	for _, msg := range msgs {
		e.history.AddUser(msg)
	}
}

// Steer adds a mid-turn user message that will be injected before the next API call.
func (e *DirectEngine) Steer(text string) {
	e.steerMu.Lock()
	defer e.steerMu.Unlock()
	e.steered = append(e.steered, text)
}
