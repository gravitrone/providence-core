//go:build darwin

package macos

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type swiftInputStub struct {
	clickCalls []clickParams
	typeCalls  []string
	keyCalls   []KeyCombo

	clickErr error
	typeErr  error
	keyErr   error
}

func (s *swiftInputStub) call(context.Context, string, any) (json.RawMessage, error) {
	return nil, nil
}

func (s *swiftInputStub) Click(_ context.Context, params clickParams) error {
	s.clickCalls = append(s.clickCalls, params)
	return s.clickErr
}

func (s *swiftInputStub) TypeText(_ context.Context, text string) error {
	s.typeCalls = append(s.typeCalls, text)
	return s.typeErr
}

func (s *swiftInputStub) KeyCombo(_ context.Context, combo KeyCombo) error {
	s.keyCalls = append(s.keyCalls, combo)
	return s.keyErr
}

func (s *swiftInputStub) Close(context.Context) error {
	return nil
}

type shellInputStub struct {
	clickCalls       [][2]int
	doubleClickCalls [][2]int
	rightClickCalls  [][2]int
	typeCalls        []string
	keyCalls         []string
}

func (s *shellInputStub) Screenshot(context.Context) (string, error) {
	return "", nil
}

func (s *shellInputStub) ScreenshotRegion(context.Context, int, int, int, int) (string, error) {
	return "", nil
}

func (s *shellInputStub) Click(_ context.Context, x, y int) error {
	s.clickCalls = append(s.clickCalls, [2]int{x, y})
	return nil
}

func (s *shellInputStub) DoubleClick(_ context.Context, x, y int) error {
	s.doubleClickCalls = append(s.doubleClickCalls, [2]int{x, y})
	return nil
}

func (s *shellInputStub) RightClick(_ context.Context, x, y int) error {
	s.rightClickCalls = append(s.rightClickCalls, [2]int{x, y})
	return nil
}

func (s *shellInputStub) Type(_ context.Context, text string) error {
	s.typeCalls = append(s.typeCalls, text)
	return nil
}

func (s *shellInputStub) Key(_ context.Context, keys string) error {
	s.keyCalls = append(s.keyCalls, keys)
	return nil
}

func (s *shellInputStub) ClipboardRead(context.Context) (string, error) {
	return "", nil
}

func (s *shellInputStub) ClipboardWrite(context.Context, string) error {
	return nil
}

func (s *shellInputStub) ListApps(context.Context) ([]AppInfo, error) {
	return nil, nil
}

func (s *shellInputStub) FocusApp(context.Context, string) error {
	return nil
}

func (s *shellInputStub) LaunchApp(context.Context, string) error {
	return nil
}

func TestBridgeClickSwiftSuccess(t *testing.T) {
	bridge := New()
	swift := &swiftInputStub{}
	shell := &shellInputStub{}
	bridge.swift = swift
	bridge.shell = shell

	err := bridge.Click(t.Context(), 12, 34)
	require.NoError(t, err)
	assert.Equal(t, []clickParams{{X: 12, Y: 34}}, swift.clickCalls)
	assert.Empty(t, shell.clickCalls)
}

func TestBridgeClickSwiftPermissionDeniedFallsBack(t *testing.T) {
	bridge := New()
	swift := &swiftInputStub{
		clickErr: &ProtocolError{Code: ErrPermissionDenied, Message: "denied"},
	}
	shell := &shellInputStub{}
	bridge.swift = swift
	bridge.shell = shell

	err := bridge.Click(t.Context(), 10, 20)
	require.NoError(t, err)
	assert.Equal(t, []clickParams{{X: 10, Y: 20}}, swift.clickCalls)
	assert.Equal(t, [][2]int{{10, 20}}, shell.clickCalls)
	assert.False(t, bridge.caps[CapClick])
}

func TestBridgeClickSwiftBadRequestNoFallback(t *testing.T) {
	bridge := New()
	swift := &swiftInputStub{
		clickErr: &ProtocolError{Code: ErrBadRequest, Message: "bad request"},
	}
	shell := &shellInputStub{}
	bridge.swift = swift
	bridge.shell = shell

	err := bridge.Click(t.Context(), 10, 20)
	require.Error(t, err)
	assert.Equal(t, []clickParams{{X: 10, Y: 20}}, swift.clickCalls)
	assert.Empty(t, shell.clickCalls)
	assert.True(t, bridge.caps[CapClick])

	var protocolErr *ProtocolError
	require.ErrorAs(t, err, &protocolErr)
	assert.Equal(t, ErrBadRequest, protocolErr.Code)
}

func TestBridgeTypeUnicode(t *testing.T) {
	bridge := New()
	swift := &swiftInputStub{}
	shell := &shellInputStub{}
	bridge.swift = swift
	bridge.shell = shell

	text := "Unicode: \u4f60\u597d \U0001F525 \"quoted\" \\\\ path"

	err := bridge.Type(t.Context(), text)
	require.NoError(t, err)
	assert.Equal(t, []string{text}, swift.typeCalls)
	assert.Empty(t, shell.typeCalls)
}

func TestBridgeKeyCombo(t *testing.T) {
	bridge := New()
	swift := &swiftInputStub{}
	shell := &shellInputStub{}
	bridge.swift = swift
	bridge.shell = shell

	err := bridge.Key(t.Context(), " command + shift + Z ")
	require.NoError(t, err)
	require.Len(t, swift.keyCalls, 1)
	assert.Equal(t, KeyCombo{
		Key:         "z",
		Modifiers:   []string{"cmd", "shift"},
		VirtualCode: 6,
	}, swift.keyCalls[0])
	assert.Empty(t, shell.keyCalls)
}
