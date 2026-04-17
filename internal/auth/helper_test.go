package auth

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveAPIKeyViaHelper_Success(t *testing.T) {
	resetAPIKeyHelperCache()
	t.Cleanup(resetAPIKeyHelperCache)

	script := writeHelperScript(t, "printf 'test-helper-key\\n'\n")

	key, err := ResolveAPIKeyViaHelper(context.Background(), script)
	require.NoError(t, err)
	assert.Equal(t, "test-helper-key", key)
}

func TestResolveAPIKeyViaHelper_Cache(t *testing.T) {
	resetAPIKeyHelperCache()
	t.Cleanup(resetAPIKeyHelperCache)

	dir := t.TempDir()
	countPath := filepath.Join(dir, "count.txt")
	script := writeHelperScript(t, fmt.Sprintf(`
count_file=%q
count=0
if [ -f "$count_file" ]; then
	count=$(cat "$count_file")
fi
count=$((count + 1))
printf '%%s' "$count" > "$count_file"
printf 'cached-key\n'
`, countPath))

	first, err := ResolveAPIKeyViaHelper(context.Background(), script)
	require.NoError(t, err)
	second, err := ResolveAPIKeyViaHelper(context.Background(), script)
	require.NoError(t, err)

	data, err := os.ReadFile(countPath)
	require.NoError(t, err)

	assert.Equal(t, "cached-key", first)
	assert.Equal(t, "cached-key", second)
	assert.Equal(t, "1", string(data))
}

func TestResolveAPIKeyViaHelper_CacheExpires(t *testing.T) {
	resetAPIKeyHelperCache()
	t.Cleanup(resetAPIKeyHelperCache)

	dir := t.TempDir()
	countPath := filepath.Join(dir, "count.txt")
	script := writeHelperScript(t, fmt.Sprintf(`
count_file=%q
count=0
if [ -f "$count_file" ]; then
	count=$(cat "$count_file")
fi
count=$((count + 1))
printf '%%s' "$count" > "$count_file"
printf 'expired-key\n'
`, countPath))

	_, err := ResolveAPIKeyViaHelper(context.Background(), script)
	require.NoError(t, err)

	expireAPIKeyHelperCacheEntry(script)

	_, err = ResolveAPIKeyViaHelper(context.Background(), script)
	require.NoError(t, err)

	data, err := os.ReadFile(countPath)
	require.NoError(t, err)
	assert.Equal(t, "2", string(data))
}

func TestResolveAPIKeyViaHelper_NonZeroExit(t *testing.T) {
	resetAPIKeyHelperCache()
	t.Cleanup(resetAPIKeyHelperCache)

	script := writeHelperScript(t, "exit 7\n")

	key, err := ResolveAPIKeyViaHelper(context.Background(), script)
	require.Error(t, err)
	assert.Empty(t, key)
	assert.Contains(t, err.Error(), script)
}

func writeHelperScript(t *testing.T, body string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "helper.sh")
	content := "#!/bin/sh\nset -eu\n" + body

	require.NoError(t, os.WriteFile(path, []byte(content), 0o755))
	return path
}
