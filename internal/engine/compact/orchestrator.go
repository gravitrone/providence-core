package compact

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

const (
	// AutocompactBufferTokens is the buffer below the effective window before
	// auto-compaction triggers.
	AutocompactBufferTokens = 13_000
	// ManualCompactBufferTokens is the tighter buffer used for the blocking
	// (manual) compaction limit.
	ManualCompactBufferTokens = 3_000
	// MaxOutputTokensForSummary caps how many output tokens are reserved when
	// computing the effective context window.
	MaxOutputTokensForSummary = 20_000
	// MaxConsecutiveFailures is the circuit breaker trip threshold.
	MaxConsecutiveFailures = 3

	keepRecentPercent = 30
)

// GetEffectiveContextWindow returns the usable context window after reserving
// space for the model's output tokens.
func GetEffectiveContextWindow(contextWindow, maxOutputTokens int) int {
	reserved := min(maxOutputTokens, MaxOutputTokensForSummary)
	return contextWindow - reserved
}

// GetAutoCompactThreshold returns the token count at which auto-compaction
// should trigger.
func GetAutoCompactThreshold(contextWindow, maxOutputTokens int) int {
	return GetEffectiveContextWindow(contextWindow, maxOutputTokens) - AutocompactBufferTokens
}

// GetBlockingLimit returns the token count at which a blocking compaction
// should fire (the hard ceiling before the API rejects).
func GetBlockingLimit(contextWindow, maxOutputTokens int) int {
	return GetEffectiveContextWindow(contextWindow, maxOutputTokens) - ManualCompactBufferTokens
}

// Phase describes the current compaction lifecycle state.
type Phase string

const (
	// PhaseIdle means no compaction work is pending.
	PhaseIdle Phase = "idle"
	// PhaseRunning means the async compaction request is still in flight.
	PhaseRunning Phase = "running"
	// PhaseReady means a summary is ready and waiting to be applied.
	PhaseReady Phase = "ready"
	// PhaseFailed means the last compaction attempt failed.
	PhaseFailed Phase = "failed"
)

type phaseChangeFn func(Phase, error)

type pendingReplacement struct {
	summary  string
	cutIndex int
}

// Orchestrator manages async provider compaction across turns.
type Orchestrator struct {
	provider Provider
	onPhase  phaseChangeFn

	mu                    sync.Mutex
	phase                 Phase
	done                  chan struct{}
	pending               pendingReplacement
	lastErr               error
	consecutiveFailures   int
	hasAttemptedReactive  bool
}

// New creates a compaction orchestrator for the given provider.
func New(provider Provider, onPhaseChange func(Phase, error)) *Orchestrator {
	return &Orchestrator{
		provider: provider,
		onPhase:  onPhaseChange,
		phase:    PhaseIdle,
	}
}

// TriggerIfNeeded starts async compaction when the configured threshold is met.
func (o *Orchestrator) TriggerIfNeeded(ctx context.Context) bool {
	contextWindow := o.provider.ContextWindow()
	if contextWindow <= 0 {
		return false
	}

	o.mu.Lock()
	if o.consecutiveFailures >= MaxConsecutiveFailures {
		o.mu.Unlock()
		return false
	}
	o.mu.Unlock()

	maxOutput := o.provider.MaxOutputTokens()
	threshold := GetAutoCompactThreshold(contextWindow, maxOutput)
	if o.provider.CurrentTokens() < threshold {
		return false
	}

	return o.TriggerNow(ctx)
}

// TriggerNow starts async compaction immediately, ignoring thresholds.
func (o *Orchestrator) TriggerNow(ctx context.Context) bool {
	if ctx == nil {
		ctx = context.Background()
	}

	o.mu.Lock()
	if o.phase != PhaseIdle {
		o.mu.Unlock()
		return false
	}

	done := make(chan struct{})
	o.phase = PhaseRunning
	o.done = done
	o.pending = pendingReplacement{}
	o.lastErr = nil
	o.mu.Unlock()

	o.emitPhase(PhaseRunning, nil)

	keepRecentTokens := o.provider.ContextWindow() * keepRecentPercent / 100
	go o.run(ctx, done, keepRecentTokens)

	return true
}

// WaitForPending waits for any async compaction to finish and applies a ready
// replacement before the next user turn starts.
func (o *Orchestrator) WaitForPending(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	o.mu.Lock()
	o.hasAttemptedReactive = false
	done := o.done
	o.mu.Unlock()

	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	o.mu.Lock()
	phase := o.phase
	pending := o.pending
	err := o.lastErr
	o.mu.Unlock()

	switch phase {
	case PhaseReady:
		if replaceErr := o.provider.Replace(pending.summary, pending.cutIndex); replaceErr != nil {
			o.resetLockedState(PhaseIdle, replaceErr)
			o.emitPhase(PhaseFailed, replaceErr)
			return replaceErr
		}
		o.mu.Lock()
		o.phase = PhaseIdle
		o.done = nil
		o.pending = pendingReplacement{}
		o.lastErr = nil
		o.consecutiveFailures = 0
		o.mu.Unlock()
		o.emitPhase(PhaseIdle, nil)
		return nil
	case PhaseFailed:
		o.resetLockedState(PhaseIdle, nil)
		return err
	default:
		return nil
	}
}

// TriggerReactive runs compaction synchronously in response to a prompt-too-long
// (413) API error. It is a one-shot guard: it will only attempt once per turn.
// The guard resets when WaitForPending is called at the start of the next turn.
func (o *Orchestrator) TriggerReactive(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	o.mu.Lock()
	if o.hasAttemptedReactive {
		o.mu.Unlock()
		return fmt.Errorf("reactive compaction already attempted this turn")
	}
	o.hasAttemptedReactive = true
	o.mu.Unlock()

	keepRecentTokens := o.provider.ContextWindow() * keepRecentPercent / 100

	compacted, err := o.provider.Compress(ctx, keepRecentTokens)
	if err != nil {
		return fmt.Errorf("reactive compress: %w", err)
	}
	if compacted > 0 {
		return nil
	}

	payload, cutIndex, err := o.provider.Serialize(keepRecentTokens)
	if err != nil {
		return fmt.Errorf("reactive serialize: %w", err)
	}
	if strings.TrimSpace(payload) == "" || cutIndex <= 0 {
		return fmt.Errorf("reactive compaction: nothing to compact")
	}

	summary, err := o.provider.OneShot(ctx, SystemPrompt, payload)
	if err != nil {
		return fmt.Errorf("reactive oneshot: %w", err)
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return fmt.Errorf("reactive compaction: empty summary")
	}

	if replaceErr := o.provider.Replace(summary, cutIndex); replaceErr != nil {
		return fmt.Errorf("reactive replace: %w", replaceErr)
	}
	return nil
}

// IsRunning reports whether an async compaction request is active.
func (o *Orchestrator) IsRunning() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.phase == PhaseRunning
}

func (o *Orchestrator) run(ctx context.Context, done chan struct{}, keepRecentTokens int) {
	defer close(done)

	compacted, err := o.provider.Compress(ctx, keepRecentTokens)
	if err != nil {
		o.finishFailed(err)
		return
	}
	if compacted > 0 {
		o.finishSuccess()
		return
	}

	payload, cutIndex, err := o.provider.Serialize(keepRecentTokens)
	if err != nil {
		o.finishFailed(err)
		return
	}
	if strings.TrimSpace(payload) == "" || cutIndex <= 0 {
		o.finishSuccess()
		return
	}

	summary, err := o.provider.OneShot(ctx, SystemPrompt, payload)
	if err != nil {
		o.finishFailed(err)
		return
	}

	summary = strings.TrimSpace(summary)
	if summary == "" {
		o.finishFailed(fmt.Errorf("compaction summary is empty"))
		return
	}

	o.mu.Lock()
	o.phase = PhaseReady
	o.pending = pendingReplacement{
		summary:  summary,
		cutIndex: cutIndex,
	}
	o.lastErr = nil
	o.consecutiveFailures = 0
	o.mu.Unlock()

	o.emitPhase(PhaseReady, nil)
}

func (o *Orchestrator) finishSuccess() {
	o.mu.Lock()
	o.phase = PhaseIdle
	o.done = nil
	o.pending = pendingReplacement{}
	o.lastErr = nil
	o.consecutiveFailures = 0
	o.mu.Unlock()

	o.emitPhase(PhaseIdle, nil)
}

func (o *Orchestrator) finishFailed(err error) {
	o.mu.Lock()
	o.phase = PhaseFailed
	o.pending = pendingReplacement{}
	o.lastErr = err
	o.consecutiveFailures++
	o.mu.Unlock()

	o.emitPhase(PhaseFailed, err)
}

func (o *Orchestrator) resetLockedState(phase Phase, err error) {
	o.mu.Lock()
	o.phase = phase
	o.done = nil
	o.pending = pendingReplacement{}
	o.lastErr = err
	o.mu.Unlock()
}

func (o *Orchestrator) emitPhase(phase Phase, err error) {
	if o.onPhase != nil {
		o.onPhase(phase, err)
	}
}
