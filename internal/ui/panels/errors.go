package panels

import (
	"fmt"
	"strings"
)

// ErrorInfo represents a recent tool error.
type ErrorInfo struct {
	Tool    string
	Message string
	Age     string
}

// RenderErrors shows recent tool errors.
func RenderErrors(errors []ErrorInfo, width int) string {
	if len(errors) == 0 {
		return "  No errors"
	}
	var lines []string
	for _, e := range errors {
		lines = append(lines, fmt.Sprintf("  %s: %s  %s", e.Tool, truncate(e.Message, width-20), e.Age))
	}
	return strings.Join(lines, "\n")
}
