package permissions

// CheckPermission implements the 7-step CC permission decision chain.
// The chain evaluates rules in strict order - early matches short-circuit.
//
// Step 1a: alwaysDenyRules -> DENY
// Step 1b: alwaysAskRules -> ASK
// Step 1c-f: tool-specific checker (deny, interactive ask, safety ask)
// Step 1g: safety path check (.git/, .claude/, shell configs) -> ASK
// Step 2a: bypass mode -> ALLOW
// Step 2b: alwaysAllowRules -> ALLOW
// Step 3: mode-specific defaults
// Fallback: ASK
func CheckPermission(ctx *Context, toolName string, input interface{}) *Result {
	// Step 1a: tool in alwaysDenyRules? -> DENY
	if matchesRules(ctx.AlwaysDenyRules, toolName, input) {
		return &Result{Decision: Deny, Reason: "denied by always-deny rule", ToolName: toolName}
	}

	// Step 1b: tool in alwaysAskRules? -> ASK
	if matchesRules(ctx.AlwaysAskRules, toolName, input) {
		return &Result{Decision: Ask, Reason: "required by always-ask rule", ToolName: toolName}
	}

	// Step 1c: tool.CheckPermissions() - tool-specific logic
	if checker, ok := ctx.ToolCheckers[toolName]; ok {
		result, err := checker.CheckPermissions(toolName, input)
		if err == nil && result != nil {
			result.ToolName = toolName

			// Step 1d: tool returned DENY? -> DENY
			if result.Decision == Deny {
				return result
			}

			// Step 1e: requiresUserInteraction? -> ASK (unoverridable)
			if result.Decision == Ask && checker.RequiresUserInteraction() {
				return result
			}

			// Step 1f: content-specific ask (bypass-immune)
			if result.Decision == Ask && result.IsSafetyCheck {
				return result
			}
		}
	}

	// Step 1g: safety checks (.git/, .claude/, shell configs)
	if isSafetyPath(toolName, input) {
		return &Result{Decision: Ask, Reason: "safety check: protected path", ToolName: toolName, IsSafetyCheck: true}
	}

	// Step 2a: mode bypass
	if ctx.Mode == ModeBypassPermissions {
		return &Result{Decision: Allow, Reason: "bypass mode", ToolName: toolName}
	}

	// Step 2b: tool in alwaysAllowRules? -> ALLOW
	if matchesRules(ctx.AlwaysAllowRules, toolName, input) {
		return &Result{Decision: Allow, Reason: "allowed by always-allow rule", ToolName: toolName}
	}

	// Step 3: mode-specific defaults
	switch ctx.Mode {
	case ModeAcceptEdits:
		if isFileEditTool(toolName) || isAcceptEditsCommand(toolName, input) {
			return &Result{Decision: Allow, Reason: "acceptEdits mode", ToolName: toolName}
		}
	case ModePlan:
		if isReadOnlyTool(toolName) {
			return &Result{Decision: Allow, Reason: "plan mode (read-only)", ToolName: toolName}
		}
		return &Result{Decision: Deny, Reason: "plan mode: write tools blocked", ToolName: toolName}
	case ModeDontAsk:
		return &Result{Decision: Deny, Reason: "dontAsk mode: uncertain", ToolName: toolName}
	}

	// Default: ASK
	return &Result{Decision: Ask, Reason: "default: requires approval", ToolName: toolName}
}
