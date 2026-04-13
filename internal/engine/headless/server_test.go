package headless

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/gravitrone/providence-core/internal/engine/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock Engine ---

type mockEngine struct {
	events     chan engine.ParsedEvent
	sent       []string
	interrupted bool
	closed     bool
	permResp   map[string]string // questionID -> optionID
}

func newMockEngine() *mockEngine {
	return &mockEngine{
		events:   make(chan engine.ParsedEvent, 64),
		permResp: make(map[string]string),
	}
}

func (m *mockEngine) Send(text string) error {
	m.sent = append(m.sent, text)
	return nil
}

func (m *mockEngine) Events() <-chan engine.ParsedEvent {
	return m.events
}

func (m *mockEngine) RespondPermission(questionID, optionID string) error {
	m.permResp[questionID] = optionID
	return nil
}

func (m *mockEngine) Interrupt() {
	m.interrupted = true
}

func (m *mockEngine) Cancel() {}

func (m *mockEngine) Close() {
	m.closed = true
	close(m.events)
}

func (m *mockEngine) Status() engine.SessionStatus {
	return engine.StatusIdle
}

func (m *mockEngine) RestoreHistory(_ []engine.RestoredMessage) error {
	return nil
}

func (m *mockEngine) TriggerCompact(_ context.Context) error {
	return nil
}

func (m *mockEngine) SessionBus() *session.Bus {
	return session.NewBus()
}

// --- Tests ---

func TestServerEmitsInitOnStart(t *testing.T) {
	eng := newMockEngine()
	stdin := strings.NewReader("") // EOF immediately
	var stdout bytes.Buffer

	srv := NewServer(eng, stdin, &stdout, "claude-sonnet-4", "direct")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := srv.Run(ctx)
	require.NoError(t, err)

	// Parse the first line of output.
	lines := nonEmptyLines(stdout.String())
	require.NotEmpty(t, lines, "should emit at least one NDJSON line")

	var initEv OutputEvent
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &initEv))

	assert.Equal(t, TypeSystem, initEv.Type)
	assert.Equal(t, SubtypeInit, initEv.Subtype)
	assert.Equal(t, "claude-sonnet-4", initEv.Model)
	assert.Equal(t, "direct", initEv.Engine)
	assert.Equal(t, "1.0", initEv.Version)
	assert.NotEmpty(t, initEv.SessionID)
}

func TestServerForwardsUserMessage(t *testing.T) {
	eng := newMockEngine()

	input := `{"type":"user","message":{"content":"hello world"}}` + "\n"
	stdin := strings.NewReader(input)
	var stdout bytes.Buffer

	srv := NewServer(eng, stdin, &stdout, "test-model", "test-engine")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := srv.Run(ctx)
	require.NoError(t, err)

	require.Len(t, eng.sent, 1)
	assert.Equal(t, "hello world", eng.sent[0])
}

func TestServerSkipsEmptyContent(t *testing.T) {
	eng := newMockEngine()

	input := `{"type":"user","message":{"content":""}}` + "\n"
	stdin := strings.NewReader(input)
	var stdout bytes.Buffer

	srv := NewServer(eng, stdin, &stdout, "test-model", "test-engine")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_ = srv.Run(ctx)
	assert.Empty(t, eng.sent)
}

func TestServerHandlesInvalidJSON(t *testing.T) {
	eng := newMockEngine()

	input := "not json at all\n"
	stdin := strings.NewReader(input)
	var stdout bytes.Buffer

	srv := NewServer(eng, stdin, &stdout, "test-model", "test-engine")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_ = srv.Run(ctx)

	lines := nonEmptyLines(stdout.String())
	// Should have init + error.
	require.GreaterOrEqual(t, len(lines), 2)

	var errEv OutputEvent
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &errEv))
	assert.Equal(t, "error", errEv.Type)
	assert.Contains(t, errEv.Error, "invalid json")
}

func TestServerHandlesControlRequestInterrupt(t *testing.T) {
	eng := newMockEngine()

	input := `{"type":"control_request","subtype":"interrupt"}` + "\n"
	stdin := strings.NewReader(input)
	var stdout bytes.Buffer

	srv := NewServer(eng, stdin, &stdout, "test-model", "test-engine")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_ = srv.Run(ctx)
	assert.True(t, eng.interrupted)
}

func TestServerHandlesControlRequestInitialize(t *testing.T) {
	eng := newMockEngine()

	input := `{"type":"control_request","subtype":"initialize"}` + "\n"
	stdin := strings.NewReader(input)
	var stdout bytes.Buffer

	srv := NewServer(eng, stdin, &stdout, "test-model", "test-engine")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_ = srv.Run(ctx)

	lines := nonEmptyLines(stdout.String())
	// init + initialized response.
	require.GreaterOrEqual(t, len(lines), 2)

	var resp OutputEvent
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &resp))
	assert.Equal(t, TypeSystem, resp.Type)
	assert.Equal(t, "initialized", resp.Subtype)
}

func TestServerHandlesControlResponse(t *testing.T) {
	eng := newMockEngine()

	input := `{"type":"control_response","request_id":"q-123","response":{"option_id":"allow"}}` + "\n"
	stdin := strings.NewReader(input)
	var stdout bytes.Buffer

	srv := NewServer(eng, stdin, &stdout, "test-model", "test-engine")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_ = srv.Run(ctx)

	assert.Equal(t, "allow", eng.permResp["q-123"])
}

func TestServerTranslatesAssistantEvent(t *testing.T) {
	eng := newMockEngine()

	// Pre-load an assistant event.
	eng.events <- engine.ParsedEvent{
		Type: "assistant",
		Data: &engine.AssistantEvent{
			Type: "assistant",
			Message: engine.AssistantMsg{
				Content: []engine.ContentPart{
					{Type: "text", Text: "hello from AI"},
				},
			},
		},
	}

	stdin := strings.NewReader("")
	var stdout bytes.Buffer

	srv := NewServer(eng, stdin, &stdout, "test-model", "test-engine")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_ = srv.Run(ctx)

	lines := nonEmptyLines(stdout.String())
	// init + assistant event.
	found := false
	for _, line := range lines {
		var ev OutputEvent
		if err := json.Unmarshal([]byte(line), &ev); err == nil && ev.Type == TypeAssistant {
			found = true
			require.NotNil(t, ev.Message)
			require.Len(t, ev.Message.Content, 1)
			assert.Equal(t, "hello from AI", ev.Message.Content[0].Text)
		}
	}
	assert.True(t, found, "should have emitted an assistant event")
}

func TestServerTranslatesResultEvent(t *testing.T) {
	eng := newMockEngine()

	eng.events <- engine.ParsedEvent{
		Type: "result",
		Data: &engine.ResultEvent{
			Type:    "result",
			Subtype: "success",
			Result:  "done",
		},
	}

	stdin := strings.NewReader("")
	var stdout bytes.Buffer

	srv := NewServer(eng, stdin, &stdout, "test-model", "test-engine")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_ = srv.Run(ctx)

	lines := nonEmptyLines(stdout.String())
	found := false
	for _, line := range lines {
		var ev OutputEvent
		if err := json.Unmarshal([]byte(line), &ev); err == nil && ev.Type == TypeResult {
			found = true
			assert.Equal(t, "success", ev.Subtype)
			assert.Equal(t, "done", ev.Result)
		}
	}
	assert.True(t, found, "should have emitted a result event")
}

func TestServerTranslatesErrorEvent(t *testing.T) {
	eng := newMockEngine()

	eng.events <- engine.ParsedEvent{
		Err: assert.AnError,
	}

	stdin := strings.NewReader("")
	var stdout bytes.Buffer

	srv := NewServer(eng, stdin, &stdout, "test-model", "test-engine")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_ = srv.Run(ctx)

	lines := nonEmptyLines(stdout.String())
	found := false
	for _, line := range lines {
		var ev OutputEvent
		if err := json.Unmarshal([]byte(line), &ev); err == nil && ev.Type == TypeResult && ev.IsError {
			found = true
			assert.Contains(t, ev.Error, "assert.AnError")
		}
	}
	assert.True(t, found, "should have emitted an error result event")
}

func TestServerTranslatesUsageUpdate(t *testing.T) {
	eng := newMockEngine()

	eng.events <- engine.ParsedEvent{
		Type: "usage_update",
		Data: &engine.UsageUpdateEvent{
			Type:         "usage_update",
			InputTokens:  100,
			OutputTokens: 50,
			TotalTokens:  150,
		},
	}

	stdin := strings.NewReader("")
	var stdout bytes.Buffer

	srv := NewServer(eng, stdin, &stdout, "test-model", "test-engine")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_ = srv.Run(ctx)

	lines := nonEmptyLines(stdout.String())
	found := false
	for _, line := range lines {
		var ev OutputEvent
		if err := json.Unmarshal([]byte(line), &ev); err == nil && ev.Type == "usage_update" {
			found = true
			assert.Equal(t, 100, ev.InputTokens)
			assert.Equal(t, 50, ev.OutputTokens)
			assert.Equal(t, 150, ev.TotalTokens)
		}
	}
	assert.True(t, found, "should have emitted a usage_update event")
}

func TestServerTranslatesPermissionRequest(t *testing.T) {
	eng := newMockEngine()

	eng.events <- engine.ParsedEvent{
		Type: "permission_request",
		Data: &engine.PermissionRequestEvent{
			Type: "permission_request",
			Tool: engine.PermissionTool{
				Name:  "Bash",
				Input: map[string]any{"command": "rm -rf /"},
			},
			QuestionID: "perm-001",
			Options: []engine.PermissionOption{
				{ID: "allow", Label: "Allow"},
				{ID: "deny", Label: "Deny"},
			},
		},
	}

	stdin := strings.NewReader("")
	var stdout bytes.Buffer

	srv := NewServer(eng, stdin, &stdout, "test-model", "test-engine")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_ = srv.Run(ctx)

	lines := nonEmptyLines(stdout.String())
	found := false
	for _, line := range lines {
		var ev OutputEvent
		if err := json.Unmarshal([]byte(line), &ev); err == nil && ev.Type == TypeControlRequest {
			found = true
			require.NotNil(t, ev.Tool)
			assert.Equal(t, "Bash", ev.Tool.Name)
			assert.Equal(t, "perm-001", ev.QuestionID)
			assert.Len(t, ev.Options, 2)
		}
	}
	assert.True(t, found, "should have emitted a control_request for permission")
}

func TestEveryEventCarriesUUIDAndSessionID(t *testing.T) {
	eng := newMockEngine()

	// Inject multiple event types so we can verify all of them get stamped.
	eng.events <- engine.ParsedEvent{
		Type: "assistant",
		Data: &engine.AssistantEvent{
			Type: "assistant",
			Message: engine.AssistantMsg{
				Content: []engine.ContentPart{{Type: "text", Text: "hi"}},
			},
		},
	}
	eng.events <- engine.ParsedEvent{
		Type: "usage_update",
		Data: &engine.UsageUpdateEvent{
			Type:        "usage_update",
			TotalTokens: 42,
		},
	}
	eng.events <- engine.ParsedEvent{
		Type: "result",
		Data: &engine.ResultEvent{
			Type:    "result",
			Subtype: "success",
			Result:  "ok",
		},
	}

	stdin := strings.NewReader("")
	var stdout bytes.Buffer

	srv := NewServer(eng, stdin, &stdout, "test-model", "test-engine")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_ = srv.Run(ctx)

	lines := nonEmptyLines(stdout.String())
	require.GreaterOrEqual(t, len(lines), 4, "init + 3 events")

	seenUUIDs := map[string]bool{}
	for _, line := range lines {
		var ev OutputEvent
		require.NoError(t, json.Unmarshal([]byte(line), &ev))

		assert.NotEmpty(t, ev.UUID, "every event must have a uuid, got type=%s", ev.Type)
		assert.NotEmpty(t, ev.SessionID, "every event must have session_id, got type=%s", ev.Type)

		// UUIDs must be unique per event.
		assert.False(t, seenUUIDs[ev.UUID], "duplicate uuid: %s", ev.UUID)
		seenUUIDs[ev.UUID] = true
	}

	// All events share the same session_id.
	var firstSessionID string
	for _, line := range lines {
		var ev OutputEvent
		_ = json.Unmarshal([]byte(line), &ev)
		if firstSessionID == "" {
			firstSessionID = ev.SessionID
		}
		assert.Equal(t, firstSessionID, ev.SessionID, "all events must share session_id")
	}
}

// --- Helpers ---

func nonEmptyLines(s string) []string {
	var result []string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			result = append(result, line)
		}
	}
	return result
}
