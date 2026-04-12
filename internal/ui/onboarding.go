package ui

import (
	"os"
	"path/filepath"
)

// OnboardingModel handles first-run setup wizard.
type OnboardingModel struct {
	Step int  // 0=engine, 1=auth, 2=theme, 3=done
	Done bool
}

// IsFirstRun checks if .providence/ directory exists in homeDir.
func IsFirstRun(homeDir string) bool {
	_, err := os.Stat(filepath.Join(homeDir, ".providence"))
	return os.IsNotExist(err)
}

// WelcomeMessage returns the system message shown on first run.
func WelcomeMessage() string {
	return "Welcome to Providence! Run /help for commands."
}
