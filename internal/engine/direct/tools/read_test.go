package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newReadTool() (*ReadTool, *FileState) {
	fs := NewFileState()
	return NewReadTool(fs), fs
}

func TestReadBasicFile(t *testing.T) {
	rt, fs := newReadTool()
	tmp := t.TempDir()
	p := filepath.Join(tmp, "hello.txt")
	require.NoError(t, os.WriteFile(p, []byte("line1\nline2\nline3\n"), 0644))

	res := rt.Execute(context.Background(), map[string]any{"file_path": p})
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content, "line1")
	assert.Contains(t, res.Content, "line2")
	assert.Contains(t, res.Content, "line3")
	assert.True(t, fs.HasBeenRead(p))
}

func TestReadWithOffset(t *testing.T) {
	rt, _ := newReadTool()
	tmp := t.TempDir()
	p := filepath.Join(tmp, "nums.txt")
	var lines []string
	for i := 1; i <= 10; i++ {
		lines = append(lines, strings.Repeat("x", 1))
	}
	require.NoError(t, os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0644))

	res := rt.Execute(context.Background(), map[string]any{
		"file_path": p,
		"offset":    float64(5),
		"limit":     float64(2),
	})
	assert.False(t, res.IsError)
	// should have line numbers 5 and 6
	assert.Contains(t, res.Content, "5\t")
	assert.Contains(t, res.Content, "6\t")
	assert.NotContains(t, res.Content, "7\t")
}

func TestReadFileNotFound(t *testing.T) {
	rt, _ := newReadTool()
	res := rt.Execute(context.Background(), map[string]any{"file_path": "/nonexistent/path.txt"})
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "file not found")
}

func TestReadMissingParam(t *testing.T) {
	rt, _ := newReadTool()
	res := rt.Execute(context.Background(), map[string]any{})
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "file_path is required")
}

func TestReadBinaryFile(t *testing.T) {
	rt, _ := newReadTool()
	tmp := t.TempDir()
	p := filepath.Join(tmp, "binary.dat")
	data := []byte{0x00, 0x01, 0x02, 0xFF, 0x00}
	require.NoError(t, os.WriteFile(p, data, 0644))

	res := rt.Execute(context.Background(), map[string]any{"file_path": p})
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "binary file")
}

func TestReadImage(t *testing.T) {
	rt, fs := newReadTool()
	tmp := t.TempDir()
	p := filepath.Join(tmp, "icon.png")
	// minimal fake PNG data (not valid, but extension-based detection)
	require.NoError(t, os.WriteFile(p, []byte("fakepng"), 0644))

	res := rt.Execute(context.Background(), map[string]any{"file_path": p})
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content, "[image:")
	assert.NotNil(t, res.Metadata)
	assert.Equal(t, "image/png", res.Metadata["mime_type"])
	assert.NotEmpty(t, res.Metadata["base64"])
	assert.True(t, fs.HasBeenRead(p))
}

func TestReadEmptyFile(t *testing.T) {
	rt, _ := newReadTool()
	tmp := t.TempDir()
	p := filepath.Join(tmp, "empty.txt")
	require.NoError(t, os.WriteFile(p, []byte{}, 0644))

	res := rt.Execute(context.Background(), map[string]any{"file_path": p})
	assert.False(t, res.IsError)
	assert.Equal(t, "", res.Content)
}

func TestReadTruncatesLargeFile(t *testing.T) {
	rt, _ := newReadTool()
	tmp := t.TempDir()
	p := filepath.Join(tmp, "large.txt")
	// create a file with lines that will exceed maxReadChars
	line := strings.Repeat("A", 1000) + "\n"
	content := strings.Repeat(line, 200) // 200K chars
	require.NoError(t, os.WriteFile(p, []byte(content), 0644))

	res := rt.Execute(context.Background(), map[string]any{"file_path": p})
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content, "truncated")
	assert.LessOrEqual(t, len(res.Content), maxReadChars+200) // some slack for truncation msg
}
