package ui

import (
	"fmt"
	"image/color"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"time"

	"math"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"

	"github.com/charmbracelet/harmonica"
	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/gravitrone/providence-core/internal/engine/claude"
	"github.com/gravitrone/providence-core/internal/ui/components"
)

// completionSpring is a critically-damped spring for the completion cool-down animation.
// FPS(12) matches the flame tick rate (~80ms). Quick settle, no oscillation.
var completionSpring = harmonica.NewSpring(harmonica.FPS(12), 6.0, 0.8)

// queuedSpring is a slightly underdamped spring for the queued message hover effect.
// Gentle bounce that overshoots a bit, creating a "held in suspension" feel.
var queuedSpring = harmonica.NewSpring(harmonica.FPS(12), 5.0, 0.4)

// --- Agent Tab Messages ---

// AgentEventMsg wraps a parsed event from the Claude headless session.
type AgentEventMsg struct {
	Event engine.ParsedEvent
}

// AgentErrorMsg signals a session-level error (failed to create session, etc).
type AgentErrorMsg struct {
	Err error
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
}

// --- Agent Tab ---

// slashCommands defines the available slash commands for the preview typeahead.
var slashCommands = []struct {
	Name string
	Desc string
}{
	{"/model", "Switch model (Haiku, Sonnet, Opus)"},
	{"/clear", "Clear chat history"},
	{"/help", "Show available commands"},
}

// QueuedMessage represents a single message in the queue with its steering state.
type QueuedMessage struct {
	Text    string
	Steered bool // true = priority, sends first when turn finishes
}

// AgentTab implements the TabModel interface for the agent chat UI.
type AgentTab struct {
	width, height int
	input         textinput.Model
	viewport      viewport.Model
	messages      []ChatMessage
	session       *claude.Session
	streaming     bool
	streamBuffer  string
	pendingPerm   *claude.PermissionRequestEvent
	mdRenderer    *glamour.TermRenderer
	follow        bool
	model         string
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

	// Queued message spring animation state (harmonica-driven hover bounce).
	queuedBright float64
	queuedVel    float64
}

// NewAgentTab creates and returns a new AgentTab.
func NewAgentTab() AgentTab {
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
	ti := components.NewProvidenceTextInput(placeholders[rand.IntN(len(placeholders))])
	ti.Prompt = "\u27E9 "
	ti.Focus()

	vp := components.NewProvidenceViewport(80, 20)

	mr, _ := glamour.NewTermRenderer(
		glamour.WithStyles(providenceGlamourStyle()),
		glamour.WithWordWrap(76),
	)

	return AgentTab{
		input:       ti,
		viewport:    vp,
		messages:    nil,
		follow:      true,
		mdRenderer:  mr,
		queueCursor: -1,
	}
}

// Init implements TabModel.
func (at AgentTab) Init() tea.Cmd {
	return flameTick()
}

// Resize updates the tab dimensions and recreates the glamour renderer.
func (at *AgentTab) Resize(width, height int) {
	at.width = width
	at.height = height

	contentW := chatContentWidth(width)
	inputH := 1
	dividerH := 1
	vpH := height - inputH - dividerH - 1
	if vpH < 3 {
		vpH = 3
	}

	at.viewport.SetWidth(contentW)
	at.viewport.SetHeight(vpH)

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

	case sessionCreatedMsg:
		cmd := at.handleSessionCreated(msg)
		return at, cmd
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
			if at.session != nil && optionID != "" {
				_ = at.session.RespondPermission(perm.QuestionID, optionID)
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
			if at.session != nil && optionID != "" {
				_ = at.session.RespondPermission(perm.QuestionID, optionID)
			}
			at.updateLastPermissionStatus("cancelled")
			at.refreshViewport()
			return at, at.safeWaitForEvent()
		}
		return at, nil
	}

	switch key {
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
		var cmd tea.Cmd
		at.viewport, cmd = at.viewport.Update(msg)
		if at.viewport.AtBottom() {
			at.follow = true
		}
		return at, cmd
	case "shift+enter":
		// Add message directly as steered (priority, sends first).
		text := strings.TrimSpace(at.input.Value())
		if text == "" {
			return at, nil
		}
		at.input.SetValue("")
		at.queueCursor = -1
		if at.streaming {
			at.queue = append(at.queue, QueuedMessage{Text: text, Steered: true})
			at.queuedBright = 0.5
			at.queuedVel = 0.0
			at.refreshViewport()
			return at, nil
		}
		at.prepareSend(text)
		return at, at.sendCmd(text)

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
		if text == "" {
			return at, nil
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
		at.messages = append(at.messages, ChatMessage{
			Role:    "user",
			Content: text,
			Done:    true,
		})
		at.messagesDirty = true
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
		if at.session == nil {
			return at, tea.Batch(createSessionAndSend(text, at.model), spinnerTick())
		}

		// Send to existing session.
		if err := at.session.Send(text); err != nil {
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
		return at, cmd

	case "ctrl+l":
		if !at.streaming {
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
		return at, cmd
	}
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

	case "stream_event":
		if se, ok := ev.Data.(*claude.StreamEvent); ok {
			if se.Event.Delta != nil && se.Event.Delta.Type == "text_delta" {
				at.streamBuffer += se.Event.Delta.Text
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

	case "assistant":
		if ae, ok := ev.Data.(*claude.AssistantEvent); ok {
			var fullText string
			for _, part := range ae.Message.Content {
				switch part.Type {
				case "text":
					fullText += part.Text
				case "tool_use":
					at.messages = append(at.messages, ChatMessage{
						Role:       "tool",
						ToolName:   part.Name,
						ToolArgs:   formatToolInput(part.Input),
						ToolStatus: "success",
						ToolBody:   randomToolFlavor(),
						Done:       true,
					})
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
			at.streamBuffer = ""
			at.refreshViewport()
		}

		return at, at.safeWaitForEvent()

	case "permission_request":
		if pr, ok := ev.Data.(*claude.PermissionRequestEvent); ok {
			at.pendingPerm = pr
			toolName := pr.Tool.Name
			toolArgs := formatToolInput(pr.Tool.Input)
			at.messages = append(at.messages, ChatMessage{
				Role:       "permission",
				Content:    fmt.Sprintf("%s: %s", toolName, toolArgs),
				Done:       true,
				ToolName:   toolName,
				ToolArgs:   toolArgs,
				ToolStatus: "pending",
			})
			at.refreshViewport()
		}
		return at, nil

	case "result":
		elapsed := int(time.Since(at.spinnerStart).Seconds())
		verb := at.spinnerVerb
		at.streaming = false
		at.streamBuffer = ""
		at.spinnerVerb = ""
		at.follow = false
		at.messagesDirty = true
		if re, ok := ev.Data.(*claude.ResultEvent); ok {
			if re.IsError {
				at.addSystemMessage(fmt.Sprintf("Error: %s", re.Result))
			}
		}
		if verb != "" && elapsed > 0 {
			past := verbToPast(verb)
			completionMsg := fmt.Sprintf("%s for %ds", past, elapsed)
			at.addSystemMessage(completionMsg)
			// Start completion spring animation: bright gold -> frozen ember.
			at.completionText = completionMsg
			at.completionActive = true
			at.completionBright = 1.0
			at.completionVel = 0.0
		}

		// Drain queue: steered messages all at once, queued one per turn.
		if len(at.queue) > 0 && at.session != nil {
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
			if err := at.session.Send(text); err != nil {
				at.addSystemMessage(fmt.Sprintf("send error: %s", err))
				at.streaming = false
				at.refreshViewport()
				return at, nil
			}
			return at, tea.Batch(at.safeWaitForEvent(), spinnerTick())
		}

		at.refreshViewport()
		return at, nil

	case "closed":
		at.streaming = false
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

	contentW := chatContentWidth(width)

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

	// Calculate left padding to center the content block.
	pad := (width - contentW) / 2
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

	// Input, padded.
	inputLine := leftPad + at.input.View()

	return "\n" + vpPadded.String() + "\n" + divider + "\n" + previewSection + inputLine
}

// renderCommandPreview returns the slash command suggestions if input starts with "/".
func (at AgentTab) renderCommandPreview(contentW int) string {
	val := at.input.Value()
	if !strings.HasPrefix(val, "/") {
		return ""
	}

	prefix := strings.ToLower(val)
	var matches []struct {
		Name string
		Desc string
	}
	for _, cmd := range slashCommands {
		if strings.HasPrefix(cmd.Name, prefix) {
			matches = append(matches, cmd)
		}
	}
	if len(matches) == 0 {
		return ""
	}

	cmdStyle := lipgloss.NewStyle().Foreground(ColorPrimary)
	descStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	var b strings.Builder
	for i, m := range matches {
		if i > 0 {
			b.WriteString("\n")
		}
		// Fixed 12-char width for command name.
		name := m.Name
		for len(name) < 12 {
			name += " "
		}
		b.WriteString("  " + cmdStyle.Render(name) + descStyle.Render(m.Desc))
	}
	return b.String()
}

// Hints returns context-dependent status bar hints.
// Only shows during special states - idle mode has no hints (clean).
func (at AgentTab) Hints() []components.HintItem {
	if at.pendingPerm != nil {
		return []components.HintItem{
			{Key: "y", Desc: "approve"},
			{Key: "n", Desc: "deny"},
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
	// No hints in idle/streaming - status line handles it.
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

// StatusLine shows model/session/cwd as hint-bar-style bordered pills.
func (at AgentTab) StatusLine() string {
	modelName := at.modelDisplay()
	if modelName == "default" {
		modelName = "sonnet"
	}

	session := "idle"
	if at.session != nil {
		if at.streaming {
			session = "streaming"
		} else {
			session = "active"
		}
	}

	items := []components.HintItem{
		{Key: modelName, Desc: "model"},
		{Key: session, Desc: "session"},
	}
	return components.StatusBarFromItems(items, 0)
}

// --- Internal Helpers ---

// PrepareSend sets up state for sending a message. Call before sendCmd.
func (at *AgentTab) prepareSend(text string) {
	at.messages = append(at.messages, ChatMessage{
		Role:    "user",
		Content: text,
		Done:    true,
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

// SendCmd returns the tea.Cmd to send a message (create session or send to existing).
func (at AgentTab) sendCmd(text string) tea.Cmd {
	if at.session == nil {
		return createSessionAndSend(text, at.model)
	}
	if err := at.session.Send(text); err != nil {
		return nil
	}
	return tea.Batch(at.safeWaitForEvent(), spinnerTick())
}

func (at *AgentTab) addMessage(role, content string, done bool) {
	at.messages = append(at.messages, ChatMessage{
		Role:    role,
		Content: content,
		Done:    done,
	})
	at.messagesDirty = true
}

func (at *AgentTab) addSystemMessage(content string) {
	at.addMessage("system", content, true)
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

	// Messages re-render when dirty, streaming, or animating (completion spring).
	// When idle with no changes, use the cached render to avoid flicker.
	if at.messagesDirty || at.streaming || at.completionActive || at.cachedMessages == "" {
		at.cachedMessages = at.renderMessages()
		at.messagesDirty = false
	}

	content := banner + "\n" + at.cachedMessages
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

	for i, msg := range at.messages {
		// Skip empty assistant messages (can happen during streaming setup).
		if msg.Role == "assistant" && strings.TrimSpace(msg.Content) == "" && msg.Done {
			continue
		}

		if i > 0 {
			b.WriteString("\n")
		}

		switch msg.Role {
		case "user":
			b.WriteString(at.renderUserMessage(msg, contentW, i == lastUserIdx))
		case "assistant":
			rendered := at.renderAssistantMessage(msg)
			if rendered != "" {
				b.WriteString(rendered)
			}
		case "system":
			b.WriteString(at.renderSystemMessage(msg))
		case "permission":
			b.WriteString(at.renderPermissionMessage(msg, contentW))
		case "tool":
			b.WriteString(at.renderToolMessage(msg, i == lastToolIdx))
		case "thinking":
			b.WriteString(at.renderThinkingMessage(msg))
		}
	}

	// Render queued messages above the spinner if any exist.
	if len(at.queue) > 0 {
		b.WriteString("\n" + at.renderQueuedMessages(contentW))
	}

	// Append spinner below the last message when streaming.
	if at.streaming {
		spinner := at.renderSpinner()
		if spinner != "" {
			b.WriteString("\n" + spinner + "\n")
		}
	}

	return b.String()
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
		boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForegroundBlend(
				lipgloss.Color("#6b3a1a"),
				ColorFrozen,
				lipgloss.Color("#6b3a1a"),
			).
			Padding(0, 1)
	}

	textStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#e0d0c0")).Bold(true)

	// ❯ prefix in flame color (animated when streaming, frozen otherwise).
	var prefixHex string
	if at.streaming && isLatest {
		prefixHex = flameColor(at.flameFrame)
	} else {
		prefixHex = "#A0704A"
	}
	prefix := lipgloss.NewStyle().Foreground(lipgloss.Color(prefixHex)).Bold(true).Render("\u27E9 ")

	wrapW := contentW - 10
	if wrapW < 20 {
		wrapW = 20
	}
	text := wordWrap(msg.Content, wrapW)

	return boxStyle.Render(prefix+textStyle.Render(text)) + "\n"
}

// RenderAssistantMessage renders assistant text with arrow prefix and indent.
// Done messages get glamour markdown rendering.
func (at AgentTab) renderAssistantMessage(msg ChatMessage) string {
	arrowStyle := lipgloss.NewStyle().Foreground(ColorSecondary)
	indent := "  " // same width as "↳ " prefix

	if msg.Done && at.mdRenderer != nil {
		// Extract viz blocks, replace with placeholders, render markdown, then swap viz back in.
		content, vizRendered := ExtractAndRenderVizBlocks(msg.Content, at.width-4)
		rendered, err := at.mdRenderer.Render(content)
		if err == nil {
			for placeholder, vizOutput := range vizRendered {
				rendered = strings.ReplaceAll(rendered, placeholder, vizOutput)
			}
		}
		if err == nil {
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
			return b.String()
		}
	}

	// Streaming - raw text with cursor.
	text := strings.TrimSpace(msg.Content)
	if text == "" {
		return ""
	}
	var b strings.Builder
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if i == 0 {
			b.WriteString(arrowStyle.Bold(true).Render("✦ ") + line + "\n")
		} else {
			b.WriteString(indent + line + "\n")
		}
	}
	if !msg.Done {
		s := b.String()
		s = strings.TrimRight(s, "\n")
		s += MutedStyle.Render("\u258d") + "\n"
		return s
	}
	return b.String()
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
		var textColor string
		if msg.Steered {
			textColor = "#FFD700" // Bright gold for steered.
			label = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#0a0a0a")).
				Background(lipgloss.Color(flameColor(at.flameFrame))).
				Bold(true).
				Padding(0, 1).
				Render("Steer")
			if isSelected {
				hint = "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("#888")).Italic(true).
					Render("  already steered  del: remove")
			}
			// Hotter gradient for steered: shift further into bright range.
			a = flameGradientStops[(offset+1)%len(flameGradientStops)]
			b = flameGradientStops[(offset+3)%len(flameGradientStops)]
			c = flameGradientStops[(offset+5)%len(flameGradientStops)]
		} else {
			textColor = "#FFA600" // Amber for queued.
			label = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#0a0a0a")).
				Background(lipgloss.Color(flameColor(at.flameFrame))).
				Bold(true).
				Padding(0, 1).
				Render("Queue")
			if isSelected {
				hint = "\n" + lipgloss.NewStyle().Foreground(fc).Italic(true).
					Render("  enter: steer  del: remove")
			}
		}
		textStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(textColor))

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
		content += ToolNameStyle.Render(msg.ToolName) + " " + ToolArgsStyle.Render(msg.ToolArgs) + "\n"
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
func (at AgentTab) renderToolMessage(msg ChatMessage, isLatest bool) string {
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
		header += ToolArgsStyle.Render("(" + msg.ToolArgs + ")")
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

	return header + result + "\n"
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
		// Just the value, no key names.
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
	if len(s) > max {
		return s[:max-3] + "..."
	}
	return s
}

// WordWrap wraps text at the given width on word boundaries.
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
			wLen := len(word)
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

// --- Slash Commands ---

// availableModels is the hardcoded list of supported Claude models with aliases.
var availableModels = []struct {
	Name    string
	Aliases []string
	Desc    string
}{
	{"claude-sonnet-4-6", []string{"sonnet"}, "Fast + capable (default)"},
	{"claude-opus-4-6", []string{"opus"}, "Most capable, slower"},
	{"claude-haiku-4-5-20251001", []string{"haiku"}, "Fastest, cheapest"},
}

// resolveModelAlias resolves an alias or model name to the full model name.
// Returns the full name and true if found, or the original string and false if not.
func resolveModelAlias(input string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(input))
	for _, m := range availableModels {
		if strings.ToLower(m.Name) == lower {
			return m.Name, true
		}
		for _, alias := range m.Aliases {
			if alias == lower {
				return m.Name, true
			}
		}
	}
	return input, false
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
			if at.session != nil {
				at.session.Close()
				at.session = nil
			}
		}
		at.refreshViewport()
		return true, nil
	case "/clear":
		at.messages = nil
		at.streamBuffer = ""
		at.pendingPerm = nil
		at.messagesDirty = true
		at.refreshViewport()
		return true, nil
	case "/help":
		// Store as markdown - will be rendered by glamour in renderAssistantMessage.
		help := "## Available Commands\n\n"
		help += "| Command | Description |\n"
		help += "|---------|-------------|\n"
		help += "| `/model <name>` | Switch model (Haiku, Sonnet, Opus) |\n"
		help += "| `/clear` | Clear chat history |\n"
		help += "| `/help` | Show available commands |"
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
	return at.model
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
	if at.session == nil {
		return nil
	}
	return waitForEvent(at.session.Events())
}

// SessionCreatedMsg carries the new session and the initial prompt to send.
type sessionCreatedMsg struct {
	session *claude.Session
	prompt  string
}

// CreateSessionAndSend spawns a new Claude session and sends the first prompt.
func createSessionAndSend(prompt, model string) tea.Cmd {
	return func() tea.Msg {
		systemPrompt := engine.BuildSystemPrompt(nil)
		sess, err := claude.NewSession(systemPrompt, []string{
			"Read", "Write", "Edit", "Bash", "Glob", "Grep", "WebSearch", "WebFetch",
		}, model)
		if err != nil {
			return AgentErrorMsg{Err: fmt.Errorf("failed to create session: %w", err)}
		}
		return sessionCreatedMsg{session: sess, prompt: prompt}
	}
}

// HandleSessionCreated is called from Update when a sessionCreatedMsg arrives.
func (at *AgentTab) handleSessionCreated(msg sessionCreatedMsg) tea.Cmd {
	at.session = msg.session
	if err := at.session.Send(msg.prompt); err != nil {
		at.addSystemMessage(fmt.Sprintf("send error: %s", err))
		at.streaming = false
		at.refreshViewport()
		return nil
	}
	return at.safeWaitForEvent()
}
