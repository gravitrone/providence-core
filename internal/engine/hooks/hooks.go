// Package hooks implements lifecycle hook execution for Providence sessions.
// Hooks can be shell commands or HTTP endpoints, triggered on events like
// tool use, session start/end, compaction, and subagent lifecycle.
package hooks

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Timeout defaults in milliseconds.
const (
	ToolHookTimeoutMS       = 10 * 60 * 1000 // 10 minutes
	SessionEndHookTimeoutMS = 1500
)

// Event name constants for hook triggers.
const (
	PreToolUse           = "PreToolUse"
	PostToolUse          = "PostToolUse"
	PostToolUseFailure   = "PostToolUseFailure"
	Stop                 = "Stop"
	SessionStart         = "SessionStart"
	SessionEnd           = "SessionEnd"
	PreCompact           = "PreCompact"
	PostCompact          = "PostCompact"
	PermissionRequest    = "PermissionRequest"
	SubagentStart        = "SubagentStart"
	SubagentStop         = "SubagentStop"
	TaskCreated          = "TaskCreated"
	TaskCompleted        = "TaskCompleted"
	UserPromptSubmit     = "UserPromptSubmit"
	HarnessChange        = "HarnessChange"
	ForkSpawn            = "ForkSpawn"
	ForkMerge            = "ForkMerge"
	WorktreeCreate       = "WorktreeCreate"
	WorktreeRemove       = "WorktreeRemove"
	DashboardPanelUpdate = "DashboardPanelUpdate"
	PostSampling         = "PostSampling"
)

// --- Types ---

// HookConfig defines a single hook - either a shell command or HTTP endpoint.
type HookConfig struct {
	Command string        `toml:"command" json:"command,omitempty"`
	URL     string        `toml:"url" json:"url,omitempty"`
	Timeout time.Duration `toml:"timeout" json:"timeout,omitempty"`
}

// HookInput is the JSON payload sent to hook executors via stdin or POST body.
type HookInput struct {
	Event     string      `json:"event"`
	ToolName  string      `json:"tool_name,omitempty"`
	ToolInput interface{} `json:"tool_input,omitempty"`
	SessionID string      `json:"session_id,omitempty"`
	Timestamp string      `json:"timestamp"`
}

// HookOutput is the structured response from a hook executor.
type HookOutput struct {
	Continue       *bool  `json:"continue,omitempty"`
	StopReason     string `json:"stop_reason,omitempty"`
	Decision       string `json:"decision,omitempty"`
	Reason         string `json:"reason,omitempty"`
	SystemMessage  string `json:"system_message,omitempty"`
	SuppressOutput bool   `json:"suppress_output,omitempty"`
}

// BlockingError is returned when a hook exits with code 2 (blocking error).
type BlockingError struct {
	Message string
	Output  *HookOutput
}

// Error implements the error interface.
func (e *BlockingError) Error() string {
	return fmt.Sprintf("blocking hook error: %s", e.Message)
}

// --- Runner ---

// Runner executes hooks for lifecycle events.
type Runner struct {
	hooks map[string][]HookConfig
}

// NewRunner creates a Runner with the given event-to-hooks mapping.
func NewRunner(hooks map[string][]HookConfig) *Runner {
	if hooks == nil {
		hooks = make(map[string][]HookConfig)
	}
	return &Runner{hooks: hooks}
}

// HasHooks returns true if any hooks are registered for the given event.
func (r *Runner) HasHooks(event string) bool {
	configs, ok := r.hooks[event]
	return ok && len(configs) > 0
}

// Run executes all hooks for an event sequentially, returning the first
// non-nil output or error. If no hooks produce output, returns nil.
func (r *Runner) Run(ctx context.Context, event string, input HookInput) (*HookOutput, error) {
	configs, ok := r.hooks[event]
	if !ok {
		return nil, nil
	}

	input.Event = event
	if input.Timestamp == "" {
		input.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}

	for _, cfg := range configs {
		out, err := r.execOne(ctx, cfg, input)
		if err != nil {
			return out, err
		}
		if out != nil {
			return out, nil
		}
	}
	return nil, nil
}

// RunAsync fires all hooks for an event without waiting for results.
func (r *Runner) RunAsync(ctx context.Context, event string, input HookInput) {
	configs, ok := r.hooks[event]
	if !ok {
		return
	}

	input.Event = event
	if input.Timestamp == "" {
		input.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}

	var wg sync.WaitGroup
	for _, cfg := range configs {
		wg.Add(1)
		go func(c HookConfig) {
			defer wg.Done()
			_, _ = r.execOne(ctx, c, input)
		}(cfg)
	}
}

// execOne dispatches to the appropriate executor based on config.
func (r *Runner) execOne(ctx context.Context, cfg HookConfig, input HookInput) (*HookOutput, error) {
	if cfg.Command != "" {
		return execShellHook(ctx, cfg, input)
	}
	if cfg.URL != "" {
		return execHTTPHook(ctx, cfg, input)
	}
	return nil, nil
}
