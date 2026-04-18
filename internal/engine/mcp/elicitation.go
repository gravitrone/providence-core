package mcp

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"
)

// --- Elicitation types ---

// DefaultElicitationTTL is the default expiry window for unresolved elicitations.
const DefaultElicitationTTL = 5 * time.Minute

// Elicitation represents a single server-initiated user-input request that is
// pending a response from the operator or UI layer.
//
// TODO(ui): integrate PendingElicitations into the agent tab so the operator
// sees prompts inline and can respond through the TUI. Current scope exposes
// the queue + ResolveElicitation only; no UI hook is wired yet.
type Elicitation struct {
	ID         string          `json:"id"`
	ServerName string          `json:"serverName"`
	Prompt     string          `json:"prompt"`
	Schema     json.RawMessage `json:"schema,omitempty"`
	Params     json.RawMessage `json:"params,omitempty"`
	CreatedAt  time.Time       `json:"createdAt"`
	ExpiresAt  time.Time       `json:"expiresAt"`
}

// Expired reports whether the elicitation has passed its TTL boundary at now.
func (e Elicitation) Expired(now time.Time) bool {
	return !e.ExpiresAt.IsZero() && !now.Before(e.ExpiresAt)
}

// ElicitationQueue is a thread-safe store of pending server-initiated requests
// awaiting a user response. Entries are keyed by the correlation id returned
// from the server alongside the internal server name for response routing.
type ElicitationQueue struct {
	mu    sync.Mutex
	items map[string]*Elicitation
	ttl   time.Duration
	now   func() time.Time
}

// NewElicitationQueue creates an empty queue with the supplied TTL. A zero or
// negative ttl falls back to DefaultElicitationTTL so callers cannot create a
// queue whose entries never expire.
func NewElicitationQueue(ttl time.Duration) *ElicitationQueue {
	if ttl <= 0 {
		ttl = DefaultElicitationTTL
	}
	return &ElicitationQueue{
		items: make(map[string]*Elicitation),
		ttl:   ttl,
		now:   time.Now,
	}
}

// TTL returns the configured time-to-live for queue entries.
func (q *ElicitationQueue) TTL() time.Duration {
	return q.ttl
}

// Enqueue adds a new elicitation under its ID. An empty ID is rejected because
// responses cannot be routed without a correlation id.
func (q *ElicitationQueue) Enqueue(e *Elicitation) error {
	if e == nil {
		return fmt.Errorf("enqueue elicitation: nil entry")
	}
	if e.ID == "" {
		return fmt.Errorf("enqueue elicitation: empty id")
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	now := q.now()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	if e.ExpiresAt.IsZero() {
		e.ExpiresAt = now.Add(q.ttl)
	}
	q.items[e.ID] = e
	return nil
}

// Take removes and returns the elicitation for id. It returns (nil, false) if
// the id is unknown or the entry has already expired (in which case the stale
// entry is also evicted).
func (q *ElicitationQueue) Take(id string) (*Elicitation, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	entry, ok := q.items[id]
	if !ok {
		return nil, false
	}
	delete(q.items, id)
	if entry.Expired(q.now()) {
		return nil, false
	}
	return entry, true
}

// Pending returns a snapshot of live (non-expired) elicitations sorted by
// CreatedAt (oldest first). Expired entries are evicted lazily on each call so
// the queue does not accumulate stale state when the UI is absent.
func (q *ElicitationQueue) Pending() []Elicitation {
	q.mu.Lock()
	defer q.mu.Unlock()

	now := q.now()
	for id, entry := range q.items {
		if entry.Expired(now) {
			delete(q.items, id)
		}
	}

	out := make([]Elicitation, 0, len(q.items))
	for _, entry := range q.items {
		out = append(out, *entry)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

// Len returns the number of entries currently in the queue (including entries
// that would be evicted on the next Pending call).
func (q *ElicitationQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// Next returns a shallow copy of the oldest live elicitation, or (zero, false)
// when the queue is empty. The entry remains in the queue until Take or
// Resolve removes it.
func (q *ElicitationQueue) Next() (Elicitation, bool) {
	pending := q.Pending()
	if len(pending) == 0 {
		return Elicitation{}, false
	}
	return pending[0], true
}
