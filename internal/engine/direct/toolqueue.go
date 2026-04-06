package direct

import (
	"context"
	"sync"

	"github.com/gravitrone/providence-core/internal/engine/direct/tools"
)

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

// StreamingToolQueue executes tool calls with concurrency rules:
//   - ReadOnly tools run in parallel (goroutines).
//   - Non-ReadOnly tools run serially: wait for all in-flight, then execute.
type StreamingToolQueue struct {
	registry *tools.Registry
	results  []ToolCallResult
	mu       sync.Mutex
	wg       sync.WaitGroup
}

// NewStreamingToolQueue creates a queue backed by the given tool registry.
func NewStreamingToolQueue(registry *tools.Registry) *StreamingToolQueue {
	return &StreamingToolQueue{
		registry: registry,
	}
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

	if tool.ReadOnly() {
		q.wg.Add(1)
		go func() {
			defer q.wg.Done()
			result := tool.Execute(ctx, call.Input)
			q.mu.Lock()
			q.results = append(q.results, ToolCallResult{ToolCall: call, Result: result})
			q.mu.Unlock()
		}()
	} else {
		// Wait for all in-flight read-only goroutines to finish.
		q.wg.Wait()
		// Execute serially (blocking).
		result := tool.Execute(ctx, call.Input)
		q.mu.Lock()
		q.results = append(q.results, ToolCallResult{ToolCall: call, Result: result})
		q.mu.Unlock()
	}
}

// Wait blocks until all in-flight tool executions complete.
func (q *StreamingToolQueue) Wait() {
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
