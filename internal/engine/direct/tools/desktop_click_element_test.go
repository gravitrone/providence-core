package tools

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"testing"

	"github.com/gravitrone/providence-core/internal/bridge/macos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDesktopClickElementName(t *testing.T) {
	tool := NewDesktopClickElementTool(&mockAXBridge{})
	assert.Equal(t, "DesktopClickElement", tool.Name())
}

func TestDesktopClickElementNotReadOnly(t *testing.T) {
	tool := NewDesktopClickElementTool(&mockAXBridge{})
	assert.False(t, tool.ReadOnly())
}

func TestDesktopClickElementMatchCallsAXPerform(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}

	var performElementID, performAction string
	var clickCalled bool

	mock := &mockAXBridge{
		axFindFn: func(_ context.Context, _ macos.AXQuery) (macos.AXFindResult, error) {
			return macos.AXFindResult{Matches: []macos.AXNode{{ID: "btn-1", Role: "AXButton"}}}, nil
		},
		axPerformFn: func(_ context.Context, elementID, action string) error {
			performElementID = elementID
			performAction = action
			return nil
		},
		clickFn: func(_ context.Context, _, _ int) error {
			clickCalled = true
			return nil
		},
	}

	tool := NewDesktopClickElementTool(mock)
	result := tool.Execute(context.Background(), map[string]any{
		"query": map[string]any{"role": "AXButton"},
	})

	require.False(t, result.IsError, result.Content)
	assert.Equal(t, "btn-1", performElementID)
	assert.Equal(t, "AXPress", performAction)
	assert.False(t, clickCalled)

	var res clickElementResult
	require.NoError(t, json.Unmarshal([]byte(result.Content), &res))
	assert.Equal(t, "ax_perform", res.Method)
}

func TestDesktopClickElementNoMatchFallbackCoords(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}

	var clickX, clickY int
	mock := &mockAXBridge{
		axFindFn: func(_ context.Context, _ macos.AXQuery) (macos.AXFindResult, error) {
			return macos.AXFindResult{Matches: nil}, nil
		},
		clickFn: func(_ context.Context, x, y int) error {
			clickX = x
			clickY = y
			return nil
		},
	}

	tool := NewDesktopClickElementTool(mock)
	result := tool.Execute(context.Background(), map[string]any{
		"query":           map[string]any{"title": "Nonexistent"},
		"fallback_coords": map[string]any{"x": 100, "y": 200},
	})

	require.False(t, result.IsError, result.Content)
	assert.Equal(t, 100, clickX)
	assert.Equal(t, 200, clickY)

	var res clickElementResult
	require.NoError(t, json.Unmarshal([]byte(result.Content), &res))
	assert.Equal(t, "fallback_coords", res.Method)
}

func TestDesktopClickElementNoMatchNoFallbackReturnsError(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}

	mock := &mockAXBridge{
		axFindFn: func(_ context.Context, _ macos.AXQuery) (macos.AXFindResult, error) {
			return macos.AXFindResult{Matches: nil}, nil
		},
	}

	tool := NewDesktopClickElementTool(mock)
	result := tool.Execute(context.Background(), map[string]any{
		"query": map[string]any{"title": "Nonexistent"},
	})

	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "no matching element")
}

func TestDesktopClickElementRightClickCallsAXShowMenu(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}

	var performAction string
	mock := &mockAXBridge{
		axFindFn: func(_ context.Context, _ macos.AXQuery) (macos.AXFindResult, error) {
			return macos.AXFindResult{Matches: []macos.AXNode{{ID: "item-1"}}}, nil
		},
		axPerformFn: func(_ context.Context, _, action string) error {
			performAction = action
			return nil
		},
	}

	tool := NewDesktopClickElementTool(mock)
	result := tool.Execute(context.Background(), map[string]any{
		"query":  map[string]any{"role": "AXMenuItem"},
		"action": "right_click",
	})

	require.False(t, result.IsError, result.Content)
	assert.Equal(t, "AXShowMenu", performAction)
}

func TestDesktopClickElementAXFindError(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}

	mock := &mockAXBridge{
		axFindFn: func(_ context.Context, _ macos.AXQuery) (macos.AXFindResult, error) {
			return macos.AXFindResult{}, errors.New("bridge unavailable")
		},
	}

	tool := NewDesktopClickElementTool(mock)
	result := tool.Execute(context.Background(), map[string]any{
		"query": map[string]any{"role": "AXButton"},
	})

	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "ax_find failed")
}
