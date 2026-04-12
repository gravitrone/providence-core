package tree

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// ThemeColors holds the color palette for tree rendering.
type ThemeColors struct {
	Primary    string
	Secondary  string
	Accent     string
	Muted      string
	Text       string
	Background string
	Error      string
}

// ChatMessage is the minimal message interface for tree building.
type ChatMessage struct {
	Role       string // user, assistant, tool, thinking, system, permission
	Content    string
	ToolName   string
	ToolArgs   string
	ToolOutput string
	ToolStatus string
	Done       bool
}

// TreeNode represents a message in the conversation tree.
type TreeNode struct {
	Role     string // user, assistant, tool_call, tool_result
	Preview  string // first line or tool name
	Children []*TreeNode
	Depth    int
	Expanded bool
}

// BuildTree converts a flat message list into a tree structure.
// Pattern: user -> assistant -> [tool_call -> tool_result, ...] -> assistant
func BuildTree(messages []ChatMessage) []*TreeNode {
	var roots []*TreeNode
	var current *TreeNode

	for i := 0; i < len(messages); i++ {
		msg := messages[i]

		switch msg.Role {
		case "user":
			node := &TreeNode{
				Role:     "user",
				Preview:  firstLine(msg.Content, 80),
				Depth:    0,
				Expanded: true,
			}
			roots = append(roots, node)
			current = node

		case "assistant":
			node := &TreeNode{
				Role:     "assistant",
				Preview:  firstLine(msg.Content, 80),
				Depth:    1,
				Expanded: true,
			}
			if current != nil {
				current.Children = append(current.Children, node)
				// If this assistant has tool calls following, they attach as children.
				// Otherwise keep current as the user node for subsequent tools.
				current = node
			} else {
				roots = append(roots, node)
				current = node
			}

		case "tool":
			toolNode := &TreeNode{
				Role:     "tool_call",
				Preview:  formatToolCall(msg.ToolName, msg.ToolArgs),
				Depth:    2,
				Expanded: true,
			}
			resultNode := &TreeNode{
				Role:    "tool_result",
				Preview: formatToolResult(msg.ToolOutput, msg.ToolStatus),
				Depth:   3,
			}
			toolNode.Children = append(toolNode.Children, resultNode)

			if current != nil {
				current.Children = append(current.Children, toolNode)
			} else {
				roots = append(roots, toolNode)
			}

		case "thinking":
			node := &TreeNode{
				Role:    "thinking",
				Preview: firstLine(msg.Content, 60),
				Depth:   1,
			}
			if current != nil {
				current.Children = append(current.Children, node)
			}

		case "system":
			node := &TreeNode{
				Role:    "system",
				Preview: firstLine(msg.Content, 60),
				Depth:   0,
			}
			roots = append(roots, node)
		}
	}

	return roots
}

// RenderTree renders the tree with ASCII connectors in flame colors.
func RenderTree(nodes []*TreeNode, width int, theme ThemeColors) string {
	if len(nodes) == 0 {
		return ""
	}
	if width <= 0 {
		width = 80
	}

	var b strings.Builder
	for i, node := range nodes {
		isLast := i == len(nodes)-1
		renderNode(&b, node, "", isLast, width, theme)
	}
	return b.String()
}

func renderNode(b *strings.Builder, node *TreeNode, prefix string, isLast bool, width int, theme ThemeColors) {
	connector := "├── "
	if isLast {
		connector = "└── "
	}

	connectorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Muted))
	roleStyle := roleStyleFor(node.Role, theme)

	// Root nodes (depth 0) have no connector prefix.
	if node.Depth == 0 {
		label := roleLabel(node.Role)
		maxPreview := width - lipgloss.Width(label) - 2
		preview := clampStr(node.Preview, maxPreview)
		b.WriteString(roleStyle.Render(label) + " " + preview + "\n")
	} else {
		label := roleLabel(node.Role)
		styledConnector := connectorStyle.Render(prefix + connector)
		maxPreview := width - lipgloss.Width(prefix+connector+label) - 2
		preview := clampStr(node.Preview, maxPreview)
		b.WriteString(styledConnector + roleStyle.Render(label) + " " + preview + "\n")
	}

	// Render children.
	childPrefix := prefix
	if node.Depth > 0 {
		if isLast {
			childPrefix = prefix + "    "
		} else {
			childPrefix = prefix + "│   "
		}
	}

	for i, child := range node.Children {
		childIsLast := i == len(node.Children)-1
		renderNode(b, child, childPrefix, childIsLast, width, theme)
	}
}

func roleLabel(role string) string {
	switch role {
	case "user":
		return "USER:"
	case "assistant":
		return "ASSISTANT:"
	case "tool_call":
		return ""
	case "tool_result":
		return ""
	case "thinking":
		return "THINKING:"
	case "system":
		return "SYSTEM:"
	default:
		return strings.ToUpper(role) + ":"
	}
}

func roleStyleFor(role string, theme ThemeColors) lipgloss.Style {
	switch role {
	case "user":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Primary)).Bold(true)
	case "assistant":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Secondary)).Bold(true)
	case "tool_call":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Accent))
	case "tool_result":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Muted))
	case "thinking":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Muted)).Italic(true)
	case "system":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Muted))
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Text))
	}
}

func formatToolCall(name, args string) string {
	if name == "" {
		return "(unknown tool)"
	}
	if args == "" {
		return name + "()"
	}
	// Show tool name with first arg, truncated.
	arg := firstLine(args, 60)
	return fmt.Sprintf("%s(%s)", name, arg)
}

func formatToolResult(output, status string) string {
	if output == "" {
		if status != "" {
			return "[" + status + "]"
		}
		return "[no output]"
	}
	line := firstLine(output, 60)
	if len(output) > len(line) {
		return fmt.Sprintf("[%d chars]", len(output))
	}
	return line
}

func firstLine(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = s[:idx]
	}
	return clampStr(s, maxLen)
}

func clampStr(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}
