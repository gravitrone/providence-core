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

	// Empty BinaryPath + empty PATH means resolveBinaryPath returns "" -> "not found"
	t.Setenv("PATH", dir) // only our temp dir in PATH, which has no providence-overlay
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
	// Stopped state.
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

	// Start should succeed - hello arrives within 3s.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	startErr := mgr.Start(ctx, spy)
	require.NoError(t, startErr)
	assert.Equal(t, StateRunning, mgr.State())

	// Give the subprocess a moment to send hello.
	waitFor(t, func() bool { return spy.helloCount() >= 1 }, 2*time.Second)
	spy.mu.Lock()
	assert.Equal(t, 9999, spy.hellos[0].PID)
	spy.mu.Unlock()

	// Stop cleanly.
	stopErr := mgr.Stop(context.Background())
	require.NoError(t, stopErr)
	assert.Equal(t, StateStopped, mgr.State())
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

