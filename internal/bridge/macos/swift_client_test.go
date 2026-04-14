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
