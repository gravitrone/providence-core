package components

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatQuoteBlock_BasicFormat(t *testing.T) {
	msg := QuoteMessage{
		Role:    "assistant",
		Content: "Fixed the auth bug by replacing the validateToken function",
		Time:    time.Now().Add(-2 * time.Minute),
	}

	block := FormatQuoteBlock(msg)

	// Should have the header line.
	assert.Contains(t, block, "> [quoting assistant message from")
	// Should contain the content.
	assert.Contains(t, block, "Fixed the auth bug")
	// All content lines should start with >.
	for _, line := range strings.Split(strings.TrimSpace(block), "\n") {
		assert.True(t, strings.HasPrefix(line, ">"), "line should start with >: %q", line)
	}
}

func TestFormatQuoteBlock_Truncation(t *testing.T) {
	long := strings.Repeat("word ", 100) // 500 chars
	msg := QuoteMessage{
		Role:    "assistant",
		Content: long,
		Time:    time.Now(),
	}

	block := FormatQuoteBlock(msg)

	// The quoted content should be truncated.
	assert.Contains(t, block, `..."`)
	// Total content in the quote line should be under maxQuoteLen + some overhead.
	lines := strings.Split(block, "\n")
	for _, line := range lines {
		if strings.Contains(line, "word") {
			// Content line length should be reasonable.
			assert.Less(t, len(line), maxQuoteLen+50, "content line should be truncated")
		}
	}
}

func TestFormatQuoteBlock_TimeAgo(t *testing.T) {
	tests := []struct {
		name     string
		offset   time.Duration
		contains string
	}{
		{"just now", 10 * time.Second, "just now"},
		{"1 minute", 90 * time.Second, "1m ago"},
		{"5 minutes", 5 * time.Minute, "5m ago"},
		{"1 hour", 90 * time.Minute, "1h ago"},
		{"3 hours", 3 * time.Hour, "3h ago"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := QuoteMessage{
				Role:    "user",
				Content: "test",
				Time:    time.Now().Add(-tt.offset),
			}
			block := FormatQuoteBlock(msg)
			assert.Contains(t, block, tt.contains)
		})
	}
}

func TestFormatQuoteBlock_ZeroTime(t *testing.T) {
	msg := QuoteMessage{
		Role:    "assistant",
		Content: "test",
		Time:    time.Time{},
	}
	block := FormatQuoteBlock(msg)
	assert.Contains(t, block, "just now")
}

func TestQuoteModel_Navigation(t *testing.T) {
	q := NewQuoteModel()
	msgs := []QuoteMessage{
		{Role: "user", Content: "first", Time: time.Now()},
		{Role: "assistant", Content: "second", Time: time.Now()},
		{Role: "user", Content: "third", Time: time.Now()},
	}

	q.Enter(msgs)
	require.True(t, q.Active())
	assert.Equal(t, 2, q.Cursor(), "should start at last message")

	// Navigate up.
	quoted, _ := q.HandleKey("up")
	assert.False(t, quoted)
	assert.Equal(t, 1, q.Cursor())

	quoted, _ = q.HandleKey("k")
	assert.False(t, quoted)
	assert.Equal(t, 0, q.Cursor())

	// At top, stays at 0.
	quoted, _ = q.HandleKey("up")
	assert.False(t, quoted)
	assert.Equal(t, 0, q.Cursor())

	// Navigate down.
	quoted, _ = q.HandleKey("down")
	assert.False(t, quoted)
	assert.Equal(t, 1, q.Cursor())

	quoted, _ = q.HandleKey("j")
	assert.False(t, quoted)
	assert.Equal(t, 2, q.Cursor())

	// At bottom, stays.
	quoted, _ = q.HandleKey("down")
	assert.False(t, quoted)
	assert.Equal(t, 2, q.Cursor())
}

func TestQuoteModel_Accept(t *testing.T) {
	q := NewQuoteModel()
	msgs := []QuoteMessage{
		{Role: "user", Content: "hello", Time: time.Now()},
		{Role: "assistant", Content: "world", Time: time.Now()},
	}

	q.Enter(msgs)
	q.HandleKey("up") // select "hello"

	quoted, block := q.HandleKey("enter")
	require.True(t, quoted)
	assert.Contains(t, block, "hello")
	assert.Contains(t, block, "quoting user")
	assert.False(t, q.Active(), "should exit after accept")
}

func TestQuoteModel_Dismiss(t *testing.T) {
	q := NewQuoteModel()
	msgs := []QuoteMessage{
		{Role: "assistant", Content: "test", Time: time.Now()},
	}

	q.Enter(msgs)
	require.True(t, q.Active())

	quoted, _ := q.HandleKey("esc")
	assert.False(t, quoted)
	assert.False(t, q.Active())
}

func TestQuoteModel_EnterEmptyMessages(t *testing.T) {
	q := NewQuoteModel()
	q.Enter(nil)
	assert.False(t, q.Active(), "should not activate with no messages")
}

func TestQuoteModel_HandleKeyWhenInactive(t *testing.T) {
	q := NewQuoteModel()
	quoted, _ := q.HandleKey("enter")
	assert.False(t, quoted, "should not quote when inactive")
}
