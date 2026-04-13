package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubRender returns a render function that produces N lines per message.
// Each line is "msg-{index}-line-{lineNum}" so height = linesPerMsg.
func stubRender(linesPerMsg int) func(int) string {
	return func(idx int) string {
		lines := make([]string, linesPerMsg)
		for i := range lines {
			lines[i] = fmt.Sprintf("msg-%d-line-%d", idx, i)
		}
		return strings.Join(lines, "\n")
	}
}

func TestTranscriptAddMessage(t *testing.T) {
	tm := NewTranscriptModel()
	tm.SetViewport(80, 20)

	msg := ChatMessage{Role: "user", Content: "hello"}
	tm.AddMessage(msg)

	require.Equal(t, 1, tm.MessageCount())
	assert.Equal(t, "user", tm.Messages()[0].Role)

	// Render to populate height cache.
	render := stubRender(3)
	_ = tm.View(render)

	// Height should be cached after View.
	assert.Equal(t, 3, tm.heightCache[0], "height should be cached after render")
}

func TestTranscriptScrollBy(t *testing.T) {
	tm := NewTranscriptModel()
	tm.SetViewport(80, 5)

	// Add 10 messages, each 3 lines = 30 total lines.
	render := stubRender(3)
	for i := 0; i < 10; i++ {
		tm.AddMessage(ChatMessage{Role: "user", Content: fmt.Sprintf("msg %d", i)})
	}
	_ = tm.View(render)

	// Initially sticky, scrolled to bottom.
	assert.True(t, tm.Sticky(), "should start sticky")

	// Scroll up clears sticky.
	tm.ScrollBy(-5)
	assert.False(t, tm.Sticky(), "scrolling up should clear sticky")

	oldTop := tm.scrollTop
	tm.ScrollBy(2)
	assert.Equal(t, oldTop+2, tm.scrollTop, "scroll down should increase scrollTop")
}

func TestTranscriptScrollToBottom(t *testing.T) {
	tm := NewTranscriptModel()
	tm.SetViewport(80, 5)

	render := stubRender(3)
	for i := 0; i < 10; i++ {
		tm.AddMessage(ChatMessage{Role: "user", Content: fmt.Sprintf("msg %d", i)})
	}
	_ = tm.View(render)

	// Scroll up to un-stick.
	tm.ScrollBy(-10)
	assert.False(t, tm.Sticky())

	// ScrollToBottom re-pins.
	tm.ScrollToBottom()
	assert.True(t, tm.Sticky(), "ScrollToBottom should re-pin sticky")
}

func TestTranscriptInvalidateOnResize(t *testing.T) {
	tm := NewTranscriptModel()
	tm.SetViewport(80, 20)

	render := stubRender(3)
	tm.AddMessage(ChatMessage{Role: "user", Content: "hello"})
	_ = tm.View(render)

	// Cache should be populated.
	assert.NotEmpty(t, tm.renderedCache, "cache should have entries")

	// Change width - should invalidate all.
	tm.SetViewport(100, 20)
	assert.Empty(t, tm.renderedCache, "cache should be cleared on width change")
	assert.Empty(t, tm.heightCache, "height cache should be cleared on width change")
}

func TestTranscriptInvalidateOnSameWidth(t *testing.T) {
	tm := NewTranscriptModel()
	tm.SetViewport(80, 20)

	render := stubRender(3)
	tm.AddMessage(ChatMessage{Role: "user", Content: "hello"})
	_ = tm.View(render)

	// Same width, different height - should NOT invalidate.
	tm.SetViewport(80, 30)
	assert.NotEmpty(t, tm.renderedCache, "cache should survive height-only change")
}

func TestTranscriptFreezeMode(t *testing.T) {
	tm := NewTranscriptModel()
	tm.SetViewport(80, 20)

	assert.False(t, tm.Frozen(), "should not start frozen")

	tm.SetFrozen(true)
	assert.True(t, tm.Frozen(), "should be frozen after SetFrozen(true)")

	tm.SetFrozen(false)
	assert.False(t, tm.Frozen(), "should be unfrozen after SetFrozen(false)")
	assert.True(t, tm.Sticky(), "exiting freeze should re-pin sticky")
}

func TestTranscriptViewOnlyRendersVisible(t *testing.T) {
	tm := NewTranscriptModel()
	// Viewport: 10 rows high.
	tm.SetViewport(80, 10)

	// Each message is 3 lines. 100 messages = 300 lines total.
	// With viewport of 10, roughly ceil(10/3) = 4 messages should be visible.
	for i := 0; i < 100; i++ {
		tm.AddMessage(ChatMessage{Role: "user", Content: fmt.Sprintf("message %d", i)})
	}

	render := stubRender(3)
	output := tm.View(render)

	// The output should contain only a handful of messages, not all 100.
	visible := tm.VisibleCount(render)
	assert.LessOrEqual(t, visible, 10, "at most ~viewport/lineHeight messages should be visible")
	assert.Greater(t, visible, 0, "at least one message should be visible")

	// Output should not contain early messages (we're sticky = at bottom).
	assert.NotContains(t, output, "msg-0-line-0", "first message should not be in output when scrolled to bottom")
	// Should contain one of the last few messages.
	assert.Contains(t, output, "msg-99-line-0", "last message should be visible")
}

func TestTranscriptSearch(t *testing.T) {
	tm := NewTranscriptModel()
	tm.SetViewport(80, 20)

	tm.AddMessage(ChatMessage{Role: "user", Content: "hello world"})
	tm.AddMessage(ChatMessage{Role: "assistant", Content: "goodbye world"})
	tm.AddMessage(ChatMessage{Role: "user", Content: "hello again"})

	tm.SetFrozen(true)
	tm.SetSearchActive(true)
	tm.SetSearchQuery("hello")

	assert.Equal(t, 2, tm.SearchHitCount(), "should find 2 matches for 'hello'")

	// Navigate hits.
	tm.SearchNext()
	assert.Equal(t, 1, tm.SearchCurrentIdx())

	tm.SearchPrev()
	assert.Equal(t, 0, tm.SearchCurrentIdx())
}

func TestTranscriptSearchCaseInsensitive(t *testing.T) {
	tm := NewTranscriptModel()
	tm.SetViewport(80, 20)

	tm.AddMessage(ChatMessage{Role: "user", Content: "Hello World"})
	tm.SetSearchQuery("hello")

	assert.Equal(t, 1, tm.SearchHitCount(), "search should be case-insensitive")
}

func TestTranscriptUpdateMessage(t *testing.T) {
	tm := NewTranscriptModel()
	tm.SetViewport(80, 20)

	render := stubRender(3)
	tm.AddMessage(ChatMessage{Role: "user", Content: "original"})
	_ = tm.View(render)

	require.Contains(t, tm.renderedCache, 0, "should have cached render")

	// Update message - cache should be invalidated.
	tm.UpdateMessage(0, ChatMessage{Role: "user", Content: "updated"})
	assert.NotContains(t, tm.renderedCache, 0, "cache should be cleared after update")
}

func TestTranscriptScrollClamp(t *testing.T) {
	tm := NewTranscriptModel()
	tm.SetViewport(80, 10)

	render := stubRender(3)
	for i := 0; i < 5; i++ {
		tm.AddMessage(ChatMessage{Role: "user", Content: fmt.Sprintf("msg %d", i)})
	}
	_ = tm.View(render)

	// Scroll way past the top.
	tm.ScrollBy(-1000)
	assert.GreaterOrEqual(t, tm.scrollTop, 0, "scrollTop should not go negative")

	// Scroll way past the bottom.
	tm.ScrollBy(10000)
	maxScroll := tm.contentHeight - tm.viewportH
	if maxScroll < 0 {
		maxScroll = 0
	}
	assert.LessOrEqual(t, tm.scrollTop, maxScroll, "scrollTop should not exceed max")
}

func TestFreezeKeyJ(t *testing.T) {
	tm := NewTranscriptModel()
	tm.SetViewport(80, 5)

	render := stubRender(3)
	for i := 0; i < 10; i++ {
		tm.AddMessage(ChatMessage{Role: "user", Content: fmt.Sprintf("msg %d", i)})
	}
	_ = tm.View(render)

	tm.SetFrozen(true)
	// Scroll up first so we have room to scroll down.
	tm.ScrollBy(-10)
	before := tm.scrollTop

	// j = scroll down by 1.
	tm.ScrollBy(1)
	assert.Equal(t, before+1, tm.scrollTop, "j (ScrollBy +1) should scroll down")
}

func TestFreezeKeyK(t *testing.T) {
	tm := NewTranscriptModel()
	tm.SetViewport(80, 5)

	render := stubRender(3)
	for i := 0; i < 10; i++ {
		tm.AddMessage(ChatMessage{Role: "user", Content: fmt.Sprintf("msg %d", i)})
	}
	_ = tm.View(render)

	tm.SetFrozen(true)
	// Start at bottom, scroll up a bit, then test k.
	tm.ScrollBy(-5)
	before := tm.scrollTop
	tm.ScrollBy(-1)
	assert.Equal(t, before-1, tm.scrollTop, "k (ScrollBy -1) should scroll up")
}

func TestSearchFindsMatch(t *testing.T) {
	tm := NewTranscriptModel()
	tm.SetViewport(80, 20)

	tm.AddMessage(ChatMessage{Role: "user", Content: "the quick brown fox"})
	tm.AddMessage(ChatMessage{Role: "assistant", Content: "lazy dog"})
	tm.AddMessage(ChatMessage{Role: "user", Content: "quick silver"})

	tm.SetFrozen(true)
	tm.SetSearchActive(true)
	tm.SetSearchQuery("quick")

	assert.Equal(t, 2, tm.SearchHitCount(), "should find 2 matches for 'quick'")
}

func TestSearchNoMatch(t *testing.T) {
	tm := NewTranscriptModel()
	tm.SetViewport(80, 20)

	tm.AddMessage(ChatMessage{Role: "user", Content: "hello world"})
	tm.AddMessage(ChatMessage{Role: "assistant", Content: "goodbye world"})

	tm.SetFrozen(true)
	tm.SetSearchActive(true)
	tm.SetSearchQuery("zzzznotfound")

	assert.Equal(t, 0, tm.SearchHitCount(), "should find 0 matches for nonexistent query")
}

func TestCountLines(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"one line", 1},
		{"line1\nline2", 2},
		{"a\nb\nc\n", 4}, // trailing newline counts as an extra empty line
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, countLines(tt.input), "countLines(%q)", tt.input)
	}
}
