package overlay

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTokenTrackerEmpty(t *testing.T) {
	tr := NewTokenTracker()
	assert.Equal(t, 0, tr.Total())
	assert.Equal(t, 0, tr.Average())
	assert.Nil(t, tr.Recent(5))
	assert.Equal(t, "No context injections yet.", tr.FormatSummary())
}

func TestTokenTrackerRecord(t *testing.T) {
	tr := NewTokenTracker()
	now := time.Now()
	tr.Record(TokenEntry{Time: now, Tokens: 10, Reason: "pattern", App: "VS Code", Mode: "system_reminder"})
	tr.Record(TokenEntry{Time: now, Tokens: 30, Reason: "error", App: "Terminal", Mode: "system_reminder"})

	assert.Equal(t, 40, tr.Total())
	assert.Equal(t, 20, tr.Average())

	recent := tr.Recent(5)
	assert.Len(t, recent, 2)
	assert.Equal(t, 10, recent[0].Tokens)
	assert.Equal(t, 30, recent[1].Tokens)
}

func TestTokenTrackerRingBuffer(t *testing.T) {
	tr := NewTokenTracker()
	for i := 0; i < 55; i++ {
		tr.Record(TokenEntry{Time: time.Now(), Tokens: i + 1, Reason: "r", App: "A", Mode: "system_reminder"})
	}
	// Total accumulates all 55 even though only last 50 retained.
	// sum(1..55) = 55*56/2 = 1540
	assert.Equal(t, 1540, tr.Total())

	recent := tr.Recent(100)
	assert.Len(t, recent, 50, "ring buffer should hold at most 50")
	assert.Equal(t, 6, recent[0].Tokens, "oldest retained should be entry #6")
	assert.Equal(t, 55, recent[len(recent)-1].Tokens)
}

func TestTokenTrackerFormatSummaryWithEntries(t *testing.T) {
	tr := NewTokenTracker()
	for i := 0; i < 3; i++ {
		tr.Record(TokenEntry{
			Time:   time.Date(2026, 4, 14, 12, 0, i, 0, time.UTC),
			Tokens: 10,
			Reason: "pattern",
			App:    "VS Code",
			Mode:   "system_reminder",
		})
	}
	out := tr.FormatSummary()
	assert.Contains(t, out, "Total injected tokens (this session): 30")
	assert.Contains(t, out, "Average per update: 10")
	assert.Contains(t, out, "Recent updates:")
	assert.Contains(t, out, "VS Code")
	// Should contain 3 entries.
	assert.Equal(t, 3, strings.Count(out, "VS Code"))
}

func TestTokenTrackerFormatSummaryCapsAt10(t *testing.T) {
	tr := NewTokenTracker()
	for i := 0; i < 15; i++ {
		tr.Record(TokenEntry{Time: time.Now(), Tokens: 5, App: "A", Reason: "r", Mode: "system_reminder"})
	}
	out := tr.FormatSummary()
	// Only last 10 shown in the recent section.
	assert.Equal(t, 10, strings.Count(out, "  5 tok  A"))
}

func TestTokenTrackerNilSafe(t *testing.T) {
	var tr *TokenTracker
	assert.NotPanics(t, func() {
		tr.Record(TokenEntry{Tokens: 5})
		_ = tr.Total()
		_ = tr.Average()
		_ = tr.Recent(3)
		_ = tr.FormatSummary()
	})
}

func TestDailyBudgetExceeded(t *testing.T) {
	tr := NewTokenTrackerWithBudget(100)
	assert.Equal(t, 100, tr.DailyBudget())
	assert.False(t, tr.BudgetExceeded(), "fresh tracker should not be exceeded")

	tr.Record(TokenEntry{Time: time.Now(), Tokens: 40, Reason: "r", App: "A", Mode: "system_reminder"})
	tr.Record(TokenEntry{Time: time.Now(), Tokens: 40, Reason: "r", App: "A", Mode: "system_reminder"})
	assert.False(t, tr.BudgetExceeded(), "80/100 tokens: still under budget")
	assert.Equal(t, 80, tr.DailyTotal())

	tr.Record(TokenEntry{Time: time.Now(), Tokens: 40, Reason: "r", App: "A", Mode: "system_reminder"})
	assert.True(t, tr.BudgetExceeded(), "120/100 tokens: should trip")
	assert.Equal(t, 120, tr.DailyTotal())
}

func TestDailyBudgetZeroMeansUnlimited(t *testing.T) {
	tr := NewTokenTrackerWithBudget(0)
	for i := 0; i < 100; i++ {
		tr.Record(TokenEntry{Time: time.Now(), Tokens: 1000, Reason: "r", App: "A", Mode: "system_reminder"})
	}
	assert.False(t, tr.BudgetExceeded(), "budget=0 must be unlimited")
	assert.Equal(t, 100000, tr.DailyTotal())
}

func TestDailyBudgetResetAcrossMidnight(t *testing.T) {
	tr := NewTokenTrackerWithBudget(100)
	// Simulate a tracker that accumulated tokens yesterday: poke dayStart back
	// in time and set dailyCount over budget directly. This avoids wall-clock
	// mocking while exercising the rollover branch in rollIfNewDayLocked.
	yesterday := time.Now().Add(-26 * time.Hour)
	tr.mu.Lock()
	tr.dayStart = startOfDay(yesterday)
	tr.dailyCount = 200
	tr.mu.Unlock()
	// DailyTotal/BudgetExceeded should roll the counter because now() is on
	// a later day than dayStart.
	assert.Equal(t, 0, tr.DailyTotal(), "stale yesterday total must roll to 0")
	assert.False(t, tr.BudgetExceeded())

	// Re-stage yesterday's state so the Record-path rollover is exercised too.
	tr.mu.Lock()
	tr.dayStart = startOfDay(yesterday)
	tr.dailyCount = 200
	tr.mu.Unlock()

	// Recording a new entry "today" should roll the counter.
	tr.Record(TokenEntry{Time: time.Now(), Tokens: 10, Reason: "r", App: "A", Mode: "system_reminder"})
	assert.Equal(t, 10, tr.DailyTotal(), "daily counter should have rolled at midnight")
	assert.False(t, tr.BudgetExceeded(), "after rollover, 10/100 is under budget")
}

func TestDailyBudgetNilSafe(t *testing.T) {
	var tr *TokenTracker
	assert.NotPanics(t, func() {
		_ = tr.BudgetExceeded()
		_ = tr.DailyTotal()
		_ = tr.DailyBudget()
		tr.SetDailyBudget(100)
	})
	assert.False(t, tr.BudgetExceeded())
}

func TestDailyBudgetSetRuntime(t *testing.T) {
	tr := NewTokenTracker()
	assert.Equal(t, 0, tr.DailyBudget())
	tr.SetDailyBudget(500)
	assert.Equal(t, 500, tr.DailyBudget())
	tr.SetDailyBudget(-10)
	assert.Equal(t, 0, tr.DailyBudget(), "negative clamps to 0")
}

func TestEstimateTokens(t *testing.T) {
	assert.Equal(t, 1, EstimateTokens(""))
	assert.Equal(t, 1, EstimateTokens("abc"))
	assert.Equal(t, 25, EstimateTokens(strings.Repeat("a", 100)))
	assert.Equal(t, 250, EstimateTokens(strings.Repeat("x", 1000)))
}
