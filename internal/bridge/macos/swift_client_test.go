//go:build darwin

package macos

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	if os.Getenv("FAKE_SWIFT_BRIDGE") == "1" {
		runFakeSwift()
		return
	}

	cleanup, err := installTestClipboard()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	code := m.Run()
	cleanup()
	os.Exit(code)
}

func runFakeSwift() {
	encoder := json.NewEncoder(os.Stdout)
	_ = encoder.Encode(map[string]string{"event": "ready"})

	if os.Getenv("FAKE_SWIFT_CRASH") == "1" {
		return
	}

	if os.Getenv("FAKE_SWIFT_EVENTS") == "1" {
		_ = encoder.Encode(map[string]any{
			"event": "focus_changed",
			"data":  nil,
		})
	}

	scanner := bufio.NewScanner(os.Stdin)
	largePayloadSent := false
	for scanner.Scan() {
		var req Request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			return
		}

		if req.Method == "shutdown" {
			_ = encoder.Encode(Response{
				ID:     req.ID,
				OK:     true,
				Result: json.RawMessage(`"shutdown-response"`),
			})
			return
		}

		if os.Getenv("FAKE_SWIFT_PERMISSION_DENIED") == "1" {
			_ = encoder.Encode(Response{
				ID: req.ID,
				Error: &ProtocolError{
					Code:       ErrPermissionDenied,
					Message:    "permission denied",
					URL:        "x-apple.systempreferences:test",
					Remediable: true,
				},
			})
			continue
		}

		if os.Getenv("FAKE_SWIFT_LARGE") == "1" && !largePayloadSent {
			largePayloadSent = true
			_ = encoder.Encode(Response{
				ID:     req.ID,
				OK:     true,
				Result: json.RawMessage(strconv.Quote(strings.Repeat("x", 8<<20))),
			})
			continue
		}

		_ = encoder.Encode(Response{
			ID:     req.ID,
			OK:     true,
			Result: json.RawMessage(strconv.Quote(req.Method + "-response")),
		})
	}
}

func fakeSwiftBinary() string {
	return os.Args[0]
}

func spawnTestClient(t *testing.T, env ...string) *swiftClient {
	t.Helper()

	t.Setenv("FAKE_SWIFT_BRIDGE", "1")
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		require.True(t, ok, "env must be KEY=VALUE: %s", entry)
		t.Setenv(key, value)
	}

	client, err := spawnSwift(context.Background(), fakeSwiftBinary(), time.Second)
	require.NoError(t, err)

	return client
}

func installTestClipboard() (func(), error) {
	dir, err := os.MkdirTemp("", "providence-test-clipboard")
	if err != nil {
		return nil, fmt.Errorf("create test clipboard dir: %w", err)
	}

	clipboardFile := filepath.Join(dir, "clipboard.txt")
	if err := os.Setenv("PROVIDENCE_TEST_CLIPBOARD_FILE", clipboardFile); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("set clipboard env: %w", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "pbcopy"), []byte(`#!/bin/sh
cat > "$PROVIDENCE_TEST_CLIPBOARD_FILE"
`), 0o755); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("write pbcopy shim: %w", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "pbpaste"), []byte(`#!/bin/sh
if [ -f "$PROVIDENCE_TEST_CLIPBOARD_FILE" ]; then
	cat "$PROVIDENCE_TEST_CLIPBOARD_FILE"
fi
`), 0o755); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("write pbpaste shim: %w", err)
	}

	originalPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+originalPath); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("set path: %w", err)
	}

	return func() {
		_ = os.Setenv("PATH", originalPath)
		_ = os.Unsetenv("PROVIDENCE_TEST_CLIPBOARD_FILE")
		_ = os.RemoveAll(dir)
	}, nil
}

func TestSwiftClientRequestResponse(t *testing.T) {
	client := spawnTestClient(t)
	defer func() {
		_ = client.Close(context.Background())
	}()

	result, err := client.call(t.Context(), "ping", nil)
	require.NoError(t, err)

	var value string
	require.NoError(t, json.Unmarshal(result, &value))
	assert.Equal(t, "ping-response", value)
}

func TestSwiftClientConcurrentCalls(t *testing.T) {
	client := spawnTestClient(t)
	defer func() {
		_ = client.Close(context.Background())
	}()

	const workers = 10

	results := make([]string, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func(index int) {
			defer wg.Done()

			method := fmt.Sprintf("method-%d", index)
			result, err := client.call(t.Context(), method, nil)
			if err != nil {
				errs <- err
				return
			}

			var value string
			if err := json.Unmarshal(result, &value); err != nil {
				errs <- err
				return
			}
			results[index] = value
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	for i := 0; i < workers; i++ {
		assert.Equal(t, fmt.Sprintf("method-%d-response", i), results[i])
	}
}

func TestSwiftClientSubprocessCrash(t *testing.T) {
	client := spawnTestClient(t, "FAKE_SWIFT_CRASH=1")

	_, err := client.call(t.Context(), "ping", nil)
	assert.Error(t, err)
}

func TestSwiftClientClose(t *testing.T) {
	client := spawnTestClient(t)

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	require.NoError(t, client.Close(ctx))

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- client.cmd.Wait()
	}()

	select {
	case err := <-waitDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("swift client did not exit within 1s")
	}
}

func TestSwiftClientEvents(t *testing.T) {
	client := spawnTestClient(t, "FAKE_SWIFT_EVENTS=1")
	defer func() {
		_ = client.Close(context.Background())
	}()

	select {
	case event := <-client.Events():
		assert.Equal(t, "focus_changed", event.Type)
		assert.Equal(t, "null", string(event.Data))
	case <-time.After(time.Second):
		t.Fatal("expected unsolicited event")
	}
}

func TestSwiftClientLargePayload(t *testing.T) {
	client := spawnTestClient(t, "FAKE_SWIFT_LARGE=1")
	defer func() {
		_ = client.Close(context.Background())
	}()

	result, err := client.call(t.Context(), "large", nil)
	require.NoError(t, err)

	var value string
	require.NoError(t, json.Unmarshal(result, &value))
	assert.GreaterOrEqual(t, len(value), 8<<20)
}

// writeFakeBinary writes a shell script to a temp dir and returns its path.
func writeFakeBinary(t *testing.T, behavior string) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "fake.sh")
	content := "#!/bin/sh\n" + behavior
	require.NoError(t, os.WriteFile(script, []byte(content), 0o755))
	return script
}

func TestSwiftClient_SpawnFailsOnMissingBinary(t *testing.T) {
	_, err := spawnSwift(context.Background(), "/nonexistent/path/to/binary", time.Second)
	require.Error(t, err)
}

func TestSwiftClient_GracefulStopClosesProcess(t *testing.T) {
	// Emits ready then sleeps forever; Close must terminate it via SIGKILL path.
	// Close escalates: send shutdown (250ms) -> SIGTERM (500ms) -> SIGKILL (500ms) = ~1.75s max.
	bin := writeFakeBinary(t, `printf '{"event":"ready"}\n'
sleep 9999
`)
	client, err := spawnSwift(context.Background(), bin, time.Second)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- client.Close(ctx) }()

	select {
	case closeErr := <-done:
		require.NoError(t, closeErr)
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not return within 3s")
	}
}

func TestSwiftClient_ContextCancellationCleansUp(t *testing.T) {
	// Normal client; cancel context while a request is in-flight.
	// The fake never responds to requests (hangs after ready).
	bin := writeFakeBinary(t, `printf '{"event":"ready"}\n'
cat > /dev/null
`)
	client, err := spawnSwift(context.Background(), bin, time.Second)
	require.NoError(t, err)
	defer func() { _ = client.Close(context.Background()) }()

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		_, err := client.call(ctx, "hanging", nil)
		errCh <- err
	}()

	// Give the request a moment to register, then cancel.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("call did not return after context cancellation within 500ms")
	}
}

func TestSwiftClient_MalformedJSONResponseTolerated(t *testing.T) {
	// Fake emits ready, then responds to first request with garbage,
	// causing the reader to fail. Subsequent call should get an error
	// (not a panic) because done is closed.
	bin := writeFakeBinary(t, `printf '{"event":"ready"}\n'
# Read first request line then emit garbage
read line
printf 'not json\n'
sleep 9999
`)
	client, err := spawnSwift(context.Background(), bin, time.Second)
	require.NoError(t, err)
	defer func() { _ = client.Close(context.Background()) }()

	// First call triggers the malformed response; readLoop calls fail().
	_, firstErr := client.call(context.Background(), "boom", nil)
	require.Error(t, firstErr)

	// Second call must also return an error (not panic), because done is closed.
	_, secondErr := client.call(context.Background(), "after-fail", nil)
	require.Error(t, secondErr)
}

func TestSwiftClient_IDCorrelation(t *testing.T) {
	// Use the re-exec fake binary (default path echoes method+"-response" with matching ID).
	// Fire 3 concurrent calls; each must receive its own correlated response.
	client := spawnTestClient(t)
	defer func() { _ = client.Close(context.Background()) }()

	const n = 3
	type result struct {
		method string
		val    string
		err    error
	}
	results := make(chan result, n)

	for i := 0; i < n; i++ {
		go func(idx int) {
			method := fmt.Sprintf("corr%d", idx)
			raw, err := client.call(context.Background(), method, nil)
			if err != nil {
				results <- result{method: method, err: err}
				return
			}
			var val string
			if err := json.Unmarshal(raw, &val); err != nil {
				results <- result{method: method, err: err}
				return
			}
			results <- result{method: method, val: val}
		}(i)
	}

	for i := 0; i < n; i++ {
		r := <-results
		require.NoError(t, r.err, "method %s", r.method)
		assert.Equal(t, r.method+"-response", r.val, "method %s got wrong correlated response", r.method)
	}
}

func TestSwiftClient_StdinBrokenReturnsError(t *testing.T) {
	// Fake emits ready then exits immediately, simulating stdin pipe closure.
	// Any subsequent Request should return an error, not panic.
	bin := writeFakeBinary(t, `printf '{"event":"ready"}\n'
exit 0
`)
	client, err := spawnSwift(context.Background(), bin, time.Second)
	require.NoError(t, err)
	defer func() { _ = client.Close(context.Background()) }()

	// Give readLoop a moment to detect exit.
	time.Sleep(30 * time.Millisecond)

	_, callErr := client.call(context.Background(), "after-exit", nil)
	require.Error(t, callErr)
}
