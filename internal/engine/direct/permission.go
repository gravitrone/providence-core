package direct

import (
	"context"
	"log"
	"sync"

	"github.com/google/uuid"
	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/gravitrone/providence-core/internal/engine/direct/tools"
	"github.com/gravitrone/providence-core/internal/engine/hooks"
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
	emitter    tools.HookEmitter
	denials    *permissions.DenialTracker
	autoMode   *permissions.AutoMode
	shadowed   []permissions.ShadowedRule
}

// NewPermissionHandler creates a permission handler with default allow rules
// for read-only tools (Read, Glob, Grep).
func NewPermissionHandler() *PermissionHandler {
	ph := &PermissionHandler{
		pending:    make(map[string]chan bool),
		allowRules: defaultAllowRules,
		denials:    permissions.NewDenialTracker(),
		autoMode:   permissions.NewAutoMode(),
	}
	ph.refreshShadowed()
	return ph
}

// NewPermissionHandlerWithRules creates a permission handler with custom
// allow and deny rules merged on top of the defaults.
func NewPermissionHandlerWithRules(allow, deny []permissions.Rule) *PermissionHandler {
	merged := make([]permissions.Rule, 0, len(defaultAllowRules)+len(allow))
	merged = append(merged, defaultAllowRules...)
	merged = append(merged, allow...)
	ph := &PermissionHandler{
		pending:    make(map[string]chan bool),
		allowRules: merged,
		denyRules:  deny,
		denials:    permissions.NewDenialTracker(),
		autoMode:   permissions.NewAutoMode(),
	}
	ph.refreshShadowed()
	return ph
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

	ph := &PermissionHandler{
		pending:    make(map[string]chan bool),
		allowRules: mergedAllow,
		denyRules:  mergedDeny,
		askRules:   configAsk,
		denials:    permissions.NewDenialTracker(),
		autoMode:   permissions.NewAutoMode(),
	}
	ph.refreshShadowed()
	return ph
}

// refreshShadowed recomputes the shadowed rule list across all rule kinds
// and logs a warning for each finding. Callers must hold no locks on p.mu.
func (p *PermissionHandler) refreshShadowed() {
	all := make([]permissions.Rule, 0, len(p.allowRules)+len(p.denyRules)+len(p.askRules))
	all = append(all, p.denyRules...)
	all = append(all, p.askRules...)
	all = append(all, p.allowRules...)
	shadowed := permissions.DetectShadowedRules(all)
	p.shadowed = shadowed
	for _, s := range shadowed {
		log.Printf("permissions: %s", s.Reason)
	}
}

// DenialHistory returns a snapshot of denials recorded this session, most
// recent first. Empty when no denials have occurred.
func (p *PermissionHandler) DenialHistory() []permissions.DenialRecord {
	if p.denials == nil {
		return nil
	}
	return p.denials.History()
}

// ShadowedRules returns the shadowed rule findings computed at load time.
func (p *PermissionHandler) ShadowedRules() []permissions.ShadowedRule {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]permissions.ShadowedRule, len(p.shadowed))
	copy(out, p.shadowed)
	return out
}

// SetAutoMode toggles the session-scoped auto-approval state for read-only
// tools. Writes, Bash, and Edit continue through the normal chain.
func (p *PermissionHandler) SetAutoMode(enabled bool) {
	if p.autoMode == nil {
		p.autoMode = permissions.NewAutoMode()
	}
	p.autoMode.SetAutoMode(enabled)
}

// AutoModeEnabled reports the current auto-approval state.
func (p *PermissionHandler) AutoModeEnabled() bool {
	if p.autoMode == nil {
		return false
	}
	return p.autoMode.Enabled()
}

// LoadPersistedRules loads previously saved rules for the project and merges
// them into the handler's allow list. Missing files are not an error.
func (p *PermissionHandler) LoadPersistedRules(projectPath string) error {
	rules, err := permissions.LoadRules(projectPath)
	if err != nil {
		return err
	}
	if len(rules) == 0 {
		return nil
	}
	p.mu.Lock()
	p.allowRules = append(p.allowRules, rules...)
	p.mu.Unlock()
	p.refreshShadowed()
	return nil
}

// PersistRules saves the current non-default allow rules to the on-disk
// project store using SaveRules.
func (p *PermissionHandler) PersistRules(projectPath string) error {
	p.mu.Lock()
	snapshot := make([]permissions.Rule, 0, len(p.allowRules))
	for _, r := range p.allowRules {
		if r.Source == "builtin" {
			continue
		}
		snapshot = append(snapshot, r)
	}
	p.mu.Unlock()
	return permissions.SaveRules(projectPath, snapshot)
}

// SetMode overrides the permission mode for this handler.
// Supported modes: "auto" (approve all), "deny" (deny all), "plan" (read-only).
func (p *PermissionHandler) SetMode(mode string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.mode = mode
}

// SetHookEmitter wires lifecycle hook dispatch for permission outcomes.
func (p *PermissionHandler) SetHookEmitter(emitter tools.HookEmitter) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.emitter = emitter
}

// Check evaluates the permission chain for the given tool invocation.
func (p *PermissionHandler) Check(t tools.Tool, input map[string]any) *permissions.Result {
	p.mu.Lock()
	mode := p.mode
	emitter := p.emitter
	autoMode := p.autoMode
	p.mu.Unlock()

	// autoApprove short-circuit for read-only tools (never overrides safety).
	if autoMode != nil && autoMode.IsAutoApproved(t.Name(), input) {
		return &permissions.Result{
			Decision: permissions.Allow,
			Reason:   "autoApprove: read-only tool",
			ToolName: t.Name(),
		}
	}

	switch mode {
	case "auto":
		return &permissions.Result{
			Decision: permissions.Allow,
			Reason:   "auto mode",
			ToolName: t.Name(),
		}
	case "deny":
		result := &permissions.Result{
			Decision: permissions.Deny,
			Reason:   "deny mode",
			ToolName: t.Name(),
		}
		p.onDenied(result, t.Name(), input, permissions.Rule{})
		emitPermissionHook(emitter, hooks.PermissionDenied, t.Name(), input, result.Reason)
		return result
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
	result := permissions.CheckPermission(ctx, t.Name(), input)
	if result != nil && result.Decision == permissions.Deny {
		matched := matchedDenyRule(p.denyRules, t.Name(), input)
		p.onDenied(result, t.Name(), input, matched)
		emitPermissionHook(emitter, hooks.PermissionDenied, t.Name(), input, result.Reason)
	}
	return result
}

// onDenied records the denial and attaches a human-readable explanation to
// the result. Safe to call with a zero-value rule when no explicit rule
// matched (mode-level deny, tool checker deny, etc).
func (p *PermissionHandler) onDenied(result *permissions.Result, tool string, input map[string]any, rule permissions.Rule) {
	if p.denials != nil {
		p.denials.Record(tool, input)
	}
	if result == nil {
		return
	}
	result.Reason = permissions.ExplainDenial(rule, tool, input) + " (" + result.Reason + ")"
}

// matchedDenyRule returns the first deny rule matching tool+input, or a
// zero-value Rule when none matches (for example when the chain denies via
// plan mode or dontAsk mode).
func matchedDenyRule(rules []permissions.Rule, tool string, input interface{}) permissions.Rule {
	for _, r := range rules {
		if permissions.MatchRule(r, tool, input) {
			return r
		}
	}
	return permissions.Rule{}
}

// NeedsPermission returns true if the tool requires explicit user approval
// according to the 7-step permission chain.
func (p *PermissionHandler) NeedsPermission(t tools.Tool, input map[string]any) bool {
	result := p.Check(t, input)
	if result == nil {
		return false
	}
	return result.Decision == permissions.Ask
}

// RequestPermission emits a permission_request ParsedEvent on the events channel
// and blocks until Respond is called with the matching questionID.
// Returns true if approved, false if denied. Returns an error if ctx is canceled.
func (p *PermissionHandler) RequestPermission(ctx context.Context, toolCallID string, events chan<- engine.ParsedEvent, toolName string, toolInput any) (bool, error) {
	questionID := uuid.New().String()

	ch := make(chan bool, 1)
	p.mu.Lock()
	p.pending[questionID] = ch
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		delete(p.pending, questionID)
		p.mu.Unlock()
	}()

	event := engine.ParsedEvent{
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

	select {
	case events <- event:
	case <-ctx.Done():
		return false, ctx.Err()
	}

	select {
	case approved := <-ch:
		if approved {
			p.mu.Lock()
			emitter := p.emitter
			p.mu.Unlock()
			emitPermissionHook(emitter, hooks.PermissionGranted, toolName, toolInput, "approved by user")
		}
		return approved, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// Respond resolves a pending permission request.
// questionID must match a pending request. approved=true for "allow".
func (p *PermissionHandler) Respond(questionID string, approved bool) {
	p.mu.Lock()
	ch, ok := p.pending[questionID]
	p.mu.Unlock()

	if ok {
		select {
		case ch <- approved:
		default:
		}
	}
}

func emitPermissionHook(emitter tools.HookEmitter, event string, toolName string, toolInput any, reason string) {
	if emitter == nil {
		return
	}
	emitter(event, hooks.HookInput{
		ToolName: toolName,
		ToolInput: map[string]any{
			"input":  toolInput,
			"reason": reason,
		},
	})
}
