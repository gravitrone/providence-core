package query

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Executor Tests ---

func TestStreamingExecutorParallelReads(t *testing.T) {
	var concurrent atomic.Int32
	var maxConcurrent atomic.Int32

	toolExec := &mockToolExecutor{
		tools: []ToolDef{
			{Name: "Read", Description: "read"},
		},
		safeFn: func(_ string) bool { return true },
		executeFn: func(_ context.Context, _, _ string) (string, error) {
			cur := concurrent.Add(1)
			for {
				old := maxConcurrent.Load()
				if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
			concurrent.Add(-1)
			return "ok", nil
		},
	}

	e := NewStreamingToolExecutor(toolExec)
	e.AddTool("t1", "Read", `{"path": "/a"}`)
	e.AddTool("t2", "Read", `{"path": "/b"}`)
	e.AddTool("t3", "Read", `{"path": "/c"}`)

	results := e.GetRemainingResults()
	require.Len(t, results, 3)

	// All 3 should have run in parallel.
	assert.GreaterOrEqual(t, int(maxConcurrent.Load()), 2, "expected parallel execution for safe tools")
}

func TestStreamingExecutorSerialWrites(t *testing.T) {
	var concurrent atomic.Int32
	var maxConcurrent atomic.Int32

	toolExec := &mockToolExecutor{
		tools: []ToolDef{
			{Name: "Write", Description: "write"},
		},
		safeFn: func(_ string) bool { return false },
		executeFn: func(_ context.Context, _, _ string) (string, error) {
			cur := concurrent.Add(1)
			for {
				old := maxConcurrent.Load()
				if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(30 * time.Millisecond)
			concurrent.Add(-1)
			return "ok", nil
		},
	}

	e := NewStreamingToolExecutor(toolExec)
	e.AddTool("t1", "Write", `{"path": "/a"}`)
	e.AddTool("t2", "Write", `{"path": "/b"}`)
	e.AddTool("t3", "Write", `{"path": "/c"}`)

	results := e.GetRemainingResults()
	require.Len(t, results, 3)

	// Unsafe tools must serialize - max concurrency should be 1.
	assert.Equal(t, int32(1), maxConcurrent.Load(), "unsafe tools must not run in parallel")
}

func TestStreamingExecutorMixed(t *testing.T) {
	var mu sync.Mutex
	var order []string

	toolExec := &mockToolExecutor{
		tools: []ToolDef{
			{Name: "Read", Description: "read"},
			{Name: "Write", Description: "write"},
		},
		safeFn: func(name string) bool { return name == "Read" },
		executeFn: func(_ context.Context, name, _ string) (string, error) {
			time.Sleep(20 * time.Millisecond)
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
			return "ok", nil
		},
	}

	e := NewStreamingToolExecutor(toolExec)

	// Add a mix: two reads then a write.
	e.AddTool("t1", "Read", `{"path": "/a"}`)
	e.AddTool("t2", "Read", `{"path": "/b"}`)
	e.AddTool("t3", "Write", `{"path": "/c"}`)

	results := e.GetRemainingResults()
	require.Len(t, results, 3)

	// The write must come after both reads.
	mu.Lock()
	defer mu.Unlock()
	writeIdx := -1
	for i, name := range order {
		if name == "Write" {
			writeIdx = i
			break
		}
	}
	assert.Equal(t, 2, writeIdx, "write should execute after reads complete")
}

func TestStreamingExecutorDiscard(t *testing.T) {
	started := make(chan struct{})

	toolExec := &mockToolExecutor{
		tools: []ToolDef{
			{Name: "Slow", Description: "slow tool"},
		},
		safeFn: func(_ string) bool { return true },
		executeFn: func(ctx context.Context, _, _ string) (string, error) {
			close(started)
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(5 * time.Second):
				return "should not reach", nil
			}
		},
	}

	e := NewStreamingToolExecutor(toolExec)
	e.AddTool("t1", "Slow", `{}`)

	// Wait for the tool to start.
	<-started

	// Discard should cancel and return quickly.
	discardDone := make(chan struct{})
	go func() {
		e.Discard()
		close(discardDone)
	}()

	select {
	case <-discardDone:
		// Good - discard completed.
	case <-time.After(2 * time.Second):
		t.Fatal("discard did not complete in time")
	}
}
