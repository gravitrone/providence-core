//go:build darwin

package macos

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBridgeFacadeFallbackOnMissing(t *testing.T) {
	commandDir := t.TempDir()
	writeExecutable(t, commandDir, "screencapture", `#!/bin/sh
for last
do
	:
done
touch "$last"
`)
	t.Setenv("PATH", commandDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	client := spawnTestClient(t, "FAKE_SWIFT_PERMISSION_DENIED=1")
	defer func() {
		_ = client.Close(context.Background())
	}()

	bridge := New()
	bridge.swift = client

	path, err := bridge.Screenshot(t.Context())
	require.NoError(t, err)
	assert.NotEmpty(t, path)
	assert.False(t, bridge.caps[CapScreenshot])
}

func TestBridgeFacadeShellOnly(t *testing.T) {
	commandDir := t.TempDir()
	writeExecutable(t, commandDir, "screencapture", `#!/bin/sh
for last
do
	:
done
touch "$last"
`)
	t.Setenv("PATH", commandDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	bridge := New(WithMode("shell"))

	path, err := bridge.Screenshot(t.Context())
	require.NoError(t, err)
	assert.NotEmpty(t, path)
	assert.Nil(t, bridge.swift)
}

func TestBridgeFacadeLookupSwiftBinary(t *testing.T) {
	originalExecutableFunc := osExecutableFunc
	originalGlobalInstallDir := swiftGlobalInstallDir
	t.Cleanup(func() {
		osExecutableFunc = originalExecutableFunc
		swiftGlobalInstallDir = originalGlobalInstallDir
	})

	tests := []struct {
		name     string
		cfgPath  string
		setup    func(t *testing.T, executablePath string) string
		expected func(created string) string
	}{
		{
			name: "config path",
			setup: func(t *testing.T, _ string) string {
				return writeExecutable(t, t.TempDir(), "providence-mac-bridge", "#!/bin/sh\nexit 0\n")
			},
			expected: func(created string) string {
				return created
			},
		},
		{
			name: "xdg data home",
			setup: func(t *testing.T, _ string) string {
				xdg := t.TempDir()
				t.Setenv("XDG_DATA_HOME", xdg)
				t.Setenv("HOME", t.TempDir())
				return writeExecutable(t, filepath.Join(xdg, "providence"), "providence-mac-bridge", "#!/bin/sh\nexit 0\n")
			},
			expected: func(created string) string {
				return created
			},
		},
		{
			name: "sibling executable",
			setup: func(t *testing.T, executablePath string) string {
				t.Setenv("XDG_DATA_HOME", t.TempDir())
				t.Setenv("HOME", t.TempDir())
				return writeExecutable(t, filepath.Dir(executablePath), "providence-mac-bridge", "#!/bin/sh\nexit 0\n")
			},
			expected: func(created string) string {
				return created
			},
		},
		{
			name: "home bin",
			setup: func(t *testing.T, _ string) string {
				t.Setenv("XDG_DATA_HOME", t.TempDir())
				home := t.TempDir()
				t.Setenv("HOME", home)
				return writeExecutable(t, filepath.Join(home, ".providence", "bin"), "providence-mac-bridge", "#!/bin/sh\nexit 0\n")
			},
			expected: func(created string) string {
				return created
			},
		},
		{
			name: "global install",
			setup: func(t *testing.T, _ string) string {
				t.Setenv("XDG_DATA_HOME", t.TempDir())
				t.Setenv("HOME", t.TempDir())
				global := writeExecutable(t, t.TempDir(), "providence-mac-bridge", "#!/bin/sh\nexit 0\n")
				swiftGlobalInstallDir = global
				return global
			},
			expected: func(created string) string {
				return created
			},
		},
		{
			name: "none",
			setup: func(t *testing.T, _ string) string {
				t.Setenv("XDG_DATA_HOME", t.TempDir())
				t.Setenv("HOME", t.TempDir())
				swiftGlobalInstallDir = filepath.Join(t.TempDir(), "missing")
				return ""
			},
			expected: func(_ string) string {
				return ""
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			executablePath := filepath.Join(root, "bin", "providence")
			require.NoError(t, os.MkdirAll(filepath.Dir(executablePath), 0o755))
			writeExecutable(t, filepath.Dir(executablePath), "providence", "#!/bin/sh\nexit 0\n")

			osExecutableFunc = func() (string, error) {
				return executablePath, nil
			}
			swiftGlobalInstallDir = filepath.Join(t.TempDir(), "missing")
			created := tc.setup(t, executablePath)

			cfgPath := tc.cfgPath
			if tc.name == "config path" {
				cfgPath = created
			}

			got := lookupSwiftBinary(cfgPath)
			assert.Equal(t, tc.expected(created), got)
		})
	}
}

func writeExecutable(t *testing.T, dir, name, content string) string {
	t.Helper()

	require.NoError(t, os.MkdirAll(dir, 0o755))

	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755))

	return path
}
