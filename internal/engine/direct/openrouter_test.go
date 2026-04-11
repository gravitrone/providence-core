package direct

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/gravitrone/providence-core/internal/engine/direct/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildOpenRouterTools(t *testing.T) {
	fs := tools.NewFileState()
	registry := tools.NewRegistry(
		tools.NewReadTool(fs),
		&tools.BashTool{},
	)

	out := buildOpenRouterTools(registry)
	require.Len(t, out, 2)

	// Every entry must be an OpenAI function-style tool.
	names := make(map[string]bool)
	for _, tool := range out {
		assert.Equal(t, "function", tool.Type)
		assert.NotEmpty(t, tool.Function.Name)
		assert.NotEmpty(t, tool.Function.Description)
		assert.Equal(t, "object", tool.Function.Parameters["type"])
		names[tool.Function.Name] = true
	}
	assert.True(t, names["Read"], "Read tool should be present")
	assert.True(t, names["Bash"], "Bash tool should be present")
}

func TestBuildOpenRouterMessages_WithSystem(t *testing.T) {
	history := []openrouterHistoryEntry{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	}
	msgs := buildOpenRouterMessages("be helpful", history)
	require.Len(t, msgs, 3)
	assert.Equal(t, "system", msgs[0].Role)
	assert.Equal(t, "be helpful", msgs[0].Content)
	assert.Equal(t, "user", msgs[1].Role)
	assert.Equal(t, "hi", msgs[1].Content)
	assert.Equal(t, "assistant", msgs[2].Role)
	assert.Equal(t, "hello", msgs[2].Content)
}

func TestBuildOpenRouterMessages_NoSystem(t *testing.T) {
	history := []openrouterHistoryEntry{
		{Role: "user", Content: "hi"},
	}
	msgs := buildOpenRouterMessages("", history)
	require.Len(t, msgs, 1)
	assert.Equal(t, "user", msgs[0].Role)
}

func TestBuildOpenRouterMessages_ToolCallRoundTrip(t *testing.T) {
	history := []openrouterHistoryEntry{
		{Role: "user", Content: "read file.txt"},
		{
			Role:    "assistant",
			Content: "",
			ToolCalls: []openrouterToolCallMsg{
				{
					ID:   "call_1",
					Type: "function",
					Function: openrouterToolCallFuncMsg{
						Name:      "Read",
						Arguments: `{"file_path":"file.txt"}`,
					},
				},
			},
		},
		{Role: "tool", CallID: "call_1", Content: "file contents"},
	}
	msgs := buildOpenRouterMessages("", history)
	require.Len(t, msgs, 3)

	// Assistant message must carry tool_calls and render cleanly to JSON in
	// the exact OpenAI chat completions shape.
	assistant := msgs[1]
	assert.Equal(t, "assistant", assistant.Role)
	require.Len(t, assistant.ToolCalls, 1)
	assert.Equal(t, "call_1", assistant.ToolCalls[0].ID)
	assert.Equal(t, "function", assistant.ToolCalls[0].Type)
	assert.Equal(t, "Read", assistant.ToolCalls[0].Function.Name)

	// Tool message maps CallID -> tool_call_id.
	tool := msgs[2]
	assert.Equal(t, "tool", tool.Role)
	assert.Equal(t, "call_1", tool.ToolCallID)
	assert.Equal(t, "file contents", tool.Content)

	// Round-trip through JSON so we catch any missing struct tags.
	raw, err := json.Marshal(assistant)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"tool_calls"`)
	assert.Contains(t, string(raw), `"call_1"`)
}

func TestCompressOpenRouterToolResults(t *testing.T) {
	longResult := strings.Repeat("x", 5000)
	shortResult := strings.Repeat("y", 300)

	msgs := []openrouterMessage{
		{Role: "user", Content: "user-0"},
		{Role: "tool", Content: longResult, ToolCallID: "call_old_long"},
		{Role: "tool", Content: shortResult, ToolCallID: "call_old_short"},
		{Role: "assistant", Content: "assistant-3"},
		{Role: "tool", Content: longResult, ToolCallID: "call_recent_long"},
		{Role: "user", Content: "user-5"},
		{Role: "assistant", Content: "assistant-6"},
	}

	compressed := compressOpenRouterToolResults(msgs, 2000)
	require.Equal(t, 1, compressed)

	assert.Equal(t, "[compressed: 5000 chars from tool_call_id=call_old_long]", msgs[1].Content)
	assert.Equal(t, shortResult, msgs[2].Content)
	assert.Equal(t, longResult, msgs[4].Content)
}

func TestParseOpenRouterStream_TextOnly(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"Hello"}}]}`,
		`data: {"choices":[{"delta":{"content":" world"}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
		"",
	}, "\n")

	e := newOpenRouterTestEngine()
	// Drain events asynchronously so the parser doesn't block on a full channel.
	done := drainEvents(e)
	defer done()

	textParts, toolCalls, err := e.parseOpenRouterStream(context.Background(), io.NopCloser(bytes.NewBufferString(sse)))
	require.NoError(t, err)
	assert.Equal(t, []string{"Hello", " world"}, textParts)
	assert.Empty(t, toolCalls)
}

func TestParseOpenRouterStream_ToolCall(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"Read","arguments":"{\"file"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"_path\":\"/tmp/x\"}"}}]}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
		"",
	}, "\n")

	e := newOpenRouterTestEngine()
	done := drainEvents(e)
	defer done()

	textParts, toolCalls, err := e.parseOpenRouterStream(context.Background(), io.NopCloser(bytes.NewBufferString(sse)))
	require.NoError(t, err)
	assert.Empty(t, textParts)
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "call_1", toolCalls[0].ID)
	assert.Equal(t, "Read", toolCalls[0].Name)
	assert.Equal(t, `{"file_path":"/tmp/x"}`, toolCalls[0].RawArgs)
}

func TestParseOpenRouterStream_MultipleToolCalls(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"Read","arguments":"{}"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_b","type":"function","function":{"name":"Bash","arguments":"{\"command\":\"ls\"}"}}]}}]}`,
		`data: [DONE]`,
		"",
	}, "\n")

	e := newOpenRouterTestEngine()
	done := drainEvents(e)
	defer done()

	_, toolCalls, err := e.parseOpenRouterStream(context.Background(), io.NopCloser(bytes.NewBufferString(sse)))
	require.NoError(t, err)
	require.Len(t, toolCalls, 2)
	assert.Equal(t, "call_a", toolCalls[0].ID)
	assert.Equal(t, "Read", toolCalls[0].Name)
	assert.Equal(t, "call_b", toolCalls[1].ID)
	assert.Equal(t, "Bash", toolCalls[1].Name)
}

func TestParseOpenRouterStream_SkipsMalformedLines(t *testing.T) {
	sse := strings.Join([]string{
		`: heartbeat comment`,
		`data: not-json`,
		`data: {"choices":[{"delta":{"content":"ok"}}]}`,
		`data: [DONE]`,
		"",
	}, "\n")

	e := newOpenRouterTestEngine()
	done := drainEvents(e)
	defer done()

	textParts, toolCalls, err := e.parseOpenRouterStream(context.Background(), io.NopCloser(bytes.NewBufferString(sse)))
	require.NoError(t, err)
	assert.Equal(t, []string{"ok"}, textParts)
	assert.Empty(t, toolCalls)
}

func TestOpenRouterUsageParsing(t *testing.T) {
	requests := make(chan openrouterRequest, 1)

	server := newSandboxSafeServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var req openrouterRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("decode request body: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		requests <- req

		w.Header().Set("Content-Type", "text/event-stream")
		if _, err := io.WriteString(w, strings.Join([]string{
			`data: {"choices":[{"delta":{"content":"Hello"}}]}`,
			`data: {"choices":[],"usage":{"prompt_tokens":11,"completion_tokens":4,"total_tokens":15}}`,
			`data: [DONE]`,
			"",
		}, "\n")); err != nil {
			t.Errorf("write sse response: %v", err)
		}
	}))
	if server == nil {
		return
	}
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	originalClient := http.DefaultClient
	http.DefaultClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			cloned := req.Clone(req.Context())
			cloned.URL.Scheme = serverURL.Scheme
			cloned.URL.Host = serverURL.Host
			return http.DefaultTransport.RoundTrip(cloned)
		}),
	}
	defer func() {
		http.DefaultClient = originalClient
	}()

	e := &DirectEngine{
		events:           make(chan engine.ParsedEvent, 64),
		history:          NewConversationHistory(),
		registry:         tools.NewRegistry(),
		permissions:      NewPermissionHandler(),
		model:            "anthropic/claude-sonnet-4-5",
		system:           "be helpful",
		openrouterAPIKey: "test-key",
		openrouterHistory: []openrouterHistoryEntry{
			{Role: "user", Content: "hello"},
		},
	}

	e.openrouterAgentLoop(context.Background())

	select {
	case req := <-requests:
		require.NotNil(t, req.StreamOptions)
		assert.True(t, req.StreamOptions.IncludeUsage)
		assert.True(t, req.Stream)
	default:
		t.Fatal("expected an OpenRouter request")
	}

	assert.Equal(t, 15, e.history.CurrentTokens())

	var usageEvent *engine.UsageUpdateEvent
	for len(e.events) > 0 {
		event := <-e.events
		if event.Type != "usage_update" {
			continue
		}

		var ok bool
		usageEvent, ok = event.Data.(*engine.UsageUpdateEvent)
		require.True(t, ok)
	}

	require.NotNil(t, usageEvent)
	assert.Equal(t, 11, usageEvent.InputTokens)
	assert.Equal(t, 4, usageEvent.OutputTokens)
	assert.Equal(t, 15, usageEvent.TotalTokens)
}

// newOpenRouterTestEngine builds a minimal DirectEngine just for stream parser
// tests - only the pieces parseOpenRouterStream actually touches are wired.
func newOpenRouterTestEngine() *DirectEngine {
	return &DirectEngine{
		events: make(chan engine.ParsedEvent, 64),
	}
}

// drainEvents continuously reads from the engine's event channel so that the
// parser's sends never block. Call the returned stop func to end the drain.
func drainEvents(e *DirectEngine) func() {
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-e.events:
			}
		}
	}()
	return func() { close(stop) }
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func newSandboxSafeServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()

	var server *httptest.Server
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Skipf("httptest.NewServer unavailable in this sandbox: %v", recovered)
		}
	}()

	server = httptest.NewServer(handler)
	return server
}
