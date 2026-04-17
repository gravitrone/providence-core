package auth

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	apiKeyHelperCacheTTL     = 5 * time.Minute
	apiKeyHelperTimeout      = 10 * time.Second
	apiKeyHelperShellCommand = "sh"
)

type apiKeyHelperCacheEntry struct {
	key    string
	expiry time.Time
}

var (
	apiKeyHelperCacheMu sync.Mutex
	apiKeyHelperCache   = map[string]apiKeyHelperCacheEntry{}
	apiKeyHelperNow     = time.Now
)

// ResolveAPIKeyViaHelper runs the configured helper command and returns the
// trimmed API key from stdout. Results are cached per command for five minutes.
func ResolveAPIKeyViaHelper(ctx context.Context, helperCmd string) (string, error) {
	helperCmd = strings.TrimSpace(helperCmd)
	if helperCmd == "" {
		return "", fmt.Errorf("resolve api key helper: empty command")
	}

	if key, ok := loadCachedAPIKey(helperCmd); ok {
		return key, nil
	}

	ctx, cancel := withAPIKeyHelperTimeout(ctx)
	defer cancel()

	var stdout bytes.Buffer

	cmd := exec.CommandContext(ctx, apiKeyHelperShellCommand, "-c", helperCmd)
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("run api key helper %q: %w", helperCmd, err)
	}

	key := strings.TrimSpace(stdout.String())
	if key == "" {
		return "", nil
	}

	storeCachedAPIKey(helperCmd, key)
	return key, nil
}

func withAPIKeyHelperTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, apiKeyHelperTimeout)
}

func loadCachedAPIKey(helperCmd string) (string, bool) {
	apiKeyHelperCacheMu.Lock()
	defer apiKeyHelperCacheMu.Unlock()

	entry, ok := apiKeyHelperCache[helperCmd]
	if !ok {
		return "", false
	}
	if apiKeyHelperNow().After(entry.expiry) {
		delete(apiKeyHelperCache, helperCmd)
		return "", false
	}
	return entry.key, true
}

func storeCachedAPIKey(helperCmd, key string) {
	apiKeyHelperCacheMu.Lock()
	defer apiKeyHelperCacheMu.Unlock()

	apiKeyHelperCache[helperCmd] = apiKeyHelperCacheEntry{
		key:    key,
		expiry: apiKeyHelperNow().Add(apiKeyHelperCacheTTL),
	}
}

func resetAPIKeyHelperCache() {
	apiKeyHelperCacheMu.Lock()
	defer apiKeyHelperCacheMu.Unlock()

	apiKeyHelperCache = map[string]apiKeyHelperCacheEntry{}
}

func expireAPIKeyHelperCacheEntry(helperCmd string) {
	apiKeyHelperCacheMu.Lock()
	defer apiKeyHelperCacheMu.Unlock()

	entry, ok := apiKeyHelperCache[helperCmd]
	if !ok {
		return
	}
	entry.expiry = apiKeyHelperNow().Add(-time.Second)
	apiKeyHelperCache[helperCmd] = entry
}
