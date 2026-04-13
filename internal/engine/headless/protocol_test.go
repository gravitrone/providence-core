package headless

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSystemInitEventRoundtrip(t *testing.T) {
	orig := SystemInitEvent{
		Type:      TypeSystem,
		Subtype:   SubtypeInit,
		SessionID: "sess-abc-123",
		Tools:     []string{"Read", "Write", "Bash"},
		Model:     "claude-sonnet-4-20250514",
		Engine:    "claude",
	}

	data, err := json.Marshal(orig)
	require.NoError(t, err)

	var decoded SystemInitEvent
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, orig.Type, decoded.Type)
	assert.Equal(t, orig.Subtype, decoded.Subtype)
	assert.Equal(t, orig.SessionID, decoded.SessionID)
	assert.Equal(t, orig.Tools, decoded.Tools)
	assert.Equal(t, orig.Model, decoded.Model)
	assert.Equal(t, orig.Engine, decoded.Engine)
}

func TestResultEventRoundtrip(t *testing.T) {
	orig := ResultEvent{
		Type:     TypeResult,
		Subtype:  SubtypeSuccess,
		Result:   "task completed successfully",
		NumTurns: 5,
		Duration: 12345,
	}

	data, err := json.Marshal(orig)
	require.NoError(t, err)

	var decoded ResultEvent
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, orig.Type, decoded.Type)
	assert.Equal(t, orig.Subtype, decoded.Subtype)
	assert.Equal(t, orig.Result, decoded.Result)
	assert.Equal(t, orig.NumTurns, decoded.NumTurns)
	assert.Equal(t, orig.Duration, decoded.Duration)
}

func TestControlRequestRoundtrip(t *testing.T) {
	orig := ControlRequest{
		Type:      TypeControlRequest,
		RequestID: "req-001",
		Request:   map[string]any{"action": "switch_engine", "target": "opencode"},
	}

	data, err := json.Marshal(orig)
	require.NoError(t, err)

	var decoded ControlRequest
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, orig.Type, decoded.Type)
	assert.Equal(t, orig.RequestID, decoded.RequestID)
	assert.NotNil(t, decoded.Request)

	reqMap, ok := decoded.Request.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "switch_engine", reqMap["action"])
	assert.Equal(t, "opencode", reqMap["target"])
}

func TestProvidenceExtensions(t *testing.T) {
	// Verify all providence extension type constants are defined and distinct.
	extensions := []string{
		TypeHarnessSwitch,
		TypeForkSpawn,
		TypeForkMerge,
		TypeDashboardUpdate,
		TypeCompactEvent,
		TypeKeepAlive,
	}

	seen := make(map[string]bool)
	for _, ext := range extensions {
		assert.NotEmpty(t, ext, "extension type should not be empty")
		assert.False(t, seen[ext], "duplicate extension type: %s", ext)
		seen[ext] = true
	}
	assert.Len(t, seen, 6)
}

func TestSubtypeConstants(t *testing.T) {
	subtypes := []string{
		SubtypeInit,
		SubtypeSuccess,
		SubtypeError,
		SubtypeMaxTurns,
		SubtypeSessionStateChanged,
		SubtypeAPIRetry,
		SubtypeCompactBoundary,
		SubtypeStatus,
	}

	seen := make(map[string]bool)
	for _, s := range subtypes {
		assert.NotEmpty(t, s)
		assert.False(t, seen[s], "duplicate subtype: %s", s)
		seen[s] = true
	}
	assert.Len(t, seen, 8)
}

func TestHarnessSwitchEventJSON(t *testing.T) {
	ev := HarnessSwitchEvent{
		Type:       TypeHarnessSwitch,
		FromEngine: "claude",
		ToEngine:   "opencode",
		Reason:     "rate limit",
	}

	data, err := json.Marshal(ev)
	require.NoError(t, err)

	var decoded HarnessSwitchEvent
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, ev, decoded)
}
