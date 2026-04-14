package overlay

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// --- State ---

// State represents the overlay process lifecycle state.
type State int

const (
	// StateStopped is the initial state and the state after a clean stop.
	StateStopped State = iota
	// StateStarting is the transient state between Start() and hello exchange.
	StateStarting
	// StateRunning is the state after a successful hello/welcome handshake.
	StateRunning
	// StateStopping is the transient state while sending bye and killing the process.
	StateStopping
)

// String returns a human-readable state label.
func (s State) String() string {
	switch s {
	case StateStopped:
		return "stopped"
	case StateStarting:
		return "starting"
	case StateRunning:
		return "running"
	case StateStopping:
		return "stopping"
	default:
		return "unknown"
	}
}

// --- Config ---

// Config holds overlay manager settings, mapped from OverlayConfig in the
// top-level config package.
type Config struct {
	SocketPath  string
	BinaryPath  string
	AutoStart   bool
	ExcludeApps []string
	LogPath     string // default ~/.providence/log/overlay.log
}

// --- Manager ---

// Manager coordinates the overlay process lifecycle: spawning the subprocess,
// running the UDS server, and tracking connection state.
type Manager struct {
	config  Config
	server  *Server
	cmd     *exec.Cmd
	cmdPID  int // captured at start, safe to read after start completes
	state   State
	stateMu sync.RWMutex
	logger  *slog.Logger

	// helloAt records when the last successful hello was received.
	helloAt   time.Time
	helloMu   sync.Mutex

	// onStart is invoked once after the overlay process has sent its Hello
	// and the bridge has replied with Welcome.
	onStart func()
	// onStop is invoked once after a clean shutdown.
	onStop func()

	// emberActivatedByUs tracks whether this manager activated ember mode,
	// so onStop can perform symmetric cleanup.
	emberActivatedByUs bool
}

// NewManager creates a Manager with the given config and logger.
func NewManager(cfg Config, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		config: cfg,
		logger: logger,
	}
}

// SetCallbacks wires onStart and onStop callbacks.
// onStart is called once the overlay is running (after hello).
// onStop is called once the overlay has stopped.
func (m *Manager) SetCallbacks(onStart, onStop func()) {
	m.onStart = onStart
	m.onStop = onStop
}

// Start spawns the overlay process and waits up to 3 seconds for a hello
// exchange on the UDS. Returns an error if the binary is not found, the
// process fails to start, or the hello times out.
func (m *Manager) Start(ctx context.Context, handler ServerHandler) error {
	m.stateMu.Lock()
	if m.state != StateStopped {
		m.stateMu.Unlock()
		return fmt.Errorf("overlay: cannot start in state %s", m.state)
	}
	m.state = StateStarting
	m.stateMu.Unlock()

	// Resolve binary.
	binPath := resolveBinaryPath(m.config.BinaryPath)
	if binPath == "" {
		m.setStateSafe(StateStopped)
		return fmt.Errorf("overlay: providence-overlay not found (checked PATH, ~/.providence/bin/, sibling of main binary)")
	}

	// Wrap the handler so that OnHello always marks the hello received,
	// regardless of whether the caller is a Bridge or a test spy.
	wrapped := &helloNotifyHandler{inner: handler, manager: m}

	// Start UDS server.
	srv, err := NewServer(m.config.SocketPath, wrapped, m.logger)
	if err != nil {
		m.setStateSafe(StateStopped)
		return fmt.Errorf("overlay: start server: %w", err)
	}
	m.server = srv

	// Start accepting connections in background.
	srvCtx, srvCancel := context.WithCancel(ctx)
	go func() {
		if err := srv.Serve(srvCtx); err != nil {
			m.logger.Warn("overlay: server error", "error", err)
		}
	}()

	// Prepare log file.
	logPath := m.config.LogPath
	if logPath == "" {
		home, _ := os.UserHomeDir()
		logPath = filepath.Join(home, ".providence", "log", "overlay.log")
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0700); err != nil {
		m.logger.Warn("overlay: create log dir failed", "error", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		m.logger.Warn("overlay: open log file failed", "error", err)
		logFile = nil
	}

	// Spawn the overlay. On macOS, if an .app bundle exists we MUST launch it
	// via `open -n -a` so LaunchServices owns the process. A direct exec.Command
	// subprocess inherits the parent's TCC responsibility chain, which causes
	// ScreenCaptureKit requests to hang indefinitely when providence itself
	// doesn't have Screen Recording permission.
	args := []string{"--socket=" + srv.SocketPath()}
	appPath := findAppBundle(binPath)

	var cmd *exec.Cmd
	var spawnedPID int

	if appPath != "" {
		// open -n (new instance) -a (app path) --args ... passes the flags to
		// the launched binary. `open` itself returns as soon as the app is
		// launched; we then pgrep to find the actual PID for signaling.
		openArgs := []string{"-n", "-a", appPath, "--args"}
		openArgs = append(openArgs, args...)
		openCmd := exec.CommandContext(ctx, "open", openArgs...)
		if logFile != nil {
			openCmd.Stdout = logFile
			openCmd.Stderr = logFile
		}
		if err := openCmd.Run(); err != nil {
			srvCancel()
			_ = srv.Close()
			m.server = nil
			m.setStateSafe(StateStopped)
			return fmt.Errorf("overlay: open -a %q: %w", appPath, err)
		}
		// Locate the spawned process by socket arg.
		pid, err := findOverlayPID(srv.SocketPath(), 2*time.Second)
		if err != nil {
			srvCancel()
			_ = srv.Close()
			m.server = nil
			m.setStateSafe(StateStopped)
			return fmt.Errorf("overlay: %w", err)
		}
		spawnedPID = pid
		m.logger.Info("overlay: launched via LaunchServices", "pid", pid, "app", appPath)
	} else {
		// Loose binary fallback (no .app bundle, e.g. dev build before install).
		cmd = exec.CommandContext(ctx, binPath, args...)
		if logFile != nil {
			cmd.Stdout = logFile
			cmd.Stderr = logFile
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			srvCancel()
			_ = srv.Close()
			m.server = nil
			m.setStateSafe(StateStopped)
			return fmt.Errorf("overlay: spawn %s: %w", binPath, err)
		}
		spawnedPID = cmd.Process.Pid
		m.logger.Info("overlay: process started", "pid", spawnedPID, "binary", binPath)
	}

	m.cmd = cmd
	m.cmdPID = spawnedPID

	// Wait for the hello exchange to complete (hello is handled inside the
	// Bridge's OnHello which marks helloAt). We poll with a 3-second timeout.
	killSpawned := func() {
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		} else if spawnedPID > 0 {
			_ = syscall.Kill(spawnedPID, syscall.SIGKILL)
		}
	}
	helloTimeout := time.After(3 * time.Second)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	gotHello := false
	for !gotHello {
		select {
		case <-helloTimeout:
			killSpawned()
			srvCancel()
			_ = srv.Close()
			m.cmd = nil
			m.cmdPID = 0
			m.server = nil
			m.setStateSafe(StateStopped)
			return fmt.Errorf("overlay: timed out waiting for hello from overlay process")
		case <-ticker.C:
			m.helloMu.Lock()
			gotHello = !m.helloAt.IsZero()
			m.helloMu.Unlock()
		case <-ctx.Done():
			killSpawned()
			srvCancel()
			_ = srv.Close()
			m.cmd = nil
			m.cmdPID = 0
			m.server = nil
			m.setStateSafe(StateStopped)
			return ctx.Err()
		}
	}

	// Prevent the cancel from leaking after we're done with startup.
	_ = srvCancel // the serve goroutine will be cancelled on Stop or ctx done

	m.setStateSafe(StateRunning)
	m.logger.Info("overlay: running", "pid", m.cmdPID)

	if m.onStart != nil {
		m.onStart()
	}

	// Watch for the spawned process to exit so we can transition to stopped.
	// For direct-exec spawns cmd.Wait works; for open-a spawns we poll signal(0).
	watchedPID := spawnedPID
	go func() {
		if cmd != nil {
			_ = cmd.Wait()
		} else {
			// Poll signal(0) every 500ms until the process is gone.
			for {
				time.Sleep(500 * time.Millisecond)
				if err := syscall.Kill(watchedPID, 0); err != nil {
					break
				}
			}
		}
		m.logger.Info("overlay: process exited", "pid", watchedPID)
		if m.State() == StateRunning {
			m.setStateSafe(StateStopped)
			if m.onStop != nil {
				m.onStop()
			}
		}
	}()

	return nil
}

// MarkHello records that the hello exchange completed. Called by the Bridge's
// OnHello implementation so Start's polling loop can unblock.
func (m *Manager) MarkHello() {
	m.helloMu.Lock()
	m.helloAt = time.Now()
	m.helloMu.Unlock()
}

// Stop sends a bye envelope, waits up to 2 seconds for the process to exit,
// then sends SIGKILL if it hasn't terminated.
func (m *Manager) Stop(ctx context.Context) error {
	m.stateMu.Lock()
	state := m.state
	if state == StateStopped {
		m.stateMu.Unlock()
		return nil // idempotent: already stopped, no error
	}
	if state != StateRunning {
		m.stateMu.Unlock()
		return fmt.Errorf("overlay: cannot stop in state %s", state)
	}
	m.state = StateStopping
	m.stateMu.Unlock()

	// Send bye.
	if m.server != nil {
		_ = m.server.Broadcast(TypeBye, struct{}{})
	}

	// SIGTERM with 2-second grace. Two code paths depending on spawn mode:
	// direct exec (cmd set) vs open-a (only cmdPID set).
	if m.cmd != nil && m.cmd.Process != nil {
		_ = m.cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan struct{})
		go func() {
			_ = m.cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			m.logger.Warn("overlay: SIGTERM grace expired, sending SIGKILL")
			_ = m.cmd.Process.Kill()
			<-done
		case <-ctx.Done():
			_ = m.cmd.Process.Kill()
			<-done
		}
	} else if m.cmdPID > 0 {
		pid := m.cmdPID
		_ = syscall.Kill(pid, syscall.SIGTERM)
		deadline := time.Now().Add(2 * time.Second)
		for {
			if time.Now().After(deadline) {
				m.logger.Warn("overlay: SIGTERM grace expired, sending SIGKILL")
				_ = syscall.Kill(pid, syscall.SIGKILL)
				break
			}
			if err := syscall.Kill(pid, 0); err != nil {
				break // process is gone
			}
			select {
			case <-ctx.Done():
				_ = syscall.Kill(pid, syscall.SIGKILL)
				deadline = time.Now()
			case <-time.After(100 * time.Millisecond):
			}
		}
	}

	if m.server != nil {
		_ = m.server.Close()
		m.server = nil
	}

	m.cmd = nil
	m.cmdPID = 0
	m.setStateSafe(StateStopped)

	m.logger.Info("overlay: stopped")
	if m.onStop != nil {
		m.onStop()
	}
	return nil
}

// State returns the current lifecycle state.
func (m *Manager) State() State {
	m.stateMu.RLock()
	defer m.stateMu.RUnlock()
	return m.state
}

// StatusInfo returns a map of observable state suitable for /overlay status output.
func (m *Manager) StatusInfo() map[string]any {
	m.stateMu.RLock()
	state := m.state
	m.stateMu.RUnlock()

	m.helloMu.Lock()
	helloAt := m.helloAt
	m.helloMu.Unlock()

	info := map[string]any{
		"state":       state.String(),
		"socket_path": m.config.SocketPath,
		"binary_path": resolveBinaryPath(m.config.BinaryPath),
		"pid":         m.cmdPID,
	}

	if !helloAt.IsZero() {
		info["last_hello_age"] = time.Since(helloAt).Round(time.Second).String()
	}

	if m.server != nil {
		info["connected_clients"] = m.server.ConnectedCount()
	} else {
		info["connected_clients"] = 0
	}

	return info
}

// Server returns the running UDS server, or nil if not started.
func (m *Manager) Server() *Server { return m.server }

// setStateSafe sets the state under the write lock. Used when the caller does
// NOT already hold stateMu.
func (m *Manager) setStateSafe(s State) {
	m.stateMu.Lock()
	m.state = s
	m.stateMu.Unlock()
}

// --- Binary Resolution ---

// resolveBinaryPath finds the providence-overlay binary.
// Search order: explicit override -> PATH -> ~/.providence/bin/ -> sibling of
// this executable.
func resolveBinaryPath(override string) string {
	if override != "" {
		return override
	}

	// PATH lookup.
	if p, err := exec.LookPath("providence-overlay"); err == nil {
		return p
	}

	// ~/.providence/bin/
	if home, err := os.UserHomeDir(); err == nil {
		candidate := filepath.Join(home, ".providence", "bin", "providence-overlay")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	// Sibling of this binary (useful when providence-overlay ships alongside providence).
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "providence-overlay")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	return ""
}

// --- .app bundle detection ---

// findAppBundle returns the absolute path to a Providence Overlay .app bundle
// associated with binPath, or "" if none. Two cases considered:
//  1. binPath already points inside an .app (.../Foo.app/Contents/MacOS/bin).
//  2. binPath is a shim script that execs into the .app - parse for the .app path.
//
// No conventional-location fallback: callers with a real working binPath that
// isn't inside an .app should use direct exec. This keeps test doubles working.
func findAppBundle(binPath string) string {
	// Case 1: binPath is inside an .app.
	if idx := strings.Index(binPath, ".app/Contents/MacOS/"); idx != -1 {
		return binPath[:idx+len(".app")]
	}

	// Case 2: shim script - read + look for a quoted .app path reference.
	// Only scan back for quote chars (not whitespace) because the .app's
	// display name may contain spaces ("Providence Overlay.app").
	if data, err := os.ReadFile(binPath); err == nil {
		content := string(data)
		if idx := strings.Index(content, ".app/Contents/MacOS/"); idx != -1 {
			start := strings.LastIndexAny(content[:idx], "\"'")
			if start >= 0 {
				candidate := content[start+1 : idx+len(".app")]
				if strings.Contains(candidate, "$HOME") {
					if home, err := os.UserHomeDir(); err == nil {
						candidate = strings.ReplaceAll(candidate, "$HOME", home)
					}
				}
				if _, err := os.Stat(candidate); err == nil {
					return candidate
				}
			}
		}
	}

	return ""
}

// findOverlayPID locates the PID of an overlay process launched with the given
// socket argument. Polls pgrep up to the timeout since open(1) returns before
// the app is fully up.
func findOverlayPID(socketPath string, timeout time.Duration) (int, error) {
	deadline := time.Now().Add(timeout)
	pattern := "providence-overlay.*--socket=" + socketPath
	for time.Now().Before(deadline) {
		out, err := exec.Command("pgrep", "-f", pattern).Output()
		if err == nil {
			fields := strings.Fields(strings.TrimSpace(string(out)))
			// pgrep may return multiple matches (open's own process, shim, app) -
			// take the last/newest which is most likely the actual app process.
			for i := len(fields) - 1; i >= 0; i-- {
				if pid, perr := strconv.Atoi(fields[i]); perr == nil && pid > 0 {
					return pid, nil
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return 0, fmt.Errorf("could not locate spawned overlay process (socket=%s)", socketPath)
}

// --- helloNotifyHandler ---

// helloNotifyHandler wraps a ServerHandler and calls manager.MarkHello() on
// the first OnHello, ensuring the Start polling loop always unblocks regardless
// of whether the caller is a Bridge or a test spy.
type helloNotifyHandler struct {
	inner   ServerHandler
	manager *Manager
}

func (h *helloNotifyHandler) OnHello(c *client, hello Hello) Welcome {
	h.manager.MarkHello()
	return h.inner.OnHello(c, hello)
}
func (h *helloNotifyHandler) OnContextUpdate(c *client, u ContextUpdate) error {
	return h.inner.OnContextUpdate(c, u)
}
func (h *helloNotifyHandler) OnUserQuery(c *client, q UserQuery) error {
	return h.inner.OnUserQuery(c, q)
}
func (h *helloNotifyHandler) OnEmberRequest(c *client, r EmberRequest) error {
	return h.inner.OnEmberRequest(c, r)
}
func (h *helloNotifyHandler) OnInterrupt(c *client) error { return h.inner.OnInterrupt(c) }
func (h *helloNotifyHandler) OnUIEvent(c *client, e UIEvent) error {
	return h.inner.OnUIEvent(c, e)
}
func (h *helloNotifyHandler) OnDisconnect(c *client) { h.inner.OnDisconnect(c) }
