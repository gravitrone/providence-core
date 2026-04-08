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
	"github.com/gravitrone/providence-core/internal/auth"
	"github.com/gravitrone/providence-core/internal/config"
	"github.com/gravitrone/providence-core/internal/engine"
	_ "github.com/gravitrone/providence-core/internal/engine/claude"  // register claude factory
	"github.com/gravitrone/providence-core/internal/engine/direct"  // register direct factory + image types
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

// authCompleteMsg signals that the OpenAI OAuth flow completed.
type authCompleteMsg struct {
	Success bool
	Message string
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
	Expanded   bool // whether this tool's result is shown expanded
	ImageCount int  // number of images attached to this message
}

// --- Agent Tab ---

// slashCommands defines the available slash commands for the preview typeahead.
var slashCommands = []struct {
	Name string
	Desc string
}{
	{"/model", "Switch model (Haiku, Sonnet, Opus, Codex)"},
	{"/engine", "Switch engine (claude, direct)"},
	{"/image", "Attach image file (png, jpg, gif, webp)"},
	{"/theme", "Switch theme (flame, night, auto)"},
	{"/auth", "Login to OpenAI (Codex OAuth)"},
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
	engine        engine.Engine
	engineType    engine.EngineType
	streaming     bool
	streamBuffer  string
	pendingPerm   *engine.PermissionRequestEvent
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

	// Viz state: tracks when a viz block is being streamed.
	visualizing bool
	vizVerb     string
	vizCount    int // number of completed viz blocks this turn

	// Queued message spring animation state (harmonica-driven hover bounce).
	queuedBright float64
	queuedVel    float64

	// Tool expansion: ctrl+o toggles all tool results visible.
	toolsExpanded bool

	// Pending image attachments for next message.
	pendingImages []ImageAttachment

	// Persisted user config.
	cfg config.Config
}

// NewAgentTab creates and returns a new AgentTab.
// engineType overrides the default engine; pass "" for the default (claude).
func NewAgentTab(engineType engine.EngineType, cfg config.Config) AgentTab {
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
	ti := components.NewProvidenceTextInput(placeholders[rand.IntN(len(placeholders))])
	ti.Prompt = "\u27E9 "
	ti.Focus()

	vp := components.NewProvidenceViewport(80, 20)

	mr, _ := glamour.NewTermRenderer(
		glamour.WithStyles(providenceGlamourStyle()),
		glamour.WithWordWrap(76),
	)

	model := cfg.Model

	return AgentTab{
		input:       ti,
		viewport:    vp,
		messages:    nil,
		follow:      true,
		mdRenderer:  mr,
		queueCursor: -1,
		engineType:  engineType,
		model:       model,
		cfg:         cfg,
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

	case engineCreatedMsg:
		cmd := at.handleEngineCreated(msg)
		return at, cmd

	case authCompleteMsg:
		at.addSystemMessage(msg.Message)
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
		imgCount := len(at.pendingImages)
		at.messages = append(at.messages, ChatMessage{
			Role:       "user",
			Content:    text,
			Done:       true,
			ImageCount: imgCount,
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
		if at.engine == nil {
			// Images will be transferred after engine creation via handleEngineCreated.
			return at, tea.Batch(createEngineAndSend(text, at.model, at.engineType), spinnerTick())
		}

		// Transfer images to engine before sending.
		at.transferImagesToEngine()

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
		return at, cmd

	case "ctrl+i":
		// Paste image from clipboard (macOS only).
		return at, func() tea.Msg {
			data, err := readClipboardImage()
			return clipboardImageMsg{Data: data, Err: err}
		}

	case "ctrl+o":
		at.toolsExpanded = !at.toolsExpanded
		at.messagesDirty = true
		at.refreshViewport()
		return at, nil

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

	case "assistant":
		if ae, ok := ev.Data.(*engine.AssistantEvent); ok {
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
		if pr, ok := ev.Data.(*engine.PermissionRequestEvent); ok {
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
			if err := at.engine.Send(text); err != nil {
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
	// Show ctrl+o hint when there are tool messages.
	for _, msg := range at.messages {
		if msg.Role == "tool" {
			desc := "expand tools"
			if at.toolsExpanded {
				desc = "collapse tools"
			}
			return []components.HintItem{
				{Key: "ctrl+o", Desc: desc},
			}
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
	if at.engine != nil {
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
		return createEngineAndSend(text, at.model, at.engineType)
	}
	if err := at.engine.Send(text); err != nil {
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
	hasBatch := at.streaming && !at.toolsExpanded && at.hasBatchTools()
	if at.messagesDirty || at.streaming || at.completionActive || hasViz || hasBatch || at.cachedMessages == "" {
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
		if msg.Role == "tool" && batchSkip[i] && !at.toolsExpanded {
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
			if g, ok := batchStart[i]; ok && !at.toolsExpanded {
				// Collect messages for this batch by indices.
				batchMsgs := make([]ChatMessage, len(g.indices))
				for bi, idx := range g.indices {
					batchMsgs[bi] = at.messages[idx]
				}
				b.WriteString(at.renderBatchToolHeader(g.name, g.count, batchMsgs))
			} else {
				b.WriteString(at.renderToolMessage(msg, i == lastToolIdx))
			}
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

	// Expandable tool output when at.toolsExpanded is true.
	if at.toolsExpanded && msg.ToolOutput != "" {
		outputLines := strings.Split(msg.ToolOutput, "\n")
		maxLines := 20
		outputStyle := lipgloss.NewStyle().Foreground(ColorMuted)

		var preview strings.Builder
		for j, line := range outputLines {
			if j >= maxLines {
				preview.WriteString(outputStyle.Render(fmt.Sprintf("  ... +%d more lines", len(outputLines)-maxLines)))
				break
			}
			preview.WriteString(outputStyle.Render("  "+line) + "\n")
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
	{"gpt-5.4", []string{"codex", "gpt5"}, "GPT-5.4 via Codex (ChatGPT sub)"},
	{"gpt-5.4-mini", []string{"codex-mini", "gpt5-mini"}, "GPT-5.4 Mini via Codex"},
	{"gpt-5.3-codex", []string{"codex-5.3"}, "GPT-5.3 Codex"},
	{"gpt-5.2-codex", []string{"codex-5.2"}, "GPT-5.2 Codex"},
	{"gpt-5.2", []string{"gpt5.2"}, "GPT-5.2 via Codex"},
	{"gpt-5.1-codex-max", []string{"codex-max", "codex-5.1-max"}, "GPT-5.1 Codex Max"},
	{"gpt-5.1-codex-mini", []string{"codex-5.1", "codex-5.1-mini"}, "GPT-5.1 Codex Mini"},
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
			at.addSystemMessage("Current engine: " + string(at.engineType) + "\nAvailable: claude, direct")
		} else {
			newType := engine.EngineType(strings.TrimSpace(args))
			switch newType {
			case engine.EngineTypeClaude, engine.EngineTypeDirect:
				if at.engine != nil {
					at.engine.Close()
					at.engine = nil
				}
				at.engineType = newType
				at.addSystemMessage("Engine set to: " + string(newType))
				// Persist to config.
				at.cfg.Engine = string(newType)
				_ = at.cfg.Save()
			default:
				at.addSystemMessage("Unknown engine: " + args + " (valid: claude, direct)")
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
			// Recreate glamour renderer with new theme colors.
			at.mdRenderer, _ = glamour.NewTermRenderer(
				glamour.WithStyles(providenceGlamourStyle()),
				glamour.WithWordWrap(chatContentWidth(at.width)-4),
			)
			at.messagesDirty = true
			at.addSystemMessage("Theme set to: " + args)
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
			at.messagesDirty = true
			at.addSystemMessage("Theme set to auto (currently: " + name + ")")
		default:
			at.addSystemMessage("Unknown theme: " + args + " (valid: flame, night, auto)")
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
		help += "| `/model <name>` | Switch model (Haiku, Sonnet, Opus, Codex) |\n"
		help += "| `/engine <type>` | Switch engine (claude, direct) |\n"
		help += "| `/theme <name>` | Switch theme (flame, night, auto) |\n"
		help += "| `/auth` | Login to OpenAI (Codex OAuth) |\n"
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

// isCodexModel returns true if the model name indicates an OpenAI/Codex model.
func isCodexModel(model string) bool {
	return strings.HasPrefix(model, "gpt-")
}

// createEngineAndSend spawns a new engine session and sends the first prompt.
func createEngineAndSend(prompt, model string, engineType engine.EngineType) tea.Cmd {
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
			SystemPrompt: engine.BuildSystemPrompt(nil),
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

		eng, err := engine.NewEngine(cfg)
		if err != nil {
			return AgentErrorMsg{Err: fmt.Errorf("failed to create session: %w", err)}
		}
		return engineCreatedMsg{engine: eng, prompt: prompt}
	}
}

// handleEngineCreated is called from Update when an engineCreatedMsg arrives.
func (at *AgentTab) handleEngineCreated(msg engineCreatedMsg) tea.Cmd {
	at.engine = msg.engine
	// Transfer any pending images to the newly created engine.
	at.transferImagesToEngine()
	if err := at.engine.Send(msg.prompt); err != nil {
		at.addSystemMessage(fmt.Sprintf("send error: %s", err))
		at.streaming = false
		at.refreshViewport()
		return nil
	}
	return at.safeWaitForEvent()
}
