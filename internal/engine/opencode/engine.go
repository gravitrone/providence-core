package opencode

import (
	"context"
	"fmt"
	"os/exec"
	"sync"

	"github.com/gravitrone/providence-core/internal/engine"
)

// EngineTypeOpenCode is the engine type identifier for the OpenCode backend.
const EngineTypeOpenCode engine.EngineType = "opencode"

func init() {
	engine.RegisterFactory(EngineTypeOpenCode, NewOpenCodeEngine)
}

// OpenCodeEngine wraps the opencode serve subprocess, communicating via REST + SSE.
// For v1 this is a stub that registers correctly and compiles. The full subprocess
// management (port discovery, health checks, SSE event streaming) will be fleshed
// out when opencode is actually installed and available.
type OpenCodeEngine struct {
	events  chan engine.ParsedEvent
	status  engine.SessionStatus
	model   string
	cmd *exec.Cmd
	mu  sync.Mutex
}

// NewOpenCodeEngine creates a new OpenCode engine from the given config.
func NewOpenCodeEngine(cfg engine.EngineConfig) (engine.Engine, error) {
	return &OpenCodeEngine{
		events: make(chan engine.ParsedEvent, 100),
		status: engine.StatusIdle,
		model:  cfg.Model,
	}, nil
}

// Send sends a user message. Stub: returns an error until opencode serve is wired.
func (e *OpenCodeEngine) Send(text string) error {
	return fmt.Errorf("opencode engine not yet connected: send not available")
}

// Events returns the read-only event channel.
func (e *OpenCodeEngine) Events() <-chan engine.ParsedEvent {
	return e.events
}

// RespondPermission responds to a permission request. Stub: no-op.
func (e *OpenCodeEngine) RespondPermission(_, _ string) error {
	return fmt.Errorf("opencode engine does not support permission responses")
}

// Interrupt sends SIGINT to the opencode subprocess. Stub: no-op.
func (e *OpenCodeEngine) Interrupt() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cmd != nil && e.cmd.Process != nil {
		_ = e.cmd.Process.Kill()
	}
}

// Cancel aborts the current operation and kills the subprocess.
func (e *OpenCodeEngine) Cancel() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cmd != nil && e.cmd.Process != nil {
		_ = e.cmd.Process.Kill()
	}
	e.status = engine.StatusFailed
}

// Close cleanly shuts down the engine.
func (e *OpenCodeEngine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cmd != nil && e.cmd.Process != nil {
		_ = e.cmd.Process.Kill()
	}
	close(e.events)
}

// Status returns the current engine status.
func (e *OpenCodeEngine) Status() engine.SessionStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.status
}

// RestoreHistory is a no-op for the opencode backend. OpenCode manages its own
// conversation state via its REST API.
func (e *OpenCodeEngine) RestoreHistory(_ []engine.RestoredMessage) error {
	return nil
}

// TriggerCompact requests manual context compaction. Not supported by opencode.
func (e *OpenCodeEngine) TriggerCompact(_ context.Context) error {
	return fmt.Errorf("opencode engine does not support manual compaction")
}
