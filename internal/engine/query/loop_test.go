package query

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock Provider ---

type mockProvider struct {
	streamFn func(ctx context.Context, messages []Message, tools []ToolDef, systemPrompt string) (<-chan StreamEvent, error)
	model    string
}

func (m *mockProvider) Stream(ctx context.Context, messages []Message, tools []ToolDef, systemPrompt string) (<-chan StreamEvent, error) {
	return m.streamFn(ctx, messages, tools, systemPrompt)
}

func (m *mockProvider) OneShot(_ context.Context, _, _ string) (string, error) {
	return "", nil
}

func (m *mockProvider) Model() string        { return m.model }
func (m *mockProvider) ContextWindow() int    { return 200000 }
func (m *mockProvider) MaxOutputTokens() int  { return 8192 }

// --- Mock Tool Executor ---

type mockToolExecutor struct {
	executeFn func(ctx context.Context, name, input string) (string, error)
	tools     []ToolDef
	safeFn    func(name string) bool
}

func (m *mockToolExecutor) Execute(ctx context.Context, name, input string) (string, error) {
	if m.executeFn != nil {
		return m.executeFn(ctx, name, input)
	}
	return "ok", nil
}

func (m *mockToolExecutor) IsConcurrencySafe(name string) bool {
	if m.safeFn != nil {
		return m.safeFn(name)
	}
	return true
}

func (m *mockToolExecutor) ListTools() []ToolDef {
	return m.tools
}

// --- Helpers ---

// streamEvents sends a sequence of StreamEvents on a channel and closes it.
func streamEvents(evs ...StreamEvent) <-chan StreamEvent {
	ch := make(chan StreamEvent, len(evs))
	for _, ev := range evs {
		ch <- ev
	}
	close(ch)
	return ch
}

// drainEvents consumes all LoopEvents until the channel closes.
func drainEvents(ch <-chan LoopEvent) []LoopEvent {
	var out []LoopEvent
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

// --- Tests ---

func TestQueryLoopBasicCompletion(t *testing.T) {
	provider := &mockProvider{
		model: "claude-sonnet-4-20250514",
		streamFn: func(_ context.Context, _ []Message, _ []ToolDef, _ string) (<-chan StreamEvent, error) {
			return streamEvents(
				StreamEvent{Type: "text_delta", Text: "hello "},
				StreamEvent{Type: "text_delta", Text: "world"},
				StreamEvent{Type: "message_complete", StopReason: "end_turn", InputTokens: 100, OutputTokens: 20},
			), nil
		},
	}

	deps := &Deps{
		Provider:     provider,
		SystemPrompt: "you are helpful",
		MaxTurns:     10,
	}
	state := &State{
		Messages: []Message{{Role: "user", Content: "hi"}},
	}

	events, done := QueryLoop(context.Background(), deps, state)

	evs := drainEvents(events)
	term := <-done

	assert.Equal(t, "completed", term.Reason)
	assert.Equal(t, 1, term.TurnCount)
	assert.Nil(t, term.Err)

	// Should have text_delta and usage events.
	var texts []string
	var hasUsage bool
	for _, ev := range evs {
		if ev.Type == "text_delta" {
			texts = append(texts, ev.Data.(TextDeltaData).Text)
		}
		if ev.Type == "usage" {
			hasUsage = true
		}
	}
	assert.Equal(t, []string{"hello ", "world"}, texts)
	assert.True(t, hasUsage)
}

func TestQueryLoopToolExecution(t *testing.T) {
	callCount := 0
	provider := &mockProvider{
		model: "claude-sonnet-4-20250514",
		streamFn: func(_ context.Context, msgs []Message, _ []ToolDef, _ string) (<-chan StreamEvent, error) {
			callCount++
			if callCount == 1 {
				// First call: model requests a tool.
				return streamEvents(
					StreamEvent{Type: "text_delta", Text: "let me read that"},
					StreamEvent{Type: "tool_use_stop", ToolUseID: "tu_1", ToolName: "Read", ToolInput: `{"path": "/tmp/test"}`},
					StreamEvent{Type: "message_complete", StopReason: "tool_use"},
				), nil
			}
			// Second call: model completes after seeing tool result.
			return streamEvents(
				StreamEvent{Type: "text_delta", Text: "file contains: hello"},
				StreamEvent{Type: "message_complete", StopReason: "end_turn"},
			), nil
		},
	}

	toolExec := &mockToolExecutor{
		tools: []ToolDef{{Name: "Read", Description: "read a file"}},
		executeFn: func(_ context.Context, name, _ string) (string, error) {
			require.Equal(t, "Read", name)
			return "hello", nil
		},
	}

	deps := &Deps{
		Provider:     provider,
		Tools:        toolExec,
		SystemPrompt: "you are helpful",
		MaxTurns:     10,
	}
	state := &State{
		Messages: []Message{{Role: "user", Content: "read /tmp/test"}},
	}

	events, done := QueryLoop(context.Background(), deps, state)

	evs := drainEvents(events)
	term := <-done

	assert.Equal(t, "completed", term.Reason)
	assert.Equal(t, 2, term.TurnCount)
	assert.Equal(t, 2, callCount)

	// Check we got tool_start and tool_result events.
	var gotToolStart, gotToolResult bool
	for _, ev := range evs {
		if ev.Type == "tool_start" {
			gotToolStart = true
			td := ev.Data.(ToolStartData)
			assert.Equal(t, "Read", td.Name)
		}
		if ev.Type == "tool_result" {
			gotToolResult = true
			td := ev.Data.(ToolResultData)
			assert.Equal(t, "hello", td.Content)
		}
	}
	assert.True(t, gotToolStart, "expected tool_start event")
	assert.True(t, gotToolResult, "expected tool_result event")
}

func TestQueryLoopMaxTurns(t *testing.T) {
	callCount := 0
	provider := &mockProvider{
		model: "claude-sonnet-4-20250514",
		streamFn: func(_ context.Context, _ []Message, _ []ToolDef, _ string) (<-chan StreamEvent, error) {
			callCount++
			return streamEvents(
				StreamEvent{Type: "tool_use_stop", ToolUseID: fmt.Sprintf("tu_%d", callCount), ToolName: "Bash", ToolInput: `{"cmd": "ls"}`},
				StreamEvent{Type: "message_complete", StopReason: "tool_use"},
			), nil
		},
	}

	toolExec := &mockToolExecutor{
		tools: []ToolDef{{Name: "Bash", Description: "run command"}},
	}

	deps := &Deps{
		Provider:     provider,
		Tools:        toolExec,
		SystemPrompt: "you are helpful",
		MaxTurns:     3,
	}
	state := &State{
		Messages: []Message{{Role: "user", Content: "keep going"}},
	}

	events, done := QueryLoop(context.Background(), deps, state)
	drainEvents(events)
	term := <-done

	assert.Equal(t, "max_turns", term.Reason)
	assert.Equal(t, 3, term.TurnCount)
}

func TestQueryLoopCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	provider := &mockProvider{
		model: "claude-sonnet-4-20250514",
		streamFn: func(_ context.Context, _ []Message, _ []ToolDef, _ string) (<-chan StreamEvent, error) {
			// Cancel context, then block the stream so the loop sees cancellation.
			cancel()
			ch := make(chan StreamEvent)
			go func() {
				// Keep channel open - the loop should detect ctx.Done.
				<-ctx.Done()
				close(ch)
			}()
			return ch, nil
		},
	}

	deps := &Deps{
		Provider:     provider,
		SystemPrompt: "test",
		MaxTurns:     10,
	}
	state := &State{
		Messages: []Message{{Role: "user", Content: "hi"}},
	}

	events, done := QueryLoop(ctx, deps, state)
	drainEvents(events)
	term := <-done

	assert.Equal(t, "cancelled", term.Reason)
	assert.Error(t, term.Err)
}
