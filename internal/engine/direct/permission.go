package direct

import (
	"sync"

	"github.com/google/uuid"
	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/gravitrone/providence-core/internal/engine/direct/tools"
)

// PermissionHandler manages permission requests for tool execution.
// Non-read-only tools require explicit approval before running.
type PermissionHandler struct {
	pending map[string]chan bool // questionID -> response channel
	mu      sync.Mutex
}

// NewPermissionHandler creates a permission handler with no pending requests.
func NewPermissionHandler() *PermissionHandler {
	return &PermissionHandler{
		pending: make(map[string]chan bool),
	}
}

// NeedsPermission returns true if the tool requires explicit approval.
// Currently auto-approves all tools in direct engine mode - the user launched it themselves.
// TODO: add configurable permission modes (auto, ask, deny) per tool.
func (p *PermissionHandler) NeedsPermission(_ tools.Tool) bool {
	return false
}

// RequestPermission emits a permission_request ParsedEvent on the events channel
// and blocks until Respond is called with the matching questionID.
// Returns true if approved, false if denied.
func (p *PermissionHandler) RequestPermission(toolCallID string, events chan<- engine.ParsedEvent, toolName string, toolInput any) bool {
	questionID := uuid.New().String()

	ch := make(chan bool, 1)
	p.mu.Lock()
	p.pending[questionID] = ch
	p.mu.Unlock()

	events <- engine.ParsedEvent{
		Type: "permission_request",
		Data: &engine.PermissionRequestEvent{
			Type: "permission_request",
			Tool: engine.PermissionTool{
				Name:  toolName,
				Input: toolInput,
			},
			QuestionID: questionID,
			Options: []engine.PermissionOption{
				{ID: "allow", Label: "Allow"},
				{ID: "deny", Label: "Deny"},
			},
		},
	}

	approved := <-ch

	p.mu.Lock()
	delete(p.pending, questionID)
	p.mu.Unlock()

	return approved
}

// Respond resolves a pending permission request.
// questionID must match a pending request. approved=true for "allow".
func (p *PermissionHandler) Respond(questionID string, approved bool) {
	p.mu.Lock()
	ch, ok := p.pending[questionID]
	p.mu.Unlock()

	if ok {
		ch <- approved
	}
}
