package tools

import (
	"fmt"
	"regexp"
	"strings"
)

// secretPattern is a single named regex the scanner checks against
// content destined for disk. Patterns cover widely-used bearer-token
// shapes so an assistant cannot accidentally paste a real credential
// into tracked files. The list is intentionally narrow - too aggressive
// and legitimate edits trip the guard.
type secretPattern struct {
	name string
	re   *regexp.Regexp
}

var secretPatterns = []secretPattern{
	{
		name: "openai_api_key",
		re:   regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`),
	},
	{
		name: "anthropic_api_key",
		re:   regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}\b`),
	},
	{
		name: "aws_access_key_id",
		re:   regexp.MustCompile(`\b(AKIA|ASIA)[0-9A-Z]{16}\b`),
	},
	{
		name: "github_token",
		re:   regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9_]{20,}\b`),
	},
	{
		name: "google_api_key",
		re:   regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`),
	},
	{
		name: "slack_token",
		re:   regexp.MustCompile(`\bxox[abpr]-[A-Za-z0-9-]{10,}\b`),
	},
	{
		name: "stripe_live_key",
		re:   regexp.MustCompile(`\b(sk|rk)_live_[A-Za-z0-9]{20,}\b`),
	},
	{
		name: "private_key_pem",
		re:   regexp.MustCompile(`-----BEGIN\s+(?:RSA|OPENSSH|EC|DSA|PGP|ENCRYPTED)?\s*PRIVATE KEY-----`),
	},
}

// ScanForSecrets returns a deduplicated list of pattern names that
// match anywhere in content. The caller reports the names - we do not
// leak the matched substring back to the tool surface.
func ScanForSecrets(content string) []string {
	seen := map[string]bool{}
	var found []string
	for _, p := range secretPatterns {
		if p.re.MatchString(content) {
			if seen[p.name] {
				continue
			}
			seen[p.name] = true
			found = append(found, p.name)
		}
	}
	return found
}

// FormatSecretsError builds the error message returned when secrets
// are detected. Callers set IsError = true on the ToolResult.
func FormatSecretsError(names []string) string {
	return fmt.Sprintf(
		"secret-like content detected (%s); pass allow_secrets: true to override after manual review",
		strings.Join(names, ", "),
	)
}
