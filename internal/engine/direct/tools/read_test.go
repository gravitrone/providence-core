package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gravitrone/providence-core/internal/engine/hooks"
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

func TestReadStripsBOMAndRemembersIt(t *testing.T) {
	rt, _ := newReadTool()
	dir := t.TempDir()
	path := filepath.Join(dir, "bom.txt")
	require.NoError(t, os.WriteFile(path, append([]byte{0xEF, 0xBB, 0xBF}, []byte("flame\n")...), 0o644))

	res := rt.Execute(context.Background(), map[string]any{"file_path": path})
	require.False(t, res.IsError, res.Content)
	assert.Contains(t, res.Content, "1\tflame")
	assert.NotContains(t, res.Content, "\uFEFF")

	encoding, ok := lookupFileEncoding(path)
	require.True(t, ok)
	assert.True(t, encoding.BOM)
	assert.Equal(t, LineEndingLF, encoding.LineEnding)
	assert.Equal(t, CharsetUTF8, encoding.Charset)
}

func TestReadDetectsCRLFAndRemembers(t *testing.T) {
	rt, _ := newReadTool()
	dir := t.TempDir()
	path := filepath.Join(dir, "windows.txt")
	require.NoError(t, os.WriteFile(path, []byte("ember\r\nash\r\n"), 0o644))

	res := rt.Execute(context.Background(), map[string]any{"file_path": path})
	require.False(t, res.IsError, res.Content)
	assert.Contains(t, res.Content, "1\tember")
	assert.Contains(t, res.Content, "2\tash")

	encoding, ok := lookupFileEncoding(path)
	require.True(t, ok)
	assert.False(t, encoding.BOM)
	assert.Equal(t, LineEndingCRLF, encoding.LineEnding)
	assert.Equal(t, CharsetUTF8, encoding.Charset)
}

func TestReadHandlesLatin1File(t *testing.T) {
	rt, _ := newReadTool()
	dir := t.TempDir()
	path := filepath.Join(dir, "latin1.txt")
	require.NoError(t, os.WriteFile(path, []byte{'c', 'a', 'f', 0xE9, '\n'}, 0o644))

	res := rt.Execute(context.Background(), map[string]any{"file_path": path})
	require.False(t, res.IsError, res.Content)
	assert.Contains(t, res.Content, "café")

	encoding, ok := lookupFileEncoding(path)
	require.True(t, ok)
	assert.Equal(t, CharsetLatin1, encoding.Charset)
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
	original := writePNGImage(t, p, 64, 64)

	res := rt.Execute(context.Background(), map[string]any{"file_path": p})
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content, "[image:")
	assert.NotNil(t, res.Metadata)
	assert.Equal(t, "image/png", res.Metadata["mime_type"])
	assert.NotEmpty(t, res.Metadata["base64"])
	assert.Equal(t, original, decodeImageResultBytes(t, res))
	assert.True(t, fs.HasBeenRead(p))
}

func TestReadImageResizesWhenOverBudget(t *testing.T) {
	rt, _ := newReadTool()
	tmp := t.TempDir()
	p := filepath.Join(tmp, "large.png")
	writePNGImage(t, p, 2000, 2000)

	res := rt.Execute(context.Background(), map[string]any{"file_path": p})
	require.False(t, res.IsError, res.Content)

	width, height := decodeImageDimensions(t, decodeImageResultBytes(t, res))
	assert.LessOrEqual(t, width*height, imageMaxPixels)
	assert.Equal(t, width, height)
}

func TestReadImagePassesThroughWhenUnderBudget(t *testing.T) {
	rt, _ := newReadTool()
	tmp := t.TempDir()
	p := filepath.Join(tmp, "small.png")
	original := writePNGImage(t, p, 500, 500)

	res := rt.Execute(context.Background(), map[string]any{"file_path": p})
	require.False(t, res.IsError, res.Content)

	assert.Equal(t, original, decodeImageResultBytes(t, res))
}

func TestReadImageUnsupportedFormatPassesThrough(t *testing.T) {
	rt, _ := newReadTool()
	tmp := t.TempDir()
	p := filepath.Join(tmp, "vector.svg")
	original := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="8" height="8"></svg>`)
	require.NoError(t, os.WriteFile(p, original, 0o644))

	res := rt.Execute(context.Background(), map[string]any{"file_path": p})
	require.False(t, res.IsError, res.Content)

	assert.Equal(t, "image/svg+xml", res.Metadata["mime_type"])
	assert.Equal(t, original, decodeImageResultBytes(t, res))
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

func TestRead_FiresFileReadHook(t *testing.T) {
	rt, _ := newReadTool()
	spy := &hookSpy{}
	rt.SetHookEmitter(spy.record)

	dir := t.TempDir()
	path := filepath.Join(dir, "hooked.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello\n"), 0o644))

	res := rt.Execute(context.Background(), map[string]any{"file_path": path})
	require.False(t, res.IsError, res.Content)

	events, inputs := spy.snapshot()
	require.Equal(t, []string{hooks.FileRead}, events)
	require.Len(t, inputs, 1)
	assert.Equal(t, "Read", inputs[0].ToolName)
	assert.Equal(t, map[string]string{"file_path": path}, inputs[0].ToolInput)
}

func TestReadToolHasLargerCap(t *testing.T) {
	rt, _ := newReadTool()

	provider, ok := any(rt).(ResultCapProvider)
	require.True(t, ok)
	assert.Equal(t, readToolResultSizeCap, provider.ResultSizeCap())
}

func writePNGImage(t *testing.T, path string, width, height int) []byte {
	t.Helper()

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o644))
	return buf.Bytes()
}

func decodeImageResultBytes(t *testing.T, res ToolResult) []byte {
	t.Helper()

	encoded, ok := res.Metadata["base64"].(string)
	require.True(t, ok)

	data, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	return data
}

func decodeImageDimensions(t *testing.T, data []byte) (int, int) {
	t.Helper()

	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	require.NoError(t, err)
	return cfg.Width, cfg.Height
}
