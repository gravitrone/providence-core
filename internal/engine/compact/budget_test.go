package compact

import (
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/assert"
)

func makeToolResultMessage(toolUseID string, content string) anthropic.MessageParam {
	return anthropic.NewUserMessage(
		anthropic.NewToolResultBlock(toolUseID, content, false),
	)
}

func TestBudgetUnderLimit(t *testing.T) {
	msgs := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("hello")),
		anthropic.NewAssistantMessage(anthropic.NewTextBlock("hi")),
		makeToolResultMessage("tool1", "short result"),
	}
	result := EnforceToolResultBudget(msgs, 1000)
	assert.Len(t, result, 3)
	// Content should be unchanged.
	tr := result[2].Content[0].OfToolResult
	assert.Contains(t, tr.Content[0].OfText.Text, "short result")
}

func TestBudgetOverLimit(t *testing.T) {
	// Create 3 tool results, each 50k chars. Total = 150k, budget = 100k.
	bigContent := strings.Repeat("x", 50000)
	msgs := []anthropic.MessageParam{
		makeToolResultMessage("tool1", bigContent), // oldest
		makeToolResultMessage("tool2", bigContent),
		makeToolResultMessage("tool3", bigContent), // newest
	}
	result := EnforceToolResultBudget(msgs, 100000)
	assert.Len(t, result, 3)

	// The oldest result should be truncated.
	tr0 := result[0].Content[0].OfToolResult
	assert.Equal(t, BudgetTruncatedStub, tr0.Content[0].OfText.Text)

	// The newest should still be intact (we truncate oldest first).
	tr2 := result[2].Content[0].OfToolResult
	assert.Equal(t, bigContent, tr2.Content[0].OfText.Text)
}

func TestBudgetDefaultMaxChars(t *testing.T) {
	// Test that 0 uses default budget.
	smallContent := "hello"
	msgs := []anthropic.MessageParam{
		makeToolResultMessage("tool1", smallContent),
	}
	result := EnforceToolResultBudget(msgs, 0)
	assert.Len(t, result, 1)
	tr := result[0].Content[0].OfToolResult
	assert.Equal(t, smallContent, tr.Content[0].OfText.Text)
}

func TestBudgetTruncatesMultiple(t *testing.T) {
	// 5 results of 30k each = 150k. Budget 60k. Should truncate 3 oldest.
	bigContent := strings.Repeat("a", 30000)
	msgs := []anthropic.MessageParam{
		makeToolResultMessage("tool1", bigContent),
		makeToolResultMessage("tool2", bigContent),
		makeToolResultMessage("tool3", bigContent),
		makeToolResultMessage("tool4", bigContent),
		makeToolResultMessage("tool5", bigContent),
	}
	result := EnforceToolResultBudget(msgs, 60000)

	truncated := 0
	for _, msg := range result {
		tr := msg.Content[0].OfToolResult
		if tr != nil && tr.Content[0].OfText.Text == BudgetTruncatedStub {
			truncated++
		}
	}
	// At least 3 should be truncated (3*30k = 90k saved, bringing 150k under 60k).
	assert.GreaterOrEqual(t, truncated, 3)
}

func TestBudgetSingleHugeResult(t *testing.T) {
	// One result that is way over the budget by itself.
	huge := strings.Repeat("z", 200000)
	msgs := []anthropic.MessageParam{
		makeToolResultMessage("tool1", huge),
	}
	result := EnforceToolResultBudget(msgs, 1000)
	assert.Len(t, result, 1)

	tr := result[0].Content[0].OfToolResult
	// Should be truncated to stub since 200k > 1k budget.
	assert.Equal(t, BudgetTruncatedStub, tr.Content[0].OfText.Text)
}

func TestBudgetMixedToolAndNonToolMessages(t *testing.T) {
	// Non-tool messages should pass through unchanged even when budget is tight.
	bigContent := strings.Repeat("x", 60000)
	msgs := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("hello")),
		makeToolResultMessage("tool1", bigContent),
		anthropic.NewAssistantMessage(anthropic.NewTextBlock("reply")),
		makeToolResultMessage("tool2", bigContent),
	}
	result := EnforceToolResultBudget(msgs, 50000)
	assert.Len(t, result, 4)

	// Non-tool messages should be intact.
	assert.Equal(t, "hello", result[0].Content[0].OfText.Text)
	assert.Equal(t, "reply", result[2].Content[0].OfText.Text)

	// At least the oldest tool result should be truncated.
	tr0 := result[1].Content[0].OfToolResult
	assert.Equal(t, BudgetTruncatedStub, tr0.Content[0].OfText.Text)
}

func TestBudgetAlreadyStubsSkipped(t *testing.T) {
	// Results that are already stubs (small) should not be re-truncated.
	msgs := []anthropic.MessageParam{
		makeToolResultMessage("tool1", BudgetTruncatedStub),
		makeToolResultMessage("tool2", strings.Repeat("y", 80000)),
	}
	result := EnforceToolResultBudget(msgs, 50000)
	assert.Len(t, result, 2)

	// The already-stub message should remain unchanged.
	tr0 := result[0].Content[0].OfToolResult
	assert.Equal(t, BudgetTruncatedStub, tr0.Content[0].OfText.Text)
}
