package overlay

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// TokenTracker accumulates per-session context-injection token counts.
// Used for the `/overlay cost` slash command and for context_ack payloads
// sent back to the overlay process.
//
// All methods are safe for concurrent use.
type TokenTracker struct {
	mu         sync.Mutex
	total      int
	entries    []TokenEntry // bounded ring, keeps the most recent maxEntries
	maxEntries int
}

// TokenEntry is a single accounting record.
type TokenEntry struct {
	Time   time.Time
	Tokens int
	Reason string
	App    string
	Mode   string
}

// NewTokenTracker returns a tracker that retains the last 50 entries.
func NewTokenTracker() *TokenTracker {
	return &TokenTracker{maxEntries: 50}
}

// Record appends an entry and updates the running total.
func (t *TokenTracker) Record(e TokenEntry) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.total += e.Tokens
	t.entries = append(t.entries, e)
	if len(t.entries) > t.maxEntries {
		t.entries = t.entries[len(t.entries)-t.maxEntries:]
	}
}

// EstimateTokens approximates a string's token count using 1 token ≈ 4 bytes.
// Matches the Swift-side TokenBudget.estimate so both halves agree on counts.
func EstimateTokens(s string) int {
	n := len(s) / 4
	if n < 1 {
		n = 1
	}
	return n
}

// Total returns the running total of tokens injected this session.
func (t *TokenTracker) Total() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.total
}

// Average returns the average tokens per retained entry.
func (t *TokenTracker) Average() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.entries) == 0 {
		return 0
	}
	sum := 0
	for _, e := range t.entries {
		sum += e.Tokens
	}
	return sum / len(t.entries)
}

// Recent returns a copy of the most recent n retained entries (up to len(entries)).
func (t *TokenTracker) Recent(n int) []TokenEntry {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if n > len(t.entries) {
		n = len(t.entries)
	}
	if n <= 0 {
		return nil
	}
	return append([]TokenEntry(nil), t.entries[len(t.entries)-n:]...)
}

// FormatSummary returns a human-readable block for `/overlay cost`.
func (t *TokenTracker) FormatSummary() string {
	if t == nil {
		return "Token tracker unavailable."
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.entries) == 0 {
		return "No context injections yet."
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Total injected tokens (this session): %d\n", t.total)
	fmt.Fprintf(&sb, "Average per update: %d\n", t.total/len(t.entries))
	sb.WriteString("\nRecent updates:\n")
	start := len(t.entries) - 10
	if start < 0 {
		start = 0
	}
	for _, e := range t.entries[start:] {
		app := e.App
		if app == "" {
			app = "?"
		}
		fmt.Fprintf(&sb, "  %s  %d tok  %s  (%s, %s)\n",
			e.Time.Format("15:04:05"), e.Tokens, app, e.Reason, e.Mode)
	}
	return sb.String()
}
