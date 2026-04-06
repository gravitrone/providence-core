package direct

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gravitrone/providence-core/internal/engine/direct/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTool is a test tool with configurable behavior.
type mockTool struct {
	name     string
	readOnly bool
	delay    time.Duration
	output   string
	calls    atomic.Int32
}

func (m *mockTool) Name() string                                         { return m.name }
func (m *mockTool) Description() string                                  { return "mock" }
func (m *mockTool) InputSchema() map[string]any                          { return nil }
func (m *mockTool) ReadOnly() bool                                       { return m.readOnly }
func (m *mockTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	m.calls.Add(1)
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	return tools.ToolResult{Content: m.output}
}

func TestStreamingToolQueue_ReadOnlyParallel(t *testing.T) {
	t1 := &mockTool{name: "read1", readOnly: true, delay: 50 * time.Millisecond, output: "r1"}
	t2 := &mockTool{name: "read2", readOnly: true, delay: 50 * time.Millisecond, output: "r2"}
	reg := tools.NewRegistry(t1, t2)
	q := NewStreamingToolQueue(reg)

	start := time.Now()
	q.Submit(context.Background(), ToolCall{ID: "1", Name: "read1"})
	q.Submit(context.Background(), ToolCall{ID: "2", Name: "read2"})
	q.Wait()
	elapsed := time.Since(start)

	results := q.Results()
	require.Len(t, results, 2)
	// Should run in parallel, so total time < 2 * delay.
	assert.Less(t, elapsed, 90*time.Millisecond, "read-only tools should run in parallel")
}

func TestStreamingToolQueue_WriteDrainsReads(t *testing.T) {
	read := &mockTool{name: "read", readOnly: true, delay: 80 * time.Millisecond, output: "r"}
	write := &mockTool{name: "write", readOnly: false, output: "w"}
	reg := tools.NewRegistry(read, write)
	q := NewStreamingToolQueue(reg)

	q.Submit(context.Background(), ToolCall{ID: "1", Name: "read"})
	// Submit write - it should wait for the read to finish first.
	q.Submit(context.Background(), ToolCall{ID: "2", Name: "write"})
	q.Wait()

	results := q.Results()
	require.Len(t, results, 2)
	// Both should have completed.
	assert.Equal(t, int32(1), read.calls.Load())
	assert.Equal(t, int32(1), write.calls.Load())
}

func TestStreamingToolQueue_UnknownTool(t *testing.T) {
	reg := tools.NewRegistry()
	q := NewStreamingToolQueue(reg)
	q.Submit(context.Background(), ToolCall{ID: "1", Name: "nonexistent"})
	q.Wait()

	results := q.Results()
	require.Len(t, results, 1)
	assert.True(t, results[0].Result.IsError)
	assert.Contains(t, results[0].Result.Content, "unknown tool")
}
