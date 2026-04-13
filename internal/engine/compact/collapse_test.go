package compact

import (
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// collapseToolResult builds a tool result user message for collapse tests.
func collapseToolResult(toolUseID string) anthropic.MessageParam {
	return anthropic.NewUserMessage(
		anthropic.NewToolResultBlock(toolUseID, "result for "+toolUseID, false),
	)
}

func TestContextCollapse_EmptyMessages(t *testing.T) {
	msgs, collapsed := ContextCollapse(nil)
	assert.Empty(t, msgs)
	assert.Equal(t, 0, collapsed)

	msgs, collapsed = ContextCollapse([]anthropic.MessageParam{})
	assert.Empty(t, msgs)
	assert.Equal(t, 0, collapsed)
}

func TestContextCollapse_UnderThresholdNotCollapsed(t *testing.T) {
	// CollapseKeepRecent*2 = 16. With fewer messages, nothing should be collapsed.
	msgs := make([]anthropic.MessageParam, 0, 10)
	for i := 0; i < 5; i++ {
		msgs = append(msgs, anthropic.NewUserMessage(anthropic.NewTextBlock("user msg")))
		msgs = append(msgs, anthropic.NewAssistantMessage(anthropic.NewTextBlock("assistant msg")))
	}

	result, collapsed := ContextCollapse(msgs)
	assert.Equal(t, len(msgs), len(result))
	assert.Equal(t, 0, collapsed)
}

func TestContextCollapse_OldToolRunsCollapsed(t *testing.T) {
	// ContextCollapse collapses runs of *consecutive user messages* that contain
	// only tool results. We need at least CollapseMinToolResults (3) consecutive
	// tool-result-only user messages in the old region.
	var msgs []anthropic.MessageParam

	// Old region: a block of consecutive tool result user messages.
	// The function scans user messages - assistant messages break the run.
	// So we need 4+ consecutive user messages with only tool results.
	for i := 0; i < 5; i++ {
		msgs = append(msgs, collapseToolResult("tool-"+string(rune('a'+i))))
	}

	// Recent region: enough user/assistant pairs to fill CollapseKeepRecent*2.
	for i := 0; i < CollapseKeepRecent; i++ {
		msgs = append(msgs, anthropic.NewUserMessage(anthropic.NewTextBlock("recent user")))
		msgs = append(msgs, anthropic.NewAssistantMessage(anthropic.NewTextBlock("recent assistant")))
	}

	result, collapsed := ContextCollapse(msgs)

	// Old tool results should be collapsed - fewer messages than before.
	assert.Less(t, len(result), len(msgs))
	assert.Greater(t, collapsed, 0)

	// Check that a stub message was inserted.
	foundStub := false
	for _, m := range result {
		for _, block := range m.Content {
			if block.OfText != nil && strings.HasPrefix(block.OfText.Text, CollapseStubPrefix) {
				foundStub = true
			}
		}
	}
	assert.True(t, foundStub, "should contain a collapse stub message")
}

func TestContextCollapse_RecentMessagesPreserved(t *testing.T) {
	var msgs []anthropic.MessageParam

	// Old tool results to be collapsed (consecutive user messages with only tool results).
	for i := 0; i < 6; i++ {
		msgs = append(msgs, collapseToolResult("old-tool-"+string(rune('a'+i))))
	}

	// Recent messages that must survive.
	recentMarker := "RECENT_PRESERVED_MARKER"
	for i := 0; i < CollapseKeepRecent; i++ {
		msgs = append(msgs, anthropic.NewUserMessage(anthropic.NewTextBlock(recentMarker)))
		msgs = append(msgs, anthropic.NewAssistantMessage(anthropic.NewTextBlock("recent reply")))
	}

	result, _ := ContextCollapse(msgs)

	// All recent markers should still be present.
	markerCount := 0
	for _, m := range result {
		for _, block := range m.Content {
			if block.OfText != nil && block.OfText.Text == recentMarker {
				markerCount++
			}
		}
	}
	assert.Equal(t, CollapseKeepRecent, markerCount, "all recent messages must be preserved")
}

func TestContextCollapse_MixedMessageTypes(t *testing.T) {
	var msgs []anthropic.MessageParam

	// Non-tool user + assistant messages in old region.
	msgs = append(msgs, anthropic.NewUserMessage(anthropic.NewTextBlock("hello")))
	msgs = append(msgs, anthropic.NewAssistantMessage(anthropic.NewTextBlock("hi")))

	// Run of 4 consecutive tool-result-only user messages (will be collapsed).
	for i := 0; i < 4; i++ {
		msgs = append(msgs, collapseToolResult("mixed-tool-"+string(rune('a'+i))))
	}

	// More regular messages after the tool run.
	msgs = append(msgs, anthropic.NewUserMessage(anthropic.NewTextBlock("another user msg")))
	msgs = append(msgs, anthropic.NewAssistantMessage(anthropic.NewTextBlock("another reply")))

	// Recent padding.
	for i := 0; i < CollapseKeepRecent; i++ {
		msgs = append(msgs, anthropic.NewUserMessage(anthropic.NewTextBlock("pad user")))
		msgs = append(msgs, anthropic.NewAssistantMessage(anthropic.NewTextBlock("pad assistant")))
	}

	result, collapsed := ContextCollapse(msgs)

	// The tool run of 4 consecutive user messages should be collapsed.
	assert.Greater(t, collapsed, 0)

	// Non-tool user message "hello" should survive.
	foundHello := false
	for _, m := range result {
		for _, block := range m.Content {
			if block.OfText != nil && block.OfText.Text == "hello" {
				foundHello = true
			}
		}
	}
	assert.True(t, foundHello, "non-tool user messages should not be collapsed")
}

func TestContextCollapse_TooFewToolResultsNotCollapsed(t *testing.T) {
	var msgs []anthropic.MessageParam

	// Only 2 consecutive tool-result user messages - below CollapseMinToolResults (3).
	msgs = append(msgs, collapseToolResult("tool-a"))
	msgs = append(msgs, collapseToolResult("tool-b"))
	msgs = append(msgs, anthropic.NewUserMessage(anthropic.NewTextBlock("break")))
	msgs = append(msgs, anthropic.NewAssistantMessage(anthropic.NewTextBlock("assistant")))

	// Recent padding.
	for i := 0; i < CollapseKeepRecent; i++ {
		msgs = append(msgs, anthropic.NewUserMessage(anthropic.NewTextBlock("pad")))
		msgs = append(msgs, anthropic.NewAssistantMessage(anthropic.NewTextBlock("pad")))
	}

	result, collapsed := ContextCollapse(msgs)
	assert.Equal(t, 0, collapsed, "groups smaller than CollapseMinToolResults should not be collapsed")
	assert.Equal(t, len(msgs), len(result))
}

func TestContextCollapse_StubContainsToolCount(t *testing.T) {
	var msgs []anthropic.MessageParam

	// 4 consecutive tool-result-only user messages, each with 1 tool result block.
	for i := 0; i < 4; i++ {
		msgs = append(msgs, collapseToolResult("stub-tool-"+string(rune('a'+i))))
	}

	// Recent padding.
	for i := 0; i < CollapseKeepRecent; i++ {
		msgs = append(msgs, anthropic.NewUserMessage(anthropic.NewTextBlock("pad")))
		msgs = append(msgs, anthropic.NewAssistantMessage(anthropic.NewTextBlock("pad")))
	}

	result, collapsed := ContextCollapse(msgs)
	require.Greater(t, collapsed, 0)

	// Find the stub and verify it contains the count.
	for _, m := range result {
		for _, block := range m.Content {
			if block.OfText != nil && strings.HasPrefix(block.OfText.Text, CollapseStubPrefix) {
				assert.Contains(t, block.OfText.Text, "4 tool results")
				return
			}
		}
	}
	t.Fatal("stub message not found")
}
