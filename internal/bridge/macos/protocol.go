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
	CapAXFind      Capability = "ax_find"
	CapAXPerform   Capability = "ax_perform"
	CapScreenDiff  Capability = "screen_diff"
	CapActionBatch Capability = "action_batch"
	CapClipboard   Capability = "clipboard"
	CapAppList     Capability = "app_list"
	CapAppFocus    Capability = "app_focus"
	CapAppLaunch   Capability = "app_launch"
)

// --- AX Types ---

// AXFrame is the bounding rectangle of an AX element in screen coordinates.
type AXFrame struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// AXNode is a single node in the macOS Accessibility tree.
type AXNode struct {
	ID          string   `json:"id"`
	Role        string   `json:"role"`
	Subrole     string   `json:"subrole,omitempty"`
	Title       string   `json:"title,omitempty"`
	Label       string   `json:"label,omitempty"`
	Value       string   `json:"value,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Frame       AXFrame  `json:"frame"`
	Enabled     bool     `json:"enabled"`
	Focused     bool     `json:"focused,omitempty"`
	Selected    bool     `json:"selected,omitempty"`
	Actions     []string `json:"actions,omitempty"`
	Children    []AXNode `json:"children,omitempty"`
	Score       float64  `json:"score,omitempty"`
}

// AXTreeParams are the parameters for an ax_tree request.
type AXTreeParams struct {
	App              string `json:"app,omitempty"`
	PID              int    `json:"pid,omitempty"`
	MaxDepth         int    `json:"max_depth,omitempty"`
	MaxNodes         int    `json:"max_nodes,omitempty"`
	IncludeInvisible bool   `json:"include_invisible,omitempty"`
	Format           string `json:"format,omitempty"`
}

// AXTreeResult is the result of an ax_tree request.
type AXTreeResult struct {
	Root      *AXNode `json:"root,omitempty"`
	Flat      string  `json:"flat,omitempty"`
	Truncated bool    `json:"truncated"`
	App       string  `json:"app,omitempty"`
	PID       int     `json:"pid,omitempty"`
}

// AXQuery is the filter used by ax_find.
type AXQuery struct {
	App          string `json:"app,omitempty"`
	Role         string `json:"role,omitempty"`
	Title        string `json:"title,omitempty"`
	Text         string `json:"text,omitempty"`
	ContainsText string `json:"contains_text,omitempty"`
	DescendantOf string `json:"descendant_of,omitempty"`
	MaxResults   int    `json:"max_results,omitempty"`
	Mode         string `json:"mode,omitempty"`
}

// AXFindResult is the result of an ax_find request.
type AXFindResult struct {
	Matches []AXNode `json:"matches"`
}

// AXPerformParams are the parameters for an ax_perform request.
type AXPerformParams struct {
	ElementID string `json:"element_id"`
	Action    string `json:"action"`
}

// --- Screen Diff Types ---

// ScreenDiffParams are the parameters for a screen_diff request.
type ScreenDiffParams struct {
	SinceTSNS    int64   `json:"since_ts_ns,omitempty"`   // 0 = diff against last captured frame
	MaxRegions   int     `json:"max_regions,omitempty"`   // default 8
	MinMagnitude float64 `json:"min_magnitude,omitempty"` // default 0.02 (2% of region)
}

// ScreenDiffRegion describes a changed region of the screen.
type ScreenDiffRegion struct {
	X         int     `json:"x"`
	Y         int     `json:"y"`
	W         int     `json:"w"`
	H         int     `json:"h"`
	Magnitude float64 `json:"magnitude"`
}

// ScreenDiffResult is the result of a screen_diff request.
type ScreenDiffResult struct {
	Changed   bool               `json:"changed"`
	Hamming   int                `json:"hamming"`
	Regions   []ScreenDiffRegion `json:"regions,omitempty"`
	FullHash  string             `json:"full_hash"`
	CaptureNS int64              `json:"capture_ns"`
}

// --- Action Batch Types ---

// BatchAction is a single action in an action_batch request.
type BatchAction struct {
	Type   string         `json:"type"`
	Params map[string]any `json:"params,omitempty"` // action-specific payload
}

// BatchActionResult is the result of a single action in an action_batch.
type BatchActionResult struct {
	Index      int            `json:"index"`
	Type       string         `json:"type"`
	OK         bool           `json:"ok"`
	Result     map[string]any `json:"result,omitempty"`
	Error      string         `json:"error,omitempty"`
	DurationMS int            `json:"duration_ms"`
}

// ActionBatchParams are the parameters for an action_batch request.
type ActionBatchParams struct {
	Actions         []BatchAction `json:"actions"`
	StopOnError     bool          `json:"stop_on_error,omitempty"`
	ScreenshotAfter bool          `json:"screenshot_after,omitempty"`
}

// ActionBatchResult is the result of an action_batch request.
type ActionBatchResult struct {
	Completed       int                 `json:"completed"`
	FailedAt        *int                `json:"failed_at,omitempty"`
	Actions         []BatchActionResult `json:"actions"`
	FinalScreenshot string              `json:"final_screenshot,omitempty"` // path
}
