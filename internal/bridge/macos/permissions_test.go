//go:build darwin

package macos

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubSwiftClient implements swiftBridgeClient for testing Preflight.
type stubSwiftClient struct {
	responses map[string]json.RawMessage
	errs      map[string]error
}

func (s *stubSwiftClient) call(_ context.Context, method string, _ any) (json.RawMessage, error) {
	if err, ok := s.errs[method]; ok {
		return nil, err
	}
	if raw, ok := s.responses[method]; ok {
		return raw, nil
	}
	return nil, &ProtocolError{Code: ErrBadRequest, Message: "method not found: " + method}
}

func (s *stubSwiftClient) Click(_ context.Context, _ clickParams) error         { return nil }
func (s *stubSwiftClient) TypeText(_ context.Context, _ string) error            { return nil }
func (s *stubSwiftClient) KeyCombo(_ context.Context, _ KeyCombo) error          { return nil }
func (s *stubSwiftClient) AXTree(_ context.Context, _ AXTreeParams) (AXTreeResult, error) {
	return AXTreeResult{}, nil
}
func (s *stubSwiftClient) AXFind(_ context.Context, _ AXQuery) (AXFindResult, error) {
	return AXFindResult{}, nil
}
func (s *stubSwiftClient) AXPerform(_ context.Context, _, _ string) error { return nil }
func (s *stubSwiftClient) ScreenDiff(_ context.Context, _ ScreenDiffParams) (ScreenDiffResult, error) {
	return ScreenDiffResult{}, nil
}
func (s *stubSwiftClient) ActionBatch(_ context.Context, _ ActionBatchParams) (ActionBatchResult, error) {
	return ActionBatchResult{}, nil
}
func (s *stubSwiftClient) Close(_ context.Context) error { return nil }

func TestPreflight_ParsesStatuses(t *testing.T) {
	payload := map[string]any{
		"permissions": []map[string]any{
			{
				"permission":   "screen_recording",
				"granted":      true,
				"settings_url": "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture",
				"hint":         "Grant Screen Recording in System Preferences",
			},
			{
				"permission":   "accessibility",
				"granted":      false,
				"settings_url": "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility",
				"hint":         "Grant Accessibility in System Preferences",
			},
		},
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)

	stub := &stubSwiftClient{
		responses: map[string]json.RawMessage{
			"preflight": raw,
		},
	}

	bridge := New()
	bridge.mu.Lock()
	bridge.swift = stub
	bridge.spawnErr = nil
	bridge.mu.Unlock()

	statuses, err := bridge.Preflight(context.Background())
	require.NoError(t, err)
	require.Len(t, statuses, 2)

	sr := statuses[0]
	assert.Equal(t, PermScreenRecording, sr.Permission)
	assert.True(t, sr.Granted)
	assert.NotEmpty(t, sr.SettingsURL)
	assert.NotEmpty(t, sr.Hint)

	ax := statuses[1]
	assert.Equal(t, PermAccessibility, ax.Permission)
	assert.False(t, ax.Granted)
	assert.NotEmpty(t, ax.SettingsURL)
}

func TestPreflight_NoBridge(t *testing.T) {
	bridge := New(WithMode("shell"))
	_, err := bridge.Preflight(context.Background())
	assert.Error(t, err, "should return error when bridge unavailable")
}

func TestPermission_String(t *testing.T) {
	tests := []struct {
		perm Permission
		want string
	}{
		{PermScreenRecording, "screen_recording"},
		{PermAccessibility, "accessibility"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.perm.String())
		})
	}
}

func TestPermissionDeniedError_Error(t *testing.T) {
	e := &PermissionDeniedError{
		Permission:  PermAccessibility,
		SettingsURL: "x-apple.systempreferences:Privacy_Accessibility",
		Hint:        "Grant Accessibility",
	}
	msg := e.Error()
	assert.Contains(t, msg, "accessibility")
	assert.Contains(t, msg, "Grant Accessibility")
	assert.Contains(t, msg, "x-apple.systempreferences")
}
