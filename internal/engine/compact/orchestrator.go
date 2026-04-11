package compact

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

const (
	triggerThresholdPercent = 80
	keepRecentPercent       = 30
)

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

	mu      sync.Mutex
	phase   Phase
	done    chan struct{}
	pending pendingReplacement
	lastErr error
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

	currentTokens := o.provider.CurrentTokens()
	if currentTokens*100 < contextWindow*triggerThresholdPercent {
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
		o.resetLockedState(PhaseIdle, nil)
		o.emitPhase(PhaseIdle, nil)
		return nil
	case PhaseFailed:
		o.resetLockedState(PhaseIdle, nil)
		return err
	default:
		return nil
	}
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
		o.finishIdle()
		return
	}

	payload, cutIndex, err := o.provider.Serialize(keepRecentTokens)
	if err != nil {
		o.finishFailed(err)
		return
	}
	if strings.TrimSpace(payload) == "" || cutIndex <= 0 {
		o.finishIdle()
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
	o.mu.Unlock()

	o.emitPhase(PhaseReady, nil)
}

func (o *Orchestrator) finishIdle() {
	o.resetLockedState(PhaseIdle, nil)
	o.emitPhase(PhaseIdle, nil)
}

func (o *Orchestrator) finishFailed(err error) {
	o.mu.Lock()
	o.phase = PhaseFailed
	o.pending = pendingReplacement{}
	o.lastErr = err
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
