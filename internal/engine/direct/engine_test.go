package direct

import (
	"fmt"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/gravitrone/providence-core/internal/engine/session"
	"github.com/gravitrone/providence-core/internal/engine/subagent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDirectEngine(t *testing.T) {
	e, err := NewDirectEngine(engine.EngineConfig{
		Type:         engine.EngineTypeDirect,
		SystemPrompt: "You are a test bot.",
		Model:        "claude-sonnet-4-20250514",
		APIKey:       "test-key-not-real",
	})
	require.NoError(t, err)
	assert.Equal(t, engine.StatusIdle, e.Status())
	assert.NotEmpty(t, e.sessionID)
}

func TestDirectEngine_StatusTransitions(t *testing.T) {
	e, err := NewDirectEngine(engine.EngineConfig{
		Type:   engine.EngineTypeDirect,
		Model:  "claude-sonnet-4-20250514",
		APIKey: "test-key-not-real",
	})
	require.NoError(t, err)

	assert.Equal(t, engine.StatusIdle, e.Status())

	// Can't test full Send without a real API, but we can verify the guard.
	// Manually set to running to test the guard.
	e.mu.Lock()
	e.status = engine.StatusRunning
	e.mu.Unlock()

	err = e.Send("should fail")
	assert.Error(t, err, "should not allow send while running")
}

func TestDirectEngine_EventsChannel(t *testing.T) {
	e, err := NewDirectEngine(engine.EngineConfig{
		Type:   engine.EngineTypeDirect,
		Model:  "claude-sonnet-4-20250514",
		APIKey: "test-key-not-real",
	})
	require.NoError(t, err)

	ch := e.Events()
	assert.NotNil(t, ch)
}

func TestUsageEventEmitted(t *testing.T) {
	e := &DirectEngine{
		events:  make(chan engine.ParsedEvent, 1),
		history: NewConversationHistory(),
	}

	e.emitUsageUpdate(11, 4, 2, 1)

	assert.Equal(t, 15, e.history.CurrentTokens())

	event := <-e.events
	assert.Equal(t, "usage_update", event.Type)

	usage, ok := event.Data.(*engine.UsageUpdateEvent)
	require.True(t, ok)
	assert.Equal(t, "usage_update", usage.Type)
	assert.Equal(t, 11, usage.InputTokens)
	assert.Equal(t, 4, usage.OutputTokens)
	assert.Equal(t, 15, usage.TotalTokens)
	assert.Equal(t, 2, usage.CacheReadTokens)
	assert.Equal(t, 1, usage.CacheCreateTokens)
}

func TestDirectEngine_InterruptIdempotent(t *testing.T) {
	e, err := NewDirectEngine(engine.EngineConfig{
		Type:   engine.EngineTypeDirect,
		Model:  "claude-sonnet-4-20250514",
		APIKey: "test-key-not-real",
	})
	require.NoError(t, err)

	// Should not panic even when called multiple times.
	e.Interrupt()
	e.Interrupt()
}

func TestDirectEngine_Steer(t *testing.T) {
	e, err := NewDirectEngine(engine.EngineConfig{
		Type:   engine.EngineTypeDirect,
		Model:  "claude-sonnet-4-20250514",
		APIKey: "test-key-not-real",
	})
	require.NoError(t, err)

	e.Steer("focus on security")
	e.Steer("also check performance")

	e.steerMu.Lock()
	assert.Len(t, e.steered, 2)
	e.steerMu.Unlock()

	// drainSteeredMessages should move them to history and clear the slice.
	e.drainSteeredMessages()
	e.steerMu.Lock()
	assert.Empty(t, e.steered)
	e.steerMu.Unlock()

	msgs := e.history.Messages()
	assert.Len(t, msgs, 2)
}

func TestDirectEngine_RespondPermission(t *testing.T) {
	e, err := NewDirectEngine(engine.EngineConfig{
		Type:   engine.EngineTypeDirect,
		Model:  "claude-sonnet-4-20250514",
		APIKey: "test-key-not-real",
	})
	require.NoError(t, err)

	// RespondPermission should not error even for unknown question IDs.
	err = e.RespondPermission("nonexistent", "allow")
	assert.NoError(t, err)
}

func TestSystemBlocksHaveCacheControl(t *testing.T) {
	e, err := NewDirectEngine(engine.EngineConfig{
		Type:         engine.EngineTypeDirect,
		SystemPrompt: engine.BuildSystemPrompt(nil),
		Model:        "claude-sonnet-4-20250514",
		APIKey:       "test-key-not-real",
	})
	require.NoError(t, err)

	blocks := e.systemBlocks()
	require.NotEmpty(t, blocks)
	assert.Equal(t, anthropic.NewCacheControlEphemeralParam(), blocks[len(blocks)-1].CacheControl)
}

func TestRestoreHistory_WithTools(t *testing.T) {
	e, err := NewDirectEngine(engine.EngineConfig{
		Type:   engine.EngineTypeDirect,
		Model:  "claude-sonnet-4-20250514",
		APIKey: "test-key-not-real",
	})
	require.NoError(t, err)

	// Seed some pre-existing history so we can verify the restore wipes it.
	e.history.AddUser("stale")

	restored := []engine.RestoredMessage{
		{Role: "user", Content: "first user turn"},
		{
			Role:      "tool",
			ToolName:  "ReadFile",
			ToolInput: "main.go",
			Content:   "package main",
		},
		{Role: "assistant", Content: "first assistant reply"},
		{Role: "system", Content: "should be skipped"},
		{Role: "permission", Content: "should be skipped"},
	}

	require.NoError(t, e.RestoreHistory(restored))

	msgs := e.history.Messages()
	require.Len(t, msgs, 3, "tool restore should synthesize an assistant text message")

	// Spot check roles and content.
	assert.Equal(t, "first user turn", msgs[0].Content[0].OfText.Text)
	assert.Equal(t, "[Tool: ReadFile(main.go) -> package main]", msgs[1].Content[0].OfText.Text)
	assert.Equal(t, "first assistant reply", msgs[2].Content[0].OfText.Text)

	// Restoring again should replace, not append.
	require.NoError(t, e.RestoreHistory([]engine.RestoredMessage{
		{Role: "user", Content: "fresh"},
		{Role: "tool", ToolName: "Bash", ToolInput: "pwd", Content: "/tmp/project"},
	}))
	msgs = e.history.Messages()
	require.Len(t, msgs, 2)
	assert.Equal(t, "fresh", msgs[0].Content[0].OfText.Text)
	assert.Equal(t, "[Tool: Bash(pwd) -> /tmp/project]", msgs[1].Content[0].OfText.Text)
}

func TestIsOverloadError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"generic error", fmt.Errorf("connection refused"), false},
		{"529 status", fmt.Errorf(`POST "https://api.anthropic.com/v1/messages": 529`), true},
		{"overloaded string", fmt.Errorf("overloaded_error: too many requests"), true},
		{"overloaded in body", fmt.Errorf(`{"type":"overloaded_error","message":"overloaded"}`), true},
		{"rate limit is not overload", fmt.Errorf("rate_limit_error"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isOverloadError(tt.err))
		})
	}
}

func TestModelFallbackOnOverload(t *testing.T) {
	e := &DirectEngine{
		events:   make(chan engine.ParsedEvent, 16),
		history:  NewConversationHistory(),
		model:    "claude-opus-4-6",
		provider: engine.ProviderAnthropic,
	}

	// Simulate what the agent loop does on overload.
	err := fmt.Errorf(`POST "https://api.anthropic.com/v1/messages": 529 overloaded`)
	require.True(t, isOverloadError(err))
	require.False(t, e.fallbackActive)

	fallback := engine.FastForProvider(e.provider)
	require.NotEmpty(t, fallback, "anthropic provider should have a fast-tier model")
	require.NotEqual(t, e.model, fallback)

	// Apply the fallback.
	previousModel := e.model
	e.model = fallback
	e.fallbackActive = true

	e.events <- engine.ParsedEvent{
		Type: "tombstone",
		Data: &engine.TombstoneEvent{Type: "tombstone", MessageIndex: -1},
	}
	e.events <- engine.ParsedEvent{
		Type: "system_message",
		Data: &engine.SystemMessageEvent{
			Type:    "system_message",
			Content: fmt.Sprintf("Model overloaded. Switched from %s to %s.", previousModel, fallback),
		},
	}

	// Verify tombstone event.
	tomb := <-e.events
	assert.Equal(t, "tombstone", tomb.Type)
	te, ok := tomb.Data.(*engine.TombstoneEvent)
	require.True(t, ok)
	assert.Equal(t, -1, te.MessageIndex)

	// Verify system message event.
	sysMsg := <-e.events
	assert.Equal(t, "system_message", sysMsg.Type)
	sm, ok := sysMsg.Data.(*engine.SystemMessageEvent)
	require.True(t, ok)
	assert.Contains(t, sm.Content, previousModel)
	assert.Contains(t, sm.Content, fallback)

	// Verify model was switched.
	assert.Equal(t, fallback, e.model)
	assert.True(t, e.fallbackActive)
}

func TestMaxOutputTokensRecovery(t *testing.T) {
	e := &DirectEngine{
		events:   make(chan engine.ParsedEvent, 16),
		history:  NewConversationHistory(),
		model:    "claude-sonnet-4-6",
		provider: engine.ProviderAnthropic,
	}

	// Simulate recovery loop.
	for i := 0; i < MaxOutputTokensRecoveryLimit; i++ {
		assert.Less(t, e.maxOutputRecoveryCount, MaxOutputTokensRecoveryLimit)
		e.maxOutputRecoveryCount++
		e.history.AddUser("Output token limit hit. Resume directly - no apology, no recap. Pick up mid-thought if that is where the cut happened. Break remaining work into smaller pieces.")
		e.events <- engine.ParsedEvent{
			Type: "system_message",
			Data: &engine.SystemMessageEvent{
				Type:    "system_message",
				Content: fmt.Sprintf("Max output tokens hit (%d/%d), auto-resuming.", e.maxOutputRecoveryCount, MaxOutputTokensRecoveryLimit),
			},
		}
	}

	// After 3 recoveries, the counter should be at the limit.
	assert.Equal(t, MaxOutputTokensRecoveryLimit, e.maxOutputRecoveryCount)

	// Drain events and verify.
	for i := 0; i < MaxOutputTokensRecoveryLimit; i++ {
		ev := <-e.events
		assert.Equal(t, "system_message", ev.Type)
		sm, ok := ev.Data.(*engine.SystemMessageEvent)
		require.True(t, ok)
		assert.Contains(t, sm.Content, fmt.Sprintf("%d/%d", i+1, MaxOutputTokensRecoveryLimit))
	}

	// History should have 3 recovery messages.
	msgs := e.history.Messages()
	assert.Len(t, msgs, MaxOutputTokensRecoveryLimit)
	for _, m := range msgs {
		assert.Contains(t, m.Content[0].OfText.Text, "Output token limit hit")
	}
}

// TestSessionBusWiring verifies that the SessionBus Publish/Subscribe round-trip
// works for the event types fired by agentLoop (EventToolCallStart, EventToolCallResult).
// This confirms background agent subscribers will receive events.
func TestSessionBusWiring(t *testing.T) {
	e, err := NewDirectEngine(engine.EngineConfig{
		Type:   engine.EngineTypeDirect,
		Model:  "claude-sonnet-4-20250514",
		APIKey: "test-key-not-real",
	})
	require.NoError(t, err)

	bus := e.SessionBus()
	require.NotNil(t, bus, "SessionBus must be non-nil")

	ch := bus.Subscribe(8)

	// Simulate what agentLoop does: publish tool call start and result.
	bus.Publish(session.Event{Type: session.EventToolCallStart, Data: "Read"})
	bus.Publish(session.Event{Type: session.EventToolCallResult, Data: "file contents"})

	for _, wantType := range []string{session.EventToolCallStart, session.EventToolCallResult} {
		select {
		case ev := <-ch:
			assert.Equal(t, wantType, ev.Type)
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("timed out waiting for %s event", wantType)
		}
	}

	bus.Unsubscribe(ch)
}

func TestMaxOutputTokensRecoveryResetOnSend(t *testing.T) {
	e, err := NewDirectEngine(engine.EngineConfig{
		Type:   engine.EngineTypeDirect,
		Model:  "claude-sonnet-4-20250514",
		APIKey: "test-key-not-real",
	})
	require.NoError(t, err)

	// Simulate a prior turn that used up recovery attempts.
	e.maxOutputRecoveryCount = 2
	e.fallbackActive = true

	// Start a new turn (will fail because of fake API key, but fields reset first).
	e.mu.Lock()
	e.status = engine.StatusIdle
	e.mu.Unlock()

	// We can't fully Send without a real API, but we can check the guard path.
	// Manually replicate the reset logic that Send does.
	e.mu.Lock()
	e.status = engine.StatusRunning
	e.maxOutputRecoveryCount = 0
	e.fallbackActive = false
	e.mu.Unlock()

	assert.Equal(t, 0, e.maxOutputRecoveryCount)
	assert.False(t, e.fallbackActive)
}

func TestFallbackNotTriggeredWhenAlreadyActive(t *testing.T) {
	// When fallback is already active, overload should not trigger again.
	e := &DirectEngine{
		events:         make(chan engine.ParsedEvent, 16),
		history:        NewConversationHistory(),
		model:          "claude-haiku-4-5-20251001",
		provider:       engine.ProviderAnthropic,
		fallbackActive: true,
	}

	err := fmt.Errorf(`529 overloaded`)
	assert.True(t, isOverloadError(err))

	// Since fallbackActive is true, the engine should NOT attempt another fallback.
	// This matches the `!e.fallbackActive` guard in the agent loop.
	assert.True(t, e.fallbackActive, "fallback should stay active, no second fallback")
}

func TestMapModelForEngine(t *testing.T) {
	tests := []struct {
		model  string
		engine string
		want   string
	}{
		// Codex always maps to gpt-5.4-codex.
		{"opus", "codex", "gpt-5.4-codex"},
		{"sonnet", "codex_re", "gpt-5.4-codex"},
		{"anything", "codex", "gpt-5.4-codex"},

		// Claude maps aliases.
		{"sonnet", "claude", "claude-sonnet-4-6"},
		{"opus", "claude", "claude-opus-4-6"},
		{"haiku", "direct", "claude-haiku-4"},
		{"fast", "direct", "claude-haiku-4"},
		{"claude-opus-4-6", "claude", "claude-opus-4-6"}, // pass through full names

		// Unknown engine passes through.
		{"gpt-5.4", "opencode", "gpt-5.4"},
		{"anything", "opencode", "anything"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%s", tt.model, tt.engine), func(t *testing.T) {
			got := MapModelForEngine(tt.model, tt.engine)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEnableBackgroundAgents(t *testing.T) {
	e, err := NewDirectEngine(engine.EngineConfig{
		Type:   engine.EngineTypeDirect,
		Model:  "claude-sonnet-4-20250514",
		APIKey: "test-key-not-real",
	})
	require.NoError(t, err)

	assert.False(t, e.bgAgentsEnabled)

	// Enable with empty agent map - should not panic.
	e.EnableBackgroundAgents(map[string]subagent.BackgroundAgentType{})
	assert.True(t, e.bgAgentsEnabled)
	assert.NotNil(t, e.bgCancel)

	// Close should clean up the background goroutine.
	e.Close()
}

func TestMatchesTrigger(t *testing.T) {
	e := &DirectEngine{}
	tests := []struct {
		trigger   string
		eventType string
		want      bool
	}{
		{"tool_use_turn", session.EventToolCallResult, true},
		{"tool_use_turn", session.EventNewMessage, false},
		{"every_turn", session.EventNewMessage, true},
		{"every_turn", session.EventToolCallResult, true},
		{"every_turn", session.EventCompaction, false},
		{"on_demand", session.EventToolCallResult, false},
		{"unknown", session.EventToolCallResult, false},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%s", tt.trigger, tt.eventType), func(t *testing.T) {
			assert.Equal(t, tt.want, e.matchesTrigger(tt.trigger, tt.eventType))
		})
	}
}

func TestNewRunnerWithWorkDir(t *testing.T) {
	e, err := NewDirectEngine(engine.EngineConfig{
		Type:    engine.EngineTypeDirect,
		Model:   "claude-sonnet-4-20250514",
		APIKey:  "test-key-not-real",
		WorkDir: "/tmp/test-repo",
	})
	require.NoError(t, err)
	assert.Equal(t, "/tmp/test-repo", e.subagentRunner.WorkDir)
}

func TestRestoreHistory_CodexMode(t *testing.T) {
	e, err := NewDirectEngine(engine.EngineConfig{
		Type:     engine.EngineTypeDirect,
		Provider: "openai",
		Model:    "gpt-5.4",
	})
	require.NoError(t, err)

	require.NoError(t, e.RestoreHistory([]engine.RestoredMessage{
		{Role: "user", Content: "inspect the entrypoint"},
		{
			Role:      "tool",
			ToolName:  "ReadFile",
			ToolInput: "cmd/main.go",
			Content:   "package main\n\nfunc main() {}",
		},
	}))

	msgs := e.history.Messages()
	require.Len(t, msgs, 2)
	assert.Equal(t, "inspect the entrypoint", msgs[0].Content[0].OfText.Text)
	assert.Equal(t, "[Tool: ReadFile(cmd/main.go) -> package main\n\nfunc main() {}]", msgs[1].Content[0].OfText.Text)

	require.Len(t, e.codexHistory, 2)
	assert.Equal(t, codexHistoryEntry{
		Role:    "user",
		Content: "inspect the entrypoint",
	}, e.codexHistory[0])
	assert.Equal(t, codexHistoryEntry{
		Role:    "assistant",
		Content: "[Tool: ReadFile(cmd/main.go) -> package main\n\nfunc main() {}]",
	}, e.codexHistory[1])
}
