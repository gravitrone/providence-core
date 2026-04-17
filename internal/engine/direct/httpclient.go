package direct

import (
	"net/http"
	"time"
)

// providerHTTPClient is the shared client used for direct-engine
// provider requests (OpenRouter, Codex OAuth, Codex compaction).
// The 120s cap matches webfetch's buildHTTPClient pattern and prevents
// a misbehaving upstream from hanging the caller's ctx-bound session.
var providerHTTPClient = &http.Client{Timeout: 120 * time.Second}
