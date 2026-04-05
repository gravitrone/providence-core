package components

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- SanitizeText ---

func TestSanitizeTextPassthroughClean(t *testing.T) {
	input := "Hello, World!"
	assert.Equal(t, input, SanitizeText(input))
}

func TestSanitizeTextEmpty(t *testing.T) {
	assert.Equal(t, "", SanitizeText(""))
}

func TestSanitizeTextStripsAnsiCSI(t *testing.T) {
	// \x1b[31m is red foreground color code (CSI sequence)
	input := "\x1b[31mred text\x1b[0m"
	out := SanitizeText(input)
	assert.Equal(t, "red text", out)
}

func TestSanitizeTextStripsAnsiOSC(t *testing.T) {
	// OSC hyperlink sequence
	input := "\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\"
	out := SanitizeText(input)
	assert.NotContains(t, out, "\x1b")
}

func TestSanitizeTextStripsAnsiDCS(t *testing.T) {
	input := "\x1bPsome data\x1b\\"
	out := SanitizeText(input)
	assert.NotContains(t, out, "\x1b")
}

func TestSanitizeTextStripsAnsiAPC(t *testing.T) {
	input := "\x1b_app data\x1b\\"
	out := SanitizeText(input)
	assert.NotContains(t, out, "\x1b")
}

func TestSanitizeTextPreservesNewlines(t *testing.T) {
	input := "line1\nline2\nline3"
	out := SanitizeText(input)
	assert.Equal(t, "line1\nline2\nline3", out)
}

func TestSanitizeTextPreservesTabs(t *testing.T) {
	input := "col1\tcol2"
	out := SanitizeText(input)
	assert.Equal(t, "col1\tcol2", out)
}

func TestSanitizeTextStripsNullByte(t *testing.T) {
	input := "hello\x00world"
	out := SanitizeText(input)
	assert.Equal(t, "helloworld", out)
}

func TestSanitizeTextStripsBidi(t *testing.T) {
	// U+202E is a bidi override character
	input := "normal\u202etext"
	out := SanitizeText(input)
	assert.Equal(t, "normaltext", out)
}

func TestSanitizeTextStripsMultipleAnsiSequences(t *testing.T) {
	input := "\x1b[1m\x1b[32mbold green\x1b[0m"
	out := SanitizeText(input)
	assert.Equal(t, "bold green", out)
}

func TestSanitizeTextStripsBellChar(t *testing.T) {
	input := "ring\x07bell"
	out := SanitizeText(input)
	assert.Equal(t, "ringbell", out)
}

func TestSanitizeTextStripsCarriageReturn(t *testing.T) {
	input := "line\r\n"
	out := SanitizeText(input)
	// \r is a control char and gets stripped; \n is preserved
	assert.Equal(t, "line\n", out)
}

// --- SanitizeOneLine ---

func TestSanitizeOneLinePassthroughClean(t *testing.T) {
	input := "Hello, World!"
	assert.Equal(t, input, SanitizeOneLine(input))
}

func TestSanitizeOneLineEmpty(t *testing.T) {
	assert.Equal(t, "", SanitizeOneLine(""))
}

func TestSanitizeOneLineCollapsesNewlines(t *testing.T) {
	input := "line1\nline2\nline3"
	out := SanitizeOneLine(input)
	assert.NotContains(t, out, "\n")
	assert.Contains(t, out, "line1")
	assert.Contains(t, out, "line2")
}

func TestSanitizeOneLineCollapsesTab(t *testing.T) {
	input := "col1\tcol2"
	out := SanitizeOneLine(input)
	assert.NotContains(t, out, "\t")
	assert.Contains(t, out, "col1")
	assert.Contains(t, out, "col2")
}

func TestSanitizeOneLineTrimsWhitespace(t *testing.T) {
	input := "  spaces  "
	out := SanitizeOneLine(input)
	assert.Equal(t, "spaces", out)
}

func TestSanitizeOneLineStripsAnsiAndFlattens(t *testing.T) {
	input := "\x1b[31mred\x1b[0m\ntext"
	out := SanitizeOneLine(input)
	assert.Equal(t, "red text", out)
}

func TestSanitizeOneLineHandlesOnlyWhitespace(t *testing.T) {
	out := SanitizeOneLine("   \t\n  ")
	assert.Equal(t, "", out)
}

func TestSanitizeOneLineHandlesCRLF(t *testing.T) {
	out := SanitizeOneLine("line1\r\nline2")
	assert.NotContains(t, out, "\n")
	assert.NotContains(t, out, "\r")
}

func TestSanitizeOneLinePreservesInternalSpaces(t *testing.T) {
	input := "hello world"
	out := SanitizeOneLine(input)
	assert.Equal(t, "hello world", out)
}
