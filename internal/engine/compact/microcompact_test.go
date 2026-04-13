package compact

import (
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helper: build a user message with a single tool result block.
func toolResultMsg(id string, content string, isError bool) anthropic.MessageParam {
	return anthropic.NewUserMessage(
		anthropic.NewToolResultBlock(id, content, isError),
	)
}

// helper: build a plain user text message.
func userMsg(text string) anthropic.MessageParam {
	return anthropic.NewUserMessage(anthropic.NewTextBlock(text))
}

// helper: build a plain assistant text message.
func assistantMsg(text string) anthropic.MessageParam {
	return anthropic.NewAssistantMessage(anthropic.NewTextBlock(text))
}

func TestMicrocompact_BelowKeepRecent(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", 5000)
	msgs := []anthropic.MessageParam{
		userMsg("hello"),
		toolResultMsg("t1", long, false),
		toolResultMsg("t2", long, false),
		assistantMsg("ok"),
	}

	out, saved := Microcompact(msgs)
	assert.Equal(t, 0, saved)
	// Messages should be unchanged - only 2 tool results, both under KeepRecent.
	require.Len(t, out, 4)
	assert.Equal(t, long, out[1].Content[0].OfToolResult.Content[0].OfText.Text)
	assert.Equal(t, long, out[2].Content[0].OfToolResult.Content[0].OfText.Text)
}

func TestMicrocompact_PrunesOldKeepsRecent(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", 3000)
	short := strings.Repeat("y", 100)

	// 7 tool results: indices 0-6. KeepRecent=5 means indices 0,1 are prunable.
	msgs := []anthropic.MessageParam{
		toolResultMsg("old-long-1", long, false),  // idx 0 - prunable, over threshold
		toolResultMsg("old-short", short, false),   // idx 1 - prunable, under threshold
		toolResultMsg("keep-1", long, false),       // idx 2 - kept (recent 5)
		toolResultMsg("keep-2", long, false),       // idx 3 - kept
		toolResultMsg("keep-3", long, false),       // idx 4 - kept
		toolResultMsg("keep-4", long, false),       // idx 5 - kept
		toolResultMsg("keep-5", long, false),       // idx 6 - kept
	}

	out, saved := Microcompact(msgs)

	// old-long-1 should be cleared.
	assert.Equal(t, ToolResultCleared, out[0].Content[0].OfToolResult.Content[0].OfText.Text)

	// old-short should be untouched (under threshold).
	assert.Equal(t, short, out[1].Content[0].OfToolResult.Content[0].OfText.Text)

	// Recent 5 should all be untouched.
	for i := 2; i <= 6; i++ {
		assert.Equal(t, long, out[i].Content[0].OfToolResult.Content[0].OfText.Text)
	}

	// Tokens saved: (3000 - len(ToolResultCleared)) / 4
	expectedChars := 3000 - len(ToolResultCleared)
	assert.Equal(t, expectedChars/4, saved)
}

func TestMicrocompact_ExactlyKeepRecent(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("z", 5000)
	msgs := make([]anthropic.MessageParam, KeepRecent)
	for i := 0; i < KeepRecent; i++ {
		msgs[i] = toolResultMsg("t"+string(rune('a'+i)), long, false)
	}

	out, saved := Microcompact(msgs)
	assert.Equal(t, 0, saved)
	require.Len(t, out, KeepRecent)
	for i := range out {
		assert.Equal(t, long, out[i].Content[0].OfToolResult.Content[0].OfText.Text)
	}
}

func TestMicrocompact_EmptyMessages(t *testing.T) {
	t.Parallel()

	out, saved := Microcompact(nil)
	assert.Nil(t, out)
	assert.Equal(t, 0, saved)
}

func TestMicrocompact_MixedMessages(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("w", 4000)

	// Mix of user, assistant, and tool result messages.
	msgs := []anthropic.MessageParam{
		userMsg("start"),
		assistantMsg("thinking..."),
		toolResultMsg("old-1", long, false), // prunable
		userMsg("more input"),
		toolResultMsg("old-2", long, false), // prunable
		assistantMsg("still thinking"),
		toolResultMsg("keep-1", long, false),
		toolResultMsg("keep-2", long, false),
		toolResultMsg("keep-3", long, false),
		toolResultMsg("keep-4", long, false),
		toolResultMsg("keep-5", long, false),
		assistantMsg("done"),
	}

	out, saved := Microcompact(msgs)

	// old-1 and old-2 should be cleared.
	assert.Equal(t, ToolResultCleared, out[2].Content[0].OfToolResult.Content[0].OfText.Text)
	assert.Equal(t, ToolResultCleared, out[4].Content[0].OfToolResult.Content[0].OfText.Text)

	// Recent 5 untouched.
	for i := 6; i <= 10; i++ {
		assert.Equal(t, long, out[i].Content[0].OfToolResult.Content[0].OfText.Text)
	}

	// User/assistant messages untouched.
	assert.Equal(t, "start", out[0].Content[0].OfText.Text)
	assert.Equal(t, "thinking...", out[1].Content[0].OfText.Text)
	assert.Equal(t, "done", out[11].Content[0].OfText.Text)

	expectedChars := (4000 - len(ToolResultCleared)) * 2
	assert.Equal(t, expectedChars/4, saved)
}

func TestMicrocompact_MultiBlockToolResult(t *testing.T) {
	t.Parallel()

	// A user message with multiple tool result blocks in one message.
	longContent := strings.Repeat("m", 3000)
	msgs := []anthropic.MessageParam{
		// One message with 2 tool result blocks.
		anthropic.NewUserMessage(
			anthropic.NewToolResultBlock("multi-1", longContent, false),
			anthropic.NewToolResultBlock("multi-2", longContent, false),
		),
		// 5 more individual tool results to fill the keep window.
		toolResultMsg("keep-1", longContent, false),
		toolResultMsg("keep-2", longContent, false),
		toolResultMsg("keep-3", longContent, false),
		toolResultMsg("keep-4", longContent, false),
		toolResultMsg("keep-5", longContent, false),
	}

	// Total tool results: 7 (2 in first msg + 5 individual).
	// Prunable: first 2 (indices 0,1 in loc list).
	out, saved := Microcompact(msgs)

	// Both blocks in the first message should be cleared.
	assert.Equal(t, ToolResultCleared, out[0].Content[0].OfToolResult.Content[0].OfText.Text)
	assert.Equal(t, ToolResultCleared, out[0].Content[1].OfToolResult.Content[0].OfText.Text)

	// Recent 5 untouched.
	for i := 1; i <= 5; i++ {
		assert.Equal(t, longContent, out[i].Content[0].OfToolResult.Content[0].OfText.Text)
	}

	expectedChars := (3000 - len(ToolResultCleared)) * 2
	assert.Equal(t, expectedChars/4, saved)
}
