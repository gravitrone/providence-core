package direct

import (
	"context"
	"testing"
	"time"

	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/gravitrone/providence-core/internal/engine/compact"
	"github.com/gravitrone/providence-core/internal/engine/direct/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubCompactProvider struct {
	currentTokens int
}

func (s stubCompactProvider) Compress(context.Context, int) (int, error) {
	return 0, nil
}

func (s stubCompactProvider) Serialize(int) (string, int, error) {
	return "", 0, nil
}

func (s stubCompactProvider) Replace(string, int) error {
	return nil
}

func (s stubCompactProvider) OneShot(context.Context, string, string) (string, error) {
	return "", nil
}

func (s stubCompactProvider) CurrentTokens() int {
	return s.currentTokens
}

func (s stubCompactProvider) ContextWindow() int {
	return 0
}

func (s stubCompactProvider) MaxOutputTokens() int {
	return 0
}

func TestRunPostCompactCleanupResetsCaches(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	e := &DirectEngine{
		history:             NewConversationHistory(),
		status:              engine.StatusCompleted,
		ctx:                 ctx,
		cancel:              cancel,
		preExecResults:      map[string]tools.ToolResult{"tool_1": {Content: "cached"}},
		contentReplacements: map[string]string{"tool_1": "original"},
	}
	e.history.AddUser("keep estimating tokens")
	e.history.SetReportedTokens(12, 8)
	e.loopHistory[0] = "Read:{}"
	e.loopIdx = 1
	e.loopFillCount = 1

	before := time.Now()
	compact.RunPostCompactCleanup(e.postCompactCleanupState())

	require.ErrorIs(t, ctx.Err(), context.Canceled)
	assert.Zero(t, e.history.lastInputTokens)
	assert.Zero(t, e.history.lastOutputTokens)
	assert.Zero(t, e.history.lastReportedTokens)
	assert.Empty(t, e.preExecResults)
	assert.Empty(t, e.contentReplacements)
	assert.Equal(t, [5]string{}, e.loopHistory)
	assert.Zero(t, e.loopIdx)
	assert.Zero(t, e.loopFillCount)
	assert.False(t, e.lastCompactedAt.IsZero())
	assert.False(t, e.lastCompactedAt.Before(before))
}

func TestCompactFlowInvokesPostCleanup(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	e := &DirectEngine{
		events:              make(chan engine.ParsedEvent, 1),
		history:             NewConversationHistory(),
		status:              engine.StatusCompleted,
		ctx:                 ctx,
		cancel:              cancel,
		preExecResults:      map[string]tools.ToolResult{"tool_1": {Content: "cached"}},
		contentReplacements: map[string]string{"tool_1": "original"},
	}
	e.history.AddUser("compact this history")
	e.history.SetReportedTokens(20, 10)

	e.handleCompactionPhase(stubCompactProvider{currentTokens: 7}, compact.PhaseIdle, nil)

	require.ErrorIs(t, ctx.Err(), context.Canceled)
	assert.Zero(t, e.history.lastReportedTokens)
	assert.Empty(t, e.preExecResults)
	assert.Empty(t, e.contentReplacements)
	assert.False(t, e.lastCompactedAt.IsZero())
}
