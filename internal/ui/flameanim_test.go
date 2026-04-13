package ui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlameColorReturnsHex(t *testing.T) {
	fc := flameColor(0)
	require.NotEmpty(t, fc)
	assert.True(t, strings.HasPrefix(fc, "#"), "flameColor should return hex string, got %q", fc)
	assert.Len(t, fc, 7, "hex color should be 7 chars (#RRGGBB)")
}

func TestFlameColorChangesPerFrame(t *testing.T) {
	a := flameColor(0)
	b := flameColor(5)
	assert.NotEqual(t, a, b, "flameColor(0) and flameColor(5) should differ")
}

func TestRenderToolShimmerContainsANSI(t *testing.T) {
	out := renderToolShimmer("hello", 0)
	require.NotEmpty(t, out)
	assert.Contains(t, out, "\x1b[", "shimmer output should contain ANSI escape codes")
}

func TestCompletionCoolRampLength(t *testing.T) {
	// recomputeFlameGradients is called by init() via ApplyTheme.
	require.Len(t, completionCoolRamp, 100, "completionCoolRamp should have exactly 100 entries")
}

func TestBannerGradientLength(t *testing.T) {
	require.Len(t, bannerAnimGradient, 16, "bannerAnimGradient should have 16 entries")
	require.Len(t, bannerGradient, len(bannerLines), "bannerGradient should match bannerLines count")
}
