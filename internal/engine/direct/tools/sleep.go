package tools

import (
	"context"
	"fmt"
	"time"
)

// SleepTool pauses the agent loop for a specified duration.
// Prefer this over Bash(sleep) - doesn't hold a shell process.
// Cache-aware: sleeping >5 min forces cache miss on next wake.
type SleepTool struct{}

func (s SleepTool) Name() string { return "Sleep" }
func (s SleepTool) Description() string {
	return "Sleep for a specified duration in milliseconds. Use this instead of Bash(sleep) - it doesn't hold a shell process and can be interrupted by user input."
}
func (s SleepTool) ReadOnly() bool { return true }

func (s SleepTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"duration_ms": map[string]any{
				"type":        "integer",
				"description": "Duration to sleep in milliseconds",
				"minimum":     100,
				"maximum":     3600000, // 1 hour max
			},
		},
		"required": []string{"duration_ms"},
	}
}

// Prompt implements ToolPrompter.
func (s SleepTool) Prompt() string {
	return `Sleep for a specified duration. Prefer this over Bash(sleep) - it doesn't hold a shell process and can be interrupted by user input. Cache-aware: sleeping >5 minutes causes a prompt cache miss, so prefer shorter intervals.`
}

// Execute blocks for the specified duration, cancellable via context.
func (s SleepTool) Execute(ctx context.Context, input map[string]any) ToolResult {
	ms := paramInt(input, "duration_ms", 1000)
	if ms < 100 {
		ms = 100
	}
	if ms > 3600000 {
		ms = 3600000
	}

	dur := time.Duration(ms) * time.Millisecond

	select {
	case <-time.After(dur):
		secs := float64(ms) / 1000.0
		return ToolResult{Content: fmt.Sprintf("Slept for %.1fs", secs)}
	case <-ctx.Done():
		return ToolResult{Content: "Sleep interrupted by user input"}
	}
}
