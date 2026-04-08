package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ImageAttachment represents an image file attached to a message.
type ImageAttachment struct {
	Path      string // original file path
	Name      string // display name (filename only)
	MediaType string // "image/png", "image/jpeg", etc
	Data      []byte // raw image bytes
	Size      int64  // file size
}

// validImageExtensions maps file extensions to MIME media types.
var validImageExtensions = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
}

// maxImageSize is the maximum allowed image size (5MB).
const maxImageSize = 5 * 1024 * 1024

// loadImageAttachment reads an image file from disk, validates it, and returns an ImageAttachment.
func loadImageAttachment(path string) (ImageAttachment, error) {
	// Expand ~ to home directory.
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, path[2:])
		}
	}

	// Resolve to absolute path.
	absPath, err := filepath.Abs(path)
	if err != nil {
		return ImageAttachment{}, fmt.Errorf("invalid path: %w", err)
	}

	// Check extension.
	ext := strings.ToLower(filepath.Ext(absPath))
	mediaType, ok := validImageExtensions[ext]
	if !ok {
		return ImageAttachment{}, fmt.Errorf("unsupported image format: %s (supported: png, jpg, jpeg, gif, webp)", ext)
	}

	// Stat the file.
	info, err := os.Stat(absPath)
	if err != nil {
		return ImageAttachment{}, fmt.Errorf("file not found: %s", absPath)
	}
	if info.IsDir() {
		return ImageAttachment{}, fmt.Errorf("path is a directory: %s", absPath)
	}
	if info.Size() > maxImageSize {
		return ImageAttachment{}, fmt.Errorf("image too large: %s (%s, max 5MB)", info.Name(), formatSize(info.Size()))
	}

	// Read the file.
	data, err := os.ReadFile(absPath)
	if err != nil {
		return ImageAttachment{}, fmt.Errorf("failed to read image: %w", err)
	}

	return ImageAttachment{
		Path:      absPath,
		Name:      info.Name(),
		MediaType: mediaType,
		Data:      data,
		Size:      info.Size(),
	}, nil
}

// readClipboardImage attempts to read an image from the macOS clipboard using osascript.
// Returns the image data as PNG bytes, or an error if no image is available.
func readClipboardImage() ([]byte, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("clipboard image paste only supported on macOS")
	}

	tmpFile := filepath.Join(os.TempDir(), "providence_clipboard.png")
	defer os.Remove(tmpFile)

	script := fmt.Sprintf(`set png to (the clipboard as «class PNGf»)
set f to open for access POSIX file "%s" with write permission
write png to f
close access f`, tmpFile)

	cmd := exec.Command("osascript", "-e", script)
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("no image in clipboard")
	}

	data, err := os.ReadFile(tmpFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read clipboard image: %w", err)
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("clipboard image is empty")
	}

	if int64(len(data)) > maxImageSize {
		return nil, fmt.Errorf("clipboard image too large (%s, max 5MB)", formatSize(int64(len(data))))
	}

	return data, nil
}

// formatSize returns a human-readable file size string.
func formatSize(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%dB", bytes)
	}
	kb := float64(bytes) / 1024
	if kb < 1024 {
		return fmt.Sprintf("%.0fKB", kb)
	}
	mb := kb / 1024
	return fmt.Sprintf("%.1fMB", mb)
}
