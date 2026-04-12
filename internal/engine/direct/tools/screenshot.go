package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"runtime"

	"github.com/gravitrone/providence-core/internal/bridge/macos"
)

// ScreenshotTool captures the screen and returns the image path.
type ScreenshotTool struct {
	bridge *macos.Bridge
}

// NewScreenshotTool creates a new screenshot tool.
func NewScreenshotTool(bridge *macos.Bridge) *ScreenshotTool {
	return &ScreenshotTool{bridge: bridge}
}

func (t *ScreenshotTool) Name() string { return "Screenshot" }
func (t *ScreenshotTool) Description() string {
	return "Capture the screen or a region and return the image path. Use Read to view the image."
}
func (t *ScreenshotTool) ReadOnly() bool { return true }

func (t *ScreenshotTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"region": map[string]any{
				"type":        "object",
				"description": "Optional region to capture. Omit for full screen.",
				"properties": map[string]any{
					"x": map[string]any{"type": "integer", "description": "X coordinate of top-left corner."},
					"y": map[string]any{"type": "integer", "description": "Y coordinate of top-left corner."},
					"w": map[string]any{"type": "integer", "description": "Width of region."},
					"h": map[string]any{"type": "integer", "description": "Height of region."},
				},
				"required": []string{"x", "y", "w", "h"},
			},
		},
	}
}

func (t *ScreenshotTool) Execute(ctx context.Context, input map[string]any) ToolResult {
	if runtime.GOOS != "darwin" {
		return ToolResult{Content: "screenshot only available on macOS", IsError: true}
	}

	var path string
	var err error

	if regionRaw, ok := input["region"]; ok {
		region, ok := regionRaw.(map[string]any)
		if !ok {
			return ToolResult{Content: "invalid region format", IsError: true}
		}
		x := paramInt(region, "x", 0)
		y := paramInt(region, "y", 0)
		w := paramInt(region, "w", 0)
		h := paramInt(region, "h", 0)
		if w <= 0 || h <= 0 {
			return ToolResult{Content: "region width and height must be positive", IsError: true}
		}
		path, err = t.bridge.ScreenshotRegion(ctx, x, y, w, h)
	} else {
		path, err = t.bridge.Screenshot(ctx)
	}

	if err != nil {
		return ToolResult{Content: fmt.Sprintf("screenshot failed: %v", err), IsError: true}
	}

	// Read dimensions from the captured image
	width, height := readPNGDimensions(path)

	result := map[string]any{
		"path":   path,
		"width":  width,
		"height": height,
	}
	data, _ := json.Marshal(result)
	return ToolResult{Content: string(data)}
}

// readPNGDimensions reads width and height from a PNG file.
func readPNGDimensions(path string) (int, int) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	cfg, err := png.DecodeConfig(f)
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}
