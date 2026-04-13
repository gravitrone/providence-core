package compact

import "github.com/anthropics/anthropic-sdk-go"

// DefaultToolResultBudget is the maximum total characters allowed across all
// tool result content blocks. If exceeded, oldest results are truncated first.
const DefaultToolResultBudget = 100_000

// BudgetTruncatedStub is the replacement text for truncated tool results.
const BudgetTruncatedStub = "[Tool result truncated - over context budget]"

// EnforceToolResultBudget caps total tool result content to maxChars.
// When over budget, it truncates the oldest tool results first.
// A maxChars of 0 uses DefaultToolResultBudget.
func EnforceToolResultBudget(messages []anthropic.MessageParam, maxChars int) []anthropic.MessageParam {
	if maxChars <= 0 {
		maxChars = DefaultToolResultBudget
	}

	// First pass: calculate total tool result size.
	total := 0
	type loc struct {
		msg   int
		block int
		size  int
	}
	var results []loc

	for i := range messages {
		for j := range messages[i].Content {
			tr := messages[i].Content[j].OfToolResult
			if tr == nil {
				continue
			}
			size := 0
			for _, inner := range tr.Content {
				if inner.OfText != nil {
					size += len(inner.OfText.Text)
				}
			}
			total += size
			results = append(results, loc{msg: i, block: j, size: size})
		}
	}

	if total <= maxChars {
		return messages
	}

	// Over budget. Truncate oldest results first until we're under budget.
	excess := total - maxChars

	for _, l := range results {
		if excess <= 0 {
			break
		}

		tr := messages[l.msg].Content[l.block].OfToolResult
		if tr == nil {
			continue
		}

		// Skip results that are already stubs.
		if l.size <= len(BudgetTruncatedStub) {
			continue
		}

		saved := l.size - len(BudgetTruncatedStub)
		excess -= saved

		tr.Content = []anthropic.ToolResultBlockParamContentUnion{{
			OfText: &anthropic.TextBlockParam{
				Text: BudgetTruncatedStub,
			},
		}}
	}

	return messages
}
