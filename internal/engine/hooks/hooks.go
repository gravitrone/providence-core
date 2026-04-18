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
	SessionStarted       = "SessionStarted"
	SessionClosed        = "SessionClosed"
	PreCompact           = "PreCompact"
	PostCompact          = "PostCompact"
	PermissionRequest    = "PermissionRequest"
	PermissionDenied     = "PermissionDenied"
	PermissionGranted    = "PermissionGranted"
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
	CwdChanged           = "CwdChanged"
	FileChanged          = "FileChanged"
	FileRead             = "FileRead"
	HookExecuted         = "HookExecuted"
	TurnStarted          = "TurnStarted"
	TurnCompleted        = "TurnCompleted"
	ModelChanged         = "ModelChanged"
)

// --- Types ---

// HookConfig defines a single hook - either a shell command or HTTP endpoint.
type HookConfig struct {
	Command string        `toml:"command" json:"command,omitempty"`
	URL     string        `toml:"url" json:"url,omitempty"`
	Timeout time.Duration `toml:"timeout" json:"timeout,omitempty"`
	Async   bool          `toml:"async" json:"async,omitempty"`
	TTL     time.Duration `toml:"ttl" json:"ttl,omitempty"`
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
	hooks   map[string][]HookConfig
	pending *PendingHooks
	exec    HookExecutor
}

// NewRunner creates a Runner with the given event-to-hooks mapping.
func NewRunner(hooks map[string][]HookConfig) *Runner {
	if hooks == nil {
		hooks = make(map[string][]HookConfig)
	}
	return &Runner{
		hooks:   hooks,
		pending: NewPendingHooks(),
		exec:    defaultHookExecutor,
	}
}

// HasHooks returns true if any hooks are registered for the given event.
func (r *Runner) HasHooks(event string) bool {
	configs, ok := r.hooks[event]
	return ok && len(configs) > 0
}

// DrainCompleted returns async hook results that are ready for injection.
func (r *Runner) DrainCompleted() []CompletedHook {
	if r == nil || r.pending == nil {
		return nil
	}
	return r.pending.DrainCompleted()
}

// PendingCount returns the number of tracked async hooks.
func (r *Runner) PendingCount() int {
	if r == nil || r.pending == nil {
		return 0
	}
	return r.pending.PendingCount()
}

// CompletedCount returns the number of completed async hooks waiting to be drained.
func (r *Runner) CompletedCount() int {
	if r == nil || r.pending == nil {
		return 0
	}
	return r.pending.CompletedCount()
}

// Close cancels all tracked async hooks.
func (r *Runner) Close() {
	if r == nil || r.pending == nil {
		return
	}
	r.pending.Close()
}

// Run executes all hooks for an event sequentially, returning the first
// non-nil output or error. If no hooks produce output, returns nil.
func (r *Runner) Run(ctx context.Context, event string, input HookInput) (*HookOutput, error) {
	out, err := r.run(ctx, event, input)
	r.runHookExecuted(ctx, event, input)
	return out, err
}

func (r *Runner) run(ctx context.Context, event string, input HookInput) (*HookOutput, error) {
	configs, ok := r.hooks[event]
	if !ok {
		return nil, nil
	}

	input.Event = event
	if input.Timestamp == "" {
		input.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}

	for _, cfg := range configs {
		if cfg.Async {
			r.pending.Dispatch(ctx, event, cfg, input, r.exec)
			continue
		}

		out, err := r.exec(ctx, cfg, input)
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

	go func() {
		var wg sync.WaitGroup
		for _, cfg := range configs {
			if cfg.Async {
				r.pending.Dispatch(ctx, event, cfg, input, r.exec)
				continue
			}
			wg.Add(1)
			go func(c HookConfig) {
				defer wg.Done()
				_, _ = r.exec(ctx, c, input)
			}(cfg)
		}
		wg.Wait()
		r.runHookExecuted(ctx, event, input)
	}()
}

func defaultHookExecutor(ctx context.Context, cfg HookConfig, input HookInput) (*HookOutput, error) {
	if cfg.Command != "" {
		return execShellHook(ctx, cfg, input)
	}
	if cfg.URL != "" {
		return execHTTPHook(ctx, cfg, input)
	}
	return nil, nil
}

func (r *Runner) runHookExecuted(ctx context.Context, event string, input HookInput) {
	if event == HookExecuted || !r.HasHooks(event) || !r.HasHooks(HookExecuted) {
		return
	}

	_, _ = r.run(ctx, HookExecuted, HookInput{
		SessionID: input.SessionID,
		ToolName:  event,
		ToolInput: map[string]any{
			"trigger_event": event,
			"trigger_input": input,
		},
	})
}
