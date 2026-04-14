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

type mockAXBridge struct {
	axFindFn   func(ctx context.Context, q macos.AXQuery) (macos.AXFindResult, error)
	axTreeFn   func(ctx context.Context, p macos.AXTreeParams) (macos.AXTreeResult, error)
	axPerformFn func(ctx context.Context, elementID, action string) error
	clickFn    func(ctx context.Context, x, y int) error
	dblClickFn func(ctx context.Context, x, y int) error
}

func (m *mockAXBridge) AXFind(ctx context.Context, q macos.AXQuery) (macos.AXFindResult, error) {
	if m.axFindFn != nil {
		return m.axFindFn(ctx, q)
	}
	return macos.AXFindResult{}, nil
}

func (m *mockAXBridge) AXTree(ctx context.Context, p macos.AXTreeParams) (macos.AXTreeResult, error) {
	if m.axTreeFn != nil {
		return m.axTreeFn(ctx, p)
	}
	return macos.AXTreeResult{}, nil
}

func (m *mockAXBridge) AXPerform(ctx context.Context, elementID, action string) error {
	if m.axPerformFn != nil {
		return m.axPerformFn(ctx, elementID, action)
	}
	return nil
}

func (m *mockAXBridge) Click(ctx context.Context, x, y int) error {
	if m.clickFn != nil {
		return m.clickFn(ctx, x, y)
	}
	return nil
}

func (m *mockAXBridge) DoubleClick(ctx context.Context, x, y int) error {
	if m.dblClickFn != nil {
		return m.dblClickFn(ctx, x, y)
	}
	return nil
}

// --- DesktopFindElement tests ---

func TestDesktopFindElementName(t *testing.T) {
	tool := NewDesktopFindElementTool(&mockAXBridge{})
	assert.Equal(t, "DesktopFindElement", tool.Name())
}

func TestDesktopFindElementDescription(t *testing.T) {
	tool := NewDesktopFindElementTool(&mockAXBridge{})
	assert.NotEmpty(t, tool.Description())
}

func TestDesktopFindElementReadOnly(t *testing.T) {
	tool := NewDesktopFindElementTool(&mockAXBridge{})
	assert.True(t, tool.ReadOnly())
}

func TestDesktopFindElementInputSchemaShape(t *testing.T) {
	tool := NewDesktopFindElementTool(&mockAXBridge{})
	schema := tool.InputSchema()
	require.Equal(t, "object", schema["type"])
	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, props, "role")
	assert.Contains(t, props, "title")
	assert.Contains(t, props, "text")
	assert.Contains(t, props, "contains_text")
	assert.Contains(t, props, "mode")
}

func TestDesktopFindElementExecuteReturnsMatches(t *testing.T) {
	node := macos.AXNode{ID: "elem-1", Role: "AXButton", Title: "OK"}
	mock := &mockAXBridge{
		axFindFn: func(_ context.Context, q macos.AXQuery) (macos.AXFindResult, error) {
			assert.Equal(t, "AXButton", q.Role)
			return macos.AXFindResult{Matches: []macos.AXNode{node}}, nil
		},
	}
	tool := NewDesktopFindElementTool(mock)

	result := tool.Execute(context.Background(), map[string]any{"role": "AXButton"})
	require.False(t, result.IsError, result.Content)

	var matches []macos.AXNode
	require.NoError(t, json.Unmarshal([]byte(result.Content), &matches))
	require.Len(t, matches, 1)
	assert.Equal(t, "elem-1", matches[0].ID)
}

func TestDesktopFindElementEmptyMatchesReturnsEmptyArray(t *testing.T) {
	mock := &mockAXBridge{
		axFindFn: func(_ context.Context, _ macos.AXQuery) (macos.AXFindResult, error) {
			return macos.AXFindResult{Matches: nil}, nil
		},
	}
	tool := NewDesktopFindElementTool(mock)

	result := tool.Execute(context.Background(), map[string]any{})
	require.False(t, result.IsError, result.Content)
	assert.Equal(t, "[]", result.Content)
}

func TestDesktopFindElementBridgeErrorPropagates(t *testing.T) {
	mock := &mockAXBridge{
		axFindFn: func(_ context.Context, _ macos.AXQuery) (macos.AXFindResult, error) {
			return macos.AXFindResult{}, errors.New("ax_find: permission denied")
		},
	}
	tool := NewDesktopFindElementTool(mock)

	result := tool.Execute(context.Background(), map[string]any{})
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "ax_find failed")
}
