package permissions

import (
	"container/list"
	"sync"
	"time"
)

// --- Denial Tracking ---

// denialTrackerCap bounds how many denial records we retain in memory. The
// oldest record is evicted when the cap is exceeded (LRU by insertion order).
const denialTrackerCap = 100

// DenialRecord captures a single permission denial. It is exposed so UI
// layers can render a "recent denials" panel.
type DenialRecord struct {
	Tool      string
	Input     string
	Timestamp time.Time
	Count     int
}

// DenialTracker stores per-session denial records with a fixed LRU cap.
// It is safe for concurrent use.
type DenialTracker struct {
	mu      sync.Mutex
	order   *list.List
	byKey   map[string]*list.Element
	nowFunc func() time.Time
}

// NewDenialTracker constructs an empty tracker using time.Now as the clock.
func NewDenialTracker() *DenialTracker {
	return &DenialTracker{
		order:   list.New(),
		byKey:   make(map[string]*list.Element),
		nowFunc: time.Now,
	}
}

// Record adds or updates the denial for the tool+input pair. Repeat calls
// for the same key increment Count and refresh Timestamp; the record also
// moves to the most-recent position in the LRU.
func (d *DenialTracker) Record(tool string, input interface{}) {
	key := tool + "\x00" + extractArg(input)
	d.mu.Lock()
	defer d.mu.Unlock()
	if el, ok := d.byKey[key]; ok {
		rec := el.Value.(*DenialRecord)
		rec.Count++
		rec.Timestamp = d.nowFunc()
		d.order.MoveToFront(el)
		return
	}
	rec := &DenialRecord{
		Tool:      tool,
		Input:     extractArg(input),
		Timestamp: d.nowFunc(),
		Count:     1,
	}
	el := d.order.PushFront(rec)
	d.byKey[key] = el
	if d.order.Len() > denialTrackerCap {
		tail := d.order.Back()
		if tail != nil {
			oldest := tail.Value.(*DenialRecord)
			oldestKey := oldest.Tool + "\x00" + oldest.Input
			delete(d.byKey, oldestKey)
			d.order.Remove(tail)
		}
	}
}

// History returns a snapshot of denial records, most recent first.
func (d *DenialTracker) History() []DenialRecord {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]DenialRecord, 0, d.order.Len())
	for el := d.order.Front(); el != nil; el = el.Next() {
		rec := el.Value.(*DenialRecord)
		out = append(out, *rec)
	}
	return out
}

// Len returns the current number of tracked denials.
func (d *DenialTracker) Len() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.order.Len()
}
