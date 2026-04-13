package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gravitrone/providence-core/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestNewAppNoPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		_ = NewApp("claude", config.Config{}, nil, nil)
	})
}

func TestAppUpdate(t *testing.T) {
	app := NewApp("claude", config.Config{}, nil, nil)
	msg := tea.WindowSizeMsg{Width: 120, Height: 40}

	updated, cmd := app.Update(msg)
	assert.Nil(t, cmd, "WindowSizeMsg should not produce a command")

	a, ok := updated.(App)
	assert.True(t, ok, "Update should return an App")
	assert.Equal(t, 120, a.width)
	assert.Equal(t, 40, a.height)
}
