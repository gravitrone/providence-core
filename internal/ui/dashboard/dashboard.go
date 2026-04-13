package dashboard

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// --- Theme-reactive colors (updated by UpdateThemeColors) ---

var (
	themeHeaderColor = "#FFA600"
	themeTextColor   = "#E8DACE"
	themeAccentColor = "#D77757"
	themeMutedColor  = "#6b5040"
)

// UpdateThemeColors updates the dashboard-internal color palette.
// Called from the TUI layer after ApplyTheme so tables use the active theme.
func UpdateThemeColors(primary, secondary, muted, text string) {
	themeHeaderColor = primary
	themeAccentColor = secondary
	themeMutedColor = muted
	themeTextColor = text
}

// AgentInfo describes an active/completed subagent.
type AgentInfo struct {
	Name          string
	Model         string
	Status        string // "running", "completed", "failed", "killed", "background"
	Elapsed       string // e.g. "12s"
	LastActivity  string // last tool/action performed
	ParentName    string // empty = top-level agent
	ResultPreview string // first few lines of result for expandable preview
}

// FileInfo describes a file touched during the session.
type FileInfo struct {
	Path   string // absolute or relative path
	Action string // "read", "write", "edit"
}

// ErrorInfo describes a tool or session error.
type ErrorInfo struct {
	Tool    string
	Message string
}

// TaskInfo describes a single todo item from TodoWrite.
type TaskInfo struct {
	Text   string
	Status string // "pending", "in_progress", "completed"
}

// CompactInfo describes the last compaction event.
type CompactInfo struct {
	Phase        string // "running", "complete", "failed"
	TokensBefore int
	TokensAfter  int
	ErrMsg       string
}

// Panel is a single dashboard panel with a title, glyph, and render function.
type Panel struct {
	ID        string
	Title     string
	Glyph     string // unicode icon prefix
	Collapsed bool
	Priority  int // lower = higher on screen
	Render    func(width int) string
	Badge     string // e.g. "[2 pending]" for approvals
	BadgeHot  bool   // true = flame red border on badge
}

// DashboardModel manages the vertical collapsible panel stack.
type DashboardModel struct {
	Panels   []Panel
	Width    int
	Height   int
	FocusIdx int  // which panel has keyboard focus for j/k nav
	Focused  bool // whether the dashboard pane is focused

	// Token usage progress bar.
	TokenProgress progress.Model
	TokenPct      float64 // 0.0 - 1.0

	// Live data backing each panel.
	CurrentTokens int
	MaxTokens     int
	Agents        []AgentInfo
	Files         []FileInfo
	Errors        []ErrorInfo
	Tasks         []TaskInfo
	Compact       CompactInfo
}

// New creates a DashboardModel with default stub panels.
func New() DashboardModel {
	p := progress.New(
		progress.WithColors(
			lipgloss.Color("#4A2010"),
			lipgloss.Color("#D77757"),
			lipgloss.Color("#FFA600"),
			lipgloss.Color("#FFD700"),
		),
		progress.WithWidth(20),
	)
	p.ShowPercentage = false

	return DashboardModel{
		Panels:        defaultPanels(),
		TokenProgress: p,
	}
}

// SetSize updates the dashboard dimensions.
func (d *DashboardModel) SetSize(width, height int) {
	d.Width = width
	d.Height = height
}

// View renders the vertical collapsible panel stack inside a bordered box.
func (d DashboardModel) View() string {
	if d.Width < 8 || d.Height < 4 {
		return ""
	}

	var sections []string
	remainingH := d.Height - 2
	if remainingH < 1 {
		return ""
	}

	for i, panel := range d.Panels {
		if remainingH <= 0 {
			break
		}

		header := d.renderPanelHeader(panel, i == d.FocusIdx)
		if panel.Collapsed {
			sections = append(sections, header)
			remainingH--
			continue
		}

		innerW := d.Width - 2
		if innerW < 4 {
			innerW = 4
		}

		var body string
		if panel.ID == "tokens" {
			// Render token progress bar.
			d.TokenProgress.SetWidth(innerW - 4)
			var pctLabel string
			if d.MaxTokens > 0 {
				pctLabel = fmt.Sprintf("  %dk / %dk (%0.0f%%)", d.CurrentTokens/1000, d.MaxTokens/1000, d.TokenPct*100)
			} else {
				pctLabel = fmt.Sprintf("  %3.0f%% context", d.TokenPct*100)
			}
			body = pctLabel + "\n  " + d.TokenProgress.ViewAs(d.TokenPct)
		} else {
			body = "  No data"
			if panel.Render != nil {
				body = panel.Render(innerW)
			}
		}

		bodyLines := strings.Split(body, "\n")
		available := remainingH - 1 // subtract header line
		if available <= 0 {
			sections = append(sections, header)
			break
		}
		if len(bodyLines) > available {
			bodyLines = bodyLines[:available]
		}

		sections = append(sections, header+"\n"+strings.Join(bodyLines, "\n"))
		remainingH -= 1 + len(bodyLines)
	}

	content := strings.Join(sections, "\n")

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#3a2518")).
		Width(d.Width-2). // lipgloss adds 2 border chars
		Height(d.Height - 2)

	return border.Render(content)
}

// renderPanelHeader builds a single panel header line: glyph + title + badge.
func (d DashboardModel) renderPanelHeader(p Panel, focused bool) string {
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFA600"))

	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#5C4A3A"))

	glyphStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFA600"))

	mutedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6b5040"))

	titleStyle := headerStyle
	if p.Collapsed {
		titleStyle = dimStyle
		glyphStyle = dimStyle
	}

	if focused && d.Focused {
		titleStyle = titleStyle.Foreground(lipgloss.Color("#FFD700"))
	}

	arrow := "▾"
	if p.Collapsed {
		arrow = "▸"
	}

	header := mutedStyle.Render(arrow) + " " +
		glyphStyle.Render(p.Glyph) + " " +
		titleStyle.Render(p.Title)

	if p.Badge != "" {
		badgeStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6b5040"))
		if p.BadgeHot {
			badgeStyle = badgeStyle.
				Foreground(lipgloss.Color("#ff5555")).
				Bold(true)
		}
		header += " " + badgeStyle.Render(p.Badge)
	}

	return header
}

// Update handles dashboard-specific key events when the dashboard is focused.
func (d DashboardModel) Update(msg tea.Msg) (DashboardModel, tea.Cmd) {
	if !d.Focused {
		return d, nil
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "j", "down":
			if d.FocusIdx < len(d.Panels)-1 {
				d.FocusIdx++
			}
		case "k", "up":
			if d.FocusIdx > 0 {
				d.FocusIdx--
			}
		case "enter", "space":
			if d.FocusIdx >= 0 && d.FocusIdx < len(d.Panels) {
				d.Panels[d.FocusIdx].Collapsed = !d.Panels[d.FocusIdx].Collapsed
			}
		}
	}
	return d, nil
}

// TogglePanel toggles the collapsed state of a panel by ID.
func (d *DashboardModel) TogglePanel(id string) {
	for i := range d.Panels {
		if d.Panels[i].ID == id {
			d.Panels[i].Collapsed = !d.Panels[i].Collapsed
			return
		}
	}
}

// SetPanelVisible sets a panel's collapsed state by ID.
func (d *DashboardModel) SetPanelVisible(id string, visible bool) {
	for i := range d.Panels {
		if d.Panels[i].ID == id {
			d.Panels[i].Collapsed = !visible
			return
		}
	}
}

// PanelByID returns a pointer to the panel with the given ID, or nil.
func (d *DashboardModel) PanelByID(id string) *Panel {
	for i := range d.Panels {
		if d.Panels[i].ID == id {
			return &d.Panels[i]
		}
	}
	return nil
}

// SetTokens updates the token usage panel with real numbers.
func (d *DashboardModel) SetTokens(current, max int) {
	d.CurrentTokens = current
	d.MaxTokens = max
	if max > 0 {
		d.TokenPct = float64(current) / float64(max)
	} else {
		d.TokenPct = 0
	}
}

// SetFiles replaces the tracked file list for the FILES panel.
func (d *DashboardModel) SetFiles(files []FileInfo) {
	d.Files = files
	p := d.PanelByID("files")
	if p == nil {
		return
	}
	if len(files) == 0 {
		p.Render = func(w int) string { return "" }
		p.Badge = ""
		return
	}
	p.Badge = fmt.Sprintf("[%d]", len(files))
	snapshot := make([]FileInfo, len(files))
	copy(snapshot, files)
	p.Render = func(w int) string {
		start := 0
		if len(snapshot) > 8 {
			start = len(snapshot) - 8
		}
		visible := snapshot[start:]
		pathW := w - 10
		if pathW < 8 {
			pathW = 8
		}
		cols := []table.Column{
			{Title: "Op", Width: 2},
			{Title: "Path", Width: pathW},
		}
		rows := make([]table.Row, len(visible))
		for i, f := range visible {
			icon := "R"
			switch f.Action {
			case "write":
				icon = "W"
			case "edit":
				icon = "E"
			}
			rows[i] = table.Row{icon, truncatePath(f.Path, pathW)}
		}
		t := table.New(
			table.WithColumns(cols),
			table.WithRows(rows),
			table.WithHeight(len(rows)+1),
		)
		s := table.Styles{
			Header:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(themeHeaderColor)),
			Cell:     lipgloss.NewStyle().Foreground(lipgloss.Color(themeTextColor)),
			Selected: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(themeAccentColor)),
		}
		t.SetStyles(s)
		result := t.View()
		if start > 0 {
			result += fmt.Sprintf("\n  ...+%d more", start)
		}
		return result
	}
}

// SetAgents updates the AGENTS panel with current subagent data.
func (d *DashboardModel) SetAgents(agents []AgentInfo) {
	d.Agents = agents
	p := d.PanelByID("agents")
	if p == nil {
		return
	}
	if len(agents) == 0 {
		p.Render = func(w int) string { return "" }
		p.Badge = ""
		return
	}
	running := 0
	for _, a := range agents {
		if a.Status == "running" {
			running++
		}
	}
	if running > 0 {
		p.Badge = fmt.Sprintf("[%d active]", running)
	} else {
		p.Badge = fmt.Sprintf("[%d]", len(agents))
	}
	snapshot := make([]AgentInfo, len(agents))
	copy(snapshot, agents)
	p.Render = func(w int) string {
		return renderAgentTree(snapshot, w)
	}
}

// renderAgentTree renders a hierarchical agent tree with status icons,
// model, elapsed time, and activity lines.
func renderAgentTree(agents []AgentInfo, width int) string {
	if len(agents) == 0 {
		return ""
	}

	nameStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(themeTextColor))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(themeMutedColor))
	activityStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(themeMutedColor)).Italic(true)

	var lines []string
	for _, a := range agents {
		indent := "  "
		if a.ParentName != "" {
			indent = "    "
		}

		icon := agentStatusIcon(a.Status)

		model := a.Model
		if model == "" {
			model = "default"
		}
		modelStr := mutedStyle.Render("[" + model + "]")

		elapsedStr := mutedStyle.Render(a.Elapsed)

		leftPart := indent + icon + " " + nameStyle.Render(truncatePath(a.Name, width-20)) + " " + modelStr
		leftWidth := lipgloss.Width(leftPart)
		rightWidth := lipgloss.Width(elapsedStr)
		gap := width - leftWidth - rightWidth - 2
		if gap < 1 {
			gap = 1
		}
		lines = append(lines, leftPart+strings.Repeat(" ", gap)+elapsedStr)

		if a.LastActivity != "" {
			connector := mutedStyle.Render("\u23BF") // ⎿
			activity := a.LastActivity
			maxLen := width - 10
			if maxLen < 10 {
				maxLen = 10
			}
			if len(activity) > maxLen {
				activity = activity[:maxLen-3] + "..."
			}
			lines = append(lines, indent+"  "+connector+" "+activityStyle.Render(activity))
		}

		if a.ResultPreview != "" {
			previewLines := strings.SplitN(a.ResultPreview, "\n", 4)
			for i, pl := range previewLines {
				if i >= 3 {
					lines = append(lines, indent+"    "+mutedStyle.Render(fmt.Sprintf("...+more")))
					break
				}
				if len(pl) > width-8 {
					pl = pl[:width-11] + "..."
				}
				lines = append(lines, indent+"    "+mutedStyle.Render(pl))
			}
		}
	}

	return strings.Join(lines, "\n")
}

// agentStatusIcon returns a styled status icon for the dashboard agent tree.
func agentStatusIcon(status string) string {
	switch status {
	case "running":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(themeAccentColor)).Bold(true).Render("\u25CF") // ●
	case "completed":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#50C878")).Bold(true).Render("\u2713") // ✓
	case "failed", "killed":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#e05050")).Bold(true).Render("\u00D7") // ×
	case "background":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(themeMutedColor)).Render("\u25C7") // ◇
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(themeMutedColor)).Render("\u25CF") // ●
	}
}

// AddFile appends a single file touch event (no-op if path+action already recorded).
func (d *DashboardModel) AddFile(path, action string) {
	for _, f := range d.Files {
		if f.Path == path && f.Action == action {
			return
		}
	}
	d.Files = append(d.Files, FileInfo{Path: path, Action: action})
	d.SetFiles(d.Files)
}

// SetErrors replaces the error list for the ERRORS panel.
func (d *DashboardModel) SetErrors(errors []ErrorInfo) {
	d.Errors = errors
	p := d.PanelByID("errors")
	if p == nil {
		return
	}
	if len(errors) == 0 {
		p.Render = func(w int) string { return "" }
		p.Badge = ""
		p.BadgeHot = false
		return
	}
	p.Badge = fmt.Sprintf("[%d]", len(errors))
	p.BadgeHot = true
	p.Collapsed = false
	snapshot := make([]ErrorInfo, len(errors))
	copy(snapshot, errors)
	p.Render = func(w int) string {
		start := 0
		if len(snapshot) > 5 {
			start = len(snapshot) - 5
		}
		visible := snapshot[start:]
		msgW := w - 12
		if msgW < 8 {
			msgW = 8
		}
		cols := []table.Column{
			{Title: "Tool", Width: 8},
			{Title: "Error", Width: msgW},
		}
		rows := make([]table.Row, len(visible))
		for i, e := range visible {
			rows[i] = table.Row{e.Tool, truncatePath(e.Message, msgW)}
		}
		t := table.New(
			table.WithColumns(cols),
			table.WithRows(rows),
			table.WithHeight(len(rows)+1),
		)
		s := table.Styles{
			Header:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(themeHeaderColor)),
			Cell:     lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5555")),
			Selected: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ff5555")),
		}
		t.SetStyles(s)
		return t.View()
	}
}

// AddError appends a single error event.
func (d *DashboardModel) AddError(tool, message string) {
	d.Errors = append(d.Errors, ErrorInfo{Tool: tool, Message: message})
	d.SetErrors(d.Errors)
}

// SetTasks updates the TASKS panel from todo items.
func (d *DashboardModel) SetTasks(tasks []TaskInfo) {
	d.Tasks = tasks
	p := d.PanelByID("tasks")
	if p == nil {
		return
	}
	if len(tasks) == 0 {
		p.Render = func(w int) string { return "  No tasks" }
		p.Badge = ""
		return
	}
	pending := 0
	for _, t := range tasks {
		if t.Status != "completed" {
			pending++
		}
	}
	if pending > 0 {
		p.Badge = fmt.Sprintf("[%d pending]", pending)
	} else {
		p.Badge = "[done]"
	}
	snapshot := make([]TaskInfo, len(tasks))
	copy(snapshot, tasks)
	p.Render = func(w int) string {
		var b strings.Builder
		for i, t := range snapshot {
			marker := "○"
			switch t.Status {
			case "in_progress":
				marker = "◐"
			case "completed":
				marker = "●"
			}
			line := fmt.Sprintf("  %s %s", marker, truncatePath(t.Text, w-6))
			b.WriteString(line)
			if i < len(snapshot)-1 {
				b.WriteByte('\n')
			}
		}
		return b.String()
	}
}

// SetCompact updates the COMPACT panel from compaction state.
func (d *DashboardModel) SetCompact(info CompactInfo) {
	d.Compact = info
	p := d.PanelByID("compact")
	if p == nil {
		return
	}
	switch info.Phase {
	case "":
		p.Render = func(w int) string { return "  Idle" }
		p.Badge = ""
		p.Collapsed = true
	case "running":
		p.Collapsed = false
		before := info.TokensBefore
		p.Badge = "[running]"
		p.Render = func(w int) string {
			if before > 0 {
				return fmt.Sprintf("  Compacting %dk tokens...", before/1000)
			}
			return "  Compacting..."
		}
	case "complete":
		p.Collapsed = false
		before := info.TokensBefore
		after := info.TokensAfter
		p.Badge = "[done]"
		p.Render = func(w int) string {
			if before > 0 && after > 0 {
				saved := before - after
				return fmt.Sprintf("  %dk -> %dk (saved %dk)", before/1000, after/1000, saved/1000)
			}
			return "  Compaction complete"
		}
	case "failed":
		p.Collapsed = false
		errMsg := info.ErrMsg
		p.Badge = "[failed]"
		p.BadgeHot = true
		p.Render = func(w int) string {
			if errMsg != "" {
				return fmt.Sprintf("  Failed: %s", truncatePath(errMsg, w-12))
			}
			return "  Compaction failed"
		}
	}
}

// truncatePath shortens a string to fit within maxLen, keeping the tail.
func truncatePath(s string, maxLen int) string {
	if maxLen < 4 {
		maxLen = 4
	}
	if len(s) <= maxLen {
		return s
	}
	return "..." + s[len(s)-maxLen+3:]
}

// --- Full-width tab renderers for the tab system ---

func (d *DashboardModel) emptyTab(width, height int, label string) string {
	return lipgloss.NewStyle().Width(width).Height(height).
		Align(lipgloss.Center, lipgloss.Center).
		Foreground(lipgloss.Color(themeMutedColor)).
		Render(label)
}

// RenderAgentsTab renders the agents panel at full width for the tab view.
func (d *DashboardModel) RenderAgentsTab(width, height int) string {
	if len(d.Agents) == 0 {
		return d.emptyTab(width, height, "No active agents")
	}
	return renderAgentTree(d.Agents, width)
}

// RenderTasksTab renders the tasks panel at full width.
func (d *DashboardModel) RenderTasksTab(width, height int) string {
	if len(d.Tasks) == 0 {
		return d.emptyTab(width, height, "No tasks")
	}
	var b strings.Builder
	for i, t := range d.Tasks {
		marker := "○"
		switch t.Status {
		case "in_progress":
			marker = "◐"
		case "completed":
			marker = "●"
		}
		line := fmt.Sprintf("  %s %s", marker, truncatePath(t.Text, width-6))
		b.WriteString(line)
		if i < len(d.Tasks)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// RenderFilesTab renders the files panel at full width.
func (d *DashboardModel) RenderFilesTab(width, height int) string {
	if len(d.Files) == 0 {
		return d.emptyTab(width, height, "No files touched")
	}
	pathW := width - 10
	if pathW < 8 {
		pathW = 8
	}
	cols := []table.Column{
		{Title: "Op", Width: 2},
		{Title: "Path", Width: pathW},
	}
	rows := make([]table.Row, len(d.Files))
	for i, f := range d.Files {
		icon := "R"
		switch f.Action {
		case "write":
			icon = "W"
		case "edit":
			icon = "E"
		}
		rows[i] = table.Row{icon, truncatePath(f.Path, pathW)}
	}
	t := table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithHeight(len(rows)+1),
	)
	s := table.Styles{
		Header:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(themeHeaderColor)),
		Cell:     lipgloss.NewStyle().Foreground(lipgloss.Color(themeTextColor)),
		Selected: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(themeAccentColor)),
	}
	t.SetStyles(s)
	return t.View()
}

// RenderTokensTab renders the token usage panel at full width, vertically centered.
func (d *DashboardModel) RenderTokensTab(width, height int) string {
	barW := width - 20
	if barW < 10 {
		barW = 10
	}
	d.TokenProgress.SetWidth(barW)
	var label string
	if d.MaxTokens > 0 {
		label = fmt.Sprintf("  %dk / %dk (%0.0f%%)", d.CurrentTokens/1000, d.MaxTokens/1000, d.TokenPct*100)
	} else {
		label = fmt.Sprintf("  %3.0f%% context", d.TokenPct*100)
	}
	content := label + "\n  " + d.TokenProgress.ViewAs(d.TokenPct)
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Align(lipgloss.Center, lipgloss.Center).
		Render(content)
}

// RenderErrorsTab renders the errors panel at full width.
func (d *DashboardModel) RenderErrorsTab(width, height int) string {
	if len(d.Errors) == 0 {
		return d.emptyTab(width, height, "No errors")
	}
	msgW := width - 12
	if msgW < 8 {
		msgW = 8
	}
	cols := []table.Column{
		{Title: "Tool", Width: 8},
		{Title: "Error", Width: msgW},
	}
	rows := make([]table.Row, len(d.Errors))
	for i, e := range d.Errors {
		rows[i] = table.Row{e.Tool, truncatePath(e.Message, msgW)}
	}
	t := table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithHeight(len(rows)+1),
	)
	s := table.Styles{
		Header:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(themeHeaderColor)),
		Cell:     lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5555")),
		Selected: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ff5555")),
	}
	t.SetStyles(s)
	return t.View()
}

// RenderCompactTab renders the compaction panel at full width.
func (d *DashboardModel) RenderCompactTab(width, height int) string {
	switch d.Compact.Phase {
	case "running":
		if d.Compact.TokensBefore > 0 {
			return fmt.Sprintf("  Compacting %dk tokens...", d.Compact.TokensBefore/1000)
		}
		return "  Compacting..."
	case "complete":
		if d.Compact.TokensBefore > 0 && d.Compact.TokensAfter > 0 {
			saved := d.Compact.TokensBefore - d.Compact.TokensAfter
			return fmt.Sprintf("  %dk -> %dk (saved %dk)", d.Compact.TokensBefore/1000, d.Compact.TokensAfter/1000, saved/1000)
		}
		return "  Compaction complete"
	case "failed":
		if d.Compact.ErrMsg != "" {
			return fmt.Sprintf("  Failed: %s", d.Compact.ErrMsg)
		}
		return "  Compaction failed"
	default:
		return d.emptyTab(width, height, "Idle")
	}
}

// RenderHooksTab renders the hooks panel at full width.
func (d *DashboardModel) RenderHooksTab(width, height int) string {
	return d.emptyTab(width, height, "No hooks configured")
}

// --- Default Panels ---

func defaultPanels() []Panel {
	return []Panel{
		{ID: "approvals", Title: "APPROVALS", Glyph: "⚠", Priority: 0,
			Render: func(w int) string { return "" }},
		{ID: "agents", Title: "AGENTS", Glyph: "⟁", Priority: 1,
			Render: func(w int) string { return "" }},
		{ID: "tokens", Title: "TOKENS", Glyph: "◬", Priority: 2,
			Render: func(w int) string { return "  0% context used" }},
		{ID: "tasks", Title: "TASKS", Glyph: "⚑", Priority: 3,
			Render: func(w int) string { return "" }},
		{ID: "files", Title: "FILES", Glyph: "⊞", Priority: 4,
			Render: func(w int) string { return "" }},
		{ID: "errors", Title: "ERRORS", Glyph: "⊛", Priority: 5, Collapsed: true,
			Render: func(w int) string { return "" }},
		{ID: "compact", Title: "COMPACT", Glyph: "⊙", Priority: 6, Collapsed: true,
			Render: func(w int) string { return "  Idle" }},
		{ID: "hooks", Title: "HOOKS", Glyph: "⊕", Priority: 7, Collapsed: true,
			Render: func(w int) string { return "" }},
	}
}
