package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKeyMapHasQuit(t *testing.T) {
	km := DefaultKeyMap()
	keys := km.Quit.Keys()
	require.NotEmpty(t, keys, "Quit binding should have at least one key")
	assert.Contains(t, keys, "q")
}

func TestKeyMapHelp(t *testing.T) {
	km := DefaultKeyMap()
	h := km.Quit.Help()
	assert.NotEmpty(t, h.Key, "Quit help key should be non-empty")
	assert.NotEmpty(t, h.Desc, "Quit help desc should be non-empty")

	h2 := km.Up.Help()
	assert.NotEmpty(t, h2.Key)
	assert.NotEmpty(t, h2.Desc)
}
