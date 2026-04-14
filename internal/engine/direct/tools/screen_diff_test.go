package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/gravitrone/providence-core/internal/bridge/macos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock ---

type mockDiffBridge struct {
	screenDiffFn func(ctx context.Context, p macos.ScreenDiffParams) (macos.ScreenDiffResult, error)
}

func (m *mockDiffBridge) ScreenDiff(ctx context.Context, p macos.ScreenDiffParams) (macos.ScreenDiffResult, error) {
	if m.screenDiffFn != nil {
		return m.screenDiffFn(ctx, p)
	}
	return macos.ScreenDiffResult{}, nil
}

// --- ScreenDiff tests ---

func TestScreenDiffName(t *testing.T) {
	tool := NewScreenDiffTool(&mockDiffBridge{})
	assert.Equal(t, "ScreenDiff", tool.Name())
}

func TestScreenDiffReadOnly(t *testing.T) {
	tool := NewScreenDiffTool(&mockDiffBridge{})
	assert.True(t, tool.ReadOnly())
}

func TestScreenDiffInputSchemaShape(t *testing.T) {
	tool := NewScreenDiffTool(&mockDiffBridge{})
	schema := tool.InputSchema()
	require.Equal(t, "object", schema["type"])
	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, props, "since_ts_ns")
	assert.Contains(t, props, "max_regions")
	assert.Contains(t, props, "min_magnitude")
}

func TestScreenDiffExecuteCallsBridgeAndMarshals(t *testing.T) {
	expected := macos.ScreenDiffResult{
		Changed:  true,
		Hamming:  42,
		FullHash: "abcdef1234",
		Regions: []macos.ScreenDiffRegion{
			{X: 10, Y: 20, W: 100, H: 50, Magnitude: 0.15},
		},
		CaptureNS: 1234567890,
	}

	var gotParams macos.ScreenDiffParams
	mock := &mockDiffBridge{
		screenDiffFn: func(_ context.Context, p macos.ScreenDiffParams) (macos.ScreenDiffResult, error) {
			gotParams = p
			return expected, nil
		},
	}
	tool := NewScreenDiffTool(mock)

	result := tool.Execute(context.Background(), map[string]any{
		"since_ts_ns":   float64(999),
		"max_regions":   float64(4),
		"min_magnitude": float64(0.05),
	})
	require.False(t, result.IsError, result.Content)

	assert.Equal(t, int64(999), gotParams.SinceTSNS)
	assert.Equal(t, 4, gotParams.MaxRegions)
	assert.InDelta(t, 0.05, gotParams.MinMagnitude, 0.001)

	var decoded macos.ScreenDiffResult
	require.NoError(t, json.Unmarshal([]byte(result.Content), &decoded))
	assert.True(t, decoded.Changed)
	assert.Equal(t, 42, decoded.Hamming)
	assert.Equal(t, "abcdef1234", decoded.FullHash)
	require.Len(t, decoded.Regions, 1)
	assert.Equal(t, 10, decoded.Regions[0].X)
}

func TestScreenDiffExecuteNoChanged(t *testing.T) {
	mock := &mockDiffBridge{
		screenDiffFn: func(_ context.Context, _ macos.ScreenDiffParams) (macos.ScreenDiffResult, error) {
			return macos.ScreenDiffResult{Changed: false, FullHash: "aabbcc"}, nil
		},
	}
	tool := NewScreenDiffTool(mock)

	result := tool.Execute(context.Background(), map[string]any{})
	require.False(t, result.IsError, result.Content)

	var decoded macos.ScreenDiffResult
	require.NoError(t, json.Unmarshal([]byte(result.Content), &decoded))
	assert.False(t, decoded.Changed)
}

func TestScreenDiffBridgeErrorPropagates(t *testing.T) {
	mock := &mockDiffBridge{
		screenDiffFn: func(_ context.Context, _ macos.ScreenDiffParams) (macos.ScreenDiffResult, error) {
			return macos.ScreenDiffResult{}, errors.New("screen_diff: requires native bridge")
		},
	}
	tool := NewScreenDiffTool(mock)

	result := tool.Execute(context.Background(), map[string]any{})
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "screen_diff failed")
}
