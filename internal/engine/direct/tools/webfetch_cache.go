package tools

import (
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

// webFetchCacheTTL is how long a cached body stays valid. Matches the
// reference implementation.
const webFetchCacheTTL = 15 * time.Minute

// webFetchCacheMaxEntries caps the LRU at a practical size. Entries
// can be large (up to MaxResponseBody), so we rely on count rather
// than byte size for simplicity.
const webFetchCacheMaxEntries = 64

// webFetchCacheEntry is a single cached response.
type webFetchCacheEntry struct {
	body        string
	contentType string
	fetchedAt   time.Time
}

// webFetchCache is a process-wide LRU. Kept package-level so repeated
// Execute calls share state. The cache is mutex-protected because
// lru's Get mutates bookkeeping.
var (
	webFetchCacheOnce sync.Once
	webFetchCache     *lru.Cache[string, webFetchCacheEntry]
	webFetchCacheMu   sync.Mutex
)

// webFetchCacheInit constructs the LRU on first use. Errors from the
// lru constructor would indicate a bug in the hashicorp library (the
// only failure mode is a zero-or-negative size), so we panic-check by
// defaulting to a no-cache fallback.
func webFetchCacheInit() {
	webFetchCacheOnce.Do(func() {
		c, err := lru.New[string, webFetchCacheEntry](webFetchCacheMaxEntries)
		if err == nil {
			webFetchCache = c
		}
	})
}

// webFetchCacheGet returns the cached body if present and still fresh
// within the TTL. Returns ok=false on miss or expired entry.
func webFetchCacheGet(url string) (webFetchCacheEntry, bool) {
	webFetchCacheInit()
	if webFetchCache == nil {
		return webFetchCacheEntry{}, false
	}
	webFetchCacheMu.Lock()
	defer webFetchCacheMu.Unlock()
	entry, ok := webFetchCache.Get(url)
	if !ok {
		return webFetchCacheEntry{}, false
	}
	if time.Since(entry.fetchedAt) > webFetchCacheTTL {
		webFetchCache.Remove(url)
		return webFetchCacheEntry{}, false
	}
	return entry, true
}

// webFetchCachePut stores a fresh response, evicting LRU as needed.
func webFetchCachePut(url, body, contentType string) {
	webFetchCacheInit()
	if webFetchCache == nil {
		return
	}
	webFetchCacheMu.Lock()
	defer webFetchCacheMu.Unlock()
	webFetchCache.Add(url, webFetchCacheEntry{
		body:        body,
		contentType: contentType,
		fetchedAt:   time.Now(),
	})
}

// webFetchCachePurge empties the cache. Used by tests to ensure a
// fresh state.
func webFetchCachePurge() {
	webFetchCacheInit()
	if webFetchCache == nil {
		return
	}
	webFetchCacheMu.Lock()
	defer webFetchCacheMu.Unlock()
	webFetchCache.Purge()
}
