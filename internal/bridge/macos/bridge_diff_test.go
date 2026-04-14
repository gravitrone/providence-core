//go:build darwin

package macos

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubSwiftDiff is a minimal swiftBridgeClient for bridge diff tests.
type stubSwiftDiff struct {
	screenDiffResult ScreenDiffResult
	screenDiffErr    error
	actionBatchResult ActionBatchResult
	actionBatchErr   error
}

func (s *stubSwiftDiff) call(_ context.Context, _ string, _ any) (json.RawMessage, error) {
	return nil, nil
}

func (s *stubSwiftDiff) Click(_ context.Context, _ clickParams) error { return nil }
func (s *stubSwiftDiff) TypeText(_ context.Context, _ string) error   { return nil }
func (s *stubSwiftDiff) KeyCombo(_ context.Context, _ KeyCombo) error { return nil }

func (s *stubSwiftDiff) AXTree(_ context.Context, _ AXTreeParams) (AXTreeResult, error) {
	return AXTreeResult{}, nil
}

func (s *stubSwiftDiff) AXFind(_ context.Context, _ AXQuery) (AXFindResult, error) {
	return AXFindResult{}, nil
}

func (s *stubSwiftDiff) AXPerform(_ context.Context, _, _ string) error { return nil }

func (s *stubSwiftDiff) ScreenDiff(_ context.Context, _ ScreenDiffParams) (ScreenDiffResult, error) {
	return s.screenDiffResult, s.screenDiffErr
}

func (s *stubSwiftDiff) ActionBatch(_ context.Context, _ ActionBatchParams) (ActionBatchResult, error) {
	return s.actionBatchResult, s.actionBatchErr
}

func (s *stubSwiftDiff) Close(_ context.Context) error { return nil }

// --- ScreenDiff ---

func TestBridgeScreenDiffRequiresSwift(t *testing.T) {
	b := New(WithMode("shell"))

	_, err := b.ScreenDiff(t.Context(), ScreenDiffParams{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "native bridge")
}

func TestBridgeScreenDiffSuccessPath(t *testing.T) {
	expected := ScreenDiffResult{
		Changed:  true,
		Hamming:  12,
		FullHash: "deadbeef",
		Regions:  []ScreenDiffRegion{{X: 0, Y: 0, W: 200, H: 100, Magnitude: 0.08}},
	}
	stub := &stubSwiftDiff{screenDiffResult: expected}

	b := New()
	b.swift = stub

	result, err := b.ScreenDiff(t.Context(), ScreenDiffParams{MaxRegions: 4})
	require.NoError(t, err)
	assert.True(t, result.Changed)
	assert.Equal(t, 12, result.Hamming)
	assert.Equal(t, "deadbeef", result.FullHash)
	require.Len(t, result.Regions, 1)
}


// --- ActionBatch ---

func TestBridgeActionBatchRequiresSwift(t *testing.T) {
	b := New(WithMode("shell"))

	_, err := b.ActionBatch(t.Context(), ActionBatchParams{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "native bridge")
}

func TestBridgeActionBatchSuccessPath(t *testing.T) {
	failIdx := 0
	expected := ActionBatchResult{
		Completed: 2,
		FailedAt:  nil,
		Actions: []BatchActionResult{
			{Index: 0, Type: "click", OK: true, DurationMS: 3},
			{Index: 1, Type: "type", OK: true, DurationMS: 7},
		},
	}
	_ = failIdx
	stub := &stubSwiftDiff{actionBatchResult: expected}

	b := New()
	b.swift = stub

	result, err := b.ActionBatch(t.Context(), ActionBatchParams{
		Actions: []BatchAction{
			{Type: "click", Params: map[string]any{"x": 50, "y": 50}},
			{Type: "type", Params: map[string]any{"text": "hello"}},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, result.Completed)
	assert.Nil(t, result.FailedAt)
	require.Len(t, result.Actions, 2)
}
