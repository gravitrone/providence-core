package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPHookSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var input HookInput
		require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
		assert.Equal(t, PreToolUse, input.Event)
		assert.Equal(t, "Read", input.ToolName)

		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"decision":"approve","system_message":"continue"}`)
	}))
	defer srv.Close()

	out, err := execHTTPHook(context.Background(), HookConfig{
		URL:     srv.URL,
		Timeout: time.Second,
	}, HookInput{
		Event:    PreToolUse,
		ToolName: "Read",
	})

	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, "approve", out.Decision)
	assert.Equal(t, "continue", out.SystemMessage)
}

func TestHTTPHookServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, "internal hook failure")
	}))
	defer srv.Close()

	out, err := execHTTPHook(context.Background(), HookConfig{
		URL:     srv.URL,
		Timeout: time.Second,
	}, HookInput{
		Event: PostToolUse,
	})

	require.Error(t, err)
	assert.Nil(t, out)
	assert.Contains(t, err.Error(), "HTTP 500")
	assert.Contains(t, err.Error(), "internal hook failure")
}

func TestHTTPHookEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	out, err := execHTTPHook(context.Background(), HookConfig{
		URL:     srv.URL,
		Timeout: time.Second,
	}, HookInput{
		Event: SessionStart,
	})

	require.NoError(t, err)
	assert.Nil(t, out)
}
