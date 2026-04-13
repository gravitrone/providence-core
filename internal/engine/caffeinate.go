package engine

import (
	"os/exec"
	"runtime"
	"sync"
	"time"
)

// Caffeinator prevents macOS from sleeping while the engine is actively
// streaming. It spawns "caffeinate -i" on Start and kills it after idleTimeout
// of inactivity. Safe to call on non-macOS (no-op).
type Caffeinator struct {
	mu          sync.Mutex
	cmd         *exec.Cmd
	idleTimer   *time.Timer
	idleTimeout time.Duration
}

// NewCaffeinator creates a Caffeinator with the given idle timeout.
func NewCaffeinator(idleTimeout time.Duration) *Caffeinator {
	return &Caffeinator{
		idleTimeout: idleTimeout,
	}
}

// Start spawns caffeinate if not already running. Resets the idle timer.
func (c *Caffeinator) Start() {
	if runtime.GOOS != "darwin" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Reset idle timer.
	if c.idleTimer != nil {
		c.idleTimer.Stop()
	}
	c.idleTimer = time.AfterFunc(c.idleTimeout, func() {
		c.Stop()
	})

	// Already running.
	if c.cmd != nil && c.cmd.Process != nil {
		return
	}

	cmd := exec.Command("caffeinate", "-i")
	if err := cmd.Start(); err != nil {
		return
	}
	c.cmd = cmd
}

// Ping resets the idle timer without spawning a new process.
func (c *Caffeinator) Ping() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.idleTimer != nil {
		c.idleTimer.Reset(c.idleTimeout)
	}
}

// Stop kills the caffeinate process and cleans up.
func (c *Caffeinator) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.idleTimer != nil {
		c.idleTimer.Stop()
		c.idleTimer = nil
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
		c.cmd = nil
	}
}
