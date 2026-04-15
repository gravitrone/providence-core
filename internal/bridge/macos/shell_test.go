//go:build darwin

package macos

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeFakeBin writes a shell script at dir/name that exits with exitCode and
// optionally writes output to stdout. The script is made executable.
func makeFakeBin(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\n" + body + "\n"
	require.NoError(t, os.WriteFile(path, []byte(script), 0755))
	return path
}

// withFakePATH prepends dir to PATH for the duration of the test and restores
// it afterwards. Returns a cleanup func (called via t.Cleanup).
func withFakePATH(t *testing.T, dir string) {
	t.Helper()
	orig := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", orig) })
	os.Setenv("PATH", dir+":"+orig)
}

// newShell returns a shellClient ready for use.
func newShell() *shellClient { return &shellClient{} }

// ---------------------------------------------------------------------------
// screencapture
// ---------------------------------------------------------------------------

// TestShell_ScreencaptureInvokesBinaryWithArgs checks that Screenshot calls
// screencapture and returns a non-empty path on success.
func TestShell_ScreencaptureInvokesBinaryWithArgs(t *testing.T) {
	binDir := t.TempDir()
	// Fake screencapture: create the output file it's told to write.
	makeFakeBin(t, binDir, "screencapture", `
# last arg is the output path
outfile="${@: -1}"
touch "$outfile"
`)
	withFakePATH(t, binDir)

	sc := newShell()
	path, err := sc.Screenshot(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, path)

	// File should have been created by the fake binary.
	_, statErr := os.Stat(path)
	assert.NoError(t, statErr)
}

// TestShell_ScreencaptureExitCodeNonZeroReturnsError verifies that a non-zero
// exit from screencapture bubbles up as an error.
func TestShell_ExitCodeNonZeroReturnsError(t *testing.T) {
	binDir := t.TempDir()
	makeFakeBin(t, binDir, "screencapture", `exit 1`)
	withFakePATH(t, binDir)

	sc := newShell()
	_, err := sc.Screenshot(context.Background())
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// osascript timeout
// ---------------------------------------------------------------------------

// TestShell_OsascriptTimeoutReturnsError exercises the ctx-timeout path on
// runOsascript. Prior to the Setpgid + killpg fix this test would hang
// forever because the shell child spawned by `do shell script` survived
// the SIGKILL delivered to osascript alone. The fix now kills the entire
// process group, so this returns with a ctx.DeadlineExceeded wrap within
// the timeout window.
func TestShell_OsascriptTimeoutReturnsError(t *testing.T) {
	if _, err := exec.LookPath("osascript"); err != nil {
		t.Skip("osascript not available on this runner")
	}

	sc := newShell()
	// 300ms ctx against a script that asks the shell to sleep 10s.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := sc.runOsascript(ctx, `do shell script "sleep 10"`)
	elapsed := time.Since(start)

	require.Error(t, err, "runOsascript must return an error when ctx expires")
	assert.ErrorIs(t, err, context.DeadlineExceeded, "error must wrap ctx.DeadlineExceeded")
	assert.Less(t, elapsed, 2*time.Second,
		"runOsascript must return within a couple of seconds of ctx expiry, not hang on the child pipe")
}

// TestShell_OsascriptCtxCancellationKillsChildProcessGroup verifies that
// the ctx-cancellation path actually reaps the shell's sleep child rather
// than leaving it orphaned. We use a sentinel file that the sleep shell
// would have written to if it outran the kill; the file's absence after
// a grace period proves the group was killed.
func TestShell_OsascriptCtxCancellationKillsChildProcessGroup(t *testing.T) {
	if _, err := exec.LookPath("osascript"); err != nil {
		t.Skip("osascript not available on this runner")
	}

	dir := t.TempDir()
	sentinel := filepath.Join(dir, "child-survived")

	sc := newShell()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// `sleep 2; touch sentinel` via AppleScript. If the group survives the
	// kill the sleep finishes and the sentinel appears.
	script := fmt.Sprintf(`do shell script "sleep 2; touch %s"`, sentinel)
	err := sc.runOsascript(ctx, script)
	require.Error(t, err)

	// Wait past the would-be sleep completion. If the child survived the
	// group kill, it would have touched the sentinel by now.
	time.Sleep(2500 * time.Millisecond)
	_, statErr := os.Stat(sentinel)
	assert.True(t, os.IsNotExist(statErr),
		"sentinel %s must not exist - its presence means the shell child outlived runOsascript's ctx kill", sentinel)
}

// TestShell_OsascriptFastPathNoLeak verifies the happy path still works
// after the ctx-cancellation rewrite: a trivial script that finishes
// quickly returns cleanly, without leaving zombies or consuming the ctx
// deadline.
func TestShell_OsascriptFastPathNoLeak(t *testing.T) {
	if _, err := exec.LookPath("osascript"); err != nil {
		t.Skip("osascript not available on this runner")
	}

	sc := newShell()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	err := sc.runOsascript(ctx, `return 1`)
	require.NoError(t, err)
	assert.Less(t, time.Since(start), 2*time.Second, "trivial script must return promptly")
}

// ---------------------------------------------------------------------------
// pbcopy
// ---------------------------------------------------------------------------

// TestShell_PbcopyStdinPipesCorrectly verifies that ClipboardWrite pipes the
// expected text to pbcopy's stdin.
func TestShell_PbcopyStdinPipesCorrectly(t *testing.T) {
	binDir := t.TempDir()
	capture := filepath.Join(binDir, "pbcopy.out")
	// Fake pbcopy: write everything from stdin to a file we can inspect.
	makeFakeBin(t, binDir, "pbcopy", `cat > "`+capture+`"`)
	withFakePATH(t, binDir)

	sc := newShell()
	require.NoError(t, sc.ClipboardWrite(context.Background(), "hello-pbcopy"))

	data, err := os.ReadFile(capture)
	require.NoError(t, err)
	assert.Equal(t, "hello-pbcopy", string(data))
}

// ---------------------------------------------------------------------------
// pbpaste
// ---------------------------------------------------------------------------

// TestShell_PbpasteCapturesStdout verifies that ClipboardRead returns whatever
// pbpaste prints to stdout.
func TestShell_PbpasteCapturesStdout(t *testing.T) {
	binDir := t.TempDir()
	makeFakeBin(t, binDir, "pbpaste", `printf 'clipboard-content'`)
	withFakePATH(t, binDir)

	sc := newShell()
	got, err := sc.ClipboardRead(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "clipboard-content", got)
}

// ---------------------------------------------------------------------------
// missing binary
// ---------------------------------------------------------------------------

// TestShell_MissingBinaryReturnsError verifies that calling Screenshot when
// screencapture is absent from PATH returns an error.
func TestShell_MissingBinaryReturnsError(t *testing.T) {
	// Point PATH at an empty dir so screencapture won't be found.
	emptyDir := t.TempDir()
	withFakePATH(t, emptyDir)
	// Also remove the real PATH so the real screencapture is not found.
	orig := os.Getenv("PATH")
	os.Setenv("PATH", emptyDir)
	t.Cleanup(func() { os.Setenv("PATH", orig) })

	sc := newShell()
	_, err := sc.Screenshot(context.Background())
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// pbpaste non-zero exit
// ---------------------------------------------------------------------------

// TestShell_PbpasteNonZeroExitReturnsError verifies that a failing pbpaste
// propagates an error through ClipboardRead.
func TestShell_PbpasteNonZeroExitReturnsError(t *testing.T) {
	binDir := t.TempDir()
	makeFakeBin(t, binDir, "pbpaste", `exit 2`)
	withFakePATH(t, binDir)

	sc := newShell()
	_, err := sc.ClipboardRead(context.Background())
	assert.Error(t, err)
}
