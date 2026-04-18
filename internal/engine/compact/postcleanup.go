package compact

import "time"

// PostCleanupState collects the engine-owned cleanup hooks that should run
// after compaction succeeds.
type PostCleanupState struct {
	ResetReportedTokens  func()
	ResetToolResultCache func()
	CancelTurnContext    func()
	MarkCompactedAt      func(time.Time)
	ResetInMemoryState   func()
}

// RunPostCompactCleanup resets engine state that still points at pre-compact
// history.
func RunPostCompactCleanup(state PostCleanupState) {
	now := time.Now()

	if state.ResetReportedTokens != nil {
		state.ResetReportedTokens()
	}
	if state.ResetToolResultCache != nil {
		state.ResetToolResultCache()
	}
	if state.CancelTurnContext != nil {
		state.CancelTurnContext()
	}
	if state.MarkCompactedAt != nil {
		state.MarkCompactedAt(now)
	}
	if state.ResetInMemoryState != nil {
		state.ResetInMemoryState()
	}
}
