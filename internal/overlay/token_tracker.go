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

	// Phase G: daily budget breaker. When dailyBudget > 0, callers can check
	// BudgetExceeded() to decide whether to skip an injection. When set to 0
	// (the default) budget gating is disabled and the tracker behaves exactly
	// as it did before Phase G.
	dailyBudget int
	dailyCount  int
	dayStart    time.Time
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
	return &TokenTracker{maxEntries: 50, dayStart: startOfDay(time.Now())}
}

// NewTokenTrackerWithBudget returns a tracker with a daily token budget.
// A budget of 0 disables budget gating (always allowed). Negative values are
// clamped to 0.
func NewTokenTrackerWithBudget(dailyBudget int) *TokenTracker {
	if dailyBudget < 0 {
		dailyBudget = 0
	}
	return &TokenTracker{
		maxEntries:  50,
		dailyBudget: dailyBudget,
		dayStart:    startOfDay(time.Now()),
	}
}

// SetDailyBudget adjusts the daily token budget at runtime. 0 disables
// gating. Negative values are clamped to 0.
func (t *TokenTracker) SetDailyBudget(limit int) {
	if t == nil {
		return
	}
	if limit < 0 {
		limit = 0
	}
	t.mu.Lock()
	t.dailyBudget = limit
	t.mu.Unlock()
}

// DailyBudget returns the currently configured daily budget (0 = unlimited).
func (t *TokenTracker) DailyBudget() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.dailyBudget
}

// DailyTotal returns the total tokens recorded so far today. Triggers a
// day-rollover check so callers get an accurate value across midnight.
func (t *TokenTracker) DailyTotal() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rollIfNewDayLocked(time.Now())
	return t.dailyCount
}

// BudgetExceeded returns true when a daily budget is configured and today's
// recorded tokens meet or exceed that budget. Returns false when the budget
// is 0 (unlimited) or when the tracker is nil.
func (t *TokenTracker) BudgetExceeded() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rollIfNewDayLocked(time.Now())
	return t.dailyBudget > 0 && t.dailyCount >= t.dailyBudget
}

// Record appends an entry and updates the running total.
func (t *TokenTracker) Record(e TokenEntry) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	ts := e.Time
	if ts.IsZero() {
		ts = time.Now()
	}
	t.rollIfNewDayLocked(ts)
	t.total += e.Tokens
	t.dailyCount += e.Tokens
	t.entries = append(t.entries, e)
	if len(t.entries) > t.maxEntries {
		t.entries = t.entries[len(t.entries)-t.maxEntries:]
	}
}

// startOfDay returns the start of the local day containing t.
func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// rollIfNewDayLocked resets the daily counter when `now` falls on a later
// local day than dayStart. Caller must hold t.mu.
func (t *TokenTracker) rollIfNewDayLocked(now time.Time) {
	if t.dayStart.IsZero() {
		t.dayStart = startOfDay(now)
		return
	}
	sd := startOfDay(now)
	if sd.After(t.dayStart) {
		t.dayStart = sd
		t.dailyCount = 0
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
