package overlay

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Spy handler ---

type spyHandler struct {
	mu            sync.Mutex
	hellos        []Hello
	contextUpdates []ContextUpdate
	userQueries   []UserQuery
	emberRequests []EmberRequest
	interrupts    int
	uiEvents      []UIEvent
	disconnects   int
	welcomeFn     func(Hello) Welcome
}

func (s *spyHandler) OnHello(c *client, h Hello) Welcome {
	s.mu.Lock()
	s.hellos = append(s.hellos, h)
	s.mu.Unlock()
	if s.welcomeFn != nil {
		return s.welcomeFn(h)
	}
	return Welcome{SessionID: "test-session", Engine: "claude", Model: "sonnet"}
}

func (s *spyHandler) OnContextUpdate(_ *client, u ContextUpdate) error {
	s.mu.Lock()
	s.contextUpdates = append(s.contextUpdates, u)
	s.mu.Unlock()
	return nil
}

func (s *spyHandler) OnUserQuery(_ *client, q UserQuery) error {
	s.mu.Lock()
	s.userQueries = append(s.userQueries, q)
	s.mu.Unlock()
	return nil
}

func (s *spyHandler) OnEmberRequest(_ *client, r EmberRequest) error {
	s.mu.Lock()
	s.emberRequests = append(s.emberRequests, r)
	s.mu.Unlock()
	return nil
}

func (s *spyHandler) OnInterrupt(_ *client) error {
	s.mu.Lock()
	s.interrupts++
	s.mu.Unlock()
	return nil
}

func (s *spyHandler) OnUIEvent(_ *client, e UIEvent) error {
	s.mu.Lock()
	s.uiEvents = append(s.uiEvents, e)
	s.mu.Unlock()
	return nil
}

func (s *spyHandler) OnDisconnect(_ *client) {
	s.mu.Lock()
	s.disconnects++
	s.mu.Unlock()
}

func (s *spyHandler) helloCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.hellos)
}

// --- Helpers ---

// startTestServer creates a server and starts serving.
// Uses a short path under /tmp to stay within macOS's 104-char UDS limit.
func startTestServer(t *testing.T, handler ServerHandler) (*Server, context.CancelFunc) {
	t.Helper()
	sockPath := shortSockPath(t)
	srv, err := NewServer(sockPath, handler, nil)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		_ = srv.Serve(ctx)
	}()
	// Give the server a moment to start.
	time.Sleep(20 * time.Millisecond)
	return srv, cancel
}

// shortSockPath returns a short temp socket path that fits within macOS's
// 104-character UDS path limit. Uses /tmp directly with a short random suffix.
func shortSockPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "pvd-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "o.sock")
}

// dialTestClient connects to the test server socket.
func dialTestClient(t *testing.T, srv *Server) net.Conn {
	t.Helper()
	conn, err := net.Dial("unix", srv.SocketPath())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// sendEnvelope marshals and writes an envelope to conn.
func sendEnvelope(t *testing.T, conn net.Conn, msgType string, data any) {
	t.Helper()
	line, err := marshalEnvelope(msgType, data)
	require.NoError(t, err)
	_, err = conn.Write(line)
	require.NoError(t, err)
}

// readEnvelope reads one NDJSON line from conn and unmarshals it.
func readEnvelope(t *testing.T, conn net.Conn) Envelope {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	scanner := bufio.NewScanner(conn)
	require.True(t, scanner.Scan(), "expected response from server")
	var env Envelope
	require.NoError(t, json.Unmarshal(scanner.Bytes(), &env))
	return env
}

// waitFor polls cond until true or timeout.
func waitFor(t *testing.T, cond func() bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

// --- Tests ---

func TestServerHelloWelcomeExchange(t *testing.T) {
	spy := &spyHandler{}
	srv, cancel := startTestServer(t, spy)
	defer cancel()
	defer srv.Close()

	conn := dialTestClient(t, srv)

	hello := Hello{ClientVersion: "1.0.0", PID: 12345, Capabilities: []string{"scstream"}}
	sendEnvelope(t, conn, TypeHello, hello)

	env := readEnvelope(t, conn)
	assert.Equal(t, ProtocolVersion, env.V)
	assert.Equal(t, TypeWelcome, env.Type)

	var w Welcome
	require.NoError(t, json.Unmarshal(env.Data, &w))
	assert.Equal(t, "test-session", w.SessionID)
	assert.Equal(t, "claude", w.Engine)
	assert.Equal(t, "sonnet", w.Model)

	// Verify the handler received the hello.
	waitFor(t, func() bool { return spy.helloCount() == 1 }, time.Second)
	spy.mu.Lock()
	assert.Equal(t, "1.0.0", spy.hellos[0].ClientVersion)
	assert.Equal(t, 12345, spy.hellos[0].PID)
	assert.Equal(t, []string{"scstream"}, spy.hellos[0].Capabilities)
	spy.mu.Unlock()
}

func TestServerContextUpdateDelivered(t *testing.T) {
	spy := &spyHandler{}
	srv, cancel := startTestServer(t, spy)
	defer cancel()
	defer srv.Close()

	conn := dialTestClient(t, srv)

	// Handshake first.
	sendEnvelope(t, conn, TypeHello, Hello{PID: 1})
	readEnvelope(t, conn) // welcome

	u := ContextUpdate{
		Transcript: "coding in vscode",
		ChangeKind: "transcript_only",
		Timestamp:  time.Now(),
	}
	sendEnvelope(t, conn, TypeContextUpdate, u)

	waitFor(t, func() bool {
		spy.mu.Lock()
		defer spy.mu.Unlock()
		return len(spy.contextUpdates) == 1
	}, time.Second)

	spy.mu.Lock()
	got := spy.contextUpdates[0]
	spy.mu.Unlock()
	assert.Equal(t, "coding in vscode", got.Transcript)
	assert.Equal(t, "transcript_only", got.ChangeKind)
}

func TestServerUserQueryDelivered(t *testing.T) {
	spy := &spyHandler{}
	srv, cancel := startTestServer(t, spy)
	defer cancel()
	defer srv.Close()

	conn := dialTestClient(t, srv)
	sendEnvelope(t, conn, TypeHello, Hello{PID: 1})
	readEnvelope(t, conn) // welcome

	q := UserQuery{Text: "what is the meaning of life?", Source: "push_to_talk"}
	sendEnvelope(t, conn, TypeUserQuery, q)

	waitFor(t, func() bool {
		spy.mu.Lock()
		defer spy.mu.Unlock()
		return len(spy.userQueries) == 1
	}, time.Second)

	spy.mu.Lock()
	got := spy.userQueries[0]
	spy.mu.Unlock()
	assert.Equal(t, "what is the meaning of life?", got.Text)
	assert.Equal(t, "push_to_talk", got.Source)
}

func TestServerEmberRequestDelivered(t *testing.T) {
	spy := &spyHandler{}
	srv, cancel := startTestServer(t, spy)
	defer cancel()
	defer srv.Close()

	conn := dialTestClient(t, srv)
	sendEnvelope(t, conn, TypeHello, Hello{PID: 1})
	readEnvelope(t, conn) // welcome

	r := EmberRequest{Desired: "active"}
	sendEnvelope(t, conn, TypeEmberRequest, r)

	waitFor(t, func() bool {
		spy.mu.Lock()
		defer spy.mu.Unlock()
		return len(spy.emberRequests) == 1
	}, time.Second)

	spy.mu.Lock()
	got := spy.emberRequests[0]
	spy.mu.Unlock()
	assert.Equal(t, "active", got.Desired)
}

func TestServerGoodbyeClosesConnection(t *testing.T) {
	spy := &spyHandler{}
	srv, cancel := startTestServer(t, spy)
	defer cancel()
	defer srv.Close()

	conn := dialTestClient(t, srv)
	sendEnvelope(t, conn, TypeHello, Hello{PID: 1})
	readEnvelope(t, conn) // welcome

	// Send goodbye.
	sendEnvelope(t, conn, TypeGoodbye, struct{}{})

	// Server should close the connection; next read returns EOF/error.
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 64)
	_, err := conn.Read(buf)
	assert.Error(t, err, "expected connection to be closed after goodbye")
}

func TestServerBroadcast(t *testing.T) {
	spy := &spyHandler{}
	srv, cancel := startTestServer(t, spy)
	defer cancel()
	defer srv.Close()

	// Connect two clients.
	conn1 := dialTestClient(t, srv)
	conn2 := dialTestClient(t, srv)

	// Handshake both.
	sendEnvelope(t, conn1, TypeHello, Hello{PID: 1})
	readEnvelope(t, conn1)
	sendEnvelope(t, conn2, TypeHello, Hello{PID: 2})
	readEnvelope(t, conn2)

	// Wait for both to register.
	waitFor(t, func() bool { return srv.ConnectedCount() >= 2 }, time.Second)

	// Broadcast an ember state.
	err := srv.Broadcast(TypeEmberState, EmberState{Active: true})
	require.NoError(t, err)

	// Both clients should receive it.
	env1 := readEnvelope(t, conn1)
	env2 := readEnvelope(t, conn2)
	assert.Equal(t, TypeEmberState, env1.Type)
	assert.Equal(t, TypeEmberState, env2.Type)

	var s1, s2 EmberState
	require.NoError(t, json.Unmarshal(env1.Data, &s1))
	require.NoError(t, json.Unmarshal(env2.Data, &s2))
	assert.True(t, s1.Active)
	assert.True(t, s2.Active)
}

func TestServerCorruptJSONDropped(t *testing.T) {
	spy := &spyHandler{}
	srv, cancel := startTestServer(t, spy)
	defer cancel()
	defer srv.Close()

	conn := dialTestClient(t, srv)

	// Send garbage - should not panic the server.
	_, err := conn.Write([]byte("this is not json\n"))
	require.NoError(t, err)

	// Send valid hello - server should still be alive and process it.
	sendEnvelope(t, conn, TypeHello, Hello{PID: 42})
	env := readEnvelope(t, conn)
	assert.Equal(t, TypeWelcome, env.Type)

	waitFor(t, func() bool { return spy.helloCount() == 1 }, time.Second)
}

func TestServerUnknownTypeLogged(t *testing.T) {
	spy := &spyHandler{}
	srv, cancel := startTestServer(t, spy)
	defer cancel()
	defer srv.Close()

	conn := dialTestClient(t, srv)
	sendEnvelope(t, conn, TypeHello, Hello{PID: 1})
	readEnvelope(t, conn)

	// Unknown type - should not crash the server.
	sendEnvelope(t, conn, "definitely_not_a_real_type", map[string]string{"x": "y"})

	// Follow-up query should still work.
	sendEnvelope(t, conn, TypeUserQuery, UserQuery{Text: "hello", Source: "panel_input"})
	waitFor(t, func() bool {
		spy.mu.Lock()
		defer spy.mu.Unlock()
		return len(spy.userQueries) == 1
	}, time.Second)
}

func TestServerInterruptDelivered(t *testing.T) {
	spy := &spyHandler{}
	srv, cancel := startTestServer(t, spy)
	defer cancel()
	defer srv.Close()

	conn := dialTestClient(t, srv)
	sendEnvelope(t, conn, TypeHello, Hello{PID: 1})
	readEnvelope(t, conn)

	sendEnvelope(t, conn, TypeInterrupt, nil)

	waitFor(t, func() bool {
		spy.mu.Lock()
		defer spy.mu.Unlock()
		return spy.interrupts == 1
	}, time.Second)
}

func TestServerSocketPermissions(t *testing.T) {
	spy := &spyHandler{}
	srv, cancel := startTestServer(t, spy)
	defer cancel()
	defer srv.Close()

	// Verify socket file has mode 0600.
	info, err := os.Stat(srv.SocketPath())
	require.NoError(t, err)
	perm := info.Mode().Perm()
	assert.Equal(t, os.FileMode(0600), perm, "socket should be mode 0600")
}
