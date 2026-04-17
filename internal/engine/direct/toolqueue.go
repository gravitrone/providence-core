package direct

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/gravitrone/providence-core/internal/engine/direct/tools"
)

// DefaultMaxToolConcurrency is the default cap for parallel ReadOnly tool execution.
const DefaultMaxToolConcurrency = 10

// ToolCall represents a pending tool invocation extracted from a model response.
type ToolCall struct {
	ID    string
	Name  string
	Input map[string]any
}

// ToolCallResult pairs a ToolCall with its execution result.
type ToolCallResult struct {
	ToolCall
	Result tools.ToolResult
}

// maxToolConcurrency returns the concurrency cap from env or default.
func maxToolConcurrency() int {
	if v := os.Getenv("PROVIDENCE_MAX_TOOL_CONCURRENCY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return DefaultMaxToolConcurrency
}

// IsConcurrencySafe checks if a tool call is safe for parallel execution.
// Unlike the static ReadOnly() check, this also inspects tool input to make
// dynamic decisions. For example, a Bash tool with `command: "cat file"` is
// safe, but `command: "rm -rf /"` is not.
func IsConcurrencySafe(tool tools.Tool, input map[string]any) bool {
	if tool.ReadOnly() {
		return true
	}

	// Per-tool input inspection for Bash: allow parallel execution for
	// clearly read-only commands (cat, head, tail, wc, ls, echo, git log, etc).
	if tool.Name() == "Bash" {
		cmd, ok := input["command"].(string)
		if !ok || cmd == "" {
			return false
		}
		return isBashCommandReadOnly(cmd)
	}

	return false
}

// isBashCommandReadOnly returns true if the bash command appears to be
// a safe read-only operation based on the first word.
func isBashCommandReadOnly(cmd string) bool {
	cmd = strings.TrimSpace(cmd)

	// Strip leading env vars like VAR=val.
	for strings.Contains(cmd[:min(len(cmd), 50)], "=") {
		parts := strings.SplitN(cmd, " ", 2)
		if len(parts) < 2 || !strings.Contains(parts[0], "=") {
			break
		}
		cmd = strings.TrimSpace(parts[1])
	}

	// Get the base command (first word, strip path).
	firstWord := strings.Fields(cmd)
	if len(firstWord) == 0 {
		return false
	}
	base := firstWord[0]
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}

	// Compound operators and redirects can introduce writes or multiple
	// commands, even when the base command itself is read-only.
	if strings.ContainsAny(cmd, "|;&><") {
		return false
	}

	readOnlyCmds := map[string]bool{
		"cat": true, "head": true, "tail": true, "wc": true,
		"ls": true, "ll": true, "echo": true, "printf": true,
		"grep": true, "rg": true, "find": true, "which": true,
		"whoami": true, "date": true, "pwd": true, "env": true,
		"uname": true, "file": true, "stat": true, "du": true,
		"df": true, "tree": true, "less": true, "more": true,
		"diff": true, "md5sum": true, "sha256sum": true, "shasum": true,
		"jq": true, "yq": true, "awk": true, "sed": false, // sed can modify
	}

	// Git read-only subcommands.
	if base == "git" && len(firstWord) >= 2 {
		gitReadOnly := map[string]bool{
			"log": true, "status": true, "diff": true, "show": true,
			"branch": true, "tag": true, "remote": true, "describe": true,
			"rev-parse": true, "ls-files": true, "cat-file": true,
			"worktree": true, "config": true, "shortlog": true,
		}
		return gitReadOnly[firstWord[1]]
	}

	if safe, ok := readOnlyCmds[base]; ok {
		return safe
	}

	return false
}

// StreamingToolQueue executes tool calls with concurrency rules:
//   - ReadOnly tools run in parallel (goroutines), capped by PROVIDENCE_MAX_TOOL_CONCURRENCY.
//   - Non-ReadOnly tools run serially: wait for all in-flight, then execute.
//   - Bash errors trigger sibling cancellation: all other concurrent tools
//     receive context cancellation when a Bash tool returns an error.
type StreamingToolQueue struct {
	registry *tools.Registry
	results  []ToolCallResult
	mu       sync.Mutex
	wg       sync.WaitGroup

	// semaphore caps the number of parallel ReadOnly tool goroutines.
	semaphore chan struct{}

	// siblingCtx/siblingCancel: when a Bash tool errors, siblingCancel is called
	// to cancel all other concurrently running tools in this batch.
	siblingCtx    context.Context
	siblingCancel context.CancelFunc

	// turnCtx is the parent context for this batch of tool executions.
	// Each tool gets a child context that can be cancelled independently.
	turnCtx    context.Context
	turnCancel context.CancelFunc
}

// NewStreamingToolQueue creates a queue backed by the given tool registry.
func NewStreamingToolQueue(registry *tools.Registry) *StreamingToolQueue {
	q := &StreamingToolQueue{
		registry:  registry,
		semaphore: make(chan struct{}, maxToolConcurrency()),
	}
	return q
}

// Submit enqueues a tool call for execution following concurrency rules.
func (q *StreamingToolQueue) Submit(ctx context.Context, call ToolCall) {
	tool, ok := q.registry.Get(call.Name)
	if !ok {
		q.mu.Lock()
		q.results = append(q.results, ToolCallResult{
			ToolCall: call,
			Result:   tools.ToolResult{Content: "unknown tool: " + call.Name, IsError: true},
		})
		q.mu.Unlock()
		return
	}

	// Lazily initialize turn and sibling contexts for abort hierarchy.
	q.mu.Lock()
	if q.turnCtx == nil {
		q.turnCtx, q.turnCancel = context.WithCancel(ctx)
	}
	if q.siblingCtx == nil {
		//nolint:gosec // q.Wait/q.Cancel release this queue-owned cancel func.
		q.siblingCtx, q.siblingCancel = context.WithCancel(q.turnCtx)
	}
	siblingCtx := q.siblingCtx
	q.mu.Unlock()

	// Use input-aware concurrency check instead of just ReadOnly().
	if IsConcurrencySafe(tool, call.Input) {
		q.wg.Add(1)
		go func() {
			defer q.wg.Done()
			// Acquire semaphore slot to cap max parallel tools.
			q.semaphore <- struct{}{}
			defer func() { <-q.semaphore }()

			// Create per-tool child context under the sibling context.
			// Tool abort bubbles up cleanly without killing the turn.
			toolCtx, toolCancel := context.WithCancel(siblingCtx)
			defer toolCancel()

			result := tool.Execute(toolCtx, call.Input)
			q.mu.Lock()
			q.results = append(q.results, ToolCallResult{ToolCall: call, Result: result})
			q.mu.Unlock()
		}()
	} else {
		// Wait for all in-flight read-only goroutines to finish.
		q.wg.Wait()

		// Create per-tool child context for serial tools too.
		toolCtx, toolCancel := context.WithCancel(siblingCtx)
		defer toolCancel()

		// Execute serially (blocking).
		result := tool.Execute(toolCtx, call.Input)
		q.mu.Lock()
		q.results = append(q.results, ToolCallResult{ToolCall: call, Result: result})
		q.mu.Unlock()

		// Bash error sibling cascade: if a Bash tool errors, cancel all
		// other running concurrent tools in this batch.
		if call.Name == "Bash" && result.IsError {
			q.mu.Lock()
			if q.siblingCancel != nil {
				q.siblingCancel()
			}
			q.mu.Unlock()
		}
	}
}

// Wait blocks until all in-flight tool executions complete.
func (q *StreamingToolQueue) Wait() {
	defer func() {
		q.mu.Lock()
		defer q.mu.Unlock()
		if q.siblingCancel != nil {
			q.siblingCancel()
		}
		if q.turnCancel != nil {
			q.turnCancel()
		}
	}()
	q.wg.Wait()
}

// Results returns all collected tool call results.
func (q *StreamingToolQueue) Results() []ToolCallResult {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]ToolCallResult, len(q.results))
	copy(out, q.results)
	return out
}

// Cancel cancels the turn context, aborting all tool executions.
func (q *StreamingToolQueue) Cancel() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.siblingCancel != nil {
		q.siblingCancel()
	}
	if q.turnCancel != nil {
		q.turnCancel()
	}
}
