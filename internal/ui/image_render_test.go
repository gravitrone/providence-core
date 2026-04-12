package ui

import (
	"image"
	"image/color"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectImageProtocol_Default(t *testing.T) {
	// Clear relevant env vars to get the default.
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("LC_TERMINAL", "")
	t.Setenv("KITTY_WINDOW_ID", "")

	proto := DetectImageProtocol()
	assert.Equal(t, ImageProtocolHalfblock, proto)
}

func TestDetectImageProtocol_ITerm2(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"TERM_PROGRAM iTerm2", map[string]string{"TERM_PROGRAM": "iTerm2"}},
		{"TERM_PROGRAM WezTerm", map[string]string{"TERM_PROGRAM": "WezTerm"}},
		{"TERM_PROGRAM vscode", map[string]string{"TERM_PROGRAM": "vscode"}},
		{"LC_TERMINAL iTerm2", map[string]string{"TERM_PROGRAM": "", "LC_TERMINAL": "iTerm2"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear all first.
			t.Setenv("TERM_PROGRAM", "")
			t.Setenv("LC_TERMINAL", "")
			t.Setenv("KITTY_WINDOW_ID", "")

			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			proto := DetectImageProtocol()
			assert.Equal(t, ImageProtocolITerm2, proto)
		})
	}
}

func TestDetectImageProtocol_Kitty(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"TERM_PROGRAM kitty", map[string]string{"TERM_PROGRAM": "kitty"}},
		{"KITTY_WINDOW_ID set", map[string]string{"TERM_PROGRAM": "", "KITTY_WINDOW_ID": "42"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TERM_PROGRAM", "")
			t.Setenv("LC_TERMINAL", "")
			t.Setenv("KITTY_WINDOW_ID", "")

			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			proto := DetectImageProtocol()
			assert.Equal(t, ImageProtocolKitty, proto)
		})
	}
}

func TestRenderImageHalfblock_ContainsBlockChars(t *testing.T) {
	// Create a small 4x4 test image with known colors.
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	red := color.RGBA{R: 255, A: 255}
	blue := color.RGBA{B: 255, A: 255}
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, red)
		}
	}
	for y := 2; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, blue)
		}
	}

	result := RenderImage(img, 4, 2, ImageProtocolHalfblock)

	// Must contain the halfblock character.
	assert.Contains(t, result, "▀")
	// Must contain ANSI escape codes for true color.
	assert.Contains(t, result, "\x1b[38;2;")
	assert.Contains(t, result, "\x1b[48;2;")
	// Must contain reset codes.
	assert.Contains(t, result, "\x1b[0m")
}

func TestRenderImageHalfblock_CorrectDimensions(t *testing.T) {
	// 10x10 image rendered into 5 cols x 3 rows (6 pixel rows).
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img.Set(x, y, color.White)
		}
	}

	result := RenderImage(img, 5, 3, ImageProtocolHalfblock)

	lines := strings.Split(result, "\n")
	// 10px high scaled to fit 6px (3 rows * 2) preserving aspect -> 5x5 pixels -> 3 cell rows (ceil(5/2)).
	require.GreaterOrEqual(t, len(lines), 1, "should produce at least 1 line")
	// Each line should have block characters (no empty lines within the image).
	for _, line := range lines {
		assert.Contains(t, line, "▀", "every line should contain halfblock chars")
	}
}

func TestRenderImageHalfblock_NilImage(t *testing.T) {
	result := RenderImage(nil, 10, 10, ImageProtocolHalfblock)
	assert.Empty(t, result)
}

func TestRenderImageHalfblock_ZeroDimensions(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	assert.Empty(t, RenderImage(img, 0, 10, ImageProtocolHalfblock))
	assert.Empty(t, RenderImage(img, 10, 0, ImageProtocolHalfblock))
}

func TestDecodeImageFile_NotFound(t *testing.T) {
	_, err := DecodeImageFile("/nonexistent/path.png")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open image")
}

func TestDecodeImageFile_InvalidFormat(t *testing.T) {
	// Create a temp file with non-image content.
	tmp, err := os.CreateTemp(t.TempDir(), "*.png")
	require.NoError(t, err)
	_, _ = tmp.WriteString("not an image")
	_ = tmp.Close()

	_, err = DecodeImageFile(tmp.Name())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode image")
}
