package sidebar

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/charmbracelet/harmonica"
	"github.com/gravitrone/providence-core/internal/engine/subagent"
)

// --- Constants ---

// EvictAfter is how long a completed agent lingers before auto-removal.
const EvictAfter = 30 * time.Second

// --- Sidebar Model ---

// Sidebar manages the left-hand agent panel. It tracks agent cards, focus
// state, the detail view, and the send-message mini-input.
type Sidebar struct {
	Agents  []AgentCard
	Focused bool
	Cursor  int

	// Detail view: expanded transcript of the selected agent.
	Expanded     bool
	DetailScroll int

	// Send message mini-input state.
	SendActive bool
	SendBuffer string

	// Slide animation: 0.0 = hidden, 1.0 = fully visible.
	Position float64
	PosVel   float64
	Spring   harmonica.Spring

	// Theme colors (set by the parent TUI on theme change).
	PrimaryHex   string
	SecondaryHex string
	MutedHex     string
	TextHex      string
	BorderHex    string
	CardHex      string
	SuccessHex   string
	ErrorHex     string
}

// AgentCard is the sidebar representation of a running/completed agent.
type AgentCard struct {
	ID        string
	Name      string
	Type      string
	Model     string
	Status    string // running, completed, failed, killed
	Activity  string // last tool call description
	ToolCount int
	Started   time.Time
	Completed time.Time
	Result    string // summary when completed
	Tokens    int
	ToolCalls []ToolEntry // transcript for detail view
}

// ToolEntry is a single tool call in the detail transcript.
type ToolEntry struct {
	Name   string
	Args   string
	Status string // success, error, pending
}

// --- Constructor ---

// New creates a Sidebar with sane defaults.
func New() Sidebar {
	return Sidebar{
		Spring:       harmonica.NewSpring(harmonica.FPS(12), 6.0, 0.8),
		PrimaryHex:   "#FFA600",
		SecondaryHex: "#D77757",
		MutedHex:     "#6b5040",
		TextHex:      "#e0d0c0",
		BorderHex:    "#3a2518",
		CardHex:      "#1a1210",
		SuccessHex:   "#19FA19",
		ErrorHex:     "#ff5555",
	}
}

// --- Public API ---

// HasAgents returns true if any agents are being tracked.
func (s *Sidebar) HasAgents() bool {
	return len(s.Agents) > 0
}

// FocusSidebar enters sidebar focus mode, placing cursor on the first agent.
func (s *Sidebar) FocusSidebar() {
	s.Focused = true
	s.Cursor = 0
	s.SendActive = false
	s.SendBuffer = ""
}

// Unfocus exits sidebar focus, collapsing the detail view.
func (s *Sidebar) Unfocus() {
	s.Focused = false
	s.Expanded = false
	s.DetailScroll = 0
	s.SendActive = false
	s.SendBuffer = ""
}

// SelectedAgent returns the agent under the cursor, or nil if none.
func (s *Sidebar) SelectedAgent() *AgentCard {
	if s.Cursor < 0 || s.Cursor >= len(s.Agents) {
		return nil
	}
	return &s.Agents[s.Cursor]
}

// SelectedAgentID returns the ID of the agent under the cursor, or "".
func (s *Sidebar) SelectedAgentID() string {
	if a := s.SelectedAgent(); a != nil {
		return a.ID
	}
	return ""
}

// HandleKey processes a key press when the sidebar is focused.
// Returns an action string: "kill", "send", "expand", "unfocus", or "".
func (s *Sidebar) HandleKey(key string) string {
	if len(s.Agents) == 0 {
		s.Unfocus()
		return "unfocus"
	}

	// Mini-input mode: typing a message to send.
	if s.SendActive {
		return s.handleSendKey(key)
	}

	// Detail view scrolling.
	if s.Expanded {
		return s.handleDetailKey(key)
	}

	switch key {
	case "up", "k":
		s.Cursor--
		if s.Cursor < 0 {
			s.Cursor = len(s.Agents) - 1
		}
		return ""

	case "down", "j":
		s.Cursor++
		if s.Cursor >= len(s.Agents) {
			s.Cursor = 0
		}
		return ""

	case "right", "esc", "q":
		s.Unfocus()
		return "unfocus"

	case "enter", "e":
		s.Expanded = true
		s.DetailScroll = 0
		return "expand"

	case "x":
		if a := s.SelectedAgent(); a != nil && a.Status == "running" {
			return "kill"
		}
		return ""

	case "s":
		if a := s.SelectedAgent(); a != nil && a.Status == "running" {
			s.SendActive = true
			s.SendBuffer = ""
			return ""
		}
		return ""
	}

	return ""
}

// handleSendKey processes keys while the send mini-input is active.
func (s *Sidebar) handleSendKey(key string) string {
	switch key {
	case "esc":
		s.SendActive = false
		s.SendBuffer = ""
		return ""
	case "enter":
		if strings.TrimSpace(s.SendBuffer) != "" {
			s.SendActive = false
			return "send"
		}
		return ""
	case "backspace":
		if len(s.SendBuffer) > 0 {
			s.SendBuffer = s.SendBuffer[:len(s.SendBuffer)-1]
		}
		return ""
	default:
		// Single printable character.
		if len(key) == 1 && key[0] >= 32 && key[0] < 127 {
			s.SendBuffer += key
		}
		return ""
	}
}

// handleDetailKey processes keys while the detail view is expanded.
func (s *Sidebar) handleDetailKey(key string) string {
	switch key {
	case "esc", "q":
		s.Expanded = false
		s.DetailScroll = 0
		return ""
	case "up", "k":
		if s.DetailScroll > 0 {
			s.DetailScroll--
		}
		return ""
	case "down", "j":
		s.DetailScroll++
		return ""
	}
	return ""
}

// SendMessage returns the current send buffer content and clears it.
func (s *Sidebar) SendMessage() string {
	msg := strings.TrimSpace(s.SendBuffer)
	s.SendBuffer = ""
	return msg
}

// --- Sync with Runner ---

// Sync updates the sidebar's agent list from the subagent runner's state.
// Call this each tick to keep the sidebar current.
func (s *Sidebar) Sync(agents []*subagent.RunningAgent) {
	existing := make(map[string]int, len(s.Agents))
	for i, a := range s.Agents {
		existing[a.ID] = i
	}

	for _, ra := range agents {
		if idx, ok := existing[ra.ID]; ok {
			// Update existing card.
			s.Agents[idx].Status = ra.Status
			if !ra.CompletedAt.IsZero() {
				s.Agents[idx].Completed = ra.CompletedAt
			}
			if ra.Result != nil {
				s.Agents[idx].Result = truncate(ra.Result.Result, 120)
				s.Agents[idx].Tokens = ra.Result.TotalTokens
				s.Agents[idx].ToolCount = ra.Result.ToolUses
			}
			delete(existing, ra.ID)
		} else {
			// New agent.
			card := AgentCard{
				ID:      ra.ID,
				Name:    ra.Name,
				Type:    ra.Type,
				Status:  ra.Status,
				Started: ra.StartedAt,
			}
			if !ra.CompletedAt.IsZero() {
				card.Completed = ra.CompletedAt
			}
			if ra.Result != nil {
				card.Result = truncate(ra.Result.Result, 120)
				card.Tokens = ra.Result.TotalTokens
				card.ToolCount = ra.Result.ToolUses
			}
			s.Agents = append(s.Agents, card)
		}
	}

	// Clamp cursor after sync.
	if s.Cursor >= len(s.Agents) {
		s.Cursor = len(s.Agents) - 1
	}
	if s.Cursor < 0 && len(s.Agents) > 0 {
		s.Cursor = 0
	}
}

// --- Tick & Eviction ---

// Tick advances the slide animation spring and evicts stale agents.
// Returns true if the sidebar should be hidden (all agents evicted, animation done).
func (s *Sidebar) Tick() bool {
	// Target position: 1.0 if agents exist, 0.0 if empty.
	target := 0.0
	if len(s.Agents) > 0 {
		target = 1.0
	}
	s.Position, s.PosVel = s.Spring.Update(s.Position, s.PosVel, target)

	// Evict completed agents after EvictAfter, unless being viewed.
	now := time.Now()
	var kept []AgentCard
	for i, a := range s.Agents {
		if a.Status != "running" && !a.Completed.IsZero() && now.Sub(a.Completed) > EvictAfter {
			// Don't evict if user is viewing this agent.
			if s.Focused && s.Cursor == i {
				kept = append(kept, a)
				continue
			}
			if s.Expanded && s.Cursor == i {
				kept = append(kept, a)
				continue
			}
			continue // evict
		}
		kept = append(kept, a)
	}
	s.Agents = kept

	// Clamp cursor.
	if s.Cursor >= len(s.Agents) {
		s.Cursor = len(s.Agents) - 1
	}
	if s.Cursor < 0 && len(s.Agents) > 0 {
		s.Cursor = 0
	}

	// Return true when sidebar should fully hide.
	return len(s.Agents) == 0 && s.Position < 0.01
}

// --- View ---

// View renders the sidebar at the given width and height.
// flameFrame is the global animation frame for consistent shimmer.
func (s *Sidebar) View(width, height, flameFrame int) string {
	if width < 8 || height < 3 {
		return ""
	}

	contentW := width - 2 // border padding

	// Header.
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(s.SecondaryHex)).
		Bold(true)
	header := headerStyle.Render("Agents")

	// Hint line.
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(s.MutedHex))
	hint := ""
	if s.Focused {
		hint = hintStyle.Render("[j/k] nav  [enter] expand  [esc] back")
	} else {
		hint = hintStyle.Render("[" + "\u2190" + "] focus sidebar")
	}

	lines := []string{header, hint, ""}
	usedH := 3

	// Render each agent card.
	for i, a := range s.Agents {
		if usedH >= height-2 {
			break
		}
		card := s.renderCard(a, i == s.Cursor && s.Focused, contentW, flameFrame)
		cardLines := strings.Split(card, "\n")
		lines = append(lines, cardLines...)
		lines = append(lines, "")
		usedH += len(cardLines) + 1
	}

	// Send mini-input at the bottom.
	if s.SendActive {
		inputStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(s.PrimaryHex)).
			Bold(true)
		inputLine := inputStyle.Render("> ") + s.SendBuffer + "_"
		// Pad to fill remaining height.
		for usedH < height-2 {
			lines = append(lines, "")
			usedH++
		}
		lines = append(lines, inputLine)
	}

	content := strings.Join(lines, "\n")

	// Wrap in a border.
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(s.BorderHex)).
		Width(width - 2).
		Height(height - 2)

	return borderStyle.Render(content)
}

// renderCard renders a single agent card line.
func (s *Sidebar) renderCard(a AgentCard, selected bool, width, flameFrame int) string {
	// Status icon.
	var icon string
	var iconColor string
	switch a.Status {
	case "running":
		// Pulse: alternate between bright and dim frames.
		if flameFrame%4 < 2 {
			icon = "\u25cf" // ●
			iconColor = s.PrimaryHex
		} else {
			icon = "\u25cf"
			iconColor = s.SecondaryHex
		}
	case "completed":
		icon = "\u2713" // ✓
		iconColor = s.SuccessHex
	case "failed":
		icon = "\u00d7" // ×
		iconColor = s.ErrorHex
	case "killed":
		icon = "\u00d7"
		iconColor = s.MutedHex
	default:
		icon = "\u25c7" // ◇
		iconColor = s.MutedHex
	}

	iconStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(iconColor))
	nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(s.TextHex)).Bold(true)
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(s.MutedHex))

	if selected {
		nameStyle = nameStyle.Foreground(lipgloss.Color(s.PrimaryHex))
	}

	// Line 1: icon + name.
	name := a.Name
	if name == "" {
		name = a.ID
	}
	line1 := iconStyle.Render(icon) + " " + nameStyle.Render(truncate(name, width-4))

	// Line 2: activity or result.
	var line2 string
	switch a.Status {
	case "running":
		activity := a.Activity
		if activity == "" {
			activity = "Working..."
		}
		line2 = "  " + mutedStyle.Render(truncate(activity, width-4))
	case "completed":
		result := a.Result
		if result == "" {
			result = "Done"
		}
		line2 = "  " + mutedStyle.Render(truncate(result, width-4))
	case "failed":
		line2 = "  " + lipgloss.NewStyle().Foreground(lipgloss.Color(s.ErrorHex)).Render("FAILED")
	case "killed":
		line2 = "  " + mutedStyle.Render("Killed")
	default:
		line2 = "  " + mutedStyle.Render("Idle")
	}

	// Line 3: stats.
	elapsed := time.Since(a.Started)
	if !a.Completed.IsZero() {
		elapsed = a.Completed.Sub(a.Started)
	}
	stats := fmt.Sprintf("  %d tools, %s", a.ToolCount, formatDuration(elapsed))
	if a.Tokens > 0 {
		stats += fmt.Sprintf(", %s tok", formatTokens(a.Tokens))
	}
	line3 := mutedStyle.Render(truncate(stats, width-2))

	return line1 + "\n" + line2 + "\n" + line3
}

// --- Helpers ---

func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return "<1s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	if s > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%dm", m)
}

func formatTokens(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1_000_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000.0)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1_000_000.0)
}
