//go:build darwin

package macos

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubSwiftAX is a minimal swiftBridgeClient used in bridge AX tests.
type stubSwiftAX struct {
	axTreeResult AXTreeResult
	axTreeErr    error
	axFindResult AXFindResult
	axFindErr    error
	axPerformErr error
}

func (s *stubSwiftAX) call(_ context.Context, _ string, _ any) (json.RawMessage, error) {
	return nil, nil
}

func (s *stubSwiftAX) Click(_ context.Context, _ clickParams) error { return nil }
func (s *stubSwiftAX) TypeText(_ context.Context, _ string) error   { return nil }
func (s *stubSwiftAX) KeyCombo(_ context.Context, _ KeyCombo) error { return nil }

func (s *stubSwiftAX) AXTree(_ context.Context, _ AXTreeParams) (AXTreeResult, error) {
	return s.axTreeResult, s.axTreeErr
}

func (s *stubSwiftAX) AXFind(_ context.Context, _ AXQuery) (AXFindResult, error) {
	return s.axFindResult, s.axFindErr
}

func (s *stubSwiftAX) AXPerform(_ context.Context, _, _ string) error {
	return s.axPerformErr
}

func (s *stubSwiftAX) Close(_ context.Context) error { return nil }

// --- AXTree ---

func TestBridgeAXTreeRequiresSwift(t *testing.T) {
	b := New(WithMode("shell"))

	_, err := b.AXTree(t.Context(), AXTreeParams{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "native bridge")
}

func TestBridgeAXTreeSuccessPath(t *testing.T) {
	expected := AXTreeResult{Flat: "AXWindow > AXButton"}
	stub := &stubSwiftAX{axTreeResult: expected}

	b := New()
	b.swift = stub

	result, err := b.AXTree(t.Context(), AXTreeParams{})
	require.NoError(t, err)
	assert.Equal(t, "AXWindow > AXButton", result.Flat)
}

// --- AXFind ---

func TestBridgeAXFindRequiresSwift(t *testing.T) {
	b := New(WithMode("shell"))

	_, err := b.AXFind(t.Context(), AXQuery{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "native bridge")
}

func TestBridgeAXFindSuccessPath(t *testing.T) {
	expected := AXFindResult{Matches: []AXNode{{ID: "btn-1", Role: "AXButton"}}}
	stub := &stubSwiftAX{axFindResult: expected}

	b := New()
	b.swift = stub

	result, err := b.AXFind(t.Context(), AXQuery{Role: "AXButton"})
	require.NoError(t, err)
	require.Len(t, result.Matches, 1)
	assert.Equal(t, "btn-1", result.Matches[0].ID)
}

// --- AXPerform ---

func TestBridgeAXPerformRequiresSwift(t *testing.T) {
	b := New(WithMode("shell"))

	err := b.AXPerform(t.Context(), "elem-1", "AXPress")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "native bridge")
}

func TestBridgeAXPerformSuccessPath(t *testing.T) {
	stub := &stubSwiftAX{}

	b := New()
	b.swift = stub

	err := b.AXPerform(t.Context(), "elem-1", "AXPress")
	require.NoError(t, err)
}
