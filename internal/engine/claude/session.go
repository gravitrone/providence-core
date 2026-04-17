package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gravitrone/providence-core/internal/engine"
)

func init() {
	engine.RegisterFactory(engine.EngineTypeClaude, func(cfg engine.EngineConfig) (engine.Engine, error) {
		return NewSession(cfg.SystemPrompt, cfg.AllowedTools, cfg.Model)
	})
}

// Session manages a single headless Claude Code subprocess.
type Session struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    *bufio.Scanner
	stderr    io.ReadCloser
	sessionID string
	status    engine.SessionStatus
	events    chan engine.ParsedEvent
	mu        sync.Mutex
	closeOnce sync.Once
}

// NewSession spawns a Claude Code subprocess in headless mode.
// systemPrompt replaces the default system prompt entirely.
// allowedTools is a comma-separated list of pre-approved tools (can be empty).
// model is an optional model name (e.g. "haiku", "sonnet", "opus"); empty uses default.
func NewSession(systemPrompt string, allowedTools []string, model string) (*Session, error) {
	args := []string{
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
	}

	if model != "" {
		args = append(args, "--model", model)
	}

	if systemPrompt != "" {
		args = append(args, "--system-prompt", systemPrompt)
	}

	if len(allowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(allowedTools, ","))
	}

	cmd := exec.Command("claude", args...)

	// Clean env for subprocess:
	// - CLAUDECODE interferes with subprocess spawning.
	// - ANTHROPIC_API_KEY forces API billing instead of OAuth.
	// - Login shell PATH ensures claude binary is found.
	env := os.Environ()
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		if strings.HasPrefix(e, "CLAUDECODE=") ||
			strings.HasPrefix(e, "ANTHROPIC_API_KEY=") {
			continue
		}
		filtered = append(filtered, e)
	}

	// Get login shell PATH for clean env resolution.
	loginPath, err := exec.Command("/bin/zsh", "-lc", "echo $PATH").Output()
	if err == nil && len(strings.TrimSpace(string(loginPath))) > 0 {
		cleanPath := strings.TrimSpace(string(loginPath))
		newFiltered := make([]string, 0, len(filtered))
		for _, e := range filtered {
			if !strings.HasPrefix(e, "PATH=") {
				newFiltered = append(newFiltered, e)
			}
		}
		newFiltered = append(newFiltered, "PATH="+cleanPath)
		filtered = newFiltered
	}

	cmd.Env = filtered

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start claude: %w", err)
	}

	go io.Copy(io.Discard, stderr)

	s := &Session{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewScanner(stdoutPipe),
		stderr: stderr,
		status: engine.StatusConnecting,
		events: make(chan engine.ParsedEvent, 64),
	}

	go s.readLoop()

	return s, nil
}

// readLoop reads NDJSON lines from stdout and sends ParsedEvents to the channel.
func (s *Session) readLoop() {
	defer close(s.events)

	for s.stdout.Scan() {
		line := s.stdout.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		eventType, data, err := ParseEvent([]byte(line))

		pe := engine.ParsedEvent{
			Type: eventType,
			Data: data,
			Raw:  line,
			Err:  err,
		}

		// Track session ID from init event.
		if err == nil && eventType == "system" {
			if init, ok := data.(*engine.SystemInitEvent); ok && init.Subtype == "init" {
				s.mu.Lock()
				s.sessionID = init.SessionID
				s.status = engine.StatusRunning
				s.mu.Unlock()
			}
		}

		// Track completion/failure from result event.
		if err == nil && eventType == "result" {
			if result, ok := data.(*engine.ResultEvent); ok {
				s.mu.Lock()
				if result.IsError || result.Subtype == "error" {
					s.status = engine.StatusFailed
				} else {
					s.status = engine.StatusCompleted
				}
				s.mu.Unlock()
			}
		}

		s.events <- pe
	}

	if err := s.stdout.Err(); err != nil {
		s.events <- engine.ParsedEvent{Err: fmt.Errorf("stdout read error: %w", err)}
	}

	s.mu.Lock()
	if s.status == engine.StatusRunning || s.status == engine.StatusConnecting {
		s.status = engine.StatusFailed
	}
	s.mu.Unlock()
}

// Send writes a user message to Claude's stdin.
func (s *Session) Send(text string) error {
	msg := UserMessage{
		Type: "user",
		Message: engine.MessageBody{
			Role: "user",
			Content: []engine.ContentPart{
				{Type: "text", Text: text},
			},
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	data = append(data, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err = s.stdin.Write(data)
	return err
}

// Events returns the read-only event channel.
func (s *Session) Events() <-chan engine.ParsedEvent {
	return s.events
}

// RespondPermission sends a permission response to Claude's stdin.
func (s *Session) RespondPermission(questionID, optionID string) error {
	resp := PermissionResponse{
		Type:       "permission_response",
		QuestionID: questionID,
		OptionID:   optionID,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal permission response: %w", err)
	}

	data = append(data, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err = s.stdin.Write(data)
	return err
}

// Interrupt sends SIGINT to the subprocess to abort the current turn.
// Unlike Cancel, it does NOT wait for the process to exit or kill it.
// The subprocess should emit a result event and remain alive for the next message.
func (s *Session) Interrupt() {
	if s.cmd.Process == nil {
		return
	}
	_ = s.cmd.Process.Signal(syscall.SIGINT)
}

// Cancel sends SIGINT to the subprocess. If it hasn't exited after 5s, sends SIGKILL.
func (s *Session) Cancel() {
	if s.cmd.Process == nil {
		return
	}

	_ = s.cmd.Process.Signal(syscall.SIGINT)

	done := make(chan struct{})
	go func() {
		_ = s.cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = s.cmd.Process.Kill()
	}

	s.mu.Lock()
	s.status = engine.StatusFailed
	s.mu.Unlock()
}

// Close closes stdin, terminates the subprocess, and waits for it to exit.
func (s *Session) Close() {
	s.closeOnce.Do(func() {
		if s.stdin != nil {
			_ = s.stdin.Close()
		}

		if s.cmd == nil || s.cmd.Process == nil || s.cmd.ProcessState != nil {
			return
		}

		process := s.cmd.Process
		_ = process.Signal(syscall.SIGTERM)

		killTimer := time.AfterFunc(5*time.Second, func() {
			_ = process.Kill()
		})
		defer killTimer.Stop()

		_ = s.cmd.Wait()
	})
}

// Status returns the current session status.
func (s *Session) Status() engine.SessionStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// TriggerCompact sends a /compact command to the claude subprocess via NDJSON
// stdin, requesting manual context compaction. This makes *Session satisfy
// engine.Compactor; callers feature-detect before invoking.
func (s *Session) TriggerCompact(_ context.Context) error {
	return s.sendJSON(map[string]any{
		"type": "user",
		"message": map[string]string{
			"role":    "user",
			"content": "/compact",
		},
	})
}

// sendJSON marshals v as JSON and writes a newline-terminated line to stdin.
func (s *Session) sendJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	data = append(data, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err = s.stdin.Write(data)
	return err
}
