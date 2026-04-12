package opencode

import (
	"testing"

	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenCodeEngineCreation(t *testing.T) {
	cfg := engine.EngineConfig{
		Type:  EngineTypeOpenCode,
		Model: "gpt-4o",
	}
	e, err := NewOpenCodeEngine(cfg)
	require.NoError(t, err)
	require.NotNil(t, e)

	oc, ok := e.(*OpenCodeEngine)
	require.True(t, ok)
	assert.Equal(t, "gpt-4o", oc.model)
	assert.NotNil(t, oc.events)
}

func TestOpenCodeEngineStatus(t *testing.T) {
	cfg := engine.EngineConfig{Model: "gpt-4o"}
	e, err := NewOpenCodeEngine(cfg)
	require.NoError(t, err)

	assert.Equal(t, engine.StatusIdle, e.Status(), "initial status should be idle")
}

func TestOpenCodeEngineFactoryRegistered(t *testing.T) {
	cfg := engine.EngineConfig{
		Type:  EngineTypeOpenCode,
		Model: "gpt-4o",
	}
	e, err := engine.NewEngine(cfg)
	require.NoError(t, err)
	require.NotNil(t, e)

	_, ok := e.(*OpenCodeEngine)
	assert.True(t, ok, "factory should produce an OpenCodeEngine")
}

func TestOpenCodeEngineSendReturnsError(t *testing.T) {
	cfg := engine.EngineConfig{Model: "gpt-4o"}
	e, err := NewOpenCodeEngine(cfg)
	require.NoError(t, err)

	err = e.Send("hello")
	assert.Error(t, err, "send should error when engine is not connected")
}

func TestOpenCodeEngineEventsChannel(t *testing.T) {
	cfg := engine.EngineConfig{Model: "gpt-4o"}
	e, err := NewOpenCodeEngine(cfg)
	require.NoError(t, err)

	ch := e.Events()
	assert.NotNil(t, ch, "events channel should not be nil")
}
