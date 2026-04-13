package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAskUserName(t *testing.T) {
	tool := NewAskUserQuestionTool(nil)
	assert.Equal(t, "AskUserQuestion", tool.Name())
}

func TestAskUserSchema(t *testing.T) {
	tool := NewAskUserQuestionTool(nil)
	schema := tool.InputSchema()
	props := schema["properties"].(map[string]any)
	assert.Contains(t, props, "questions")
}

func TestAskUserNotReadOnly(t *testing.T) {
	tool := NewAskUserQuestionTool(nil)
	assert.False(t, tool.ReadOnly())
}
