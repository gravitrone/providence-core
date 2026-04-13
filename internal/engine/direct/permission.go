package direct

import (
	"sync"

	"github.com/google/uuid"
	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/gravitrone/providence-core/internal/engine/direct/tools"
	"github.com/gravitrone/providence-core/internal/engine/permissions"
)

// defaultAllowRules auto-approves read-only tools that never mutate state.
var defaultAllowRules = []permissions.Rule{
	{Pattern: "Read", Behavior: permissions.Allow, Source: "builtin"},
	{Pattern: "Glob", Behavior: permissions.Allow, Source: "builtin"},
	{Pattern: "Grep", Behavior: permissions.Allow, Source: "builtin"},
}

// PermissionHandler manages permission requests for tool execution.
// Non-read-only tools require explicit approval before running.
type PermissionHandler struct {
	pending    map[string]chan bool // questionID -> response channel
	mu         sync.Mutex
	allowRules []permissions.Rule
	denyRules  []permissions.Rule
}

// NewPermissionHandler creates a permission handler with default allow rules
// for read-only tools (Read, Glob, Grep).
func NewPermissionHandler() *PermissionHandler {
	return &PermissionHandler{
		pending:    make(map[string]chan bool),
		allowRules: defaultAllowRules,
	}
}

// NewPermissionHandlerWithRules creates a permission handler with custom
// allow and deny rules merged on top of the defaults.
func NewPermissionHandlerWithRules(allow, deny []permissions.Rule) *PermissionHandler {
	merged := make([]permissions.Rule, 0, len(defaultAllowRules)+len(allow))
	merged = append(merged, defaultAllowRules...)
	merged = append(merged, allow...)
	return &PermissionHandler{
		pending:    make(map[string]chan bool),
		allowRules: merged,
		denyRules:  deny,
	}
}

// NeedsPermission returns true if the tool requires explicit user approval
// according to the 7-step permission chain.
func (p *PermissionHandler) NeedsPermission(t tools.Tool) bool {
	ctx := &permissions.Context{
		Mode:             permissions.ModeDefault,
		AlwaysAllowRules: p.allowRules,
		AlwaysDenyRules:  p.denyRules,
	}
	result := permissions.CheckPermission(ctx, t.Name(), nil)
	return result.Decision == permissions.Ask
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
