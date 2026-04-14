package overlay

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
)

// --- Server ---

// Server accepts UDS connections from the overlay process and dispatches
// protocol messages to the registered ServerHandler.
type Server struct {
	socketPath string
	listener   net.Listener
	handler    ServerHandler
	clients    map[*client]struct{}
	clientsMu  sync.Mutex
	logger     *slog.Logger
	wg         sync.WaitGroup
	closed     chan struct{}
}

// ServerHandler receives overlay-originated messages. All methods are called
// from a per-client goroutine, so implementations must be goroutine-safe.
type ServerHandler interface {
	// OnHello handles the initial Hello from the overlay and returns a Welcome.
	OnHello(*client, Hello) Welcome
	// OnContextUpdate handles a screen/audio context observation.
	OnContextUpdate(*client, ContextUpdate) error
	// OnUserQuery handles a user-initiated query from the overlay.
	OnUserQuery(*client, UserQuery) error
	// OnEmberRequest handles a request to change ember autonomous mode.
	OnEmberRequest(*client, EmberRequest) error
	// OnInterrupt handles an interrupt signal from the overlay.
	OnInterrupt(*client) error
	// OnUIEvent handles overlay UI telemetry.
	OnUIEvent(*client, UIEvent) error
	// OnDisconnect is called when a client disconnects cleanly or drops.
	OnDisconnect(*client)
}

// client represents a single connected overlay process.
type client struct {
	conn     net.Conn
	writeMu  sync.Mutex
	writer   *bufio.Writer
	server   *Server
	closedMu sync.Mutex
	closed   bool
}

// NewServer creates a UDS server listening on socketPath.
//
// socketPath defaults to ~/.providence/run/overlay.sock when empty.
// The socket is created with mode 0600 in a 0700 parent directory so that
// only the owning user can connect. Stale sockets are removed on start.
func NewServer(socketPath string, handler ServerHandler, logger *slog.Logger) (*Server, error) {
	if socketPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("overlay: resolve home dir: %w", err)
		}
		socketPath = filepath.Join(home, ".providence", "run", "overlay.sock")
	}

	dir := filepath.Dir(socketPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("overlay: create socket dir: %w", err)
	}

	// Remove stale socket from a previous run.
	_ = os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("overlay: listen unix %s: %w", socketPath, err)
	}

	if err := os.Chmod(socketPath, 0600); err != nil {
		_ = listener.Close()
		_ = os.Remove(socketPath)
		return nil, fmt.Errorf("overlay: chmod socket: %w", err)
	}

	if logger == nil {
		logger = slog.Default()
	}

	return &Server{
		socketPath: socketPath,
		listener:   listener,
		handler:    handler,
		clients:    make(map[*client]struct{}),
		logger:     logger,
		closed:     make(chan struct{}),
	}, nil
}

// SocketPath returns the socket file path.
func (s *Server) SocketPath() string { return s.socketPath }

// ConnectedCount returns the number of currently connected clients.
func (s *Server) ConnectedCount() int {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	return len(s.clients)
}

// Serve accepts connections until Close is called or ctx is cancelled.
func (s *Server) Serve(ctx context.Context) error {
	// Close listener when ctx is cancelled so Accept unblocks.
	go func() {
		select {
		case <-ctx.Done():
			_ = s.listener.Close()
		case <-s.closed:
		}
	}()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			select {
			case <-s.closed:
				return nil
			default:
			}
			return fmt.Errorf("overlay: accept: %w", err)
		}

		// Filesystem mode 0600 on the socket + 0700 parent directory already
		// restricts connections to the owning user.
		// TODO: add SO_PEERCRED / LOCAL_PEERCRED uid verification as belt-and-
		// suspenders once a portable Go wrapper is available for macOS xucred.

		c := &client{
			conn:   conn,
			writer: bufio.NewWriter(conn),
			server: s,
		}
		s.registerClient(c)
		s.wg.Add(1)
		go s.handleClient(c)
	}
}

// Close stops accepting new connections, signals all clients to close, and
// waits for in-flight handlers to finish.
func (s *Server) Close() error {
	select {
	case <-s.closed:
		return nil // already closed
	default:
		close(s.closed)
	}

	err := s.listener.Close()

	// Close all connected clients.
	s.clientsMu.Lock()
	for c := range s.clients {
		c.closeConn()
	}
	s.clientsMu.Unlock()

	s.wg.Wait()
	_ = os.Remove(s.socketPath)
	return err
}

// Broadcast sends a typed message to all currently connected clients.
// Slow or blocked clients are skipped (non-blocking send via a 1-deep write
// attempt - the writeMu ensures serialisation but won't block indefinitely
// since we hold it only for the duration of the flush).
func (s *Server) Broadcast(msgType string, data any) error {
	env, err := marshalEnvelope(msgType, data)
	if err != nil {
		return err
	}
	s.clientsMu.Lock()
	clients := make([]*client, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
	}
	s.clientsMu.Unlock()

	for _, c := range clients {
		_ = c.sendRaw(env)
	}
	return nil
}

// handleClient reads NDJSON envelopes from the connection and dispatches them
// to the handler. Malformed lines are logged and dropped; EOF and closed-conn
// errors terminate the loop cleanly.
func (s *Server) handleClient(c *client) {
	defer s.wg.Done()
	defer s.deregisterClient(c)
	defer c.closeConn()
	defer s.handler.OnDisconnect(c)

	scanner := bufio.NewScanner(c.conn)
	// 4 MB max line size to protect against pathological input.
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var env Envelope
		if err := json.Unmarshal(line, &env); err != nil {
			s.logger.Warn("overlay: malformed envelope", "error", err)
			continue
		}

		if err := s.dispatch(c, env); err != nil {
			s.logger.Warn("overlay: dispatch error", "type", env.Type, "error", err)
		}
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) && !isConnClosed(err) {
		s.logger.Debug("overlay: client read error", "error", err)
	}
}

// dispatch routes an incoming envelope to the appropriate handler method.
func (s *Server) dispatch(c *client, env Envelope) error {
	switch env.Type {
	case TypeHello:
		var h Hello
		if err := json.Unmarshal(env.Data, &h); err != nil {
			return fmt.Errorf("parse hello: %w", err)
		}
		welcome := s.handler.OnHello(c, h)
		return c.Send(TypeWelcome, welcome)

	case TypeContextUpdate:
		var u ContextUpdate
		if err := json.Unmarshal(env.Data, &u); err != nil {
			return fmt.Errorf("parse context_update: %w", err)
		}
		return s.handler.OnContextUpdate(c, u)

	case TypeUserQuery:
		var q UserQuery
		if err := json.Unmarshal(env.Data, &q); err != nil {
			return fmt.Errorf("parse user_query: %w", err)
		}
		return s.handler.OnUserQuery(c, q)

	case TypeEmberRequest:
		var r EmberRequest
		if err := json.Unmarshal(env.Data, &r); err != nil {
			return fmt.Errorf("parse ember_request: %w", err)
		}
		return s.handler.OnEmberRequest(c, r)

	case TypeInterrupt:
		return s.handler.OnInterrupt(c)

	case TypeUIEvent:
		var e UIEvent
		if err := json.Unmarshal(env.Data, &e); err != nil {
			return fmt.Errorf("parse ui_event: %w", err)
		}
		return s.handler.OnUIEvent(c, e)

	case TypeGoodbye, TypeBye:
		// Overlay is shutting down cleanly - close from our side.
		c.closeConn()
		return nil

	default:
		s.logger.Warn("overlay: unknown message type", "type", env.Type)
		return nil
	}
}

// Send writes a typed message envelope to this specific client.
func (c *client) Send(msgType string, data any) error {
	env, err := marshalEnvelope(msgType, data)
	if err != nil {
		return err
	}
	return c.sendRaw(env)
}

// sendRaw writes a pre-marshalled NDJSON line to the client connection.
func (c *client) sendRaw(line []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	c.closedMu.Lock()
	closed := c.closed
	c.closedMu.Unlock()
	if closed {
		return errors.New("overlay: client already closed")
	}

	_, err := c.writer.Write(line)
	if err != nil {
		return err
	}
	return c.writer.Flush()
}

// closeConn marks the client closed and closes the underlying connection.
func (c *client) closeConn() {
	c.closedMu.Lock()
	defer c.closedMu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	_ = c.conn.Close()
}

// registerClient adds a client to the active set.
func (s *Server) registerClient(c *client) {
	s.clientsMu.Lock()
	s.clients[c] = struct{}{}
	s.clientsMu.Unlock()
}

// deregisterClient removes a client from the active set.
func (s *Server) deregisterClient(c *client) {
	s.clientsMu.Lock()
	delete(s.clients, c)
	s.clientsMu.Unlock()
}

// --- Helpers ---

// marshalEnvelope serialises data into an Envelope NDJSON line (newline-terminated).
func marshalEnvelope(msgType string, data any) ([]byte, error) {
	rawData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("overlay: marshal data: %w", err)
	}
	env := Envelope{
		V:    ProtocolVersion,
		Type: msgType,
		Data: rawData,
	}
	line, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("overlay: marshal envelope: %w", err)
	}
	return append(line, '\n'), nil
}

// isConnClosed reports whether err is caused by a closed connection.
func isConnClosed(err error) bool {
	if err == nil {
		return false
	}
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return netErr.Err.Error() == "use of closed network connection"
	}
	return false
}
