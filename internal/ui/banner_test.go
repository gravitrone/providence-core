package ui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRenderBannerNotEmpty(t *testing.T) {
	out := RenderBanner()
	assert.NotEmpty(t, out)
}

func TestRenderBannerContainsArtText(t *testing.T) {
	out := RenderBanner()
	// The banner art contains these characters - after ANSI strip, raw chars survive
	// since lipgloss adds color codes but preserves the rune content.
	assert.Contains(t, out, "█", "banner should contain block character from ASCII art")
}

func TestRenderBannerIsMultiLine(t *testing.T) {
	out := RenderBanner()
	lines := strings.Split(out, "\n")
	assert.Greater(t, len(lines), 5, "banner should span multiple lines")
}

func TestRenderBannerStartsWithNewline(t *testing.T) {
	out := RenderBanner()
	assert.True(t, strings.HasPrefix(out, "\n"), "banner should start with a newline")
}

func TestRenderBannerEndsWithNewline(t *testing.T) {
	out := RenderBanner()
	assert.True(t, strings.HasSuffix(out, "\n"), "banner should end with a newline")
}

func TestRenderBannerIsDeterministic(t *testing.T) {
	first := RenderBanner()
	second := RenderBanner()
	assert.Equal(t, first, second, "banner output should be deterministic")
}
