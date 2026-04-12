package panels

import (
	"fmt"
	"strings"
)

// PendingApproval represents a permission request waiting for user action.
type PendingApproval struct {
	ToolName string
	Args     string
	Age      string // "30s", "1m"
}

// RenderApprovals shows pending permission requests.
func RenderApprovals(pending []PendingApproval, width int) string {
	if len(pending) == 0 {
		return "  No pending approvals"
	}
	var lines []string
	for _, p := range pending {
		lines = append(lines, fmt.Sprintf("  ! %-20s %s", p.ToolName, truncate(p.Args, width-25)))
	}
	return strings.Join(lines, "\n")
}
