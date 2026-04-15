package opencode

import (
	"context"
	"sync"
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

func TestOpenCodeEngineSendEmitsSystemEvent(t *testing.T) {
	cfg := engine.EngineConfig{Model: "gpt-4o"}
	e, err := NewOpenCodeEngine(cfg)
	require.NoError(t, err)

	err = e.Send("hello")
	assert.NoError(t, err, "send should not error - it emits a system event instead")

	// Drain the event to confirm it was emitted.
	ev := <-e.Events()
	assert.Equal(t, "system_message", ev.Type, "event type should be system_message")
	sme, ok := ev.Data.(*engine.SystemMessageEvent)
	require.True(t, ok, "event data should be SystemMessageEvent")
	assert.Contains(t, sme.Content, "opencode serve", "message should mention opencode serve")
}

func TestOpenCodeEngineEventsChannel(t *testing.T) {
	cfg := engine.EngineConfig{Model: "gpt-4o"}
	e, err := NewOpenCodeEngine(cfg)
	require.NoError(t, err)

	ch := e.Events()
	assert.NotNil(t, ch, "events channel should not be nil")
}

// TestNewOpenCodeEngineEventChannelCapacity verifies the event channel
// is buffered at 100 so a burst of events during handshake does not
// block the producer. Regressions here would surface as deadlocks
// under load rather than obvious crashes.
func TestNewOpenCodeEngineEventChannelCapacity(t *testing.T) {
	t.Parallel()

	e, err := NewOpenCodeEngine(engine.EngineConfig{Model: "gpt-4o"})
	require.NoError(t, err)

	oc, ok := e.(*OpenCodeEngine)
	require.True(t, ok)
	assert.Equal(t, 100, cap(oc.events),
		"events channel capacity must stay at 100 - reducing it risks producer blocks under burst")
}

// TestOpenCodeSendEmitsGuidanceWithInstallURL extends the existing
// Send-emits-system-event test by also pinning the GitHub install URL
// in the guidance content. If a future copy-edit drops the URL the
// user loses the single cue pointing them at how to enable the engine.
func TestOpenCodeSendEmitsGuidanceWithInstallURL(t *testing.T) {
	t.Parallel()

	e, err := NewOpenCodeEngine(engine.EngineConfig{Model: "gpt-4o"})
	require.NoError(t, err)
	require.NoError(t, e.Send("hi"))

	ev := <-e.Events()
	sme, ok := ev.Data.(*engine.SystemMessageEvent)
	require.True(t, ok)
	assert.Contains(t, sme.Content, "github.com/sst/opencode",
		"guidance must include the GitHub install URL so users know how to enable the engine")
}

// TestOpenCodeRespondPermissionReturnsStubError verifies the stub
// contract: the method must return a non-nil error rather than a
// silent nil. Downstream /ask UX relies on the error to decide
// whether to route the request elsewhere; a silent nil would deadlock
// the permission prompt.
func TestOpenCodeRespondPermissionReturnsStubError(t *testing.T) {
	t.Parallel()

	e, err := NewOpenCodeEngine(engine.EngineConfig{Model: "gpt-4o"})
	require.NoError(t, err)

	err = e.RespondPermission("q-1", "allow")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission",
		"error must identify which capability is missing so callers can diagnose")
}

// TestOpenCodeTriggerCompactReturnsStubError verifies TriggerCompact
// surfaces as a clean error so the compaction orchestrator falls back
// to another strategy rather than hanging waiting for a result.
func TestOpenCodeTriggerCompactReturnsStubError(t *testing.T) {
	t.Parallel()

	e, err := NewOpenCodeEngine(engine.EngineConfig{Model: "gpt-4o"})
	require.NoError(t, err)

	err = e.TriggerCompact(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "compaction")
}

// TestOpenCodeStatusConcurrentReadsAreRaceFree spins N goroutines each
// calling Status() many times. Passes under `-race` because Status
// holds the mutex; a regression that dropped the lock would surface
// here as a data race report.
func TestOpenCodeStatusConcurrentReadsAreRaceFree(t *testing.T) {
	t.Parallel()

	e, err := NewOpenCodeEngine(engine.EngineConfig{Model: "gpt-4o"})
	require.NoError(t, err)

	var wg sync.WaitGroup
	const workers = 8
	const iterations = 100
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = e.Status()
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, engine.StatusIdle, e.Status(), "status must remain stable under concurrent reads")
}
