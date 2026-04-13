package components

import (
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
)

// Providence theme colors (flame).
var (
	// Exported so the theme system can update them.
	ThemePrimary = lipgloss.Color("#D77757")
	ThemeMuted   = lipgloss.Color("#6b5040")
	ThemeText    = lipgloss.Color("#e0d0c0")
	themeBorder  = lipgloss.Color("#3a2a1a")
)

// NewProvidenceTextInput returns a textinput.Model styled to match the providence theme.
func NewProvidenceTextInput(placeholder string) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.Focus()

	styles := textinput.DefaultDarkStyles()
	styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(ThemeMuted)
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(ThemePrimary)
	styles.Focused.Text = lipgloss.NewStyle().Foreground(ThemeText)
	styles.Blurred.Placeholder = lipgloss.NewStyle().Foreground(ThemeMuted)
	styles.Blurred.Text = lipgloss.NewStyle().Foreground(ThemeMuted)
	styles.Cursor.Color = ThemePrimary
	ti.SetStyles(styles)

	return ti
}

// ReapplyInputStyles updates an existing textinput with current theme colors.
func ReapplyInputStyles(ti *textinput.Model) {
	styles := textinput.DefaultDarkStyles()
	styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(ThemeMuted)
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(ThemePrimary)
	styles.Focused.Text = lipgloss.NewStyle().Foreground(ThemeText)
	styles.Blurred.Placeholder = lipgloss.NewStyle().Foreground(ThemeMuted)
	styles.Blurred.Text = lipgloss.NewStyle().Foreground(ThemeMuted)
	styles.Cursor.Color = ThemePrimary
	ti.SetStyles(styles)
}

// NewProvidenceTextArea returns a textarea.Model styled to match the providence theme.
// It is configured for 3-line multiline input with enter = submit (handled by caller)
// and shift+enter = newline.
func NewProvidenceTextArea(placeholder string) textarea.Model {
	ta := textarea.New()
	ta.Placeholder = placeholder
	ta.ShowLineNumbers = false
	ta.SetHeight(3)
	ta.Prompt = "❯ "

	// Remap InsertNewline from enter to shift+enter so enter can be handled
	// by the caller as "submit".
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter"), key.WithHelp("shift+enter", "newline"))

	styles := textarea.DefaultDarkStyles()
	styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(ThemeMuted)
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(ThemePrimary)
	styles.Focused.Text = lipgloss.NewStyle().Foreground(ThemeText)
	styles.Focused.CursorLine = lipgloss.NewStyle()
	styles.Blurred.Placeholder = lipgloss.NewStyle().Foreground(ThemeMuted)
	styles.Blurred.Text = lipgloss.NewStyle().Foreground(ThemeMuted)
	styles.Cursor.Color = ThemePrimary
	ta.SetStyles(styles)

	ta.Focus()
	return ta
}

// ReapplyTextAreaStyles updates an existing textarea with current theme colors.
func ReapplyTextAreaStyles(ta *textarea.Model) {
	styles := textarea.DefaultDarkStyles()
	styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(ThemeMuted)
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(ThemePrimary)
	styles.Focused.Text = lipgloss.NewStyle().Foreground(ThemeText)
	styles.Focused.CursorLine = lipgloss.NewStyle()
	styles.Blurred.Placeholder = lipgloss.NewStyle().Foreground(ThemeMuted)
	styles.Blurred.Text = lipgloss.NewStyle().Foreground(ThemeMuted)
	styles.Cursor.Color = ThemePrimary
	ta.SetStyles(styles)
}

// NewProvidenceSpinner returns a spinner.Model styled to match the providence theme.
func NewProvidenceSpinner() spinner.Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(ThemePrimary)
	return s
}

// TableBaseStyle wraps a table.View() in a bordered box.
var TableBaseStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(themeBorder)

// TableBaseBorderWidth is the horizontal frame size of TableBaseStyle.
const TableBaseBorderWidth = 2

// NewProvidenceTable returns a table.Model styled with providence flame theme.
func NewProvidenceTable(cols []table.Column, height int) table.Model {
	if cols == nil {
		cols = []table.Column{{Title: "", Width: 40}}
	}

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(themeBorder).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("#0a0a0a")).
		Background(ThemePrimary).
		Bold(false)

	t := table.New(
		table.WithColumns(cols),
		table.WithHeight(height),
		table.WithStyles(s),
		table.WithFocused(true),
	)
	return t
}

// RenderInfoTable renders key-value pairs as a read-only 2-column table.
func RenderInfoTable(rows []InfoTableRow, width int) string {
	if len(rows) == 0 || width <= 0 {
		return ""
	}

	innerWidth := width - 2
	if innerWidth < 20 {
		innerWidth = 20
	}

	keyWidth := 0
	for _, r := range rows {
		w := lipgloss.Width(SanitizeOneLine(r.Key))
		if w > keyWidth {
			keyWidth = w
		}
	}
	if keyWidth > 24 {
		keyWidth = 24
	}
	if keyWidth < 6 {
		keyWidth = 6
	}

	valWidth := innerWidth - keyWidth - (2 * 2)
	if valWidth < 10 {
		valWidth = 10
	}

	tableRows := make([]table.Row, len(rows))
	for i, r := range rows {
		tableRows[i] = table.Row{
			ClampTextWidthEllipsis(SanitizeOneLine(r.Key), keyWidth),
			ClampTextWidthEllipsis(SanitizeOneLine(r.Value), valWidth),
		}
	}

	cols := []table.Column{
		{Title: "Field", Width: keyWidth},
		{Title: "Value", Width: valWidth},
	}

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(themeBorder).
		BorderBottom(true).
		Bold(false)
	s.Selected = lipgloss.NewStyle()

	actualW := keyWidth + valWidth + (2 * 2)
	t := table.New(
		table.WithColumns(cols),
		table.WithRows(tableRows),
		table.WithHeight(len(rows)+1),
		table.WithWidth(actualW),
		table.WithStyles(s),
	)
	t.Blur()

	return TableBaseStyle.Render(t.View())
}

// InfoTableRow is a key-value pair for RenderInfoTable.
type InfoTableRow struct {
	Key   string
	Value string
}

// NewProvidenceViewport returns a viewport.Model with the given dimensions.
func NewProvidenceViewport(width, height int) viewport.Model {
	return viewport.New(
		viewport.WithWidth(width),
		viewport.WithHeight(height),
	)
}
