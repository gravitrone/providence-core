package direct

import (
	"context"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/gravitrone/providence-core/internal/engine/direct/tools"
	"github.com/gravitrone/providence-core/internal/engine/session"
	"github.com/gravitrone/providence-core/internal/engine/subagent"
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

func (m *mockTool) Name() string                { return m.name }
func (m *mockTool) Description() string         { return "mock" }
func (m *mockTool) InputSchema() map[string]any { return nil }
func (m *mockTool) ReadOnly() bool              { return m.readOnly }
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

func TestStreamingToolQueue_Cancel(t *testing.T) {
	slow := &mockTool{name: "slow", readOnly: true, delay: 500 * time.Millisecond, output: "slow"}
	reg := tools.NewRegistry(slow)
	q := NewStreamingToolQueue(reg)

	q.Submit(context.Background(), ToolCall{ID: "1", Name: "slow"})
	// Cancel should not panic even without waiting.
	q.Cancel()
	q.Wait()
}

func TestToolQueueWaitCancelsContexts(t *testing.T) {
	read := &mockTool{name: "read", readOnly: true, output: "ok"}
	reg := tools.NewRegistry(read)
	q := NewStreamingToolQueue(reg)

	q.Submit(context.Background(), ToolCall{ID: "1", Name: "read"})

	require.NotNil(t, q.turnCtx)
	require.NotNil(t, q.siblingCtx)

	q.Wait()

	require.ErrorIs(t, q.turnCtx.Err(), context.Canceled)
	require.ErrorIs(t, q.siblingCtx.Err(), context.Canceled)
}

func TestIsConcurrencySafe_ReadOnlyToolAlwaysSafe(t *testing.T) {
	readOnly := &mockTool{name: "safe", readOnly: true}
	assert.True(t, IsConcurrencySafe(readOnly, nil))
	assert.True(t, IsConcurrencySafe(readOnly, map[string]any{"anything": "here"}))
}

func TestIsConcurrencySafe_WriteToolAlwaysUnsafe(t *testing.T) {
	writeTool := &mockTool{name: "writer", readOnly: false}
	assert.False(t, IsConcurrencySafe(writeTool, nil))
}

type mockBashTool struct {
	readOnly bool
}

func (m *mockBashTool) Name() string                { return "Bash" }
func (m *mockBashTool) Description() string         { return "bash" }
func (m *mockBashTool) InputSchema() map[string]any { return nil }
func (m *mockBashTool) ReadOnly() bool              { return m.readOnly }
func (m *mockBashTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	return tools.ToolResult{Content: "ok"}
}

func TestIsConcurrencySafe_BashReadOnlyCommands(t *testing.T) {
	bash := &mockBashTool{readOnly: false}

	// Safe read-only bash commands.
	safeInputs := []map[string]any{
		{"command": "cat main.go"},
		{"command": "head -20 file.txt"},
		{"command": "ls -la"},
		{"command": "git log --oneline -10"},
		{"command": "git status"},
		{"command": "wc -l file.go"},
		{"command": "pwd"},
	}
	for _, input := range safeInputs {
		assert.True(t, IsConcurrencySafe(bash, input), "should be safe: %s", input["command"])
	}

	// Unsafe bash commands.
	unsafeInputs := []map[string]any{
		{"command": "rm -rf /"},
		{"command": "mv file1 file2"},
		{"command": ""},
	}
	for _, input := range unsafeInputs {
		assert.False(t, IsConcurrencySafe(bash, input), "should be unsafe: %s", input["command"])
	}
}

func TestIsBashCommandReadOnly(t *testing.T) {
	tests := []struct {
		cmd  string
		safe bool
	}{
		{"cat file.go", true},
		{"head -20 main.go", true},
		{"tail -f log.txt", true},
		{"ls -la /tmp", true},
		{"grep pattern file", true},
		{"git log --oneline", true},
		{"git diff HEAD", true},
		{"git status", true},
		{"git branch -v", true},
		{"git push origin main", false},
		{"git commit -m 'msg'", false},
		{"rm file.txt", false},
		{"cp a b", false},
		{"VAR=val cat file.go", true},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			assert.Equal(t, tt.safe, isBashCommandReadOnly(tt.cmd))
		})
	}
}

func TestIsBashCommandReadOnlyRejectsCompoundCommands(t *testing.T) {
	tests := []string{
		"cat file.go && rm file.go",
		"ls | grep foo && rm x",
		"echo hello; echo world",
		"echo > /tmp/x",
	}

	for _, cmd := range tests {
		t.Run(cmd, func(t *testing.T) {
			assert.False(t, isBashCommandReadOnly(cmd))
		})
	}
}

func TestStreamingToolQueue_ResultsOrder(t *testing.T) {
	// Serial tools should produce results in submission order.
	t1 := &mockTool{name: "w1", readOnly: false, output: "first"}
	t2 := &mockTool{name: "w2", readOnly: false, output: "second"}
	reg := tools.NewRegistry(t1, t2)
	q := NewStreamingToolQueue(reg)

	q.Submit(context.Background(), ToolCall{ID: "1", Name: "w1"})
	q.Submit(context.Background(), ToolCall{ID: "2", Name: "w2"})
	q.Wait()

	results := q.Results()
	require.Len(t, results, 2)
	assert.Equal(t, "first", results[0].Result.Content)
	assert.Equal(t, "second", results[1].Result.Content)
}

func TestCloseWaitsForPendingToolSummary(t *testing.T) {
	e := &DirectEngine{
		history: NewConversationHistory(),
	}

	releaseSummary := make(chan struct{})
	e.summaryWg.Add(1)
	go func() {
		defer e.summaryWg.Done()
		<-releaseSummary
	}()

	closeDone := make(chan struct{})
	go func() {
		e.Close()
		close(closeDone)
	}()

	select {
	case <-closeDone:
		t.Fatal("close returned before pending tool summary finished")
	case <-time.After(150 * time.Millisecond):
	}

	close(releaseSummary)

	select {
	case <-closeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("close did not return after pending tool summary finished")
	}
}

func TestEnableBackgroundAgentsCancelsPriorOnReEnable(t *testing.T) {
	e, err := NewDirectEngine(engine.EngineConfig{
		Type:   engine.EngineTypeDirect,
		Model:  "claude-sonnet-4-20250514",
		APIKey: "test-key-not-real",
	})
	require.NoError(t, err)
	t.Cleanup(func() { e.Close() })

	e.EnableBackgroundAgents(map[string]subagent.BackgroundAgentType{})
	require.Eventually(t, func() bool {
		return sessionSubscriberCount(e.sessionBus) == 1
	}, time.Second, 10*time.Millisecond)

	firstCancelCalled := make(chan struct{})
	firstCancel := e.bgCancel
	require.NotNil(t, firstCancel)
	e.bgCancel = func() {
		close(firstCancelCalled)
		firstCancel()
	}

	e.EnableBackgroundAgents(map[string]subagent.BackgroundAgentType{})

	require.Eventually(t, func() bool {
		select {
		case <-firstCancelCalled:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)

	require.Eventually(t, func() bool {
		return sessionSubscriberCount(e.sessionBus) == 1
	}, time.Second, 10*time.Millisecond)
}

func sessionSubscriberCount(bus *session.Bus) int {
	if bus == nil {
		return 0
	}
	return reflect.ValueOf(bus).Elem().FieldByName("subscribers").Len()
}
