package claude

import (
	"testing"

	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/stretchr/testify/assert"
)

func TestSessionStatusConstants(t *testing.T) {
	// Verify the iota values are in the expected order
	assert.Equal(t, engine.SessionStatus(0), engine.StatusIdle)
	assert.Equal(t, engine.SessionStatus(1), engine.StatusConnecting)
	assert.Equal(t, engine.SessionStatus(2), engine.StatusRunning)
	assert.Equal(t, engine.SessionStatus(3), engine.StatusCompleted)
	assert.Equal(t, engine.SessionStatus(4), engine.StatusFailed)
}

func TestSessionStatusInitial(t *testing.T) {
	// A zero-value SessionStatus should be Idle
	var s engine.SessionStatus
	assert.Equal(t, engine.StatusIdle, s)
}

func TestSessionStatusDistinct(t *testing.T) {
	statuses := []engine.SessionStatus{
		engine.StatusIdle,
		engine.StatusConnecting,
		engine.StatusRunning,
		engine.StatusCompleted,
		engine.StatusFailed,
	}
	seen := make(map[engine.SessionStatus]bool)
	for _, s := range statuses {
		assert.False(t, seen[s], "duplicate status value: %d", s)
		seen[s] = true
	}
	assert.Len(t, seen, 5)
}

func TestNewSessionFailsWithBadCommand(t *testing.T) {
	// "claude" binary is unlikely to exist in CI / test env without a real install.
	// If it exists this may pass or fail depending on environment, but error handling
	// should always return an error or a valid session - never panic.
	sess, err := NewSession("test prompt", nil, "")
	if err != nil {
		// Expected path: binary not found or startup error
		assert.Error(t, err)
		assert.Nil(t, sess)
	} else {
		// If somehow claude is installed, we get a live session - close it cleanly
		assert.NotNil(t, sess)
		sess.Close()
	}
}

func TestParsedEventStructure(t *testing.T) {
	pe := engine.ParsedEvent{
		Type: "system",
		Data: nil,
		Raw:  `{"type":"system"}`,
		Err:  nil,
	}
	assert.Equal(t, "system", pe.Type)
	assert.Nil(t, pe.Err)
	assert.NotEmpty(t, pe.Raw)
}

func TestParsedEventWithError(t *testing.T) {
	pe := engine.ParsedEvent{
		Type: "",
		Err:  assert.AnError,
	}
	assert.Error(t, pe.Err)
}
