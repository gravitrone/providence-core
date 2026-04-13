package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"

	"github.com/charmbracelet/harmonica"
	"github.com/google/uuid"
	"github.com/gravitrone/providence-core/internal/auth"
	"github.com/gravitrone/providence-core/internal/config"
	"github.com/gravitrone/providence-core/internal/engine"
	_ "github.com/gravitrone/providence-core/internal/engine/claude"    // register claude factory
	_ "github.com/gravitrone/providence-core/internal/engine/codex_re" // register codex_re factory
	"github.com/gravitrone/providence-core/internal/engine/customtools"
	"github.com/gravitrone/providence-core/internal/engine/direct" // register direct factory + image types
	"github.com/gravitrone/providence-core/internal/engine/kairos"
	"github.com/gravitrone/providence-core/internal/engine/outputstyles"
	_ "github.com/gravitrone/providence-core/internal/engine/opencode" // register opencode factory
	"github.com/gravitrone/providence-core/internal/engine/skills"
	"github.com/gravitrone/providence-core/internal/engine/subagent"
	"github.com/gravitrone/providence-core/internal/store"
	"github.com/gravitrone/providence-core/internal/ui/components"
	"github.com/gravitrone/providence-core/internal/ui/dashboard"
	"github.com/gravitrone/providence-core/internal/ui/picker"
	"github.com/gravitrone/providence-core/internal/ui/tree"
)

// Tab constants for the tab system (replaces sidebar).
const (
	tabChat    = 0
	tabAgents  = 1
	tabTasks   = 2
	tabFiles   = 3
	tabTokens  = 4
	tabErrors  = 5
	tabCompact = 6
	tabHooks   = 7
	tabCount   = 8
)

var tabNames = []string{"Chat", "Agents", "Tasks", "Files", "Tokens", "Errors", "Compact", "Hooks"}

// completionSpring is a critically-damped spring for the completion cool-down animation.
// FPS(12) matches the flame tick rate (~80ms). Quick settle, no oscillation.
var completionSpring = harmonica.NewSpring(harmonica.FPS(12), 6.0, 0.8)

// queuedSpring is a slightly underdamped spring for the queued message hover effect.
// Gentle bounce that overshoots a bit, creating a "held in suspension" feel.
var queuedSpring = harmonica.NewSpring(harmonica.FPS(12), 5.0, 0.4)

// slashOpenSpring drives the slash command table entry/exit animation.
// Critically damped so it settles fast without bouncing.
var slashOpenSpring = harmonica.NewSpring(harmonica.FPS(12), 6.5, 0.9)

// slashPulseSpring drives the selected row breathing pulse. Slightly
// underdamped for a hover/heartbeat feel.
var slashPulseSpring = harmonica.NewSpring(harmonica.FPS(12), 5.0, 0.45)

// compactSpring drives the compaction indicator entry and cool-down.
// Same shape as the completion spring so the dissolve feels consistent.
var compactSpring = harmonica.NewSpring(harmonica.FPS(12), 6.0, 0.8)

// DoublePressTimeoutMS is the window (in ms) within which a second ctrl+c or
// ctrl+d press is interpreted as "exit". Outside this window the press resets.
const DoublePressTimeoutMS = 800

// doublePressResetMsg resets the pending double-press state after the timeout.
type doublePressResetMsg struct {
	Key string // "ctrl+c" or "ctrl+d"
}

// doublePressReset returns a tea.Cmd that fires after the timeout.
func doublePressReset(key string) tea.Cmd {
	return tea.Tick(time.Duration(DoublePressTimeoutMS)*time.Millisecond, func(t time.Time) tea.Msg {
		return doublePressResetMsg{Key: key}
	})
}

// --- Agent Tab Messages ---

// AgentEventMsg wraps a parsed event from the Claude headless session.
type AgentEventMsg struct {
	Event engine.ParsedEvent
}

// AgentErrorMsg signals a session-level error (failed to create session, etc).
type AgentErrorMsg struct {
	Err error
}

// authCompleteMsg signals that the OpenAI OAuth flow completed.
type authCompleteMsg struct {
	Success bool
	Message string
}

// compactTriggerMsg reports whether a manual compaction request started.
type compactTriggerMsg struct {
	AwaitEvents bool
	Err         error
}

// clipboardImageMsg carries image data read from the system clipboard.
type clipboardImageMsg struct {
	Data []byte
	Err  error
}

// --- Chat Message ---

// ChatMessage represents a single message in the agent chat history.
type ChatMessage struct {
	Role        string // "user", "assistant", "system", "permission", "tool", "thinking"
	Content     string
	Done        bool   // false while streaming
	ToolName    string // for tool/permission messages
	ToolArgs    string // brief tool input description
	ToolStatus  string // "pending", "success", "error", "cancelled"
	ToolBody    string // tool result body
	ToolResult  string // summary line shown after ⎿
	ToolPreview string // multi-line file preview content
	ToolOutput  string // actual tool output content (file content, bash output, etc)
	Expanded    bool   // whether this tool's result is shown expanded
	ImageCount  int    // number of images attached to this message
}

// --- Agent Tab ---

// slashCommand is a single entry in the slash command table.
type slashCommand struct {
	Name string
	Desc string
}

// slashCommands defines the available slash commands for the preview typeahead.
var slashCommands = []slashCommand{
	{"/model", "Switch model (Haiku, Sonnet, Opus, Codex)"},
	{"/engine", "Switch engine (claude, direct, codex_re)"},
	{"/image", "Attach image file (png, jpg, gif, webp)"},
	{"/theme", "Switch theme (flame, night, auto)"},
	{"/auth", "Login to OpenAI (Codex OAuth)"},
	{"/sessions", "List past sessions, or search with /sessions <query>"},
	{"/resume", "Resume a past session"},
	{"/compact", "Manually trigger context compaction"},
	{"/rewind", "Rewind to a previous user message"},
	{"/dashboard", "Toggle dashboard panel (or: pin, hide)"},
	{"/tree", "Toggle conversation tree view"},
	{"/clear", "Clear chat history"},
	{"/kairos", "Toggle kairos autonomous mode"},
	{"/cost", "Show token usage and context window"},
	{"/doctor", "Health check (Go, OS, API keys, engine)"},
	{"/stats", "Session statistics (messages, tokens)"},
	{"/effort", "Set effort level (low, medium, high)"},
	{"/rename", "Rename current session"},
	{"/skills", "List discovered skills"},
	{"/agents", "List built-in agent types"},
	{"/permissions", "Show current permission mode"},
	{"/hooks", "Show hook configuration info"},
	{"/diff", "Show git diff --stat"},
	{"/branch", "Show git branches"},
	{"/share", "Export session as JSONL"},
	{"/review", "Spawn code review agent"},
	{"/fork", "Fork N background agents from current context"},
	{"/init", "Create CLAUDE.md in project root with detected project info"},
	{"/help", "Show available commands"},
}

// isKnownSlashCommand returns true if the given token (e.g. "/resume")
// exactly matches a registered slash command name.
func isKnownSlashCommand(token string) bool {
	lower := strings.ToLower(token)
	for _, c := range slashCommands {
		if c.Name == lower {
			return true
		}
	}
	return false
}

// QueuedMessage represents a single message in the queue with its steering state.
type QueuedMessage struct {
	Text    string
	Steered bool // true = priority, sends first when turn finishes
}

// AgentTab implements the TabModel interface for the agent chat UI.
type AgentTab struct {
	width, height int
	input         textarea.Model
	viewport      viewport.Model
	messages      []ChatMessage
	engine        engine.Engine
	engineType    engine.EngineType
	streaming     bool
	streamBuffer  string
	pendingPerm   *engine.PermissionRequestEvent
	mdRenderer    *glamour.TermRenderer
	follow        bool
	model         string
	currentTokens int
	compacting    bool
	flameFrame    int
	// Spinner state.
	spinnerFrame    int
	spinnerVerb     string
	spinnerStart    time.Time
	spinnerLastVerb time.Time
	// Completion spring animation state (harmonica-driven).
	completionActive bool
	completionBright float64 // 1.0 = bright gold, 0.0 = frozen ember
	completionVel    float64
	completionText   string

	// Message queue: submitted while streaming, auto-sent on result.
	// Each message can be individually selected, steered (priority), or removed.
	queue       []QueuedMessage
	queueCursor int // Which message is highlighted (-1 = none, back to input).
	// Cached rendered messages to avoid re-rendering on every tick.
	cachedMessages string
	messagesDirty  bool

	// Viz state: tracks when a viz block is being streamed.
	visualizing bool
	vizVerb     string
	vizCount    int // number of completed viz blocks this turn

	// Queued message spring animation state (harmonica-driven hover bounce).
	queuedBright float64
	queuedVel    float64

	// Streaming tool input: accumulates partial JSON from tool_input_delta events.
	toolInputBuffer string

	// Tool expansion: per-message toggle via freeze mode.
	toolsExpanded map[int]bool

	// Focus arbiter: controls which sub-model receives key events.
	focus Focus
	// Transcript virtual scroll model.
	transcript TranscriptModel

	// Pending image attachments for next message.
	pendingImages []ImageAttachment

	// Persisted user config.
	cfg config.Config

	// Session persistence.
	store     *store.Store
	sessionID string

	// Slash command table state (harmonica-driven).
	// slashCursor is the highlighted row in the filtered match list.
	// -1 means no explicit selection (user is still typing).
	slashCursor    int
	slashOpen      float64 // 0.0 = closed, 1.0 = fully open
	slashOpenVel   float64
	slashPulse     float64 // 0.0..1.0 breathing on the selected row
	slashPulseVel  float64
	// slashMatchCount is the number of rows rendered on the last frame,
	// used to clamp slashCursor when the user edits the input.
	slashMatchCount int

	// Double-press state for ctrl+c / ctrl+d exit pattern.
	// First press interrupts or starts the timer, second press within
	// DoublePressTimeoutMS exits the app.
	ctrlCLastPress time.Time
	ctrlCPending   bool
	ctrlDLastPress time.Time
	ctrlDPending   bool

	// Compact indicator state.
	// compactPhase tracks the active compaction lifecycle: "" (inactive),
	// "running", "complete", "failed". "complete" and "failed" stay briefly
	// while the dissolve spring runs so the user sees the terminal frame.
	compactPhase        string
	compactStart        time.Time
	compactTokensBefore int
	compactTokensAfter  int
	compactErrMsg       string
	compactVerb         string
	compactVerbAt       time.Time
	compactSettledAt    time.Time
	// compactBright drives the dissolve animation (1.0 bright -> 0.0 frozen).
	compactBright float64
	compactVel    float64

	// Rewind picker state.
	rewindModel components.RewindModel

	// Quote/reply selection state.
	quoteModel components.QuoteModel

	// Tree view state.
	treeViewOpen bool

	// Tab system (replaces sidebar).
	tab          int
	tabNav       bool
	tabSpring    harmonica.Spring
	tabIndicator float64
	tabIndVel    float64
	tabIndTarget float64
	dashboard    dashboard.DashboardModel

	// Context portability: pending state to restore after engine switch.
	pendingPortableState *engine.ConversationState

	// Kairos autonomous mode state.
	kairos *kairos.State

	// Notification toast system.
	notifications NotificationModel

	// Track which background subagent IDs we already notified about.
	notifiedAgents map[string]bool

	// Permission "always allow" set - tool names that the user chose to auto-approve.
	alwaysAllowTools map[string]bool

	// permissionMode is the active permission mode (default, acceptEdits, plan, bypassPermissions, dontAsk).
	permissionMode string

	// Discovered skills, custom agents, and custom tools loaded at startup.
	discoveredSkills []skills.SkillDefinition
	customAgents     map[string]subagent.AgentType
	customTools      []customtools.CustomTool

	// File picker (@-mention autocomplete).
	filePicker picker.FilePickerModel

	// Keybinding overrides from ~/.providence/keybindings.json.
	keybindings *KeybindingsConfig

	// Auto-title: generate session title from first user message.
	autoTitleGenerated bool

	// Input history: up-arrow recalls previous submissions.
	inputHistory         []string
	inputHistoryIdx      int
	inputHistoryBrowsing bool
}

// NewAgentTab creates and returns a new AgentTab.
// engineType overrides the default engine; pass "" for the default (claude).
func NewAgentTab(engineType engine.EngineType, cfg config.Config, st *store.Store) AgentTab {
	if engineType == "" {
		engineType = engine.EngineTypeClaude
	}
	placeholders := []string{
		"Speak to the Profaned...",
		"Command the flame...",
		"The goddess awaits...",
		"Invoke Providence...",
		"Channel the holy fire...",
		"Summon divine judgment...",
		"The Profaned Core listens...",
		"Ignite your will...",
	}
	ti := components.NewProvidenceTextArea(placeholders[rand.IntN(len(placeholders))])
	ti.Focus()

	vp := components.NewProvidenceViewport(80, 20)

	mr, _ := glamour.NewTermRenderer(
		glamour.WithStyles(providenceGlamourStyle()),
		glamour.WithWordWrap(76),
	)

	model := cfg.Model

	// First-run onboarding: show welcome message and create .providence/ dir.
	var initialMessages []ChatMessage
	home, _ := os.UserHomeDir()
	if IsFirstRun(home) {
		initialMessages = append(initialMessages, ChatMessage{
			Role: "system",
			Content: "Welcome to Providence! The Profaned Core awaits.\n\n" +
				"Quick start:\n" +
				"  /model  - switch models\n" +
				"  /engine - switch engines\n" +
				"  /help   - see all commands\n" +
				"  /theme  - change theme\n\n" +
				"Type a message to begin.",
			Done: true,
		})
		os.MkdirAll(filepath.Join(home, ".providence"), 0755)
	}

	// Discover skills, custom agents, and custom tools at startup.
	cwd, _ := os.Getwd()
	discoveredSkills, _ := skills.LoadSkills(cwd, home)
	customAgents, _ := subagent.LoadCustomAgents(cwd, home)
	customTools, _ := customtools.LoadCustomTools(cwd, home)

	// Load keybinding overrides.
	keybindings, _ := LoadKeybindings(home)

	// Initialize file picker for @-mention autocomplete.
	fp := picker.NewFilePickerModel(cwd, 80)

	return AgentTab{
		input:            ti,
		viewport:         vp,
		messages:         initialMessages,
		follow:           true,
		mdRenderer:       mr,
		queueCursor:      -1,
		slashCursor:      -1,
		engineType:       engineType,
		model:            model,
		cfg:              cfg,
		store:            st,
		focus:            FocusInput,
		transcript:       NewTranscriptModel(),
		tab:       tabChat,
		tabSpring: harmonica.NewSpring(harmonica.FPS(60), 10.0, 1.0),
		dashboard:        dashboard.New(),
		kairos:           kairos.New(),
		discoveredSkills: discoveredSkills,
		customAgents:     customAgents,
		customTools:      customTools,
		filePicker:       fp,
		keybindings:      keybindings,
		toolsExpanded:    make(map[int]bool),
	}
}

// Init implements TabModel.
func (at AgentTab) Init() tea.Cmd {
	return tea.Batch(flameTick(), at.filePicker.Init())
}

// Resize updates the tab dimensions and recreates the glamour renderer.
func (at *AgentTab) Resize(width, height int) {
	at.width = width
	at.height = height

	contentW := chatContentWidth(width)
	inputH := 3
	dividerH := 1
	vpH := height - inputH - dividerH - 1
	if vpH < 3 {
		vpH = 3
	}

	at.viewport.SetWidth(contentW)
	at.viewport.SetHeight(vpH)
	at.input.SetWidth(contentW - 4)
	at.input.SetHeight(inputH)

	// Word wrap width accounts for the "↳ " prefix (2 chars).
	wrapW := contentW - 4
	if wrapW < 40 {
		wrapW = 40
	}
	at.mdRenderer, _ = glamour.NewTermRenderer(
		glamour.WithStyles(providenceGlamourStyle()),
		glamour.WithWordWrap(wrapW),
	)

	at.refreshViewport()
}

// Update handles incoming messages and returns the updated AgentTab.
func (at AgentTab) Update(msg tea.Msg) (AgentTab, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		at.Resize(msg.Width, msg.Height)
		return at, nil

	case picker.FilesLoadedMsg, picker.FilesRefreshMsg:
		var cmd tea.Cmd
		at.filePicker, cmd = at.filePicker.Update(msg)
		return at, cmd

	case tea.KeyPressMsg:
		return at.handleKey(msg)

	case flameTickMsg:
		// Always tick the flame frame - banner and empty state animate even when idle.
		at.flameFrame++
		// Spring-driven completion cool-down: brightness 1.0 -> 0.0 via harmonica.
		if at.completionActive {
			at.completionBright, at.completionVel = completionSpring.Update(
				at.completionBright, at.completionVel, 0.0,
			)
			if math.Abs(at.completionBright) < 0.01 {
				at.completionActive = false
				at.completionBright = 0.0
			}
		}
		// Queued message spring: oscillates between 0.0 and 1.0 for breathing effect.
		// Target alternates based on frame to keep the spring bouncing.
		if len(at.queue) > 0 {
			// Use a slow sine wave to set the target, spring chases it with bounce.
			target := (math.Sin(float64(at.flameFrame)*0.15) + 1.0) / 2.0
			at.queuedBright, at.queuedVel = queuedSpring.Update(
				at.queuedBright, at.queuedVel, target,
			)
		}
		// Slash command table: spring the panel open when the input starts
		// with "/", spring it closed otherwise. Selected-row pulse is a
		// slow sine the spring chases, giving a gentle heartbeat.
		if strings.HasPrefix(at.input.Value(), "/") {
			at.slashOpen, at.slashOpenVel = slashOpenSpring.Update(
				at.slashOpen, at.slashOpenVel, 1.0,
			)
			target := (math.Sin(float64(at.flameFrame)*0.18) + 1.0) / 2.0
			at.slashPulse, at.slashPulseVel = slashPulseSpring.Update(
				at.slashPulse, at.slashPulseVel, target,
			)
		} else if at.slashOpen > 0.0 {
			at.slashOpen, at.slashOpenVel = slashOpenSpring.Update(
				at.slashOpen, at.slashOpenVel, 0.0,
			)
			if math.Abs(at.slashOpen) < 0.01 {
				at.slashOpen = 0.0
				at.slashOpenVel = 0.0
				at.slashCursor = -1
			}
		}
		// Compaction indicator springs. Running: bright -> settle near 1.0
		// with a breathing target. Complete/failed: dissolve to 0.0 then
		// clear the phase after a short hold.
		switch at.compactPhase {
		case "running":
			target := 0.85 + 0.15*(math.Sin(float64(at.flameFrame)*0.18)+1.0)/2.0
			at.compactBright, at.compactVel = compactSpring.Update(
				at.compactBright, at.compactVel, target,
			)
			// Rotate the verb every ~8s to keep the flavor fresh.
			if time.Since(at.compactVerbAt) >= 8*time.Second {
				at.compactVerb = randomCompactVerb(at.compactVerb)
				at.compactVerbAt = time.Now()
			}
		case "complete", "failed":
			at.compactBright, at.compactVel = compactSpring.Update(
				at.compactBright, at.compactVel, 0.0,
			)
			if math.Abs(at.compactBright) < 0.02 && time.Since(at.compactSettledAt) >= 2*time.Second {
				at.compactPhase = ""
				at.compactBright = 0.0
				at.compactVel = 0.0
			}
		}
		// Tab indicator spring animation.
		at.tabIndicator, at.tabIndVel = at.tabSpring.Update(at.tabIndicator, at.tabIndVel, at.tabIndTarget)
		at.notifications.Tick()
		// Poll for completed background subagents and inject notifications.
		at.drainCompletedSubagents()
		at.refreshViewport()
		return at, flameTick()

	case AgentEventMsg:
		return at.handleAgentEvent(msg)

	case AgentErrorMsg:
		at.addSystemMessage(fmt.Sprintf("error: %s", msg.Err))
		at.streaming = false
		at.refreshViewport()
		return at, nil

	case spinnerTickMsg:
		if at.streaming {
			at.spinnerFrame++
			// Rotate verb every 30s.
			if time.Since(at.spinnerLastVerb) >= 30*time.Second {
				at.spinnerVerb = randomVerb(at.spinnerVerb)
				at.spinnerLastVerb = time.Now()
			}
			at.refreshViewport()
			return at, spinnerTick()
		}
		return at, nil

	case engineCreatedMsg:
		cmd := at.handleEngineCreated(msg)
		return at, cmd

	case engineRestoredMsg:
		if msg.err != nil {
			at.addSystemMessage("Resume error: " + msg.err.Error())
			at.refreshViewport()
			return at, nil
		}
		at.engine = msg.engine
		at.currentTokens = 0
		at.compacting = false
		at.transferImagesToEngine()
		// No Send here - engine waits for the next user turn. Start the event
		// pump anyway so system init / later events are drained.
		return at, at.safeWaitForEvent()

	case authCompleteMsg:
		at.addSystemMessage(msg.Message)
		at.refreshViewport()
		return at, nil

	case compactTriggerMsg:
		if msg.Err != nil {
			at.addSystemMessage("Compaction error: " + msg.Err.Error())
			at.refreshViewport()
			return at, nil
		}
		if msg.AwaitEvents {
			return at, at.safeWaitForEvent()
		}
		at.addSystemMessage("Manual compaction is only available on the direct engine")
		at.refreshViewport()
		return at, nil

	case clipboardImageMsg:
		if msg.Err != nil {
			at.addSystemMessage("Clipboard: " + msg.Err.Error())
			at.refreshViewport()
			return at, nil
		}
		img := ImageAttachment{
			Name:      fmt.Sprintf("clipboard_%d.png", time.Now().Unix()),
			MediaType: "image/png",
			Data:      msg.Data,
			Size:      int64(len(msg.Data)),
		}
		at.pendingImages = append(at.pendingImages, img)
		at.addSystemMessage(fmt.Sprintf("Image attached: %s (%s)", img.Name, formatSize(img.Size)))
		at.refreshViewport()
		return at, nil

	case doublePressResetMsg:
		switch msg.Key {
		case "ctrl+c":
			at.ctrlCPending = false
		case "ctrl+d":
			at.ctrlDPending = false
		}
		at.refreshViewport()
		return at, nil
	}

	// Forward to input.
	var cmd tea.Cmd
	at.input, cmd = at.input.Update(msg)
	return at, cmd
}

func (at AgentTab) handleKey(msg tea.KeyPressMsg) (AgentTab, tea.Cmd) {
	key := msg.String()

	// Permission prompt takes priority.
	if at.pendingPerm != nil {
		switch key {
		case "y":
			perm := at.pendingPerm
			at.pendingPerm = nil
			optionID := ""
			for _, opt := range perm.Options {
				if strings.Contains(strings.ToLower(opt.Label), "allow") ||
					strings.Contains(strings.ToLower(opt.ID), "allow") ||
					opt.ID == "yes" {
					optionID = opt.ID
					break
				}
			}
			if optionID == "" && len(perm.Options) > 0 {
				optionID = perm.Options[0].ID
			}
			if at.engine != nil && optionID != "" {
				_ = at.engine.RespondPermission(perm.QuestionID, optionID)
			}
			// Update the permission message status to success
			at.updateLastPermissionStatus("success")
			at.refreshViewport()
			return at, at.safeWaitForEvent()
		case "n":
			perm := at.pendingPerm
			at.pendingPerm = nil
			optionID := ""
			for _, opt := range perm.Options {
				if strings.Contains(strings.ToLower(opt.Label), "deny") ||
					strings.Contains(strings.ToLower(opt.ID), "deny") ||
					opt.ID == "no" {
					optionID = opt.ID
					break
				}
			}
			if optionID == "" && len(perm.Options) > 1 {
				optionID = perm.Options[len(perm.Options)-1].ID
			}
			if at.engine != nil && optionID != "" {
				_ = at.engine.RespondPermission(perm.QuestionID, optionID)
			}
			at.updateLastPermissionStatus("cancelled")
			at.refreshViewport()
			return at, at.safeWaitForEvent()
		case "a":
			// Always allow this tool for the rest of the session.
			perm := at.pendingPerm
			at.pendingPerm = nil
			if at.alwaysAllowTools == nil {
				at.alwaysAllowTools = make(map[string]bool)
			}
			at.alwaysAllowTools[perm.Tool.Name] = true
			optionID := ""
			for _, opt := range perm.Options {
				if strings.Contains(strings.ToLower(opt.Label), "allow") ||
					strings.Contains(strings.ToLower(opt.ID), "allow") ||
					opt.ID == "yes" {
					optionID = opt.ID
					break
				}
			}
			if optionID == "" && len(perm.Options) > 0 {
				optionID = perm.Options[0].ID
			}
			if at.engine != nil && optionID != "" {
				_ = at.engine.RespondPermission(perm.QuestionID, optionID)
			}
			at.updateLastPermissionStatus("success")
			at.addSystemMessage(fmt.Sprintf("Always allowing %s for this session", perm.Tool.Name))
			at.refreshViewport()
			return at, at.safeWaitForEvent()
		}
		return at, nil
	}

	// Rewind picker takes priority when active.
	if at.rewindModel.Active() {
		var rewindMsg *components.RewindMsg
		var handled bool
		at.rewindModel, rewindMsg, handled = at.rewindModel.HandleKey(key)
		if rewindMsg != nil {
			switch rewindMsg.Action {
			case components.RewindRestore:
				// Slice messages at the selected index.
				if rewindMsg.Index >= 0 && rewindMsg.Index < len(at.messages) {
					// Keep the user message text for re-populating input.
					userText := at.messages[rewindMsg.Index].Content
					at.messages = at.messages[:rewindMsg.Index]
					at.input.SetValue(userText)
					at.input.CursorEnd()
					at.messagesDirty = true
					// Kill engine so next send creates fresh session.
					if at.engine != nil {
						at.engine.Close()
						at.engine = nil
					}
					at.addSystemMessage(fmt.Sprintf("Rewound to message %d", rewindMsg.Index+1))
				}
			case components.RewindSummarize:
				at.addSystemMessage(fmt.Sprintf("Summarize from message %d (not yet implemented)", rewindMsg.Index+1))
			case components.RewindCancel:
				// Nothing to do.
			}
			at.refreshViewport()
			return at, nil
		}
		if handled {
			at.refreshViewport()
			return at, nil
		}
	}

	// Quote/reply mode takes priority when active.
	if at.quoteModel.Active() {
		quoted, block := at.quoteModel.HandleKey(key)
		if quoted {
			// Prepend quote block to current input.
			cur := at.input.Value()
			at.input.SetValue(block + cur)
			at.input.CursorEnd()
			at.focus = FocusInput
		}
		if !at.quoteModel.Active() {
			at.focus = FocusInput
		}
		at.refreshViewport()
		return at, nil
	}

	// File picker (@-mention) takes priority when active.
	if at.filePicker.Active() {
		switch key {
		case "up", "down", "esc":
			at.filePicker.HandleKey(key)
			at.refreshViewport()
			return at, nil
		case "tab", "enter":
			accepted, replacement := at.filePicker.HandleKey(key)
			if accepted {
				// Replace @query with the selected file path.
				inputText := at.input.Value()
				start := at.filePicker.TokenStart()
				at.input.SetValue(inputText[:start] + replacement)
				at.input.CursorEnd()
			}
			at.refreshViewport()
			return at, nil
		}
	}

	// Number keys switch tabs when input is empty.
	if at.input.Value() == "" && len(key) == 1 && key[0] >= '1' && key[0] <= '8' {
		newTab := int(key[0] - '1')
		if newTab < tabCount {
			at.switchTab(newTab)
			return at, nil
		}
	}

	// Escape returns to chat tab from any other tab.
	if key == "esc" && at.tab != tabChat {
		at.switchTab(tabChat)
		return at, nil
	}

	// Slash table navigation takes priority when the table is visible.
	// The user is clearly browsing commands at that point, so up/down
	// should move within the table instead of scrolling the viewport
	// or navigating the message queue.
	if strings.HasPrefix(at.input.Value(), "/") {
		matches := at.matchingSlashCommands()
		if len(matches) > 0 {
			switch key {
			case "up":
				if at.slashCursor <= 0 {
					at.slashCursor = len(matches) - 1
				} else {
					at.slashCursor--
				}
				at.refreshViewport()
				return at, nil
			case "down":
				if at.slashCursor < 0 || at.slashCursor >= len(matches)-1 {
					at.slashCursor = 0
				} else {
					at.slashCursor++
				}
				at.refreshViewport()
				return at, nil
			case "esc":
				// Dismiss the table: clear the input so the panel springs
				// back closed. Preserves the existing esc-from-queue path
				// below because we early-return only when the input starts
				// with "/".
				at.input.SetValue("")
				at.slashCursor = -1
				at.refreshViewport()
				return at, nil
			}
		}
	}

	// Focus-based routing: when transcript is focused (freeze mode),
	// handle keys there instead of the normal input path.
	if at.focus == FocusTranscript {
		return at.handleTranscriptKey(key)
	}

	switch key {
	case "ctrl+c":
		now := time.Now()
		if at.ctrlCPending && now.Sub(at.ctrlCLastPress) < time.Duration(DoublePressTimeoutMS)*time.Millisecond {
			// Second press within window - exit.
			return at, tea.Quit
		}
		// First press: interrupt if streaming, then start the window.
		if at.streaming && at.engine != nil {
			at.engine.Interrupt()
		}
		at.ctrlCPending = true
		at.ctrlCLastPress = now
		if !at.streaming {
			at.addSystemMessage("Press ctrl+c again to exit")
		}
		at.refreshViewport()
		return at, doublePressReset("ctrl+c")

	case "ctrl+d":
		now := time.Now()
		if at.ctrlDPending && now.Sub(at.ctrlDLastPress) < time.Duration(DoublePressTimeoutMS)*time.Millisecond {
			return at, tea.Quit
		}
		if at.streaming && at.engine != nil {
			at.engine.Interrupt()
		}
		at.ctrlDPending = true
		at.ctrlDLastPress = now
		if !at.streaming {
			at.addSystemMessage("Press ctrl+d again to exit")
		}
		at.refreshViewport()
		return at, doublePressReset("ctrl+d")

	case "up", "pgup":
		// If input is empty and queue has messages, enter queue navigation.
		if strings.TrimSpace(at.input.Value()) == "" && len(at.queue) > 0 {
			if at.queueCursor < 0 {
				// Enter queue from input: select last message.
				at.queueCursor = len(at.queue) - 1
				at.refreshViewport()
				return at, nil
			}
			if at.queueCursor > 0 {
				// Move up within queue.
				at.queueCursor--
				at.refreshViewport()
				return at, nil
			}
			// Already at top of queue, scroll viewport.
			at.queueCursor = -1
		}
		// Input history: recall previous submissions when input is empty.
		if strings.TrimSpace(at.input.Value()) == "" && len(at.inputHistory) > 0 && len(at.queue) == 0 {
			if !at.inputHistoryBrowsing {
				at.inputHistoryBrowsing = true
				at.inputHistoryIdx = len(at.inputHistory) - 1
			} else if at.inputHistoryIdx > 0 {
				at.inputHistoryIdx--
			}
			at.input.SetValue(at.inputHistory[at.inputHistoryIdx])
			at.input.CursorEnd()
			return at, nil
		}
		at.follow = false
		var cmd tea.Cmd
		at.viewport, cmd = at.viewport.Update(msg)
		return at, cmd
	case "down", "pgdown":
		if at.queueCursor >= 0 {
			if at.queueCursor < len(at.queue)-1 {
				// Move down within queue.
				at.queueCursor++
			} else {
				// Past last message: exit queue, back to input.
				at.queueCursor = -1
			}
			at.refreshViewport()
			return at, nil
		}
		// Input history: navigate forward or exit history browsing.
		if at.inputHistoryBrowsing {
			at.inputHistoryIdx++
			if at.inputHistoryIdx >= len(at.inputHistory) {
				at.inputHistoryBrowsing = false
				at.input.Reset()
			} else {
				at.input.SetValue(at.inputHistory[at.inputHistoryIdx])
				at.input.CursorEnd()
			}
			return at, nil
		}
		var cmd tea.Cmd
		at.viewport, cmd = at.viewport.Update(msg)
		if at.viewport.AtBottom() {
			at.follow = true
		}
		return at, cmd
	case "shift+enter":
		// Insert newline in textarea (multiline editing).
		var cmd tea.Cmd
		at.input, cmd = at.input.Update(msg)
		return at, cmd

	case "enter":
		// If navigating queue, steer the selected message.
		if at.queueCursor >= 0 && at.queueCursor < len(at.queue) {
			if !at.queue[at.queueCursor].Steered {
				at.queue[at.queueCursor].Steered = true
			}
			at.refreshViewport()
			return at, nil
		}

		text := strings.TrimSpace(at.input.Value())
		// Slash command fast path: when the text starts with "/" and the
		// first token exactly matches a known command, fire it immediately
		// without routing through the streaming/queue path. This fixes the
		// race where the slash table was visible and the first enter was
		// ignored - the command now runs on the first keystroke regardless
		// of queue/streaming state.
		if text != "" && strings.HasPrefix(text, "/") {
			head := strings.SplitN(text, " ", 2)[0]
			if isKnownSlashCommand(head) {
				at.input.SetValue("")
				at.slashCursor = -1
				handled, cmd := at.handleSlashCommand(text)
				if handled {
					return at, cmd
				}
			}
		}
		// If the slash table is visible and the user navigated with arrows
		// (slashCursor >= 0), enter commits the highlighted command.
		if at.slashCursor >= 0 && strings.HasPrefix(at.input.Value(), "/") {
			matches := at.matchingSlashCommands()
			if len(matches) > 0 {
				idx := at.slashCursor
				if idx >= len(matches) {
					idx = len(matches) - 1
				}
				selected := matches[idx].Name
				at.input.SetValue("")
				at.slashCursor = -1
				handled, cmd := at.handleSlashCommand(selected)
				if handled {
					return at, cmd
				}
			}
		}
		if text == "" {
			return at, nil
		}
		// Push to input history (skip slash commands).
		if !strings.HasPrefix(text, "/") {
			at.pushHistory(text)
		}
		if at.streaming {
			// Queue the message - it will auto-send when the current turn finishes.
			at.queue = append(at.queue, QueuedMessage{Text: text, Steered: false})
			at.queuedBright = 0.5
			at.queuedVel = 0.0
			at.input.SetValue("")
			at.refreshViewport()
			return at, nil
		}
		if strings.HasPrefix(text, "/") {
			at.input.SetValue("")
			handled, cmd := at.handleSlashCommand(text)
			if handled {
				return at, cmd
			}
		}
		at.input.SetValue("")
		// Create session on first real send.
		if at.sessionID == "" && at.store != nil {
			at.sessionID = uuid.New().String()
			cwd, _ := os.Getwd()
			at.store.CreateSession(at.sessionID, cwd, string(at.engineType), at.model)
		}
		// Auto-title the session from the first user message.
		at.generateAutoTitle()
		imgCount := len(at.pendingImages)
		at.messages = append(at.messages, ChatMessage{
			Role:       "user",
			Content:    text,
			Done:       true,
			ImageCount: imgCount,
		})
		at.messagesDirty = true
		at.persistLastMessage()
		at.streaming = true
		at.streamBuffer = ""
		at.follow = true
		at.viewport.GotoBottom()
		// Initialize spinner state
		at.spinnerFrame = 0
		at.spinnerVerb = randomVerb("")
		at.spinnerStart = time.Now()
		at.spinnerLastVerb = time.Now()
		at.refreshViewport()

		// Create session on first use.
		if at.engine == nil {
			// Images will be transferred after engine creation via handleEngineCreated.
			return at, tea.Batch(createEngineAndSend(text, at.model, at.engineType, at.cfg.OutputStyle), spinnerTick())
		}

		// Transfer images to engine before sending.
		at.transferImagesToEngine()

		// Track user activity for kairos focus detection.
		if at.kairos != nil {
			at.kairos.RecordUserMessage()
		}

		// Send to existing session.
		if err := at.engine.Send(text); err != nil {
			at.addSystemMessage(fmt.Sprintf("send error: %s", err))
			at.streaming = false
			at.refreshViewport()
			return at, nil
		}
		return at, tea.Batch(at.safeWaitForEvent(), spinnerTick())

	case "backspace", "delete":
		// Remove selected message from queue.
		if at.queueCursor >= 0 && at.queueCursor < len(at.queue) {
			at.queue = append(at.queue[:at.queueCursor], at.queue[at.queueCursor+1:]...)
			if len(at.queue) == 0 {
				at.queueCursor = -1
			} else if at.queueCursor >= len(at.queue) {
				at.queueCursor = len(at.queue) - 1
			}
			at.refreshViewport()
			return at, nil
		}
		// Not in queue - forward to input.
		var cmd tea.Cmd
		at.input, cmd = at.input.Update(msg)
		// Re-check file picker after backspace changes input.
		at.filePicker.HandleInput(at.input.Value())
		return at, cmd

	case "ctrl+i":
		// Paste image from clipboard (macOS only).
		return at, func() tea.Msg {
			data, err := readClipboardImage()
			return clipboardImageMsg{Data: data, Err: err}
		}

	case "ctrl+o":
		// Enter transcript freeze mode.
		at.transcript.SetMessages(at.messages)
		at.transcript.SetViewport(chatContentWidth(at.width), at.height)
		at.transcript.SetFrozen(true)
		at.focus = FocusTranscript
		at.messagesDirty = true
		at.refreshViewport()
		return at, nil

	case "shift+up":
		// Enter quote/reply mode - navigate messages to quote.
		if len(at.messages) > 0 {
			var quoteMessages []components.QuoteMessage
			for _, m := range at.messages {
				if m.Role == "user" || m.Role == "assistant" {
					quoteMessages = append(quoteMessages, components.QuoteMessage{
						Role:    m.Role,
						Content: m.Content,
					})
				}
			}
			if len(quoteMessages) > 0 {
				at.quoteModel.Enter(quoteMessages)
				at.focus = FocusTranscript
				at.refreshViewport()
			}
		}
		return at, nil

	case "ctrl+l":
		if !at.streaming {
			if at.store != nil && at.sessionID != "" {
				at.store.DeleteSession(at.sessionID)
			}
			at.sessionID = ""
			at.messages = nil
			at.streamBuffer = ""
			at.pendingPerm = nil
			at.messagesDirty = true
			at.refreshViewport()
		}
		return at, nil

	case "esc":
		// Exit queue selection without clearing queue.
		if at.queueCursor >= 0 {
			at.queueCursor = -1
			at.refreshViewport()
			return at, nil
		}
		return at, nil

	case "ctrl+backspace", "super+backspace", "ctrl+u":
		// Delete entire input line.
		at.input.SetValue("")
		return at, nil

	case "tab":
		// Autocomplete slash command.
		val := at.input.Value()
		if strings.HasPrefix(val, "/") {
			prefix := strings.ToLower(val)
			for _, cmd := range slashCommands {
				if strings.HasPrefix(cmd.Name, prefix) && cmd.Name != prefix {
					at.input.SetValue(cmd.Name + " ")
					at.input.CursorEnd()
					return at, nil
				}
			}
		}
		return at, nil

	default:
		var cmd tea.Cmd
		at.input, cmd = at.input.Update(msg)
		// After input changes, check for @ trigger to activate file picker.
		at.filePicker.HandleInput(at.input.Value())
		return at, cmd
	}
}

// handleTranscriptKey handles keyboard input when the transcript is frozen
// (FocusTranscript). j/k scroll, PgUp/PgDown page, / to search, n/N for
// next/prev match, q/esc/ctrl+o to exit freeze mode.
func (at AgentTab) handleTranscriptKey(key string) (AgentTab, tea.Cmd) {
	// When the search input is active, most keys go to the search query.
	if at.transcript.SearchActive() {
		switch key {
		case "esc":
			at.transcript.SetSearchActive(false)
			at.messagesDirty = true
			at.refreshViewport()
			return at, nil
		case "enter":
			// Confirm search, exit search input but stay frozen.
			at.transcript.SetSearchActive(false)
			at.messagesDirty = true
			at.refreshViewport()
			return at, nil
		case "backspace":
			q := at.transcript.SearchQuery()
			if len(q) > 0 {
				at.transcript.SetSearchQuery(q[:len(q)-1])
			}
			at.messagesDirty = true
			at.refreshViewport()
			return at, nil
		default:
			// Append printable characters to the search query.
			if len(key) == 1 && key[0] >= 32 && key[0] < 127 {
				at.transcript.SetSearchQuery(at.transcript.SearchQuery() + key)
				at.messagesDirty = true
				at.refreshViewport()
			}
			return at, nil
		}
	}

	switch key {
	case "j", "down":
		at.transcript.ScrollBy(1)
		at.follow = false
		at.messagesDirty = true
		at.refreshViewport()
		return at, nil

	case "k", "up":
		at.transcript.ScrollBy(-1)
		at.follow = false
		at.messagesDirty = true
		at.refreshViewport()
		return at, nil

	case "pgdown", "ctrl+f":
		at.transcript.ScrollBy(at.transcript.viewportH)
		at.follow = false
		at.messagesDirty = true
		at.refreshViewport()
		return at, nil

	case "pgup", "ctrl+b":
		at.transcript.ScrollBy(-at.transcript.viewportH)
		at.follow = false
		at.messagesDirty = true
		at.refreshViewport()
		return at, nil

	case "G":
		at.transcript.ScrollToBottom()
		at.follow = true
		at.messagesDirty = true
		at.refreshViewport()
		return at, nil

	case "g":
		at.transcript.scrollTop = 0
		at.transcript.sticky = false
		at.follow = false
		at.messagesDirty = true
		at.refreshViewport()
		return at, nil

	case "/":
		at.transcript.SetSearchActive(true)
		at.transcript.SetSearchQuery("")
		at.messagesDirty = true
		at.refreshViewport()
		return at, nil

	case "n":
		at.transcript.SearchNext()
		at.messagesDirty = true
		at.refreshViewport()
		return at, nil

	case "N":
		at.transcript.SearchPrev()
		at.messagesDirty = true
		at.refreshViewport()
		return at, nil

	case "e":
		// Toggle expand/collapse for all tool messages.
		hasAny := false
		for _, v := range at.toolsExpanded {
			if v {
				hasAny = true
				break
			}
		}
		if hasAny {
			at.toolsExpanded = make(map[int]bool)
		} else {
			for i, msg := range at.messages {
				if msg.Role == "tool" {
					at.toolsExpanded[i] = true
				}
			}
		}
		at.messagesDirty = true
		at.refreshViewport()
		return at, nil

	case "q", "esc", "ctrl+o":
		// Exit freeze mode, return to input.
		at.transcript.SetFrozen(false)
		at.focus = FocusInput
		at.follow = true
		at.messagesDirty = true
		at.refreshViewport()
		return at, nil
	}

	return at, nil
}

func (at AgentTab) handleAgentEvent(msg AgentEventMsg) (AgentTab, tea.Cmd) {
	ev := msg.Event

	if ev.Err != nil {
		at.addSystemMessage(fmt.Sprintf("event error: %s", ev.Err))
		at.refreshViewport()
		return at, at.safeWaitForEvent()
	}

	switch ev.Type {
	case "system":
		// Model info shown in status line, no system message needed.
		_ = ev.Data
		return at, at.safeWaitForEvent()

	case "tombstone":
		// Remove the last streaming assistant message from the transcript.
		for i := len(at.messages) - 1; i >= 0; i-- {
			if at.messages[i].Role == "assistant" && !at.messages[i].Done {
				at.messages = append(at.messages[:i], at.messages[i+1:]...)
				at.messagesDirty = true
				break
			}
		}
		at.refreshViewport()
		return at, at.safeWaitForEvent()

	case "system_message":
		if sm, ok := ev.Data.(*engine.SystemMessageEvent); ok {
			at.addSystemMessage(sm.Content)
			at.refreshViewport()
		}
		return at, at.safeWaitForEvent()

	case "usage_update":
		if usage, ok := ev.Data.(*engine.UsageUpdateEvent); ok {
			at.currentTokens = usage.TotalTokens
			at.messagesDirty = true
			// Wire to dashboard TOKENS panel.
			ctxWindow := engine.ContextWindowFor(at.model)
			at.dashboard.SetTokens(usage.TotalTokens, ctxWindow)
		}
		return at, at.safeWaitForEvent()

	case "compaction":
		if compaction, ok := ev.Data.(*engine.CompactionEvent); ok {
			switch compaction.Phase {
			case "running":
				at.compacting = true
				at.compactPhase = "running"
				at.compactStart = time.Now()
				at.compactTokensBefore = compaction.TokensBefore
				at.compactTokensAfter = 0
				at.compactErrMsg = ""
				at.compactVerb = randomCompactVerb("")
				at.compactVerbAt = time.Now()
				at.compactBright = 1.0
				at.compactVel = 0.0
				if compaction.TokensBefore > 0 {
					at.currentTokens = compaction.TokensBefore
				}
				at.messagesDirty = true
				at.refreshViewport()
				at.dashboard.SetCompact(dashboard.CompactInfo{
					Phase:        "running",
					TokensBefore: compaction.TokensBefore,
				})
			case "idle":
				at.compacting = false
				if compaction.TokensAfter > 0 {
					at.currentTokens = compaction.TokensAfter
					at.compactTokensAfter = compaction.TokensAfter
				}
				// Only flip to the completion state if we were actually
				// tracking a compaction - otherwise an idle ping from the
				// orchestrator at startup would spam a dissolve frame.
				if at.compactPhase == "running" {
					at.compactPhase = "complete"
					at.compactSettledAt = time.Now()
					at.compactBright = 1.0
					at.compactVel = 0.0
				}
				at.messagesDirty = true
				at.refreshViewport()
				if at.compactPhase == "complete" {
					at.dashboard.SetCompact(dashboard.CompactInfo{
						Phase:        "complete",
						TokensBefore: at.compactTokensBefore,
						TokensAfter:  at.compactTokensAfter,
					})
					// Also refresh token panel after compaction.
					ctxWindow := engine.ContextWindowFor(at.model)
					at.dashboard.SetTokens(at.currentTokens, ctxWindow)
				}
			case "failed":
				at.compacting = false
				if compaction.Err != nil {
					at.compactErrMsg = compaction.Err.Error()
				} else {
					at.compactErrMsg = "compaction failed"
				}
				at.compactPhase = "failed"
				at.compactSettledAt = time.Now()
				at.compactBright = 1.0
				at.compactVel = 0.0
				at.messagesDirty = true
				at.refreshViewport()
				at.dashboard.SetCompact(dashboard.CompactInfo{
					Phase:  "failed",
					ErrMsg: at.compactErrMsg,
				})
			}
		}
		return at, at.safeWaitForEvent()

	case "stream_event":
		if se, ok := ev.Data.(*engine.StreamEvent); ok {
			if se.Event.Delta != nil && se.Event.Delta.Type == "text_delta" {
				at.streamBuffer += se.Event.Delta.Text

				// Detect viz block state in stream buffer.
				openMarker := "```providence-viz"
				if idx := strings.LastIndex(at.streamBuffer, openMarker); idx != -1 {
					afterOpen := at.streamBuffer[idx+len(openMarker):]
					if !strings.Contains(afterOpen, "```") {
						// In-progress viz block.
						if !at.visualizing {
							at.visualizing = true
							at.vizVerb = randomVizVerb("")
						}
					} else {
						if at.visualizing {
							at.vizCount++ // viz block just completed
						}
						at.visualizing = false
					}
				} else {
					at.visualizing = false
				}
				if len(at.messages) > 0 && at.messages[len(at.messages)-1].Role == "assistant" && !at.messages[len(at.messages)-1].Done {
					at.messages[len(at.messages)-1].Content = at.streamBuffer
				} else {
					at.messages = append(at.messages, ChatMessage{
						Role:    "assistant",
						Content: at.streamBuffer,
						Done:    false,
					})
				}
				at.refreshViewport()
			}
		}
		return at, at.safeWaitForEvent()

	case "tool_input_delta":
		if tid, ok := ev.Data.(*engine.ToolInputDelta); ok {
			at.toolInputBuffer += tid.PartialJSON
			// Update the last tool message's args display with streaming input.
			for i := len(at.messages) - 1; i >= 0; i-- {
				if at.messages[i].Role == "tool" && at.messages[i].ToolOutput == "" {
					at.messages[i].ToolArgs = at.toolInputBuffer
					at.messagesDirty = true
					at.refreshViewport()
					break
				}
			}
		}
		return at, at.safeWaitForEvent()

	case "assistant":
		hasToolUse := false
		if ae, ok := ev.Data.(*engine.AssistantEvent); ok {
			var fullText string
			for _, part := range ae.Message.Content {
				switch part.Type {
				case "text":
					fullText += part.Text
				case "tool_use":
					hasToolUse = true
					at.toolInputBuffer = "" // Reset streaming tool input buffer.
					toolArgs := formatToolArgs(part.Name, part.Input)
					at.messages = append(at.messages, ChatMessage{
						Role:       "tool",
						ToolName:   part.Name,
						ToolArgs:   toolArgs,
						ToolStatus: "success",
						ToolBody:   randomToolFlavor(),
						Done:       true,
					})
					at.persistLastMessage()
					// Wire file touches to dashboard FILES panel.
					at.trackToolFile(part.Name, part.Input)
				}
			}
			if fullText == "" {
				fullText = at.streamBuffer
			}
			if len(at.messages) > 0 && at.messages[len(at.messages)-1].Role == "assistant" && !at.messages[len(at.messages)-1].Done {
				at.messages[len(at.messages)-1].Content = fullText
				at.messages[len(at.messages)-1].Done = true
			} else {
				at.messages = append(at.messages, ChatMessage{
					Role:    "assistant",
					Content: fullText,
					Done:    true,
				})
			}
			at.persistLastMessage()
			at.streamBuffer = ""
			at.refreshViewport()
		}

		// Fire Red-Team-Advisor background agent after tool-use turns.
		if hasToolUse && at.cfg.BGAgentsEnabled {
			if de, ok := at.engine.(*direct.DirectEngine); ok && de.SubagentRunner() != nil {
				rtInput := subagent.TaskInput{
					Description:  "Red team review",
					Prompt:       "Review the last tool calls for drift, missed requirements, or repeat errors. If issues found, report them. If no issues, say LGTM.",
					SubagentType: "Explore",
					RunInBG:      true,
				}
				go de.SubagentRunner().Spawn(context.Background(), rtInput, subagent.BuiltinAgents["Explore"], de.SubagentExecutor()) //nolint:errcheck // fire-and-forget bg agent
			}
		}

		return at, at.safeWaitForEvent()

	case "permission_request":
		if pr, ok := ev.Data.(*engine.PermissionRequestEvent); ok {
			toolName := pr.Tool.Name
			toolArgs := formatToolArgs(toolName, pr.Tool.Input)

			// Auto-approve if user previously chose "always allow" for this tool.
			if at.alwaysAllowTools[toolName] {
				optionID := ""
				for _, opt := range pr.Options {
					if strings.Contains(strings.ToLower(opt.Label), "allow") ||
						strings.Contains(strings.ToLower(opt.ID), "allow") ||
						opt.ID == "yes" {
						optionID = opt.ID
						break
					}
				}
				if optionID == "" && len(pr.Options) > 0 {
					optionID = pr.Options[0].ID
				}
				if at.engine != nil && optionID != "" {
					_ = at.engine.RespondPermission(pr.QuestionID, optionID)
				}
				at.messages = append(at.messages, ChatMessage{
					Role:       "permission",
					Content:    fmt.Sprintf("%s: %s (auto-approved)", toolName, toolArgs),
					Done:       true,
					ToolName:   toolName,
					ToolArgs:   toolArgs,
					ToolStatus: "success",
				})
				at.trackToolFile(toolName, pr.Tool.Input)
				at.refreshViewport()
				return at, at.safeWaitForEvent()
			}

			at.pendingPerm = pr
			at.messages = append(at.messages, ChatMessage{
				Role:       "permission",
				Content:    fmt.Sprintf("%s: %s", toolName, toolArgs),
				Done:       true,
				ToolName:   toolName,
				ToolArgs:   toolArgs,
				ToolStatus: "pending",
			})
			// Track file touches from permission-gated tools too.
			at.trackToolFile(toolName, pr.Tool.Input)
			at.refreshViewport()
		}
		return at, nil

	case "tool_result":
		if tr, ok := ev.Data.(*engine.ToolResultEvent); ok {
			// Find the last tool message matching this tool name and update its output.
			for i := len(at.messages) - 1; i >= 0; i-- {
				if at.messages[i].Role == "tool" && at.messages[i].ToolName == tr.ToolName && at.messages[i].ToolOutput == "" {
					at.messages[i].ToolOutput = tr.Output
					at.messagesDirty = true
					break
				}
			}
			// Wire errors to dashboard ERRORS panel.
			if tr.IsError {
				at.dashboard.AddError(tr.ToolName, tr.Output)
			}
			// Wire TodoWrite results to dashboard TASKS panel.
			if tr.ToolName == "TodoWrite" && !tr.IsError {
				if tp, ok := at.engine.(engine.TodoProvider); ok {
					at.dashboard.SetTasks(convertTodosToTaskInfo(tp.GetCurrentTodos()))
				}
			}
		}
		return at, at.safeWaitForEvent()

	case "result":
		elapsed := int(time.Since(at.spinnerStart).Seconds())
		verb := at.spinnerVerb
		at.streaming = false
		at.streamBuffer = ""
		at.spinnerVerb = ""
		at.visualizing = false
		at.messagesDirty = true
		if re, ok := ev.Data.(*engine.ResultEvent); ok {
			if re.IsError {
				at.addSystemMessage(fmt.Sprintf("Error: %s", re.Result))
				at.dashboard.AddError("session", re.Result)
			}
		}
		vizCount := at.vizCount
		lastVizVerb := at.vizVerb
		at.vizCount = 0
		at.vizVerb = ""
		if verb != "" && elapsed > 0 {
			past := verbToPast(verb)
			var completionMsg string
			if vizCount > 0 && lastVizVerb != "" {
				// Turn viz verb into past tense-ish: "Conjuring the flames" -> "conjured the flames"
				vizPast := strings.ToLower(vizVerbToPast(lastVizVerb))
				completionMsg = fmt.Sprintf("%s and %s for %ds", past, vizPast, elapsed)
			} else {
				completionMsg = fmt.Sprintf("%s for %ds", past, elapsed)
			}
			at.addSystemMessage(completionMsg)
			// Start completion spring animation: bright gold -> frozen ember.
			at.completionText = completionMsg
			at.completionActive = true
			at.completionBright = 1.0
			at.completionVel = 0.0
		}

		// Auto-title: set session title from first user message if not yet set.
		if at.store != nil && at.sessionID != "" {
			if sess, err := at.store.GetSession(at.sessionID); err == nil && sess != nil && sess.Title == "" {
				for _, m := range at.messages {
					if m.Role == "user" && m.Content != "" {
						title := strings.TrimSpace(m.Content)
						// Remove newlines - title should be single line.
						if idx := strings.IndexAny(title, "\n\r"); idx > 0 {
							title = title[:idx]
						}
						if len(title) > 80 {
							title = title[:80]
							// Trim at word boundary if reasonable.
							if idx := strings.LastIndex(title, " "); idx > 40 {
								title = title[:idx]
							}
						}
						title = strings.TrimSpace(title)
						if title != "" {
							at.store.UpdateSessionTitle(at.sessionID, title)
						}
						break
					}
				}
			}
		}

		// Drain queue: steered messages all at once, queued one per turn.
		if len(at.queue) > 0 && at.engine != nil {
			// Collect all steered messages into one combined message.
			var steered []string
			var remaining []QueuedMessage
			for _, m := range at.queue {
				if m.Steered {
					steered = append(steered, m.Text)
				} else {
					remaining = append(remaining, m)
				}
			}

			var text string
			if len(steered) > 0 {
				// Send all steered messages as one combined message.
				text = strings.Join(steered, "\n\n")
				at.queue = remaining
			} else {
				// No steered messages, drain first queued message.
				text = at.queue[0].Text
				at.queue = at.queue[1:]
			}

			at.queueCursor = -1
			at.messages = append(at.messages, ChatMessage{
				Role:    "user",
				Content: text,
				Done:    true,
			})
			at.persistLastMessage()
			at.messagesDirty = true
			at.streaming = true
			at.streamBuffer = ""
			at.follow = true
			at.viewport.GotoBottom()
			at.spinnerFrame = 0
			at.spinnerVerb = randomVerb("")
			at.spinnerStart = time.Now()
			at.spinnerLastVerb = time.Now()
			at.refreshViewport()
			// Track user activity for kairos focus detection.
			if at.kairos != nil {
				at.kairos.RecordUserMessage()
			}
			if err := at.engine.Send(text); err != nil {
				at.addSystemMessage(fmt.Sprintf("send error: %s", err))
				at.streaming = false
				at.refreshViewport()
				return at, nil
			}
			return at, tea.Batch(at.safeWaitForEvent(), spinnerTick())
		}

		// Kairos tick injection: after turn completes, fire next tick.
		if at.kairos != nil && at.kairos.ShouldTick() {
			at.kairos.RecordTick()
			tickMsg := kairos.GenerateTick()
			go func() {
				time.Sleep(100 * time.Millisecond)
				if at.engine != nil && at.kairos.ShouldTick() {
					at.engine.Send(tickMsg)
				}
			}()
		}

		at.refreshViewport()
		if at.compacting {
			return at, at.safeWaitForEvent()
		}
		return at, nil

	case "closed":
		at.streaming = false
		at.compacting = false
		at.spinnerVerb = ""
		at.addSystemMessage("Session closed")
		at.refreshViewport()
		return at, nil

	default:
		return at, at.safeWaitForEvent()
	}
}

// View implements TabModel.
func (at AgentTab) View(width, height int) string {
	if width != at.width || height != at.height {
		at.Resize(width, height)
	}

	// Tab bar + content routing.
	tabBar := at.renderTabBar()

	// Content based on active tab.
	var content string
	switch at.tab {
	case tabChat:
		content = at.renderChatPane(width, height-1) // -1 for tab bar
	default:
		content = at.renderDashboardTab(width, height-1)
	}

	return tabBar + "\n" + content
}

// renderTabBar renders the horizontal tab strip at the top of the view.
func (at AgentTab) renderTabBar() string {
	animTab := int(math.Round(at.tabIndicator))
	if animTab < 0 {
		animTab = 0
	}
	if animTab >= tabCount {
		animTab = tabCount - 1
	}

	segments := make([]string, 0, len(tabNames))
	for i, name := range tabNames {
		isActive := i == at.tab
		isAnimating := i == animTab && animTab != at.tab

		if isActive {
			if at.tabNav {
				segments = append(segments, TabFocusStyle.Render(name))
			} else {
				segments = append(segments, TabActiveStyle.Render(name))
			}
		} else if isAnimating {
			segments = append(segments, TabTrailStyle.Render(name))
		} else {
			segments = append(segments, TabInactiveStyle.Render(name))
		}
	}

	tabRow := lipgloss.JoinHorizontal(lipgloss.Top, segments...)
	return lipgloss.NewStyle().Width(at.width).Align(lipgloss.Center).Render(tabRow)
}

// switchTab changes the active tab and kicks off the indicator spring.
func (at *AgentTab) switchTab(newTab int) {
	at.tab = newTab
	at.tabIndTarget = float64(newTab)
	at.tabNav = true
}

// renderDashboardTab renders a full-width dashboard panel for non-chat tabs.
func (at AgentTab) renderDashboardTab(width, height int) string {
	switch at.tab {
	case tabAgents:
		return at.dashboard.RenderAgentsTab(width, height)
	case tabTasks:
		return at.dashboard.RenderTasksTab(width, height)
	case tabFiles:
		return at.dashboard.RenderFilesTab(width, height)
	case tabTokens:
		return at.dashboard.RenderTokensTab(width, height)
	case tabErrors:
		return at.dashboard.RenderErrorsTab(width, height)
	case tabCompact:
		return at.dashboard.RenderCompactTab(width, height)
	case tabHooks:
		return at.dashboard.RenderHooksTab(width, height)
	}
	return ""
}

// renderChatPane renders the full chat view (viewport + divider + preview + input)
// constrained to the given pane width and height.
func (at AgentTab) renderChatPane(paneWidth, height int) string {
	contentW := chatContentWidth(paneWidth)

	// Set input width to match content area.
	at.input.SetWidth(contentW - 4) // subtract prompt width.

	// Build command preview if typing /.
	preview := at.renderCommandPreview(contentW)
	previewLines := 0
	if preview != "" {
		previewLines = strings.Count(preview, "\n") + 1
	}

	// Viewport gets remaining height minus: divider(1) + input(1) + preview.
	bottomH := 2 + previewLines
	vpH := height - bottomH
	if vpH < 3 {
		vpH = 3
	}

	at.viewport.SetWidth(contentW)
	at.viewport.SetHeight(vpH)
	at.refreshViewport()

	// Calculate left padding to center the content block within the pane.
	pad := (paneWidth - contentW) / 2
	if pad < 0 {
		pad = 0
	}
	leftPad := strings.Repeat(" ", pad)

	// Build viewport with left padding on each line.
	vpLines := strings.Split(at.viewport.View(), "\n")
	var vpPadded strings.Builder
	for i, line := range vpLines {
		if i > 0 {
			vpPadded.WriteString("\n")
		}
		vpPadded.WriteString(leftPad + line)
	}

	// Gradient divider at content width, padded.
	divider := leftPad + gradientDivider(contentW)

	// Command preview, padded.
	previewSection := ""
	if preview != "" {
		for _, line := range strings.Split(preview, "\n") {
			previewSection += leftPad + line + "\n"
		}
	}

	// File picker popup, padded.
	pickerSection := ""
	if pv := at.filePicker.View(contentW); pv != "" {
		for _, line := range strings.Split(pv, "\n") {
			pickerSection += leftPad + line + "\n"
		}
	}

	// Quote selection overlay, padded.
	quoteSection := ""
	if at.quoteModel.Active() {
		if qv := at.quoteModel.View(contentW); qv != "" {
			for _, line := range strings.Split(qv, "\n") {
				quoteSection += leftPad + line + "\n"
			}
		}
	}

	// Notification toasts, padded.
	notifSection := ""
	if nv := at.notifications.View(contentW); nv != "" {
		for _, line := range strings.Split(nv, "\n") {
			notifSection += leftPad + line + "\n"
		}
	}

	// Input, padded.
	inputLine := leftPad + at.input.View()

	return "\n" + vpPadded.String() + "\n" + divider + "\n" + previewSection + quoteSection + pickerSection + notifSection + inputLine
}

// matchingSlashCommands returns the slash commands whose names start with
// the current input value (case-insensitive). Pure helper so callers can
// reason about what renderCommandPreview is about to draw.
func (at AgentTab) matchingSlashCommands() []slashCommand {
	val := at.input.Value()
	if !strings.HasPrefix(val, "/") {
		return nil
	}
	// Match on the first token only - the command name - so "/model haiku"
	// still shows the /model row as the active match.
	head := strings.ToLower(strings.SplitN(val, " ", 2)[0])
	var matches []slashCommand
	for _, cmd := range slashCommands {
		if strings.HasPrefix(cmd.Name, head) {
			matches = append(matches, cmd)
		}
	}
	return matches
}

// renderCommandPreview renders the flame-styled slash command table that
// floats above the input whenever the user is typing a slash command.
// The panel is spring-animated on entry/exit (slashOpen) and the active
// row breathes via a harmonica pulse (slashPulse).
func (at AgentTab) renderCommandPreview(contentW int) string {
	matches := at.matchingSlashCommands()
	if len(matches) == 0 {
		return ""
	}
	// Dismissed / mid-exit: suppress render entirely once the spring has
	// basically collapsed. refreshViewport will re-render every frame.
	if at.slashOpen < 0.05 && !strings.HasPrefix(at.input.Value(), "/") {
		return ""
	}

	// The input holds the input for this frame so the table knows which
	// row is "in focus". If the user has not navigated explicitly, the
	// first match is shown as active (the one that will run on enter).
	activeIdx := at.slashCursor
	if activeIdx < 0 || activeIdx >= len(matches) {
		activeIdx = 0
	}

	// Panel width: clamp to a reasonable pill within contentW.
	panelW := contentW - 4
	if panelW > 72 {
		panelW = 72
	}
	if panelW < 32 {
		panelW = 32
	}
	innerW := panelW - 4 // account for border + padding

	// Column widths: command name (left), description (right).
	nameW := 12
	descW := innerW - nameW - 2
	if descW < 10 {
		descW = 10
	}

	mutedStyle := lipgloss.NewStyle().Foreground(ColorMuted)
	descStyle := mutedStyle

	// Breathing color for the active row uses the spring pulse interpolated
	// between the secondary ember and the primary peak.
	pulse := at.slashPulse
	if pulse < 0 {
		pulse = 0
	}
	if pulse > 1 {
		pulse = 1
	}
	activeHex := blendHex(
		darkenHex(ActiveTheme.Primary, 0.55),
		ActiveTheme.Accent,
		pulse,
	)
	activeColor := lipgloss.Color(activeHex)

	// How many rows to render this frame: scale with slashOpen so the
	// panel grows from 0 rows -> len(matches) rows during the entry spring.
	open := at.slashOpen
	if open < 0 {
		open = 0
	}
	if open > 1 {
		open = 1
	}
	visibleRows := int(math.Ceil(open * float64(len(matches))))
	if visibleRows < 1 && open > 0.02 {
		visibleRows = 1
	}
	if visibleRows > len(matches) {
		visibleRows = len(matches)
	}
	if visibleRows == 0 {
		return ""
	}

	var rows []string
	for i := 0; i < visibleRows; i++ {
		m := matches[i]
		name := m.Name
		if len(name) > nameW {
			name = name[:nameW]
		}
		for len(name) < nameW {
			name += " "
		}
		desc := m.Desc
		if len(desc) > descW {
			desc = desc[:descW-1] + "\u2026"
		}

		if i == activeIdx {
			// Active row: solid bright background tint, bold command,
			// chevron marker on the left.
			marker := lipgloss.NewStyle().Foreground(activeColor).Bold(true).Render("\u27A4 ")
			nameStyled := lipgloss.NewStyle().
				Foreground(activeColor).
				Bold(true).
				Render(name)
			descStyled := lipgloss.NewStyle().
				Foreground(ColorText).
				Render(desc)
			row := marker + nameStyled + "  " + descStyled
			// Pad to innerW so the row fills the panel.
			rows = append(rows, padRight(row, innerW+2))
		} else {
			marker := mutedStyle.Render("  ")
			nameStyled := lipgloss.NewStyle().Foreground(ColorPrimary).Render(name)
			descStyled := descStyle.Render(desc)
			row := marker + nameStyled + "  " + descStyled
			rows = append(rows, padRight(row, innerW+2))
		}
	}

	// Footer hint row.
	hintStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	hint := hintStyle.Render("\u2191\u2193 select  \u21B5 run  esc close")
	hintLine := padRight(hint, innerW+2)

	// Animated gradient border: rotate the flame stops like the user message
	// box does while streaming. Matches the flame aesthetic.
	offset := at.flameFrame % len(flameGradientStops)
	a := flameGradientStops[offset]
	b := flameGradientStops[(offset+2)%len(flameGradientStops)]
	cEdge := flameGradientStops[(offset+4)%len(flameGradientStops)]

	body := strings.Join(rows, "\n") + "\n" + hintLine
	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForegroundBlend(a, b, cEdge).
		Padding(0, 1).
		Width(panelW).
		Render(body)
	return panel
}

// padRight pads s with spaces so its visible width is at least w.
// Safe on ANSI-styled strings because it uses lipgloss.Width.
func padRight(s string, w int) string {
	current := lipgloss.Width(s)
	if current >= w {
		return s
	}
	return s + strings.Repeat(" ", w-current)
}

// Hints returns context-dependent status bar hints.
// Only shows during special states - idle mode has no hints (clean).
func (at AgentTab) Hints() []components.HintItem {
	// Freeze mode hints.
	if at.focus == FocusTranscript && at.transcript.Frozen() {
		hints := []components.HintItem{
			{Key: "j/k", Desc: "scroll"},
			{Key: "/", Desc: "search"},
		}
		if at.transcript.SearchHitCount() > 0 {
			hints = append(hints, components.HintItem{Key: "n/N", Desc: "next/prev"})
		}
		hints = append(hints, components.HintItem{Key: "q", Desc: "exit freeze"})
		return hints
	}
	if at.pendingPerm != nil {
		return []components.HintItem{
			{Key: "y", Desc: "approve"},
			{Key: "n", Desc: "deny"},
			{Key: "a", Desc: "always allow"},
		}
	}
	if at.queueCursor >= 0 {
		return []components.HintItem{
			{Key: "enter", Desc: "steer"},
			{Key: "del", Desc: "remove"},
			{Key: "esc", Desc: "back"},
		}
	}
	if at.streaming && len(at.queue) > 0 {
		return []components.HintItem{
			{Key: "up", Desc: "select queue"},
		}
	}
	// No extra hints in idle/streaming - status line handles it.
	return nil
}

// Focused implements TabModel.
func (at AgentTab) Focused() bool {
	return at.streaming || at.pendingPerm != nil
}

// cwdShort returns the last 2-3 segments of the current working directory.
func cwdShort() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "?"
	}
	parts := strings.Split(filepath.ToSlash(cwd), "/")
	// Drop empty trailing segment if any.
	for len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) <= 2 {
		return filepath.Join(parts...)
	}
	return "~/" + filepath.Join(parts[len(parts)-2:]...)
}

// StatusLine shows all status info as a single row of bordered pills:
// [engine] [model] [session] [ctx%] [cwd] [ctrl+o freeze]
func (at AgentTab) StatusLine() string {
	modelName := at.modelDisplay()
	if modelName == "default" {
		modelName = "sonnet"
	}

	session := "idle"
	if at.engine != nil {
		if at.compacting {
			session = "compacting"
		} else if at.streaming {
			session = "streaming"
		} else {
			session = "active"
		}
	}

	items := []components.HintItem{
		{Key: string(at.engineType), Desc: "engine"},
		{Key: modelName, Desc: "model"},
		{Key: session, Desc: "session"},
	}

	// Context % pill with color.
	if at.engine != nil {
		ctxWindow := engine.ContextWindowFor(at.model)
		if ctxWindow > 0 {
			pct := at.currentTokens * 100 / ctxWindow

			pillColor := ColorMuted
			switch {
			case pct >= 85:
				pillColor = ColorError
			case pct >= 70:
				pillColor = ColorWarning
			case pct >= 50:
				pillColor = ColorPrimary
			}

			items = append(items, components.TintedHint(fmt.Sprintf("%d%%", pct), "ctx", pillColor))
		}
	}

	// CWD as a pill.
	items = append(items, components.HintItem{Key: cwdShort(), Desc: "cwd"})

	// Freeze hint when there are messages to browse.
	if len(at.messages) > 0 {
		items = append(items, components.HintItem{Key: "ctrl+o", Desc: "freeze"})
	}

	return components.StatusBarFromItems(items, at.width)
}

// --- Internal Helpers ---

// PrepareSend sets up state for sending a message. Call before sendCmd.
const inputHistoryMax = 50

// pushHistory appends a message to the input history ring buffer.
func (at *AgentTab) pushHistory(text string) {
	// Deduplicate: skip if same as last entry.
	if len(at.inputHistory) > 0 && at.inputHistory[len(at.inputHistory)-1] == text {
		at.inputHistoryIdx = len(at.inputHistory)
		at.inputHistoryBrowsing = false
		return
	}
	at.inputHistory = append(at.inputHistory, text)
	if len(at.inputHistory) > inputHistoryMax {
		at.inputHistory = at.inputHistory[1:]
	}
	at.inputHistoryIdx = len(at.inputHistory)
	at.inputHistoryBrowsing = false
}

func (at *AgentTab) prepareSend(text string) {
	imgCount := len(at.pendingImages)
	at.messages = append(at.messages, ChatMessage{
		Role:       "user",
		Content:    text,
		Done:       true,
		ImageCount: imgCount,
	})
	at.streaming = true
	at.streamBuffer = ""
	at.follow = true
	at.spinnerFrame = 0
	at.spinnerVerb = randomVerb("")
	at.spinnerStart = time.Now()
	at.spinnerLastVerb = time.Now()
	at.refreshViewport()
}

// transferImagesToEngine moves pending images from the UI to the engine (if supported).
// Returns the number of images transferred.
func (at *AgentTab) transferImagesToEngine() int {
	if len(at.pendingImages) == 0 || at.engine == nil {
		return 0
	}
	type imageSetter interface {
		SetPendingImages([]direct.ImageData)
	}
	if setter, ok := at.engine.(imageSetter); ok {
		images := make([]direct.ImageData, len(at.pendingImages))
		for i, img := range at.pendingImages {
			images[i] = direct.ImageData{
				MediaType: img.MediaType,
				Data:      img.Data,
			}
		}
		setter.SetPendingImages(images)
		n := len(at.pendingImages)
		at.pendingImages = nil
		return n
	}
	// Engine doesn't support images (e.g. claude headless) - warn and clear.
	if len(at.pendingImages) > 0 {
		at.addSystemMessage("Images not supported by this engine, sending text only")
		at.pendingImages = nil
	}
	return 0
}

// SendCmd returns the tea.Cmd to send a message (create session or send to existing).
func (at AgentTab) sendCmd(text string) tea.Cmd {
	if at.engine == nil {
		return createEngineAndSend(text, at.model, at.engineType, at.cfg.OutputStyle)
	}
	if err := at.engine.Send(text); err != nil {
		return nil
	}
	return tea.Batch(at.safeWaitForEvent(), spinnerTick())
}

// generateAutoTitle generates a session title from the first user message.
// Runs once per session, in a goroutine, using simple truncation.
func (at *AgentTab) generateAutoTitle() {
	if at.sessionID == "" || at.autoTitleGenerated {
		return
	}
	at.autoTitleGenerated = true

	firstUser := ""
	for _, msg := range at.messages {
		if msg.Role == "user" {
			firstUser = msg.Content
			break
		}
	}
	if firstUser == "" {
		return
	}

	go func() {
		title := strings.ReplaceAll(firstUser, "\n", " ")
		title = strings.TrimSpace(title)
		if len(title) > 80 {
			title = title[:80]
			if idx := strings.LastIndex(title, " "); idx > 40 {
				title = title[:idx]
			}
		}
		if at.store != nil {
			at.store.UpdateSessionTitle(at.sessionID, title)
		}
	}()
}

func (at *AgentTab) addMessage(role, content string, done bool) {
	at.messages = append(at.messages, ChatMessage{
		Role:    role,
		Content: content,
		Done:    done,
	})
	at.messagesDirty = true
	// Persist done messages to DB.
	if done {
		at.persistLastMessage()
	}
}

// persistLastMessage saves the last message in at.messages to the DB.
func (at *AgentTab) persistLastMessage() {
	if at.store == nil || at.sessionID == "" || len(at.messages) == 0 {
		return
	}
	m := at.messages[len(at.messages)-1]
	at.store.AddMessage(at.sessionID, m.Role, m.Content, m.ToolName, m.ToolArgs, m.ToolStatus, m.ToolBody, m.ToolOutput, m.ImageCount, m.Done)
}

func (at *AgentTab) addSystemMessage(content string) {
	at.addMessage("system", content, true)
}

// messagesToRestored converts the current chat messages to RestoredMessage
// format for context portability during engine switches.
func (at AgentTab) messagesToRestored() []engine.RestoredMessage {
	var restored []engine.RestoredMessage
	for _, m := range at.messages {
		switch m.Role {
		case "user", "assistant":
			if m.Content != "" {
				restored = append(restored, engine.RestoredMessage{
					Role:    m.Role,
					Content: m.Content,
				})
			}
		case "tool":
			if m.ToolOutput != "" || m.ToolBody != "" {
				output := m.ToolOutput
				if output == "" {
					output = m.ToolBody
				}
				restored = append(restored, engine.RestoredMessage{
					Role:       "tool",
					Content:    output,
					ToolName:   m.ToolName,
					ToolInput:  m.ToolArgs,
				})
			}
		}
	}
	return restored
}

// hasBatchTools returns true if there are consecutive same-name tool messages (2+).
func (at AgentTab) hasBatchTools() bool {
	for i := 0; i < len(at.messages)-1; i++ {
		if at.messages[i].Role == "tool" && at.messages[i+1].Role == "tool" && at.messages[i].ToolName == at.messages[i+1].ToolName {
			return true
		}
	}
	return false
}

// hasVizMessages returns true if any done message contains viz blocks.
func (at AgentTab) hasVizMessages() bool {
	for _, m := range at.messages {
		if m.Done && m.Role == "assistant" && strings.Contains(m.Content, "```providence-viz") {
			return true
		}
	}
	return false
}

// hasSteeredMessage returns true if any queued message is marked as steered.
func (at AgentTab) hasSteeredMessage() bool {
	for _, m := range at.queue {
		if m.Steered {
			return true
		}
	}
	return false
}

func (at *AgentTab) updateLastPermissionStatus(status string) {
	for i := len(at.messages) - 1; i >= 0; i-- {
		if at.messages[i].Role == "permission" {
			at.messages[i].ToolStatus = status
			return
		}
	}
}

func (at *AgentTab) refreshViewport() {
	// Banner animates every tick (cheap).
	banner := centerBlockUniform(
		RenderBannerAnimated(at.flameFrame, at.streaming),
		chatContentWidth(at.width),
	)

	// Messages re-render when dirty, streaming, animating, viz breathing, or batch tools animating.
	// When idle with no changes, use the cached render to avoid flicker.
	hasViz := at.hasVizMessages()
	hasBatch := at.streaming && len(at.toolsExpanded) == 0 && at.hasBatchTools()
	compactLive := at.compactPhase != ""
	if at.messagesDirty || at.streaming || at.completionActive || compactLive || hasViz || hasBatch || at.cachedMessages == "" {
		at.cachedMessages = at.renderMessages()
		at.messagesDirty = false
	}

	content := banner + "\n" + at.cachedMessages

	// Overlay: tree view replaces message content when active.
	if at.treeViewOpen && len(at.messages) > 0 {
		treeTheme := tree.ThemeColors{
			Primary:    ActiveTheme.Primary,
			Secondary:  ActiveTheme.Secondary,
			Accent:     ActiveTheme.Accent,
			Muted:      ActiveTheme.Muted,
			Text:       ActiveTheme.Text,
			Background: ActiveTheme.Background,
			Error:      ActiveTheme.Error,
		}
		treeMsgs := make([]tree.ChatMessage, len(at.messages))
		for i, m := range at.messages {
			treeMsgs[i] = tree.ChatMessage{
				Role:       m.Role,
				Content:    m.Content,
				ToolName:   m.ToolName,
				ToolArgs:   m.ToolArgs,
				ToolOutput: m.ToolOutput,
				ToolStatus: m.ToolStatus,
				Done:       m.Done,
			}
		}
		nodes := tree.BuildTree(treeMsgs)
		treeContent := tree.RenderTree(nodes, chatContentWidth(at.width), treeTheme)
		content = banner + "\n" + treeContent
	}

	// Overlay: rewind picker appended at bottom when active.
	if at.rewindModel.Active() {
		content += "\n" + at.rewindModel.View()
	}

	at.viewport.SetContent(content)
	if at.follow {
		at.viewport.GotoBottom()
	}
}

// invalidateMessages marks the message cache as dirty so it re-renders on next refreshViewport.
func (at *AgentTab) invalidateMessages() {
	at.messagesDirty = true
}

func (at AgentTab) renderMessages() string {
	if len(at.messages) == 0 {
		// Centered empty state: title with ember breathing, hint with dim ember breathing.
		contentW := chatContentWidth(at.width)
		titleText := "Providence Awaits"
		hintText := "The Profaned God is ready for your command"
		titleColor := emberBreathe(at.flameFrame)
		hintColor := emberBreatheD(at.flameFrame)
		title := lipgloss.NewStyle().Foreground(titleColor).Bold(true).Render(titleText)
		hint := lipgloss.NewStyle().Foreground(hintColor).Render(hintText)
		titlePad := (contentW - lipgloss.Width(titleText)) / 2
		hintPad := (contentW - lipgloss.Width(hintText)) / 2
		if titlePad < 0 {
			titlePad = 0
		}
		if hintPad < 0 {
			hintPad = 0
		}
		return "\n\n\n" + strings.Repeat(" ", titlePad) + title + "\n" + strings.Repeat(" ", hintPad) + hint
	}

	contentW := chatContentWidth(at.width)
	var b strings.Builder

	// Find the last user message index and last tool index AFTER it.
	lastUserIdx := -1
	lastToolIdx := -1
	for i, msg := range at.messages {
		if msg.Role == "user" {
			lastUserIdx = i
			lastToolIdx = -1 // Reset: only tools after this user message count.
		}
		if msg.Role == "tool" {
			lastToolIdx = i
		}
	}

	// Build batch groups of same-name tool messages.
	// Groups tools even if assistant/system messages sit between them,
	// as long as no user message or different tool name interrupts.
	type toolGroup struct {
		indices []int // all message indices in this group
		name    string
		count   int
	}
	var groups []toolGroup
	for i := 0; i < len(at.messages); i++ {
		if at.messages[i].Role != "tool" {
			continue
		}
		name := at.messages[i].ToolName
		indices := []int{i}
		// Look ahead: skip assistant/system, collect same-name tools.
		for j := i + 1; j < len(at.messages); j++ {
			if at.messages[j].Role == "tool" {
				if at.messages[j].ToolName == name {
					indices = append(indices, j)
				} else {
					break // different tool name, stop
				}
			} else if at.messages[j].Role == "user" {
				break // user message, stop
			}
			// assistant/system messages: skip, keep looking
		}
		if len(indices) >= 2 {
			groups = append(groups, toolGroup{indices: indices, name: name, count: len(indices)})
			i = indices[len(indices)-1] // skip past the last tool in this group
		}
	}

	// Create lookup sets for batch rendering.
	batchStart := make(map[int]toolGroup) // first tool index -> group
	batchSkip := make(map[int]bool)       // non-first tool indices to skip
	for _, g := range groups {
		batchStart[g.indices[0]] = g
		for _, idx := range g.indices[1:] {
			batchSkip[idx] = true
		}
	}

	for i, msg := range at.messages {
		// Skip empty assistant messages (can happen during streaming setup).
		if msg.Role == "assistant" && strings.TrimSpace(msg.Content) == "" && msg.Done {
			continue
		}
		// Skip batched tool messages (handled by batch header).
		if msg.Role == "tool" && batchSkip[i] && !at.toolsExpanded[i] {
			continue
		}

		if i > 0 {
			b.WriteString("\n")
		}

		var rendered string
		switch msg.Role {
		case "user":
			rendered = at.renderUserMessage(msg, contentW, i == lastUserIdx)
		case "assistant":
			rendered = at.renderAssistantMessage(msg)
		case "system":
			rendered = at.renderSystemMessage(msg)
		case "permission":
			rendered = at.renderPermissionMessage(msg, contentW)
		case "tool":
			if g, ok := batchStart[i]; ok && !at.toolsExpanded[i] {
				// Collect messages for this batch by indices.
				batchMsgs := make([]ChatMessage, len(g.indices))
				for bi, idx := range g.indices {
					batchMsgs[bi] = at.messages[idx]
				}
				rendered = at.renderBatchToolHeader(g.name, g.count, batchMsgs)
			} else {
				rendered = at.renderToolMessage(msg, i, i == lastToolIdx)
			}
		case "thinking":
			rendered = at.renderThinkingMessage(msg)
		}
		// In freeze mode with an active search query, highlight matches.
		if rendered != "" && at.focus == FocusTranscript && at.transcript.searchQuery != "" {
			rendered = highlightSearchMatches(rendered, at.transcript.searchQuery)
		}
		if rendered != "" {
			b.WriteString(rendered)
		}
	}

	// Render queued messages above the spinner if any exist.
	if len(at.queue) > 0 {
		b.WriteString("\n" + at.renderQueuedMessages(contentW))
	}

	// Compaction indicator: when a compaction is in flight we replace the
	// regular spinner with a dedicated compact indicator so both animations
	// never run at once. The dissolve frame also lingers briefly after the
	// compaction completes so the user sees the terminal state.
	if at.compactPhase != "" {
		if ind := at.renderCompactIndicator(); ind != "" {
			b.WriteString("\n" + ind + "\n")
		}
	} else if at.streaming {
		// Append spinner below the last message when streaming.
		spinner := at.renderSpinner()
		if spinner != "" {
			b.WriteString("\n" + spinner + "\n")
		}
	}

	return b.String()
}

// renderCompactIndicator renders the live compaction indicator with a
// breathing flame spinner, Providence-themed verb, and a token counter
// that reports "before -> after" once the numbers land. Mirrors the
// regular streaming spinner but uses its own spring so the two animations
// can coexist in the model even though only one renders at a time.
func (at AgentTab) renderCompactIndicator() string {
	if at.compactPhase == "" {
		return ""
	}

	bright := at.compactBright
	if bright < 0 {
		bright = 0
	}
	if bright > 1 {
		bright = 1
	}

	// Live frame: flame block spinner + verb + token counter.
	frame := string(spinnerFrames[at.flameFrame%len(spinnerFrames)])
	mutedStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	switch at.compactPhase {
	case "running":
		// Bright breathing pulse: interpolate between frozen ember and
		// primary peak using the spring brightness + sine.
		accentHex := blendHex(
			ActiveTheme.Secondary,
			ActiveTheme.Accent,
			bright,
		)
		accent := lipgloss.Color(accentHex)
		spinnerStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(flameColor(at.flameFrame))).
			Bold(true)
		verbStyle := lipgloss.NewStyle().Foreground(accent).Italic(true).Bold(true)
		verb := at.compactVerb
		if verb == "" {
			verb = "Compacting flames"
		}

		before := at.compactTokensBefore
		tokenLine := mutedStyle.Render("  " + formatTokenTrail(before, 0))
		elapsed := int(time.Since(at.compactStart).Seconds())
		line := "  " + spinnerStyle.Render(frame) + " " +
			verbStyle.Render(verb+"...") + " " +
			mutedStyle.Render(fmt.Sprintf("(%ds)", elapsed))
		return line + "\n" + tokenLine

	case "complete":
		// Dissolve frame: color chases from primary -> frozen via the
		// completionCoolRamp, same as the main completion animation.
		c := completionColor(bright)
		style := lipgloss.NewStyle().Foreground(c).Italic(true).Bold(true)
		verb := at.compactVerb
		if verb == "" {
			verb = "Compaction"
		}
		past := verbToPast(strings.SplitN(verb, " ", 2)[0])
		rest := ""
		if parts := strings.SplitN(verb, " ", 2); len(parts) > 1 {
			rest = " " + parts[1]
		}
		before := at.compactTokensBefore
		after := at.compactTokensAfter
		elapsed := int(time.Since(at.compactStart).Seconds())
		head := style.Render("  \u2756 " + past + rest + fmt.Sprintf(" in %ds", elapsed))
		trail := mutedStyle.Render("  " + formatTokenTrail(before, after))
		return head + "\n" + trail

	case "failed":
		errStyle := lipgloss.NewStyle().Foreground(ColorError).Italic(true).Bold(true)
		msg := at.compactErrMsg
		if msg == "" {
			msg = "Compaction failed"
		}
		return errStyle.Render("  \u2716 " + msg)
	}

	return ""
}

// formatTokenTrail builds a "NK -> ?" or "NK -> MK" style counter line
// used by the compaction indicator. Before is always shown, after is "?"
// while the compaction is still running.
func formatTokenTrail(before, after int) string {
	if before <= 0 && after <= 0 {
		return ""
	}
	b := formatTokenCount(before)
	if after <= 0 {
		return b + " \u2192 ?"
	}
	return b + " \u2192 " + formatTokenCount(after)
}

// formatTokenCount formats a token count compactly: <1000 raw, >=1000
// uses "K" suffix with one decimal when meaningful.
func formatTokenCount(n int) string {
	if n <= 0 {
		return "0"
	}
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	f := float64(n) / 1000.0
	if f >= 100 {
		return fmt.Sprintf("%.0fK", f)
	}
	return fmt.Sprintf("%.1fK", f)
}

// RenderUserMessage renders a user message in a rounded border box.
// Border animates in flame colors while streaming, freezes to amber when done.
func (at AgentTab) renderUserMessage(msg ChatMessage, contentW int, isLatest bool) string {
	var boxStyle lipgloss.Style

	if at.streaming && isLatest {
		// Animated gradient border: rotate 3 color stops from the 7-stop flame palette.
		offset := at.flameFrame % len(flameGradientStops)
		a := flameGradientStops[offset]
		b := flameGradientStops[(offset+2)%len(flameGradientStops)]
		c := flameGradientStops[(offset+4)%len(flameGradientStops)]

		boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForegroundBlend(a, b, c).
			Padding(0, 1)
	} else {
		// Frozen: static warm gradient with ColorFrozen, no animation.
		frozenEdge := lipgloss.Color(darkenHex(ActiveTheme.Primary, 0.4))
		boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForegroundBlend(
				frozenEdge,
				ColorFrozen,
				frozenEdge,
			).
			Padding(0, 1)
	}

	textStyle := lipgloss.NewStyle().Foreground(ColorText).Bold(true)

	// ❯ prefix in flame color (animated when streaming, frozen otherwise).
	var prefixHex string
	if at.streaming && isLatest {
		prefixHex = flameColor(at.flameFrame)
	} else {
		prefixHex = ActiveTheme.Frozen
	}
	prefix := lipgloss.NewStyle().Foreground(lipgloss.Color(prefixHex)).Bold(true).Render("\u27E9 ")

	wrapW := contentW - 10
	if wrapW < 20 {
		wrapW = 20
	}
	text := wordWrap(msg.Content, wrapW)

	// Prepend image labels if this message had images.
	var imageLabels string
	if msg.ImageCount > 0 {
		imgStyle := lipgloss.NewStyle().
			Foreground(ColorBackground).
			Background(ColorSecondary).
			Bold(true).
			Padding(0, 1)
		for i := range msg.ImageCount {
			if i > 0 {
				imageLabels += " "
			}
			imageLabels += imgStyle.Render(fmt.Sprintf("Image #%d", i+1))
		}
		imageLabels += "\n"
	}

	return boxStyle.Render(imageLabels+prefix+textStyle.Render(text)) + "\n"
}

// RenderAssistantMessage renders assistant text with glamour markdown rendering.
// Works for both streaming and done messages. Viz blocks are extracted and rendered separately.
func (at AgentTab) renderAssistantMessage(msg ChatMessage) string {
	arrowStyle := lipgloss.NewStyle().Foreground(ColorSecondary)
	indent := "  "

	text := strings.TrimSpace(msg.Content)
	if text == "" {
		return ""
	}

	// Process viz blocks: render completed ones, strip in-progress ones.
	if !msg.Done {
		text = at.processStreamingViz(text)
		text = strings.TrimSpace(text)
		if text == "" && at.visualizing {
			return "" // spinner handles the viz indicator
		}
	}

	if text == "" {
		return ""
	}

	// Glamour render for both streaming and done messages.
	if at.mdRenderer != nil {
		content, vizRendered := ExtractAndRenderVizBlocks(text, at.width-4, at.flameFrame)
		rendered, err := at.mdRenderer.Render(content)
		if err == nil {
			for placeholder, vizOutput := range vizRendered {
				// Glamour wraps placeholders in paragraph spacing - strip the extra blank lines.
				rendered = strings.ReplaceAll(rendered, "\n\n"+placeholder+"\n\n", "\n"+vizOutput+"\n")
				rendered = strings.ReplaceAll(rendered, "\n\n"+placeholder+"\n", "\n"+vizOutput+"\n")
				rendered = strings.ReplaceAll(rendered, "\n"+placeholder+"\n\n", "\n"+vizOutput+"\n")
				rendered = strings.ReplaceAll(rendered, placeholder, vizOutput)
			}
			trimmed := strings.TrimSpace(rendered)
			lines := strings.Split(trimmed, "\n")
			var b strings.Builder
			for i, line := range lines {
				if i == 0 {
					b.WriteString(arrowStyle.Bold(true).Render("✦ ") + line + "\n")
				} else {
					b.WriteString(indent + line + "\n")
				}
			}
			if !msg.Done && !at.visualizing {
				s := strings.TrimRight(b.String(), "\n")
				return s + MutedStyle.Render("\u258d") + "\n"
			}
			return b.String()
		}
	}

	// Fallback: raw text if glamour fails.
	var b strings.Builder
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if i == 0 {
			b.WriteString(arrowStyle.Bold(true).Render("✦ ") + line + "\n")
		} else {
			b.WriteString(indent + line + "\n")
		}
	}
	if !msg.Done && !at.visualizing {
		s := strings.TrimRight(b.String(), "\n")
		return s + MutedStyle.Render("\u258d") + "\n"
	}
	return b.String()
}

// processStreamingViz handles viz blocks during streaming:
// - Completed blocks (```providence-viz ... ```) get rendered immediately
// - In-progress blocks (opening ``` but no closing) show a "Visualizing" spinner with calamity verbs
func (at AgentTab) processStreamingViz(text string) string {
	// Only strip in-progress viz blocks. Completed blocks are handled
	// by ExtractAndRenderVizBlocks in the glamour rendering path.
	openMarker := "```providence-viz"
	lastOpen := strings.LastIndex(text, openMarker)
	if lastOpen == -1 {
		return text
	}
	afterOpen := text[lastOpen+len(openMarker):]
	if strings.Contains(afterOpen, "```") {
		return text // complete, glamour path will handle it
	}

	// Strip the in-progress block from display.
	return strings.TrimRight(text[:lastOpen], "\n") + "\n"
}

// RenderSystemMessage renders a system message in italic muted with 2-char indent.
// Completion messages get a harmonica spring cool-down: bright gold -> frozen ember.
func (at AgentTab) renderSystemMessage(msg ChatMessage) string {
	// Check if this is the active completion message with spring animation.
	if at.completionText != "" && msg.Content == at.completionText && at.completionActive {
		c := completionColor(at.completionBright)
		style := lipgloss.NewStyle().Foreground(c).Italic(true)
		return style.Render("  "+msg.Content) + "\n"
	}
	style := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	return style.Render("  "+msg.Content) + "\n"
}

// renderQueuedMessages renders each queued message as its own mini-box.
// Not selected + not steered: dashed flame border, "Queue:" label.
// Selected: gold solid border with action hints.
// Steered: double-line border, "Steer:" label, brighter color.
func (at AgentTab) renderQueuedMessages(contentW int) string {
	// Spring-driven brightness for border color interpolation.
	bright := at.queuedBright
	if bright < 0.0 {
		bright = 0.0
	}
	if bright > 1.0 {
		bright = 1.0
	}

	wrapW := contentW - 14
	if wrapW < 20 {
		wrapW = 20
	}

	var result strings.Builder
	for i, msg := range at.queue {
		isSelected := (at.queueCursor == i)
		wrapped := wordWrap(msg.Text, wrapW)

		// Gradient border: steered = hotter (gold peak), queued = cooler (ember).
		offset := at.flameFrame % len(flameGradientStops)
		a := flameGradientStops[offset]
		b := flameGradientStops[(offset+2)%len(flameGradientStops)]
		c := flameGradientStops[(offset+4)%len(flameGradientStops)]

		fc := lipgloss.Color(flameColor(at.flameFrame))

		var label, hint string
		var textColor color.Color
		if msg.Steered {
			textColor = ColorAccent
			label = lipgloss.NewStyle().
				Foreground(ColorBackground).
				Background(lipgloss.Color(flameColor(at.flameFrame))).
				Bold(true).
				Padding(0, 1).
				Render("Steer")
			if isSelected {
				hint = "\n" + lipgloss.NewStyle().Foreground(ColorMuted).Italic(true).
					Render("  already steered  del: remove")
			}
			// Hotter gradient for steered: shift further into bright range.
			a = flameGradientStops[(offset+1)%len(flameGradientStops)]
			b = flameGradientStops[(offset+3)%len(flameGradientStops)]
			c = flameGradientStops[(offset+5)%len(flameGradientStops)]
		} else {
			textColor = ColorPrimary
			label = lipgloss.NewStyle().
				Foreground(ColorBackground).
				Background(lipgloss.Color(flameColor(at.flameFrame))).
				Bold(true).
				Padding(0, 1).
				Render("Queue")
			if isSelected {
				hint = "\n" + lipgloss.NewStyle().Foreground(fc).Italic(true).
					Render("  enter: steer  del: remove")
			}
		}
		textStyle := lipgloss.NewStyle().Foreground(textColor)

		chevron := lipgloss.NewStyle().Foreground(fc).Bold(true).Render("\u27E9")
		content := label + " " + chevron + " " + textStyle.Render(wrapped) + hint

		boxStyle := lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForegroundBlend(a, b, c).
			Padding(0, 1)

		result.WriteString(boxStyle.Render(content) + "\n")
	}

	return result.String()
}

// RenderPermissionMessage renders a permission request box.
func (at AgentTab) renderPermissionMessage(msg ChatMessage, contentW int) string {
	borderColor := ColorWarning
	switch msg.ToolStatus {
	case "success":
		borderColor = ColorSuccess
	case "cancelled":
		borderColor = ColorMuted
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).
		Width(contentW - 4)

	// Title.
	titleStyle := lipgloss.NewStyle().Foreground(borderColor).Bold(true)

	var content string
	switch msg.ToolStatus {
	case "pending":
		content = titleStyle.Render("Permission Required") + "\n"
		if strings.Contains(msg.ToolArgs, "\n") {
			content += ToolNameStyle.Render(msg.ToolName) + "\n" + ToolArgsStyle.Render(msg.ToolArgs) + "\n"
		} else {
			content += ToolNameStyle.Render(msg.ToolName) + " " + ToolArgsStyle.Render(msg.ToolArgs) + "\n"
		}
		content += "\n"
		approveKey := lipgloss.NewStyle().Foreground(ColorSuccess).Bold(true).Render("[y]")
		denyKey := lipgloss.NewStyle().Foreground(ColorError).Bold(true).Render("[n]")
		content += approveKey + MutedStyle.Render(" approve") + "    " + denyKey + MutedStyle.Render(" deny")
	case "success":
		icon := lipgloss.NewStyle().Foreground(ColorSuccess).Render("\u2713")
		content = icon + " " + ToolNameStyle.Render(msg.ToolName) + " " + ToolArgsStyle.Render(msg.ToolArgs)
	case "cancelled":
		icon := lipgloss.NewStyle().Foreground(ColorMuted).Render("\u25cf")
		content = icon + " " + ToolNameStyle.Render(msg.ToolName) + " " + ToolArgsStyle.Render(msg.ToolArgs)
	default:
		content = msg.Content
	}

	return boxStyle.Render(content) + "\n"
}

// RenderToolMessage renders a tool call in Claude Code style:
//
//	● ToolName(primary_arg)
//	  ⎿ Result summary
//	      1 first line
//	      2 second line
//	    ... +N lines (ctrl+o to expand)
func (at AgentTab) renderToolMessage(msg ChatMessage, msgIdx int, isLatest bool) string {
	frozen := !at.streaming || !isLatest

	var icon string
	var toolNameRendered string

	if frozen {
		// Frozen state: static ColorFrozen for icon and tool name.
		icon = lipgloss.NewStyle().Foreground(ColorFrozen).Render("✧")
		toolNameRendered = lipgloss.NewStyle().Foreground(ColorFrozen).Bold(true).Render(msg.ToolName)
	} else {
		// Animated state: scramble char icon + shimmer tool name.
		switch msg.ToolStatus {
		case "error":
			flameCh, flameHx := flameBlock(at.flameFrame)
			icon = lipgloss.NewStyle().Foreground(lipgloss.Color(flameHx)).Bold(true).Render(flameCh)
		default:
			icon = renderScrambleChar(at.flameFrame)
		}
		// Shimmer gradient across tool name text.
		toolNameRendered = renderToolShimmer(msg.ToolName, at.flameFrame)
	}

	header := icon + " " + toolNameRendered
	if msg.ToolArgs != "" {
		if strings.Contains(msg.ToolArgs, "\n") {
			// Multiline args (e.g. TodoWrite task list) - render below header.
			header += "\n" + ToolArgsStyle.Render(msg.ToolArgs)
		} else {
			header += ToolArgsStyle.Render("(" + msg.ToolArgs + ")")
		}
	}

	// Result line with connector.
	result := ""
	if msg.ToolStatus == "success" && msg.ToolBody != "" {
		var flavorColor color.Color
		if frozen {
			flavorColor = ColorFrozen
		} else {
			flavorColor = lipgloss.Color(flameColor(at.flameFrame))
		}
		flavorStyle := lipgloss.NewStyle().Foreground(flavorColor).Italic(true)
		resultPrefix := lipgloss.NewStyle().Foreground(flavorColor).Render("  \u2514 ")
		result = "\n" + resultPrefix + flavorStyle.Render(msg.ToolBody+"...")
	} else if msg.ToolStatus == "error" && msg.ToolBody != "" {
		resultPrefix := lipgloss.NewStyle().Foreground(ColorError).Render("  \u2514 ")
		result = "\n" + resultPrefix + lipgloss.NewStyle().Foreground(ColorError).Italic(true).Render(msg.ToolBody+"...")
	}

	// Expandable tool output when at.toolsExpanded[msgIdx] is true.
	if at.toolsExpanded[msgIdx] && msg.ToolOutput != "" {
		outputLines := strings.Split(msg.ToolOutput, "\n")
		maxLines := 20
		outputStyle := lipgloss.NewStyle().Foreground(ColorMuted)

		// Detect diff-like output for Edit/Write tools and color-code.
		isDiffLike := (msg.ToolName == "Edit" || msg.ToolName == "Write") && looksLikeDiff(msg.ToolOutput)
		addStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFD700"))
		delStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#e05050"))

		var preview strings.Builder
		for j, line := range outputLines {
			if j >= maxLines {
				preview.WriteString(outputStyle.Render(fmt.Sprintf("  ... +%d more lines", len(outputLines)-maxLines)))
				break
			}
			if isDiffLike && strings.HasPrefix(line, "+") {
				preview.WriteString(addStyle.Render("  "+line) + "\n")
			} else if isDiffLike && strings.HasPrefix(line, "-") {
				preview.WriteString(delStyle.Render("  "+line) + "\n")
			} else {
				preview.WriteString(outputStyle.Render("  "+line) + "\n")
			}
		}
		result += "\n" + preview.String()
	}

	return header + result + "\n"
}

// batchVerb returns an active/past-tense verb for a tool name batch.
func batchVerb(name string, count int, past bool) string {
	unit := "files"
	switch name {
	case "Read":
		if past {
			return fmt.Sprintf("Read %d %s", count, unit)
		}
		return fmt.Sprintf("Reading %d %s", count, unit)
	case "Write":
		if past {
			return fmt.Sprintf("Wrote %d %s", count, unit)
		}
		return fmt.Sprintf("Writing %d %s", count, unit)
	case "Edit":
		if past {
			return fmt.Sprintf("Edited %d %s", count, unit)
		}
		return fmt.Sprintf("Editing %d %s", count, unit)
	case "Glob":
		if past {
			return fmt.Sprintf("Scanned %d patterns", count)
		}
		return fmt.Sprintf("Scanning %d patterns", count)
	case "Grep":
		if past {
			return fmt.Sprintf("Searched %d queries", count)
		}
		return fmt.Sprintf("Searching %d queries", count)
	case "Bash":
		if past {
			return fmt.Sprintf("Executed %d commands", count)
		}
		return fmt.Sprintf("Executing %d commands", count)
	case "WebFetch":
		if past {
			return fmt.Sprintf("Fetched %d pages", count)
		}
		return fmt.Sprintf("Fetching %d pages", count)
	case "WebSearch":
		if past {
			return fmt.Sprintf("Searched %d queries", count)
		}
		return fmt.Sprintf("Searching %d queries", count)
	default:
		return fmt.Sprintf("%s x%d", name, count)
	}
}

// renderBatchToolHeader renders a "super tool" header for batched consecutive same-name tool calls.
// Animated while streaming (pulsating shimmer), frozen with past tense when done.
func (at AgentTab) renderBatchToolHeader(name string, count int, msgs []ChatMessage) string {
	var args []string
	for _, m := range msgs {
		if m.ToolArgs != "" {
			args = append(args, m.ToolArgs)
		}
	}

	frozen := !at.streaming

	if frozen {
		// Static frozen state: past tense, ColorFrozen, no animation.
		icon := lipgloss.NewStyle().Foreground(ColorFrozen).Bold(true).Render("✧✧")
		verb := batchVerb(name, count, true)
		verbStyle := lipgloss.NewStyle().Foreground(ColorFrozen).Bold(true)

		argsLine := ""
		if len(args) > 0 {
			joined := strings.Join(args, ", ")
			if len(joined) > 80 {
				joined = joined[:77] + "..."
			}
			connector := lipgloss.NewStyle().Foreground(ColorFrozen).Render("  ⎿ ")
			argsStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
			argsLine = "\n" + connector + argsStyle.Render(joined)
		}

		hint := lipgloss.NewStyle().Foreground(ColorMuted).Render("  [ctrl+o]")
		return icon + " " + verbStyle.Render(verb) + hint + argsLine + "\n"
	}

	// Animated state: active verb with pulsating shimmer.
	frame := at.flameFrame
	t := (math.Sin(float64(frame)*0.3) + 1.0) / 2.0
	// Blend from accent to lightened accent for pulsing super-tool color.
	superColor := lipgloss.Color(blendHex(ActiveTheme.Accent, lightenHex(ActiveTheme.Accent, 0.5), t))

	icon := lipgloss.NewStyle().Foreground(superColor).Bold(true).Render("✦✦")

	verb := batchVerb(name, count, false)
	var headerBuf strings.Builder
	for i, ch := range []rune(verb) {
		colorIdx := (frame*3 + i*2) % len(flameShimmerRamp)
		style := lipgloss.NewStyle().Foreground(flameShimmerRamp[colorIdx]).Bold(true)
		headerBuf.WriteString(style.Render(string(ch)))
	}

	argsLine := ""
	if len(args) > 0 {
		joined := strings.Join(args, ", ")
		if len(joined) > 80 {
			joined = joined[:77] + "..."
		}
		connector := lipgloss.NewStyle().Foreground(superColor).Render("  ⎿ ")
		argsStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
		argsLine = "\n" + connector + argsStyle.Render(joined)
	}

	hint := lipgloss.NewStyle().Foreground(ColorMuted).Render("  [ctrl+o]")
	return icon + " " + headerBuf.String() + hint + argsLine + "\n"
}

// RenderThinkingMessage renders a thinking indicator.
func (at AgentTab) renderThinkingMessage(msg ChatMessage) string {
	prefix := lipgloss.NewStyle().Foreground(ColorMuted).Render("\u2234")
	return ThinkingStyle.Render("  "+prefix+" "+msg.Content) + "\n"
}

func chatContentWidth(screenWidth int) int {
	w := (screenWidth * 80) / 100
	if w < 60 {
		w = 60
	}
	if w > 140 {
		w = 140
	}
	return w
}

func formatToolInput(input any) string {
	if input == nil {
		return ""
	}
	switch v := input.(type) {
	case string:
		return truncate(v, 60)
	case map[string]any:
		// Just the value, no key names for common tools.
		for _, key := range []string{"command", "query", "file_path", "pattern", "url", "path", "prompt"} {
			if val, ok := v[key]; ok {
				return truncate(fmt.Sprintf("%v", val), 60)
			}
		}
		for _, val := range v {
			return truncate(fmt.Sprintf("%v", val), 60)
		}
		return ""
	default:
		return truncate(fmt.Sprintf("%v", v), 60)
	}
}

// formatTodoWriteInput renders TodoWrite input as a styled task checklist.
func formatTodoWriteInput(input any) string {
	m, ok := input.(map[string]any)
	if !ok {
		return formatToolInput(input)
	}
	raw, err := json.Marshal(m["todos"])
	if err != nil {
		return formatToolInput(input)
	}
	var items []struct {
		Content    string `json:"content"`
		ActiveForm string `json:"activeForm"`
		Status     string `json:"status"`
	}
	if err := json.Unmarshal(raw, &items); err != nil || len(items) == 0 {
		return formatToolInput(input)
	}
	var lines []string
	for _, t := range items {
		icon := "○"
		switch t.Status {
		case "in_progress":
			icon = "◉"
		case "completed":
			icon = "✓"
		case "failed":
			icon = "✗"
		case "blocked":
			icon = "⊘"
		}
		label := t.Content
		if t.ActiveForm != "" && t.Status == "in_progress" {
			label = t.ActiveForm
		}
		lines = append(lines, fmt.Sprintf("  %s %s", icon, label))
	}
	return strings.Join(lines, "\n")
}

// formatAskUserInput renders AskUserQuestion input as a formatted question.
func formatAskUserInput(input any) string {
	m, ok := input.(map[string]any)
	if !ok {
		return formatToolInput(input)
	}
	raw, err := json.Marshal(m["questions"])
	if err != nil {
		return formatToolInput(input)
	}
	var questions []struct {
		Question string `json:"question"`
	}
	if err := json.Unmarshal(raw, &questions); err != nil || len(questions) == 0 {
		// Fallback: try top-level question field.
		if q, ok := m["question"].(string); ok {
			return truncate(q, 80)
		}
		return formatToolInput(input)
	}
	if len(questions) == 1 {
		return truncate(questions[0].Question, 80)
	}
	var lines []string
	for i, q := range questions {
		lines = append(lines, fmt.Sprintf("  %d. %s", i+1, truncate(q.Question, 76)))
	}
	return strings.Join(lines, "\n")
}

// formatToolArgs picks the best formatter for a tool's input display.
func formatToolArgs(toolName string, input any) string {
	switch toolName {
	case "TodoWrite":
		return formatTodoWriteInput(input)
	case "AskUserQuestion":
		return formatAskUserInput(input)
	case "Task", "Agent":
		return formatTaskInput(input)
	default:
		return formatToolInput(input)
	}
}

// formatTaskInput renders a styled subagent card for the Task/Agent tool.
func formatTaskInput(input any) string {
	raw, err := json.Marshal(input)
	if err != nil {
		return formatToolInput(input)
	}
	var ti struct {
		Description  string `json:"description"`
		SubagentType string `json:"subagent_type"`
		RunInBG      bool   `json:"run_in_background"`
	}
	if err := json.Unmarshal(raw, &ti); err != nil {
		return formatToolInput(input)
	}
	agentType := ti.SubagentType
	if agentType == "" {
		agentType = "general-purpose"
	}
	icon := "\u27C1" // ⟁
	if ti.RunInBG {
		icon = "\u27C1 (bg)"
	}
	return fmt.Sprintf("\n  %s %s [%s]", icon, ti.Description, agentType)
}

// convertTodosToTaskInfo maps tools.TodoItem slice to dashboard.TaskInfo slice.
// looksLikeDiff returns true if the text looks like unified diff output.
func looksLikeDiff(text string) bool {
	diffLines := 0
	for _, line := range strings.SplitN(text, "\n", 20) {
		if strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") {
			diffLines++
		}
	}
	return diffLines >= 2
}

func convertTodosToTaskInfo(items []engine.TodoItem) []dashboard.TaskInfo {
	out := make([]dashboard.TaskInfo, len(items))
	for i, item := range items {
		out[i] = dashboard.TaskInfo{
			Text:   item.Content,
			Status: item.Status,
		}
	}
	return out
}

// CalamityToolFlavor is the Calamity boss flavor text for tool completions.
var calamityToolFlavor = []string{
	"Providence has blessed this operation",
	"The Profaned Flame answered",
	"Holy fire consumed the task",
	"Divine judgment was rendered",
	"The goddess has spoken",
	"Profaned energy forged the result",
	"Sacred flames illuminated the path",
	"The Profaned Core delivered",
	"Consecrated by divine will",
	"Providence's light revealed all",
	"Immolated and reborn as data",
	"The holy flame burned through",
}

func randomToolFlavor() string {
	return calamityToolFlavor[rand.IntN(len(calamityToolFlavor))]
}

// vizVerbToPast converts a viz verb phrase to past tense.
// "Conjuring the flames" -> "Conjured the flames"
func vizVerbToPast(phrase string) string {
	parts := strings.SplitN(phrase, " ", 2)
	if len(parts) == 1 {
		return verbToPast(parts[0])
	}
	return verbToPast(parts[0]) + " " + parts[1]
}

// VerbToPast converts a spinner verb to past tense.
func verbToPast(verb string) string {
	// Handle -ying verbs (Purifying → Purified, Sanctifying → Sanctified).
	if strings.HasSuffix(verb, "ying") {
		return strings.TrimSuffix(verb, "ying") + "ied"
	}
	// Handle -ting verbs (Immolating → Immolated, Incinerating → Incinerated).
	if strings.HasSuffix(verb, "ting") {
		return strings.TrimSuffix(verb, "ing") + "ed"
	}
	// Default: strip -ing, add -ed.
	return strings.TrimSuffix(verb, "ing") + "ed"
}

func truncate(s string, max int) string {
	if lipgloss.Width(s) > max {
		// Trim rune-by-rune until display width fits.
		runes := []rune(s)
		for len(runes) > 0 && lipgloss.Width(string(runes)) > max-3 {
			runes = runes[:len(runes)-1]
		}
		return string(runes) + "..."
	}
	return s
}

// WordWrap wraps text at the given width on word boundaries.
// Uses lipgloss.Width for display-width measurement so ANSI escape codes
// and wide Unicode characters (CJK, emoji) don't inflate line lengths.
func wordWrap(text string, width int) string {
	if width <= 0 {
		return text
	}
	var result strings.Builder
	for _, paragraph := range strings.Split(text, "\n") {
		if result.Len() > 0 {
			result.WriteString("\n")
		}
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			continue
		}
		lineLen := 0
		for i, word := range words {
			wLen := lipgloss.Width(word)
			if i > 0 && lineLen+1+wLen > width {
				result.WriteString("\n")
				lineLen = 0
			} else if i > 0 {
				result.WriteString(" ")
				lineLen++
			}
			result.WriteString(word)
			lineLen += wLen
		}
	}
	return result.String()
}

// highlightSearchMatches wraps all case-insensitive occurrences of query in
// the rendered string with bold+underline styling for freeze-mode search.
// It operates on the plain-text portions of each line to avoid corrupting
// ANSI escape sequences mid-sequence.
func highlightSearchMatches(rendered, query string) string {
	if query == "" {
		return rendered
	}
	hl := lipgloss.NewStyle().Bold(true).Underline(true)
	lower := strings.ToLower(query)
	var out strings.Builder
	for i, line := range strings.Split(rendered, "\n") {
		if i > 0 {
			out.WriteByte('\n')
		}
		lowerLine := strings.ToLower(line)
		pos := 0
		for {
			idx := strings.Index(lowerLine[pos:], lower)
			if idx < 0 {
				out.WriteString(line[pos:])
				break
			}
			abs := pos + idx
			out.WriteString(line[pos:abs])
			out.WriteString(hl.Render(line[abs : abs+len(lower)]))
			pos = abs + len(lower)
		}
	}
	return out.String()
}

// --- Slash Commands ---

// availableModels is the UI view of the shared engine model catalog.
var availableModels = func() []struct {
	Name    string
	Aliases []string
	Desc    string
} {
	models := make([]struct {
		Name    string
		Aliases []string
		Desc    string
	}, 0, len(engine.ModelCatalog))

	for _, spec := range engine.ModelCatalog {
		display := spec.Display
		if display == "" {
			display = spec.Name
		}
		models = append(models, struct {
			Name    string
			Aliases []string
			Desc    string
		}{
			Name:    display,
			Aliases: spec.Aliases,
			Desc:    availableModelDescription(spec),
		})
	}

	return models
}()

func availableModelDescription(spec engine.ModelSpec) string {
	provider := spec.Provider
	switch spec.Provider {
	case "anthropic":
		provider = "Anthropic"
	case "openai":
		provider = "OpenAI"
	case "openrouter":
		provider = "OpenRouter"
	}

	switch spec.Tier {
	case engine.TierFast:
		return fmt.Sprintf("Fast tier via %s", provider)
	case engine.TierMedium:
		return fmt.Sprintf("Balanced tier via %s", provider)
	case engine.TierCapable:
		return fmt.Sprintf("Most capable tier via %s", provider)
	default:
		return fmt.Sprintf("Model via %s", provider)
	}
}

// resolveModelAlias resolves an alias or model name to the full model name.
// Returns the full name and true if found, or the original string and false if not.
func resolveModelAlias(input string) (string, bool) {
	if spec := engine.SpecFor(input); spec != nil {
		return engine.ResolveAlias(input), true
	}
	return engine.ResolveAlias(input), false
}

// trackToolFile extracts file path info from a tool_use event and updates the
// dashboard FILES panel. Supports Read, Write, Edit, Glob, Grep, Bash tools.
func (at *AgentTab) trackToolFile(toolName string, input any) {
	m, ok := input.(map[string]any)
	if !ok {
		return
	}
	switch toolName {
	case "Read":
		if p, ok := m["file_path"].(string); ok {
			at.dashboard.AddFile(p, "read")
		}
	case "Write":
		if p, ok := m["file_path"].(string); ok {
			at.dashboard.AddFile(p, "write")
		}
	case "Edit":
		if p, ok := m["file_path"].(string); ok {
			at.dashboard.AddFile(p, "edit")
		}
	}
}

func (at *AgentTab) handleSlashCommand(text string) (bool, tea.Cmd) {
	parts := strings.SplitN(text, " ", 2)
	cmd := strings.ToLower(parts[0])
	args := ""
	if len(parts) > 1 {
		args = parts[1]
	}

	switch cmd {
	case "/model":
		if args == "" {
			// Build model list as markdown for glamour rendering.
			var b strings.Builder
			b.WriteString("## Available Models\n\n")
			b.WriteString("| Model | Alias | Description |\n")
			b.WriteString("|-------|-------|-------------|\n")
			for _, m := range availableModels {
				alias := ""
				if len(m.Aliases) > 0 {
					alias = m.Aliases[0]
				}
				b.WriteString(fmt.Sprintf("| %s | %s | %s |\n", m.Name, alias, m.Desc))
			}
			b.WriteString("\n**Current:** " + at.modelDisplay() + "\n\n")
			b.WriteString("Use `/model <alias>` to switch (e.g. `/model haiku`)")
			at.messages = append(at.messages, ChatMessage{
				Role:    "assistant",
				Content: b.String(),
				Done:    true,
			})
		} else {
			resolved, ok := resolveModelAlias(args)
			at.model = resolved
			if ok {
				at.addSystemMessage("Model set to: " + resolved)
			} else {
				at.addSystemMessage("Model set to: " + resolved + " (unknown - using as-is)")
			}
			// Kill existing session so next message creates new one with new model.
			if at.engine != nil {
				at.engine.Close()
				at.engine = nil
			}
			// Persist to config.
			at.cfg.Model = resolved
			_ = at.cfg.Save()
		}
		at.refreshViewport()
		return true, nil
	case "/engine":
		if args == "" {
			at.addSystemMessage("Current engine: " + string(at.engineType) + "\nAvailable: claude, direct, codex_re")
		} else {
			newType := engine.EngineType(strings.TrimSpace(args))
			switch newType {
			case engine.EngineTypeClaude, engine.EngineTypeDirect, "codex_re":
				// Serialize current state for context portability.
				var portableState *engine.ConversationState
				if at.engine != nil {
					restored := at.messagesToRestored()
					state, err := engine.SerializeState(at.engine, restored, "", at.model, string(at.engineType))
					if err == nil {
						portableState = state
					}
					at.engine.Close()
					at.engine = nil
				}
				at.engineType = newType
				at.addSystemMessage("Engine set to: " + string(newType))
				// Apply per-engine banner text.
				if theme, ok := EngineThemes[string(newType)]; ok {
					SetBannerSubtitle(theme.BannerText + " - " + theme.Name)
				}
				// Restore conversation state into the new engine on next Send.
				if portableState != nil && len(portableState.Messages) > 0 {
					at.pendingPortableState = portableState
					at.addSystemMessage("Conversation context queued for handoff (" + strconv.Itoa(len(portableState.Messages)) + " messages)")
				}
				// Persist to config.
				at.cfg.Engine = string(newType)
				_ = at.cfg.Save()
			default:
				at.addSystemMessage("Unknown engine: " + args + " (valid: claude, direct, codex_re)")
			}
		}
		at.refreshViewport()
		return true, nil
	case "/auth":
		at.addSystemMessage("Opening browser for OpenAI login...")
		at.refreshViewport()
		return true, func() tea.Msg {
			tokens, err := auth.LoginOpenAI()
			if err != nil {
				return authCompleteMsg{Success: false, Message: "Login failed: " + err.Error()}
			}
			if err := auth.SaveOpenAITokens(tokens); err != nil {
				return authCompleteMsg{Success: false, Message: "Login OK but save failed: " + err.Error()}
			}
			acct := tokens.AccountID
			if acct == "" {
				acct = "(unknown)"
			}
			return authCompleteMsg{
				Success: true,
				Message: fmt.Sprintf("OpenAI login successful! Account: %s", acct),
			}
		}
	case "/theme":
		if args == "" {
			at.addSystemMessage("Current theme: " + currentThemeName)
			at.refreshViewport()
			return true, nil
		}
		switch args {
		case "flame", "night":
			ApplyTheme(args)
			at.mdRenderer, _ = glamour.NewTermRenderer(
				glamour.WithStyles(providenceGlamourStyle()),
				glamour.WithWordWrap(chatContentWidth(at.width)-4),
			)
			components.ReapplyTextAreaStyles(&at.input)
			at.messagesDirty = true
			at.addSystemMessage("Theme set to: " + args)
			// Persist to config.
			at.cfg.Theme = args
			_ = at.cfg.Save()
		case "auto":
			hour := time.Now().Hour()
			name := "flame"
			if hour < 6 || hour >= 18 {
				name = "night"
			}
			ApplyTheme(name)
			at.mdRenderer, _ = glamour.NewTermRenderer(
				glamour.WithStyles(providenceGlamourStyle()),
				glamour.WithWordWrap(chatContentWidth(at.width)-4),
			)
			components.ReapplyTextAreaStyles(&at.input)
			at.messagesDirty = true
			at.addSystemMessage("Theme set to auto (currently: " + name + ")")
			// Persist to config (store "auto" so the resolution re-runs next launch).
			at.cfg.Theme = "auto"
			_ = at.cfg.Save()
		default:
			at.addSystemMessage("Unknown theme: " + args + " (valid: flame, night, auto)")
		}
		at.refreshViewport()
		return true, nil
	case "/sessions":
		if at.store == nil {
			at.addSystemMessage("Session store not available")
			at.refreshViewport()
			return true, nil
		}
		// Search mode: /sessions <query>
		if args != "" {
			results, err := at.store.SearchMessages(args, 10)
			if err != nil {
				at.addSystemMessage("Search error: " + err.Error())
				at.refreshViewport()
				return true, nil
			}
			if len(results) == 0 {
				at.addSystemMessage("No results for '" + args + "'")
				at.refreshViewport()
				return true, nil
			}
			var lines []string
			for _, r := range results {
				sid := r.SessionID
				if len(sid) > 8 {
					sid = sid[:8]
				}
				lines = append(lines, fmt.Sprintf("[%s] %s: %s", sid, r.Role, truncate(r.Snippet, 60)))
			}
			at.addSystemMessage("Search results for '" + args + "':\n" + strings.Join(lines, "\n"))
			at.refreshViewport()
			return true, nil
		}
		cwd, _ := os.Getwd()
		sessions, err := at.store.ListSessions(cwd, 10)
		if err != nil {
			at.addSystemMessage("Failed to list sessions: " + err.Error())
			at.refreshViewport()
			return true, nil
		}
		if len(sessions) == 0 {
			at.addSystemMessage("No past sessions found")
			at.refreshViewport()
			return true, nil
		}
		var b strings.Builder
		b.WriteString("## Past Sessions\n\n")
		b.WriteString("| # | Title | Date | Messages |\n")
		b.WriteString("|---|-------|------|----------|\n")
		for i, s := range sessions {
			title := s.Title
			if title == "" {
				title = "(untitled)"
			}
			date := s.UpdatedAt.Format("Jan 02 15:04")
			b.WriteString(fmt.Sprintf("| %d | %s | %s | %d |\n", i+1, title, date, s.MessageCount))
		}
		b.WriteString("\nUse `/resume N` to restore a session")
		at.messages = append(at.messages, ChatMessage{
			Role:    "assistant",
			Content: b.String(),
			Done:    true,
		})
		at.refreshViewport()
		return true, nil

	case "/resume":
		if at.store == nil {
			at.addSystemMessage("Session store not available")
			at.refreshViewport()
			return true, nil
		}
		cwd, _ := os.Getwd()
		sessions, err := at.store.ListSessions(cwd, 10)
		if err != nil {
			at.addSystemMessage("Failed to list sessions: " + err.Error())
			at.refreshViewport()
			return true, nil
		}
		if len(sessions) == 0 {
			at.addSystemMessage("No past sessions found")
			at.refreshViewport()
			return true, nil
		}
		if args == "" {
			// Show sessions list with hint.
			var b strings.Builder
			b.WriteString("## Past Sessions\n\n")
			b.WriteString("| # | Title | Date | Messages |\n")
			b.WriteString("|---|-------|------|----------|\n")
			for i, s := range sessions {
				title := s.Title
				if title == "" {
					title = "(untitled)"
				}
				date := s.UpdatedAt.Format("Jan 02 15:04")
				b.WriteString(fmt.Sprintf("| %d | %s | %s | %d |\n", i+1, title, date, s.MessageCount))
			}
			b.WriteString("\nUse `/resume N` to restore a session")
			at.messages = append(at.messages, ChatMessage{
				Role:    "assistant",
				Content: b.String(),
				Done:    true,
			})
			at.refreshViewport()
			return true, nil
		}
		idx, err := strconv.Atoi(strings.TrimSpace(args))
		if err != nil || idx < 1 || idx > len(sessions) {
			at.addSystemMessage(fmt.Sprintf("Invalid session number. Use 1-%d", len(sessions)))
			at.refreshViewport()
			return true, nil
		}
		sess := sessions[idx-1]
		msgs, err := at.store.GetMessages(sess.ID)
		if err != nil {
			at.addSystemMessage("Failed to load messages: " + err.Error())
			at.refreshViewport()
			return true, nil
		}
		// Close current engine.
		if at.engine != nil {
			at.engine.Close()
			at.engine = nil
		}
		// Populate UI messages from loaded session and build the restored
		// engine history. Tool rows are paired with synthetic tool_call IDs so
		// engines that support proper tool_use/tool_result blocks (direct,
		// openrouter) can reconstruct the correct message structure instead of
		// falling back to flat text synthesis.
		at.messages = nil
		restored := make([]engine.RestoredMessage, 0, len(msgs))
		// pendingToolID tracks the synthetic call ID generated for the most
		// recent tool-bearing assistant turn so the following tool result can
		// reference the same ID.
		pendingToolIDs := make(map[string]string) // toolName -> callID
		for _, m := range msgs {
			at.messages = append(at.messages, ChatMessage{
				Role:       m.Role,
				Content:    m.Content,
				Done:       m.Done,
				ToolName:   m.ToolName,
				ToolArgs:   m.ToolArgs,
				ToolStatus: m.ToolStatus,
				ToolBody:   m.ToolBody,
				ToolOutput: m.ToolOutput,
				ImageCount: m.ImageCount,
			})

			restoredMessage := engine.RestoredMessage{
				Role:    m.Role,
				Content: m.Content,
			}
			switch m.Role {
			case "assistant":
				// If this assistant turn included a tool invocation, register a
				// synthetic call ID so the subsequent tool result can reference it.
				if m.ToolName != "" {
					callID := fmt.Sprintf("call_%s_%d", m.ToolName, len(restored))
					pendingToolIDs[m.ToolName] = callID
				}
			case "tool":
				// Pair this result with the most recently registered call ID for
				// its tool name. Fall back to a fresh synthetic ID if none exists.
				callID, ok := pendingToolIDs[m.ToolName]
				if !ok || callID == "" {
					callID = fmt.Sprintf("call_%s_%d", m.ToolName, len(restored))
				}
				delete(pendingToolIDs, m.ToolName)
				restoredMessage.ToolName = m.ToolName
				restoredMessage.ToolInput = m.ToolArgs
				restoredMessage.ToolCallID = callID
				restoredMessage.Content = m.ToolOutput
			}
			restored = append(restored, restoredMessage)
		}
		at.sessionID = sess.ID
		at.messagesDirty = true
		title := sess.Title
		if title == "" {
			title = "(untitled)"
		}
		at.addSystemMessage("Session restored: " + title)
		at.refreshViewport()
		// Spin up a fresh engine and rehydrate its history so the model
		// actually remembers this conversation on the next turn.
		return true, createEngineAndRestore(restored, at.model, at.engineType, at.cfg.OutputStyle)

	case "/compact":
		if at.engine == nil {
			at.addSystemMessage("No active session to compact")
			at.refreshViewport()
			return true, nil
		}

		eng := at.engine
		awaitEvents := at.engineType == engine.EngineTypeDirect
		return true, func() tea.Msg {
			return compactTriggerMsg{
				AwaitEvents: awaitEvents,
				Err:         eng.TriggerCompact(context.Background()),
			}
		}

	case "/rewind":
		// Build rewind items from user messages.
		var items []components.RewindItem
		for i, m := range at.messages {
			if m.Role == "user" {
				preview := m.Content
				if len(preview) > 80 {
					preview = preview[:77] + "..."
				}
				items = append(items, components.RewindItem{
					Role:    m.Role,
					Preview: preview,
					Index:   i,
				})
			}
		}
		if len(items) == 0 {
			at.addSystemMessage("No user messages to rewind to")
			at.refreshViewport()
			return true, nil
		}
		at.rewindModel = components.NewRewindModel(items, chatContentWidth(at.width))
		at.refreshViewport()
		return true, nil

	case "/dashboard":
		at.addSystemMessage("Tabs: 1-Chat 2-Agents 3-Tasks 4-Files 5-Tokens 6-Errors 7-Compact 8-Hooks\nPress number key to switch.")
		return true, nil

	case "/tree":
		at.treeViewOpen = !at.treeViewOpen
		at.messagesDirty = true
		if at.treeViewOpen {
			contentW := chatContentWidth(at.width)
			treeMessages := make([]tree.ChatMessage, len(at.messages))
			for i, m := range at.messages {
				treeMessages[i] = tree.ChatMessage{
					Role:       m.Role,
					Content:    m.Content,
					ToolName:   m.ToolName,
					ToolArgs:   m.ToolArgs,
					ToolOutput: m.ToolOutput,
					ToolStatus: m.ToolStatus,
					Done:       m.Done,
				}
			}
			treeColors := tree.ThemeColors{
				Primary:    ActiveTheme.Primary,
				Secondary:  ActiveTheme.Secondary,
				Accent:     ActiveTheme.Accent,
				Muted:      ActiveTheme.Muted,
				Text:       ActiveTheme.Text,
				Background: ActiveTheme.Background,
				Error:      ActiveTheme.Error,
			}
			treeStr := tree.RenderTree(tree.BuildTree(treeMessages), contentW, treeColors)
			at.addSystemMessage(treeStr)
		}
		at.refreshViewport()
		return true, nil

	case "/clear":
		if at.store != nil && at.sessionID != "" {
			at.store.DeleteSession(at.sessionID)
		}
		at.sessionID = ""
		at.messages = nil
		at.streamBuffer = ""
		at.pendingPerm = nil
		at.messagesDirty = true
		at.refreshViewport()
		return true, nil
	case "/kairos":
		if args == "" {
			// Toggle kairos mode.
			if at.kairos.ShouldTick() || at.kairos.Active {
				at.kairos.Deactivate()
				at.addSystemMessage("Kairos autonomous mode deactivated")
			} else {
				at.kairos.Activate()
				at.addSystemMessage("Kairos autonomous mode activated - ticks will fire after each turn")
			}
		} else {
			switch strings.TrimSpace(args) {
			case "status":
				at.addSystemMessage(at.kairos.Status())
			case "pause":
				at.kairos.Pause()
				at.addSystemMessage("Kairos paused - ticks suspended")
			case "resume":
				at.kairos.Resume()
				at.addSystemMessage("Kairos resumed - ticks active")
			default:
				at.addSystemMessage("Usage: /kairos [status|pause|resume]")
			}
		}
		at.refreshViewport()
		return true, nil

	case "/cost":
		ctxWindow := engine.ContextWindowFor(at.model)
		pct := 0
		if ctxWindow > 0 {
			pct = at.currentTokens * 100 / ctxWindow
		}
		msg := fmt.Sprintf("Session tokens: %d / %d (%d%%)\nTurns: %d",
			at.currentTokens, ctxWindow, pct, len(at.messages))
		at.addSystemMessage(msg)
		at.refreshViewport()
		return true, nil

	case "/doctor":
		var checks []string
		checks = append(checks, fmt.Sprintf("Go: %s", runtime.Version()))
		checks = append(checks, fmt.Sprintf("OS: %s/%s", runtime.GOOS, runtime.GOARCH))
		if os.Getenv("ANTHROPIC_API_KEY") != "" {
			checks = append(checks, "Anthropic API: set")
		} else {
			checks = append(checks, "Anthropic API: NOT SET")
		}
		if _, err := auth.LoadOpenAITokens(); err == nil {
			checks = append(checks, "OpenAI OAuth: valid")
		} else {
			checks = append(checks, "OpenAI OAuth: not configured")
		}
		checks = append(checks, fmt.Sprintf("Engine: %s", at.engineType))
		checks = append(checks, fmt.Sprintf("Model: %s", at.model))
		at.addSystemMessage(strings.Join(checks, "\n"))
		at.refreshViewport()
		return true, nil

	case "/stats":
		userMsgs := 0
		assistantMsgs := 0
		toolCalls := 0
		for _, m := range at.messages {
			switch m.Role {
			case "user":
				userMsgs++
			case "assistant":
				assistantMsgs++
			case "tool":
				toolCalls++
			}
		}
		msg := fmt.Sprintf("Messages: %d user, %d assistant, %d tool calls\nTokens: %d\nSession: %s",
			userMsgs, assistantMsgs, toolCalls, at.currentTokens, at.sessionID)
		at.addSystemMessage(msg)
		at.refreshViewport()
		return true, nil

	case "/effort":
		if args == "" {
			at.addSystemMessage("Usage: /effort low|medium|high\nCurrent: " + at.cfg.Effort)
			at.refreshViewport()
			return true, nil
		}
		at.cfg.Effort = args
		_ = at.cfg.Save()
		at.addSystemMessage("Effort set to: " + args)
		at.refreshViewport()
		return true, nil

	case "/rename":
		if args == "" {
			at.addSystemMessage("Usage: /rename <title>")
			at.refreshViewport()
			return true, nil
		}
		if at.store != nil && at.sessionID != "" {
			at.store.UpdateSessionTitle(at.sessionID, args)
		}
		at.addSystemMessage("Session renamed to: " + args)
		at.refreshViewport()
		return true, nil

	case "/skills":
		cwd, _ := os.Getwd()
		home, _ := os.UserHomeDir()
		skillList, _ := skills.LoadSkills(cwd, home)
		if len(skillList) == 0 {
			at.addSystemMessage("No skills found")
			at.refreshViewport()
			return true, nil
		}
		var lines []string
		for _, s := range skillList {
			lines = append(lines, fmt.Sprintf("/%s - %s", s.Name, s.Description))
		}
		at.addSystemMessage(strings.Join(lines, "\n"))
		at.refreshViewport()
		return true, nil

	case "/agents":
		var lines []string
		for name, agent := range subagent.BuiltinAgents {
			lines = append(lines, fmt.Sprintf("%s - %s (model: %s)", name, agent.Description, agent.Model))
		}
		if len(lines) == 0 {
			at.addSystemMessage("No built-in agents registered")
		} else {
			at.addSystemMessage("Built-in agents:\n" + strings.Join(lines, "\n"))
		}
		at.refreshViewport()
		return true, nil

	case "/permissions":
		if args == "" {
			mode := at.permissionMode
			if mode == "" {
				mode = "default"
			}
			at.addSystemMessage(fmt.Sprintf("Permission mode: %s\n\nUsage: /permissions <mode>\nModes: default, acceptEdits, plan, bypassPermissions, dontAsk\n\nSwitch mode: shift+tab", mode))
			at.refreshViewport()
			return true, nil
		}
		validModes := map[string]bool{"default": true, "acceptEdits": true, "plan": true, "bypassPermissions": true, "dontAsk": true}
		if !validModes[args] {
			at.addSystemMessage("Invalid mode: " + args + "\nValid: default, acceptEdits, plan, bypassPermissions, dontAsk")
			at.refreshViewport()
			return true, nil
		}
		at.permissionMode = args
		at.addSystemMessage("Permission mode set to: " + args)
		at.refreshViewport()
		return true, nil

	case "/hooks":
		at.addSystemMessage("Hooks: configured via ~/.claude/settings.json\nSee /help for hook events")
		at.refreshViewport()
		return true, nil

	case "/diff":
		out, err := exec.Command("git", "diff", "--stat").Output()
		if err != nil {
			at.addSystemMessage("No git changes")
		} else {
			at.addSystemMessage(string(out))
		}
		at.refreshViewport()
		return true, nil

	case "/branch":
		out, _ := exec.Command("git", "branch", "-v").Output()
		at.addSystemMessage(string(out))
		at.refreshViewport()
		return true, nil

	case "/share":
		if at.store == nil || at.sessionID == "" {
			at.addSystemMessage("No active session to share.")
			at.refreshViewport()
			return true, nil
		}
		msgs, err := at.store.GetMessages(at.sessionID)
		if err != nil {
			at.addSystemMessage("Error: " + err.Error())
			at.refreshViewport()
			return true, nil
		}
		exportID := at.sessionID
		if len(exportID) > 8 {
			exportID = exportID[:8]
		}
		exportPath := fmt.Sprintf("/tmp/providence-session-%s.jsonl", exportID)
		f, err := os.Create(exportPath)
		if err != nil {
			at.addSystemMessage("Error: " + err.Error())
			at.refreshViewport()
			return true, nil
		}
		enc := json.NewEncoder(f)
		for _, m := range msgs {
			enc.Encode(m)
		}
		f.Close()
		at.addSystemMessage("Session exported to: " + exportPath)
		at.refreshViewport()
		return true, nil

	case "/review":
		de, ok := at.engine.(*direct.DirectEngine)
		if !ok || de.SubagentRunner() == nil {
			at.addSystemMessage("Code review requires native engine with subagent support")
			at.refreshViewport()
			return true, nil
		}
		reviewPrompt := "Review recent code changes in this project. Check for bugs, style issues, security problems, and adherence to AGENTS.md conventions. Report findings with file:line references."
		if args != "" {
			reviewPrompt = args
		}
		input := subagent.TaskInput{
			Description:  "Code review",
			Prompt:       reviewPrompt,
			SubagentType: "Code-Reviewer",
			RunInBG:      true,
		}
		agentType, _ := subagent.ResolveAgentType("Code-Reviewer", at.customAgents)
		agentID, err := de.SubagentRunner().Spawn(context.Background(), input, agentType, de.SubagentExecutor())
		if err != nil {
			at.addSystemMessage("Review failed: " + err.Error())
			at.refreshViewport()
			return true, nil
		}
		at.addSystemMessage("Code review started: " + agentID)
		at.refreshViewport()
		return true, nil

	case "/fork":
		n := 1
		if args != "" {
			if parsed, err := strconv.Atoi(strings.Fields(args)[0]); err == nil && parsed > 0 {
				n = parsed
			}
		}
		if at.engine == nil {
			at.addSystemMessage("No active engine for /fork")
			at.refreshViewport()
			return true, nil
		}
		de, ok := at.engine.(*direct.DirectEngine)
		if !ok {
			at.addSystemMessage("/fork requires native engine")
			at.refreshViewport()
			return true, nil
		}
		if de.SubagentRunner() == nil {
			at.addSystemMessage("/fork: subagent runner not initialized")
			at.refreshViewport()
			return true, nil
		}

		// Serialize current conversation for context inheritance.
		convState := at.serializeConversationState()

		for i := 0; i < n; i++ {
			input := subagent.TaskInput{
				Description: fmt.Sprintf("Fork %d of %d", i+1, n),
				Prompt:      "Continue the current task with full conversation context.",
				RunInBG:     true,
				Name:        fmt.Sprintf("fork-%d", i+1),
			}
			agentID, err := de.SubagentRunner().SpawnWithContext(context.Background(), input, subagent.DefaultAgentType(), de.SubagentContextExecutor(), convState)
			if err != nil {
				at.addSystemMessage(fmt.Sprintf("Fork %d failed: %s", i+1, err))
				continue
			}
			at.addSystemMessage(fmt.Sprintf("Fork %d spawned: %s (with full context)", i+1, agentID))
		}
		at.refreshViewport()
		return true, nil

	case "/init":
		cwd, err := os.Getwd()
		if err != nil {
			at.addSystemMessage("Could not determine working directory: " + err.Error())
			at.refreshViewport()
			return true, nil
		}
		// Check if CLAUDE.md already exists.
		candidatePaths := []string{
			filepath.Join(cwd, ".claude", "CLAUDE.md"),
			filepath.Join(cwd, "CLAUDE.md"),
		}
		for _, p := range candidatePaths {
			if _, err := os.Stat(p); err == nil {
				at.addSystemMessage("CLAUDE.md already exists. Edit it directly or delete to reinit.")
				at.refreshViewport()
				return true, nil
			}
		}

		// Scan the project to detect language/framework.
		detected := "Unknown project"
		buildCmd := ""
		testCmd := ""

		if _, err := os.Stat(filepath.Join(cwd, "go.mod")); err == nil {
			data, _ := os.ReadFile(filepath.Join(cwd, "go.mod"))
			modLine := ""
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "module ") {
					modLine = strings.TrimPrefix(line, "module ")
					break
				}
			}
			if modLine != "" {
				detected = "Go project (module: " + modLine + ")"
			} else {
				detected = "Go project"
			}
			buildCmd = "go build ./..."
			testCmd = "go test -race -count=1 ./..."
		} else if _, err := os.Stat(filepath.Join(cwd, "package.json")); err == nil {
			data, _ := os.ReadFile(filepath.Join(cwd, "package.json"))
			pkg := struct {
				Name string `json:"name"`
			}{}
			_ = json.Unmarshal(data, &pkg)
			if pkg.Name != "" {
				detected = "Node.js project (name: " + pkg.Name + ")"
			} else {
				detected = "Node.js project"
			}
			buildCmd = "npm run build"
			testCmd = "npm test"
		} else if _, err := os.Stat(filepath.Join(cwd, "Cargo.toml")); err == nil {
			detected = "Rust project"
			buildCmd = "cargo build"
			testCmd = "cargo test"
		} else if _, err := os.Stat(filepath.Join(cwd, "pyproject.toml")); err == nil {
			detected = "Python project"
			buildCmd = "pip install -e ."
			testCmd = "pytest"
		} else if _, err := os.Stat(filepath.Join(cwd, "requirements.txt")); err == nil {
			detected = "Python project"
			buildCmd = "pip install -r requirements.txt"
			testCmd = "pytest"
		}

		// Check for .git.
		gitInfo := ""
		if _, err := os.Stat(filepath.Join(cwd, ".git")); err == nil {
			gitInfo = " (git repo)"
		}

		// List top-level directories.
		entries, _ := os.ReadDir(cwd)
		var dirs []string
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				dirs = append(dirs, e.Name())
			}
		}
		dirList := ""
		if len(dirs) > 0 {
			dirList = strings.Join(dirs, ", ")
		}

		// Build CLAUDE.md content.
		var b strings.Builder
		b.WriteString("# Project Instructions\n\n")
		b.WriteString("## Overview\n\n")
		b.WriteString("Detected: " + detected + gitInfo + "\n")
		if dirList != "" {
			b.WriteString("Directories: " + dirList + "\n")
		}
		b.WriteString("\n## Conventions\n\n")
		b.WriteString("- [placeholder for user to fill]\n\n")
		b.WriteString("## Build & Test\n\n")
		b.WriteString("```\n")
		if buildCmd != "" {
			b.WriteString(buildCmd + "\n")
		}
		if testCmd != "" {
			b.WriteString(testCmd + "\n")
		}
		if buildCmd == "" && testCmd == "" {
			b.WriteString("# Add build and test commands here\n")
		}
		b.WriteString("```\n")

		claudeMD := filepath.Join(cwd, "CLAUDE.md")
		if err := os.WriteFile(claudeMD, []byte(b.String()), 0o644); err != nil {
			at.addSystemMessage("Failed to write CLAUDE.md: " + err.Error())
			at.refreshViewport()
			return true, nil
		}
		at.addSystemMessage("Created CLAUDE.md with detected project info. Edit to customize.")
		at.refreshViewport()
		return true, nil

	case "/help":
		// Store as markdown - will be rendered by glamour in renderAssistantMessage.
		help := "## Available Commands\n\n"
		help += "| Command | Description |\n"
		help += "|---------|-------------|\n"
		for _, c := range slashCommands {
			help += fmt.Sprintf("| `%s` | %s |\n", c.Name, c.Desc)
		}
		at.messages = append(at.messages, ChatMessage{
			Role:    "assistant",
			Content: help,
			Done:    true,
		})
		at.refreshViewport()
		return true, nil
	}
	return false, nil
}

func (at AgentTab) modelDisplay() string {
	if at.model == "" {
		return "default"
	}
	if spec := engine.SpecFor(at.model); spec != nil && spec.Display != "" {
		return spec.Display
	}
	return at.model
}

// serializeConversationState converts the UI message history into a portable
// subagent.ConversationState for context inheritance in /fork.
func (at *AgentTab) serializeConversationState() *subagent.ConversationState {
	var msgs []subagent.PortableMessage
	for _, m := range at.messages {
		switch m.Role {
		case "user", "assistant":
			if m.Content == "" {
				continue
			}
			msgs = append(msgs, subagent.PortableMessage{
				Role:    m.Role,
				Content: m.Content,
			})
		case "tool":
			// Flatten tool results into assistant-visible text.
			text := fmt.Sprintf("[Tool: %s] %s", m.ToolName, m.ToolBody)
			if m.ToolOutput != "" {
				text = fmt.Sprintf("[Tool: %s] %s", m.ToolName, m.ToolOutput)
			}
			msgs = append(msgs, subagent.PortableMessage{
				Role:    "assistant",
				Content: text,
			})
		}
	}

	return &subagent.ConversationState{
		Messages:     msgs,
		SystemPrompt: "", // child uses its own system prompt
		Model:        at.model,
		Engine:       string(at.engineType),
	}
}

// drainCompletedSubagents checks the subagent runner (if available) for any
// background agents that completed since the last tick and injects a system
// message notification for each one, plus steers the result into the engine.
func (at *AgentTab) drainCompletedSubagents() {
	de, ok := at.engine.(*direct.DirectEngine)
	if !ok || de == nil {
		return
	}
	runner := de.SubagentRunner()
	if runner == nil {
		return
	}
	if at.notifiedAgents == nil {
		at.notifiedAgents = make(map[string]bool)
	}
	for _, agent := range runner.List() {
		if agent.Status != "completed" && agent.Status != "failed" {
			continue
		}
		if at.notifiedAgents[agent.ID] {
			continue
		}
		at.notifiedAgents[agent.ID] = true
		if agent.Result == nil {
			continue
		}
		// Inject a notification into the transcript.
		label := agent.Name
		if label == "" {
			label = agent.ID
		}
		if agent.Result.Status == "completed" {
			summary := agent.Result.Result
			// Wrap result text to a reasonable width so it doesn't overflow the chat pane.
			contentW := chatContentWidth(at.width)
			if contentW > 20 {
				summary = wordWrap(summary, contentW-4)
			}
			if len(summary) > 500 {
				summary = summary[:500] + "..."
			}
			at.addSystemMessage(fmt.Sprintf("Background agent %s completed:\n%s", label, summary))
			// Steer the full result into the engine so the model sees it.
			de.Steer(fmt.Sprintf("[background agent %s completed]\n%s", label, agent.Result.Result))
		} else {
			at.addSystemMessage(fmt.Sprintf("Background agent %s failed: %s", label, agent.Result.Result))
			de.Steer(fmt.Sprintf("[background agent %s failed]\n%s", label, agent.Result.Result))
		}
		at.messagesDirty = true
	}
}

// --- Commands ---

// WaitForEvent returns a Cmd that reads the next event from the session channel.
func waitForEvent(events <-chan engine.ParsedEvent) tea.Cmd {
	if events == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-events
		if !ok {
			return AgentEventMsg{Event: engine.ParsedEvent{Type: "closed"}}
		}
		return AgentEventMsg{Event: ev}
	}
}

// SafeWaitForEvent returns a waitForEvent cmd only if session is non-nil.
func (at AgentTab) safeWaitForEvent() tea.Cmd {
	if at.engine == nil {
		return nil
	}
	return waitForEvent(at.engine.Events())
}

// engineCreatedMsg carries the new engine and the initial prompt to send.
type engineCreatedMsg struct {
	engine engine.Engine
	prompt string
}

// engineRestoredMsg carries a new engine that has been pre-populated with
// restored history. Unlike engineCreatedMsg it does not trigger a Send - the
// engine is simply installed on the tab and waits for the next user turn.
type engineRestoredMsg struct {
	engine engine.Engine
	err    error
}

// isCodexModel returns true if the model resolves to an OpenAI/Codex model.
// OpenRouter models (e.g. "openai/gpt-5.4") are routed separately and must
// NOT match here, otherwise they'd be sent to the Codex endpoint.
func isCodexModel(model string) bool {
	spec := engine.SpecFor(model)
	return spec != nil && spec.Provider == "openai"
}

// isOpenRouterModel returns true if the model resolves to an OpenRouter
// catalog entry, whether it was provided as a canonical slug or alias.
func isOpenRouterModel(model string) bool {
	spec := engine.SpecFor(model)
	return spec != nil && spec.Provider == "openrouter"
}

// buildSystemPromptWithStyle builds the system prompt, prepends the active
// output style if configured, and appends discovered CLAUDE.md/AGENTS.md
// instruction files plus system reminders.
func buildSystemPromptWithStyle(outputStyleName string) string {
	base := engine.BuildSystemPrompt(nil)

	// Prepend output style if configured.
	if outputStyleName != "" {
		cwd, _ := os.Getwd()
		home, _ := os.UserHomeDir()
		styles, err := outputstyles.LoadOutputStyles(cwd, home)
		if err == nil {
			for _, s := range styles {
				if s.Name == outputStyleName && s.Prompt != "" {
					base = s.Prompt + "\n\n" + base
					break
				}
			}
		}
	}

	// Discover and inject CLAUDE.md, AGENTS.md, .claude/rules/*.md.
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	instructionFiles := engine.DiscoverInstructionFiles(cwd, home)
	if injection := engine.FormatInstructionInjection(instructionFiles); injection != "" {
		base = base + "\n\n" + injection
	}

	// Append system reminders (date, plan mode, etc).
	reminders := engine.BuildSystemReminders(engine.ReminderState{})
	if reminders != "" {
		base = base + "\n\n" + reminders
	}

	return base
}

// createEngineAndSend spawns a new engine session and sends the first prompt.
func createEngineAndSend(prompt, model string, engineType engine.EngineType, outputStyle string) tea.Cmd {
	return func() tea.Msg {
		// Allowed tools differ by engine type.
		var allowedTools []string
		if engineType == engine.EngineTypeClaude {
			allowedTools = []string{"Read", "Write", "Edit", "Bash", "Glob", "Grep", "WebSearch", "WebFetch"}
		}
		// Direct engine builds tools into the registry itself, so empty allowed tools.

		wd, _ := os.Getwd()

		cfg := engine.EngineConfig{
			Type:         engineType,
			SystemPrompt: buildSystemPromptWithStyle(outputStyle),
			AllowedTools: allowedTools,
			Model:        model,
			APIKey:       os.Getenv("ANTHROPIC_API_KEY"),
			WorkDir:      wd,
		}

		// Detect codex models and configure OpenAI provider.
		if isCodexModel(model) {
			cfg.Type = engine.EngineTypeDirect // codex runs through direct engine
			cfg.Provider = "openai"
			// Load tokens - they'll be checked per-request in the engine.
			if tokens, err := auth.EnsureValidOpenAITokens(); err == nil {
				cfg.OpenAIAccessToken = tokens.AccessToken
				cfg.OpenAIAccountID = tokens.AccountID
			}
		}

		// Detect OpenRouter models ("provider/model" slugs) and configure
		// the openrouter provider. Key comes from env first, then config.
		if isOpenRouterModel(model) {
			cfg.Type = engine.EngineTypeDirect
			cfg.Provider = engine.ProviderOpenRouter
			cfg.OpenRouterAPIKey = os.Getenv("OPENROUTER_API_KEY")
		}

		eng, err := engine.NewEngine(cfg)
		if err != nil {
			return AgentErrorMsg{Err: fmt.Errorf("failed to create session: %w", err)}
		}
		return engineCreatedMsg{engine: eng, prompt: prompt}
	}
}

// createEngineAndRestore spawns a new engine session and immediately
// populates its history with the supplied restored messages. The engine is
// left idle and ready for the next Send. Used by /resume so the model
// actually remembers the prior conversation.
func createEngineAndRestore(restored []engine.RestoredMessage, model string, engineType engine.EngineType, outputStyle string) tea.Cmd {
	return func() tea.Msg {
		var allowedTools []string
		if engineType == engine.EngineTypeClaude {
			allowedTools = []string{"Read", "Write", "Edit", "Bash", "Glob", "Grep", "WebSearch", "WebFetch"}
		}

		wd, _ := os.Getwd()

		cfg := engine.EngineConfig{
			Type:         engineType,
			SystemPrompt: buildSystemPromptWithStyle(outputStyle),
			AllowedTools: allowedTools,
			Model:        model,
			APIKey:       os.Getenv("ANTHROPIC_API_KEY"),
			WorkDir:      wd,
		}

		if isCodexModel(model) {
			cfg.Type = engine.EngineTypeDirect
			cfg.Provider = "openai"
			if tokens, err := auth.EnsureValidOpenAITokens(); err == nil {
				cfg.OpenAIAccessToken = tokens.AccessToken
				cfg.OpenAIAccountID = tokens.AccountID
			}
		}

		eng, err := engine.NewEngine(cfg)
		if err != nil {
			return engineRestoredMsg{err: fmt.Errorf("failed to create session: %w", err)}
		}
		if err := eng.RestoreHistory(restored); err != nil {
			eng.Close()
			return engineRestoredMsg{err: fmt.Errorf("failed to restore history: %w", err)}
		}
		return engineRestoredMsg{engine: eng}
	}
}

// handleEngineCreated is called from Update when an engineCreatedMsg arrives.
func (at *AgentTab) handleEngineCreated(msg engineCreatedMsg) tea.Cmd {
	at.engine = msg.engine
	at.currentTokens = 0
	at.compacting = false
	// Transfer any pending images to the newly created engine.
	at.transferImagesToEngine()
	// Restore portable state from a prior engine switch if available.
	if at.pendingPortableState != nil {
		if err := engine.RestoreState(at.engine, at.pendingPortableState); err != nil {
			at.addSystemMessage("Context restore warning: " + err.Error())
		}
		at.pendingPortableState = nil
	}
	if err := at.engine.Send(msg.prompt); err != nil {
		at.addSystemMessage(fmt.Sprintf("send error: %s", err))
		at.streaming = false
		at.refreshViewport()
		return nil
	}
	return at.safeWaitForEvent()
}
