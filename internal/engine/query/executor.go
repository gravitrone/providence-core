package query

import (
	"context"
	"sync"
)

// StreamingToolExecutor manages concurrent tool execution during streaming.
// Read-only (concurrency-safe) tools run in parallel; write tools serialize.
type StreamingToolExecutor struct {
	tools     []trackedTool
	executor  ToolExecutor
	mu        sync.Mutex
	wg        sync.WaitGroup
	results   []ToolResult
	resultsMu sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc
}

type trackedTool struct {
	id     string
	name   string
	input  string
	safe   bool   // isConcurrencySafe
	status string // "queued", "executing", "done"
}

// ToolResult holds the outcome of a single tool execution.
type ToolResult struct {
	ToolUseID string
	Name      string
	Content   string
	Error     error
}

// NewStreamingToolExecutor creates a new executor backed by the given ToolExecutor.
func NewStreamingToolExecutor(executor ToolExecutor) *StreamingToolExecutor {
	ctx, cancel := context.WithCancel(context.Background())
	return &StreamingToolExecutor{
		executor: executor,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// AddTool queues a tool for execution, starting it immediately if concurrency
// rules allow.
func (e *StreamingToolExecutor) AddTool(id, name, input string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	safe := e.executor.IsConcurrencySafe(name)
	t := trackedTool{
		id:     id,
		name:   name,
		input:  input,
		safe:   safe,
		status: "queued",
	}
	e.tools = append(e.tools, t)
	idx := len(e.tools) - 1

	if e.canExecute(safe) {
		e.startTool(idx)
	}
}

// GetCompletedResults returns all completed results without blocking.
func (e *StreamingToolExecutor) GetCompletedResults() []ToolResult {
	e.resultsMu.Lock()
	defer e.resultsMu.Unlock()

	out := make([]ToolResult, len(e.results))
	copy(out, e.results)
	e.results = e.results[:0]
	return out
}

// GetRemainingResults blocks until all queued tools complete, then returns
// every result.
func (e *StreamingToolExecutor) GetRemainingResults() []ToolResult {
	// Start any remaining queued tools that are waiting on serial ordering.
	e.drainQueued()
	e.wg.Wait()

	e.resultsMu.Lock()
	defer e.resultsMu.Unlock()

	out := make([]ToolResult, len(e.results))
	copy(out, e.results)
	e.results = nil
	return out
}

// Discard cancels all pending tool executions (e.g. on model fallback).
func (e *StreamingToolExecutor) Discard() {
	e.cancel()
	e.wg.Wait()
}

// canExecute reports whether a tool with the given safety can start now.
// Must be called with e.mu held.
func (e *StreamingToolExecutor) canExecute(isSafe bool) bool {
	executing := 0
	allSafe := true
	for _, t := range e.tools {
		if t.status == "executing" {
			executing++
			if !t.safe {
				allSafe = false
			}
		}
	}
	return executing == 0 || (isSafe && allSafe)
}

// startTool launches execution for tools[idx] in a goroutine.
// Must be called with e.mu held.
func (e *StreamingToolExecutor) startTool(idx int) {
	e.tools[idx].status = "executing"
	e.wg.Add(1)

	go func(t trackedTool) {
		defer e.wg.Done()

		result, err := e.executor.Execute(e.ctx, t.name, t.input)

		e.resultsMu.Lock()
		e.results = append(e.results, ToolResult{
			ToolUseID: t.id,
			Name:      t.name,
			Content:   result,
			Error:     err,
		})
		e.resultsMu.Unlock()

		// Mark done and start queued tools that are now unblocked.
		e.mu.Lock()
		for i := range e.tools {
			if e.tools[i].id == t.id {
				e.tools[i].status = "done"
				break
			}
		}
		e.startQueued()
		e.mu.Unlock()
	}(e.tools[idx])
}

// startQueued starts any queued tools that can now execute.
// Must be called with e.mu held.
func (e *StreamingToolExecutor) startQueued() {
	for i := range e.tools {
		if e.tools[i].status == "queued" && e.canExecute(e.tools[i].safe) {
			e.startTool(i)
		}
	}
}

// drainQueued starts all remaining queued tools respecting concurrency.
func (e *StreamingToolExecutor) drainQueued() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.startQueued()
}
