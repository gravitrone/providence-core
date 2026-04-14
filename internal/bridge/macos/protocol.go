//go:build darwin

package macos

import "encoding/json"

// Request is a Swift bridge request envelope.
type Request struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

// Response is a Swift bridge response envelope.
type Response struct {
	ID     string          `json:"id"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *ProtocolError  `json:"error,omitempty"`
}

// ProtocolError is a typed Swift bridge protocol error.
type ProtocolError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	URL        string `json:"url,omitempty"`
	Remediable bool   `json:"remediable,omitempty"`
}

// Error implements error.
func (e *ProtocolError) Error() string {
	if e == nil {
		return ""
	}

	return e.Code + ": " + e.Message
}

const (
	// ErrPermissionDenied indicates the bridge lacks a required macOS permission.
	ErrPermissionDenied = "permission_denied"
	// ErrUnsupportedOS indicates the current macOS version is unsupported.
	ErrUnsupportedOS = "unsupported_os"
	// ErrBadRequest indicates the request payload could not be processed.
	ErrBadRequest = "bad_request"
	// ErrElementNotFound indicates the requested UI element could not be located.
	ErrElementNotFound = "element_not_found"
	// ErrTimeout indicates the operation exceeded its deadline.
	ErrTimeout = "timeout"
	// ErrCaptureFailed indicates screen capture failed.
	ErrCaptureFailed = "capture_failed"
	// ErrFocusChanged indicates focus changed during an operation.
	ErrFocusChanged = "focus_changed"
	// ErrInternal indicates an internal bridge failure.
	ErrInternal = "internal"
)

// Event is an unsolicited Swift bridge event.
type Event struct {
	Type string          `json:"event"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Capability identifies a bridge feature that can fall back to shell mode.
type Capability string

const (
	CapScreenshot  Capability = "screenshot"
	CapClick       Capability = "click"
	CapDoubleClick Capability = "double_click"
	CapRightClick  Capability = "right_click"
	CapType        Capability = "type"
	CapKey         Capability = "key"
	CapAXTree      Capability = "ax_tree"
	CapScreenDiff  Capability = "screen_diff"
	CapActionBatch Capability = "action_batch"
	CapClipboard   Capability = "clipboard"
	CapAppList     Capability = "app_list"
	CapAppFocus    Capability = "app_focus"
	CapAppLaunch   Capability = "app_launch"
)
