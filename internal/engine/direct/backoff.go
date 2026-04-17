package direct

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/gravitrone/providence-core/internal/engine"
)

const overloadRetryLimit = 4

type retryState struct {
	consecutive529       int
	consecutiveConnReset int
	disableKeepAlives    bool
}

type retrySnapshot struct {
	consecutive529       int
	consecutiveConnReset int
	disableKeepAlives    bool
}

func newRetryStates() map[string]*retryState {
	return map[string]*retryState{
		engine.ProviderAnthropic:  {},
		engine.ProviderOpenAI:     {},
		engine.ProviderOpenRouter: {},
	}
}

func newAnthropicClient(apiKey, baseURL string, disableKeepAlives bool) anthropic.Client {
	opts := []option.RequestOption{
		option.WithHTTPClient(newAnthropicHTTPClient(disableKeepAlives)),
		option.WithMaxRetries(0),
	}
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	return anthropic.NewClient(opts...)
}

func newAnthropicHTTPClient(disableKeepAlives bool) *http.Client {
	return &http.Client{
		Timeout:   120 * time.Second,
		Transport: newHTTPTransport(disableKeepAlives),
	}
}

func newHTTPTransport(disableKeepAlives bool) *http.Transport {
	baseTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Transport{DisableKeepAlives: disableKeepAlives}
	}

	transport := baseTransport.Clone()
	transport.DisableKeepAlives = disableKeepAlives
	return transport
}

func overloadRetryDelay(retries int) time.Duration {
	if retries < 1 {
		retries = 1
	}
	if retries > overloadRetryLimit {
		retries = overloadRetryLimit
	}
	return time.Second << (retries - 1)
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isConnectionReset(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ECONNRESET) {
		return true
	}

	var netErr *net.OpError
	if errors.As(err, &netErr) && errors.Is(netErr.Err, syscall.ECONNRESET) {
		return true
	}

	msg := err.Error()
	return strings.Contains(msg, "connection reset by peer") || strings.Contains(msg, "ECONNRESET")
}

func retrySourceLabel(source string) string {
	switch source {
	case engine.ProviderOpenAI:
		return "codex"
	default:
		return source
	}
}

func (e *DirectEngine) rebuildAnthropicClient(disableKeepAlives bool) {
	e.client = newAnthropicClient(e.anthropicAPIKey, e.anthropicBaseURL, disableKeepAlives)
}

func (e *DirectEngine) resetRetryCounts(source string) {
	e.retryMu.Lock()
	defer e.retryMu.Unlock()

	state := e.ensureRetryStateLocked(source)
	state.consecutive529 = 0
	state.consecutiveConnReset = 0
}

func (e *DirectEngine) noteOverloadRetry(source string) retrySnapshot {
	e.retryMu.Lock()
	defer e.retryMu.Unlock()

	state := e.ensureRetryStateLocked(source)
	state.consecutive529++
	state.consecutiveConnReset = 0

	return retrySnapshot{
		consecutive529:       state.consecutive529,
		consecutiveConnReset: state.consecutiveConnReset,
		disableKeepAlives:    state.disableKeepAlives,
	}
}

func (e *DirectEngine) noteConnResetRetry(source string) (retrySnapshot, bool) {
	e.retryMu.Lock()
	defer e.retryMu.Unlock()

	state := e.ensureRetryStateLocked(source)
	keepAliveChanged := !state.disableKeepAlives
	state.disableKeepAlives = true
	state.consecutiveConnReset++
	state.consecutive529 = 0

	return retrySnapshot{
		consecutive529:       state.consecutive529,
		consecutiveConnReset: state.consecutiveConnReset,
		disableKeepAlives:    state.disableKeepAlives,
	}, keepAliveChanged
}

func (e *DirectEngine) ensureRetryStateLocked(source string) *retryState {
	if e.retryStates == nil {
		e.retryStates = newRetryStates()
	}

	state, ok := e.retryStates[source]
	if !ok {
		state = &retryState{}
		e.retryStates[source] = state
	}

	return state
}
