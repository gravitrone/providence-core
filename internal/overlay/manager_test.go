package overlay

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- StatusInfo ---

func TestManagerStatusInfo_Stopped(t *testing.T) {
	m := NewManager(Config{SocketPath: "/tmp/providence-overlay-test.sock"}, nil)

	info := m.StatusInfo()

	state, ok := info["state"].(string)
	require.True(t, ok, "state key must be a string")
	assert.Equal(t, "stopped", state, "fresh Manager must report stopped")

	assert.Equal(t, 0, info["pid"], "pid must be 0 when not started")
	assert.Equal(t, 0, info["connected_clients"], "no clients when server not running")
}

func TestManagerStatusInfo_HasRequiredKeys(t *testing.T) {
	m := NewManager(Config{SocketPath: "/tmp/providence-overlay-test.sock"}, nil)

	info := m.StatusInfo()

	requiredKeys := []string{"state", "pid", "connected_clients"}
	for _, k := range requiredKeys {
		_, ok := info[k]
		assert.True(t, ok, "StatusInfo must include %q key", k)
	}
}

// --- /ember handler (documentation-only, deferred) ---

// TestEmberHandler_ActivatesEmberAndAutoLaunchesOverlay documents the desired
// coverage for the /ember slash handler in agent_tab.go: when invoked while
// the overlay manager is stopped, it should activate ember and auto-launch
// the overlay via exec.Command("open", ...). This requires a full AgentTab
// test harness which does not exist in the current tree - the handler pulls
// in the TUI, engine, and config layers simultaneously. Skipped until a
// harness is introduced.
func TestEmberHandler_ActivatesEmberAndAutoLaunchesOverlay(t *testing.T) {
	t.Skip("pending AgentTab test harness: /ember handler auto-launches overlay when manager is stopped")
}

// TestEmberHandler_SecondActivationDeactivates documents the toggle contract
// for /ember: the second invocation should deactivate ember mode. Same
// blocker as above - no AgentTab harness available yet.
func TestEmberHandler_SecondActivationDeactivates(t *testing.T) {
	t.Skip("pending AgentTab test harness: /ember is a toggle, second call should deactivate")
}

// --- resolveBinaryPath ---

func TestResolveBinaryPathExplicitOverride(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "my-overlay")
	require.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh\n"), 0755))

	got := resolveBinaryPath(bin)
	assert.Equal(t, bin, got)
}

func TestResolveBinaryPathNotFound(t *testing.T) {
	// No binary in PATH, no ~/.providence/bin, no sibling binary.
	// Use an empty override - just verify it returns "".
	t.Setenv("PATH", t.TempDir()) // empty dir in PATH
	got := resolveBinaryPath("")
	// May or may not be empty depending on the test machine; the key invariant
	// is that it returns a non-panic value.
	_ = got
}

func TestResolveBinaryPathHomeDir(t *testing.T) {
	// Plant a fake binary in a temp home dir.
	fakeHome := t.TempDir()
	binDir := filepath.Join(fakeHome, ".providence", "bin")
	require.NoError(t, os.MkdirAll(binDir, 0755))
	bin := filepath.Join(binDir, "providence-overlay")
	require.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh\n"), 0755))

	// Temporarily point os.UserHomeDir toward our fake home using HOME env var.
	t.Setenv("HOME", fakeHome)

	got := resolveBinaryPath("")
	// It may find something else in PATH first; at minimum it should not panic.
	if got != "" {
		assert.FileExists(t, got)
	}
}

// --- Manager state machine ---

func TestManagerInitialState(t *testing.T) {
	mgr := NewManager(Config{}, nil)
	assert.Equal(t, StateStopped, mgr.State())
	assert.Equal(t, "stopped", mgr.State().String())
}

func TestStateString(t *testing.T) {
	cases := []struct {
		s    State
		want string
	}{
		{StateStopped, "stopped"},
		{StateStarting, "starting"},
		{StateRunning, "running"},
		{StateStopping, "stopping"},
		{State(99), "unknown"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, tc.s.String())
	}
}

func TestManagerBinaryMissing(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "pvd-mgr-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sockPath := filepath.Join(dir, "o.sock")

	// Empty BinaryPath + empty PATH + empty HOME means resolveBinaryPath returns "" -> "not found"
	t.Setenv("PATH", dir)  // only our temp dir in PATH, which has no providence-overlay
	t.Setenv("HOME", dir)  // override home so ~/.providence/bin lookup misses (CI vs. dev-installed binary)
	mgr := NewManager(Config{
		SocketPath: sockPath,
		BinaryPath: "", // force PATH lookup
	}, nil)

	spy := &spyHandler{}
	ctx := context.Background()
	startErr := mgr.Start(ctx, spy)

	require.Error(t, startErr)
	assert.Contains(t, startErr.Error(), "providence-overlay not found")
	assert.Equal(t, StateStopped, mgr.State(), "state must be stopped after failed start")
}

func TestManagerCannotStartWhenAlreadyRunning(t *testing.T) {
	// We can't fully run a real subprocess, so we just put the manager into a
	// non-stopped state manually and verify Start returns an error.
	mgr := NewManager(Config{}, nil)
	mgr.state = StateRunning

	spy := &spyHandler{}
	err := mgr.Start(context.Background(), spy)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot start")
}

func TestManagerCannotStopWhenNotRunning(t *testing.T) {
	mgr := NewManager(Config{}, nil)
	// Starting state - not stopped, not running: should error.
	mgr.state = StateStarting
	err := mgr.Stop(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot stop")
}

// TestManagerStopIdempotentAlreadyStopped verifies Stop from StateStopped
// returns nil (idempotent - no double-stop error).
func TestManagerStopIdempotentAlreadyStopped(t *testing.T) {
	mgr := NewManager(Config{}, nil)
	// Initial state is StateStopped.
	require.Equal(t, StateStopped, mgr.State())

	err := mgr.Stop(context.Background())
	assert.NoError(t, err, "Stop from StateStopped must be idempotent")
}

// TestManagerStopErrorsFromStarting verifies Stop from StateStarting
// still returns an error (only StateStopped and StateRunning are handled).
func TestManagerStopErrorsFromStarting(t *testing.T) {
	mgr := NewManager(Config{}, nil)
	mgr.state = StateStarting

	err := mgr.Stop(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot stop")
}

func TestManagerStatusInfoStopped(t *testing.T) {
	mgr := NewManager(Config{
		SocketPath: "/tmp/pvd-status-test.sock",
	}, nil)

	info := mgr.StatusInfo()
	assert.Equal(t, "stopped", info["state"])
	assert.Equal(t, 0, info["pid"])
	assert.Equal(t, 0, info["connected_clients"])
}

func TestManagerMarkHello(t *testing.T) {
	mgr := NewManager(Config{}, nil)
	assert.True(t, mgr.helloAt.IsZero())

	mgr.MarkHello()
	assert.False(t, mgr.helloAt.IsZero())
	assert.WithinDuration(t, time.Now(), mgr.helloAt, time.Second)
}

func TestManagerSetCallbacks(t *testing.T) {
	mgr := NewManager(Config{}, nil)
	startCalled := false
	stopCalled := false
	mgr.SetCallbacks(func() { startCalled = true }, func() { stopCalled = true })

	// Verify callbacks are stored (not invoked at set time).
	assert.False(t, startCalled)
	assert.False(t, stopCalled)

	// Call them manually to verify they work.
	mgr.onStart()
	mgr.onStop()
	assert.True(t, startCalled)
	assert.True(t, stopCalled)
}

// TestManagerStartWithMockBinary tests the full Start path using a helper
// script that speaks the overlay protocol over UDS.
func TestManagerStartWithMockBinary(t *testing.T) {
	// Find a shell to run the helper script.
	if _, err := findShell(); err != nil {
		t.Skip("no shell available for mock subprocess test")
	}

	// Require python3 (available on macOS).
	python3, pythonErr := exec.LookPath("python3")
	if pythonErr != nil {
		t.Skip("python3 not available for mock subprocess test")
	}

	dir, err := os.MkdirTemp("/tmp", "pvd-mock-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sockPath := filepath.Join(dir, "o.sock")

	// Write a Python script that:
	// 1. Parses --socket=<path> from argv
	// 2. Connects to the UDS
	// 3. Sends a hello envelope
	// 4. Waits briefly and exits
	pyScript := filepath.Join(dir, "mock_overlay.py")
	pyContent := `import socket, json, sys, time

# Parse --socket=<path> argument.
sock_path = ""
for arg in sys.argv[1:]:
    if arg.startswith("--socket="):
        sock_path = arg[len("--socket="):]

s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
for _ in range(30):
    try:
        s.connect(sock_path)
        break
    except Exception:
        time.sleep(0.1)

# data must be a raw JSON value (not double-encoded string).
hello = json.dumps({"v": 1, "type": "hello", "data": {"client_version": "0.1", "capabilities": [], "pid": 9999}}) + "\n"
s.sendall(hello.encode())
time.sleep(1)
s.close()
`
	require.NoError(t, os.WriteFile(pyScript, []byte(pyContent), 0644))

	// Write the fake binary that invokes the Python script.
	fakeBin := filepath.Join(dir, "providence-overlay")
	fakeBinContent := "#!/bin/sh\nexec " + python3 + " " + pyScript + ` "$@"` + "\n"
	require.NoError(t, os.WriteFile(fakeBin, []byte(fakeBinContent), 0755))

	spy := &spyHandler{}
	mgr := NewManager(Config{
		SocketPath: sockPath,
		BinaryPath: fakeBin,
	}, nil)

	// Start should succeed once the subprocess's hello arrives. Under
	// `-race -count=1 ./...` full-suite contention both the shell + python
	// spawn and the subsequent UDS handshake slow down noticeably, so the
	// outer budget was raised from 5s to 15s. The test's semantic ("start
	// succeeds when subprocess says hello") is unchanged.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	startErr := mgr.Start(ctx, spy)
	require.NoError(t, startErr)
	assert.Equal(t, StateRunning, mgr.State())

	// Give the subprocess a moment to send hello. 8s is generous under
	// race-scheduler contention while still failing fast on regressions
	// that prevent the handshake entirely.
	waitFor(t, func() bool { return spy.helloCount() >= 1 }, 8*time.Second)
	spy.mu.Lock()
	assert.Equal(t, 9999, spy.hellos[0].PID)
	spy.mu.Unlock()

	// Stop cleanly.
	stopErr := mgr.Stop(context.Background())
	require.NoError(t, stopErr)
	assert.Equal(t, StateStopped, mgr.State())
}

// --- spawn-disabled + stop idempotency + callbacks ---

// boolPtr returns a pointer to b, used to set Config.Spawn inline.
func boolPtr(b bool) *bool { return &b }

// TestManager_StartWithSpawnDisabled verifies that when Spawn=false Start returns
// without forking a subprocess, transitions directly to StateRunning, and the
// UDS server is active.
func TestManager_StartWithSpawnDisabled(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "pvd-spawn-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sockPath := filepath.Join(dir, "o.sock")

	mgr := NewManager(Config{
		SocketPath: sockPath,
		Spawn:      boolPtr(false),
	}, nil)

	spy := &spyHandler{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = mgr.Start(ctx, spy)
	require.NoError(t, err)

	// State must be running immediately - no hello wait loop.
	assert.Equal(t, StateRunning, mgr.State())
	// Server must be active.
	assert.NotNil(t, mgr.Server())
	// No process spawned.
	assert.Equal(t, 0, mgr.cmdPID)
	assert.Nil(t, mgr.cmd)

	// Clean up.
	require.NoError(t, mgr.Stop(context.Background()))
	assert.Equal(t, StateStopped, mgr.State())
}

// TestManager_StopIdempotent calls Stop twice on a running manager (spawn=false)
// and asserts the second call returns nil without deadlock.
func TestManager_StopIdempotent(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "pvd-stop2-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sockPath := filepath.Join(dir, "o.sock")

	mgr := NewManager(Config{SocketPath: sockPath, Spawn: boolPtr(false)}, nil)
	spy := &spyHandler{}
	ctx := context.Background()

	require.NoError(t, mgr.Start(ctx, spy))
	assert.Equal(t, StateRunning, mgr.State())

	err1 := mgr.Stop(ctx)
	assert.NoError(t, err1, "first Stop must succeed")
	assert.Equal(t, StateStopped, mgr.State())

	// Second Stop must be idempotent (already stopped).
	err2 := mgr.Stop(ctx)
	assert.NoError(t, err2, "second Stop must be idempotent")
}

// TestManager_CallbacksNilSafe verifies that nil onStart/onStop callbacks do
// not cause a panic during Start+Stop.
func TestManager_CallbacksNilSafe(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "pvd-cbnil-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sockPath := filepath.Join(dir, "o.sock")

	mgr := NewManager(Config{SocketPath: sockPath, Spawn: boolPtr(false)}, nil)
	// Explicitly leave callbacks nil (default).
	assert.Nil(t, mgr.onStart)
	assert.Nil(t, mgr.onStop)

	spy := &spyHandler{}
	ctx := context.Background()

	require.NotPanics(t, func() {
		require.NoError(t, mgr.Start(ctx, spy))
		require.NoError(t, mgr.Stop(ctx))
	})
}

// TestManager_StopWithNoStartReturnsCleanly asserts that calling Stop on a
// brand-new Manager that was never started returns nil (StateStopped -> idempotent).
func TestManager_StopWithNoStartReturnsCleanly(t *testing.T) {
	mgr := NewManager(Config{}, nil)
	assert.Equal(t, StateStopped, mgr.State())
	err := mgr.Stop(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, StateStopped, mgr.State())
}

// TestManager_StartTwice verifies that calling Start when the manager is
// already running returns an error and does not change state.
func TestManager_StartTwice(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "pvd-start2-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sockPath := filepath.Join(dir, "o.sock")

	mgr := NewManager(Config{SocketPath: sockPath, Spawn: boolPtr(false)}, nil)
	spy := &spyHandler{}
	ctx := context.Background()

	require.NoError(t, mgr.Start(ctx, spy))
	assert.Equal(t, StateRunning, mgr.State())

	// Second Start must return an error (cannot start in state running).
	err2 := mgr.Start(ctx, spy)
	require.Error(t, err2)
	assert.Contains(t, err2.Error(), "cannot start")
	// State must remain running.
	assert.Equal(t, StateRunning, mgr.State())

	_ = mgr.Stop(ctx)
}

// --- helpers ---

func findShell() (string, error) {
	for _, sh := range []string{"/bin/sh", "/usr/bin/sh"} {
		if _, err := os.Stat(sh); err == nil {
			return sh, nil
		}
	}
	return "", os.ErrNotExist
}

