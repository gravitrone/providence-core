package ui

// slash_parser_test.go - edge case tests for slash command parsing.
//
// The parser lives in handleSlashCommand (agent_tab.go:4670).
// Core logic: strings.SplitN(text, " ", 2) -> cmd (lowercased) + args.
// We test it two ways:
//   1. parseSlashInput helper (mirrors logic, no side-effects)
//   2. handleSlashCommand directly via NewAgentTab (same package)

import (
	"strings"
	"testing"

	"github.com/gravitrone/providence-core/internal/config"
	"github.com/stretchr/testify/assert"
)

// parseSlashInput mirrors the exact parsing logic in handleSlashCommand.
// Kept here so edge-case tests don't need a full AgentTab or side-effects.
func parseSlashInput(text string) (cmd, args string) {
	parts := strings.SplitN(text, " ", 2)
	cmd = strings.ToLower(parts[0])
	if len(parts) > 1 {
		args = parts[1]
	}
	return
}

// TestSlashParser_CommandWithSpecialChars verifies that special chars in args
// are passed through unchanged.
func TestSlashParser_CommandWithSpecialChars(t *testing.T) {
	cmd, args := parseSlashInput("/note hello! @#$%^")
	assert.Equal(t, "/note", cmd)
	assert.Equal(t, "hello! @#$%^", args)
}

// TestSlashParser_MalformedFlagsHandled verifies no panic for malformed flags.
func TestSlashParser_MalformedFlagsHandled(t *testing.T) {
	assert.NotPanics(t, func() {
		cmd, args := parseSlashInput("/cmd --bogus=")
		assert.Equal(t, "/cmd", cmd)
		assert.Equal(t, "--bogus=", args)
	})
}

// TestSlashParser_EmptyArgsAfterSpace: trailing space means empty args string.
func TestSlashParser_EmptyArgsAfterSpace(t *testing.T) {
	cmd, args := parseSlashInput("/cmd ")
	assert.Equal(t, "/cmd", cmd)
	assert.Equal(t, "", args)
}

// TestSlashParser_NoArgs: no space means args is empty string.
func TestSlashParser_NoArgs(t *testing.T) {
	cmd, args := parseSlashInput("/cmd")
	assert.Equal(t, "/cmd", cmd)
	assert.Equal(t, "", args)
}

// TestSlashParser_MultiWordArgs: everything after first space is args.
func TestSlashParser_MultiWordArgs(t *testing.T) {
	cmd, args := parseSlashInput("/note hello world from me")
	assert.Equal(t, "/note", cmd)
	assert.Equal(t, "hello world from me", args)
}

// TestSlashParser_QuotedArgsPreservedOrSplit: quotes are not special to the
// parser - they pass through verbatim (split on first space only).
func TestSlashParser_QuotedArgsPreservedOrSplit(t *testing.T) {
	input := `/note "hello world"`
	cmd, args := parseSlashInput(input)
	assert.Equal(t, "/note", cmd)
	// Parser does NOT interpret quotes - raw args contains the quote chars.
	assert.Equal(t, `"hello world"`, args)
}

// TestSlashParser_LeadingTrailingWhitespace: the parser does NOT strip leading
// whitespace from the raw input (that's the caller's job). Verify it doesn't
// panic. The first "word" before the space is the cmd; leading-space input
// produces a cmd like "  /cmd" (first token before first space boundary).
// We document the actual behavior: SplitN on first space splits "  /cmd" as
// the first part (no leading space before the input starts matters - SplitN
// splits on the FIRST space character).
func TestSlashParser_LeadingTrailingWhitespace(t *testing.T) {
	assert.NotPanics(t, func() {
		// "  /cmd  args  " splits at first space: parts[0]="", parts[1]=" /cmd  args  "
		// cmd = "" (lowercased empty first token), args = " /cmd  args  "
		cmd, args := parseSlashInput("  /cmd  args  ")
		// Parser is not whitespace-aware - just assert no panic and types are strings.
		assert.IsType(t, "", cmd)
		assert.IsType(t, "", args)
		// Combined, original content is preserved (no data loss).
		assert.Equal(t, "  /cmd  args  ", cmd+" "+args)
	})
}

// TestSlashParser_UnknownCommandReturnsGraceful verifies that an unknown slash
// command fed into handleSlashCommand returns (false, nil) without panicking.
func TestSlashParser_UnknownCommandReturnsGraceful(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil, nil)
	assert.NotPanics(t, func() {
		handled, cmd := at.handleSlashCommand("/notreal")
		assert.False(t, handled, "unknown command should return handled=false")
		assert.Nil(t, cmd, "unknown command should return nil tea.Cmd")
	})
}
