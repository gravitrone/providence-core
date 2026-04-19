package dashboard

import (
	"fmt"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// --- Progress Ticker ---

// DefaultProgressInterval is how often the ticker polls active subagents.
const DefaultProgressInterval = 30 * time.Second

// SubagentProgress is a single-line snapshot of one active subagent.
// Used by the dashboard ticker to render a rolling summary row per subagent.
type SubagentProgress struct {
	AgentID    string
	AgentType  string
	LastTool   string
	LastToolAt time.Time
	Summary    string
	StartedAt  time.Time
}

// ProgressSource returns the current list of active subagent progress records.
// Callers inject this so the ticker stays decoupled from the runner package.
type ProgressSource func() []SubagentProgress

// ProgressTickMsg is emitted on the Bubble Tea update loop when the ticker
// fires. Carries the snapshot sampled at tick time so the View layer can
// rerender without re-reading the source.
type ProgressTickMsg struct {
	Snapshot []SubagentProgress
	At       time.Time
}

// ProgressTicker polls a ProgressSource on a fixed interval and emits
// ProgressTickMsg values via a tea.Cmd. The clock and interval are injectable
// so tests can drive the ticker deterministically without real sleeps.
type ProgressTicker struct {
	mu       sync.Mutex
	source   ProgressSource
	clock    func() time.Time
	sleep    func(time.Duration) <-chan time.Time
	interval time.Duration
	stopped  bool
	stopCh   chan struct{}
}

// NewProgressTicker creates a ticker bound to source with the default 30s
// interval and real wall-clock time. Callers wire Tick() into their Bubble
// Tea Init/Update loops.
func NewProgressTicker(source ProgressSource) *ProgressTicker {
	return &ProgressTicker{
		source:   source,
		clock:    time.Now,
		sleep:    time.After,
		interval: DefaultProgressInterval,
		stopCh:   make(chan struct{}),
	}
}

// WithInterval overrides the poll interval. Returns the ticker for chaining.
func (t *ProgressTicker) WithInterval(d time.Duration) *ProgressTicker {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.interval = d
	return t
}

// WithClock injects a clock function for deterministic tests.
func (t *ProgressTicker) WithClock(clock func() time.Time) *ProgressTicker {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.clock = clock
	return t
}

// WithSleep injects the wait primitive for deterministic tests. The returned
// channel fires once after the requested duration; tests use this to step
// the ticker without real time.Sleep.
func (t *ProgressTicker) WithSleep(sleep func(time.Duration) <-chan time.Time) *ProgressTicker {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sleep = sleep
	return t
}

// Snapshot samples the current progress source once. Safe to call on any
// goroutine. Returns an empty slice when there are no active subagents.
func (t *ProgressTicker) Snapshot() []SubagentProgress {
	t.mu.Lock()
	src := t.source
	clock := t.clock
	t.mu.Unlock()
	if src == nil {
		return nil
	}
	snap := src()
	_ = clock // Reserved for future "as-of" filtering.
	return snap
}

// Tick returns a tea.Cmd that blocks on the injected sleep primitive and,
// on fire, emits a ProgressTickMsg carrying the fresh snapshot. Chain
// another Tick() from the Update loop to keep the ticker running.
func (t *ProgressTicker) Tick() tea.Cmd {
	t.mu.Lock()
	sleep := t.sleep
	clock := t.clock
	interval := t.interval
	stopCh := t.stopCh
	stopped := t.stopped
	t.mu.Unlock()

	if stopped || sleep == nil {
		return nil
	}

	return func() tea.Msg {
		select {
		case <-sleep(interval):
		case <-stopCh:
			return nil
		}
		return ProgressTickMsg{
			Snapshot: t.Snapshot(),
			At:       clock(),
		}
	}
}

// Stop signals any in-flight Tick command to return without emitting. Safe
// to call multiple times. Prevents goroutine leaks on dashboard teardown.
func (t *ProgressTicker) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopped {
		return
	}
	t.stopped = true
	close(t.stopCh)
}

// --- Rendering ---

// FormatSubagentProgress renders a single progress record into the canonical
// dashboard line: "type . tool . 2m ago . since 00:05:12". Uses the supplied
// now for the elapsed calculation so tests stay time-independent.
func FormatSubagentProgress(p SubagentProgress, now time.Time) string {
	typeName := p.AgentType
	if typeName == "" {
		typeName = p.AgentID
	}
	if typeName == "" {
		typeName = "agent"
	}

	tool := p.LastTool
	if tool == "" {
		tool = "idle"
	}

	lastToolAgo := "just now"
	if !p.LastToolAt.IsZero() {
		lastToolAgo = humanizeAgo(now.Sub(p.LastToolAt))
	}

	var sinceStr string
	if !p.StartedAt.IsZero() {
		sinceStr = formatDuration(now.Sub(p.StartedAt))
	} else {
		sinceStr = "00:00:00"
	}

	line := fmt.Sprintf("%s \u2022 %s \u2022 %s \u2022 since %s",
		typeName, tool, lastToolAgo, sinceStr)

	if p.Summary != "" {
		line = fmt.Sprintf("%s \u2022 %s", line, p.Summary)
	}

	return line
}

// RenderSubagentProgressPanel renders the full panel body for a snapshot.
// Returns "no subagents" when snap is empty, per spec.
func RenderSubagentProgressPanel(snap []SubagentProgress, width int, now time.Time) string {
	if len(snap) == 0 {
		muted := lipgloss.NewStyle().Foreground(lipgloss.Color(themeMutedColor)).Italic(true)
		return "  " + muted.Render("no subagents")
	}

	rows := make([]string, 0, len(snap))
	nameStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(themeTextColor))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(themeMutedColor))

	for _, p := range snap {
		line := FormatSubagentProgress(p, now)
		// Style: bold the agent type segment, mute the rest.
		parts := strings.SplitN(line, " \u2022 ", 2)
		if len(parts) == 2 {
			rendered := nameStyle.Render(parts[0]) + " " + muted.Render("\u2022 "+parts[1])
			rows = append(rows, "  "+truncateLine(rendered, width-2))
			continue
		}
		rows = append(rows, "  "+truncateLine(muted.Render(line), width-2))
	}

	return strings.Join(rows, "\n")
}

// humanizeAgo renders a short ago-string like "2m ago" or "12s ago".
func humanizeAgo(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Second:
		return "just now"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
}

// formatDuration renders an elapsed duration as HH:MM:SS.
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d.Seconds())
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

// truncateLine clips a rendered line to visible width preserving the prefix.
// Operates on the styled string; approximate length via lipgloss.Width.
func truncateLine(s string, width int) string {
	if width < 4 {
		width = 4
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	// Fallback: byte-truncate with ellipsis. Style escapes may survive.
	if len(s) <= width {
		return s
	}
	return s[:width-3] + "..."
}

// --- Dashboard wiring ---

// SetSubagentProgress installs/updates the ticker-backed "SUBAGENTS" panel
// body. Call from the Bubble Tea Update loop in response to a
// ProgressTickMsg. Safe to call with an empty snapshot.
func (d *DashboardModel) SetSubagentProgress(snap []SubagentProgress, now time.Time) {
	p := d.PanelByID("subagent_progress")
	if p == nil {
		d.Panels = append(d.Panels, Panel{
			ID:       "subagent_progress",
			Title:    "SUBAGENTS",
			Glyph:    "\u22C8", // ⋈
			Priority: 2,
		})
		p = d.PanelByID("subagent_progress")
	}

	if len(snap) == 0 {
		p.Badge = ""
		p.Render = func(w int) string {
			return RenderSubagentProgressPanel(nil, w, now)
		}
		return
	}

	p.Badge = fmt.Sprintf("[%d]", len(snap))
	snapshot := make([]SubagentProgress, len(snap))
	copy(snapshot, snap)
	p.Render = func(w int) string {
		return RenderSubagentProgressPanel(snapshot, w, now)
	}
}
