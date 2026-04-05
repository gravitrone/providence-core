package engine

// Engine is the interface all AI backends must implement.
type Engine interface {
	// Send sends a user message to the AI.
	Send(text string) error
	// Events returns a channel of parsed events from the AI.
	Events() <-chan ParsedEvent
	// RespondPermission responds to a permission request.
	RespondPermission(questionID, optionID string) error
	// Interrupt sends SIGINT to abort the current turn without killing the session.
	// The engine should emit a result event and remain usable for the next message.
	Interrupt()
	// Cancel aborts the current operation and kills the session.
	Cancel()
	// Close cleanly shuts down the engine.
	Close()
	// Status returns the current engine status.
	Status() SessionStatus
}

// ParsedEvent is a decoded event from an AI engine.
type ParsedEvent struct {
	Type string
	Data any
	Raw  string
	Err  error
}

// SessionStatus represents the lifecycle state of an engine session.
type SessionStatus int

const (
	StatusIdle SessionStatus = iota
	StatusConnecting
	StatusRunning
	StatusCompleted
	StatusFailed
)
