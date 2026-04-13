package compact

import "github.com/anthropics/anthropic-sdk-go"

// --- Microcompact ---

// ToolResultCleared is the stub text that replaces pruned tool results.
const ToolResultCleared = "[Old tool result content cleared]"

// KeepRecent is the number of most-recent tool results left untouched.
const KeepRecent = 5

// CompressThreshold is the minimum character count before a tool result is
// eligible for pruning.
const CompressThreshold = 2000

// Microcompact prunes old tool results from conversation history.
// It walks all messages, finds tool_result content blocks, keeps the most
// recent KeepRecent untouched, and replaces older ones that exceed
// CompressThreshold chars with ToolResultCleared.
// Returns the modified messages and an estimate of tokens saved (chars/4).
// Zero API cost - runs locally before each API call.
func Microcompact(messages []anthropic.MessageParam) ([]anthropic.MessageParam, int) {
	// Collect indices of all tool result blocks: (message index, block index).
	type loc struct {
		msg   int
		block int
	}

	var locs []loc
	for i := range messages {
		for j := range messages[i].Content {
			if messages[i].Content[j].OfToolResult != nil {
				locs = append(locs, loc{msg: i, block: j})
			}
		}
	}

	// Nothing to prune if we have fewer tool results than the keep window.
	if len(locs) <= KeepRecent {
		return messages, 0
	}

	// Only prune the older ones (everything before the last KeepRecent).
	prunable := locs[:len(locs)-KeepRecent]

	charsSaved := 0
	for _, l := range prunable {
		tr := messages[l.msg].Content[l.block].OfToolResult
		if tr == nil {
			continue
		}

		// Sum content length.
		totalLen := 0
		for _, inner := range tr.Content {
			if inner.OfText != nil {
				totalLen += len(inner.OfText.Text)
			}
		}

		if totalLen <= CompressThreshold {
			continue
		}

		charsSaved += totalLen - len(ToolResultCleared)
		tr.Content = []anthropic.ToolResultBlockParamContentUnion{{
			OfText: &anthropic.TextBlockParam{
				Text: ToolResultCleared,
			},
		}}
	}

	tokensSaved := charsSaved / 4
	return messages, tokensSaved
}
