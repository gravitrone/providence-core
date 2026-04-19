package permissions

import (
	"fmt"
	"strings"
)

// --- Shadowed Rule Detection ---

// ShadowedRule is a rule that can never fire because an earlier rule subsumes it.
type ShadowedRule struct {
	// Shadowed is the rule that never matches.
	Shadowed Rule
	// ShadowedBy is the earlier rule that subsumes Shadowed.
	ShadowedBy Rule
	// Reason explains why the later rule is unreachable.
	Reason string
}

// DetectShadowedRules walks rules in order and reports entries that can never
// fire because an earlier rule subsumes their pattern. Shadowing only applies
// when the earlier rule's behavior would short-circuit the later rule in the
// decision chain. The function does not modify the input slice.
func DetectShadowedRules(rules []Rule) []ShadowedRule {
	if len(rules) < 2 {
		return nil
	}
	shadowed := make([]ShadowedRule, 0)
	for i := 1; i < len(rules); i++ {
		later := rules[i]
		for j := 0; j < i; j++ {
			earlier := rules[j]
			if !patternSubsumes(earlier.Pattern, later.Pattern) {
				continue
			}
			if !behaviorShadows(earlier.Behavior, later.Behavior) {
				continue
			}
			shadowed = append(shadowed, ShadowedRule{
				Shadowed:   later,
				ShadowedBy: earlier,
				Reason: fmt.Sprintf(
					"rule %q (%s) is covered by earlier rule %q (%s)",
					later.Pattern, later.Behavior,
					earlier.Pattern, earlier.Behavior,
				),
			})
			break
		}
	}
	return shadowed
}

// patternSubsumes returns true when every input matched by sub is also matched
// by super. It handles two common cases: identical patterns and glob-on-tool
// patterns subsuming their specific argument children (for example
// "Bash(git *)" subsumes "Bash(git push)").
func patternSubsumes(super, sub string) bool {
	if super == sub {
		return true
	}
	superTool, superArg := splitPattern(super)
	subTool, subArg := splitPattern(sub)
	if !matchGlob(superTool, subTool) {
		return false
	}
	// Bare-tool pattern covers every argumented variant of the same tool.
	if superArg == "" {
		return true
	}
	if subArg == "" {
		return false
	}
	return globCovers(superArg, subArg)
}

// splitPattern breaks "Tool(arg)" into ("Tool", "arg"). Patterns without
// parentheses split into (pattern, "").
func splitPattern(p string) (tool, arg string) {
	if parenIdx := strings.IndexByte(p, '('); parenIdx >= 0 && strings.HasSuffix(p, ")") {
		return p[:parenIdx], p[parenIdx+1 : len(p)-1]
	}
	return p, ""
}

// globCovers returns true when every string matched by sub is also matched
// by super. The heuristic is conservative: identical globs cover each other,
// and "prefix*" covers any string with that prefix.
func globCovers(super, sub string) bool {
	if super == sub {
		return true
	}
	if strings.HasSuffix(super, "*") {
		prefix := strings.TrimSuffix(super, "*")
		if strings.HasPrefix(sub, prefix) {
			return true
		}
	}
	return false
}

// behaviorShadows returns true when a later rule would never fire because the
// earlier rule's behavior would short-circuit the chain first. Allow shadows
// allow+ask, deny shadows everything, ask shadows ask.
func behaviorShadows(earlier, later Decision) bool {
	switch earlier {
	case Deny:
		return true
	case Ask:
		return later == Ask
	case Allow:
		return later == Allow || later == Ask
	}
	return false
}
