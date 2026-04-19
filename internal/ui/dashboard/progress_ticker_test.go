package dashboard

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedNow builds a stable reference timestamp used by tests.
func fixedNow() time.Time {
	return time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
}

func TestSubagentProgressFormats(t *testing.T) {
	now := fixedNow()

	cases := []struct {
		name     string
		input    SubagentProgress
		contains []string
	}{
		{
			name: "canonical line",
			input: SubagentProgress{
				AgentID:    "a1",
				AgentType:  "researcher",
				LastTool:   "Read file.go",
				LastToolAt: now.Add(-2 * time.Minute),
				StartedAt:  now.Add(-5*time.Minute - 12*time.Second),
			},
			contains: []string{"researcher", "Read file.go", "2m ago", "since 00:05:12"},
		},
		{
			name: "missing tool falls back to idle",
			input: SubagentProgress{
				AgentType: "implementer",
				StartedAt: now.Add(-30 * time.Second),
			},
			contains: []string{"implementer", "idle", "since 00:00:30"},
		},
		{
			name: "missing type falls back to agent id",
			input: SubagentProgress{
				AgentID:    "xyz",
				LastTool:   "Bash",
				LastToolAt: now.Add(-45 * time.Second),
				StartedAt:  now.Add(-time.Minute),
			},
			contains: []string{"xyz", "Bash", "45s ago", "since 00:01:00"},
		},
		{
			name: "hour granularity for old tool calls",
			input: SubagentProgress{
				AgentType:  "verifier",
				LastTool:   "Grep",
				LastToolAt: now.Add(-2 * time.Hour),
				StartedAt:  now.Add(-3 * time.Hour),
			},
			contains: []string{"verifier", "Grep", "2h ago", "since 03:00:00"},
		},
		{
			name: "zero last-tool time reports just now",
			input: SubagentProgress{
				AgentType: "idle-agent",
				LastTool:  "Waiting",
				StartedAt: now.Add(-10 * time.Second),
			},
			contains: []string{"idle-agent", "Waiting", "just now", "since 00:00:10"},
		},
		{
			name: "summary appended when present",
			input: SubagentProgress{
				AgentType:  "researcher",
				LastTool:   "WebSearch",
				LastToolAt: now.Add(-10 * time.Second),
				StartedAt:  now.Add(-1 * time.Minute),
				Summary:    "scanning docs",
			},
			contains: []string{"researcher", "WebSearch", "10s ago", "scanning docs"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			line := FormatSubagentProgress(tc.input, now)
			for _, want := range tc.contains {
				assert.Contains(t, line, want, "rendered line %q should contain %q", line, want)
			}
			// Bullet separator must be present.
			assert.Contains(t, line, "\u2022", "line should use bullet separator")
			// Hard rule: no em/en dash anywhere.
			assert.NotContains(t, line, "\u2014", "em dash forbidden")
			assert.NotContains(t, line, "\u2013", "en dash forbidden")
		})
	}
}

func TestProgressTickerEmitsOnInterval(t *testing.T) {
	// Record sleep calls and fire on demand via a controlled channel.
	sleepCalls := make(chan time.Duration, 4)
	fire := make(chan time.Time, 1)

	now := fixedNow()
	want := []SubagentProgress{
		{AgentID: "a1", AgentType: "researcher", LastTool: "Read", LastToolAt: now, StartedAt: now},
	}

	var sourceCalls int32
	src := func() []SubagentProgress {
		atomic.AddInt32(&sourceCalls, 1)
		return want
	}

	ticker := NewProgressTicker(src).
		WithInterval(30 * time.Second).
		WithClock(func() time.Time { return now }).
		WithSleep(func(d time.Duration) <-chan time.Time {
			sleepCalls <- d
			return fire
		})

	cmd := ticker.Tick()
	require.NotNil(t, cmd, "tick cmd should be non-nil while ticker is live")

	// Run the cmd on a goroutine and wait for it to consume the sleep.
	msgCh := make(chan interface{}, 1)
	go func() {
		msgCh <- cmd()
	}()

	select {
	case d := <-sleepCalls:
		assert.Equal(t, 30*time.Second, d, "ticker should sleep for configured interval")
	case <-time.After(time.Second):
		t.Fatal("ticker did not request sleep within 1s")
	}

	// Fire the injected timer; ticker should emit exactly one ProgressTickMsg.
	fire <- now

	select {
	case raw := <-msgCh:
		msg, ok := raw.(ProgressTickMsg)
		require.True(t, ok, "expected ProgressTickMsg, got %T", raw)
		assert.Equal(t, now, msg.At, "tick message should carry injected clock value")
		require.Len(t, msg.Snapshot, 1)
		assert.Equal(t, "researcher", msg.Snapshot[0].AgentType)
		assert.GreaterOrEqual(t, atomic.LoadInt32(&sourceCalls), int32(1),
			"source should have been polled at least once")
	case <-time.After(time.Second):
		t.Fatal("ticker did not emit ProgressTickMsg after fire")
	}

	// Second tick confirms interval chaining still works after one fire.
	cmd2 := ticker.Tick()
	require.NotNil(t, cmd2, "second tick cmd should be non-nil")

	msgCh2 := make(chan interface{}, 1)
	go func() {
		msgCh2 <- cmd2()
	}()

	select {
	case d := <-sleepCalls:
		assert.Equal(t, 30*time.Second, d, "second tick should also sleep interval")
	case <-time.After(time.Second):
		t.Fatal("second tick did not request sleep")
	}

	// Stop the ticker before firing the second sleep to ensure no leak.
	ticker.Stop()

	select {
	case raw := <-msgCh2:
		// Stop races with fire; accept either nil or ProgressTickMsg, but
		// the goroutine must have exited (no leak).
		if raw != nil {
			_, ok := raw.(ProgressTickMsg)
			assert.True(t, ok, "post-stop message should be ProgressTickMsg or nil")
		}
	case <-time.After(time.Second):
		t.Fatal("stop did not release the blocked tick goroutine")
	}
}

func TestProgressTickerHandlesZeroSubagents(t *testing.T) {
	now := fixedNow()

	// Source returns an empty slice.
	src := func() []SubagentProgress { return nil }

	ticker := NewProgressTicker(src).
		WithInterval(time.Millisecond).
		WithClock(func() time.Time { return now }).
		WithSleep(func(d time.Duration) <-chan time.Time {
			// Fire immediately so the test stays time-independent.
			ch := make(chan time.Time, 1)
			ch <- now
			return ch
		})

	cmd := ticker.Tick()
	require.NotNil(t, cmd)

	raw := cmd()
	msg, ok := raw.(ProgressTickMsg)
	require.True(t, ok, "expected ProgressTickMsg, got %T", raw)
	assert.Empty(t, msg.Snapshot, "snapshot should be empty when no subagents are active")

	// Snapshot() used directly must also return empty for nil source output.
	assert.Empty(t, ticker.Snapshot())

	// Panel render of an empty snapshot must include "no subagents".
	body := RenderSubagentProgressPanel(nil, 60, now)
	assert.Contains(t, body, "no subagents",
		"empty progress panel must render 'no subagents' per spec")

	// Dashboard integration: installing an empty snapshot still creates the
	// panel and the panel body renders the empty-state string.
	d := newTestDashboard(60, 30)
	d.SetSubagentProgress(nil, now)
	panel := d.PanelByID("subagent_progress")
	require.NotNil(t, panel, "SetSubagentProgress should create the panel")
	assert.Empty(t, panel.Badge, "empty snapshot should clear badge")
	require.NotNil(t, panel.Render)
	assert.Contains(t, panel.Render(60), "no subagents")

	// Stop must be idempotent and leak-free.
	ticker.Stop()
	ticker.Stop()
	assert.Nil(t, ticker.Tick(), "post-stop Tick should return nil cmd")
}

func TestSubagentProgressPanelRendersRows(t *testing.T) {
	now := fixedNow()
	snap := []SubagentProgress{
		{
			AgentType:  "researcher",
			LastTool:   "Read file.go",
			LastToolAt: now.Add(-2 * time.Minute),
			StartedAt:  now.Add(-5*time.Minute - 12*time.Second),
		},
		{
			AgentType:  "implementer",
			LastTool:   "Edit dashboard.go",
			LastToolAt: now.Add(-30 * time.Second),
			StartedAt:  now.Add(-2 * time.Minute),
		},
	}

	body := RenderSubagentProgressPanel(snap, 80, now)
	assert.Contains(t, body, "researcher")
	assert.Contains(t, body, "implementer")
	assert.Contains(t, body, "Read file.go")
	assert.Contains(t, body, "Edit dashboard.go")
	assert.Contains(t, body, "since 00:05:12")
	assert.Contains(t, body, "since 00:02:00")

	// One row per subagent.
	lines := strings.Split(body, "\n")
	assert.Len(t, lines, 2, "panel should render one line per subagent")
}

func TestSetSubagentProgressUpdatesPanelBadge(t *testing.T) {
	d := newTestDashboard(80, 40)
	now := fixedNow()

	snap := []SubagentProgress{
		{AgentType: "a", LastTool: "Read", LastToolAt: now, StartedAt: now},
		{AgentType: "b", LastTool: "Edit", LastToolAt: now, StartedAt: now},
		{AgentType: "c", LastTool: "Bash", LastToolAt: now, StartedAt: now},
	}

	d.SetSubagentProgress(snap, now)
	p := d.PanelByID("subagent_progress")
	require.NotNil(t, p)
	assert.Equal(t, "[3]", p.Badge, "badge should reflect subagent count")

	// Shrinking the snapshot to zero clears the badge and swaps body text.
	d.SetSubagentProgress(nil, now)
	p = d.PanelByID("subagent_progress")
	require.NotNil(t, p)
	assert.Empty(t, p.Badge)
	assert.Contains(t, p.Render(60), "no subagents")
}
