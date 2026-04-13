package panels

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Approvals ---

func TestRenderApprovals(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		got := RenderApprovals(nil, 80)
		assert.Equal(t, "  No pending approvals", got)
	})

	t.Run("with items", func(t *testing.T) {
		pending := []PendingApproval{
			{ToolName: "Bash", Args: "rm -rf /tmp/test", Age: "30s"},
			{ToolName: "Write", Args: "/etc/hosts", Age: "1m"},
		}
		got := RenderApprovals(pending, 80)
		require.Contains(t, got, "Bash")
		require.Contains(t, got, "Write")
		assert.Equal(t, 2, strings.Count(got, "\n")+1)
	})
}

// --- Agents ---

func TestRenderAgents(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		got := RenderAgents(nil, 80)
		assert.Equal(t, "  No active agents", got)
	})

	t.Run("active agents", func(t *testing.T) {
		agents := []AgentInfo{
			{Name: "worker-1", Status: "active", Elapsed: "12s"},
			{Name: "worker-2", Status: "idle", Elapsed: "4m"},
		}
		got := RenderAgents(agents, 80)
		assert.Contains(t, got, "* worker-1")
		assert.Contains(t, got, "o worker-2")
	})

	t.Run("mixed statuses", func(t *testing.T) {
		agents := []AgentInfo{
			{Name: "a", Status: "active", Elapsed: "1s"},
			{Name: "b", Status: "idle", Elapsed: "2s"},
			{Name: "c", Status: "offline", Elapsed: "3s"},
		}
		got := RenderAgents(agents, 80)
		lines := strings.Split(got, "\n")
		require.Len(t, lines, 3)
		// active and offline both get "*", idle gets "o"
		assert.Contains(t, lines[0], "* a")
		assert.Contains(t, lines[1], "o b")
		assert.Contains(t, lines[2], "* c")
	})
}

// --- Tokens ---

func TestRenderTokens(t *testing.T) {
	tests := []struct {
		name    string
		current int
		max     int
		width   int
		check   func(t *testing.T, got string)
	}{
		{
			name: "zero max", current: 0, max: 0, width: 40,
			check: func(t *testing.T, got string) {
				assert.Equal(t, "  0% context used", got)
			},
		},
		{
			name: "50 percent", current: 50000, max: 100000, width: 40,
			check: func(t *testing.T, got string) {
				assert.Contains(t, got, "50%")
				assert.Contains(t, got, "50K / 100K tokens")
			},
		},
		{
			name: "90 percent", current: 90000, max: 100000, width: 40,
			check: func(t *testing.T, got string) {
				assert.Contains(t, got, "90%")
			},
		},
		{
			name: "100 percent", current: 100000, max: 100000, width: 40,
			check: func(t *testing.T, got string) {
				assert.Contains(t, got, "100%")
				// bar should be all filled blocks
				assert.NotContains(t, got, "\u2591")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderTokens(tt.current, tt.max, tt.width)
			tt.check(t, got)
		})
	}
}

// --- Tasks ---

func TestRenderTasks(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		got := RenderTasks(nil, 80)
		assert.Equal(t, "  No tasks", got)
	})

	t.Run("with items", func(t *testing.T) {
		todos := []TaskInfo{
			{Content: "Build panels", Status: "in_progress", Depth: 0},
			{Content: "Write tests", Status: "pending", Depth: 0},
			{Content: "Fix bug", Status: "completed", Depth: 0},
			{Content: "Retry deploy", Status: "failed", Depth: 0},
		}
		got := RenderTasks(todos, 80)
		assert.Contains(t, got, "* Build panels")
		assert.Contains(t, got, "o Write tests")
		assert.Contains(t, got, "v Fix bug")
		assert.Contains(t, got, "x Retry deploy")
	})

	t.Run("nested", func(t *testing.T) {
		todos := []TaskInfo{
			{Content: "Parent task", Status: "in_progress", Depth: 0},
			{Content: "Child task", Status: "pending", Depth: 1},
		}
		got := RenderTasks(todos, 80)
		lines := strings.Split(got, "\n")
		require.Len(t, lines, 2)
		assert.True(t, strings.HasPrefix(lines[0], "  "), "parent should have 2-space indent")
		assert.True(t, strings.HasPrefix(lines[1], "    "), "child should have 4-space indent")
	})
}

// --- Files ---

func TestRenderFiles(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		got := RenderFiles(nil, 80)
		assert.Equal(t, "  No files touched", got)
	})

	t.Run("modified and read-only", func(t *testing.T) {
		files := []FileInfo{
			{Path: "main.go", Modified: true},
			{Path: "go.sum", ReadOnly: true},
			{Path: "README.md"},
		}
		got := RenderFiles(files, 80)
		lines := strings.Split(got, "\n")
		require.Len(t, lines, 3)
		assert.Contains(t, lines[0], "* main.go")
		assert.Contains(t, lines[1], "o go.sum")
		assert.Contains(t, lines[2], "  README.md")
	})
}

// --- Errors ---

func TestRenderErrors(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		got := RenderErrors(nil, 80)
		assert.Equal(t, "  No errors", got)
	})

	t.Run("with items", func(t *testing.T) {
		errs := []ErrorInfo{
			{Tool: "Bash", Message: "exit code 1", Age: "5s"},
			{Tool: "Write", Message: "permission denied", Age: "30s"},
		}
		got := RenderErrors(errs, 80)
		assert.Contains(t, got, "Bash: exit code 1")
		assert.Contains(t, got, "Write: permission denied")
	})
}

// --- Compact ---

func TestRenderCompact(t *testing.T) {
	t.Run("never compacted", func(t *testing.T) {
		got := RenderCompact(CompactInfo{}, 80)
		assert.Equal(t, "  Never compacted", got)
	})

	t.Run("after compaction", func(t *testing.T) {
		state := CompactInfo{
			LastRun:      "2m ago",
			BeforePct:    85,
			AfterPct:     40,
			ThresholdPct: 80,
			Mode:         "cc-tail-replace",
		}
		got := RenderCompact(state, 80)
		assert.Contains(t, got, "2m ago")
		assert.Contains(t, got, "85% -> 40%")
		assert.Contains(t, got, "~80% fill")
		assert.Contains(t, got, "cc-tail-replace")
	})
}

// --- Hooks ---

func TestRenderHooks(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		got := RenderHooks(nil, 80)
		assert.Equal(t, "  No hooks configured", got)
	})

	t.Run("with firings", func(t *testing.T) {
		hooks := []HookInfo{
			{Event: "PreToolUse", LastFired: "0.2s"},
			{Event: "PostToolUse", LastFired: ""},
		}
		got := RenderHooks(hooks, 80)
		assert.Contains(t, got, "fired 0.2s")
		assert.Contains(t, got, "idle")
	})
}

// --- Zero/Empty edge cases ---

func TestRenderTokensZero(t *testing.T) {
	got := RenderTokens(0, 100000, 40)
	assert.Contains(t, got, "0%")
}

func TestRenderTokensFull(t *testing.T) {
	got := RenderTokens(100000, 100000, 40)
	assert.Contains(t, got, "100%")
}

func TestRenderAgentsEmpty(t *testing.T) {
	got := RenderAgents(nil, 80)
	assert.Contains(t, got, "No active agents")
}

func TestRenderFilesEmpty(t *testing.T) {
	got := RenderFiles(nil, 80)
	assert.Contains(t, got, "No files touched")
}

func TestRenderErrorsEmpty(t *testing.T) {
	got := RenderErrors(nil, 80)
	assert.Contains(t, got, "No errors")
}

func TestAllEmptyPanelsReturnEmpty(t *testing.T) {
	// Every render function with nil/zero input should return a non-empty
	// placeholder string (not "").
	checks := []string{
		RenderAgents(nil, 80),
		RenderFiles(nil, 80),
		RenderErrors(nil, 80),
		RenderTasks(nil, 80),
		RenderApprovals(nil, 80),
		RenderHooks(nil, 80),
		RenderCompact(CompactInfo{}, 80),
		RenderTokens(0, 0, 40),
	}
	for i, s := range checks {
		assert.NotEmpty(t, s, "panel render %d should not be empty", i)
	}
}

func TestPanelWidthRespected(t *testing.T) {
	width := 60
	agents := []AgentInfo{
		{Name: "worker-1", Status: "active", Elapsed: "12s"},
	}
	got := RenderAgents(agents, width)
	for _, line := range strings.Split(got, "\n") {
		assert.LessOrEqual(t, len(line), width, "line exceeds panel width: %q", line)
	}

	files := []FileInfo{
		{Path: strings.Repeat("x", 200), Modified: true},
	}
	got = RenderFiles(files, width)
	for _, line := range strings.Split(got, "\n") {
		assert.LessOrEqual(t, len(line), width, "line exceeds panel width: %q", line)
	}
}

// --- Truncate ---

func TestTruncate(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short string", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"long string", "hello world", 8, "hello..."},
		{"very short max", "hello", 2, "he"},
		{"max 3", "hello", 3, "hel"},
		{"max 4", "hello world", 4, "h..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.input, tt.maxLen)
			assert.Equal(t, tt.want, got)
		})
	}
}
