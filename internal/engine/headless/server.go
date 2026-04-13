// Package headless provides the NDJSON stdio serve loop for headless mode.
// It bridges an engine.Engine to stdin/stdout using the CC-compatible protocol.
package headless

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gravitrone/providence-core/internal/config"
	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/gravitrone/providence-core/internal/engine/mcp"
	"github.com/gravitrone/providence-core/internal/engine/subagent"
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
	UUID    string `json:"uuid,omitempty"`

	// System init fields.
	SessionID      string   `json:"session_id,omitempty"`
	Tools          []string `json:"tools,omitempty"`
	Model          string   `json:"model,omitempty"`
	Engine         string   `json:"engine,omitempty"`
	Version        string   `json:"version,omitempty"`
	Cwd            string   `json:"cwd,omitempty"`
	SlashCommands  []string `json:"slash_commands,omitempty"`
	PermissionMode string   `json:"permission_mode,omitempty"`

	// Assistant fields.
	Message *engine.AssistantMsg `json:"message,omitempty"`

	// Stream event fields.
	Event *engine.StreamEventData `json:"event,omitempty"`

	// Result fields.
	Result       string  `json:"result,omitempty"`
	TotalCostUSD float64 `json:"total_cost_usd,omitempty"`
	IsError      bool    `json:"is_error,omitempty"`
	DurationMS   int64   `json:"duration_ms,omitempty"`
	NumTurns     int     `json:"num_turns,omitempty"`

	// Tool result fields.
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	Output     string `json:"output,omitempty"`

	// Permission request fields.
	Tool       *engine.PermissionTool    `json:"tool,omitempty"`
	QuestionID string                    `json:"question_id,omitempty"`
	Options    []engine.PermissionOption `json:"options,omitempty"`

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

	// Optional collaborators set via With* options before Run.
	subagentRunner *subagent.Runner
	mcpManager     *mcp.Manager
	config         *config.Config
	cancelCtx      context.CancelFunc

	// Runtime state.
	maxThinkingTokens int
	sessionState      string // "idle", "running" - tracked for state change events
	turnStartTime     time.Time
	turnCount         int
	cwd               string // working directory for init enrichment

	mu     sync.Mutex
	closed bool
}

// NewServer creates a headless Server that reads from r and writes to w.
func NewServer(eng engine.Engine, r io.Reader, w io.Writer, model, engineID string) *Server {
	scanner := bufio.NewScanner(r)
	// Allow large messages (1MB).
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	return &Server{
		engine:       eng,
		scanner:      scanner,
		writer:       json.NewEncoder(w),
		sessionID:    uuid.New().String(),
		model:        model,
		engineID:     engineID,
		sessionState: "idle",
	}
}

// WithSubagentRunner sets the subagent runner for stop_task support.
func (s *Server) WithSubagentRunner(r *subagent.Runner) {
	s.subagentRunner = r
}

// WithMCPManager sets the MCP manager for mcp_status support.
func (s *Server) WithMCPManager(m *mcp.Manager) {
	s.mcpManager = m
}

// WithConfig sets the runtime config for get_settings/apply_flag_settings.
func (s *Server) WithConfig(c *config.Config) {
	s.config = c
}

// WithCwd sets the working directory reported in system/init.
func (s *Server) WithCwd(cwd string) {
	s.cwd = cwd
}

// Run starts the headless serve loop. It blocks until ctx is cancelled,
// stdin reaches EOF, or a fatal error occurs.
func (s *Server) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	s.cancelCtx = cancel

	// 1. Emit enriched system/init.
	cwd := s.cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	initTools, slashCmds, permMode := s.gatherInitInfo()
	s.emit(OutputEvent{
		Type:           TypeSystem,
		Subtype:        SubtypeInit,
		SessionID:      s.sessionID,
		Model:          s.model,
		Engine:         s.engineID,
		Version:        "1.0",
		Cwd:            cwd,
		Tools:          initTools,
		SlashCommands:  slashCmds,
		PermissionMode: permMode,
	})

	// 2. Start goroutine draining engine events to stdout.
	// The drainer exits when the engine's event channel is closed (by engine.Close).
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.drainEvents()
	}()

	// 3. Start keep_alive ticker (30s).
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.keepAliveLoop(ctx)
	}()

	// 4. Read stdin line by line, dispatch messages.
	err := s.readLoop(ctx)

	// Cleanup: close engine (closes event channel), wait for drainer + ticker.
	cancel()
	s.engine.Close()
	wg.Wait()

	return err
}

// --- Internal Methods ---

// emit writes an OutputEvent as a single NDJSON line. Thread-safe.
// Stamps UUID and SessionID on every event.
func (s *Server) emit(ev OutputEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	ev.UUID = uuid.New().String()
	if ev.SessionID == "" {
		ev.SessionID = s.sessionID
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

		if s.dispatch(ctx, msg) {
			return nil // end_session requested
		}
	}
}

// dispatch routes an input message to the appropriate handler.
// Returns true if the main loop should exit (e.g. end_session).
func (s *Server) dispatch(ctx context.Context, msg InputMessage) bool {
	switch msg.Type {
	case TypeUser:
		s.handleUser(msg)
	case TypeControlResponse:
		s.handleControlResponse(msg)
	case TypeKeepAlive:
		// Accept silently, no response needed.
	default:
		if msg.Type == TypeControlRequest {
			return s.handleControlRequest(ctx, msg)
		}
		s.emitError(fmt.Sprintf("unknown message type: %s", msg.Type))
	}
	return false
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

	s.turnStartTime = time.Now()
	s.turnCount++
	s.transitionState("running")
	if err := s.engine.Send(body.Content); err != nil {
		s.emitError("send failed: " + err.Error())
	}
}

// handleControlRequest handles inbound control requests from the host.
// Returns true if the serve loop should exit (end_session).
func (s *Server) handleControlRequest(ctx context.Context, msg InputMessage) bool {
	params := parseParams(msg.Message)

	switch msg.Subtype {
	case SubtypeInit, "initialize":
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
		s.emit(OutputEvent{
			Type:    TypeSystem,
			Subtype: "model_set",
		})

	case "set_permission_mode":
		mode, _ := params["mode"].(string)
		if mode == "" {
			s.emitError("set_permission_mode: missing mode param")
			return false
		}
		s.setPermissionMode(mode)
		s.emit(OutputEvent{
			Type:    TypeSystem,
			Subtype: "permission_mode_set",
			Content: mode,
		})

	case "get_context_usage":
		s.emit(s.buildContextUsageEvent())

	case "end_session":
		s.engine.Close()
		s.emit(OutputEvent{
			Type:    TypeSystem,
			Subtype: "session_ended",
		})
		if s.cancelCtx != nil {
			s.cancelCtx()
		}
		return true

	case "stop_task":
		taskID, _ := params["task_id"].(string)
		if taskID == "" {
			s.emitError("stop_task: missing task_id param")
			return false
		}
		if s.subagentRunner == nil {
			s.emitError("stop_task: no subagent runner configured")
			return false
		}
		if err := s.subagentRunner.Kill(taskID); err != nil {
			s.emitError(fmt.Sprintf("stop_task: %v", err))
			return false
		}
		s.emit(OutputEvent{
			Type:    TypeSystem,
			Subtype: "task_stopped",
			Content: taskID,
		})

	case "set_max_thinking_tokens":
		val, _ := params["max_thinking_tokens"].(float64)
		s.maxThinkingTokens = int(val)
		s.emit(OutputEvent{
			Type:    TypeSystem,
			Subtype: "max_thinking_tokens_set",
			Content: fmt.Sprintf("%d", s.maxThinkingTokens),
		})

	case "get_settings":
		s.emit(s.buildSettingsEvent())

	case "apply_flag_settings":
		s.applyFlagSettings(params)
		s.emit(OutputEvent{
			Type:    TypeSystem,
			Subtype: "settings_applied",
		})

	case "mcp_status":
		s.emit(s.buildMCPStatusEvent())

	default:
		s.emitError(fmt.Sprintf("unknown control_request subtype: %s", msg.Subtype))
	}
	_ = ctx
	return false
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
			var durationMS int64
			if !s.turnStartTime.IsZero() {
				durationMS = time.Since(s.turnStartTime).Milliseconds()
			}
			resultEv := OutputEvent{
				Type:         TypeResult,
				Subtype:      re.Subtype,
				Result:       re.Result,
				SessionID:    re.SessionID,
				TotalCostUSD: re.TotalCostUSD,
				IsError:      re.IsError,
				DurationMS:   durationMS,
				NumTurns:     s.turnCount,
			}
			// Attach final usage if the engine supports it.
			type usageProvider interface {
				ContextUsage() (inputTokens, outputTokens, contextWindow, messageCount int)
			}
			if up, ok := s.engine.(usageProvider); ok {
				input, output, _, _ := up.ContextUsage()
				resultEv.InputTokens = input
				resultEv.OutputTokens = output
				resultEv.TotalTokens = input + output
			}
			s.emit(resultEv)
			s.transitionState("idle")
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

	case "rate_limit":
		if rl, ok := ev.Data.(*engine.RateLimitEvent); ok {
			s.emit(OutputEvent{
				Type:    TypeSystem,
				Subtype: SubtypeAPIRetry,
				Content: fmt.Sprintf(
					`{"attempt":%d,"delay_sec":%d,"max_retry":%d}`,
					rl.Attempt, rl.DelaySec, rl.MaxRetry,
				),
			})
		}

	case "compaction":
		if ce, ok := ev.Data.(*engine.CompactionEvent); ok {
			// Emit status event for compaction lifecycle.
			status := "compaction_" + ce.Phase
			s.emit(OutputEvent{
				Type:    TypeSystem,
				Subtype: SubtypeStatus,
				Content: status,
			})
			// Also emit the legacy compact_boundary for backwards compat.
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

// --- State Tracking ---

// transitionState emits a session_state_changed event when the state changes.
func (s *Server) transitionState(newState string) {
	s.mu.Lock()
	old := s.sessionState
	if old == newState {
		s.mu.Unlock()
		return
	}
	s.sessionState = newState
	s.mu.Unlock()

	s.emit(OutputEvent{
		Type:    TypeSystem,
		Subtype: SubtypeSessionStateChanged,
		Content: fmt.Sprintf(`{"from":%q,"to":%q}`, old, newState),
	})
}

// keepAliveLoop emits keep_alive events every 30 seconds until ctx is cancelled.
func (s *Server) keepAliveLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.emit(OutputEvent{
				Type: TypeKeepAlive,
			})
		}
	}
}

// --- Init Enrichment ---

// gatherInitInfo queries the engine for tools, slash commands, and permission mode.
func (s *Server) gatherInitInfo() (tools []string, slashCmds []string, permMode string) {
	// Tools: ask the engine if it can list them.
	type toolLister interface {
		ToolNames() []string
	}
	if tl, ok := s.engine.(toolLister); ok {
		tools = tl.ToolNames()
	}

	// Slash commands: ask the engine if it can list them.
	type slashLister interface {
		SlashCommands() []string
	}
	if sl, ok := s.engine.(slashLister); ok {
		slashCmds = sl.SlashCommands()
	}

	// Permission mode from config.
	if s.config != nil && s.config.Permissions.Mode != "" {
		permMode = s.config.Permissions.Mode
	} else {
		permMode = "default"
	}

	return tools, slashCmds, permMode
}

// --- Control Request Helpers ---

// parseParams extracts the params map from a raw JSON message body.
func parseParams(raw json.RawMessage) map[string]any {
	if raw == nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}

// setPermissionMode delegates to the engine's permission handler if supported.
func (s *Server) setPermissionMode(mode string) {
	type permSetter interface {
		SetPermissionMode(string)
	}
	if ps, ok := s.engine.(permSetter); ok {
		ps.SetPermissionMode(mode)
	}
}

// buildContextUsageEvent returns a context_usage output event with token stats.
func (s *Server) buildContextUsageEvent() OutputEvent {
	type usageProvider interface {
		ContextUsage() (inputTokens, outputTokens, contextWindow, messageCount int)
	}
	ev := OutputEvent{
		Type:    TypeSystem,
		Subtype: "context_usage",
	}
	if up, ok := s.engine.(usageProvider); ok {
		input, output, window, msgs := up.ContextUsage()
		total := input + output
		pct := 0.0
		if window > 0 {
			pct = float64(total) / float64(window) * 100
		}
		ev.InputTokens = input
		ev.OutputTokens = output
		ev.TotalTokens = total
		ev.Content = fmt.Sprintf(
			`{"context_window":%d,"percentage":%.1f,"message_count":%d}`,
			window, pct, msgs,
		)
	}
	return ev
}

// buildSettingsEvent returns the current merged config as a system event.
func (s *Server) buildSettingsEvent() OutputEvent {
	ev := OutputEvent{
		Type:    TypeSystem,
		Subtype: "settings",
	}
	if s.config != nil {
		data, err := json.Marshal(s.config)
		if err == nil {
			ev.Content = string(data)
		}
	}
	return ev
}

// applyFlagSettings merges incoming param overrides into the runtime config.
func (s *Server) applyFlagSettings(params map[string]any) {
	if s.config == nil {
		return
	}
	if m, ok := params["model"].(string); ok && m != "" {
		s.config.Model = m
		s.model = m
	}
	if e, ok := params["effort"].(string); ok && e != "" {
		s.config.Effort = e
	}
	if eng, ok := params["engine"].(string); ok && eng != "" {
		s.config.Engine = eng
	}
}

// MCPServerInfo describes a connected MCP server for the mcp_status event.
type MCPServerInfo struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Tools  int    `json:"tools"`
}

// buildMCPStatusEvent returns the list of connected MCP servers.
func (s *Server) buildMCPStatusEvent() OutputEvent {
	ev := OutputEvent{
		Type:    TypeSystem,
		Subtype: "mcp_status",
	}
	if s.mcpManager == nil {
		ev.Content = "[]"
		return ev
	}
	allTools := s.mcpManager.GetAllTools()
	servers := make([]MCPServerInfo, 0, len(allTools))
	for name, tools := range allTools {
		servers = append(servers, MCPServerInfo{
			Name:   name,
			Status: "connected",
			Tools:  len(tools),
		})
	}
	data, err := json.Marshal(servers)
	if err == nil {
		ev.Content = string(data)
	}
	return ev
}

// Close marks the server as closed, preventing further output.
func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
}
