package session

import "sync"

// --- Events ---

// Event types for the session event bus.
const (
	EventNewMessage     = "new_message"
	EventToolCallStart  = "tool_call_start"
	EventToolCallResult = "tool_call_result"
	EventCompaction     = "compaction"
	EventHarnessSwitch  = "harness_switch"
)

// Event is a typed payload broadcast to all subscribers.
type Event struct {
	Type string
	Data any
}

// --- Bus ---

// Bus is a fan-out event broadcaster for background agents.
// Subscribers receive events on buffered channels. Slow subscribers
// are dropped (non-blocking publish) to avoid stalling the main loop.
type Bus struct {
	subscribers []chan Event
	mu          sync.RWMutex
}

// NewBus creates an empty event bus.
func NewBus() *Bus { return &Bus{} }

// Publish sends ev to every subscriber. If a subscriber's channel is
// full the event is silently dropped.
func (b *Bus) Publish(ev Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subscribers {
		select {
		case ch <- ev:
		default:
		}
	}
}

// Subscribe registers a new listener and returns its receive channel.
// bufSize controls backpressure - larger buffers tolerate slower readers.
func (b *Bus) Subscribe(bufSize int) <-chan Event {
	if bufSize < 1 {
		bufSize = 1
	}
	ch := make(chan Event, bufSize)
	b.mu.Lock()
	b.subscribers = append(b.subscribers, ch)
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a previously-subscribed channel and closes it.
func (b *Bus) Unsubscribe(ch <-chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, sub := range b.subscribers {
		if sub == ch {
			b.subscribers = append(b.subscribers[:i], b.subscribers[i+1:]...)
			close(sub)
			return
		}
	}
}
