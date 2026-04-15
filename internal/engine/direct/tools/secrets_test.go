package tools

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestScanForSecretsDetectsEachPatternFamily pins one hit per named
// pattern so a regex regression surfaces against the specific family
// rather than a generic "some secret" failure.
func TestScanForSecretsDetectsEachPatternFamily(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		content string
		wantAny string
	}{
		{"openai", "API_KEY=" + "sk-" + "abcdefghijklmnopqrstuvwx", "openai_api_key"},
		{"anthropic", "key: " + "sk-ant-" + "api03-abcdefghij_klmnopqrstuvwx", "anthropic_api_key"},
		{"aws", "creds AKIAABCDEFGHIJKLMNOP end", "aws_access_key_id"},
		{"github", "token=" + "ghp_" + "abcdefghijklmnopqrstuvwxyzABCD1234", "github_token"},
		{"google", "GOOGLE=" + "AIza" + "SyAbcdefghijklmnopqrstuvwxyz0123456", "google_api_key"},
		{"slack", "slack: xoxb-1234567890-abcdefghij", "slack_token"},
		// Split literal so the source file never contains a string
		// that trips upstream secret scanners on this repo's CI.
		{"stripe", "live=" + "sk_" + "live_" + "abcdefghijklmnopqrstuvwx", "stripe_live_key"},
		{"pem", "-----BEGIN RSA PRIVATE KEY-----\nfake\n", "private_key_pem"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			found := ScanForSecrets(tc.content)
			assert.Contains(t, found, tc.wantAny)
		})
	}
}

// TestScanForSecretsIgnoresPlainText verifies the scanner does not
// fire on ordinary prose, code, or URLs so legitimate edits are not
// gated behind allow_secrets unnecessarily.
func TestScanForSecretsIgnoresPlainText(t *testing.T) {
	t.Parallel()

	samples := []string{
		"just a plain sentence with no tokens at all",
		"package foo\n\nfunc Bar() error { return nil }",
		"see https://example.com/docs for details",
		"sk-short", // too short to match the openai pattern (20+ chars after sk-)
		"AKIA-only-prefix-not-enough",
	}
	for _, s := range samples {
		assert.Empty(t, ScanForSecrets(s), "benign content must not trip the scanner: %q", s)
	}
}

// TestScanForSecretsDeduplicates verifies repeat matches of the same
// pattern collapse to a single entry in the output list.
func TestScanForSecretsDeduplicates(t *testing.T) {
	t.Parallel()

	content := "one " + "sk-" + "abcdefghijklmnopqrstuvwx and another " + "sk-" + "abcdefghijklmnopqrstABCDEF"
	found := ScanForSecrets(content)
	count := 0
	for _, name := range found {
		if name == "openai_api_key" {
			count++
		}
	}
	assert.Equal(t, 1, count, "duplicate matches must collapse to one name")
}

// TestFormatSecretsErrorMentionsOverrideFlag verifies the error string
// tells the caller how to bypass. Users who see the error need a clear
// path forward.
func TestFormatSecretsErrorMentionsOverrideFlag(t *testing.T) {
	t.Parallel()
	msg := FormatSecretsError([]string{"openai_api_key"})
	assert.Contains(t, msg, "openai_api_key")
	assert.Contains(t, strings.ToLower(msg), "allow_secrets")
}
