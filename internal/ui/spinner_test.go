package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRandomToolFlavorNonEmpty(t *testing.T) {
	for i := 0; i < 20; i++ {
		got := randomToolFlavor()
		assert.NotEmpty(t, got, "randomToolFlavor should never return empty string")
	}
}

func TestRandomCompactVerbNonEmpty(t *testing.T) {
	for i := 0; i < 20; i++ {
		got := randomCompactVerb("seed")
		assert.NotEmpty(t, got, "randomCompactVerb should never return empty string")
	}
}

func TestSpinnerFramesNotEmpty(t *testing.T) {
	require.NotEmpty(t, spinnerFrames, "spinnerFrames should have elements")
}
