package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
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
	allowHookLookup(t, srv.URL, "1.1.1.1")

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
	allowHookLookup(t, srv.URL, "1.1.1.1")

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
	allowHookLookup(t, srv.URL, "1.1.1.1")

	out, err := execHTTPHook(context.Background(), HookConfig{
		URL:     srv.URL,
		Timeout: time.Second,
	}, HookInput{
		Event: SessionStart,
	})

	require.NoError(t, err)
	assert.Nil(t, out)
}

func TestHTTPHookRefusesSSRFTargetBeforeRequest(t *testing.T) {
	originalLookupIP := lookupIP
	lookupIP = net.LookupIP
	t.Cleanup(func() {
		lookupIP = originalLookupIP
	})

	var hits atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	parsedURL, err := url.Parse(srv.URL)
	require.NoError(t, err)
	host := parsedURL.Hostname()

	out, err := execHTTPHook(context.Background(), HookConfig{
		URL:     srv.URL,
		Timeout: time.Second,
	}, HookInput{
		Event: PreToolUse,
	})

	require.Error(t, err)
	assert.Nil(t, out)
	assert.Equal(t, int32(0), hits.Load())
	assert.Equal(t, fmt.Sprintf("ssrf: refused target %s: resolved to loopback address %s", host, host), err.Error())
}

func TestHTTPHookRefusesTargetWhenDNSResolutionFails(t *testing.T) {
	originalLookupIP := lookupIP
	lookupIP = func(host string) ([]net.IP, error) {
		return nil, fmt.Errorf("lookup %s: no such host", host)
	}
	t.Cleanup(func() {
		lookupIP = originalLookupIP
	})

	out, err := execHTTPHook(context.Background(), HookConfig{
		URL:     "http://missing.invalid/hook",
		Timeout: time.Second,
	}, HookInput{
		Event: PreToolUse,
	})

	require.Error(t, err)
	assert.Nil(t, out)
	assert.Equal(t, "ssrf: refused target missing.invalid: dns resolution failed", err.Error())
}

func allowHookLookup(t *testing.T, rawURL string, ips ...string) {
	t.Helper()

	parsedURL, err := url.Parse(rawURL)
	require.NoError(t, err)

	host := parsedURL.Hostname()
	allowedIPs := make([]net.IP, 0, len(ips))
	for _, rawIP := range ips {
		ip := net.ParseIP(rawIP)
		require.NotNil(t, ip)
		allowedIPs = append(allowedIPs, ip)
	}

	originalLookupIP := lookupIP
	lookupIP = func(lookupHost string) ([]net.IP, error) {
		if lookupHost == host {
			return allowedIPs, nil
		}
		return originalLookupIP(lookupHost)
	}
	t.Cleanup(func() {
		lookupIP = originalLookupIP
	})
}
