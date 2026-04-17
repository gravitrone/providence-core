package claude

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSessionDrainsStderrAndCloseWaitsForExit(t *testing.T) {
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	zdotDir := filepath.Join(tempDir, "zdotdir")

	require.NoError(t, os.MkdirAll(binDir, 0o755))
	require.NoError(t, os.MkdirAll(zdotDir, 0o755))

	scriptPath := filepath.Join(binDir, "claude")
	script := `#!/bin/sh
trap '' TERM
dd if=/dev/zero bs=1024 count=96 2>/dev/null | tr '\000' 'x' >&2
printf '\n' >&2
printf '%s\n' '{"type":"system","subtype":"init","session_id":"test-session","tools":[],"model":"test-model"}'
while :; do
  printf 'still flooding stderr\n' >&2
done
`
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))

	zdotEnv := "export PATH=\"" + binDir + ":$PATH\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(zdotDir, ".zshenv"), []byte(zdotEnv), 0o644))

	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))
	t.Setenv("ZDOTDIR", zdotDir)

	type newSessionResult struct {
		session *Session
		err     error
	}

	resultCh := make(chan newSessionResult, 1)
	go func() {
		session, err := NewSession("test prompt", nil, "")
		resultCh <- newSessionResult{session: session, err: err}
	}()

	var session *Session
	select {
	case result := <-resultCh:
		require.NoError(t, result.err)
		require.NotNil(t, result.session)
		session = result.session
	case <-time.After(time.Second):
		t.Fatal("NewSession did not return within 1s")
	}

	assert.True(t, strings.HasPrefix(session.cmd.Path, binDir+string(os.PathSeparator)))

	require.Eventually(t, func() bool {
		return session.Status() == engine.StatusRunning
	}, 2*time.Second, 25*time.Millisecond)

	closeDone := make(chan struct{})
	go func() {
		session.Close()
		close(closeDone)
	}()

	select {
	case <-closeDone:
	case <-time.After(6 * time.Second):
		t.Fatal("Close did not return within 6s")
	}

	require.NotNil(t, session.cmd.ProcessState)
	assert.Error(t, session.cmd.Process.Signal(syscall.Signal(0)))
}
