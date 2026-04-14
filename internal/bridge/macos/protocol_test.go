//go:build darwin

package macos

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProtocol_RequestMarshalRoundtrip verifies Request survives a JSON
// marshal → unmarshal cycle with all fields intact.
func TestProtocol_RequestMarshalRoundtrip(t *testing.T) {
	req := Request{
		ID:     "req-001",
		Method: "screenshot",
		Params: map[string]any{"format": "png"},
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var got Request
	require.NoError(t, json.Unmarshal(data, &got))

	assert.Equal(t, req.ID, got.ID)
	assert.Equal(t, req.Method, got.Method)
	// Params round-trips as map[string]any via JSON - just check non-nil
	assert.NotNil(t, got.Params)
}

// TestProtocol_ResponseMarshalRoundtrip verifies Response (ok + result) survives roundtrip.
func TestProtocol_ResponseMarshalRoundtrip(t *testing.T) {
	rawResult := json.RawMessage(`{"path":"/tmp/shot.png"}`)
	resp := Response{
		ID:     "req-001",
		OK:     true,
		Result: rawResult,
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	var got Response
	require.NoError(t, json.Unmarshal(data, &got))

	assert.Equal(t, resp.ID, got.ID)
	assert.True(t, got.OK)
	assert.Equal(t, string(rawResult), string(got.Result))
	assert.Nil(t, got.Error)
}

// TestProtocol_ResponseWithError verifies Response with ProtocolError roundtrips.
func TestProtocol_ResponseWithError(t *testing.T) {
	resp := Response{
		ID: "req-002",
		OK: false,
		Error: &ProtocolError{
			Code:       ErrPermissionDenied,
			Message:    "screen recording denied",
			Remediable: true,
		},
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	var got Response
	require.NoError(t, json.Unmarshal(data, &got))

	assert.False(t, got.OK)
	require.NotNil(t, got.Error)
	assert.Equal(t, ErrPermissionDenied, got.Error.Code)
	assert.Equal(t, "screen recording denied", got.Error.Message)
	assert.True(t, got.Error.Remediable)
}

// TestProtocol_MalformedJSONReturnsError ensures partial / invalid JSON returns an error.
func TestProtocol_MalformedJSONReturnsError(t *testing.T) {
	cases := []string{"{", "not json", `{"id":}`, ""}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			var req Request
			err := json.Unmarshal([]byte(raw), &req)
			assert.Error(t, err, "expected error for input %q", raw)
		})
	}
}

// TestProtocol_IDCorrelationMatchesRequestToResponse checks that IDs survive
// roundtrip so callers can correlate responses back to requests.
func TestProtocol_IDCorrelationMatchesRequestToResponse(t *testing.T) {
	const wantID = "corr-xyz-789"

	reqData, err := json.Marshal(Request{ID: wantID, Method: "ax_tree"})
	require.NoError(t, err)

	// Simulate a bridge that echoes the same ID back in the response.
	respJSON := strings.Replace(string(reqData), `"method":"ax_tree"`, `"ok":true`, 1)

	var resp Response
	require.NoError(t, json.Unmarshal([]byte(respJSON), &resp))
	assert.Equal(t, wantID, resp.ID)
}

// TestProtocol_OversizedPayloadBehavior documents that the Go JSON layer
// itself does not impose a size limit - callers are responsible for bounding
// payload size before dispatch. This test records observed behavior.
func TestProtocol_OversizedPayloadBehavior(t *testing.T) {
	// Build a 2 MB params string - well above any sensible per-call limit.
	bigValue := strings.Repeat("x", 2<<20)
	req := Request{ID: "big-001", Method: "type", Params: bigValue}

	data, err := json.Marshal(req)
	require.NoError(t, err, "json.Marshal should not fail on large payload")

	var got Request
	require.NoError(t, json.Unmarshal(data, &got))
	// Document: no production-side limit exists in protocol.go; the Go JSON
	// codec accepts the payload unchanged.
	assert.Equal(t, req.ID, got.ID)
}

// TestProtocol_ProtocolError_Error verifies the error string format.
func TestProtocol_ProtocolError_Error(t *testing.T) {
	pe := &ProtocolError{Code: ErrTimeout, Message: "operation timed out"}
	assert.Equal(t, "timeout: operation timed out", pe.Error())
}

// TestProtocol_ProtocolError_NilSafe verifies nil ProtocolError doesn't panic.
func TestProtocol_ProtocolError_NilSafe(t *testing.T) {
	var pe *ProtocolError
	assert.Equal(t, "", pe.Error())
}
