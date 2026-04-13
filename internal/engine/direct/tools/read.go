package tools

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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

	cacheMu   sync.Mutex
	readCache map[string]string // path -> content sha256 hex
}

// NewReadTool creates a ReadTool backed by the given FileState tracker.
func NewReadTool(fs *FileState) *ReadTool {
	return &ReadTool{
		fileState: fs,
		readCache: make(map[string]string),
	}
}

func (r *ReadTool) Name() string        { return "Read" }
func (r *ReadTool) Description() string { return "Read a file from disk with line numbers." }
func (r *ReadTool) ReadOnly() bool      { return true }

// Prompt implements ToolPrompter with CC-parity guidance for file reading.
func (r *ReadTool) Prompt() string {
	return `Reads a file from the local filesystem. You can access any file directly by using this tool.
Assume this tool is able to read all files on the machine. If the User provides a path to a file assume that path is valid. It is okay to read a file that does not exist; an error will be returned.

Usage:
- The file_path parameter must be an absolute path, not a relative path
- By default, it reads up to 2000 lines starting from the beginning of the file
- When you already know which part of the file you need, only read that part. This can be important for larger files.
- Results are returned using cat -n format, with line numbers starting at 1
- This tool can read images (eg PNG, JPG, etc). When reading an image file the contents are presented visually as Providence is a multimodal LLM.
- This tool can read PDF files (.pdf). For large PDFs (more than 10 pages), you MUST provide the pages parameter to read specific page ranges (e.g., pages: "1-5"). Reading a large PDF without the pages parameter will fail. Maximum 20 pages per request.
- This tool can read Jupyter notebooks (.ipynb files) and returns all cells with their outputs, combining code, text, and visualizations.
- This tool can only read files, not directories. To read a directory, use an ls command via the Bash tool.
- You will regularly be asked to read screenshots. If the user provides a path to a screenshot, ALWAYS use this tool to view the file at the path. This tool will work with all temporary file paths.
- If you read a file that exists but has empty contents you will receive a system reminder warning in place of file contents.
- Do NOT re-read a file you just edited to verify - Edit/Write would have errored if the change failed, and the harness tracks file state for you.`
}

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

func (r *ReadTool) Execute(ctx context.Context, input map[string]any) ToolResult {
	path := paramString(input, "file_path", "")
	if path == "" {
		return ToolResult{Content: "file_path is required", IsError: true}
	}

	// Clean the path.
	path = filepath.Clean(path)

	// Block device file access.
	if strings.HasPrefix(path, "/dev/") {
		return ToolResult{Content: "Reading device files is not supported.", IsError: true}
	}

	// Clean extension for routing.
	ext := strings.ToLower(filepath.Ext(path))

	// Check if image.
	if mime, ok := imageExts[ext]; ok {
		return r.readImage(path, mime)
	}

	// PDF files
	offset := paramInt(input, "offset", 1)
	if offset < 1 {
		offset = 1
	}
	limit := paramInt(input, "limit", defaultReadLimit)
	if limit < 1 {
		limit = defaultReadLimit
	}

	if ext == ".pdf" {
		return r.readPDF(ctx, path, offset, limit)
	}

	// Jupyter notebooks
	if ext == ".ipynb" {
		return r.readNotebook(path)
	}

	// Check if binary.
	if isBinaryFile(path) {
		return ToolResult{Content: "binary file, cannot read", IsError: true}
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
	// Increase buffer for long lines.
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

	content := b.String()

	// File-unchanged-since-last-read detection.
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
	r.cacheMu.Lock()
	if cached, ok := r.readCache[path]; ok && cached == hash {
		r.cacheMu.Unlock()
		r.fileState.MarkRead(path)
		return ToolResult{Content: "[file unchanged since last read]"}
	}
	r.readCache[path] = hash
	r.cacheMu.Unlock()

	r.fileState.MarkRead(path)
	return ToolResult{Content: content}
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

// readPDF extracts text from a PDF using pdftotext (poppler).
func (r *ReadTool) readPDF(ctx context.Context, path string, offset, limit int) ToolResult {
	out, err := exec.CommandContext(ctx, "pdftotext", "-layout", path, "-").Output()
	if err != nil {
		return ToolResult{
			Content: "PDF reading requires pdftotext (brew install poppler)",
			IsError: true,
		}
	}
	text := string(out)
	lines := strings.Split(text, "\n")

	// apply offset (1-based) and limit
	start := offset - 1
	if start < 0 {
		start = 0
	}
	if start > len(lines) {
		start = len(lines)
	}
	end := start + limit
	if end > len(lines) {
		end = len(lines)
	}
	lines = lines[start:end]

	var b strings.Builder
	for i, line := range lines {
		b.WriteString(fmt.Sprintf("%6d\t%s\n", start+i+1, line))
	}

	r.fileState.MarkRead(path)
	return ToolResult{Content: b.String()}
}

// readNotebook reads a Jupyter .ipynb file and renders cells as text.
func (r *ReadTool) readNotebook(path string) ToolResult {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ToolResult{Content: fmt.Sprintf("file not found: %s", path), IsError: true}
		}
		return ToolResult{Content: fmt.Sprintf("cannot read notebook: %v", err), IsError: true}
	}

	var nb struct {
		Cells []struct {
			CellType string   `json:"cell_type"`
			Source   []string `json:"source"`
			Outputs  []struct {
				Text []string `json:"text"`
			} `json:"outputs"`
		} `json:"cells"`
	}
	if err := json.Unmarshal(data, &nb); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid notebook format: %v", err), IsError: true}
	}

	var sb strings.Builder
	for i, cell := range nb.Cells {
		sb.WriteString(fmt.Sprintf("--- Cell %d (%s) ---\n", i+1, cell.CellType))
		sb.WriteString(strings.Join(cell.Source, ""))
		sb.WriteString("\n")
		for _, out := range cell.Outputs {
			if len(out.Text) > 0 {
				sb.WriteString("Output:\n")
				sb.WriteString(strings.Join(out.Text, ""))
				sb.WriteString("\n")
			}
		}
	}

	r.fileState.MarkRead(path)
	return ToolResult{Content: sb.String()}
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

	// Check for null bytes (strong binary indicator).
	for _, b := range buf {
		if b == 0 {
			return true
		}
	}
	// Check if valid UTF-8.
	return !utf8.Valid(buf)
}
