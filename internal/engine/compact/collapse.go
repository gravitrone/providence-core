package compact

import (
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// CollapseStubPrefix is prepended to collapsed tool group summaries.
const CollapseStubPrefix = "[Collapsed tool group: "

// CollapseMinToolResults is the minimum number of consecutive tool results
// that must exist in an older region before they are eligible for collapsing.
const CollapseMinToolResults = 3

// CollapseKeepRecent is the number of most-recent message pairs to preserve
// from collapsing. Only older regions are collapsed.
const CollapseKeepRecent = 8

// ContextCollapse performs a pre-compaction pass that summarizes groups of
// old tool-result turns into 1-line stubs inline. This is cheaper than full
// compaction (no API call) and reduces token count enough that full compaction
// may be avoided entirely.
//
// Returns the modified messages and the number of tool result blocks collapsed.
func ContextCollapse(messages []anthropic.MessageParam) ([]anthropic.MessageParam, int) {
	if len(messages) <= CollapseKeepRecent*2 {
		return messages, 0
	}

	// Only collapse in the older region - leave recent messages untouched.
	boundary := len(messages) - CollapseKeepRecent*2

	collapsed := 0
	i := 0
	for i < boundary {
		// Find runs of consecutive user messages that contain only tool results
		// (these follow assistant messages with tool_use blocks).
		groupStart := -1
		groupEnd := -1
		toolNames := []string{}

		for j := i; j < boundary; j++ {
			msg := messages[j]
			if msg.Role != "user" {
				if groupStart >= 0 {
					break
				}
				continue
			}

			// Check if this user message is purely tool results.
			allToolResults := true
			hasToolResult := false
			for _, block := range msg.Content {
				if block.OfToolResult != nil {
					hasToolResult = true
					toolNames = append(toolNames, block.OfToolResult.ToolUseID)
				} else if block.OfText != nil {
					allToolResults = false
				}
			}

			if hasToolResult && allToolResults {
				if groupStart < 0 {
					groupStart = j
				}
				groupEnd = j + 1
			} else {
				if groupStart >= 0 {
					break
				}
			}
		}

		// Collapse the group if it has enough consecutive tool result messages.
		if groupStart >= 0 && (groupEnd-groupStart) >= CollapseMinToolResults {
			// Count total tool results in this group.
			totalResults := 0
			for k := groupStart; k < groupEnd; k++ {
				for _, block := range messages[k].Content {
					if block.OfToolResult != nil {
						totalResults++
					}
				}
			}

			// Build a single collapsed stub message.
			stub := fmt.Sprintf("%s%d tool results collapsed]", CollapseStubPrefix, totalResults)
			if len(toolNames) > 0 {
				preview := toolNames
				if len(preview) > 3 {
					preview = preview[:3]
				}
				stub = fmt.Sprintf("%s%d tool results for %s...]", CollapseStubPrefix, totalResults, strings.Join(preview, ", "))
			}

			// Replace the group with a single user message containing the stub.
			stubMsg := anthropic.NewUserMessage(anthropic.NewTextBlock(stub))

			newMessages := make([]anthropic.MessageParam, 0, len(messages)-(groupEnd-groupStart)+1)
			newMessages = append(newMessages, messages[:groupStart]...)
			newMessages = append(newMessages, stubMsg)
			newMessages = append(newMessages, messages[groupEnd:]...)

			collapsed += totalResults
			// Adjust boundary since we removed messages.
			removed := (groupEnd - groupStart) - 1
			boundary -= removed
			messages = newMessages
			i = groupStart + 1
		} else {
			if groupStart >= 0 {
				i = groupEnd
			} else {
				i++
			}
		}
	}

	return messages, collapsed
}
