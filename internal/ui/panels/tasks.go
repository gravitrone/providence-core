package panels

import (
	"fmt"
	"strings"
)

// TaskInfo represents a TodoWrite item.
type TaskInfo struct {
	Content  string
	Status   string
	Priority int
	Depth    int
}

// RenderTasks shows TodoWrite items.
func RenderTasks(todos []TaskInfo, width int) string {
	if len(todos) == 0 {
		return "  No tasks"
	}
	var lines []string
	for _, t := range todos {
		icon := "o" // pending
		switch t.Status {
		case "in_progress":
			icon = "*"
		case "completed":
			icon = "v"
		case "failed":
			icon = "x"
		}
		prefix := "  "
		if t.Depth > 0 {
			prefix = "    "
		}
		lines = append(lines, fmt.Sprintf("%s%s %s", prefix, icon, truncate(t.Content, width-6)))
	}
	return strings.Join(lines, "\n")
}
