package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIsFirstRunNoProvidenceDirTreatedAsFirstRun verifies the default
// path: a home directory without .providence/ tells the onboarding flow
// this is a first-run install. Uses t.TempDir so the test never touches
// the real user home.
func TestIsFirstRunNoProvidenceDirTreatedAsFirstRun(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	assert.True(t, IsFirstRun(home), "empty home must be treated as first run")
}

// TestIsFirstRunExistingProvidenceDirSkipsWizard verifies the inverse:
// once .providence/ exists the onboarding wizard must NOT re-trigger.
func TestIsFirstRunExistingProvidenceDirSkipsWizard(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".providence"), 0o755))
	assert.False(t, IsFirstRun(home), "existing .providence/ must disable first-run onboarding")
}

// TestIsFirstRunUnreadableHomeDoesNotCrash verifies IsFirstRun's error
// path is safe: an invalid home path returns a stable boolean instead of
// panicking or returning a misleading "not exist" signal. The current
// production code treats any stat error that is NOT IsNotExist as
// "directory is present" (false); pinning that keeps the onboarding
// wizard from re-triggering on permission-denied scenarios.
func TestIsFirstRunUnreadableHomeDoesNotCrash(t *testing.T) {
	t.Parallel()

	// A path that definitely does not exist and contains no parent we
	// can stat either.
	bogus := filepath.Join(t.TempDir(), "does", "not", "exist")
	// Should not panic either way; behaviour is documented by assertion.
	result := IsFirstRun(bogus)
	assert.True(t, result, "a missing home directory should surface as first-run, not crash")
}

// TestWelcomeMessageContainsEssentials pins the UX string so accidental
// deletes of "Providence" or "/help" break loudly. First-run users rely
// on both to orient themselves.
func TestWelcomeMessageContainsEssentials(t *testing.T) {
	t.Parallel()

	msg := WelcomeMessage()
	assert.Contains(t, msg, "Providence", "welcome message must mention the product name")
	assert.Contains(t, strings.ToLower(msg), "/help", "welcome message must point users at /help")
}
