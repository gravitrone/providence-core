package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gravitrone/providence-core/internal/config"
	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/gravitrone/providence-core/internal/engine/direct"
	"github.com/gravitrone/providence-core/internal/ui/components"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAgentTab(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	// Creates without panic, dbPath stored
	assert.NotNil(t, at)
	// Input is initialized and focused
	assert.True(t, at.input.Focused())
}

func TestAgentTabView(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	out := at.View(120, 40)
	// Does not panic, produces output
	assert.NotEmpty(t, out)
}

func TestAgentTabViewContainsInputArea(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	out := at.View(120, 40)
	// The view should contain the ❯ prompt from the textarea.
	assert.Contains(t, out, "❯", "view should contain the input prompt indicator")
}

func TestAgentTabFocusedWhenIdle(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	// Agent tab is NOT focused when idle (allows tab switching)
	assert.False(t, at.Focused())
}

func TestAgentTabFocusedWhenStreaming(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.streaming = true
	assert.True(t, at.Focused())
}

func TestAgentTabHintsDefaultState(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	hints := at.Hints()
	// Default state has no hints (clean UI).
	assert.Nil(t, hints)
}

func TestAgentTabHintsStreamingState(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.streaming = true
	hints := at.Hints()
	// Streaming without queue has no hints.
	assert.Nil(t, hints)
}

func TestAgentTabHintsQueueSelected(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.queue = []QueuedMessage{{Text: "test"}}
	at.queueCursor = 0
	hints := at.Hints()
	require.NotEmpty(t, hints)

	keys := make([]string, len(hints))
	for i, h := range hints {
		keys[i] = h.Key
	}
	assert.Contains(t, keys, "enter", "should have steer hint")
	assert.Contains(t, keys, "del", "should have remove hint")
	assert.Contains(t, keys, "esc", "should have back hint")
}

func TestAgentTabHintsPermissionPending(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.pendingPerm = &engine.PermissionRequestEvent{
		QuestionID: "q-1",
	}
	hints := at.Hints()
	require.NotEmpty(t, hints)

	keys := make([]string, len(hints))
	for i, h := range hints {
		keys[i] = h.Key
	}
	assert.Contains(t, keys, "y", "permission mode should have approve hint")
	assert.Contains(t, keys, "n", "permission mode should have deny hint")
}

func TestAgentTabStatusLineShowsContextPillWhenEngineActive(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.model = "sonnet"
	at.engine = &direct.DirectEngine{}
	at.currentTokens = 120000

	out := at.StatusLine()

	assert.Contains(t, out, "60%")
	assert.Contains(t, out, "ctx")
}

func TestStatusLineSingleRow(t *testing.T) {
	at := NewAgentTab("direct", config.Config{}, nil)
	at.model = "sonnet"
	at.width = 200
	at.engine = &direct.DirectEngine{}
	at.currentTokens = 50000
	at.messages = []ChatMessage{{Role: "user", Content: "hi", Done: true}}

	out := at.StatusLine()

	// All pills rendered in one StatusBarFromItems call (single bar).
	// The bordered segments produce 3 visual lines (top border, content, bottom border),
	// but all pills must be on that same single bar, not split across sections.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	assert.Equal(t, 3, len(lines), "bordered status bar should be exactly 3 lines (top/content/bottom)")
	// All pill content is on the middle line.
	assert.Contains(t, lines[1], "direct", "engine pill on content line")
	assert.Contains(t, lines[1], "cwd", "cwd pill on content line")
	assert.Contains(t, lines[1], "ctrl+o", "freeze pill on content line")
}

func TestStatusLineTruncation(t *testing.T) {
	at := NewAgentTab("direct", config.Config{}, nil)
	at.model = "sonnet"
	at.width = 60 // narrow - should drop low-priority pills
	at.engine = &direct.DirectEngine{}
	at.currentTokens = 50000
	at.messages = []ChatMessage{{Role: "user", Content: "hi", Done: true}}

	out := at.StatusLine()

	// Engine pill (first) should survive.
	assert.Contains(t, out, "direct", "engine pill must survive narrow width")
	// Low-priority pills (cwd, freeze) should be dropped or replaced with overflow.
	// We just check it doesn't panic and produces output.
	assert.NotEmpty(t, out)
}

func TestStatusLineAllPills(t *testing.T) {
	at := NewAgentTab("direct", config.Config{}, nil)
	at.model = "sonnet"
	at.width = 300
	at.engine = &direct.DirectEngine{}
	at.currentTokens = 120000
	at.messages = []ChatMessage{{Role: "user", Content: "hi", Done: true}}

	out := at.StatusLine()

	assert.Contains(t, out, "direct", "engine pill missing")
	assert.Contains(t, out, "sonnet", "model pill missing")
	assert.Contains(t, out, "active", "session pill missing")
	assert.Contains(t, out, "60%", "ctx pill missing")
	assert.Contains(t, out, "cwd", "cwd pill missing")
	assert.Contains(t, out, "ctrl+o", "freeze pill missing")
	assert.Contains(t, out, "freeze", "freeze desc missing")
}

func TestAgentTabResize(t *testing.T) {
	tests := []struct {
		name   string
		width  int
		height int
	}{
		{"standard", 120, 40},
		{"wide", 220, 60},
		{"narrow", 60, 24},
		{"tiny", 20, 10},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			at := NewAgentTab("", config.Config{}, nil)
			assert.NotPanics(t, func() {
				at.Resize(tc.width, tc.height)
			})
		})
	}
}

func TestAgentTabResizeUpdatesView(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.Resize(120, 40)
	out := at.View(120, 40)
	assert.NotEmpty(t, out)
}

func TestAgentTabEmptyChat(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	// Initial state has no messages
	assert.Empty(t, at.messages)
	// View should show empty state indicator
	out := at.View(120, 40)
	assert.NotEmpty(t, out)
	// Empty message placeholder should appear
	assert.Contains(t, out, "Providence Awaits")
}

func TestAgentTabInit(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	cmd := at.Init()
	// Init returns flameTick cmd for flame animation.
	assert.NotNil(t, cmd)
}

func TestAgentTabRenderMessagesEmpty(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	rendered := at.renderMessages()
	assert.Contains(t, rendered, "Providence Awaits")
}

func TestAgentTabRenderMessagesWithUserMessage(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.addMessage("user", "find me ML jobs", true)
	rendered := at.renderMessages()
	assert.Contains(t, rendered, "find me ML jobs")
}

func TestAgentTabRenderMessagesWithSystemMessage(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.addMessage("system", "session connected", true)
	rendered := at.renderMessages()
	assert.Contains(t, rendered, "session connected")
}

func TestAgentTabViewWidthPropagation(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	// View at a narrower vs wider width should produce different results
	narrow := at.View(60, 30)
	wide := at.View(200, 30)
	assert.NotEqual(t, narrow, wide)
}

func TestChatContentWidth(t *testing.T) {
	tests := []struct {
		name        string
		screenWidth int
		expectMin   int
		expectMax   int
	}{
		{"narrow", 50, 60, 60},
		{"normal", 100, 60, 140},
		{"wide", 200, 60, 140},
		{"very wide", 400, 60, 140},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := chatContentWidth(tc.screenWidth)
			assert.GreaterOrEqual(t, w, tc.expectMin)
			assert.LessOrEqual(t, w, tc.expectMax)
		})
	}
}

func TestFormatToolInput(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		contains string
	}{
		{"nil input", nil, ""},
		{"short string", "hello", "hello"},
		{"long string", strings.Repeat("x", 100), "..."},
		{"map", map[string]any{"url": "https://example.com"}, "example.com"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := formatToolInput(tc.input)
			if tc.contains != "" {
				assert.Contains(t, out, tc.contains)
			} else {
				assert.Empty(t, out)
			}
		})
	}
}

func TestFormatToolInputLongStringTruncated(t *testing.T) {
	long := strings.Repeat("a", 200)
	out := formatToolInput(long)
	assert.LessOrEqual(t, len(out), 83, "should be truncated to ~80 chars + '...'")
	assert.True(t, strings.HasSuffix(out, "..."))
}

// --- Message Queue & Steering Tests ---

func TestQueueMessageDuringStreaming(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.streaming = true
	at.input.SetValue("hello world")

	at, _ = at.handleKey(keyPress("enter"))

	require.Len(t, at.queue, 1)
	assert.Equal(t, "hello world", at.queue[0].Text)
	assert.False(t, at.queue[0].Steered, "regular enter should not steer")
}

func TestQueueMultipleMessages(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.streaming = true

	messages := []string{"first", "second", "third"}
	for _, msg := range messages {
		at.input.SetValue(msg)
		at, _ = at.handleKey(keyPress("enter"))
	}

	require.Len(t, at.queue, 3)
	assert.Equal(t, "first", at.queue[0].Text)
	assert.Equal(t, "second", at.queue[1].Text)
	assert.Equal(t, "third", at.queue[2].Text)
}

func TestShiftEnterInsertsNewline(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)

	// Type some text then shift+enter should insert a newline (handled by textarea).
	at.input.SetValue("line one")
	at, _ = at.handleKey(keyPress("shift+enter"))

	// The textarea should now contain a newline (multiline input).
	val := at.input.Value()
	assert.Contains(t, val, "\n", "shift+enter should insert a newline in textarea")
}

func TestEscExitsQueueSelection(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.streaming = true
	at.queue = []QueuedMessage{
		{Text: "msg1"}, {Text: "msg2"}, {Text: "msg3"},
	}
	at.queueCursor = 1 // selected second message

	at, _ = at.handleKey(keyPress("esc"))

	// Queue should NOT be cleared, just deselected.
	require.Len(t, at.queue, 3)
	assert.Equal(t, -1, at.queueCursor, "cursor should be back to input")
}

func TestUpArrowSelectsLastQueueMessage(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.queue = []QueuedMessage{{Text: "first"}, {Text: "second"}}
	at.input.SetValue("") // empty input required for queue selection

	at, _ = at.handleKey(keyPress("up"))

	assert.Equal(t, 1, at.queueCursor, "should select last message")
}

func TestUpArrowNavigatesQueue(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.queue = []QueuedMessage{{Text: "first"}, {Text: "second"}, {Text: "third"}}
	at.queueCursor = 2

	at, _ = at.handleKey(keyPress("up"))

	assert.Equal(t, 1, at.queueCursor, "should move to previous message")
}

func TestDownArrowNavigatesQueue(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.queue = []QueuedMessage{{Text: "first"}, {Text: "second"}}
	at.queueCursor = 0

	at, _ = at.handleKey(keyPress("down"))

	assert.Equal(t, 1, at.queueCursor, "should move to next message")
}

func TestDownArrowPastLastExitsQueue(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.queue = []QueuedMessage{{Text: "first"}, {Text: "second"}}
	at.queueCursor = 1 // last message

	at, _ = at.handleKey(keyPress("down"))

	assert.Equal(t, -1, at.queueCursor, "should exit queue back to input")
}

func TestEnterOnSelectedSteersMessage(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.queue = []QueuedMessage{{Text: "waiting"}, {Text: "also waiting"}}
	at.queueCursor = 0

	at, _ = at.handleKey(keyPress("enter"))

	assert.True(t, at.queue[0].Steered, "enter should steer the selected message")
	assert.False(t, at.queue[1].Steered, "other messages should be unaffected")
}

func TestEnterOnAlreadySteeredIsNoop(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.queue = []QueuedMessage{{Text: "already steered", Steered: true}}
	at.queueCursor = 0

	at, _ = at.handleKey(keyPress("enter"))

	assert.True(t, at.queue[0].Steered, "should still be steered")
}

func TestDeleteRemovesSelectedMessage(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.queue = []QueuedMessage{{Text: "first"}, {Text: "second"}, {Text: "third"}}
	at.queueCursor = 1 // select "second"

	at, _ = at.handleKey(keyPress("delete"))

	require.Len(t, at.queue, 2)
	assert.Equal(t, "first", at.queue[0].Text)
	assert.Equal(t, "third", at.queue[1].Text)
	assert.Equal(t, 1, at.queueCursor, "cursor should stay at same index")
}

func TestDeleteLastMessageExitsQueue(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.queue = []QueuedMessage{{Text: "only one"}}
	at.queueCursor = 0

	at, _ = at.handleKey(keyPress("delete"))

	assert.Empty(t, at.queue)
	assert.Equal(t, -1, at.queueCursor)
}

func TestBackspaceRemovesSelectedMessage(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.queue = []QueuedMessage{{Text: "first"}, {Text: "second"}}
	at.queueCursor = 0

	at, _ = at.handleKey(keyPress("backspace"))

	require.Len(t, at.queue, 1)
	assert.Equal(t, "second", at.queue[0].Text)
	assert.Equal(t, 0, at.queueCursor)
}

// --- Slash Command Fast Path Tests ---

// TestSlashCommandFiresOnFirstEnter guards against the /resume race where
// the slash command table intercepted the first enter press and required
// a second keystroke to actually fire the command. For commands that add
// messages we assert at least one message lands; for /clear we simply
// assert the input was consumed (messages would stay empty).
func TestSlashCommandFiresOnFirstEnter(t *testing.T) {
	cases := []struct {
		cmd      string
		wantMsgs bool
	}{
		{"/help", true},
		{"/resume", true},
		{"/sessions", true},
		{"/clear", false},
	}
	for _, tc := range cases {
		t.Run(tc.cmd, func(t *testing.T) {
			at := NewAgentTab("", config.Config{}, nil)
			at.Resize(120, 40)
			at.input.SetValue(tc.cmd)

			msgCountBefore := len(at.messages)
			at, _ = at.handleKey(keyPress("enter"))

			assert.Empty(t, at.input.Value(), "input should clear on first enter")
			if tc.wantMsgs {
				assert.Greater(t, len(at.messages), msgCountBefore,
					"slash command should execute on first enter, not wait for a second keystroke")
			}
		})
	}
}

// TestSlashTableNavigation verifies up/down navigates the slash table
// when the input starts with "/".
func TestSlashTableNavigation(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.Resize(120, 40)
	at.input.SetValue("/")

	matches := at.matchingSlashCommands()
	require.GreaterOrEqual(t, len(matches), 3, "need several matches for this test")

	// Initial cursor is -1 (implicit first match).
	assert.Equal(t, -1, at.slashCursor)

	// Down should land on index 0 (the second command since cursor was < 0).
	at, _ = at.handleKey(keyPress("down"))
	assert.Equal(t, 0, at.slashCursor)

	at, _ = at.handleKey(keyPress("down"))
	assert.Equal(t, 1, at.slashCursor)

	at, _ = at.handleKey(keyPress("up"))
	assert.Equal(t, 0, at.slashCursor)
}

// TestCompactIndicatorStateMachine feeds synthetic compaction events
// through handleAgentEvent and verifies the indicator state machine
// transitions cleanly through running -> complete and running -> failed.
func TestCompactIndicatorStateMachine(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.Resize(120, 40)

	// Running phase: indicator should arm with before token count and verb.
	at, _ = at.handleAgentEvent(AgentEventMsg{Event: engine.ParsedEvent{
		Type: "compaction",
		Data: &engine.CompactionEvent{Phase: "running", TokensBefore: 150000},
	}})
	assert.Equal(t, "running", at.compactPhase)
	assert.Equal(t, 150000, at.compactTokensBefore)
	assert.NotEmpty(t, at.compactVerb, "running phase should seed a compact verb")

	// Indicator render should mention the verb and "->" trail.
	out := at.renderCompactIndicator()
	assert.Contains(t, out, at.compactVerb)
	assert.Contains(t, out, "\u2192", "token trail arrow should render")

	// Complete phase: flips to dissolve state with after tokens populated.
	at, _ = at.handleAgentEvent(AgentEventMsg{Event: engine.ParsedEvent{
		Type: "compaction",
		Data: &engine.CompactionEvent{Phase: "idle", TokensAfter: 45000},
	}})
	assert.Equal(t, "complete", at.compactPhase)
	assert.Equal(t, 45000, at.compactTokensAfter)

	// Failed path from a fresh running state.
	at2 := NewAgentTab("", config.Config{}, nil)
	at2.Resize(120, 40)
	at2, _ = at2.handleAgentEvent(AgentEventMsg{Event: engine.ParsedEvent{
		Type: "compaction",
		Data: &engine.CompactionEvent{Phase: "running", TokensBefore: 100000},
	}})
	at2, _ = at2.handleAgentEvent(AgentEventMsg{Event: engine.ParsedEvent{
		Type: "compaction",
		Data: &engine.CompactionEvent{Phase: "failed"},
	}})
	assert.Equal(t, "failed", at2.compactPhase)
}

// TestIsKnownSlashCommand verifies the slash command lookup matches
// registered commands exactly and ignores everything else.
func TestIsKnownSlashCommand(t *testing.T) {
	assert.True(t, isKnownSlashCommand("/clear"))
	assert.True(t, isKnownSlashCommand("/resume"))
	assert.True(t, isKnownSlashCommand("/compact"))
	assert.True(t, isKnownSlashCommand("/Clear"), "case-insensitive")
	assert.False(t, isKnownSlashCommand("/nope"))
	assert.False(t, isKnownSlashCommand("hello"))
	assert.False(t, isKnownSlashCommand("/"))
}

func TestSafeWaitForEventNilSession(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	// session is nil by default
	require.Nil(t, at.engine)

	cmd := at.safeWaitForEvent()

	assert.Nil(t, cmd)
}

func TestPrepareSend(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)

	at.prepareSend("test message")

	assert.True(t, at.streaming)
	require.NotEmpty(t, at.messages)
	assert.Equal(t, "user", at.messages[len(at.messages)-1].Role)
	assert.Equal(t, "test message", at.messages[len(at.messages)-1].Content)
	assert.NotEmpty(t, at.spinnerVerb)
}

func TestRenderQueuedMessages(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.streaming = true
	at.queue = []QueuedMessage{{Text: "find me remote ML jobs"}}
	// Need at least one message so renderMessages doesn't show empty state.
	at.addMessage("user", "test prompt", true)

	rendered := at.renderMessages()

	assert.Contains(t, rendered, "Queue")
}

func TestRenderSteeredMessage(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.streaming = true
	at.queue = []QueuedMessage{{Text: "urgent task", Steered: true}}
	at.addMessage("user", "test prompt", true)

	rendered := at.renderMessages()

	assert.Contains(t, rendered, "Steer")
}

func TestRenderSelectedMessage(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.streaming = true
	at.queue = []QueuedMessage{{Text: "selectable"}, {Text: "also here"}}
	at.queueCursor = 0
	at.addMessage("user", "test prompt", true)

	rendered := at.renderMessages()

	assert.Contains(t, rendered, "enter: steer")
	assert.Contains(t, rendered, "del: remove")
}

// --- Batch Tool Display Tests ---

func TestBatchGrouping_ConsecutiveSameTool(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.width = 120
	// Add 5 consecutive Read tool messages.
	for i := 0; i < 5; i++ {
		at.messages = append(at.messages, ChatMessage{
			Role:       "tool",
			ToolName:   "Read",
			ToolArgs:   fmt.Sprintf("file%d.go", i),
			ToolStatus: "success",
			ToolBody:   "read ok",
			Done:       true,
		})
	}
	rendered := at.renderMessages()
	// Should show batch header with "Reading 5 files" and ctrl+o hint.
	plain := components.SanitizeText(rendered)
	assert.Contains(t, plain, "Read 5 files", "should show batch count in past tense")
	assert.Contains(t, rendered, "ctrl+o", "should show expand hint")
	// Individual tool args should appear in the compressed args line.
	assert.Contains(t, rendered, "file0.go", "should show first file arg")
}

func TestBatchGrouping_NonConsecutiveNotGrouped(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.width = 120
	at.messages = append(at.messages,
		ChatMessage{Role: "tool", ToolName: "Read", ToolArgs: "a.go", ToolStatus: "success", Done: true},
		ChatMessage{Role: "tool", ToolName: "Write", ToolArgs: "b.go", ToolStatus: "success", Done: true},
		ChatMessage{Role: "tool", ToolName: "Read", ToolArgs: "c.go", ToolStatus: "success", Done: true},
	)
	rendered := at.renderMessages()
	// No batching should occur - each tool appears individually.
	assert.NotContains(t, rendered, "calls)", "should not show batch header for non-consecutive tools")
}

func TestBatchGrouping_SingleToolNotGrouped(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.width = 120
	at.messages = append(at.messages,
		ChatMessage{Role: "tool", ToolName: "Read", ToolArgs: "only.go", ToolStatus: "success", Done: true},
	)
	rendered := at.renderMessages()
	assert.NotContains(t, rendered, "calls)", "single tool should not be batched")
	assert.Contains(t, rendered, "only.go")
}

func TestBatchGrouping_MixedTools(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.width = 120
	// Read, Read, Write -> first 2 grouped, Write standalone.
	at.messages = append(at.messages,
		ChatMessage{Role: "tool", ToolName: "Read", ToolArgs: "a.go", ToolStatus: "success", Done: true},
		ChatMessage{Role: "tool", ToolName: "Read", ToolArgs: "b.go", ToolStatus: "success", Done: true},
		ChatMessage{Role: "tool", ToolName: "Write", ToolArgs: "c.go", ToolStatus: "success", Done: true},
	)
	rendered := at.renderMessages()
	plain := components.SanitizeText(rendered)
	assert.Contains(t, plain, "Read 2 files", "first 2 Read tools should be batched in past tense")
	assert.Contains(t, rendered, "Write", "Write tool should appear standalone")
}

func TestBatchGrouping_ExpandedShowsAll(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.width = 120
	at.toolsExpanded = true
	for i := 0; i < 3; i++ {
		at.messages = append(at.messages, ChatMessage{
			Role:       "tool",
			ToolName:   "Read",
			ToolArgs:   fmt.Sprintf("file%d.go", i),
			ToolStatus: "success",
			ToolBody:   "read ok",
			Done:       true,
		})
	}
	rendered := at.renderMessages()
	// When expanded, all individual tools should render (no batch header).
	assert.NotContains(t, rendered, "calls)", "expanded mode should not show batch header")
	assert.Contains(t, rendered, "file0.go")
	assert.Contains(t, rendered, "file1.go")
	assert.Contains(t, rendered, "file2.go")
}

func TestCtrlOEntersFreezeMode(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.messages = append(at.messages, ChatMessage{Role: "user", Content: "hello"})
	assert.Equal(t, FocusInput, at.focus)

	at, _ = at.handleKey(keyPress("ctrl+o"))
	assert.Equal(t, FocusTranscript, at.focus)
	assert.True(t, at.transcript.Frozen())

	// ctrl+o again exits freeze.
	at, _ = at.handleKey(keyPress("ctrl+o"))
	assert.Equal(t, FocusInput, at.focus)
	assert.False(t, at.transcript.Frozen())
}

func TestHintsNoCtrlOInIdle(t *testing.T) {
	// ctrl+o freeze hint moved to StatusLine; Hints() should be empty in idle.
	at := NewAgentTab("", config.Config{}, nil)
	at.messages = append(at.messages, ChatMessage{Role: "tool", ToolName: "Read", Done: true})
	hints := at.Hints()
	assert.Empty(t, hints, "idle hints should be empty, ctrl+o is now in StatusLine")
}

func TestHintsShowFreezeControls(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.messages = append(at.messages, ChatMessage{Role: "user", Content: "hello"})
	at.focus = FocusTranscript
	at.transcript.SetFrozen(true)
	hints := at.Hints()
	require.NotEmpty(t, hints)
	assert.Equal(t, "j/k", hints[0].Key)
	assert.Equal(t, "scroll", hints[0].Desc)
	// Last hint should be quit.
	assert.Equal(t, "q", hints[len(hints)-1].Key)
	assert.Equal(t, "exit freeze", hints[len(hints)-1].Desc)
}

func TestToolOutputShownWhenExpanded(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil)
	at.width = 120
	at.toolsExpanded = true
	at.messages = append(at.messages, ChatMessage{
		Role:       "tool",
		ToolName:   "Read",
		ToolArgs:   "main.go",
		ToolStatus: "success",
		ToolBody:   "read ok",
		ToolOutput: "package main\n\nfunc main() {}",
		Done:       true,
	})
	rendered := at.renderMessages()
	assert.Contains(t, rendered, "package main", "expanded tool should show output content")
}

// keyPress builds a tea.KeyPressMsg for the given key string.
// Supports: "enter", "escape", "up", "down", "shift+enter", and plain text.
func keyPress(key string) tea.KeyPressMsg {
	switch key {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "escape", "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "shift+enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "delete":
		return tea.KeyPressMsg{Code: tea.KeyDelete}
	case "ctrl+o":
		return tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl}
	default:
		return tea.KeyPressMsg{Text: key}
	}
}
