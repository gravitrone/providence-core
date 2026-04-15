package direct

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/google/uuid"
	"github.com/gravitrone/providence-core/internal/auth"
	"github.com/gravitrone/providence-core/internal/bridge/macos"
	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/gravitrone/providence-core/internal/engine/compact"
	"github.com/gravitrone/providence-core/internal/engine/direct/tools"
	"github.com/gravitrone/providence-core/internal/engine/filewatch"
	"github.com/gravitrone/providence-core/internal/engine/hooks"
	"github.com/gravitrone/providence-core/internal/engine/mcp"
	"github.com/gravitrone/providence-core/internal/engine/session"
	"github.com/gravitrone/providence-core/internal/engine/subagent"
	"github.com/gravitrone/providence-core/internal/engine/teams"
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

	// Cache-break diagnostics: last fingerprint of the inputs that
	// contribute to the Anthropic prompt cache key. Compared on every
	// API call so we can write a diff when the cache prefix changes.
	lastFingerprint CacheFingerprint
	cacheFpMu       sync.Mutex

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

	// Unattended retry mode: when true, 429/529 errors retry indefinitely
	// (with max 5min backoff, 30s heartbeat, 6hr total cap) instead of
	// giving up after maxRetries. Set by ember autonomous mode or
	// PROVIDENCE_UNATTENDED_RETRY env var.
	unattendedRetry bool

	// Content replacement tracking: maps toolUseID -> original content for
	// tool results that were compressed or cleared by microcompact/budget passes.
	// Allows future restore if context re-expansion is needed.
	contentReplacements   map[string]string
	contentReplacementsMu sync.Mutex

	// Task budget tracking: total token budget for the session. When non-zero,
	// each API call's usage is deducted. Warnings fire near exhaustion, and
	// the session stops when the budget is fully consumed.
	tokenBudget    int
	tokensConsumed int
	budgetMu       sync.Mutex

	// Stuck loop detection: track recent tool calls to detect repeated patterns.
	loopHistory   [5]string // ring buffer of "toolName:argsHash" keys
	loopIdx       int       // next write index into loopHistory
	loopFillCount int       // how many entries have been written (max 5)

	// Team membership: when set, inbox is polled for teammate messages.
	teamStore   *teams.Store
	teamName    string // team this engine belongs to (empty = no team)
	teamAgentID string // this agent's name within the team

	// Coordinator mode: when true, only coordinator-safe tools are exposed.
	// The leader gets Agent, SendMessage, TaskStop (Kill), TeamCreate,
	// TeamDelete, TodoWrite, and brief. Workers get all tools.
	coordinatorMode bool

	// Caffeinate: prevent macOS sleep while the engine is active.
	caffeinator *engine.Caffeinator

	// Context injector for overlay screen-context reminders.
	contextInjector contextInjector
}

// contextInjector is a local interface matching overlay.Injector to avoid an
// import cycle between internal/engine/direct and internal/overlay. The
// vision pipeline pulls ScreenshotPNGs() + Transcript() at turn start; the
// PendingSystemReminder is now just the rolling-transcript wrapper.
type contextInjector interface {
	PendingSystemReminder() string
	// ScreenshotPNGs returns the decoded PNG frames in the bridge ring buffer,
	// oldest first. Empty/nil = no overlay attached or screen capture off.
	ScreenshotPNGs() [][]byte
	Transcript() string
	// MarkAttached lets the bridge know the engine consumed the current ring
	// head so it can dedup identical attachments on subsequent ember ticks.
	MarkAttached()
	// RingChangedSinceLastAttach allows the engine to skip re-attaching the
	// same frames when nothing new has come in (saves token cost on idle).
	RingChangedSinceLastAttach() bool
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
		tools.NewFileHistoryTool(),
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
			tools.NewDesktopFindElementTool(bridge),
			tools.NewDesktopClickElementTool(bridge),
			tools.NewDesktopReadScreenTool(bridge),
			tools.NewScreenDiffTool(bridge),
			tools.NewDesktopActionBatchTool(bridge),
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
		client:              client,
		model:               model,
		system:              sysFlat,
		blocks:              sysBlocks,
		events:              make(chan engine.ParsedEvent, 64),
		history:             history,
		registry:            registry,
		permissions:         NewPermissionHandler(),
		workDir:             cfg.WorkDir,
		sessionID:           uuid.New().String(),
		status:              engine.StatusIdle,
		ctx:                 ctx,
		cancel:              cancel,
		provider:            providerName,
		codexMode:           isCodex,
		openrouterMode:      isOpenRouter,
		openrouterAPIKey:    openrouterKey,
		subagentRunner:      subagent.NewRunnerWithWorkDir(cfg.WorkDir),
		apiKey:              cfg.APIKey,
		sessionBus:          session.NewBus(),
		todoTool:            todoTool,
		startTime:           time.Now(),
		hooksRunner:         hooksRunner,
		contentReplacements: make(map[string]string),
		caffeinator:         engine.NewCaffeinator(5 * time.Minute),
	}

	taskTool := tools.NewTaskTool(e.subagentRunner, e.subagentExecutor)
	registry.Register(taskTool)
	sendMsgTool := tools.NewSendMessageTool(e.subagentRunner)
	registry.Register(sendMsgTool)

	// Brief tool: proactive notifications via session bus.
	briefTool := tools.NewBriefTool(e.sessionBus)
	registry.Register(briefTool)

	// Config tool: runtime settings get/set.
	configTool := tools.NewConfigTool()
	registry.Register(configTool)

	// StructuredOutput tool: headless JSON output (enabled in headless mode only).
	structuredOutputTool := tools.NewStructuredOutputTool()
	registry.Register(structuredOutputTool)

	// Team tools: TeamCreate and TeamDelete for agent team coordination.
	if teamStore, err := teams.DefaultStore(); err == nil {
		teamCreateTool := tools.NewTeamCreateTool(teamStore)
		registry.Register(teamCreateTool)
		teamDeleteTool := tools.NewTeamDeleteTool(teamStore)
		registry.Register(teamDeleteTool)
	}

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

// CacheSafeSystemBlocks returns the engine's pre-built system prompt blocks
// for cache-sharing with forked subagents. The child engine can reuse these
// exact bytes so the Anthropic API prompt cache key matches the parent.
func (e *DirectEngine) CacheSafeSystemBlocks() []subagent.SystemBlock {
	if len(e.blocks) == 0 {
		return nil
	}
	out := make([]subagent.SystemBlock, len(e.blocks))
	for i, b := range e.blocks {
		out[i] = subagent.SystemBlock{
			Text:      b.Text,
			Cacheable: b.Cacheable,
		}
	}
	return out
}

// SessionBus returns the engine's session event bus.
func (e *DirectEngine) SessionBus() *session.Bus {
	return e.sessionBus
}

// Model returns the active model identifier.
func (e *DirectEngine) Model() string { return e.model }

// EngineType returns the engine registry key.
func (e *DirectEngine) EngineType() string { return "direct" }

// SetRegistry replaces the tool registry (for use before first Send).
func (e *DirectEngine) SetRegistry(r *tools.Registry) {
	e.registry = r
}

// JoinTeam assigns this engine to a team for inbox polling.
// Pass the team name and the agent's identifier within the team.
func (e *DirectEngine) JoinTeam(store *teams.Store, teamName, agentID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.teamStore = store
	e.teamName = teamName
	e.teamAgentID = agentID
}

// LeaveTeam removes team membership from this engine.
func (e *DirectEngine) LeaveTeam() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.teamStore = nil
	e.teamName = ""
	e.teamAgentID = ""
}

// TeamInfo returns the current team name and agent ID, or empty strings.
func (e *DirectEngine) TeamInfo() (string, string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.teamName, e.teamAgentID
}

// SetCoordinatorMode enables or disables coordinator mode.
// When active, only orchestration tools are exposed to the model.
func (e *DirectEngine) SetCoordinatorMode(on bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.coordinatorMode = on
}

// IsCoordinatorMode returns whether the engine is in coordinator mode.
func (e *DirectEngine) IsCoordinatorMode() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.coordinatorMode
}

// SetUnattendedRetry enables or disables unattended retry mode.
// When active, 429/529 errors retry indefinitely with max 5min backoff
// and a 6hr total cap instead of giving up after maxRetries.
func (e *DirectEngine) SetUnattendedRetry(on bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.unattendedRetry = on
}

// isUnattendedRetry returns true if unattended retry mode is active,
// checking both the engine flag and the PROVIDENCE_UNATTENDED_RETRY env var.
func (e *DirectEngine) isUnattendedRetry() bool {
	e.mu.Lock()
	flag := e.unattendedRetry
	e.mu.Unlock()
	if flag {
		return true
	}
	if v := os.Getenv("PROVIDENCE_UNATTENDED_RETRY"); v != "" {
		return v == "true" || v == "1"
	}
	return false
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

// SetContextInjector attaches a context injector (typically the overlay bridge).
// When set, each user message sent via Send gets any pending system-reminder
// prepended and the injector is cleared. Nil-safe: a nil injector means no-op.
// The overlay writes to the injector from its UDS handler.
func (e *DirectEngine) SetContextInjector(inj contextInjector) {
	e.contextInjector = inj
}

// prepareUserText prepends any pending overlay system-reminder to text.
// Returns the (possibly modified) text. Safe to call with nil injector.
func (e *DirectEngine) prepareUserText(text string) string {
	if e.contextInjector == nil {
		return text
	}
	if reminder := e.contextInjector.PendingSystemReminder(); reminder != "" {
		return reminder + "\n\n" + text
	}
	return text
}

// Send sends a user message to the AI and starts the agent loop.
func (e *DirectEngine) Send(text string) error {
	text = e.prepareUserText(text)
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

	// Ambient mode: pull screenshots from the overlay ring buffer and attach
	// them to this turn as image content. Subsample to oldest + 2 newest so
	// the model gets a "before / now-1 / now" view without ballooning token
	// cost. Skip if the ring is unchanged since the last attach (idle ember
	// ticks during quiet periods).
	if e.contextInjector != nil && e.contextInjector.RingChangedSinceLastAttach() {
		pngs := e.contextInjector.ScreenshotPNGs()
		if len(pngs) > 0 {
			selected := selectAmbientFrames(pngs)
			for _, png := range selected {
				images = append(images, ImageData{MediaType: "image/png", Data: png})
			}
			e.contextInjector.MarkAttached()
		}
	}

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
// Each shutdown step has its own timeout so a hung subsystem cannot block exit.
// A 10s failsafe timer guarantees Close returns even if everything hangs.
func (e *DirectEngine) Close() {
	failsafe, failsafeCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer failsafeCancel()

	// Step 1: Cancel background agents.
	runWithDeadline(failsafe, 2*time.Second, func() {
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
		if e.caffeinator != nil {
			e.caffeinator.Stop()
		}
	})

	// Step 2: Close MCP servers (3s timeout).
	runWithDeadline(failsafe, 3*time.Second, func() {
		if e.mcpManager != nil {
			e.mcpManager.CloseAll()
		}
	})

	// Step 3: Fire SessionEnd hook (1.5s timeout).
	runWithDeadline(failsafe, 1500*time.Millisecond, func() {
		if e.hooksRunner != nil {
			ctx, cancel := context.WithTimeout(failsafe, 1500*time.Millisecond)
			defer cancel()
			e.hooksRunner.Run(ctx, hooks.SessionEnd, hooks.HookInput{
				SessionID: e.sessionID,
				ToolInput: "close",
			})
		}
	})

	// Step 4: Save learnings.
	runWithDeadline(failsafe, 2*time.Second, func() {
		if e.store != nil {
			e.saveSessionLearnings(e.store, e.startTime)
		}
		e.appendSessionMemory()
	})

	// Step 5: Print resume hint.
	if e.sessionID != "" {
		fmt.Fprintf(os.Stderr, "Resume with: providence --resume %s\n", e.sessionID)
	}
}

// runWithDeadline executes fn in a goroutine and waits for it to finish, the
// step timeout to expire, or the parent context to cancel, whichever comes first.
func runWithDeadline(parent context.Context, timeout time.Duration, fn func()) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	case <-parent.Done():
	}
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

// QuickQuery performs a single-turn, tool-free API call using the engine's
// client and model. Intended for lightweight side queries like /btw.
func (e *DirectEngine) QuickQuery(ctx context.Context, systemPrompt, userMessage string) (string, error) {
	if e.codexMode || e.openrouterMode {
		return "", fmt.Errorf("quick query not supported on codex/openrouter engines")
	}
	resp, err := e.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(e.model),
		MaxTokens: 1024,
		System: []anthropic.TextBlockParam{
			{Text: systemPrompt},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userMessage)),
		},
	})
	if err != nil {
		return "", fmt.Errorf("quick query failed: %w", err)
	}
	var sb strings.Builder
	for _, block := range resp.Content {
		if block.Type == "text" {
			sb.WriteString(block.Text)
		}
	}
	return sb.String(), nil
}

// MCPInstructions returns concatenated MCP server instructions, or empty string
// if no MCP servers are connected. Used by prompt assembly.
func (e *DirectEngine) MCPInstructions() string {
	if e.mcpManager == nil {
		return ""
	}
	return e.mcpManager.GetInstructions()
}

// MCPServerInfo holds status information for a single MCP server.
type MCPServerInfo struct {
	Name      string
	ToolCount int
}

// MCPStatus returns status information for all configured MCP servers.
// Returns nil when no MCP manager is configured.
func (e *DirectEngine) MCPStatus() []MCPServerInfo {
	if e.mcpManager == nil {
		return nil
	}
	allTools := e.mcpManager.GetAllTools()
	out := make([]MCPServerInfo, 0, len(allTools))
	for name, tools := range allTools {
		out = append(out, MCPServerInfo{
			Name:      name,
			ToolCount: len(tools),
		})
	}
	return out
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

// TriggerCollapse runs lightweight context collapse on the conversation
// history, summarizing groups of old tool-result turns into 1-line stubs.
// This is cheaper than full compaction (no API call). Returns the number
// of tool-result blocks collapsed.
func (e *DirectEngine) TriggerCollapse() (int, error) {
	msgs := e.history.Messages()
	collapsed, n := compact.ContextCollapse(msgs)
	if n > 0 {
		e.history.ReplaceAll(collapsed)
	}
	return n, nil
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
	case "codex", "codex_headless":
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
//
// Cache-sharing optimization: when the ConversationState carries
// CacheSafeSystemBlocks (the parent's pre-built prompt blocks), those exact
// bytes are reused in the child engine. This ensures the Anthropic API
// prompt cache key matches between parent and child, giving near-zero extra
// input cost for forked agents.
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

	// Cache-sharing: reuse parent's system blocks if available so the API
	// prompt cache key matches (identical byte prefix = cache hit).
	var sysBlocks []engine.SystemBlock
	if state != nil && len(state.CacheSafeSystemBlocks) > 0 {
		sysBlocks = make([]engine.SystemBlock, len(state.CacheSafeSystemBlocks))
		for i, sb := range state.CacheSafeSystemBlocks {
			sysBlocks[i] = engine.SystemBlock{
				Text:      sb.Text,
				Cacheable: sb.Cacheable,
			}
		}
	}

	cfg := engine.EngineConfig{
		Type:             engine.EngineTypeDirect,
		SystemPrompt:     systemPrompt,
		SystemBlocks:     sysBlocks,
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

		// Per-turn context injection: inject dynamic context as a system-reminder
		// block before each API call (recent file changes, time since last message, etc).
		e.injectPerTurnContext()

		// Attachment injection: check for pending file change attachments, memory
		// results, or skill discovery results between tool results and next API call.
		e.injectPendingAttachments()

		// MCP tool refresh: pick up newly-connected MCP servers mid-conversation.
		if e.mcpManager != nil {
			e.mcpManager.RefreshTools()
		}

		// Snip: drop old message pairs as a cheap first pass.
		msgs := e.history.Messages()
		msgs = compact.SnipOldMessages(msgs, 0)

		// Tool result budget: cap total tool result content.
		msgs = compact.EnforceToolResultBudget(msgs, 0)

		// Microcompact: prune old tool results before the API call (zero cost).
		msgs, _ = compact.Microcompact(msgs)

		// Context collapse: before full autocompact, try a cheaper "collapse"
		// pass that summarizes groups of old tool turns into 1-line stubs.
		// Only trigger full compaction if collapse doesn't get under threshold.
		msgs, collapsed := compact.ContextCollapse(msgs)
		if collapsed > 0 {
			e.events <- engine.ParsedEvent{
				Type: "system_message",
				Data: &engine.SystemMessageEvent{
					Type:    "system_message",
					Content: fmt.Sprintf("Collapsed %d old tool results to save context.", collapsed),
				},
			}
		}

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

		// Keep macOS awake during active streaming.
		e.caffeinator.Start()

		// Record any cache-prefix drift just before the API call. Fire
		// and forget - diagnostics must never fail a real request.
		e.checkAndRecordCacheBreak()

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

		// Content error classification: detect PDF, multi-image, and image
		// API errors and show user-friendly messages with actionable hints.
		if streamErr != nil {
			if contentMsg := classifyContentError(streamErr); contentMsg != "" {
				e.events <- engine.ParsedEvent{
					Type: "system_message",
					Data: &engine.SystemMessageEvent{
						Type:    "system_message",
						Content: contentMsg,
					},
				}
				e.emitError(fmt.Errorf("content error: %s", contentMsg))
				return
			}
			if imgMsg := classifyImageError(streamErr); imgMsg != "" {
				e.events <- engine.ParsedEvent{
					Type: "system_message",
					Data: &engine.SystemMessageEvent{
						Type:    "system_message",
						Content: imgMsg,
					},
				}
				e.emitError(fmt.Errorf("image error: %s", imgMsg))
				return
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

		// Task budget tracking: deduct tokens and stop if exhausted.
		if e.deductBudget(int(accumulated.Usage.InputTokens), int(accumulated.Usage.OutputTokens)) {
			e.history.AddAssistant(accumulated)
			e.emitAssistant(accumulated)
			e.events <- engine.ParsedEvent{
				Type: "system_message",
				Data: &engine.SystemMessageEvent{
					Type:    "system_message",
					Content: "Token budget exhausted. Stopping session.",
				},
			}
			return
		}

		e.history.AddAssistant(accumulated)
		if e.compactor != nil {
			e.compactor.TriggerIfNeeded(ctx)
		}

		e.emitAssistant(accumulated)

		// Fire PostSampling hook after each model response completes.
		e.fireHookAsync(hooks.PostSampling, hooks.HookInput{
			ToolName:  e.model,
			ToolInput: accumulated.StopReason,
		})

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

		// Stuck loop detection: track tool calls in a ring buffer.
		// If 3+ consecutive entries are identical, inject a nudge.
		if len(toolCalls) == 1 {
			inputJSON, _ := json.Marshal(toolCalls[0].Input)
			key := toolCalls[0].Name + ":" + string(inputJSON)
			e.loopHistory[e.loopIdx%5] = key
			e.loopIdx++
			if e.loopFillCount < 5 {
				e.loopFillCount++
			}
			if e.loopFillCount >= 3 {
				consecutive := 0
				for i := 1; i <= e.loopFillCount; i++ {
					idx := ((e.loopIdx - i) % 5 + 5) % 5
					if e.loopHistory[idx] == key {
						consecutive++
					} else {
						break
					}
				}
				if consecutive >= 3 {
					e.history.AddUser("You appear to be in a loop. Try a different approach.")
					e.events <- engine.ParsedEvent{
						Type: "system_message",
						Data: &engine.SystemMessageEvent{
							Type:    "system_message",
							Content: "Loop detected: same tool+args called 3 times consecutively. Injected nudge.",
						},
					}
				}
			}
		} else if len(toolCalls) > 1 {
			// Multiple different tools in one turn breaks any loop pattern.
			e.loopFillCount = 0
			e.loopIdx = 0
		}

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
			// Normalize tool result before adding to history.
			r.Result.Content = normalizeToolResult(r.Result.Content, maxToolResultSize)

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

// coordinatorToolSet defines which tools are available in coordinator mode.
// The leader only gets orchestration tools - all actual work goes through agents.
var coordinatorToolSet = map[string]bool{
	"Agent":       true,
	"SendMessage": true,
	"TeamCreate":  true,
	"TeamDelete":  true,
	"TodoWrite":   true,
	"Brief":       true,
	"Read":        true,
	"AskUser":     true,
}

// filteredTools returns the tool list, optionally filtered for coordinator mode.
func (e *DirectEngine) filteredTools() []tools.Tool {
	allTools := e.registry.All()
	if !e.coordinatorMode {
		return allTools
	}
	filtered := make([]tools.Tool, 0, len(coordinatorToolSet))
	for _, t := range allTools {
		if coordinatorToolSet[t.Name()] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// toolParams converts the registry into Anthropic SDK tool params.
func (e *DirectEngine) toolParams() []anthropic.ToolUnionParam {
	allTools := e.filteredTools()
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

// streamMaxRetries returns the configured max retry count from PROVIDENCE_MAX_RETRIES
// env var, defaulting to 10.
func streamMaxRetries() int {
	if v := os.Getenv("PROVIDENCE_MAX_RETRIES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 10
}

// extractRetryDelay tries to parse a Retry-After delay from the API error response
// headers. It checks the standard Retry-After header and the Anthropic-specific
// anthropic-ratelimit-unified-reset header. Returns 0 if no usable value is found.
func extractRetryDelay(err error) time.Duration {
	var apiErr *anthropic.Error
	if !errors.As(err, &apiErr) || apiErr.Response == nil {
		return 0
	}

	// Try standard Retry-After header first (value in seconds).
	if ra := apiErr.Response.Header.Get("Retry-After"); ra != "" {
		if secs, parseErr := strconv.Atoi(ra); parseErr == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
		// Retry-After can also be an HTTP-date; parse RFC1123.
		if t, parseErr := time.Parse(time.RFC1123, ra); parseErr == nil {
			if d := time.Until(t); d > 0 {
				return d
			}
		}
	}

	// Try Anthropic-specific reset timestamp header.
	if reset := apiErr.Response.Header.Get("anthropic-ratelimit-unified-reset"); reset != "" {
		if t, parseErr := time.Parse(time.RFC3339, reset); parseErr == nil {
			if d := time.Until(t); d > 0 {
				return d
			}
		}
	}

	return 0
}

// streamIdleTimeout returns the idle watchdog timeout from
// PROVIDENCE_STREAM_IDLE_TIMEOUT_MS env var, defaulting to 90 seconds.
func streamIdleTimeout() time.Duration {
	if v := os.Getenv("PROVIDENCE_STREAM_IDLE_TIMEOUT_MS"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return 90 * time.Second
}

// unattendedMaxBackoff is the maximum delay between retries in unattended mode.
const unattendedMaxBackoff = 5 * time.Minute

// unattendedHeartbeat is how often keep-alive events are emitted during long waits.
const unattendedHeartbeat = 30 * time.Second

// unattendedTotalCap is the maximum total wall time for unattended retry loops.
const unattendedTotalCap = 6 * time.Hour

// streamWithRetry calls the Messages API with streaming, retrying on 429 rate
// limit errors. Uses Retry-After header delay when available, falling back to
// exponential backoff. Max retries configurable via PROVIDENCE_MAX_RETRIES (default 10).
// When unattended retry mode is active (ember/autonomous), 429 and 529 errors
// retry indefinitely with max 5min backoff, 30s heartbeat, and 6hr total cap.
// A stream idle watchdog cancels the attempt if no events arrive within the
// idle timeout (PROVIDENCE_STREAM_IDLE_TIMEOUT_MS, default 90000ms).
// ReadOnly tools are pre-executed as their blocks complete during streaming.
func (e *DirectEngine) streamWithRetry(ctx context.Context, params anthropic.MessageNewParams) (anthropic.Message, error) {
	maxRetries := streamMaxRetries()
	idleTimeout := streamIdleTimeout()
	unattended := e.isUnattendedRetry()
	startTime := time.Now()

	// Reset pre-execution state for this streaming call.
	e.preExecMu.Lock()
	e.preExecResults = make(map[string]tools.ToolResult)
	e.preExecMu.Unlock()

	for attempt := 0; ; attempt++ {
		// In normal mode, enforce the max retry limit.
		if !unattended && attempt >= maxRetries {
			break
		}
		// In unattended mode, enforce the total time cap.
		if unattended && time.Since(startTime) > unattendedTotalCap {
			return anthropic.Message{}, fmt.Errorf("unattended retry cap reached (%s)", unattendedTotalCap)
		}

		// Create a child context so the idle watchdog can cancel just this attempt.
		attemptCtx, attemptCancel := context.WithCancel(ctx)

		stream := e.client.Messages.NewStreaming(attemptCtx, params)

		// Idle watchdog: a timer that fires if no stream events arrive within
		// the timeout. On fire, it cancels the attempt context so the stream
		// read returns an error and streamWithRetry can retry.
		idleTimer := time.NewTimer(idleTimeout)
		idleTriggered := false
		go func() {
			select {
			case <-idleTimer.C:
				idleTriggered = true
				attemptCancel()
			case <-attemptCtx.Done():
			}
		}()

		// Track tool_use block indices seen via ContentBlockStartEvent.
		toolBlockIndices := make(map[int64]bool)

		var accumulated anthropic.Message
		for stream.Next() {
			// Reset idle watchdog on each event.
			if !idleTimer.Stop() {
				select {
				case <-idleTimer.C:
				default:
				}
			}
			idleTimer.Reset(idleTimeout)

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

		idleTimer.Stop()
		attemptCancel()

		if err := stream.Err(); err != nil {
			// Idle watchdog fired - treat as a transient error and retry.
			if idleTriggered {
				if !unattended && attempt >= maxRetries-1 {
					return accumulated, err
				}
				continue
			}

			// Auth errors: attempt recovery (OAuth refresh) once, then fail.
			if isAuthError(err) {
				if e.handleAuthError(err) {
					continue // recovery succeeded, retry
				}
				return accumulated, err
			}

			errStr := err.Error()
			isRateLimit := strings.Contains(errStr, "429") || strings.Contains(errStr, "rate_limit")
			isOverload := isOverloadError(err)

			if isRateLimit || (unattended && isOverload) {
				canRetry := unattended || attempt < maxRetries-1
				if canRetry {
					delay := extractRetryDelay(err)
					if delay == 0 {
						delay = engine.Backoff(attempt)
					}
					// In unattended mode, cap backoff at 5 minutes.
					if unattended && delay > unattendedMaxBackoff {
						delay = unattendedMaxBackoff
					}
					delaySec := int(delay.Seconds())
					if delaySec < 1 {
						delaySec = 1
					}
					displayMax := maxRetries
					if unattended {
						displayMax = -1 // signals "indefinite" to UI
					}
					e.events <- engine.ParsedEvent{
						Type: "rate_limit",
						Data: &engine.RateLimitEvent{
							Type:     "rate_limit",
							DelaySec: delaySec,
							Attempt:  attempt + 1,
							MaxRetry: displayMax,
						},
					}
					e.sleepWithHeartbeat(ctx, delay)
					continue
				}
			}
			return accumulated, err
		}
		return accumulated, nil
	}
	return anthropic.Message{}, fmt.Errorf("stream retry exhausted")
}

// sleepWithHeartbeat sleeps for the given duration, emitting keep-alive system
// messages every unattendedHeartbeat so the TUI doesn't appear frozen.
func (e *DirectEngine) sleepWithHeartbeat(ctx context.Context, total time.Duration) {
	remaining := total
	for remaining > 0 {
		chunk := unattendedHeartbeat
		if chunk > remaining {
			chunk = remaining
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(chunk):
		}
		remaining -= chunk
		if remaining > 0 {
			e.events <- engine.ParsedEvent{
				Type: "system_message",
				Data: &engine.SystemMessageEvent{
					Type:    "system_message",
					Content: fmt.Sprintf("Waiting for rate limit... %s remaining", remaining.Round(time.Second)),
				},
			}
		}
	}
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

// isAuthError returns true if the error indicates an authentication or
// authorization failure (HTTP 401 or 403).
func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "401") ||
		strings.Contains(msg, "403") ||
		strings.Contains(msg, "authentication_error") ||
		strings.Contains(msg, "permission_error") ||
		strings.Contains(msg, "organization_disabled")
}

// classifyAuthError returns a user-friendly message for auth errors, or empty
// string if the error is not auth-related.
func classifyAuthError(err error, provider string) string {
	if err == nil {
		return ""
	}
	msg := err.Error()

	// 403 with organization disabled.
	if strings.Contains(msg, "organization_disabled") || strings.Contains(msg, "organization") && strings.Contains(msg, "403") {
		return "API access denied: your organization has been disabled. Check your Anthropic dashboard."
	}

	// 401 handling differs by provider.
	if strings.Contains(msg, "401") || strings.Contains(msg, "authentication_error") {
		switch provider {
		case engine.ProviderOpenAI:
			return "OpenAI OAuth token invalid or expired. Attempting refresh..."
		case engine.ProviderAnthropic:
			return "API key invalid or expired. Check ANTHROPIC_API_KEY env var."
		case engine.ProviderOpenRouter:
			return "OpenRouter API key invalid or expired. Check OPENROUTER_API_KEY env var."
		default:
			return "API authentication failed. Check your API key or credentials."
		}
	}

	// 403 generic.
	if strings.Contains(msg, "403") || strings.Contains(msg, "permission_error") {
		return "API access forbidden (403). Check your API key permissions."
	}

	return ""
}

// handleAuthError attempts to recover from auth errors. For OpenAI OAuth, it
// tries to refresh the token and rebuild the client. Returns true if recovery
// succeeded and the caller should retry.
func (e *DirectEngine) handleAuthError(err error) bool {
	if !isAuthError(err) {
		return false
	}

	msg := err.Error()

	// Only attempt OAuth refresh for OpenAI 401 errors.
	if e.codexMode && (strings.Contains(msg, "401") || strings.Contains(msg, "authentication_error")) {
		// Check if we have saved OAuth tokens to refresh.
		tokens, loadErr := auth.LoadOpenAITokens()
		if loadErr != nil {
			return false
		}
		if tokens.RefreshToken == "" {
			return false
		}

		e.events <- engine.ParsedEvent{
			Type: "system_message",
			Data: &engine.SystemMessageEvent{
				Type:    "system_message",
				Content: "OpenAI token expired. Refreshing...",
			},
		}

		refreshed, refreshErr := auth.RefreshOpenAI(tokens.RefreshToken)
		if refreshErr != nil {
			e.events <- engine.ParsedEvent{
				Type: "system_message",
				Data: &engine.SystemMessageEvent{
					Type:    "system_message",
					Content: fmt.Sprintf("Token refresh failed: %v. Run /auth to re-authenticate.", refreshErr),
				},
			}
			return false
		}

		// Save refreshed tokens.
		_ = auth.SaveOpenAITokens(refreshed)

		e.events <- engine.ParsedEvent{
			Type: "system_message",
			Data: &engine.SystemMessageEvent{
				Type:    "system_message",
				Content: "Token refreshed successfully. Retrying...",
			},
		}
		return true
	}

	// For non-OAuth providers, emit the specific error message but don't recover.
	if authMsg := classifyAuthError(err, e.provider); authMsg != "" {
		e.events <- engine.ParsedEvent{
			Type: "system_message",
			Data: &engine.SystemMessageEvent{
				Type:    "system_message",
				Content: authMsg,
			},
		}
	}

	return false
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

// --- Task budget tracking ---

// SetTokenBudget sets the total token budget for this session. When non-zero,
// each API call's total tokens (input + output) are deducted from the budget.
// Warnings fire when 80% consumed, and the session stops when exhausted.
func (e *DirectEngine) SetTokenBudget(budget int) {
	e.budgetMu.Lock()
	defer e.budgetMu.Unlock()
	e.tokenBudget = budget
	e.tokensConsumed = 0
}

// deductBudget records token usage and returns true if the budget is exhausted.
// Also emits warnings when approaching the limit.
func (e *DirectEngine) deductBudget(inputTokens, outputTokens int) bool {
	e.budgetMu.Lock()
	defer e.budgetMu.Unlock()

	if e.tokenBudget <= 0 {
		return false // no budget set, never exhausted
	}

	e.tokensConsumed += inputTokens + outputTokens

	// Warn at 80%.
	threshold80 := e.tokenBudget * 80 / 100
	if e.tokensConsumed >= threshold80 && (e.tokensConsumed-inputTokens-outputTokens) < threshold80 {
		remaining := e.tokenBudget - e.tokensConsumed
		e.events <- engine.ParsedEvent{
			Type: "system_message",
			Data: &engine.SystemMessageEvent{
				Type:    "system_message",
				Content: fmt.Sprintf("Token budget warning: %d/%d consumed (%d remaining).", e.tokensConsumed, e.tokenBudget, remaining),
			},
		}
	}

	return e.tokensConsumed >= e.tokenBudget
}

// BudgetRemaining returns the remaining token budget, or -1 if no budget is set.
func (e *DirectEngine) BudgetRemaining() int {
	e.budgetMu.Lock()
	defer e.budgetMu.Unlock()
	if e.tokenBudget <= 0 {
		return -1
	}
	remaining := e.tokenBudget - e.tokensConsumed
	if remaining < 0 {
		remaining = 0
	}
	return remaining
}

// --- Per-turn context injection ---

// injectPerTurnContext adds dynamic per-turn context before each API call.
// Includes time since last message and active todo count as system-reminder blocks.
func (e *DirectEngine) injectPerTurnContext() {
	var parts []string

	// Time since session start.
	elapsed := time.Since(e.startTime)
	if elapsed > 1*time.Minute {
		parts = append(parts, fmt.Sprintf("Session uptime: %s", elapsed.Truncate(time.Second)))
	}

	// Active todo count.
	todos := e.todoTool.GetCurrentTodos()
	activeTodos := 0
	for _, t := range todos {
		if t.Status == "in_progress" || t.Status == "pending" {
			activeTodos++
		}
	}
	if activeTodos > 0 {
		parts = append(parts, fmt.Sprintf("Active todos: %d", activeTodos))
	}

	// File watcher: recent changes.
	if e.fileWatcher != nil {
		changes := e.fileWatcher.RecentChanges()
		if len(changes) > 0 {
			var changedFiles []string
			for _, c := range changes {
				if len(changedFiles) >= 3 {
					changedFiles = append(changedFiles, fmt.Sprintf("and %d more", len(changes)-3))
					break
				}
				changedFiles = append(changedFiles, c)
			}
			parts = append(parts, fmt.Sprintf("Recent file changes: %s", strings.Join(changedFiles, ", ")))
		}
	}

	// Team inbox polling: check for unread teammate messages.
	if e.teamStore != nil && e.teamName != "" && e.teamAgentID != "" {
		mailbox := teams.NewMailbox(e.teamStore)
		unread, err := mailbox.ReadUnread(e.teamName, e.teamAgentID)
		if err == nil && len(unread) > 0 {
			for _, msg := range unread {
				parts = append(parts, fmt.Sprintf("<teammate-message from=%q>%s</teammate-message>", msg.From, msg.Text))
			}
			_ = mailbox.MarkRead(e.teamName, e.teamAgentID)
		}
	}

	if len(parts) == 0 {
		return
	}

	// Inject as the last block in the system blocks (non-cacheable since it's dynamic).
	contextBlock := engine.SystemBlock{
		Text:      "<system-reminder>\n" + strings.Join(parts, "\n") + "\n</system-reminder>",
		Cacheable: false,
	}

	// Replace any previous per-turn context block (identified by prefix).
	replaced := false
	for i, b := range e.blocks {
		if strings.HasPrefix(b.Text, "<system-reminder>\nSession uptime:") || strings.HasPrefix(b.Text, "<system-reminder>\nActive todos:") {
			e.blocks[i] = contextBlock
			replaced = true
			break
		}
	}
	if !replaced {
		e.blocks = append(e.blocks, contextBlock)
	}
	e.system = engine.FlattenBlocks(e.blocks)
}

// --- Attachment injection ---

// injectPendingAttachments checks for pending context that should be injected
// between tool results and the next API call. This includes file change
// notifications and steered messages that arrived during tool execution.
func (e *DirectEngine) injectPendingAttachments() {
	// Drain any steered messages that arrived during tool execution.
	e.drainSteeredMessages()
}

// --- Content replacement tracking ---

// TrackContentReplacement records that a tool result's content was replaced
// (e.g. by compression or budget enforcement), storing the original for
// potential future restoration.
func (e *DirectEngine) TrackContentReplacement(toolUseID, originalContent string) {
	e.contentReplacementsMu.Lock()
	defer e.contentReplacementsMu.Unlock()
	e.contentReplacements[toolUseID] = originalContent
}

// GetReplacedContent returns the original content for a replaced tool result,
// or empty string if not tracked.
func (e *DirectEngine) GetReplacedContent(toolUseID string) (string, bool) {
	e.contentReplacementsMu.Lock()
	defer e.contentReplacementsMu.Unlock()
	content, ok := e.contentReplacements[toolUseID]
	return content, ok
}

// ContentReplacementCount returns the number of tracked replacements.
func (e *DirectEngine) ContentReplacementCount() int {
	e.contentReplacementsMu.Lock()
	defer e.contentReplacementsMu.Unlock()
	return len(e.contentReplacements)
}

// --- Message normalization ---

// normalizeToolResult cleans a tool result string before adding to history:
// trims trailing whitespace, caps at maxResultSize, and ensures valid UTF-8.
func normalizeToolResult(content string, maxResultSize int) string {
	// Trim trailing whitespace.
	content = strings.TrimRight(content, " \t\n\r")

	// Ensure valid UTF-8 by replacing invalid sequences.
	content = strings.ToValidUTF8(content, "\uFFFD")

	// Cap at maxResultSize.
	if maxResultSize > 0 && len(content) > maxResultSize {
		content = content[:maxResultSize] + "\n[truncated: exceeded " + fmt.Sprintf("%d", maxResultSize) + " bytes]"
	}

	return content
}

// maxToolResultSize is the ceiling for individual tool result content.
const maxToolResultSize = 512_000 // 512KB

// --- Image error classification ---

// classifyImageError checks if an API error is image-related and returns
// a user-friendly message with resize hints. Returns empty string if not
// an image error.
func classifyImageError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()

	if strings.Contains(msg, "image_too_large") || strings.Contains(msg, "ImageSizeError") {
		return "Image too large for the API. Resize to under 5MB or 8000x8000px before retrying."
	}
	if strings.Contains(msg, "invalid_image") || strings.Contains(msg, "ImageResizeError") {
		return "Image could not be processed. Ensure it is a valid PNG, JPEG, GIF, or WebP file."
	}
	if strings.Contains(msg, "image_resize") || strings.Contains(msg, "image dimensions") {
		return "Image dimensions exceed limits. Try resizing to 2048x2048 or smaller."
	}
	if strings.Contains(msg, "unsupported_image") || strings.Contains(msg, "image_content_type") {
		return "Unsupported image format. Use PNG, JPEG, GIF, or WebP."
	}

	return ""
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

// selectAmbientFrames picks at most 3 PNG frames out of the overlay ring
// buffer for vision attachment: the oldest (~30s ago) and the two most
// recent. This gives the model a temporal "before / now-1 / now" comparison
// while keeping per-turn vision token cost bounded (~2300 tokens at 768x768).
func selectAmbientFrames(pngs [][]byte) [][]byte {
	n := len(pngs)
	if n == 0 {
		return nil
	}
	if n <= 3 {
		return pngs
	}
	return [][]byte{pngs[0], pngs[n-2], pngs[n-1]}
}
