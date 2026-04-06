package tools

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	defaultReadLimit = 2000
	maxReadChars     = 100_000
)

var imageExts = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
}

// ReadTool reads text files with line numbers (cat -n format).
type ReadTool struct {
	fileState *FileState
}

// NewReadTool creates a ReadTool backed by the given FileState tracker.
func NewReadTool(fs *FileState) *ReadTool {
	return &ReadTool{fileState: fs}
}

func (r *ReadTool) Name() string        { return "Read" }
func (r *ReadTool) Description() string { return "Read a file from disk with line numbers." }
func (r *ReadTool) ReadOnly() bool      { return true }

func (r *ReadTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "Absolute path to the file to read.",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "Line number to start reading from (1-based).",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Number of lines to read (default 2000).",
			},
		},
		"required": []string{"file_path"},
	}
}

func (r *ReadTool) Execute(_ context.Context, input map[string]any) ToolResult {
	path := paramString(input, "file_path", "")
	if path == "" {
		return ToolResult{Content: "file_path is required", IsError: true}
	}

	// clean the path
	path = filepath.Clean(path)

	// check if image
	ext := strings.ToLower(filepath.Ext(path))
	if mime, ok := imageExts[ext]; ok {
		return r.readImage(path, mime)
	}

	// check if binary
	if isBinaryFile(path) {
		return ToolResult{Content: "binary file, cannot read", IsError: true}
	}

	offset := paramInt(input, "offset", 1)
	if offset < 1 {
		offset = 1
	}
	limit := paramInt(input, "limit", defaultReadLimit)
	if limit < 1 {
		limit = defaultReadLimit
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ToolResult{Content: fmt.Sprintf("file not found: %s", path), IsError: true}
		}
		return ToolResult{Content: fmt.Sprintf("cannot open file: %v", err), IsError: true}
	}
	defer f.Close()

	var b strings.Builder
	scanner := bufio.NewScanner(f)
	// increase buffer for long lines
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	lineNum := 0
	linesEmitted := 0
	totalChars := 0

	for scanner.Scan() {
		lineNum++
		if lineNum < offset {
			continue
		}
		if linesEmitted >= limit {
			break
		}

		line := fmt.Sprintf("%6d\t%s\n", lineNum, scanner.Text())
		if totalChars+len(line) > maxReadChars {
			b.WriteString(fmt.Sprintf("\n... truncated at %d chars\n", maxReadChars))
			break
		}
		b.WriteString(line)
		totalChars += len(line)
		linesEmitted++
	}

	if err := scanner.Err(); err != nil {
		return ToolResult{Content: fmt.Sprintf("read error: %v", err), IsError: true}
	}

	r.fileState.MarkRead(path)
	return ToolResult{Content: b.String()}
}

func (r *ReadTool) readImage(path, mime string) ToolResult {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ToolResult{Content: fmt.Sprintf("file not found: %s", path), IsError: true}
		}
		return ToolResult{Content: fmt.Sprintf("cannot read image: %v", err), IsError: true}
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	r.fileState.MarkRead(path)
	return ToolResult{
		Content: fmt.Sprintf("[image: %s (%d bytes)]", filepath.Base(path), len(data)),
		Metadata: map[string]any{
			"base64":    encoded,
			"mime_type": mime,
		},
	}
}

// isBinaryFile checks if a file looks like binary by reading the first 8KB.
func isBinaryFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, 8192)
	n, err := f.Read(buf)
	if n == 0 {
		return false
	}
	buf = buf[:n]

	// check for null bytes (strong binary indicator)
	for _, b := range buf {
		if b == 0 {
			return true
		}
	}
	// check if valid UTF-8
	return !utf8.Valid(buf)
}
