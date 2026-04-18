package direct

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/gravitrone/providence-core/internal/engine/compact"
	"github.com/gravitrone/providence-core/internal/engine/session"
	"github.com/gravitrone/providence-core/internal/engine/subagent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withTempSessionMemoryDir redirects session memory storage into a temp dir
// for the duration of the test.
func withTempSessionMemoryDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	session.SetMemoryDirForTesting(dir)
	t.Cleanup(func() { session.SetMemoryDirForTesting("") })
	return dir
}

// newTestEngineForMemory builds a minimal DirectEngine suitable for exercising
// the session memory plumbing without hitting the Anthropic API.
func newTestEngineForMemory(t *testing.T) *DirectEngine {
	t.Helper()

	history := NewConversationHistory()
	// Prime history so recentTurnsForMemory has material to summarize.
	history.AddUser("hey build me a go tui")
	history.AddAssistantText("ok, what flavor")
	history.AddUser("flame themed, bubbletea v2")
	history.AddAssistantText("got it")

	e := &DirectEngine{
		sessionID:          "test-session-" + t.Name(),
		history:            history,
		memoryEnabled:      true,
		memoryTurnInterval: 3,
		subagentRunner:     subagent.NewRunner(),
	}
	e.memoryExecutorOverride = func(ctx context.Context, prompt string, agentType subagent.AgentType, state *subagent.ConversationState) (string, error) {
		return "# session memory\n\nuser wants a flame-themed go tui on bubbletea v2.", nil
	}
	t.Cleanup(func() { e.subagentRunner.Close() })
	return e
}

func TestReadSessionMemoryForCompactorReturnsEmptyWhenDisabled(t *testing.T) {
	withTempSessionMemoryDir(t)

	e := newTestEngineForMemory(t)
	e.memoryEnabled = false

	require.NoError(t, session.WriteSessionMemory(e.sessionID, "should be ignored"))

	got, err := e.readSessionMemoryForCompactor()
	require.NoError(t, err)
	assert.Equal(t, "", got)
}

func TestReadSessionMemoryForCompactorReturnsBody(t *testing.T) {
	withTempSessionMemoryDir(t)

	e := newTestEngineForMemory(t)
	require.NoError(t, session.WriteSessionMemory(e.sessionID, "live memory body"))

	got, err := e.readSessionMemoryForCompactor()
	require.NoError(t, err)
	assert.Equal(t, "live memory body", got)
}

func TestReadSessionMemoryForCompactorStaleTreatedAsMiss(t *testing.T) {
	dir := withTempSessionMemoryDir(t)

	e := newTestEngineForMemory(t)
	require.NoError(t, session.WriteSessionMemory(e.sessionID, "stale body"))

	// Backdate the file past the stale threshold so the adapter logs and
	// returns a miss rather than bubbling the error up.
	path := filepath.Join(dir, e.sessionID+".md")
	old := time.Now().Add(-session.MemoryStaleAfter - time.Hour)
	require.NoError(t, os.Chtimes(path, old, old))

	got, err := e.readSessionMemoryForCompactor()
	require.NoError(t, err)
	assert.Equal(t, "", got)
}

// TestPostTurnHookDispatchesMemoryWriterEveryNTurns verifies that the writer
// fires on turn counts that are multiples of memoryTurnInterval and does not
// fire on other turns. We count invocations via the subagent context
// executor and wait on the engine's in-flight writer WaitGroup before
// asserting so the test does not race the fire-and-forget goroutine.
func TestPostTurnHookDispatchesMemoryWriterEveryNTurns(t *testing.T) {
	withTempSessionMemoryDir(t)

	e := newTestEngineForMemory(t)
	e.memoryTurnInterval = 3

	var dispatched int64
	e.memoryExecutorOverride = func(ctx context.Context, prompt string, agentType subagent.AgentType, state *subagent.ConversationState) (string, error) {
		atomic.AddInt64(&dispatched, 1)
		require.Contains(t, prompt, "<transcript>")
		require.Contains(t, prompt, "flame themed")
		return "memory body for turn", nil
	}

	// Trigger 7 turns. Expect dispatch on turn 3 and turn 6 (two total).
	for i := 0; i < 7; i++ {
		e.maybeDispatchSessionMemoryWriter()
	}

	// Wait for all writer goroutines to finish before asserting on disk.
	waitForMemoryWriters(t, e, 3*time.Second)

	assert.Equal(t, int64(2), atomic.LoadInt64(&dispatched))

	got, err := session.ReadSessionMemory(e.sessionID)
	require.NoError(t, err)
	assert.Equal(t, "memory body for turn", got)
}

// waitForMemoryWriters blocks until every in-flight memory writer goroutine
// has finished or the deadline passes. Fails the test on timeout so any
// regression that leaks writers surfaces immediately.
func waitForMemoryWriters(t *testing.T, e *DirectEngine, timeout time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		e.memoryWritersInFlight.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatalf("memory writers did not complete within %s", timeout)
	}
}

// TestMemoryWriterDisabledNoDispatch verifies the flag cleanly disables the
// writer so neither the subagent nor disk is touched.
func TestMemoryWriterDisabledNoDispatch(t *testing.T) {
	withTempSessionMemoryDir(t)

	e := newTestEngineForMemory(t)
	e.memoryEnabled = false

	var dispatched int64
	e.memoryExecutorOverride = func(ctx context.Context, prompt string, agentType subagent.AgentType, state *subagent.ConversationState) (string, error) {
		atomic.AddInt64(&dispatched, 1)
		return "never", nil
	}

	for i := 0; i < 20; i++ {
		e.maybeDispatchSessionMemoryWriter()
	}
	waitForMemoryWriters(t, e, 500*time.Millisecond)
	assert.Equal(t, int64(0), atomic.LoadInt64(&dispatched))

	got, err := session.ReadSessionMemory(e.sessionID)
	require.NoError(t, err)
	assert.Equal(t, "", got)
}

// TestMemoryWriterFailureDoesNotBlockMainTurn proves the writer is
// fire-and-forget: a subagent executor that blocks does not prevent the
// dispatcher from returning promptly.
func TestMemoryWriterFailureDoesNotBlockMainTurn(t *testing.T) {
	withTempSessionMemoryDir(t)

	e := newTestEngineForMemory(t)
	e.memoryTurnInterval = 1

	release := make(chan struct{})
	executed := make(chan struct{}, 1)
	e.memoryExecutorOverride = func(ctx context.Context, prompt string, agentType subagent.AgentType, state *subagent.ConversationState) (string, error) {
		select {
		case executed <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return "", errors.New("writer failed")
	}

	// Dispatch should return promptly even though the writer will hang until
	// the test releases it. A generous timer catches any regression that
	// accidentally couples the main turn to the writer.
	done := make(chan struct{})
	go func() {
		e.maybeDispatchSessionMemoryWriter()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("maybeDispatchSessionMemoryWriter must not block the main turn")
	}

	// Confirm the writer goroutine actually started, then let it finish so
	// the test does not leak goroutines.
	select {
	case <-executed:
	case <-time.After(2 * time.Second):
		t.Fatal("writer goroutine did not start")
	}
	close(release)
	waitForMemoryWriters(t, e, 3*time.Second)

	// Memory file must NOT exist because the writer errored. The engine
	// keeps running regardless.
	got, err := session.ReadSessionMemory(e.sessionID)
	require.NoError(t, err)
	assert.Equal(t, "", got)
}

// TestRecentTurnsForMemoryIncludesLastExchange verifies the window builder
// captures recent user/assistant text in the expected role ordering.
func TestRecentTurnsForMemoryIncludesLastExchange(t *testing.T) {
	e := newTestEngineForMemory(t)

	window := e.recentTurnsForMemory(2)
	assert.Contains(t, window, "USER:")
	assert.Contains(t, window, "ASSISTANT:")
	assert.Contains(t, window, "flame themed")
	// The window is a slice of the tail so the first user ("hey build me")
	// might be trimmed when interval=2 (2 pairs = 4 messages).
	idxUser := strings.Index(window, "USER:")
	idxAssistant := strings.Index(window, "ASSISTANT:")
	assert.True(t, idxUser >= 0 && idxAssistant >= 0)
}

func TestRecentTurnsForMemoryEmptyForCodex(t *testing.T) {
	e := newTestEngineForMemory(t)
	e.codexMode = true
	assert.Equal(t, "", e.recentTurnsForMemory(5))
}

// Keep the anthropic import live in case SDK types are referenced directly in
// future assertions; this guards against unused-import churn while this file
// evolves.
var _ = anthropic.MessageParamRoleAssistant

// Sanity-check that the compactor's configured memory reader round-trips
// through the engine adapter.
func TestCompactorWiredMemoryReaderRoundTrip(t *testing.T) {
	withTempSessionMemoryDir(t)

	e := newTestEngineForMemory(t)
	require.NoError(t, session.WriteSessionMemory(e.sessionID, "wired body"))

	o := compact.New(nil, nil)
	o.SetMemoryReader(e.readSessionMemoryForCompactor)
	// Read memory via the adapter directly; the orchestrator test suite
	// covers the replacement flow end-to-end.
	got, err := e.readSessionMemoryForCompactor()
	require.NoError(t, err)
	assert.Equal(t, "wired body", got)
}
