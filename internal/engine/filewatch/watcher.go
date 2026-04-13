package filewatch

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Event represents a file change notification.
type Event struct {
	Path string
}

// DefaultWatchFiles are the config files monitored by default.
var DefaultWatchFiles = []string{
	".env",
	".providence/config.toml",
	"CLAUDE.md",
	".mcp.json",
}

// Watcher polls key config files for mtime changes and emits events on change.
// Uses simple 5s polling instead of fsnotify to avoid cgo/platform deps.
type Watcher struct {
	dir      string
	files    []string
	events   chan Event
	mtimes   map[string]time.Time
	mu       sync.Mutex
	cancel   context.CancelFunc
	interval time.Duration
}

// New creates a watcher rooted at dir, monitoring the given relative paths.
// If files is nil, DefaultWatchFiles is used.
func New(dir string, files []string) *Watcher {
	if len(files) == 0 {
		files = DefaultWatchFiles
	}
	return &Watcher{
		dir:      dir,
		files:    files,
		events:   make(chan Event, 8),
		mtimes:   make(map[string]time.Time),
		interval: 5 * time.Second,
	}
}

// Events returns the read-only channel of file change events.
func (w *Watcher) Events() <-chan Event {
	return w.events
}

// Start begins polling in a background goroutine. Call Stop to clean up.
func (w *Watcher) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel

	// Snapshot initial mtimes so we only report changes, not initial state.
	w.snapshot()

	go w.poll(ctx)
}

// Stop halts the polling goroutine and closes the events channel.
func (w *Watcher) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
}

func (w *Watcher) snapshot() {
	w.mu.Lock()
	defer w.mu.Unlock()

	for _, f := range w.files {
		abs := filepath.Join(w.dir, f)
		info, err := os.Stat(abs)
		if err == nil {
			w.mtimes[f] = info.ModTime()
		}
	}
}

func (w *Watcher) poll(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	defer close(w.events)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.check()
		}
	}
}

func (w *Watcher) check() {
	w.mu.Lock()
	defer w.mu.Unlock()

	for _, f := range w.files {
		abs := filepath.Join(w.dir, f)
		info, err := os.Stat(abs)
		if err != nil {
			// File doesn't exist or can't stat - check if it was just deleted.
			if _, existed := w.mtimes[f]; existed {
				delete(w.mtimes, f)
				select {
				case w.events <- Event{Path: f}:
				default:
				}
			}
			continue
		}

		mtime := info.ModTime()
		prev, existed := w.mtimes[f]
		if !existed || !mtime.Equal(prev) {
			w.mtimes[f] = mtime
			if existed {
				select {
				case w.events <- Event{Path: f}:
				default:
				}
			}
		}
	}
}
