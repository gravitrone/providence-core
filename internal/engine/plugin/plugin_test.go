package plugin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testManifestYAML = `apiVersion: providence/v1
name: test-plugin
displayName: Test Plugin
version: "1.0.0"
pluginApi: 1
compatibleProvidence: ">=0.1.0"
entrypoint:
  command: echo
  args: ["hello"]
capabilities:
  tabs: true
  commands: true
  tools: false
  background: false
contributes:
  tabs:
    - id: test-tab
      title: Test Tab
      icon: flame
      order: 10
  commands:
    - id: test-cmd
      title: Test Command
permissions:
  providence:
    - read:context
    - write:messages
`

func writeManifest(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(testManifestYAML), 0o644))
}

func TestParseManifest(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir)

	m, err := ParseManifest(filepath.Join(dir, "plugin.yaml"))
	require.NoError(t, err)

	assert.Equal(t, "test-plugin", m.Name)
	assert.Equal(t, "Test Plugin", m.DisplayName)
	assert.Equal(t, "1.0.0", m.Version)
	assert.Equal(t, 1, m.PluginAPI)
	assert.True(t, m.Capabilities.Tabs)
	assert.True(t, m.Capabilities.Commands)
	assert.False(t, m.Capabilities.Tools)
	assert.Len(t, m.Contributes.Tabs, 1)
	assert.Equal(t, "test-tab", m.Contributes.Tabs[0].ID)
	assert.Len(t, m.Contributes.Commands, 1)
	assert.Equal(t, "test-cmd", m.Contributes.Commands[0].ID)
	assert.Equal(t, []string{"read:context", "write:messages"}, m.Permissions.Providence)
}

func TestParseManifest_MissingName(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte("version: '1.0'\n"), 0o644))

	_, err := ParseManifest(filepath.Join(dir, "plugin.yaml"))
	assert.ErrorContains(t, err, "name")
}

func TestParseManifest_MissingVersion(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte("name: foo\n"), 0o644))

	_, err := ParseManifest(filepath.Join(dir, "plugin.yaml"))
	assert.ErrorContains(t, err, "version")
}

func TestManagerInstall(t *testing.T) {
	sourceDir := t.TempDir()
	writeManifest(t, sourceDir)

	pluginDir := t.TempDir()
	mgr := NewManager(pluginDir)

	require.NoError(t, mgr.Install(sourceDir))

	plugins := mgr.List()
	require.Len(t, plugins, 1)
	assert.Equal(t, "test-plugin", plugins[0].Manifest.Name)

	// Verify on-disk structure: pluginDir/test-plugin/1.0.0/plugin.yaml
	_, err := os.Stat(filepath.Join(pluginDir, "test-plugin", "1.0.0", "plugin.yaml"))
	assert.NoError(t, err)
}

func TestManagerList(t *testing.T) {
	sourceDir := t.TempDir()
	writeManifest(t, sourceDir)

	pluginDir := t.TempDir()
	mgr := NewManager(pluginDir)

	assert.Empty(t, mgr.List())

	require.NoError(t, mgr.Install(sourceDir))
	assert.Len(t, mgr.List(), 1)
}

func TestManagerRemove(t *testing.T) {
	sourceDir := t.TempDir()
	writeManifest(t, sourceDir)

	pluginDir := t.TempDir()
	mgr := NewManager(pluginDir)
	require.NoError(t, mgr.Install(sourceDir))

	require.NoError(t, mgr.Remove("test-plugin"))
	assert.Empty(t, mgr.List())

	// Verify directory removed.
	_, err := os.Stat(filepath.Join(pluginDir, "test-plugin"))
	assert.True(t, os.IsNotExist(err))
}

func TestManagerRemove_NotFound(t *testing.T) {
	mgr := NewManager(t.TempDir())
	err := mgr.Remove("nonexistent")
	assert.ErrorContains(t, err, "not found")
}

func TestManagerLoadAll(t *testing.T) {
	pluginDir := t.TempDir()

	// Pre-create the on-disk structure.
	destDir := filepath.Join(pluginDir, "test-plugin", "1.0.0")
	require.NoError(t, os.MkdirAll(destDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(destDir, "plugin.yaml"), []byte(testManifestYAML), 0o644))

	mgr := NewManager(pluginDir)
	require.NoError(t, mgr.LoadAll())

	plugins := mgr.List()
	require.Len(t, plugins, 1)
	assert.Equal(t, "test-plugin", plugins[0].Manifest.Name)
}

func TestManagerLoadAll_EmptyDir(t *testing.T) {
	mgr := NewManager(t.TempDir())
	require.NoError(t, mgr.LoadAll())
	assert.Empty(t, mgr.List())
}

func TestManagerLoadAll_NonexistentDir(t *testing.T) {
	mgr := NewManager(filepath.Join(t.TempDir(), "nonexistent"))
	require.NoError(t, mgr.LoadAll())
	assert.Empty(t, mgr.List())
}
