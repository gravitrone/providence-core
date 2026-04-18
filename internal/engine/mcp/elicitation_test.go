package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Client RPC: resources + prompts ---

func TestClientResourcesList(t *testing.T) {
	client, writer := newTestClient(`{"jsonrpc":"2.0","id":1,"result":{"resources":[` +
		`{"uri":"file:///etc/hosts","name":"hosts","description":"system hosts","mimeType":"text/plain"},` +
		`{"uri":"file:///tmp/a","name":"a"}` +
		`]}}` + "\n")

	got, err := client.ListResources()
	require.NoError(t, err)
	require.Len(t, got, 2)

	assert.Equal(t, "file:///etc/hosts", got[0].URI)
	assert.Equal(t, "hosts", got[0].Name)
	assert.Equal(t, "system hosts", got[0].Description)
	assert.Equal(t, "text/plain", got[0].MIMEType)

	assert.Equal(t, "a", got[1].Name)
	assert.Empty(t, got[1].MIMEType)

	wire := writer.String()
	assert.Contains(t, wire, `"method":"resources/list"`)
}

func TestClientReadResourceReturnsContents(t *testing.T) {
	client, writer := newTestClient(`{"jsonrpc":"2.0","id":1,"result":{"contents":[` +
		`{"uri":"file:///a","mimeType":"text/plain","text":"hello"},` +
		`{"uri":"file:///b","mimeType":"application/octet-stream","blob":"YmluYXJ5"}` +
		`]}}` + "\n")

	got, err := client.ReadResource("file:///a")
	require.NoError(t, err)
	require.Len(t, got, 2)

	assert.Equal(t, "hello", got[0].Text)
	assert.Equal(t, "YmluYXJ5", got[1].Blob)
	assert.Contains(t, writer.String(), `"method":"resources/read"`)
	assert.Contains(t, writer.String(), `"uri":"file:///a"`)
}

func TestClientReadResourceRejectsEmptyURI(t *testing.T) {
	client, _ := newTestClient("")
	_, err := client.ReadResource("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty uri")
}

func TestClientPromptsList(t *testing.T) {
	client, writer := newTestClient(`{"jsonrpc":"2.0","id":1,"result":{"prompts":[` +
		`{"name":"summarize","description":"summarize input","arguments":[` +
		`{"name":"text","description":"target text","required":true},` +
		`{"name":"tone","description":"desired tone"}` +
		`]},` +
		`{"name":"translate"}` +
		`]}}` + "\n")

	got, err := client.ListPrompts()
	require.NoError(t, err)
	require.Len(t, got, 2)

	assert.Equal(t, "summarize", got[0].Name)
	assert.Equal(t, "summarize input", got[0].Description)
	require.Len(t, got[0].Arguments, 2)
	assert.Equal(t, "text", got[0].Arguments[0].Name)
	assert.True(t, got[0].Arguments[0].Required)
	assert.False(t, got[0].Arguments[1].Required)

	assert.Equal(t, "translate", got[1].Name)
	assert.Empty(t, got[1].Arguments)

	assert.Contains(t, writer.String(), `"method":"prompts/list"`)
}

func TestClientPromptsGetWithArgs(t *testing.T) {
	client, writer := newTestClient(`{"jsonrpc":"2.0","id":1,"result":{` +
		`"description":"expanded prompt",` +
		`"messages":[` +
		`{"role":"user","content":{"type":"text","text":"summarize this: hello world"}},` +
		`{"role":"assistant","content":{"type":"text","text":"ok"}}` +
		`]}}` + "\n")

	got, err := client.GetPrompt("summarize", map[string]string{
		"text": "hello world",
		"tone": "casual",
	})
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, "expanded prompt", got.Description)
	require.Len(t, got.Messages, 2)
	assert.Equal(t, "user", got.Messages[0].Role)
	assert.Equal(t, "text", got.Messages[0].Content.Type)
	assert.Equal(t, "summarize this: hello world", got.Messages[0].Content.Text)

	wire := writer.String()
	assert.Contains(t, wire, `"method":"prompts/get"`)
	assert.Contains(t, wire, `"name":"summarize"`)
	// Arguments must be passed through as a string map.
	assert.Contains(t, wire, `"text":"hello world"`)
	assert.Contains(t, wire, `"tone":"casual"`)
}

func TestClientPromptsGetRejectsEmptyName(t *testing.T) {
	client, _ := newTestClient("")
	_, err := client.GetPrompt("", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty name")
}

// --- Queue unit tests ---

func TestElicitationQueueEnqueueAndPendingOrder(t *testing.T) {
	q := NewElicitationQueue(time.Minute)
	base := time.Unix(1_700_000_000, 0)
	clock := base
	q.now = func() time.Time { return clock }

	require.NoError(t, q.Enqueue(&Elicitation{ID: "srv:2", ServerName: "srv", Prompt: "second"}))
	clock = base.Add(time.Second)
	require.NoError(t, q.Enqueue(&Elicitation{ID: "srv:1", ServerName: "srv", Prompt: "first"}))

	pending := q.Pending()
	require.Len(t, pending, 2)
	// Oldest CreatedAt first.
	assert.Equal(t, "srv:2", pending[0].ID)
	assert.Equal(t, "srv:1", pending[1].ID)
}

func TestElicitationQueueRejectsEmptyID(t *testing.T) {
	q := NewElicitationQueue(time.Minute)
	err := q.Enqueue(&Elicitation{ID: "", ServerName: "srv"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty id")
}

func TestElicitationQueueTakeRemovesEntry(t *testing.T) {
	q := NewElicitationQueue(time.Minute)
	require.NoError(t, q.Enqueue(&Elicitation{ID: "srv:1", ServerName: "srv"}))

	entry, ok := q.Take("srv:1")
	require.True(t, ok)
	require.NotNil(t, entry)
	assert.Equal(t, "srv:1", entry.ID)

	_, ok2 := q.Take("srv:1")
	assert.False(t, ok2, "take on removed entry should report false")
	assert.Equal(t, 0, q.Len())
}

func TestElicitationTTLExpires(t *testing.T) {
	q := NewElicitationQueue(100 * time.Millisecond)
	base := time.Unix(1_700_000_000, 0)
	clock := base
	q.now = func() time.Time { return clock }

	require.NoError(t, q.Enqueue(&Elicitation{ID: "srv:1", ServerName: "srv", Prompt: "pick a color"}))
	assert.Equal(t, 1, q.Len())
	assert.Len(t, q.Pending(), 1)

	// Advance past the TTL boundary.
	clock = base.Add(150 * time.Millisecond)

	assert.Empty(t, q.Pending(), "pending must evict expired entries")
	assert.Equal(t, 0, q.Len(), "expired entry should be removed on pending sweep")

	// Re-enqueue; take after expiry should fail.
	require.NoError(t, q.Enqueue(&Elicitation{ID: "srv:2", ServerName: "srv"}))
	clock = clock.Add(500 * time.Millisecond)
	_, ok := q.Take("srv:2")
	assert.False(t, ok, "expired entries must not be taken")
}

// --- Manager: elicitation enqueue + resolve ---

func TestElicitationRequestEnqueues(t *testing.T) {
	mgr := NewManager()
	client, _ := newTestClient("")
	mgr.clients["filesystem"] = client
	mgr.bindNotificationHandler("filesystem", client)

	raw := json.RawMessage(`{"message":"pick a file","requestedSchema":{"type":"string"}}`)
	ack, err := mgr.handleServerRequest("filesystem", ServerRequest{
		ID:     7,
		Method: "elicitation/request",
		Params: raw,
	})
	require.NoError(t, err)

	ackMap, ok := ack.(map[string]any)
	require.True(t, ok, "ack must be a json object")
	assert.Equal(t, "pending", ackMap["action"])
	assert.Equal(t, "filesystem:7", ackMap["id"])

	pending := mgr.PendingElicitations()
	require.Len(t, pending, 1)
	assert.Equal(t, "filesystem:7", pending[0].ID)
	assert.Equal(t, "filesystem", pending[0].ServerName)
	assert.Equal(t, "pick a file", pending[0].Prompt)
	assert.Contains(t, string(pending[0].Schema), `"type":"string"`)

	assert.Equal(t, 1, mgr.ElicitationQueueSize())
}

func TestManagerHandleServerRequestRejectsUnknownMethod(t *testing.T) {
	mgr := NewManager()
	_, err := mgr.handleServerRequest("filesystem", ServerRequest{
		ID:     1,
		Method: "sampling/createMessage",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported server request")
}

func TestElicitationResolveSendsResponse(t *testing.T) {
	mgr := NewManager()
	client, writer := newTestClient("")
	mgr.clients["filesystem"] = client
	mgr.bindNotificationHandler("filesystem", client)

	_, err := mgr.handleServerRequest("filesystem", ServerRequest{
		ID:     42,
		Method: "elicitation/request",
		Params: json.RawMessage(`{"message":"pick a file"}`),
	})
	require.NoError(t, err)
	require.Equal(t, 1, mgr.ElicitationQueueSize())

	err = mgr.ResolveElicitation("filesystem:42", "accept", map[string]any{
		"path": "/tmp/answer.txt",
	})
	require.NoError(t, err)

	// Queue must be drained and wire entry removed.
	assert.Equal(t, 0, mgr.ElicitationQueueSize())
	mgr.mu.RLock()
	_, stillWired := mgr.elicitWire["filesystem:42"]
	mgr.mu.RUnlock()
	assert.False(t, stillWired, "wire entry must be cleaned up after resolve")

	// Response frame must be on the wire, correlated with id 42.
	wire := writer.String()
	require.NotEmpty(t, wire, "resolve must write a response frame")

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(wire)), &out))
	assert.Equal(t, "2.0", out["jsonrpc"])
	assert.Equal(t, float64(42), out["id"])

	result, ok := out["result"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "accept", result["action"])

	content, ok := result["content"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "/tmp/answer.txt", content["path"])
}

func TestResolveElicitationRejectsUnknownID(t *testing.T) {
	mgr := NewManager()
	err := mgr.ResolveElicitation("nope:1", "accept", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown or expired")
}

func TestResolveElicitationRejectsEmptyArgs(t *testing.T) {
	mgr := NewManager()
	err := mgr.ResolveElicitation("", "accept", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty id")

	err = mgr.ResolveElicitation("srv:1", "", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty action")
}

// --- Manager: resources + prompts accessors ---

func TestManagerResourcesAccessorReturnsCopy(t *testing.T) {
	mgr := NewManager()
	mgr.resourcesCache["srv"] = []Resource{{URI: "file:///a", Name: "a"}}

	out := mgr.Resources()
	require.Len(t, out["srv"], 1)

	// Mutating the snapshot must not leak into the cache.
	out["srv"][0].Name = "MUTATED"
	assert.Equal(t, "a", mgr.resourcesCache["srv"][0].Name)
}

func TestManagerPromptsAccessorReturnsCopy(t *testing.T) {
	mgr := NewManager()
	mgr.promptsCache["srv"] = []Prompt{{Name: "summarize"}}

	out := mgr.Prompts()
	require.Len(t, out["srv"], 1)

	out["srv"][0].Name = "MUTATED"
	assert.Equal(t, "summarize", mgr.promptsCache["srv"][0].Name)
}

// --- Client request handler wiring ---

func TestClientDispatchesServerRequestsToHandler(t *testing.T) {
	// Server pushes a request with id=77, then a response to our ping id=1.
	client, writer := newTestClient(strings.Join([]string{
		`{"jsonrpc":"2.0","id":77,"method":"elicitation/request","params":{"message":"pick a file"}}`,
		`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`,
	}, "\n") + "\n")

	var (
		gotID     atomic.Int64
		gotMethod atomic.Value
	)
	client.SetRequestHandler(func(req ServerRequest) (any, error) {
		gotID.Store(req.ID)
		gotMethod.Store(req.Method)
		return map[string]any{"action": "pending", "id": fmt.Sprintf("srv:%d", req.ID)}, nil
	})

	result, err := client.call("ping", nil)
	require.NoError(t, err)
	assert.JSONEq(t, `{"ok":true}`, string(result))

	assert.Equal(t, int64(77), gotID.Load())
	assert.Equal(t, "elicitation/request", gotMethod.Load())

	// The response to request id 77 must have been written to stdin.
	wire := writer.String()
	assert.Contains(t, wire, `"id":77`)
	assert.Contains(t, wire, `"action":"pending"`)
}

func TestClientServerRequestWithoutHandlerReturnsMethodNotFound(t *testing.T) {
	client, writer := newTestClient(strings.Join([]string{
		`{"jsonrpc":"2.0","id":99,"method":"elicitation/request","params":{}}`,
		`{"jsonrpc":"2.0","id":1,"result":{}}`,
	}, "\n") + "\n")

	_, err := client.call("ping", nil)
	require.NoError(t, err)

	wire := writer.String()
	// Must carry a method-not-found JSON-RPC error (-32601) back to the server.
	assert.Contains(t, wire, `"id":99`)
	assert.Contains(t, wire, `"code":-32601`)
}

func TestClientSendElicitationResponseMarshalsFrame(t *testing.T) {
	client, writer := newTestClient("")

	err := client.SendElicitationResponse(11, "decline", nil)
	require.NoError(t, err)

	frame := strings.TrimSpace(writer.String())
	require.NotEmpty(t, frame)

	// Must be single line terminated by newline.
	assert.Equal(t, 1, strings.Count(writer.String(), "\n"))

	// Round trip to confirm the shape.
	scanner := bufio.NewScanner(strings.NewReader(writer.String()))
	require.True(t, scanner.Scan())

	var out map[string]any
	require.NoError(t, json.Unmarshal(scanner.Bytes(), &out))
	assert.Equal(t, "2.0", out["jsonrpc"])
	assert.Equal(t, float64(11), out["id"])

	result, ok := out["result"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "decline", result["action"])
	_, hasContent := result["content"]
	assert.False(t, hasContent, "nil content must not emit a content field")
}
