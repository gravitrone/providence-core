package panels

import (
	"fmt"
	"strings"
)

// FileInfo represents a file touched in the current session.
type FileInfo struct {
	Path     string
	Modified bool
	ReadOnly bool
}

// RenderFiles shows files touched in current session.
func RenderFiles(files []FileInfo, width int) string {
	if len(files) == 0 {
		return "  No files touched"
	}
	var lines []string
	for _, f := range files {
		marker := " "
		if f.Modified {
			marker = "*"
		}
		if f.ReadOnly {
			marker = "o"
		}
		lines = append(lines, fmt.Sprintf("  %s %s", marker, truncate(f.Path, width-5)))
	}
	return strings.Join(lines, "\n")
}
