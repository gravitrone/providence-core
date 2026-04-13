package codex_headless

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/gravitrone/providence-core/internal/engine/session"
)

// EngineTypeCodexHeadless is the engine type for the codex CLI wrapper.
const EngineTypeCodexHeadless engine.EngineType = "codex_headless"

func init() {
	engine.RegisterFactory(EngineTypeCodexHeadless, func(cfg engine.EngineConfig) (engine.Engine, error) {
		return NewCodexHeadlessEngine(cfg)
	})
}

// CodexHeadlessEngine wraps the codex CLI subprocess in headless mode,
// mapping its JSONL output to Providence events.
type CodexHeadlessEngine struct {
	codexPath string // resolved path to codex binary
	model     string
	workDir   string

	cmd    *exec.Cmd
	stdout *bufio.Scanner
	events chan engine.ParsedEvent
	bus    *session.Bus

	mu     sync.Mutex
	status engine.SessionStatus
}

// NewCodexHeadlessEngine creates a new codex headless engine. The codex binary
// must be installed and on PATH. If not found, returns a helpful error pointing
// users to the codex_re engine as an alternative.
func NewCodexHeadlessEngine(cfg engine.EngineConfig) (*CodexHeadlessEngine, error) {
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		return nil, fmt.Errorf(
			"codex CLI not installed (not found on PATH). "+
				"Install it or use the codex_re engine instead, "+
				"which calls the OpenAI API directly: %w", err,
		)
	}

	model := cfg.Model
	if model == "" {
		model = "gpt-5.4"
	}

	workDir := cfg.WorkDir
	if workDir == "" {
		workDir, _ = os.Getwd()
	}

	return &CodexHeadlessEngine{
		codexPath: codexPath,
		model:     model,
		workDir:   workDir,
		events:    make(chan engine.ParsedEvent, 64),
		bus:       session.NewBus(),
		status:    engine.StatusIdle,
	}, nil
}

// Send starts a codex subprocess for the given prompt and begins streaming
// events. Each call to Send spawns a new codex process (codex exec is
// single-shot, not a persistent session like claude -p).
func (e *CodexHeadlessEngine) Send(prompt string) error {
	e.mu.Lock()
	// If a previous process is still running, kill it.
	if e.cmd != nil && e.cmd.Process != nil {
		_ = e.cmd.Process.Kill()
		_ = e.cmd.Wait()
	}
	e.events = make(chan engine.ParsedEvent, 64)
	e.status = engine.StatusRunning
	e.mu.Unlock()

	args := []string{
		"exec",
		"--json",
		"--full-auto",
		"-m", e.model,
		"-C", e.workDir,
		prompt,
	}

	cmd := exec.Command(e.codexPath, args...)

	// Clean env: strip vars that could interfere with codex subprocess.
	env := os.Environ()
	filtered := make([]string, 0, len(env))
	for _, v := range env {
		if strings.HasPrefix(v, "CLAUDECODE=") {
			continue
		}
		filtered = append(filtered, v)
	}

	// Resolve login shell PATH so codex can find its dependencies.
	loginPath, err := exec.Command("/bin/zsh", "-lc", "echo $PATH").Output()
	if err == nil && len(strings.TrimSpace(string(loginPath))) > 0 {
		cleanPath := strings.TrimSpace(string(loginPath))
		newFiltered := make([]string, 0, len(filtered))
		for _, v := range filtered {
			if !strings.HasPrefix(v, "PATH=") {
				newFiltered = append(newFiltered, v)
			}
		}
		newFiltered = append(newFiltered, "PATH="+cleanPath)
		filtered = newFiltered
	}

	cmd.Env = filtered

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("codex stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start codex: %w", err)
	}

	e.mu.Lock()
	e.cmd = cmd
	e.stdout = bufio.NewScanner(stdoutPipe)
	e.mu.Unlock()

	go e.readLoop()

	return nil
}

// readLoop reads JSONL lines from the codex subprocess stdout and maps them
// to Providence events on the events channel.
func (e *CodexHeadlessEngine) readLoop() {
	defer func() {
		e.mu.Lock()
		ch := e.events
		e.mu.Unlock()
		close(ch)
	}()

	for e.stdout.Scan() {
		line := e.stdout.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		pe, err := parseCodexEvent([]byte(line))
		if err != nil {
			e.mu.Lock()
			ch := e.events
			e.mu.Unlock()
			ch <- engine.ParsedEvent{Err: fmt.Errorf("codex parse error: %w", err)}
			continue
		}

		// Skip events that have no Providence mapping (empty Type).
		if pe.Type == "" {
			continue
		}

		// Track completion/failure from result events.
		if pe.Type == "result" {
			if result, ok := pe.Data.(*engine.ResultEvent); ok {
				e.mu.Lock()
				if result.IsError {
					e.status = engine.StatusFailed
				} else {
					e.status = engine.StatusCompleted
				}
				e.mu.Unlock()
			}
		}

		e.mu.Lock()
		ch := e.events
		e.mu.Unlock()
		ch <- pe
	}

	if err := e.stdout.Err(); err != nil {
		e.mu.Lock()
		ch := e.events
		e.mu.Unlock()
		ch <- engine.ParsedEvent{Err: fmt.Errorf("codex stdout read error: %w", err)}
	}

	// Wait for process exit and update status if still running.
	if e.cmd != nil {
		_ = e.cmd.Wait()
	}

	e.mu.Lock()
	if e.status == engine.StatusRunning {
		e.status = engine.StatusCompleted
	}
	e.mu.Unlock()
}

// Events returns the read-only event channel.
func (e *CodexHeadlessEngine) Events() <-chan engine.ParsedEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.events
}

// RespondPermission is a no-op for codex headless. The codex CLI in --full-auto
// mode does not prompt for permissions.
func (e *CodexHeadlessEngine) RespondPermission(_, _ string) error {
	return nil
}

// Interrupt sends SIGINT to the codex subprocess to abort the current turn.
func (e *CodexHeadlessEngine) Interrupt() {
	e.mu.Lock()
	cmd := e.cmd
	e.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(syscall.SIGINT)
}

// Cancel sends SIGINT to the subprocess. If it hasn't exited after 5s, sends SIGKILL.
func (e *CodexHeadlessEngine) Cancel() {
	e.mu.Lock()
	cmd := e.cmd
	e.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return
	}

	_ = cmd.Process.Signal(syscall.SIGINT)

	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
	}

	e.mu.Lock()
	e.status = engine.StatusFailed
	e.mu.Unlock()
}

// Close kills any running codex subprocess.
func (e *CodexHeadlessEngine) Close() {
	e.mu.Lock()
	cmd := e.cmd
	e.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
}

// Status returns the current engine status.
func (e *CodexHeadlessEngine) Status() engine.SessionStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.status
}

// RestoreHistory is a no-op. The codex CLI is single-shot (codex exec) and does
// not support injecting prior conversation history.
func (e *CodexHeadlessEngine) RestoreHistory(_ []engine.RestoredMessage) error {
	return nil
}

// TriggerCompact is a no-op. The codex CLI manages its own context internally.
func (e *CodexHeadlessEngine) TriggerCompact(_ context.Context) error {
	return nil
}

// SessionBus returns the engine's session event bus.
func (e *CodexHeadlessEngine) SessionBus() *session.Bus {
	return e.bus
}
