package tree

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildTreeUserAssistant(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "fix the auth bug"},
		{Role: "assistant", Content: "I'll look into that"},
	}

	nodes := BuildTree(msgs)
	require.Len(t, nodes, 1, "single user turn = single root")
	assert.Equal(t, "user", nodes[0].Role)
	assert.Contains(t, nodes[0].Preview, "fix the auth bug")
	require.Len(t, nodes[0].Children, 1)
	assert.Equal(t, "assistant", nodes[0].Children[0].Role)
}

func TestBuildTreeWithTools(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "fix the auth bug"},
		{Role: "assistant", Content: "Let me check the file"},
		{Role: "tool", ToolName: "Read", ToolArgs: "internal/auth/auth.go", ToolOutput: "package auth\n// lots of code here"},
		{Role: "tool", ToolName: "Edit", ToolArgs: "internal/auth/auth.go", ToolOutput: "replaced validateToken"},
		{Role: "assistant", Content: "Fixed the auth bug"},
	}

	nodes := BuildTree(msgs)
	require.Len(t, nodes, 1)

	user := nodes[0]
	assert.Equal(t, "user", user.Role)
	// user -> assistant -> [Read, Edit] -> assistant
	require.GreaterOrEqual(t, len(user.Children), 1)

	// First child is the initial assistant message.
	firstAssist := user.Children[0]
	assert.Equal(t, "assistant", firstAssist.Role)

	// Tools should be children of the first assistant.
	var toolNodes []*TreeNode
	for _, ch := range firstAssist.Children {
		if ch.Role == "tool_call" {
			toolNodes = append(toolNodes, ch)
		}
	}
	assert.GreaterOrEqual(t, len(toolNodes), 2, "should have at least 2 tool calls")

	// Each tool call should have a tool_result child.
	for _, tn := range toolNodes {
		require.Len(t, tn.Children, 1, "each tool_call should have one result")
		assert.Equal(t, "tool_result", tn.Children[0].Role)
	}
}

func TestBuildTreeMultipleTurns(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "first question"},
		{Role: "assistant", Content: "first answer"},
		{Role: "user", Content: "second question"},
		{Role: "assistant", Content: "second answer"},
	}

	nodes := BuildTree(msgs)
	require.Len(t, nodes, 2, "two user turns = two roots")
	assert.Equal(t, "user", nodes[0].Role)
	assert.Equal(t, "user", nodes[1].Role)
}

func TestBuildTreeEmpty(t *testing.T) {
	nodes := BuildTree(nil)
	assert.Empty(t, nodes)
}

func TestBuildTreeSystemMessage(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "system", Content: "Model set to opus"},
		{Role: "user", Content: "hello"},
	}

	nodes := BuildTree(msgs)
	require.Len(t, nodes, 2)
	assert.Equal(t, "system", nodes[0].Role)
	assert.Equal(t, "user", nodes[1].Role)
}

func TestRenderTreeBasic(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "fix the auth bug"},
		{Role: "assistant", Content: "Looking at the code"},
		{Role: "tool", ToolName: "Read", ToolArgs: "auth.go", ToolOutput: "package auth"},
		{Role: "assistant", Content: "Fixed it"},
	}

	theme := ThemeColors{
		Primary:   "#FFA600",
		Secondary: "#D77757",
		Accent:    "#FFD700",
		Muted:     "#3D3530",
		Text:      "#e0d0c0",
	}

	nodes := BuildTree(msgs)
	output := RenderTree(nodes, 80, theme)
	assert.NotEmpty(t, output)
	assert.Contains(t, output, "USER:")
	assert.Contains(t, output, "fix the auth bug")
}

func TestRenderTreeConnectors(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "hello"},
		{Role: "tool", ToolName: "Read", ToolArgs: "file.go", ToolOutput: "contents"},
		{Role: "tool", ToolName: "Edit", ToolArgs: "file.go", ToolOutput: "done"},
		{Role: "assistant", Content: "all done"},
	}

	theme := ThemeColors{
		Primary:   "#FFA600",
		Secondary: "#D77757",
		Accent:    "#FFD700",
		Muted:     "#3D3530",
		Text:      "#e0d0c0",
	}

	nodes := BuildTree(msgs)
	output := RenderTree(nodes, 80, theme)
	assert.NotEmpty(t, output)
	// Should contain tree connectors.
	assert.Contains(t, output, "──")
}

func TestRenderTreeZeroWidth(t *testing.T) {
	nodes := []*TreeNode{{Role: "user", Preview: "test"}}
	theme := ThemeColors{Primary: "#FFA600", Muted: "#3D3530", Text: "#e0d0c0"}
	output := RenderTree(nodes, 0, theme)
	assert.NotEmpty(t, output, "zero width should use fallback")
}

func TestFormatToolCall(t *testing.T) {
	assert.Equal(t, "Read(auth.go)", formatToolCall("Read", "auth.go"))
	assert.Equal(t, "Bash()", formatToolCall("Bash", ""))
	assert.Equal(t, "(unknown tool)", formatToolCall("", ""))
}

func TestFormatToolResult(t *testing.T) {
	assert.Equal(t, "[success]", formatToolResult("", "success"))
	assert.Equal(t, "[no output]", formatToolResult("", ""))
	assert.Equal(t, "short result", formatToolResult("short result", ""))
}
