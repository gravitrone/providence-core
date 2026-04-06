package tools

import (
	"os"
	"sync"
	"time"
)

// FileState tracks which files have been read and their mtime at read-time.
// Used for stale-write detection: a tool can check whether a file changed
// since the last time the model read it.
type FileState struct {
	mu    sync.RWMutex
	state map[string]time.Time
}

// NewFileState creates a new empty file state tracker.
func NewFileState() *FileState {
	return &FileState{state: make(map[string]time.Time)}
}

// MarkRead records the current mtime of path. If the file cannot be stat'd
// (e.g. permission denied), the zero time is stored so HasBeenRead still
// returns true.
func (fs *FileState) MarkRead(path string) {
	info, err := os.Stat(path)
	var mtime time.Time
	if err == nil {
		mtime = info.ModTime()
	}

	fs.mu.Lock()
	fs.state[path] = mtime
	fs.mu.Unlock()
}

// HasBeenRead returns true if the file was read at least once in this session.
func (fs *FileState) HasBeenRead(path string) bool {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	_, ok := fs.state[path]
	return ok
}

// CheckStale returns true if the file's current mtime differs from the mtime
// recorded at the last MarkRead call. Returns false if the file was never read.
func (fs *FileState) CheckStale(path string) bool {
	fs.mu.RLock()
	readTime, ok := fs.state[path]
	fs.mu.RUnlock()
	if !ok {
		return false
	}

	info, err := os.Stat(path)
	if err != nil {
		// file deleted or inaccessible since read - that's stale
		return true
	}
	return !info.ModTime().Equal(readTime)
}
