package direct

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompressCodexToolResults(t *testing.T) {
	longResult := strings.Repeat("x", 5000)
	shortResult := strings.Repeat("y", 300)

	items := []codexHistoryEntry{
		{Role: "user", Content: "user-0"},
		{Role: "tool", Content: longResult, CallID: "call_old_long"},
		{Role: "tool", Content: shortResult, CallID: "call_old_short"},
		{Role: "assistant", Content: "assistant-3"},
		{Role: "tool", Content: longResult, CallID: "call_recent_long"},
		{Role: "user", Content: "user-5"},
		{Role: "assistant", Content: "assistant-6"},
	}

	compressed := compressCodexToolResults(items, 2000)
	require.Equal(t, 1, compressed)

	assert.Equal(t, "[compressed: 5000 chars from call_id=call_old_long]", items[1].Content)
	assert.Equal(t, shortResult, items[2].Content)
	assert.Equal(t, longResult, items[4].Content)
}
