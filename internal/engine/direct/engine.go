package direct

import (
	"context"
	"encoding/json"
	"fmt"
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
}

// NewDirectEngine creates a DirectEngine from the given config.
func NewDirectEngine(cfg engine.EngineConfig) (*DirectEngine, error) {
	opts := []option.RequestOption{}
	if cfg.APIKey != "" {
		opts = append(opts, option.WithAPIKey(cfg.APIKey))
	}
	client := anthropic.NewClient(opts...)

	model := cfg.Model
	if model == "" {
		model = string(anthropic.ModelClaudeSonnet4_20250514)
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
		client:      client,
		model:       model,
		system:      cfg.SystemPrompt,
		events:      make(chan engine.ParsedEvent, 64),
		history:     NewConversationHistory(),
		registry:    registry,
		permissions: NewPermissionHandler(),
		workDir:     cfg.WorkDir,
		sessionID:   uuid.New().String(),
		status:      engine.StatusIdle,
		ctx:         ctx,
		cancel:      cancel,
	}, nil
}

// SetRegistry replaces the tool registry (for use before first Send).
func (e *DirectEngine) SetRegistry(r *tools.Registry) {
	e.registry = r
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
	e.mu.Unlock()

	e.history.AddUser(text)
	go e.agentLoop(e.ctx)
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

		// Collect results, add to history.
		var resultBlocks []anthropic.ContentBlockParamUnion
		for _, r := range queue.Results() {
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
	return []anthropic.TextBlockParam{{Text: e.system}}
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
