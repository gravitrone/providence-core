package components

import (
	"fmt"
	"strings"
	"time"
)

// maxQuoteLen is the maximum number of characters extracted from a message for quoting.
const maxQuoteLen = 200

// QuoteMessage represents a quotable message in the transcript.
type QuoteMessage struct {
	Role    string // "user", "assistant", "system", "tool"
	Content string
	Time    time.Time
}

// QuoteModel manages the quote/reply selection mode.
// When active, the user navigates through messages with j/k/up/down,
// selects one with enter, and gets a formatted quote block prepended to input.
type QuoteModel struct {
	messages []QuoteMessage
	cursor   int  // index into messages (from end)
	active   bool // selection mode is on
}

// NewQuoteModel creates a new quote model.
func NewQuoteModel() QuoteModel {
	return QuoteModel{}
}

// Active returns true if quote selection mode is on.
func (q QuoteModel) Active() bool {
	return q.active
}

// Cursor returns the current selection index.
func (q QuoteModel) Cursor() int {
	return q.cursor
}

// Enter activates quote selection mode with the given messages.
// Starts with the cursor on the last message.
func (q *QuoteModel) Enter(messages []QuoteMessage) {
	if len(messages) == 0 {
		return
	}
	q.messages = messages
	q.cursor = len(messages) - 1
	q.active = true
}

// Exit leaves quote selection mode without quoting.
func (q *QuoteModel) Exit() {
	q.active = false
	q.cursor = 0
}

// HandleKey processes navigation keys in quote mode.
// Returns: (quoted bool, quoteBlock string).
// If quoted is true, quoteBlock contains the formatted text to prepend to input.
func (q *QuoteModel) HandleKey(key string) (bool, string) {
	if !q.active || len(q.messages) == 0 {
		return false, ""
	}

	switch key {
	case "up", "k":
		if q.cursor > 0 {
			q.cursor--
		}
		return false, ""

	case "down", "j":
		if q.cursor < len(q.messages)-1 {
			q.cursor++
		}
		return false, ""

	case "enter":
		msg := q.messages[q.cursor]
		block := FormatQuoteBlock(msg)
		q.active = false
		return true, block

	case "esc":
		q.active = false
		return false, ""
	}

	return false, ""
}

// FormatQuoteBlock formats a message into a quote block for the input.
func FormatQuoteBlock(msg QuoteMessage) string {
	var sb strings.Builder

	// Header line.
	ago := formatTimeAgo(msg.Time)
	fmt.Fprintf(&sb, "> [quoting %s message from %s]\n", msg.Role, ago)

	// Content (truncated).
	content := truncateQuote(msg.Content)
	for _, line := range strings.Split(content, "\n") {
		sb.WriteString("> " + strings.TrimRight(line, " \t") + "\n")
	}

	sb.WriteString("\n")
	return sb.String()
}

// truncateQuote truncates content to maxQuoteLen, adding "..." if truncated.
func truncateQuote(s string) string {
	// Take first N chars, break at last space if possible.
	s = strings.TrimSpace(s)
	if len(s) <= maxQuoteLen {
		return `"` + s + `"`
	}

	truncated := s[:maxQuoteLen]
	// Try to break at last space.
	if idx := strings.LastIndex(truncated, " "); idx > maxQuoteLen/2 {
		truncated = truncated[:idx]
	}
	return `"` + truncated + `..."`
}

// formatTimeAgo returns a human-readable relative time string.
func formatTimeAgo(t time.Time) string {
	if t.IsZero() {
		return "just now"
	}

	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < 2*time.Minute:
		return "1m ago"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 2*time.Hour:
		return "1h ago"
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
}
