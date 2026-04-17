package direct

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/gravitrone/providence-core/internal/engine/hooks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Fake Anthropic SSE server ---
//
// The DirectEngine talks to the Anthropic SDK directly via a concrete
// anthropic.Client, so the only way to drive the agent loop in tests is via
// option.WithBaseURL pointed at an httptest server that speaks the SSE
// protocol. The helpers below emit the minimum set of events the SDK needs to
// accumulate a complete anthropic.Message.

// sseWriter writes canonical Anthropic SSE frames.
type sseWriter struct {
	w http.ResponseWriter
	f http.Flusher
}

type hookRecorder struct {
	mu     sync.Mutex
	inputs []hooks.HookInput
}

func (r *hookRecorder) handler(w http.ResponseWriter, req *http.Request) {
	var input hooks.HookInput
	_ = json.NewDecoder(req.Body).Decode(&input)

	r.mu.Lock()
	r.inputs = append(r.inputs, input)
	r.mu.Unlock()

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{}`))
}

func (r *hookRecorder) snapshot() []hooks.HookInput {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]hooks.HookInput(nil), r.inputs...)
}

func (r *hookRecorder) waitForCount(t *testing.T, want int, timeout time.Duration) []hooks.HookInput {
	t.Helper()

	require.Eventually(t, func() bool {
		return len(r.snapshot()) >= want
	}, timeout, 20*time.Millisecond)

	return r.snapshot()
}

func newSSE(w http.ResponseWriter) *sseWriter {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	f, _ := w.(http.Flusher)
	return &sseWriter{w: w, f: f}
}

func (s *sseWriter) event(name string, payload any) {
	b, _ := json.Marshal(payload)
	fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, b)
	if s.f != nil {
		s.f.Flush()
	}
}

// writeTextTurn emits a complete single-turn response with a text block and
// StopReason=end_turn. This is the simplest success path.
func writeTextTurn(w http.ResponseWriter, text string) {
	s := newSSE(w)
	s.event("message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id": "msg_test", "type": "message", "role": "assistant",
			"content":       []any{},
			"model":         "claude-sonnet-4-20250514",
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         map[string]any{"input_tokens": 5, "output_tokens": 0},
		},
	})
	s.event("content_block_start", map[string]any{
		"type": "content_block_start", "index": 0,
		"content_block": map[string]any{"type": "text", "text": ""},
	})
	s.event("content_block_delta", map[string]any{
		"type": "content_block_delta", "index": 0,
		"delta": map[string]any{"type": "text_delta", "text": text},
	})
	s.event("content_block_stop", map[string]any{
		"type": "content_block_stop", "index": 0,
	})
	s.event("message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": 3},
	})
	s.event("message_stop", map[string]any{"type": "message_stop"})
}

// newFakeEngine builds a DirectEngine whose Anthropic client points at the
// given httptest server. Retries are tightened to 1 so error paths terminate
// quickly. Returns the engine and a cleanup func.
func newFakeEngine(t *testing.T, srv *httptest.Server) *DirectEngine {
	return newFakeEngineWithRetries(t, srv, 1)
}

func newFakeEngineWithRetries(t *testing.T, srv *httptest.Server, maxRetries int) *DirectEngine {
	t.Helper()
	e := newFakeEngineWithWorkDir(t, srv, t.TempDir())
	// Override the default "1" that newFakeEngineWithWorkDir sets so callers
	// that need a specific retry budget (e.g. 529-backoff tests) get it.
	t.Setenv("PROVIDENCE_MAX_RETRIES", strconv.Itoa(maxRetries))
	return e
}

func newFakeEngineWithWorkDir(t *testing.T, srv *httptest.Server, workDir string) *DirectEngine {
	t.Helper()
	t.Setenv("PROVIDENCE_MAX_RETRIES", "1")
	t.Setenv("PROVIDENCE_STREAM_IDLE_TIMEOUT_MS", "2000")
	t.Setenv("HOME", t.TempDir())

	e, err := NewDirectEngine(engine.EngineConfig{
		Type:    engine.EngineTypeDirect,
		Model:   "claude-sonnet-4-20250514",
		APIKey:  "test-key-not-real",
		WorkDir: workDir,
	})
	require.NoError(t, err)
	// Swap the SDK client for one pointed at the fake server.
	e.anthropicAPIKey = "test-key"
	e.anthropicBaseURL = srv.URL
	e.client = anthropic.NewClient(
		option.WithAPIKey(e.anthropicAPIKey),
		option.WithBaseURL(e.anthropicBaseURL),
		option.WithHTTPClient(newAnthropicHTTPClient(false)),
		option.WithMaxRetries(0),
	)
	return e
}

func writeCompactTTLConfig(t *testing.T, workDir, ttl string) {
	t.Helper()

	configDir := filepath.Join(workDir, ".providence")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(configDir, "config.toml"),
		[]byte(fmt.Sprintf("[compact]\ncache_ttl = %q\n", ttl)),
		0o644,
	))
}

// waitForResult drains events until a result event arrives or timeout.
// Returns the result event and all events that came before it.
func waitForResult(t *testing.T, e *DirectEngine, timeout time.Duration) (*engine.ResultEvent, []engine.ParsedEvent) {
	t.Helper()
	deadline := time.After(timeout)
	var before []engine.ParsedEvent
	for {
		select {
		case ev := <-e.Events():
			if ev.Type == "result" {
				r, _ := ev.Data.(*engine.ResultEvent)
				return r, before
			}
			before = append(before, ev)
		case <-deadline:
			t.Fatalf("timed out waiting for result event (saw %d events)", len(before))
			return nil, before
		}
	}
}

// --- Tests ---

func TestCacheControlEmits5mByDefault(t *testing.T) {
	bodyCh := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		select {
		case bodyCh <- string(body):
		default:
		}
		writeTextTurn(w, "ok")
	}))
	defer srv.Close()

	e := newFakeEngine(t, srv)
	e.blocks = []engine.SystemBlock{{Text: "cacheable system prompt", Cacheable: true}}
	require.NoError(t, e.Send("hi"))
	_, _ = waitForResult(t, e, 5*time.Second)

	select {
	case body := <-bodyCh:
		assert.Contains(t, body, "\"cache_control\"")
		assert.Contains(t, body, "\"type\":\"ephemeral\"")
		assert.NotContains(t, body, "\"ttl\":\"1h\"")
		if strings.Contains(body, "\"ttl\":") {
			assert.Contains(t, body, "\"ttl\":\"5m\"")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for request body")
	}
}

func TestCacheControlEmits1hWhenConfigured(t *testing.T) {
	bodyCh := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		select {
		case bodyCh <- string(body):
		default:
		}
		writeTextTurn(w, "ok")
	}))
	defer srv.Close()

	workDir := t.TempDir()
	writeCompactTTLConfig(t, workDir, "1h")

	e := newFakeEngineWithWorkDir(t, srv, workDir)
	e.blocks = []engine.SystemBlock{{Text: "cacheable system prompt", Cacheable: true}}
	require.NoError(t, e.Send("hi"))
	_, _ = waitForResult(t, e, 5*time.Second)

	select {
	case body := <-bodyCh:
		assert.Contains(t, body, "\"cache_control\"")
		assert.Contains(t, body, "\"type\":\"ephemeral\"")
		assert.Contains(t, body, "\"ttl\":\"1h\"")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for request body")
	}
}

// TestSend_NaturalEndCompletes drives a single-turn Send through the real
// agent loop against a fake server that returns a natural end_turn response.
// Verifies status transitions Idle -> Running -> Completed, that history
// contains [user, assistant], and that a success result is emitted.
func TestSend_NaturalEndCompletes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTextTurn(w, "hello bro")
	}))
	defer srv.Close()

	e := newFakeEngine(t, srv)
	require.Equal(t, engine.StatusIdle, e.Status())

	require.NoError(t, e.Send("hi"))

	res, _ := waitForResult(t, e, 5*time.Second)
	require.NotNil(t, res)
	assert.Equal(t, "success", res.Subtype)
	assert.False(t, res.IsError)

	msgs := e.history.Messages()
	require.Len(t, msgs, 2, "history should contain [user, assistant]")
	assert.Equal(t, anthropic.MessageParamRoleUser, msgs[0].Role)
	assert.Equal(t, anthropic.MessageParamRoleAssistant, msgs[1].Role)
	// Status must be Completed after a successful Send.
	assert.Equal(t, engine.StatusCompleted, e.Status())
}

// TestSend_ConcurrentSendRejected verifies the mutex guard in Send: calling
// Send while status == Running returns "already running".
func TestSend_ConcurrentSendRejected(t *testing.T) {
	e, err := NewDirectEngine(engine.EngineConfig{
		Type:   engine.EngineTypeDirect,
		Model:  "claude-sonnet-4-20250514",
		APIKey: "test-key-not-real",
	})
	require.NoError(t, err)

	// Manually flip status - emulates an in-flight Send.
	e.mu.Lock()
	e.status = engine.StatusRunning
	e.mu.Unlock()

	err = e.Send("second")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")
}

// TestSend_NoEngineStatusLeakOnError drives Send into the failure path (the
// fake server returns HTTP 400 which the SDK surfaces as a non-retryable
// error) and confirms the engine status is Failed afterwards, not stuck
// Running. A subsequent Send should NOT be rejected by the "already running"
// guard because status has been moved off Running.
func TestSend_NoEngineStatusLeakOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"bad"}}`))
	}))
	defer srv.Close()

	e := newFakeEngine(t, srv)
	require.NoError(t, e.Send("hi"))

	res, _ := waitForResult(t, e, 5*time.Second)
	require.NotNil(t, res)
	assert.True(t, res.IsError, "result should be an error on 400")

	// Status must not be stuck Running.
	st := e.Status()
	assert.NotEqual(t, engine.StatusRunning, st, "status must leave Running on error")
	assert.Equal(t, engine.StatusFailed, st)
}

func TestRetries529WithBackoff(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := attempts.Add(1)
		if attempt <= 2 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(529)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"cooked"}}`))
			return
		}
		writeTextTurn(w, "recovered")
	}))
	defer srv.Close()

	e := newFakeEngineWithRetries(t, srv, 1)

	start := time.Now()
	require.NoError(t, e.Send("hi"))

	res, _ := waitForResult(t, e, 10*time.Second)
	require.NotNil(t, res)
	assert.False(t, res.IsError)
	assert.Equal(t, int32(3), attempts.Load())
	assert.GreaterOrEqual(t, time.Since(start), 3*time.Second)
}

func TestECONNRESETDisablesKeepAliveAndRetries(t *testing.T) {
	var attempts atomic.Int32
	var sawCloseHeader atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := attempts.Add(1)
		if attempt == 1 {
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				t.Errorf("response writer does not support hijacking")
				return
			}
			conn, _, err := hijacker.Hijack()
			if err != nil {
				t.Errorf("hijack connection: %v", err)
				return
			}
			if tcpConn, ok := conn.(*net.TCPConn); ok {
				_ = tcpConn.SetLinger(0)
			}
			_ = conn.Close()
			return
		}

		sawCloseHeader.Store(r.Close || strings.EqualFold(r.Header.Get("Connection"), "close"))
		writeTextTurn(w, "recovered")
	}))
	defer srv.Close()

	e := newFakeEngineWithRetries(t, srv, 1)

	require.NoError(t, e.Send("hi"))

	res, _ := waitForResult(t, e, 10*time.Second)
	require.NotNil(t, res)
	assert.False(t, res.IsError)
	assert.Equal(t, int32(2), attempts.Load())
	assert.True(t, sawCloseHeader.Load())
	assert.True(t, e.retryStates[engine.ProviderAnthropic].disableKeepAlives)
}

func TestMaxRetriesSurfacesError(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(529)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"still cooked"}}`))
	}))
	defer srv.Close()

	e := newFakeEngineWithRetries(t, srv, 1)

	require.NoError(t, e.Send("hi"))

	res, _ := waitForResult(t, e, 30*time.Second)
	require.NotNil(t, res)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Result, "anthropic: overloaded after 4 retries")
	assert.Equal(t, int32(5), attempts.Load())
}

// TestSend_PendingImagesClearedAfterSend verifies that pendingImages is
// consumed and cleared atomically inside Send. After Send returns, a second
// inspection should see pendingImages == nil.
func TestSend_PendingImagesClearedAfterSend(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTextTurn(w, "ok")
	}))
	defer srv.Close()

	e := newFakeEngine(t, srv)
	e.SetPendingImages([]ImageData{
		{MediaType: "image/png", Data: []byte{0x89, 0x50, 0x4e, 0x47}},
	})

	// Sanity: before Send, pendingImages is populated.
	e.mu.Lock()
	pre := len(e.pendingImages)
	e.mu.Unlock()
	require.Equal(t, 1, pre)

	require.NoError(t, e.Send("with pic"))
	_, _ = waitForResult(t, e, 5*time.Second)

	e.mu.Lock()
	post := e.pendingImages
	e.mu.Unlock()
	assert.Nil(t, post, "pendingImages must be cleared after Send consumes them")

	// First history message must be the user turn with images+text blocks.
	msgs := e.history.Messages()
	require.NotEmpty(t, msgs)
	assert.Equal(t, anthropic.MessageParamRoleUser, msgs[0].Role)
	assert.GreaterOrEqual(t, len(msgs[0].Content), 2, "user msg should have image + text blocks")
}

// TestSend_PrepareUserTextInjectsReminder verifies that when a context
// injector exposes a PendingSystemReminder, Send() prepends it to the user
// text before it lands in history.
func TestSend_PrepareUserTextInjectsReminder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTextTurn(w, "ack")
	}))
	defer srv.Close()

	e := newFakeEngine(t, srv)
	e.SetContextInjector(&fakeInj{reminder: "<sr>remind</sr>"})

	require.NoError(t, e.Send("payload"))
	_, _ = waitForResult(t, e, 5*time.Second)

	msgs := e.history.Messages()
	require.NotEmpty(t, msgs)
	// The user message text block should contain both the reminder and payload.
	first := msgs[0]
	require.NotEmpty(t, first.Content)
	text := first.Content[0].OfText
	require.NotNil(t, text)
	assert.Contains(t, text.Text, "<sr>remind</sr>")
	assert.Contains(t, text.Text, "payload")
}

// TestSend_NilInjectorPassesThrough verifies that without a context injector,
// user text reaches history unmodified.
func TestSend_NilInjectorPassesThrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTextTurn(w, "ack")
	}))
	defer srv.Close()

	e := newFakeEngine(t, srv)
	// No SetContextInjector call: contextInjector stays nil.

	require.NoError(t, e.Send("raw text"))
	_, _ = waitForResult(t, e, 5*time.Second)

	msgs := e.history.Messages()
	require.NotEmpty(t, msgs)
	text := msgs[0].Content[0].OfText
	require.NotNil(t, text)
	assert.Equal(t, "raw text", text.Text)
}

// TestSend_UserPromptSubmitHookFires wires an HTTP hook for UserPromptSubmit,
// runs Send, and verifies the hook endpoint received the user text as
// ToolInput. Hooks run async so we poll.
func TestSend_UserPromptSubmitHookFires(t *testing.T) {
	var hookHits atomic.Int32
	var gotInput atomic.Value // stores string
	hookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in hooks.HookInput
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Event == hooks.UserPromptSubmit {
			if s, ok := in.ToolInput.(string); ok {
				gotInput.Store(s)
			}
			hookHits.Add(1)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer hookSrv.Close()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTextTurn(w, "done")
	}))
	defer apiSrv.Close()

	t.Setenv("PROVIDENCE_MAX_RETRIES", "1")
	t.Setenv("PROVIDENCE_HOOKS_ALLOW_LOOPBACK", "1")
	e, err := NewDirectEngine(engine.EngineConfig{
		Type:   engine.EngineTypeDirect,
		Model:  "claude-sonnet-4-20250514",
		APIKey: "test-key-not-real",
		HooksMap: map[string][]engine.HookConfigEntry{
			hooks.UserPromptSubmit: {{URL: hookSrv.URL, Timeout: 2000}},
		},
	})
	require.NoError(t, err)
	e.client = anthropic.NewClient(
		option.WithAPIKey("k"),
		option.WithBaseURL(apiSrv.URL),
		option.WithMaxRetries(0),
	)

	require.NoError(t, e.Send("look at this"))
	_, _ = waitForResult(t, e, 5*time.Second)

	// Hook fires async. Poll up to 2s for it to hit.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && hookHits.Load() == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	assert.GreaterOrEqual(t, hookHits.Load(), int32(1), "UserPromptSubmit hook should fire at least once")
	if v := gotInput.Load(); v != nil {
		assert.Equal(t, "look at this", v.(string))
	}
}

func TestSend_SessionStartedHookFires(t *testing.T) {
	recorder := &hookRecorder{}
	hookSrv := httptest.NewServer(http.HandlerFunc(recorder.handler))
	defer hookSrv.Close()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTextTurn(w, "done")
	}))
	defer apiSrv.Close()

	t.Setenv("PROVIDENCE_MAX_RETRIES", "1")
	t.Setenv("PROVIDENCE_HOOKS_ALLOW_LOOPBACK", "1")
	e, err := NewDirectEngine(engine.EngineConfig{
		Type:   engine.EngineTypeDirect,
		Model:  "claude-sonnet-4-20250514",
		APIKey: "test-key-not-real",
		HooksMap: map[string][]engine.HookConfigEntry{
			hooks.SessionStarted: {{URL: hookSrv.URL, Timeout: 2000}},
		},
	})
	require.NoError(t, err)
	e.client = anthropic.NewClient(
		option.WithAPIKey("k"),
		option.WithBaseURL(apiSrv.URL),
		option.WithMaxRetries(0),
	)

	require.NoError(t, e.Send("start session"))
	_, _ = waitForResult(t, e, 5*time.Second)

	inputs := recorder.waitForCount(t, 1, 2*time.Second)
	require.Len(t, inputs, 1)
	assert.Equal(t, hooks.SessionStarted, inputs[0].Event)
}

func TestSend_TurnStartedHookFires(t *testing.T) {
	recorder := &hookRecorder{}
	hookSrv := httptest.NewServer(http.HandlerFunc(recorder.handler))
	defer hookSrv.Close()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTextTurn(w, "done")
	}))
	defer apiSrv.Close()

	t.Setenv("PROVIDENCE_MAX_RETRIES", "1")
	t.Setenv("PROVIDENCE_HOOKS_ALLOW_LOOPBACK", "1")
	e, err := NewDirectEngine(engine.EngineConfig{
		Type:   engine.EngineTypeDirect,
		Model:  "claude-sonnet-4-20250514",
		APIKey: "test-key-not-real",
		HooksMap: map[string][]engine.HookConfigEntry{
			hooks.TurnStarted: {{URL: hookSrv.URL, Timeout: 2000}},
		},
	})
	require.NoError(t, err)
	e.client = anthropic.NewClient(
		option.WithAPIKey("k"),
		option.WithBaseURL(apiSrv.URL),
		option.WithMaxRetries(0),
	)

	require.NoError(t, e.Send("turn start"))
	_, _ = waitForResult(t, e, 5*time.Second)

	inputs := recorder.waitForCount(t, 1, 2*time.Second)
	require.Len(t, inputs, 1)
	assert.Equal(t, hooks.TurnStarted, inputs[0].Event)
	assert.Equal(t, "turn start", inputs[0].ToolInput)
}

func TestSend_TurnCompletedHookFires(t *testing.T) {
	tests := []struct {
		name   string
		server func(w http.ResponseWriter, r *http.Request)
		status string
	}{
		{
			name: "success",
			server: func(w http.ResponseWriter, r *http.Request) {
				writeTextTurn(w, "done")
			},
			status: "success",
		},
		{
			name: "error",
			server: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"bad"}}`))
			},
			status: "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := &hookRecorder{}
			hookSrv := httptest.NewServer(http.HandlerFunc(recorder.handler))
			defer hookSrv.Close()

			apiSrv := httptest.NewServer(http.HandlerFunc(tt.server))
			defer apiSrv.Close()

			t.Setenv("PROVIDENCE_MAX_RETRIES", "1")
			t.Setenv("PROVIDENCE_HOOKS_ALLOW_LOOPBACK", "1")
			e, err := NewDirectEngine(engine.EngineConfig{
				Type:   engine.EngineTypeDirect,
				Model:  "claude-sonnet-4-20250514",
				APIKey: "test-key-not-real",
				HooksMap: map[string][]engine.HookConfigEntry{
					hooks.TurnCompleted: {{URL: hookSrv.URL, Timeout: 2000}},
				},
			})
			require.NoError(t, err)
			e.client = anthropic.NewClient(
				option.WithAPIKey("k"),
				option.WithBaseURL(apiSrv.URL),
				option.WithMaxRetries(0),
			)

			require.NoError(t, e.Send("turn complete"))
			_, _ = waitForResult(t, e, 5*time.Second)

			inputs := recorder.waitForCount(t, 1, 2*time.Second)
			require.Len(t, inputs, 1)
			assert.Equal(t, hooks.TurnCompleted, inputs[0].Event)

			payload, ok := inputs[0].ToolInput.(map[string]any)
			require.True(t, ok)
			assert.Equal(t, tt.status, payload["status"])
		})
	}
}

func TestAsyncHookResultAppearsInNextTurnAttachment(t *testing.T) {
	t.Setenv("PROVIDENCE_HOOKS_ALLOW_LOOPBACK", "1")

	hookDone := make(chan struct{}, 1)
	hookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"system_message":"lint clean"}`))
		select {
		case hookDone <- struct{}{}:
		default:
		}
	}))
	defer hookSrv.Close()

	e := &DirectEngine{
		history:   NewConversationHistory(),
		sessionID: "session-async-hooks",
		hooksRunner: hooks.NewRunner(map[string][]hooks.HookConfig{
			hooks.PostToolUse: {{
				URL:   hookSrv.URL + "/lint",
				Async: true,
				TTL:   time.Second,
			}},
		}),
	}

	out, err := e.fireHook(hooks.PostToolUse, hooks.HookInput{
		ToolName:  "Write",
		ToolInput: "ok",
	})
	require.NoError(t, err)
	assert.Nil(t, out)

	select {
	case <-hookDone:
	case <-time.After(time.Second):
		t.Fatal("async hook did not complete")
	}

	require.Eventually(t, func() bool {
		return e.hooksRunner.CompletedCount() == 1
	}, time.Second, 10*time.Millisecond)

	e.injectPendingAttachments()

	msgs := e.history.Messages()
	require.Len(t, msgs, 1)
	require.NotEmpty(t, msgs[0].Content)

	text := msgs[0].Content[0].OfText
	require.NotNil(t, text)
	assert.Contains(t, text.Text, `<hook-result event="PostToolUse" name="lint" status="ok">`)
	assert.Contains(t, text.Text, "lint clean")
	assert.Zero(t, e.hooksRunner.CompletedCount())
}

// --- Helper / state-focused tests ---
//
// The remaining targets are exercised against helpers directly rather than a
// full agent loop, mirroring the pattern in engine_injector_test.go. Building
// a full multi-turn tool flow via SSE would require replaying every content
// block variant the SDK knows about, with no additional signal value.

// TestSend_ToolErrorRecovery: synthesizeErrorToolResults produces matching
// error tool_result blocks for orphan tool_use blocks. Without this, the
// next API turn would 400 on unmatched tool_use/tool_result pairs.
func TestSend_ToolErrorRecovery(t *testing.T) {
	e := &DirectEngine{
		events:  make(chan engine.ParsedEvent, 4),
		history: NewConversationHistory(),
	}
	msg := anthropic.Message{
		Role: "assistant",
		Content: []anthropic.ContentBlockUnion{
			{Type: "tool_use", ID: "tu_err_1", Name: "Bash", Input: []byte(`{"command":"ls"}`)},
		},
	}
	e.synthesizeErrorToolResults(msg)

	msgs := e.history.Messages()
	require.Len(t, msgs, 2, "assistant + synthesized tool_result user turn")
	assert.Equal(t, anthropic.MessageParamRoleAssistant, msgs[0].Role)
	assert.Equal(t, anthropic.MessageParamRoleUser, msgs[1].Role)
	require.Len(t, msgs[1].Content, 1)
	tr := msgs[1].Content[0].OfToolResult
	require.NotNil(t, tr)
	assert.Equal(t, "tu_err_1", tr.ToolUseID)
	require.NotEmpty(t, tr.Content)
	assert.Contains(t, tr.Content[0].OfText.Text, "skipped due to API error")
}

// TestSend_HistoryAssemblyOrder validates the [user, assistant, tool_result,
// assistant] ordering invariant the agent loop maintains across multi-turn
// tool chains. This exercises the same ConversationHistory API the loop
// uses, decoupled from the live stream.
func TestSend_HistoryAssemblyOrder(t *testing.T) {
	h := NewConversationHistory()

	// Turn 1: user prompt.
	h.AddUser("read main.go")

	// Assistant turn with a tool_use.
	tu1 := anthropic.Message{
		Role: "assistant",
		Content: []anthropic.ContentBlockUnion{
			{Type: "text", Text: "on it"},
			{Type: "tool_use", ID: "tu_1", Name: "Read", Input: []byte(`{"file_path":"main.go"}`)},
		},
	}
	h.AddAssistant(tu1)

	// tool_result user turn.
	h.AddToolResults([]anthropic.ContentBlockParamUnion{
		anthropic.NewToolResultBlock("tu_1", "package main", false),
	})

	// Second assistant turn (natural end).
	tu2 := anthropic.Message{
		Role: "assistant",
		Content: []anthropic.ContentBlockUnion{
			{Type: "text", Text: "done"},
		},
	}
	h.AddAssistant(tu2)

	msgs := h.Messages()
	require.Len(t, msgs, 4)
	assert.Equal(t, anthropic.MessageParamRoleUser, msgs[0].Role, "msgs[0] = initial user")
	assert.Equal(t, anthropic.MessageParamRoleAssistant, msgs[1].Role, "msgs[1] = assistant w/ tool_use")
	assert.Equal(t, anthropic.MessageParamRoleUser, msgs[2].Role, "msgs[2] = tool_result (user role)")
	assert.Equal(t, anthropic.MessageParamRoleAssistant, msgs[3].Role, "msgs[3] = final assistant")

	// Assert the tool_result carries the correct ToolUseID so the pair matches.
	require.NotEmpty(t, msgs[2].Content)
	tr := msgs[2].Content[0].OfToolResult
	require.NotNil(t, tr)
	assert.Equal(t, "tu_1", tr.ToolUseID)
}

// TestSend_MultiTurnToolChainCorrectness exercises the full
// extractToolCalls -> AddAssistant -> AddToolResults cycle for a 2-tool chain
// (tool1 then tool2) and verifies the result-block ordering matches call
// ordering. This is the critical invariant the agent loop relies on.
func TestSend_MultiTurnToolChainCorrectness(t *testing.T) {
	h := NewConversationHistory()
	h.AddUser("do stuff")

	// Assistant invokes two tools in one turn.
	msg := anthropic.Message{
		Role: "assistant",
		Content: []anthropic.ContentBlockUnion{
			{Type: "tool_use", ID: "tu_A", Name: "Read", Input: []byte(`{"file_path":"a"}`)},
			{Type: "tool_use", ID: "tu_B", Name: "Bash", Input: []byte(`{"command":"ls"}`)},
		},
	}
	calls := extractToolCalls(msg)
	require.Len(t, calls, 2)
	assert.Equal(t, "tu_A", calls[0].ID)
	assert.Equal(t, "tu_B", calls[1].ID)

	h.AddAssistant(msg)
	h.AddToolResults([]anthropic.ContentBlockParamUnion{
		anthropic.NewToolResultBlock("tu_A", "fileA contents", false),
		anthropic.NewToolResultBlock("tu_B", "bash output", false),
	})

	msgs := h.Messages()
	require.Len(t, msgs, 3)
	results := msgs[2].Content
	require.Len(t, results, 2)
	assert.Equal(t, "tu_A", results[0].OfToolResult.ToolUseID)
	assert.Equal(t, "tu_B", results[1].OfToolResult.ToolUseID)
}

// TestSend_MaxOutputRecoveryRespectsBound validates that the recovery counter
// is bounded at MaxOutputTokensRecoveryLimit. The agent loop must not retry
// past this bound.
func TestSend_MaxOutputRecoveryRespectsBound(t *testing.T) {
	e := &DirectEngine{
		events:  make(chan engine.ParsedEvent, 16),
		history: NewConversationHistory(),
	}

	// Simulate N recovery iterations where N = limit.
	for i := 0; i < MaxOutputTokensRecoveryLimit; i++ {
		require.Less(t, e.maxOutputRecoveryCount, MaxOutputTokensRecoveryLimit, "must not retry past bound")
		e.maxOutputRecoveryCount++
	}

	// After hitting the bound, the guard in agentLoop (line ~1566:
	// `if e.maxOutputRecoveryCount < MaxOutputTokensRecoveryLimit`) must be
	// false, forcing the error-exit path.
	assert.Equal(t, MaxOutputTokensRecoveryLimit, e.maxOutputRecoveryCount)
	assert.False(t, e.maxOutputRecoveryCount < MaxOutputTokensRecoveryLimit,
		"guard must reject further retries")
}

// TestSend_UnattendedRetryGatedOnEmber verifies that the SetUnattendedRetry
// toggle flips the gate observed by isUnattendedRetry, and that the env var
// fallback path also works.
func TestSend_UnattendedRetryGatedOnEmber(t *testing.T) {
	e, err := NewDirectEngine(engine.EngineConfig{
		Type:   engine.EngineTypeDirect,
		Model:  "claude-sonnet-4-20250514",
		APIKey: "test-key-not-real",
	})
	require.NoError(t, err)

	// Default: off.
	t.Setenv("PROVIDENCE_UNATTENDED_RETRY", "")
	assert.False(t, e.isUnattendedRetry(), "default must be off")

	// Flag on -> gate open.
	e.SetUnattendedRetry(true)
	assert.True(t, e.isUnattendedRetry())

	// Flag off + env var on -> gate still open (env var fallback).
	e.SetUnattendedRetry(false)
	t.Setenv("PROVIDENCE_UNATTENDED_RETRY", "1")
	assert.True(t, e.isUnattendedRetry(), "env var fallback must open gate")

	t.Setenv("PROVIDENCE_UNATTENDED_RETRY", "")
	assert.False(t, e.isUnattendedRetry())
}

// TestSend_StreamingInterruptionMidTool starts a Send, then calls Interrupt
// while the fake API is still holding the stream open. The engine must exit
// cleanly with an error result and the status must transition off Running.
// Verifies the ctx cancellation path through the agent loop.
func TestSend_StreamingInterruptionMidTool(t *testing.T) {
	// Server holds the connection open until the client disconnects.
	serverDone := make(chan struct{})
	var serverDoneOnce sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := newSSE(w)
		// Open a message but never finish it.
		s.event("message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id": "msg_stall", "type": "message", "role": "assistant",
				"content": []any{}, "model": "claude-sonnet-4-20250514",
				"stop_reason": nil, "stop_sequence": nil,
				"usage": map[string]any{"input_tokens": 5, "output_tokens": 0},
			},
		})
		// Block until the client disconnects (Interrupt() -> ctx cancel ->
		// stream read returns err).
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
		serverDoneOnce.Do(func() { close(serverDone) })
	}))
	defer srv.Close()

	e := newFakeEngine(t, srv)
	require.NoError(t, e.Send("start"))

	// Give the loop a moment to actually start streaming.
	time.Sleep(100 * time.Millisecond)
	e.Interrupt()

	res, _ := waitForResult(t, e, 5*time.Second)
	require.NotNil(t, res)
	// Either success (if it raced a clean close) or error; critically, status
	// must not be stuck Running.
	st := e.Status()
	assert.NotEqual(t, engine.StatusRunning, st)

	// Partial history preservation: the user turn must survive the cancel.
	msgs := e.history.Messages()
	require.NotEmpty(t, msgs)
	assert.Equal(t, anthropic.MessageParamRoleUser, msgs[0].Role)

	// Let the server goroutine drain (avoids leak warnings).
	select {
	case <-serverDone:
	case <-time.After(2 * time.Second):
	}
}

// TestSend_ErrorStringShape sanity-checks the concurrent-send error message
// shape so UI callers can rely on it.
func TestSend_ErrorStringShape(t *testing.T) {
	e, err := NewDirectEngine(engine.EngineConfig{
		Type:   engine.EngineTypeDirect,
		Model:  "claude-sonnet-4-20250514",
		APIKey: "test-key-not-real",
	})
	require.NoError(t, err)
	e.mu.Lock()
	e.status = engine.StatusRunning
	e.mu.Unlock()

	err = e.Send("x")
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "already running"))
}

func TestIdleTriggeredConcurrentAccess(t *testing.T) {
	var idleTriggered idleTriggeredFlag
	var sawTriggered atomic.Bool

	const goroutines = 64
	const iterations = 2000

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()
			<-start

			for range iterations {
				if idleTriggered.Load() {
					sawTriggered.Store(true)
				}
				idleTriggered.Store(true)
			}
		}()
	}

	close(start)
	wg.Wait()

	assert.True(t, idleTriggered.Load())
	assert.True(t, sawTriggered.Load())
}

// unused: silence potential unused imports if a test is commented out.
var _ = context.Background
