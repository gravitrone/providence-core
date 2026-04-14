package overlay

// Injector is implemented by Bridge for use in the engine's Send path.
//
// Post-ambient-rewire, the injector serves three purposes:
//   - PendingSystemReminder: returns a small system-reminder block wrapping the
//     rolling speech transcript (no longer carries OCR/AX/activity text).
//   - Screenshots / Transcript: provide the most recent observation snapshot
//     for the engine to attach as image content + transcript on each turn.
//   - MarkAttached / RingChangedSinceLastAttach: lightweight dedup so idle
//     ember ticks don't re-attach identical frames.
type Injector interface {
	PendingSystemReminder() string
	ScreenshotPNGs() [][]byte
	Transcript() string
	MarkAttached()
	RingChangedSinceLastAttach() bool
}
