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
	askRules   []permissions.Rule
	mode       string // "", "auto", "deny", "plan"
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

// NewPermissionHandlerWithConfig creates a permission handler with rules from
// both explicit allow/deny lists and config permission rules. Config rules are
// appended after the defaults and explicit rules.
func NewPermissionHandlerWithConfig(allow, deny []permissions.Rule, configAllow, configDeny, configAsk []permissions.Rule) *PermissionHandler {
	mergedAllow := make([]permissions.Rule, 0, len(defaultAllowRules)+len(allow)+len(configAllow))
	mergedAllow = append(mergedAllow, defaultAllowRules...)
	mergedAllow = append(mergedAllow, allow...)
	mergedAllow = append(mergedAllow, configAllow...)

	mergedDeny := make([]permissions.Rule, 0, len(deny)+len(configDeny))
	mergedDeny = append(mergedDeny, deny...)
	mergedDeny = append(mergedDeny, configDeny...)

	return &PermissionHandler{
		pending:    make(map[string]chan bool),
		allowRules: mergedAllow,
		denyRules:  mergedDeny,
		askRules:   configAsk,
	}
}

// SetMode overrides the permission mode for this handler.
// Supported modes: "auto" (approve all), "deny" (deny all), "plan" (read-only).
func (p *PermissionHandler) SetMode(mode string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.mode = mode
}

// NeedsPermission returns true if the tool requires explicit user approval
// according to the 7-step permission chain.
func (p *PermissionHandler) NeedsPermission(t tools.Tool) bool {
	p.mu.Lock()
	mode := p.mode
	p.mu.Unlock()

	// Short-circuit for override modes.
	switch mode {
	case "auto":
		return false // auto-approve everything
	case "deny":
		return true // always ask (caller will deny)
	}

	permMode := permissions.ModeDefault
	if mode == "plan" {
		permMode = permissions.ModePlan
	}

	ctx := &permissions.Context{
		Mode:             permMode,
		AlwaysAllowRules: p.allowRules,
		AlwaysDenyRules:  p.denyRules,
		AlwaysAskRules:   p.askRules,
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
