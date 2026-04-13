// Package headless provides the NDJSON stdio serve loop for headless mode.
// It bridges an engine.Engine to stdin/stdout using the CC-compatible protocol.
package headless

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/google/uuid"
	"github.com/gravitrone/providence-core/internal/engine"
)

// --- Input Message Types ---

// InputMessage is the envelope for all stdin NDJSON messages.
type InputMessage struct {
	Type    string          `json:"type"`
	Subtype string          `json:"subtype,omitempty"`
	Message json.RawMessage `json:"message,omitempty"`

	// Control fields (used when Type == "control_response").
	RequestID string `json:"request_id,omitempty"`
	Response  any    `json:"response,omitempty"`
}

// UserMessageBody is the inner body for type "user" messages.
type UserMessageBody struct {
	Content string `json:"content"`
}

// --- Output Event Types ---

// OutputEvent is a generic NDJSON output envelope. Fields are populated
// based on the event being serialized. Zero-value fields are omitted.
type OutputEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`

	// System init fields.
	SessionID string   `json:"session_id,omitempty"`
	Tools     []string `json:"tools,omitempty"`
	Model     string   `json:"model,omitempty"`
	Engine    string   `json:"engine,omitempty"`
	Version   string   `json:"version,omitempty"`

	// Assistant fields.
	Message *engine.AssistantMsg `json:"message,omitempty"`

	// Stream event fields.
	Event *engine.StreamEventData `json:"event,omitempty"`

	// Result fields.
	Result       string  `json:"result,omitempty"`
	TotalCostUSD float64 `json:"total_cost_usd,omitempty"`
	IsError      bool    `json:"is_error,omitempty"`

	// Tool result fields.
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	Output     string `json:"output,omitempty"`

	// Permission request fields.
	Tool       *engine.PermissionTool     `json:"tool,omitempty"`
	QuestionID string                     `json:"question_id,omitempty"`
	Options    []engine.PermissionOption  `json:"options,omitempty"`

	// Generic content (system messages, errors).
	Content string `json:"content,omitempty"`
	Error   string `json:"error,omitempty"`

	// Usage fields.
	InputTokens       int `json:"input_tokens,omitempty"`
	OutputTokens      int `json:"output_tokens,omitempty"`
	TotalTokens       int `json:"total_tokens,omitempty"`
	CacheReadTokens   int `json:"cache_read_tokens,omitempty"`
	CacheCreateTokens int `json:"cache_create_tokens,omitempty"`
}

// Server is the headless NDJSON serve loop. It reads user messages from stdin,
// forwards them to an engine.Engine, and emits NDJSON events on stdout.
type Server struct {
	engine    engine.Engine
	scanner   *bufio.Scanner
	writer    *json.Encoder
	sessionID string
	model     string
	engineID  string

	mu     sync.Mutex
	closed bool
}

// NewServer creates a headless Server that reads from r and writes to w.
func NewServer(eng engine.Engine, r io.Reader, w io.Writer, model, engineID string) *Server {
	scanner := bufio.NewScanner(r)
	// Allow large messages (1MB).
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	return &Server{
		engine:    eng,
		scanner:   scanner,
		writer:    json.NewEncoder(w),
		sessionID: uuid.New().String(),
		model:     model,
		engineID:  engineID,
	}
}

// Run starts the headless serve loop. It blocks until ctx is cancelled,
// stdin reaches EOF, or a fatal error occurs.
func (s *Server) Run(ctx context.Context) error {
	// 1. Emit system/init.
	s.emit(OutputEvent{
		Type:      TypeSystem,
		Subtype:   SubtypeInit,
		SessionID: s.sessionID,
		Model:     s.model,
		Engine:    s.engineID,
		Version:   "1.0",
	})

	// 2. Start goroutine draining engine events to stdout.
	// The drainer exits when the engine's event channel is closed (by engine.Close).
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.drainEvents()
	}()

	// 3. Read stdin line by line, dispatch messages.
	err := s.readLoop(ctx)

	// Cleanup: close engine (closes event channel), wait for drainer to finish.
	s.engine.Close()
	wg.Wait()

	return err
}

// --- Internal Methods ---

// emit writes an OutputEvent as a single NDJSON line. Thread-safe.
func (s *Server) emit(ev OutputEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	_ = s.writer.Encode(ev)
}

// emitError writes an error event.
func (s *Server) emitError(msg string) {
	s.emit(OutputEvent{
		Type:  "error",
		Error: msg,
	})
}

// readLoop reads NDJSON lines from stdin and dispatches them.
func (s *Server) readLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if !s.scanner.Scan() {
			// EOF or scanner error.
			if err := s.scanner.Err(); err != nil {
				return fmt.Errorf("stdin read: %w", err)
			}
			return nil // clean EOF
		}

		line := s.scanner.Text()
		if line == "" {
			continue
		}

		var msg InputMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			s.emitError("invalid json: " + err.Error())
			continue
		}

		s.dispatch(ctx, msg)
	}
}

// dispatch routes an input message to the appropriate handler.
func (s *Server) dispatch(_ context.Context, msg InputMessage) {
	switch msg.Type {
	case TypeUser:
		s.handleUser(msg)
	case TypeControlResponse:
		s.handleControlResponse(msg)
	default:
		// Check for control_request subtypes on the type field itself.
		// Some clients send {type: "control_request", subtype: "initialize"}.
		if msg.Type == TypeControlRequest {
			s.handleControlRequest(msg)
			return
		}
		s.emitError(fmt.Sprintf("unknown message type: %s", msg.Type))
	}
}

// handleUser parses the user message body and sends it to the engine.
func (s *Server) handleUser(msg InputMessage) {
	var body UserMessageBody
	if msg.Message != nil {
		if err := json.Unmarshal(msg.Message, &body); err != nil {
			s.emitError("invalid user message body: " + err.Error())
			return
		}
	}

	if body.Content == "" {
		return
	}

	if err := s.engine.Send(body.Content); err != nil {
		s.emitError("send failed: " + err.Error())
	}
}

// handleControlRequest handles inbound control requests from the host.
func (s *Server) handleControlRequest(msg InputMessage) {
	switch msg.Subtype {
	case SubtypeInit, "initialize":
		// Respond with capabilities.
		s.emit(OutputEvent{
			Type:      TypeSystem,
			Subtype:   "initialized",
			SessionID: s.sessionID,
			Model:     s.model,
			Engine:    s.engineID,
		})
	case "interrupt":
		s.engine.Interrupt()
	case "set_model":
		// Model switching not yet supported, ack anyway.
		s.emit(OutputEvent{
			Type:    TypeSystem,
			Subtype: "model_set",
		})
	default:
		s.emitError(fmt.Sprintf("unknown control_request subtype: %s", msg.Subtype))
	}
}

// handleControlResponse handles responses to permission requests.
func (s *Server) handleControlResponse(msg InputMessage) {
	if msg.RequestID == "" {
		s.emitError("control_response missing request_id")
		return
	}
	// Extract the option ID from the response payload.
	optionID := ""
	if respMap, ok := msg.Response.(map[string]any); ok {
		if oid, ok := respMap["option_id"].(string); ok {
			optionID = oid
		}
	}
	if err := s.engine.RespondPermission(msg.RequestID, optionID); err != nil {
		s.emitError("permission response failed: " + err.Error())
	}
}

// drainEvents reads from the engine's event channel and emits NDJSON.
// It exits when the channel is closed (by engine.Close).
func (s *Server) drainEvents() {
	for ev := range s.engine.Events() {
		s.translateEvent(ev)
	}
}

// translateEvent maps an engine.ParsedEvent to an OutputEvent and emits it.
func (s *Server) translateEvent(ev engine.ParsedEvent) {
	if ev.Err != nil {
		s.emit(OutputEvent{
			Type:    TypeResult,
			Subtype: SubtypeError,
			Error:   ev.Err.Error(),
			IsError: true,
		})
		return
	}

	switch ev.Type {
	case "system":
		if init, ok := ev.Data.(*engine.SystemInitEvent); ok {
			s.emit(OutputEvent{
				Type:      TypeSystem,
				Subtype:   SubtypeInit,
				SessionID: init.SessionID,
				Tools:     init.Tools,
				Model:     init.Model,
			})
		}

	case "assistant":
		if ae, ok := ev.Data.(*engine.AssistantEvent); ok {
			s.emit(OutputEvent{
				Type:    TypeAssistant,
				Message: &ae.Message,
			})
		}

	case "stream_event":
		if se, ok := ev.Data.(*engine.StreamEvent); ok {
			s.emit(OutputEvent{
				Type:  TypeStreamEvent,
				Event: &se.Event,
			})
		}

	case "result":
		if re, ok := ev.Data.(*engine.ResultEvent); ok {
			s.emit(OutputEvent{
				Type:         TypeResult,
				Subtype:      re.Subtype,
				Result:       re.Result,
				SessionID:    re.SessionID,
				TotalCostUSD: re.TotalCostUSD,
				IsError:      re.IsError,
			})
		}

	case "tool_result":
		if tr, ok := ev.Data.(*engine.ToolResultEvent); ok {
			s.emit(OutputEvent{
				Type:       "tool_result",
				ToolCallID: tr.ToolCallID,
				ToolName:   tr.ToolName,
				Output:     tr.Output,
				IsError:    tr.IsError,
			})
		}

	case "permission_request":
		if pr, ok := ev.Data.(*engine.PermissionRequestEvent); ok {
			s.emit(OutputEvent{
				Type:       TypeControlRequest,
				Tool:       &pr.Tool,
				QuestionID: pr.QuestionID,
				Options:    pr.Options,
			})
		}

	case "usage_update":
		if uu, ok := ev.Data.(*engine.UsageUpdateEvent); ok {
			s.emit(OutputEvent{
				Type:              "usage_update",
				InputTokens:       uu.InputTokens,
				OutputTokens:      uu.OutputTokens,
				TotalTokens:       uu.TotalTokens,
				CacheReadTokens:   uu.CacheReadTokens,
				CacheCreateTokens: uu.CacheCreateTokens,
			})
		}

	case "system_message":
		if sm, ok := ev.Data.(*engine.SystemMessageEvent); ok {
			s.emit(OutputEvent{
				Type:    TypeSystem,
				Subtype: "message",
				Content: sm.Content,
			})
		}

	case "compaction":
		if ce, ok := ev.Data.(*engine.CompactionEvent); ok {
			s.emit(OutputEvent{
				Type:    TypeSystem,
				Subtype: SubtypeCompactBoundary,
				Content: ce.Phase,
			})
		}

	case "tombstone":
		// Tombstones are UI-only, skip in headless mode.

	default:
		// Forward unknown events with raw data for extensibility.
		if ev.Raw != "" {
			s.emit(OutputEvent{
				Type:    ev.Type,
				Content: ev.Raw,
			})
		}
	}
}

// Close marks the server as closed, preventing further output.
func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
}
