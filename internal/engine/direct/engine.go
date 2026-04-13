package direct

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/google/uuid"
	"github.com/gravitrone/providence-core/internal/bridge/macos"
	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/gravitrone/providence-core/internal/engine/compact"
	"github.com/gravitrone/providence-core/internal/engine/direct/tools"
	"github.com/gravitrone/providence-core/internal/engine/session"
	"github.com/gravitrone/providence-core/internal/engine/subagent"
)

// MaxOutputTokensRecoveryLimit is the maximum number of times the engine will
// automatically retry after hitting max_tokens before surfacing the error.
const MaxOutputTokensRecoveryLimit = 3

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
	compactor   *compact.Orchestrator
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

	// Provider identifier ("anthropic", "openai", "openrouter").
	provider string

	// maxOutputRecoveryCount tracks how many times we've auto-recovered from
	// max_tokens in the current user turn. Reset at the start of each Send.
	maxOutputRecoveryCount int

	// fallbackActive is true when we've already fallen back to a fast model
	// for this turn, preventing infinite fallback loops.
	fallbackActive bool

	// Codex mode: use OpenAI Codex API instead of Anthropic.
	codexMode    bool
	codexHistory []codexHistoryEntry

	// OpenRouter mode: use OpenRouter OpenAI-compatible API.
	openrouterMode    bool
	openrouterAPIKey  string
	openrouterHistory []openrouterHistoryEntry

	// Subagent support.
	subagentRunner *subagent.Runner
	apiKey         string // stored for sub-engine creation

	// Session event bus for background agents and plugin subscribers.
	sessionBus *session.Bus
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
	planState := tools.NewPlanModeState(nil) // event wiring comes in Phase 5
	coreTools := []tools.Tool{
		tools.NewReadTool(fs),
		tools.NewWriteTool(fs),
		tools.NewEditTool(fs),
		&tools.BashTool{},
		&tools.GlobTool{},
		&tools.GrepTool{},
		&tools.WebFetchTool{},
		&tools.WebSearchTool{},
		tools.NewTodoWriteTool(),
		tools.NewAskUserQuestionTool(nil), // event wiring comes in Phase 5
		tools.NewEnterPlanModeTool(planState),
		tools.NewExitPlanModeTool(planState),
	}

	// Register computer use tools on macOS only.
	if runtime.GOOS == "darwin" {
		bridge := macos.New()
		coreTools = append(coreTools,
			tools.NewScreenshotTool(bridge),
			tools.NewDesktopClickTool(bridge),
			tools.NewDesktopTypeTool(bridge),
			tools.NewDesktopAppsTool(bridge),
			tools.NewClipboardTool(bridge),
		)
	}

	registry := tools.NewRegistry(coreTools...)

	ctx, cancel := context.WithCancel(context.Background())
	history := NewConversationHistory()

	providerName := engine.ProviderAnthropic
	if isCodex {
		providerName = engine.ProviderOpenAI
	} else if isOpenRouter {
		providerName = engine.ProviderOpenRouter
	}

	e := &DirectEngine{
		client:           client,
		model:            model,
		system:           cfg.SystemPrompt,
		events:           make(chan engine.ParsedEvent, 64),
		history:          history,
		registry:         registry,
		permissions:      NewPermissionHandler(),
		workDir:          cfg.WorkDir,
		sessionID:        uuid.New().String(),
		status:           engine.StatusIdle,
		ctx:              ctx,
		cancel:           cancel,
		provider:         providerName,
		codexMode:        isCodex,
		openrouterMode:   isOpenRouter,
		openrouterAPIKey: openrouterKey,
		subagentRunner:   subagent.NewRunner(),
		apiKey:           cfg.APIKey,
		sessionBus:       session.NewBus(),
	}

	// Register subagent TaskTool now that the engine exists for the executor.
	taskTool := tools.NewTaskTool(e.subagentRunner, e.subagentExecutor)
	registry.Register(taskTool)

	var provider compact.Provider
	switch {
	case isCodex:
		provider = newCodexCompactProvider(&e.codexHistory, e.model)
	case isOpenRouter:
		provider = newOpenRouterCompactProvider(&e.openrouterHistory, e.openrouterAPIKey, e.model)
	default:
		provider = newAnthropicCompactProvider(e.history, e.client, e.model)
	}

	e.compactor = compact.New(provider, func(phase compact.Phase, err error) {
		if phase == compact.PhaseRunning {
			e.sessionBus.Publish(session.Event{Type: session.EventCompaction, Data: nil})
		}
		event := engine.ParsedEvent{
			Type: "compaction",
			Data: &engine.CompactionEvent{
				Type:  "compaction",
				Phase: string(phase),
				Err:   err,
			},
		}

		compactionEvent := event.Data.(*engine.CompactionEvent)
		switch phase {
		case compact.PhaseRunning:
			compactionEvent.TokensBefore = provider.CurrentTokens()
		default:
			compactionEvent.TokensAfter = provider.CurrentTokens()
		}

		select {
		case e.events <- event:
		default:
		}
	})

	return e, nil
}

// SubagentRunner returns the engine's subagent runner for external polling
// (e.g. the TUI checking for completed background tasks).
func (e *DirectEngine) SubagentRunner() *subagent.Runner {
	return e.subagentRunner
}

// SubagentExecutor returns the executor callback for spawning subagents
// from outside the engine (e.g. /fork command in the TUI).
func (e *DirectEngine) SubagentExecutor() subagent.Executor {
	return e.subagentExecutor
}

// SessionBus returns the engine's session event bus.
func (e *DirectEngine) SessionBus() *session.Bus {
	return e.sessionBus
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
	e.maxOutputRecoveryCount = 0
	e.fallbackActive = false
	// Reset context for this turn.
	e.ctx, e.cancel = context.WithCancel(context.Background())
	e.mu.Unlock()

	if e.compactor != nil && (e.codexMode || e.openrouterMode) {
		if err := e.compactor.WaitForPending(e.ctx); err != nil {
			e.cancel()
			e.mu.Lock()
			e.status = engine.StatusIdle
			e.mu.Unlock()
			return err
		}
	}

	e.mu.Lock()
	// Consume pending images.
	images := e.pendingImages
	e.pendingImages = nil
	e.mu.Unlock()

	// Publish new message event to session bus.
	e.sessionBus.Publish(session.Event{Type: session.EventNewMessage, Data: text})

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

// TriggerCompact requests immediate context compaction when available.
func (e *DirectEngine) TriggerCompact(ctx context.Context) error {
	if e.compactor == nil {
		return fmt.Errorf("compaction not available for this engine mode")
	}
	if !e.compactor.TriggerNow(ctx) {
		return fmt.Errorf("compaction already running or not ready")
	}
	return nil
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

// subagentExecutor creates a child DirectEngine and runs a single-turn conversation,
// returning the assistant's text output. Used as the subagent.Executor callback.
func (e *DirectEngine) subagentExecutor(ctx context.Context, prompt string, agentType subagent.AgentType) (string, error) {
	systemPrompt := agentType.SystemPrompt + "\n\n" + subagent.AntiRecursionPrompt

	model := agentType.Model
	if model == "inherit" || model == "" {
		model = e.model
	}

	cfg := engine.EngineConfig{
		Type:             engine.EngineTypeDirect,
		SystemPrompt:     systemPrompt,
		Model:            model,
		APIKey:           e.apiKey,
		WorkDir:          e.workDir,
		Provider:         e.provider,
		OpenRouterAPIKey: e.openrouterAPIKey,
	}

	sub, err := NewDirectEngine(cfg)
	if err != nil {
		return "", fmt.Errorf("create sub-engine: %w", err)
	}
	defer sub.Close()

	// Apply agent's permission mode if set.
	if agentType.PermissionMode != "" && agentType.PermissionMode != "inherit" {
		switch agentType.PermissionMode {
		case "plan":
			// In plan mode, restrict to read-only tools by removing write/execute tools.
			sub.permissions.SetMode("plan")
		case "auto":
			// Auto-approve all tool permissions.
			sub.permissions.SetMode("auto")
		case "deny":
			// Deny all tool permissions.
			sub.permissions.SetMode("deny")
		}
	}

	if err := sub.Send(prompt); err != nil {
		return "", fmt.Errorf("sub-engine send: %w", err)
	}

	// maxTurns enforcement: cap the number of agentic turns.
	turnCount := 0
	maxTurns := agentType.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 100 // safety cap
	}

	var result strings.Builder
	for ev := range sub.Events() {
		if ctx.Err() != nil {
			sub.Interrupt()
			return "", ctx.Err()
		}
		switch ev.Type {
		case "assistant":
			if ae, ok := ev.Data.(*engine.AssistantEvent); ok {
				for _, part := range ae.Message.Content {
					if part.Type == "text" {
						result.WriteString(part.Text)
					}
				}
			}
		case "result":
			if re, ok := ev.Data.(*engine.ResultEvent); ok && re.IsError {
				return "", fmt.Errorf("sub-engine error: %s", re.Result)
			}
			turnCount++
			if turnCount >= maxTurns {
				sub.Interrupt()
				result.WriteString("\n[max turns reached]")
				return result.String(), nil
			}
			return result.String(), nil
		}
	}
	return result.String(), nil
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

		if e.compactor != nil {
			if err := e.compactor.WaitForPending(ctx); err != nil {
				e.emitError(err)
				return
			}
		}

		// Build tool params.
		toolParams := e.toolParams()

		// Call Messages API with streaming + 429 retry + 413 reactive compact.
		apiParams := anthropic.MessageNewParams{
			Model:     anthropic.Model(e.model),
			MaxTokens: 16384,
			System:    e.systemBlocks(),
			Messages:  e.history.Messages(),
			Tools:     toolParams,
		}

		accumulated, streamErr := e.streamWithRetry(ctx, apiParams)

		// 413 prompt-too-long reactive compaction.
		if streamErr != nil {
			errStr := streamErr.Error()
			if (strings.Contains(errStr, "413") || strings.Contains(errStr, "prompt_too_long")) && e.compactor != nil {
				e.events <- engine.ParsedEvent{
					Type: "system_message",
					Data: &engine.SystemMessageEvent{
						Type:    "system_message",
						Content: "Prompt too long. Compacting context...",
					},
				}
				if compactErr := e.compactor.TriggerReactive(ctx); compactErr == nil {
					// Compaction succeeded, retry the turn.
					continue
				}
			}
		}

		if streamErr != nil {
			if isOverloadError(streamErr) && !e.fallbackActive {
				fallback := engine.FastForProvider(e.provider)
				if fallback != "" && fallback != e.model {
					// Tombstone any partial streaming content so UI can clear it.
					e.events <- engine.ParsedEvent{
						Type: "tombstone",
						Data: &engine.TombstoneEvent{Type: "tombstone", MessageIndex: -1},
					}

					previousModel := e.model
					e.model = fallback
					e.fallbackActive = true

					e.events <- engine.ParsedEvent{
						Type: "system_message",
						Data: &engine.SystemMessageEvent{
							Type:    "system_message",
							Content: fmt.Sprintf("Model overloaded. Switched from %s to %s.", previousModel, fallback),
						},
					}
					continue
				}
			}
			e.emitError(streamErr)
			return
		}
		e.emitUsageUpdate(
			int(accumulated.Usage.InputTokens),
			int(accumulated.Usage.OutputTokens),
			int(accumulated.Usage.CacheReadInputTokens),
			int(accumulated.Usage.CacheCreationInputTokens),
		)

		// Add assistant message to history.
		e.history.AddAssistant(accumulated)
		if e.compactor != nil {
			e.compactor.TriggerIfNeeded(ctx)
		}

		// Emit full assistant event.
		e.emitAssistant(accumulated)

		// Max output tokens recovery: when the model hits max_tokens, inject a
		// recovery prompt and retry up to MaxOutputTokensRecoveryLimit times.
		if accumulated.StopReason == anthropic.StopReasonMaxTokens {
			if e.maxOutputRecoveryCount < MaxOutputTokensRecoveryLimit {
				e.maxOutputRecoveryCount++
				e.history.AddUser("Output token limit hit. Resume directly - no apology, no recap. Pick up mid-thought if that is where the cut happened. Break remaining work into smaller pieces.")
				e.events <- engine.ParsedEvent{
					Type: "system_message",
					Data: &engine.SystemMessageEvent{
						Type:    "system_message",
						Content: fmt.Sprintf("Max output tokens hit (%d/%d), auto-resuming.", e.maxOutputRecoveryCount, MaxOutputTokensRecoveryLimit),
					},
				}
				continue
			}
			e.emitError(fmt.Errorf("max output tokens hit %d times, giving up", MaxOutputTokensRecoveryLimit))
			return
		}

		// If no tool use, we're done.
		if accumulated.StopReason != anthropic.StopReasonToolUse {
			e.history.CompressLongToolResults(2000)
			return
		}

		// Execute tools.
		toolCalls := extractToolCalls(accumulated)
		queue := NewStreamingToolQueue(e.registry)
		for _, tc := range toolCalls {
			e.sessionBus.Publish(session.Event{Type: session.EventToolCallStart, Data: tc.Name})
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
			e.sessionBus.Publish(session.Event{Type: session.EventToolCallResult, Data: r.Result.Content})
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
		e.history.CompressLongToolResults(2000)

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

func (e *DirectEngine) emitUsageUpdate(inputTokens, outputTokens, cacheReadTokens, cacheCreateTokens int) {
	if inputTokens == 0 && outputTokens == 0 && cacheReadTokens == 0 && cacheCreateTokens == 0 {
		return
	}
	if e.history != nil {
		e.history.SetReportedTokens(inputTokens, outputTokens)
	}
	e.events <- engine.ParsedEvent{
		Type: "usage_update",
		Data: &engine.UsageUpdateEvent{
			Type:              "usage_update",
			InputTokens:       inputTokens,
			OutputTokens:      outputTokens,
			TotalTokens:       inputTokens + outputTokens,
			CacheReadTokens:   cacheReadTokens,
			CacheCreateTokens: cacheCreateTokens,
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

	if wasRunning && e.compactor != nil && (e.codexMode || e.openrouterMode) {
		e.compactor.TriggerIfNeeded(context.Background())
	}

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

// streamWithRetry calls the Messages API with streaming, retrying up to 3 times
// on 429 rate limit errors with exponential backoff.
func (e *DirectEngine) streamWithRetry(ctx context.Context, params anthropic.MessageNewParams) (anthropic.Message, error) {
	const maxRetries = 3

	for attempt := 0; attempt < maxRetries; attempt++ {
		stream := e.client.Messages.NewStreaming(ctx, params)

		var accumulated anthropic.Message
		for stream.Next() {
			event := stream.Current()
			_ = accumulated.Accumulate(event)
			if pe := translateStreamEvent(event); pe != nil {
				e.events <- *pe
			}
		}
		if err := stream.Err(); err != nil {
			errStr := err.Error()
			if strings.Contains(errStr, "429") || strings.Contains(errStr, "rate_limit") {
				if attempt < maxRetries-1 {
					delay := engine.Backoff(attempt)
					e.events <- engine.ParsedEvent{
						Type: "system_message",
						Data: &engine.SystemMessageEvent{
							Type:    "system_message",
							Content: fmt.Sprintf("Rate limited. Retrying in %s...", delay),
						},
					}
					time.Sleep(delay)
					continue
				}
			}
			return accumulated, err
		}
		return accumulated, nil
	}
	// Unreachable, but satisfies the compiler.
	return anthropic.Message{}, fmt.Errorf("stream retry exhausted")
}

// isOverloadError returns true if the error indicates a model overload (HTTP 529
// or "overloaded_error" in the response body).
func isOverloadError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "529") || strings.Contains(msg, "overloaded")
}
