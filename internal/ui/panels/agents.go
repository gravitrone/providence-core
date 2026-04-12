package panels

import (
	"fmt"
	"strings"
)

// AgentInfo represents an active subagent goroutine.
type AgentInfo struct {
	Name    string
	Status  string // active, idle, offline
	Elapsed string // "12s", "4m"
}

// RenderAgents shows active subagent goroutines.
func RenderAgents(agents []AgentInfo, width int) string {
	if len(agents) == 0 {
		return "  No active agents"
	}
	var lines []string
	for _, a := range agents {
		status := "*"
		if a.Status == "idle" {
			status = "o"
		}
		lines = append(lines, fmt.Sprintf("  %s %-18s %-8s %s", status, a.Name, a.Status, a.Elapsed))
	}
	return strings.Join(lines, "\n")
}
