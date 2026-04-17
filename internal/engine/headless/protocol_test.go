package headless

import (
	"encoding/json"
	"strings"
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

func TestToolSummary_Bash(t *testing.T) {
	command := strings.Repeat("x", 70)

	summary := summarizeToolCall("Bash", map[string]any{
		"command": command,
	}, "/repo")

	assert.Equal(t, "Running: "+strings.Repeat("x", 57)+"...", summary)
	assert.LessOrEqual(t, len(summary), 80)
}

func TestToolSummary_Read(t *testing.T) {
	summary := summarizeToolCall("Read", map[string]any{
		"file_path": "/repo/internal/engine/headless/server.go",
	}, "/repo")

	assert.Equal(t, "Reading internal/engine/headless/server.go", summary)
	assert.LessOrEqual(t, len(summary), 80)
}

func TestToolSummary_Write(t *testing.T) {
	summary := summarizeToolCall("Write", map[string]any{
		"file_path": "/repo/internal/engine/headless/server.go",
	}, "/repo")

	assert.Equal(t, "Writing internal/engine/headless/server.go", summary)
	assert.LessOrEqual(t, len(summary), 80)
}

func TestToolSummary_Edit(t *testing.T) {
	summary := summarizeToolCall("Edit", map[string]any{
		"file_path": "/repo/internal/engine/headless/server.go",
	}, "/repo")

	assert.Equal(t, "Editing internal/engine/headless/server.go", summary)
	assert.LessOrEqual(t, len(summary), 80)
}

func TestToolSummary_Grep(t *testing.T) {
	summary := summarizeToolCall("Grep", map[string]any{
		"pattern": "TODO",
		"path":    "/repo/internal/engine",
	}, "/repo")

	assert.Equal(t, "Searching for 'TODO' in internal/engine", summary)
	assert.LessOrEqual(t, len(summary), 80)
}

func TestToolSummary_Glob(t *testing.T) {
	summary := summarizeToolCall("Glob", map[string]any{
		"pattern": "**/*_test.go",
	}, "/repo")

	assert.Equal(t, "Finding files matching **/*_test.go", summary)
	assert.LessOrEqual(t, len(summary), 80)
}

func TestToolSummary_WebFetch(t *testing.T) {
	summary := summarizeToolCall("WebFetch", map[string]any{
		"url": "https://example.com/docs",
	}, "/repo")

	assert.Equal(t, "Fetching https://example.com/docs", summary)
	assert.LessOrEqual(t, len(summary), 80)
}

func TestToolSummary_WebSearch(t *testing.T) {
	query := strings.Repeat("q", 70)

	summary := summarizeToolCall("WebSearch", map[string]any{
		"query": query,
	}, "/repo")

	assert.Equal(t, "Searching web for '"+strings.Repeat("q", 57)+"...'", summary)
	assert.LessOrEqual(t, len(summary), 80)
}

func TestToolSummary_DefaultFallback(t *testing.T) {
	summary := summarizeToolCall("Sleep", map[string]any{}, "/repo")

	assert.Equal(t, "Sleep call", summary)
	assert.LessOrEqual(t, len(summary), 80)
}
