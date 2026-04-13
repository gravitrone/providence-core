package components

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// RewindMsg is emitted when the user confirms a rewind action.
type RewindMsg struct {
	Index  int
	Action RewindAction
}

// RewindAction describes what to do after rewinding.
type RewindAction int

const (
	// RewindRestore slices the conversation at the selected index.
	RewindRestore RewindAction = iota
	// RewindSummarize summarizes from the selected index onward.
	RewindSummarize
	// RewindCancel dismisses the rewind picker.
	RewindCancel
)

// RewindItem represents a single user message available for rewinding.
type RewindItem struct {
	UUID      string
	Role      string
	Preview   string // first 80 chars of message
	Timestamp string
	Index     int // original message index
}

// RewindModel is a 7-item virtual scroll picker for selecting a message to rewind to.
type RewindModel struct {
	messages    []RewindItem
	selected    int
	visible     int // max visible items
	offset      int // scroll offset for virtual scroll
	confirmMode bool
	confirmIdx  int // which confirm option is highlighted (0=restore, 1=summarize, 2=cancel)
	width       int
	active      bool
}

// NewRewindModel creates a new rewind picker from a list of user messages.
func NewRewindModel(items []RewindItem, width int) RewindModel {
	vis := 7
	if len(items) < vis {
		vis = len(items)
	}
	return RewindModel{
		messages: items,
		selected: 0,
		visible:  vis,
		offset:   0,
		width:    width,
		active:   true,
	}
}

// Active returns whether the rewind picker is open.
func (m RewindModel) Active() bool {
	return m.active
}

// ConfirmMode returns whether the picker is in confirm mode.
func (m RewindModel) ConfirmMode() bool {
	return m.confirmMode
}

// Selected returns the currently selected item index.
func (m RewindModel) Selected() int {
	return m.selected
}

// HandleKey processes a key press and returns the updated model plus an optional RewindMsg.
// Returns (model, msg, handled). If handled is false the key was not consumed.
func (m RewindModel) HandleKey(key string) (RewindModel, *RewindMsg, bool) {
	if !m.active {
		return m, nil, false
	}

	if m.confirmMode {
		return m.handleConfirmKey(key)
	}

	switch key {
	case "j", "down":
		if m.selected < len(m.messages)-1 {
			m.selected++
			if m.selected >= m.offset+m.visible {
				m.offset = m.selected - m.visible + 1
			}
		}
		return m, nil, true

	case "k", "up":
		if m.selected > 0 {
			m.selected--
			if m.selected < m.offset {
				m.offset = m.selected
			}
		}
		return m, nil, true

	case "enter":
		if len(m.messages) > 0 {
			m.confirmMode = true
			m.confirmIdx = 0
		}
		return m, nil, true

	case "esc":
		m.active = false
		return m, &RewindMsg{Action: RewindCancel}, true
	}

	return m, nil, false
}

func (m RewindModel) handleConfirmKey(key string) (RewindModel, *RewindMsg, bool) {
	switch key {
	case "j", "down":
		if m.confirmIdx < 2 {
			m.confirmIdx++
		}
		return m, nil, true

	case "k", "up":
		if m.confirmIdx > 0 {
			m.confirmIdx--
		}
		return m, nil, true

	case "enter":
		item := m.messages[m.selected]
		var action RewindAction
		switch m.confirmIdx {
		case 0:
			action = RewindRestore
		case 1:
			action = RewindSummarize
		case 2:
			action = RewindCancel
		}
		m.active = false
		return m, &RewindMsg{Index: item.Index, Action: action}, true

	case "esc":
		m.confirmMode = false
		return m, nil, true
	}

	return m, nil, false
}

// View renders the rewind picker.
func (m RewindModel) View() string {
	if !m.active || len(m.messages) == 0 {
		return ""
	}

	w := m.width
	if w <= 0 {
		w = 60
	}

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFA600")).
		Bold(true)
	mutedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6b5040"))
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#0a0a0a")).
		Background(lipgloss.Color("#FFA600")).
		Bold(true)
	normalStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#e0d0c0"))
	confirmActiveStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#0a0a0a")).
		Background(lipgloss.Color("#D77757")).
		Bold(true)
	confirmNormalStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#e0d0c0"))

	var b strings.Builder

	if m.confirmMode {
		item := m.messages[m.selected]
		b.WriteString(titleStyle.Render("Rewind to:"))
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render(fmt.Sprintf("  %s", truncatePreview(item.Preview, w-4))))
		b.WriteString("\n\n")

		options := []string{"Restore conversation", "Summarize from here", "Never mind"}
		for i, opt := range options {
			if i == m.confirmIdx {
				b.WriteString("  " + confirmActiveStyle.Render(" "+opt+" "))
			} else {
				b.WriteString("  " + confirmNormalStyle.Render("  "+opt))
			}
			b.WriteString("\n")
		}
		return b.String()
	}

	b.WriteString(titleStyle.Render("/rewind - select a message"))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("j/k navigate, enter select, esc dismiss"))
	b.WriteString("\n\n")

	end := m.offset + m.visible
	if end > len(m.messages) {
		end = len(m.messages)
	}

	for i := m.offset; i < end; i++ {
		item := m.messages[i]
		prefix := fmt.Sprintf(" %d. ", item.Index+1)
		preview := truncatePreview(item.Preview, w-len(prefix)-2)
		line := prefix + preview

		if i == m.selected {
			b.WriteString(selectedStyle.Render(line))
		} else {
			b.WriteString(normalStyle.Render(line))
		}
		b.WriteString("\n")
	}

	if len(m.messages) > m.visible {
		scrollInfo := fmt.Sprintf("  [%d/%d]", m.selected+1, len(m.messages))
		b.WriteString(mutedStyle.Render(scrollInfo))
		b.WriteString("\n")
	}

	return b.String()
}

func truncatePreview(s string, maxLen int) string {
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
