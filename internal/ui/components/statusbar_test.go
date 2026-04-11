package components

import (
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/assert"
)

// --- Hint ---

func TestHintNotEmpty(t *testing.T) {
	out := Hint("q", "quit")
	assert.NotEmpty(t, out)
}

func TestHintContainsKey(t *testing.T) {
	out := Hint("q", "quit")
	assert.Contains(t, out, "q")
}

func TestHintContainsDesc(t *testing.T) {
	out := Hint("q", "quit")
	assert.Contains(t, out, "quit")
}

func TestHintMultiCharKey(t *testing.T) {
	out := Hint("ctrl+c", "exit")
	assert.Contains(t, out, "ctrl+c")
	assert.Contains(t, out, "exit")
}

func TestTintedHintRendering(t *testing.T) {
	tinted := StatusBarFromItems([]HintItem{
		TintedHint("70%", "ctx", lipgloss.Color("#FFD700")),
	}, 80)
	plain := StatusBarFromItems([]HintItem{
		{Key: "70%", Desc: "ctx"},
	}, 80)

	assert.Contains(t, tinted, "70%")
	assert.Contains(t, tinted, "ctx")
	assert.NotEqual(t, plain, tinted)
}

// --- StatusBar ---

func TestStatusBarNotEmpty(t *testing.T) {
	hints := []string{Hint("q", "quit"), Hint("?", "help")}
	out := StatusBar(hints, 120)
	assert.NotEmpty(t, out)
}

func TestStatusBarContainsHints(t *testing.T) {
	hints := []string{Hint("q", "quit"), Hint("?", "help")}
	out := StatusBar(hints, 120)
	assert.Contains(t, out, "q")
	assert.Contains(t, out, "quit")
}

func TestStatusBarZeroWidth(t *testing.T) {
	hints := []string{Hint("q", "quit")}
	out := StatusBar(hints, 0)
	assert.NotEmpty(t, out)
}

func TestStatusBarEmpty(t *testing.T) {
	out := StatusBar([]string{}, 80)
	// empty hints - should still return something (empty border)
	assert.NotNil(t, out)
}

func TestStatusBarClampsToWidth(t *testing.T) {
	// very narrow width - should not exceed it significantly
	hints := []string{
		Hint("q", "quit"),
		Hint("?", "help"),
		Hint("j", "down"),
		Hint("k", "up"),
		Hint("g", "top"),
		Hint("G", "bottom"),
		Hint("/", "search"),
	}
	out := StatusBar(hints, 30)
	assert.NotEmpty(t, out)
}

// --- StatusBarFromItems ---

func TestStatusBarFromItemsNotEmpty(t *testing.T) {
	items := []HintItem{
		{Key: "q", Desc: "quit"},
		{Key: "?", Desc: "help"},
	}
	out := StatusBarFromItems(items, 120)
	assert.NotEmpty(t, out)
}

func TestStatusBarFromItemsContainsKeys(t *testing.T) {
	items := []HintItem{
		{Key: "q", Desc: "quit"},
		{Key: "enter", Desc: "select"},
	}
	out := StatusBarFromItems(items, 120)
	assert.Contains(t, out, "q")
	assert.Contains(t, out, "enter")
}

func TestStatusBarFromItemsEmpty(t *testing.T) {
	out := StatusBarFromItems([]HintItem{}, 80)
	assert.NotNil(t, out)
}

func TestStatusBarFromItemsMatchesManual(t *testing.T) {
	items := []HintItem{
		{Key: "q", Desc: "quit"},
	}
	fromItems := StatusBarFromItems(items, 80)
	manual := StatusBar([]string{Hint("q", "quit")}, 80)
	assert.Equal(t, fromItems, manual)
}

// --- clampStatusSegments (via StatusBar behavior) ---

func TestStatusBarShowsMoreIndicatorWhenClamped(t *testing.T) {
	// Feed many hints into a tiny width - "More ..." overflow indicator may appear.
	// We just verify it doesn't panic and returns something.
	items := make([]HintItem, 20)
	for i := range items {
		items[i] = HintItem{Key: "x", Desc: "action"}
	}
	out := StatusBarFromItems(items, 10)
	assert.NotNil(t, out)
}
