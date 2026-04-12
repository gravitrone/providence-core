package plugin

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Manifest is the plugin.yaml schema.
type Manifest struct {
	APIVersion           string       `yaml:"apiVersion"`
	Name                 string       `yaml:"name"`
	DisplayName          string       `yaml:"displayName"`
	Version              string       `yaml:"version"`
	PluginAPI            int          `yaml:"pluginApi"`
	CompatibleProvidence string       `yaml:"compatibleProvidence"`
	Entrypoint           Entrypoint   `yaml:"entrypoint"`
	Capabilities         Capabilities `yaml:"capabilities"`
	Contributes          Contributes  `yaml:"contributes"`
	Permissions          Permissions  `yaml:"permissions"`
	Standalone           *Standalone  `yaml:"standalone,omitempty"`
}

// Entrypoint defines the command and args to launch the plugin subprocess.
type Entrypoint struct {
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
}

// Capabilities declares what features the plugin provides.
type Capabilities struct {
	Tabs       bool `yaml:"tabs"`
	Commands   bool `yaml:"commands"`
	Tools      bool `yaml:"tools"`
	Background bool `yaml:"background"`
}

// Contributes declares the tabs and commands the plugin registers.
type Contributes struct {
	Tabs     []TabContribution     `yaml:"tabs"`
	Commands []CommandContribution `yaml:"commands"`
}

// TabContribution declares a tab the plugin contributes to the UI.
type TabContribution struct {
	ID    string `yaml:"id"`
	Title string `yaml:"title"`
	Icon  string `yaml:"icon"`
	Order int    `yaml:"order"`
}

// CommandContribution declares a command the plugin registers.
type CommandContribution struct {
	ID    string `yaml:"id"`
	Title string `yaml:"title"`
}

// Permissions declares what Providence APIs the plugin may call.
type Permissions struct {
	Providence []string `yaml:"providence"`
}

// Standalone defines an optional standalone command for the plugin.
type Standalone struct {
	Command string `yaml:"command"`
}

// Manager handles plugin lifecycle: install, remove, start, stop.
type Manager struct {
	plugins   map[string]*LoadedPlugin
	pluginDir string
}

// LoadedPlugin pairs a parsed manifest with its on-disk directory and optional running process.
type LoadedPlugin struct {
	Manifest Manifest
	Dir      string
	Process  *exec.Cmd // running subprocess (nil if not started)
}

// NewManager creates a plugin manager rooted at pluginDir.
func NewManager(pluginDir string) *Manager {
	return &Manager{
		plugins:   make(map[string]*LoadedPlugin),
		pluginDir: pluginDir,
	}
}

// ParseManifest reads and validates a plugin.yaml file.
func ParseManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to parse manifest yaml: %w", err)
	}
	if m.Name == "" {
		return nil, fmt.Errorf("manifest missing required field: name")
	}
	if m.Version == "" {
		return nil, fmt.Errorf("manifest missing required field: version")
	}
	return &m, nil
}

// Install installs a plugin from a local source directory containing plugin.yaml.
func (m *Manager) Install(sourcePath string) error {
	manifestPath := filepath.Join(sourcePath, "plugin.yaml")
	manifest, err := ParseManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("failed to parse plugin manifest: %w", err)
	}

	destDir := filepath.Join(m.pluginDir, manifest.Name, manifest.Version)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("failed to create plugin directory: %w", err)
	}

	// Copy manifest to destination.
	srcData, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("failed to read source manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(destDir, "plugin.yaml"), srcData, 0o644); err != nil { //nolint:gosec // plugin install writes to managed directory
		return fmt.Errorf("failed to write manifest: %w", err)
	}

	m.plugins[manifest.Name] = &LoadedPlugin{
		Manifest: *manifest,
		Dir:      destDir,
	}
	return nil
}

// List returns all loaded plugins.
func (m *Manager) List() []*LoadedPlugin {
	result := make([]*LoadedPlugin, 0, len(m.plugins))
	for _, p := range m.plugins {
		result = append(result, p)
	}
	return result
}

// Remove removes a plugin by name from disk and the registry.
func (m *Manager) Remove(name string) error {
	p, ok := m.plugins[name]
	if !ok {
		return fmt.Errorf("plugin %q not found", name)
	}
	// Stop if running.
	if p.Process != nil && p.Process.Process != nil {
		_ = p.Process.Process.Kill()
	}
	// Remove the version directory and the parent name directory.
	nameDir := filepath.Dir(p.Dir)
	if err := os.RemoveAll(nameDir); err != nil {
		return fmt.Errorf("failed to remove plugin directory: %w", err)
	}
	delete(m.plugins, name)
	return nil
}

// LoadAll discovers installed plugins by scanning pluginDir.
func (m *Manager) LoadAll() error {
	entries, err := os.ReadDir(m.pluginDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read plugin directory: %w", err)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Scan version subdirectories, use the first found.
		nameDir := filepath.Join(m.pluginDir, e.Name())
		versions, err := os.ReadDir(nameDir)
		if err != nil {
			continue
		}
		for _, v := range versions {
			if !v.IsDir() {
				continue
			}
			manifestPath := filepath.Join(nameDir, v.Name(), "plugin.yaml")
			manifest, err := ParseManifest(manifestPath)
			if err != nil {
				continue
			}
			m.plugins[manifest.Name] = &LoadedPlugin{
				Manifest: *manifest,
				Dir:      filepath.Join(nameDir, v.Name()),
			}
			break // use first version found
		}
	}
	return nil
}

// Start launches a plugin subprocess.
func (m *Manager) Start(name string) error {
	p, ok := m.plugins[name]
	if !ok {
		return fmt.Errorf("plugin %q not found", name)
	}
	if p.Process != nil {
		return fmt.Errorf("plugin %q already running", name)
	}

	cmd := exec.Command(p.Manifest.Entrypoint.Command, p.Manifest.Entrypoint.Args...) //nolint:gosec
	cmd.Dir = p.Dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start plugin %q: %w", name, err)
	}
	p.Process = cmd
	return nil
}

// Stop stops a running plugin subprocess.
func (m *Manager) Stop(name string) error {
	p, ok := m.plugins[name]
	if !ok {
		return fmt.Errorf("plugin %q not found", name)
	}
	if p.Process == nil || p.Process.Process == nil {
		return fmt.Errorf("plugin %q not running", name)
	}
	if err := p.Process.Process.Kill(); err != nil {
		return fmt.Errorf("failed to stop plugin %q: %w", name, err)
	}
	p.Process = nil
	return nil
}
