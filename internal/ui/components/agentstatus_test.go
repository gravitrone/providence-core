package components

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentStatusEmptyView(t *testing.T) {
	m := NewAgentStatus()
	view := m.View()
	assert.Empty(t, view, "empty agent list should produce empty view")
}

func TestAgentStatusRunningIcon(t *testing.T) {
	m := NewAgentStatus()
	m.Width = 60
	m.Frame = 5
	m.Agents = []AgentStatusInfo{
		{
			Name:         "fix-auth-bug",
			Model:        "opus",
			Status:       AgentRunning,
			Elapsed:      12 * time.Second,
			LastActivity: "Reading internal/auth/handler.go",
		},
	}
	view := m.View()
	plain := SanitizeText(view)
	assert.Contains(t, plain, "fix-auth-bug", "should contain agent name")
	assert.Contains(t, plain, "[opus]", "should contain model in brackets")
	assert.Contains(t, plain, "12s", "should contain elapsed time")
	// Running icon is ● (U+25CF).
	assert.Contains(t, view, "\u25CF", "running agent should show filled circle icon")
}

func TestAgentStatusCompletedIcon(t *testing.T) {
	m := NewAgentStatus()
	m.Width = 60
	m.Agents = []AgentStatusInfo{
		{
			Name:         "verify-tests",
			Model:        "sonnet",
			Status:       AgentCompleted,
			Elapsed:      8 * time.Second,
			LastActivity: "PASS (14 checks, 0 failures)",
		},
	}
	view := m.View()
	// Completed icon is check mark (U+2713).
	assert.Contains(t, view, "\u2713", "completed agent should show check mark")
	plain := SanitizeText(view)
	assert.Contains(t, plain, "verify-tests")
	assert.Contains(t, plain, "[sonnet]")
}

func TestAgentStatusFailedIcon(t *testing.T) {
	m := NewAgentStatus()
	m.Width = 60
	m.Agents = []AgentStatusInfo{
		{
			Name:    "broken-task",
			Model:   "haiku",
			Status:  AgentFailed,
			Elapsed: 3 * time.Second,
		},
	}
	view := m.View()
	// Failed icon is multiplication sign (U+00D7).
	assert.Contains(t, view, "\u00D7", "failed agent should show x icon")
}

func TestAgentStatusBackgroundIcon(t *testing.T) {
	m := NewAgentStatus()
	m.Width = 60
	m.Agents = []AgentStatusInfo{
		{
			Name:         "codex-review",
			Model:        "gpt-5.4",
			Status:       AgentBackground,
			Elapsed:      2 * time.Minute,
			LastActivity: "Analyzing tool execution...",
		},
	}
	view := m.View()
	// Background icon is diamond (U+25C7).
	assert.Contains(t, view, "\u25C7", "background agent should show diamond icon")
	plain := SanitizeText(view)
	assert.Contains(t, plain, "[gpt-5.4]")
	assert.Contains(t, plain, "2m", "should show minutes for duration >= 1m")
}

func TestAgentStatusMultipleAgents(t *testing.T) {
	m := NewAgentStatus()
	m.Width = 60
	m.Agents = []AgentStatusInfo{
		{Name: "agent-1", Model: "opus", Status: AgentRunning, Elapsed: 5 * time.Second},
		{Name: "agent-2", Model: "sonnet", Status: AgentCompleted, Elapsed: 10 * time.Second},
		{Name: "agent-3", Model: "haiku", Status: AgentFailed, Elapsed: 2 * time.Second},
	}
	view := m.View()
	plain := SanitizeText(view)
	assert.Contains(t, plain, "agent-1")
	assert.Contains(t, plain, "agent-2")
	assert.Contains(t, plain, "agent-3")
}

func TestAgentStatusActivityLine(t *testing.T) {
	m := NewAgentStatus()
	m.Width = 60
	m.Agents = []AgentStatusInfo{
		{
			Name:         "worker",
			Model:        "opus",
			Status:       AgentCompleted,
			Elapsed:      5 * time.Second,
			LastActivity: "Tests passed",
		},
	}
	view := m.View()
	// Activity line should use the ⎿ connector (U+23BF).
	assert.Contains(t, view, "\u23BF", "activity line should use ⎿ connector")
	plain := SanitizeText(view)
	assert.Contains(t, plain, "Tests passed")
}

func TestAgentStatusChildIndent(t *testing.T) {
	m := NewAgentStatus()
	m.Width = 60
	m.Agents = []AgentStatusInfo{
		{Name: "parent", Model: "opus", Status: AgentRunning, Elapsed: 10 * time.Second},
		{Name: "child", Model: "haiku", Status: AgentRunning, Elapsed: 3 * time.Second, ParentName: "parent"},
	}
	view := m.View()
	lines := strings.Split(view, "\n")
	require.GreaterOrEqual(t, len(lines), 2, "should have at least 2 lines")
	// Child line should be more indented than parent.
	parentLine := lines[0]
	childLine := lines[1]
	parentIndent := len(parentLine) - len(strings.TrimLeft(SanitizeText(parentLine), " "))
	childIndent := len(childLine) - len(strings.TrimLeft(SanitizeText(childLine), " "))
	assert.Greater(t, childIndent, parentIndent, "child should be more indented than parent")
}

func TestAgentStatusExpandedResult(t *testing.T) {
	m := NewAgentStatus()
	m.Width = 60
	m.Agents = []AgentStatusInfo{
		{
			Name:        "done-agent",
			Model:       "opus",
			Status:      AgentCompleted,
			Elapsed:     5 * time.Second,
			Expanded:    true,
			ResultLines: []string{"line 1 of result", "line 2 of result", "line 3 of result"},
		},
	}
	view := m.View()
	plain := SanitizeText(view)
	assert.Contains(t, plain, "line 1 of result")
	assert.Contains(t, plain, "line 2 of result")
	assert.Contains(t, plain, "line 3 of result")
}

func TestAgentStatusExpandedResultTruncated(t *testing.T) {
	m := NewAgentStatus()
	m.Width = 60
	m.Agents = []AgentStatusInfo{
		{
			Name:        "verbose-agent",
			Model:       "opus",
			Status:      AgentCompleted,
			Elapsed:     5 * time.Second,
			Expanded:    true,
			ResultLines: []string{"line 1", "line 2", "line 3", "line 4", "line 5"},
		},
	}
	view := m.View()
	plain := SanitizeText(view)
	assert.Contains(t, plain, "line 1")
	assert.Contains(t, plain, "line 2")
	assert.Contains(t, plain, "line 3")
	assert.NotContains(t, plain, "line 4", "should truncate after 3 lines")
	assert.Contains(t, plain, "+2 more lines", "should show more indicator")
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{500 * time.Millisecond, "<1s"},
		{1 * time.Second, "1s"},
		{12 * time.Second, "12s"},
		{59 * time.Second, "59s"},
		{60 * time.Second, "1m"},
		{90 * time.Second, "1m30s"},
		{2 * time.Minute, "2m"},
		{5*time.Minute + 30*time.Second, "5m30s"},
		{1 * time.Hour, "1h0m"},
		{1*time.Hour + 30*time.Minute, "1h30m"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			got := FormatDuration(tc.d)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestAgentStatusKilledIcon(t *testing.T) {
	m := NewAgentStatus()
	m.Width = 60
	m.Agents = []AgentStatusInfo{
		{Name: "killed-agent", Model: "opus", Status: AgentKilled, Elapsed: 5 * time.Second},
	}
	view := m.View()
	// Killed uses same icon as failed: U+00D7.
	assert.Contains(t, view, "\u00D7", "killed agent should show x icon")
}
