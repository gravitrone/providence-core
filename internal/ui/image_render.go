package ui

import (
	"fmt"
	"image"
	"os"
	"strings"

	// Register decoders for common image formats.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

// ImageProtocol is the detected terminal image protocol.
type ImageProtocol int

const (
	// ImageProtocolHalfblock uses unicode ▀ with fg/bg true color (universal fallback).
	ImageProtocolHalfblock ImageProtocol = iota
	// ImageProtocolKitty uses the kitty graphics protocol (kitty, Ghostty).
	ImageProtocolKitty
	// ImageProtocolITerm2 uses the iTerm2 inline image protocol (iTerm2, WezTerm, VSCode).
	ImageProtocolITerm2
	// ImageProtocolSixel uses the sixel graphics protocol (xterm, foot).
	ImageProtocolSixel
)

// DetectImageProtocol probes the terminal for image capability via env vars.
// For v1 this does simple env var detection. Kitty/sixel probes deferred.
func DetectImageProtocol() ImageProtocol {
	tp := os.Getenv("TERM_PROGRAM")
	switch strings.ToLower(tp) {
	case "iterm.app", "iterm2", "wezterm", "vscode":
		return ImageProtocolITerm2
	case "kitty":
		return ImageProtocolKitty
	}

	// LC_TERMINAL is set by some iTerm2-compatible terminals.
	if strings.EqualFold(os.Getenv("LC_TERMINAL"), "iterm2") {
		return ImageProtocolITerm2
	}

	// KITTY_WINDOW_ID is set inside kitty.
	if os.Getenv("KITTY_WINDOW_ID") != "" {
		return ImageProtocolKitty
	}

	return ImageProtocolHalfblock
}

// RenderImage renders an image inline in the terminal using the given protocol.
// maxCols and maxRows define the cell budget. For v1 only halfblock is implemented.
func RenderImage(img image.Image, maxCols, maxRows int, protocol ImageProtocol) string {
	if img == nil || maxCols <= 0 || maxRows <= 0 {
		return ""
	}

	// For v1: all protocols fall back to halfblock.
	return renderHalfblock(img, maxCols, maxRows)
}

// renderHalfblock renders an image using the unicode upper-half block (▀) technique.
// Each terminal cell represents 2 vertical pixels: top pixel = foreground, bottom pixel = background.
func renderHalfblock(img image.Image, maxCols, maxRows int) string {
	bounds := img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()
	if srcW == 0 || srcH == 0 {
		return ""
	}

	// Target pixel dimensions: maxCols wide, maxRows*2 tall (2 pixels per cell row).
	targetW := maxCols
	targetH := maxRows * 2

	// Preserve aspect ratio.
	scaleX := float64(targetW) / float64(srcW)
	scaleY := float64(targetH) / float64(srcH)
	scale := scaleX
	if scaleY < scale {
		scale = scaleY
	}

	dstW := int(float64(srcW) * scale)
	dstH := int(float64(srcH) * scale)
	if dstW <= 0 {
		dstW = 1
	}
	if dstH <= 0 {
		dstH = 1
	}

	// Nearest-neighbor sample from source image.
	// samplePixel returns the RGBA at the given destination coordinate,
	// mapping back to the source image.
	samplePixel := func(dx, dy int) (uint8, uint8, uint8) {
		sx := bounds.Min.X + dx*srcW/dstW
		sy := bounds.Min.Y + dy*srcH/dstH
		// Clamp to bounds.
		if sx >= bounds.Max.X {
			sx = bounds.Max.X - 1
		}
		if sy >= bounds.Max.Y {
			sy = bounds.Max.Y - 1
		}
		r, g, b, _ := img.At(sx, sy).RGBA()
		// Shift 32-bit RGBA to 8-bit. The top 16 bits are the pre-multiplied value,
		// so >>8 is safe: max value is 0xFFFF, shifted to 0xFF.
		return uint8(r >> 8), uint8(g >> 8), uint8(b >> 8) //nolint:gosec // safe: RGBA() returns [0,0xFFFF], >>8 fits uint8
	}

	// Render pairs of rows. Each pair produces one line of terminal output.
	var sb strings.Builder
	for row := 0; row < dstH; row += 2 {
		for col := 0; col < dstW; col++ {
			// Top pixel -> foreground color, bottom pixel -> background color.
			tr, tg, tb := samplePixel(col, row)
			if row+1 < dstH {
				br, bg, bb := samplePixel(col, row+1)
				fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀\x1b[0m", tr, tg, tb, br, bg, bb)
			} else {
				// Odd height: last row has no bottom pixel, use black background.
				fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%dm▀\x1b[0m", tr, tg, tb)
			}
		}
		if row+2 < dstH {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// DecodeImageFile reads and decodes an image from a file path.
// Supports PNG, JPEG, and GIF via the standard library decoders registered at import.
func DecodeImageFile(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open image: %w", err)
	}
	defer func() { _ = f.Close() }()

	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}
	return img, nil
}

