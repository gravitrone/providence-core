package direct

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	"github.com/gravitrone/providence-core/internal/engine/filewatch"
	"github.com/gravitrone/providence-core/internal/engine/hooks"
	"github.com/gravitrone/providence-core/internal/engine/mcp"
	"github.com/gravitrone/providence-core/internal/engine/session"
	"github.com/gravitrone/providence-core/internal/engine/subagent"
)

// MaxOutputTokensRecoveryLimit is the maximum number of times the engine will
// automatically retry after hitting max_tokens before surfacing the error.
const MaxOutputTokensRecoveryLimit = 3

// DefaultMaxOutputTokens is the standard output token limit per API call.
const DefaultMaxOutputTokens = 16384

// EscalatedMaxOutputTokens is the higher limit tried before multi-turn recovery.
const EscalatedMaxOutputTokens = 64000

func init() {
	engine.RegisterFactory(engine.EngineTypeDirect, func(cfg engine.EngineConfig) (engine.Engine, error) {
		return NewDirectEngine(cfg)
	})
}

// DirectEngine implements engine.Engine using the Anthropic Messages API directly.
// It also implements engine.TodoProvider for per-session todo state.
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

	// outputTokensEscalated is true when we've already tried EscalatedMaxOutputTokens
	// for this turn. Prevents re-escalation on subsequent max_tokens hits.
	outputTokensEscalated bool

	// Structured system prompt blocks (preferred over e.system).
	blocks []engine.SystemBlock

	// Per-session TodoWrite tool instance.
	todoTool *tools.TodoWriteTool

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

	// Background agent support.
	bgAgentsEnabled  bool
	backgroundAgents map[string]subagent.BackgroundAgentType
	bgCancel         context.CancelFunc

	// store is optional; when set, session learnings are persisted on Close.
	store     storeIface
	startTime time.Time

	// Hooks runner for lifecycle event hooks.
	hooksRunner    *hooks.Runner
	sessionStarted bool // tracks whether SessionStart hook has fired

	// MCP server manager (nil when no MCP servers are configured).
	mcpManager *mcp.Manager

	// File watcher for config change detection.
	fileWatcher *filewatch.Watcher

	// Pre-executed tool results from streaming-overlapped execution.
	preExecResults map[string]tools.ToolResult
	preExecMu      sync.Mutex
	preExecWg      sync.WaitGroup
}

// NewDirectEngine creates a DirectEngine from the given config.
func NewDirectEngine(cfg engine.EngineConfig) (*DirectEngine, error) {
	isCodex := cfg.Provider == "openai"
	isOpenRouter := cfg.Provider == "openrouter"

	// Resolve OpenRouter API key: explicit config field takes precedence over env var.
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

	fs := tools.NewFileState()
	planState := tools.NewPlanModeState(nil)
	todoTool := tools.NewTodoWriteTool()
	coreTools := []tools.Tool{
		tools.NewReadTool(fs),
		tools.NewWriteTool(fs),
		tools.NewEditTool(fs),
		&tools.BashTool{},
		&tools.GlobTool{},
		&tools.GrepTool{},
		&tools.WebFetchTool{},
		&tools.WebSearchTool{},
		todoTool,
		tools.NewAskUserQuestionTool(nil),
		tools.NewEnterPlanModeTool(planState),
		tools.NewExitPlanModeTool(planState),
		tools.NewSkillTool(),
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

	// Register ToolSearch tool (needs registry reference, so added after init).
	toolSearchTool := tools.NewToolSearchTool(registry)
	registry.Register(toolSearchTool)

	ctx, cancel := context.WithCancel(context.Background())
	history := NewConversationHistory()

	providerName := engine.ProviderAnthropic
	if isCodex {
		providerName = engine.ProviderOpenAI
	} else if isOpenRouter {
		providerName = engine.ProviderOpenRouter
	}

	// Prefer structured blocks; fall back to wrapping the flat string as a single cacheable block.
	sysBlocks := cfg.SystemBlocks
	sysFlat := cfg.SystemPrompt
	if len(sysBlocks) == 0 && sysFlat != "" {
		sysBlocks = []engine.SystemBlock{{Text: sysFlat, Cacheable: true}}
	}
	if sysFlat == "" && len(sysBlocks) > 0 {
		sysFlat = engine.FlattenBlocks(sysBlocks)
	}

	hooksMap := make(map[string][]hooks.HookConfig)
	for event, entries := range cfg.HooksMap {
		for _, entry := range entries {
			hc := hooks.HookConfig{
				Command: entry.Command,
				URL:     entry.URL,
			}
			if entry.Timeout > 0 {
				hc.Timeout = time.Duration(entry.Timeout) * time.Millisecond
			}
			hooksMap[event] = append(hooksMap[event], hc)
		}
	}
	hooksRunner := hooks.NewRunner(hooksMap)

	e := &DirectEngine{
		client:           client,
		model:            model,
		system:           sysFlat,
		blocks:           sysBlocks,
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
		subagentRunner:   subagent.NewRunnerWithWorkDir(cfg.WorkDir),
		apiKey:           cfg.APIKey,
		sessionBus:       session.NewBus(),
		todoTool:         todoTool,
		startTime:        time.Now(),
		hooksRunner:      hooksRunner,
	}

	taskTool := tools.NewTaskTool(e.subagentRunner, e.subagentExecutor)
	registry.Register(taskTool)
	sendMsgTool := tools.NewSendMessageTool(e.subagentRunner)
	registry.Register(sendMsgTool)

	// MCP server support: load config, connect servers, register tools.
	mcpHomeDir, _ := os.UserHomeDir()
	mcpWorkDir := cfg.WorkDir
	if mcpWorkDir == "" {
		mcpWorkDir, _ = os.Getwd()
	}
	mcpConfigs, mcpErr := mcp.LoadMCPConfig(mcpWorkDir, mcpHomeDir)
	if mcpErr == nil && len(mcpConfigs) > 0 {
		mgr := mcp.NewManager()
		// ConnectAll logs failures per-server but continues with successful ones.
		_ = mgr.ConnectAll(mcpConfigs)
		if mgr.ServerCount() > 0 {
			mcp.RegisterMCPTools(mgr, registry)
			e.mcpManager = mgr

			// Inject MCP server instructions into system prompt blocks.
			if inst := mgr.GetInstructions(); inst != "" {
				e.blocks = append(e.blocks, engine.SystemBlock{
					Text:      inst,
					Cacheable: false,
				})
				e.system = engine.FlattenBlocks(e.blocks)
			}
		}
	}

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
			// Fire PreCompact hook.
			e.fireHookAsync(hooks.PreCompact, hooks.HookInput{
				ToolInput: provider.CurrentTokens(),
			})
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
			// Fire PostCompact hook.
			e.fireHookAsync(hooks.PostCompact, hooks.HookInput{
				ToolInput: map[string]int{
					"original_count": compactionEvent.TokensBefore,
					"new_count":      provider.CurrentTokens(),
				},
			})
		}

		select {
		case e.events <- event:
		default:
		}
	})

	// Start file watcher for config change detection.
	fw := filewatch.New(cfg.WorkDir, nil)
	fw.Start()
	e.fileWatcher = fw
	go e.drainFileWatchEvents()

	return e, nil
}

// drainFileWatchEvents reads file change events from the watcher and emits
// system messages so the user knows a config file changed.
func (e *DirectEngine) drainFileWatchEvents() {
	for evt := range e.fileWatcher.Events() {
		select {
		case e.events <- engine.ParsedEvent{
			Type: "system_message",
			Data: &engine.SystemMessageEvent{
				Type:    "system_message",
				Content: fmt.Sprintf("File changed: %s. Reload with /clear or continue.", evt.Path),
			},
		}:
		default:
		}
	}
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

// SubagentContextExecutor returns a ContextExecutor that restores conversation
// state before running the prompt. Used by /fork for full context inheritance.
func (e *DirectEngine) SubagentContextExecutor() subagent.ContextExecutor {
	return e.subagentContextExecutor
}

// SessionBus returns the engine's session event bus.
func (e *DirectEngine) SessionBus() *session.Bus {
	return e.sessionBus
}

// SetRegistry replaces the tool registry (for use before first Send).
func (e *DirectEngine) SetRegistry(r *tools.Registry) {
	e.registry = r
}

// EnableBackgroundAgents activates background agent processing. When enabled,
// the engine subscribes to SessionBus events and fires matching background
// agents automatically. Call before the first Send.
func (e *DirectEngine) EnableBackgroundAgents(agents map[string]subagent.BackgroundAgentType) {
	e.bgAgentsEnabled = true
	e.backgroundAgents = agents
	bgCtx, bgCancel := context.WithCancel(context.Background())
	e.bgCancel = bgCancel
	go e.runBackgroundAgents(bgCtx)
}

// runBackgroundAgents subscribes to SessionBus and fires matching background
// agents when their trigger events occur.
func (e *DirectEngine) runBackgroundAgents(ctx context.Context) {
	ch := e.sessionBus.Subscribe(32)
	defer e.sessionBus.Unsubscribe(ch)

	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			for _, bg := range e.backgroundAgents {
				if e.matchesTrigger(bg.TriggerOn, evt.Type) {
					go e.fireBackgroundAgent(ctx, bg, evt)
				}
			}
		}
	}
}

// matchesTrigger checks if a session event type matches a background agent's trigger.
func (e *DirectEngine) matchesTrigger(trigger, eventType string) bool {
	switch trigger {
	case "tool_use_turn":
		return eventType == session.EventToolCallResult
	case "every_turn":
		return eventType == session.EventNewMessage || eventType == session.EventToolCallResult
	case "on_demand":
		return false // only fired manually
	default:
		return false
	}
}

// fireBackgroundAgent spawns a background agent in response to a session event.
func (e *DirectEngine) fireBackgroundAgent(ctx context.Context, bg subagent.BackgroundAgentType, evt session.Event) {
	prompt := fmt.Sprintf("Session event: %s\nData: %v", evt.Type, evt.Data)
	input := subagent.TaskInput{
		Description:  bg.Name + " (background)",
		Prompt:       prompt,
		SubagentType: bg.Name,
		RunInBG:      true,
		Name:         bg.Name,
	}
	if _, err := e.subagentRunner.Spawn(ctx, input, bg.AgentType, e.subagentExecutor); err != nil {
		e.events <- engine.ParsedEvent{
			Type: "system_message",
			Data: &engine.SystemMessageEvent{
				Type:    "system_message",
				Content: fmt.Sprintf("Background agent %s spawn failed: %v", bg.Name, err),
			},
		}
	}
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
	e.outputTokensEscalated = false
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
	images := e.pendingImages
	e.pendingImages = nil
	e.mu.Unlock()

	if !e.sessionStarted {
		e.sessionStarted = true
		e.fireHookAsync(hooks.SessionStart, hooks.HookInput{
			ToolName:  e.model,
			ToolInput: map[string]string{"source": e.provider, "model": e.model},
		})
	}

	e.fireHookAsync(hooks.UserPromptSubmit, hooks.HookInput{
		ToolInput: text,
	})

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
	// Fire Stop hook before cancellation.
	e.fireHookAsync(hooks.Stop, hooks.HookInput{
		ToolInput: "interrupt",
	})

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

// SetStore wires a store for session learnings persistence.
// Call before the first Send; safe to call with nil to disable.
// Accepts any value that implements storeIface (e.g. *store.Store).
func (e *DirectEngine) SetStore(st storeIface) {
	e.store = st
}

// Close cleanly shuts down the engine and closes the events channel.
// If a store is wired, mechanical session learnings are persisted before closing.
func (e *DirectEngine) Close() {
	// Fire SessionEnd hook before teardown.
	e.fireHookAsync(hooks.SessionEnd, hooks.HookInput{
		ToolInput: "close",
	})

	e.Interrupt()
	if e.bgCancel != nil {
		e.bgCancel()
	}
	if e.fileWatcher != nil {
		e.fileWatcher.Stop()
	}
	if e.subagentRunner != nil {
		e.subagentRunner.Close()
	}
	if e.mcpManager != nil {
		e.mcpManager.CloseAll()
	}
	if e.store != nil {
		e.saveSessionLearnings(e.store, e.startTime)
	}
	// Append mechanical session memory to project MEMORY.md.
	e.appendSessionMemory()
}

// GetCurrentTodos implements engine.TodoProvider.
func (e *DirectEngine) GetCurrentTodos() []engine.TodoItem {
	raw := e.todoTool.GetCurrentTodos()
	out := make([]engine.TodoItem, len(raw))
	for i, t := range raw {
		out[i] = engine.TodoItem{
			ID:       t.ID,
			Content:  t.Content,
			Status:   t.Status,
			Priority: t.Priority,
			ParentID: t.ParentID,
		}
	}
	return out
}

// MCPInstructions returns concatenated MCP server instructions, or empty string
// if no MCP servers are connected. Used by prompt assembly.
func (e *DirectEngine) MCPInstructions() string {
	if e.mcpManager == nil {
		return ""
	}
	return e.mcpManager.GetInstructions()
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

// MapModelForEngine maps a requested model name to the correct model string
// for a target engine. Codex only supports one model, Claude maps aliases,
// and direct/opencode pass through.
func MapModelForEngine(requestedModel, targetEngine string) string {
	switch engine.EngineType(targetEngine) {
	case "codex", "codex_re":
		return "gpt-5.4-codex"
	case engine.EngineTypeClaude, engine.EngineTypeDirect:
		switch requestedModel {
		case "sonnet":
			return "claude-sonnet-4-6"
		case "opus":
			return "claude-opus-4-6"
		case "haiku", "fast":
			return "claude-haiku-4"
		default:
			return requestedModel
		}
	default:
		return requestedModel
	}
}

// subagentExecutor creates a child engine and runs a single-turn conversation,
// returning the assistant's text output. Used as the subagent.Executor callback.
// Supports cross-engine spawning: if agentType.Engine is set and differs from
// "direct", a child engine of the requested type is created via the factory.
func (e *DirectEngine) subagentExecutor(ctx context.Context, prompt string, agentType subagent.AgentType) (string, error) {
	agentID := agentType.Name
	if agentID == "" {
		agentID = "subagent"
	}

	// Fire SubagentStart hook.
	e.fireHookAsync(hooks.SubagentStart, hooks.HookInput{
		ToolName:  agentID,
		ToolInput: agentType.Name,
	})
	defer func() {
		// Fire SubagentStop hook when subagent completes.
		e.fireHookAsync(hooks.SubagentStop, hooks.HookInput{
			ToolName: agentID,
		})
	}()

	systemPrompt := agentType.SystemPrompt + "\n\n" + subagent.AntiRecursionPrompt

	model := agentType.Model
	if model == "inherit" || model == "" {
		model = e.model
	}

	workDir := e.workDir
	if agentType.WorkDir != "" {
		workDir = agentType.WorkDir
	}

	// Cross-engine path: spawn a different engine type.
	agentEngine := agentType.Engine
	if agentEngine != "" && agentEngine != "inherit" && agentEngine != string(engine.EngineTypeDirect) {
		return e.crossEngineExecutor(ctx, prompt, systemPrompt, agentType, model, workDir)
	}

	cfg := engine.EngineConfig{
		Type:             engine.EngineTypeDirect,
		SystemPrompt:     systemPrompt,
		Model:            model,
		APIKey:           e.apiKey,
		WorkDir:          workDir,
		Provider:         e.provider,
		OpenRouterAPIKey: e.openrouterAPIKey,
	}

	sub, err := NewDirectEngine(cfg)
	if err != nil {
		return "", fmt.Errorf("create sub-engine: %w", err)
	}
	defer sub.Close()

	if agentType.PermissionMode != "" && agentType.PermissionMode != "inherit" {
		switch agentType.PermissionMode {
		case "plan":
			sub.permissions.SetMode("plan")
		case "auto":
			sub.permissions.SetMode("auto")
		case "deny":
			sub.permissions.SetMode("deny")
		}
	}

	if err := sub.Send(prompt); err != nil {
		return "", fmt.Errorf("sub-engine send: %w", err)
	}

	return e.drainEngineEvents(ctx, sub, agentType.MaxTurns)
}

// crossEngineExecutor creates a non-direct engine of the requested type and
// collects its output. Used when an agent specifies engine != "direct".
func (e *DirectEngine) crossEngineExecutor(ctx context.Context, prompt, systemPrompt string, agentType subagent.AgentType, model, workDir string) (string, error) {
	engineType := engine.EngineType(agentType.Engine)
	mappedModel := MapModelForEngine(model, agentType.Engine)

	cfg := engine.EngineConfig{
		Type:         engineType,
		SystemPrompt: systemPrompt,
		Model:        mappedModel,
		APIKey:       e.apiKey,
		WorkDir:      workDir,
	}

	childEngine, err := engine.NewEngine(cfg)
	if err != nil {
		return "", fmt.Errorf("create cross-engine %s: %w", engineType, err)
	}
	defer childEngine.Close()

	if err := childEngine.Send(prompt); err != nil {
		return "", fmt.Errorf("cross-engine send: %w", err)
	}

	return e.drainEngineEvents(ctx, childEngine, agentType.MaxTurns)
}

// drainEngineEvents reads events from a child engine until completion,
// collecting text output and enforcing maxTurns.
func (e *DirectEngine) drainEngineEvents(ctx context.Context, child engine.Engine, maxTurns int) (string, error) {
	turnCount := 0
	if maxTurns <= 0 {
		maxTurns = 100 // safety cap
	}

	// Wall-clock timeout: 5 minutes per subagent to prevent infinite hangs.
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	var result strings.Builder
	events := child.Events()
	for {
		select {
		case <-timeoutCtx.Done():
			child.Interrupt()
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			result.WriteString("\n[subagent timed out after 5 minutes]")
			return result.String(), nil
		case ev, ok := <-events:
			if !ok {
				// Channel closed - engine finished.
				return result.String(), nil
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
					child.Interrupt()
					result.WriteString("\n[max turns reached]")
					return result.String(), nil
				}
				return result.String(), nil
			}
		}
	}
}

// subagentContextExecutor creates a child DirectEngine, restores conversation
// state into it, then sends the prompt. Used by /fork for full context inheritance.
func (e *DirectEngine) subagentContextExecutor(ctx context.Context, prompt string, agentType subagent.AgentType, state *subagent.ConversationState) (string, error) {
	systemPrompt := agentType.SystemPrompt + "\n\n" + subagent.AntiRecursionPrompt

	model := agentType.Model
	if model == "inherit" || model == "" {
		model = e.model
	}

	workDir := e.workDir
	if agentType.WorkDir != "" {
		workDir = agentType.WorkDir
	}

	cfg := engine.EngineConfig{
		Type:             engine.EngineTypeDirect,
		SystemPrompt:     systemPrompt,
		Model:            model,
		APIKey:           e.apiKey,
		WorkDir:          workDir,
		Provider:         e.provider,
		OpenRouterAPIKey: e.openrouterAPIKey,
	}

	sub, err := NewDirectEngine(cfg)
	if err != nil {
		return "", fmt.Errorf("create sub-engine: %w", err)
	}
	defer sub.Close()

	if state != nil && len(state.Messages) > 0 {
		restored := make([]engine.RestoredMessage, 0, len(state.Messages))
		for _, pm := range state.Messages {
			restored = append(restored, engine.RestoredMessage{
				Role:    pm.Role,
				Content: pm.Content,
			})
		}
		if restoreErr := sub.RestoreHistory(restored); restoreErr != nil {
			return "", fmt.Errorf("restore context: %w", restoreErr)
		}
	}

	if err := sub.Send(prompt); err != nil {
		return "", fmt.Errorf("sub-engine send: %w", err)
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

		// Snip: drop old message pairs as a cheap first pass.
		msgs := e.history.Messages()
		msgs = compact.SnipOldMessages(msgs, 0)

		// Tool result budget: cap total tool result content.
		msgs = compact.EnforceToolResultBudget(msgs, 0)

		// Microcompact: prune old tool results before the API call (zero cost).
		msgs, _ = compact.Microcompact(msgs)

		toolParams := e.toolParams()

		// Pre-call blocking limit check: if estimated tokens exceed the hard
		// ceiling (effectiveWindow - 3000), skip the API call and trigger
		// reactive compaction instead of wasting an API round-trip.
		if e.compactor != nil {
			currentTokens := e.history.CurrentTokens()
			contextWindow := engine.ContextWindowFor(e.model)
			maxOutput := engine.MaxOutputTokensFor(e.model)
			hardBlock := compact.GetEffectiveContextWindow(contextWindow, maxOutput) - 3000
			if currentTokens > hardBlock {
				e.events <- engine.ParsedEvent{
					Type: "system_message",
					Data: &engine.SystemMessageEvent{
						Type:    "system_message",
						Content: fmt.Sprintf("Prompt tokens (%d) exceed hard limit (%d). Compacting before API call...", currentTokens, hardBlock),
					},
				}
				if compactErr := e.compactor.TriggerReactive(ctx); compactErr == nil {
					continue
				}
				e.emitError(fmt.Errorf("prompt exceeds context window and compaction failed"))
				return
			}
		}

		// Use escalated output token limit if a previous turn hit max_tokens.
		maxTokens := int64(DefaultMaxOutputTokens)
		if e.outputTokensEscalated {
			maxTokens = int64(EscalatedMaxOutputTokens)
		}

		apiParams := anthropic.MessageNewParams{
			Model:     anthropic.Model(e.model),
			MaxTokens: maxTokens,
			System:    e.systemBlocks(),
			Messages:  msgs,
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
			if isFallbackTriggerable(streamErr) && !e.fallbackActive {
				fallback := engine.FastForProvider(e.provider)
				if fallback != "" && fallback != e.model {
					// Tombstone any partial streaming content so UI can clear it.
					e.events <- engine.ParsedEvent{
						Type: "tombstone",
						Data: &engine.TombstoneEvent{Type: "tombstone", MessageIndex: -1},
					}

					// If partial content was streamed with tool_use blocks,
					// synthesize error results so history stays consistent.
					e.synthesizeErrorToolResults(accumulated)

					previousModel := e.model
					e.model = fallback
					e.fallbackActive = true

					// Strip thinking/signature blocks from history before retrying
					// on a different model - they are model-bound and will 400.
					e.history.StripThinkingBlocks()

					e.events <- engine.ParsedEvent{
						Type: "system_message",
						Data: &engine.SystemMessageEvent{
							Type:    "system_message",
							Content: fmt.Sprintf("Model unavailable (%s). Switched from %s to %s.", streamErr, previousModel, fallback),
						},
					}
					continue
				}
			}
			// Synthesize error tool_results for any orphaned tool_use blocks
			// so the next API call doesn't 400 on unmatched pairs.
			e.synthesizeErrorToolResults(accumulated)
			e.emitError(streamErr)
			return
		}
		e.emitUsageUpdate(
			int(accumulated.Usage.InputTokens),
			int(accumulated.Usage.OutputTokens),
			int(accumulated.Usage.CacheReadInputTokens),
			int(accumulated.Usage.CacheCreationInputTokens),
		)

		e.history.AddAssistant(accumulated)
		if e.compactor != nil {
			e.compactor.TriggerIfNeeded(ctx)
		}

		e.emitAssistant(accumulated)

		// Max output tokens recovery: first try escalating to 64k, then
		// fall through to multi-turn resume with recovery prompts.
		if accumulated.StopReason == anthropic.StopReasonMaxTokens {
			// Step 1: Escalate output tokens from 16k to 64k and retry
			// the SAME request (no recovery prompt injection).
			if !e.outputTokensEscalated {
				e.outputTokensEscalated = true
				e.events <- engine.ParsedEvent{
					Type: "system_message",
					Data: &engine.SystemMessageEvent{
						Type:    "system_message",
						Content: fmt.Sprintf("Max output tokens hit at %d. Escalating to %d and retrying...", DefaultMaxOutputTokens, EscalatedMaxOutputTokens),
					},
				}
				// Remove the partial assistant message from history since we're
				// retrying the same request with a higher limit.
				e.history.RemoveLastAssistant()
				continue
			}

			// Step 2: Already at 64k, fall through to multi-turn recovery.
			if e.maxOutputRecoveryCount < MaxOutputTokensRecoveryLimit {
				e.maxOutputRecoveryCount++
				e.history.AddUser("Output token limit hit. Resume directly - no apology, no recap. Pick up mid-thought if that is where the cut happened. Break remaining work into smaller pieces.")
				e.events <- engine.ParsedEvent{
					Type: "system_message",
					Data: &engine.SystemMessageEvent{
						Type:    "system_message",
						Content: fmt.Sprintf("Max output tokens hit at %d (%d/%d), auto-resuming.", EscalatedMaxOutputTokens, e.maxOutputRecoveryCount, MaxOutputTokensRecoveryLimit),
					},
				}
				continue
			}
			e.emitError(fmt.Errorf("max output tokens hit %d times at %d, giving up", MaxOutputTokensRecoveryLimit, EscalatedMaxOutputTokens))
			return
		}

		if accumulated.StopReason != anthropic.StopReasonToolUse {
			// Fire Stop hook on natural turn completion (no more tool calls).
			e.fireHookAsync(hooks.Stop, hooks.HookInput{
				ToolInput: "natural_end",
			})
			e.history.CompressLongToolResults(2000)
			return
		}

		// Wait for any pre-executed ReadOnly tools from streaming overlap to finish.
		e.preExecWg.Wait()

		toolCalls := extractToolCalls(accumulated)
		queue := NewStreamingToolQueue(e.registry)
		for _, tc := range toolCalls {
			e.sessionBus.Publish(session.Event{Type: session.EventToolCallStart, Data: tc.Name})

			// Check if this tool was already pre-executed during streaming.
			e.preExecMu.Lock()
			preResult, preExecuted := e.preExecResults[tc.ID]
			if preExecuted {
				delete(e.preExecResults, tc.ID)
			}
			e.preExecMu.Unlock()

			if preExecuted {
				queue.mu.Lock()
				queue.results = append(queue.results, ToolCallResult{
					ToolCall: tc,
					Result:   preResult,
				})
				queue.mu.Unlock()
				continue
			}

			// Fire PreToolUse hook.
			if hookOut, hookErr := e.fireHook(hooks.PreToolUse, hooks.HookInput{
				ToolName:  tc.Name,
				ToolInput: tc.Input,
			}); hookErr != nil {
				if _, ok := hookErr.(*hooks.BlockingError); ok {
					queue.mu.Lock()
					queue.results = append(queue.results, ToolCallResult{
						ToolCall: tc,
						Result:   tools.ToolResult{Content: "blocked by PreToolUse hook: " + hookErr.Error(), IsError: true},
					})
					queue.mu.Unlock()
					continue
				}
			} else if hookOut != nil && hookOut.SuppressOutput {
				queue.mu.Lock()
				queue.results = append(queue.results, ToolCallResult{
					ToolCall: tc,
					Result:   tools.ToolResult{Content: "suppressed by PreToolUse hook"},
				})
				queue.mu.Unlock()
				continue
			}

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
					// Fire PermissionDenied hook.
					e.fireHookAsync(hooks.PermissionRequest, hooks.HookInput{
						ToolName:  tc.Name,
						ToolInput: "permission denied by user",
					})
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

		var resultBlocks []anthropic.ContentBlockParamUnion
		for _, r := range queue.Results() {
			e.sessionBus.Publish(session.Event{Type: session.EventToolCallResult, Data: r.Result.Content})

			// Fire PostToolUse or PostToolUseFailure hook.
			if r.Result.IsError {
				e.fireHookAsync(hooks.PostToolUseFailure, hooks.HookInput{
					ToolName:  r.Name,
					ToolInput: r.Result.Content,
				})
			} else {
				e.fireHookAsync(hooks.PostToolUse, hooks.HookInput{
					ToolName:  r.Name,
					ToolInput: r.Result.Content,
				})
			}

			// Apply context modifier if the tool returned one.
			// Called after hooks but before appending to history so that
			// state changes (e.g. file cache invalidation) are visible
			// to the next turn.
			if r.Result.ContextModifier != nil {
				r.Result.ContextModifier()
			}

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

		// Computer use cleanup: remove temp files and restore macOS state.
		e.cleanupComputerUse(queue.Results())

		// Tool use summary: when 2+ tools ran, fire a background goroutine to
		// generate a 30-char summary via the fast-tier model. The summary is
		// prepended to the next turn's context so the model can skip re-reading
		// full tool results, saving tokens.
		if len(queue.Results()) >= 2 {
			go e.generateToolSummary(queue.Results())
		}

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

// emitUsageUpdate emits a usage_update event with token counts.
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
// Uses pre-computed e.blocks directly - no comparison hack needed.
func (e *DirectEngine) systemBlocks() []anthropic.TextBlockParam {
	if len(e.blocks) == 0 && e.system == "" {
		return nil
	}

	blocks := e.blocks
	if len(blocks) == 0 {
		// Fallback for engines created without structured blocks.
		blocks = []engine.SystemBlock{{
			Text:      e.system,
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

// drainSteeredMessages checks for mid-turn steering messages and injects them
// as system-reminder blocks so the model addresses them after the current task.
func (e *DirectEngine) drainSteeredMessages() {
	e.steerMu.Lock()
	msgs := e.steered
	e.steered = nil
	e.steerMu.Unlock()

	for _, msg := range msgs {
		reminder := fmt.Sprintf("<system-reminder>\nThe user sent a new message while you were working:\n%s\nIMPORTANT: After completing your current task, address the user's message.\n</system-reminder>", msg)
		e.history.AddUser(reminder)
	}
}

// Steer adds a mid-turn user message that will be injected before the next API call.
func (e *DirectEngine) Steer(text string) {
	e.steerMu.Lock()
	defer e.steerMu.Unlock()
	e.steered = append(e.steered, text)
}

// streamWithRetry calls the Messages API with streaming, retrying up to 3 times
// on 429 rate limit errors with exponential backoff. ReadOnly tools are
// pre-executed as their blocks complete during streaming (overlapped execution).
func (e *DirectEngine) streamWithRetry(ctx context.Context, params anthropic.MessageNewParams) (anthropic.Message, error) {
	const maxRetries = 3

	// Reset pre-execution state for this streaming call.
	e.preExecMu.Lock()
	e.preExecResults = make(map[string]tools.ToolResult)
	e.preExecMu.Unlock()

	for attempt := 0; attempt < maxRetries; attempt++ {
		stream := e.client.Messages.NewStreaming(ctx, params)

		// Track tool_use block indices seen via ContentBlockStartEvent.
		toolBlockIndices := make(map[int64]bool)

		var accumulated anthropic.Message
		for stream.Next() {
			event := stream.Current()
			_ = accumulated.Accumulate(event)
			if pe := translateStreamEvent(event); pe != nil {
				e.events <- *pe
			}

			// Detect tool_use block starts and completions for overlapped execution.
			switch variant := event.AsAny().(type) {
			case anthropic.ContentBlockStartEvent:
				if variant.ContentBlock.Type == "tool_use" {
					toolBlockIndices[variant.Index] = true
				}
			case anthropic.ContentBlockStopEvent:
				if toolBlockIndices[variant.Index] {
					// A tool_use block just completed. Check if its tool is ReadOnly.
					e.maybePreExecuteTool(ctx, accumulated, int(variant.Index))
				}
			}
		}
		if err := stream.Err(); err != nil {
			errStr := err.Error()
			if strings.Contains(errStr, "429") || strings.Contains(errStr, "rate_limit") {
				if attempt < maxRetries-1 {
					delay := engine.Backoff(attempt)
					delaySec := int(delay.Seconds())
					if delaySec < 1 {
						delaySec = 1
					}
					e.events <- engine.ParsedEvent{
						Type: "rate_limit",
						Data: &engine.RateLimitEvent{
							Type:     "rate_limit",
							DelaySec: delaySec,
							Attempt:  attempt + 1,
							MaxRetry: maxRetries,
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

// maybePreExecuteTool checks if the tool_use block at the given index in the
// accumulated message is for a ReadOnly tool. If so, it spawns a goroutine to
// execute it immediately, storing the result in preExecResults.
func (e *DirectEngine) maybePreExecuteTool(ctx context.Context, accumulated anthropic.Message, blockIndex int) {
	if blockIndex >= len(accumulated.Content) {
		return
	}
	block := accumulated.Content[blockIndex]
	if block.Type != "tool_use" {
		return
	}

	tool, ok := e.registry.Get(block.Name)
	if !ok || !tool.ReadOnly() {
		return
	}

	var input map[string]any
	if len(block.Input) > 0 {
		_ = json.Unmarshal(block.Input, &input)
	}
	if input == nil {
		input = make(map[string]any)
	}

	toolID := block.ID
	e.preExecWg.Add(1)
	go func() {
		defer e.preExecWg.Done()
		result := tool.Execute(ctx, input)
		e.preExecMu.Lock()
		e.preExecResults[toolID] = result
		e.preExecMu.Unlock()
	}()
}

// synthesizeErrorToolResults checks if an accumulated (partial) response contains
// tool_use blocks and, if so, adds matching error tool_results to history. This
// prevents the next API call from failing on unmatched tool_use/tool_result pairs.
func (e *DirectEngine) synthesizeErrorToolResults(accumulated anthropic.Message) {
	toolCalls := extractToolCalls(accumulated)
	if len(toolCalls) == 0 {
		return
	}

	// Add the partial assistant message to history so the tool_results have
	// a matching assistant turn.
	e.history.AddAssistant(accumulated)

	var resultBlocks []anthropic.ContentBlockParamUnion
	for _, tc := range toolCalls {
		resultBlocks = append(resultBlocks, anthropic.NewToolResultBlock(
			tc.ID, "[tool execution skipped due to API error]", true,
		))
	}
	e.history.AddToolResults(resultBlocks)
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

// isFallbackTriggerable returns true if the error warrants a model fallback.
// Covers overload errors plus server-side failures (500, 502, 503) that commonly
// occur mid-stream and indicate the primary model is temporarily unavailable.
func isFallbackTriggerable(err error) bool {
	if err == nil {
		return false
	}
	if isOverloadError(err) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "500") ||
		strings.Contains(msg, "502") ||
		strings.Contains(msg, "503") ||
		strings.Contains(msg, "server_error") ||
		strings.Contains(msg, "internal_error")
}

// fireHook runs hooks for the given event synchronously and returns the output.
// Returns nil output if no hooks are registered or all hooks return nil.
func (e *DirectEngine) fireHook(event string, input hooks.HookInput) (*hooks.HookOutput, error) {
	if e.hooksRunner == nil {
		return nil, nil
	}
	input.SessionID = e.sessionID
	return e.hooksRunner.Run(e.ctx, event, input)
}

// fireHookAsync runs hooks for the given event without blocking.
func (e *DirectEngine) fireHookAsync(event string, input hooks.HookInput) {
	if e.hooksRunner == nil {
		return
	}
	input.SessionID = e.sessionID
	e.hooksRunner.RunAsync(e.ctx, event, input)
}

// generateToolSummary uses the fast-tier model to produce a short summary of
// what the tool batch accomplished. The summary is injected as a system note
// so the primary model can skip re-reading verbose tool output on the next turn.
func (e *DirectEngine) generateToolSummary(results []ToolCallResult) {
	fastModel := engine.FastForProvider(e.provider)
	if fastModel == "" || e.codexMode || e.openrouterMode {
		return
	}

	var sb strings.Builder
	sb.WriteString("Summarize what these tool calls did in under 30 characters:\n")
	for _, r := range results {
		content := r.Result.Content
		if len(content) > 200 {
			content = content[:200] + "..."
		}
		sb.WriteString(fmt.Sprintf("- %s: %s\n", r.Name, content))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := e.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(fastModel),
		MaxTokens: 64,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(sb.String())),
		},
	})
	if err != nil {
		return
	}

	var summary string
	for _, block := range resp.Content {
		if block.Type == "text" {
			summary = block.Text
			break
		}
	}
	if summary == "" {
		return
	}

	// Truncate to 60 chars max (model may overshoot the 30-char ask).
	if len(summary) > 60 {
		summary = summary[:60]
	}

	// Inject as a system note in the conversation history.
	e.history.AddUser(fmt.Sprintf("[Tool summary: %s]", summary))
}

// computerUseToolNames lists the CU tools that may leave macOS state dirty.
var computerUseToolNames = map[string]bool{
	"Screenshot":   true,
	"DesktopClick": true,
	"DesktopType":  true,
	"DesktopApps":  true,
	"Clipboard":    true,
}

// cleanupComputerUse restores macOS state after computer use tools:
// removes temp screenshot files and ensures no apps were left hidden.
func (e *DirectEngine) cleanupComputerUse(toolResults []ToolCallResult) {
	usedCU := false
	for _, r := range toolResults {
		if computerUseToolNames[r.Name] {
			usedCU = true
			break
		}
	}
	if !usedCU {
		return
	}

	// Clean up temp screenshot files.
	matches, _ := filepath.Glob("/tmp/providence-screenshot-*.png")
	for _, f := range matches {
		os.Remove(f)
	}
}
