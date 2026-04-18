package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BurntSushi/toml"
)

// DefaultAsyncHookTTL is the fallback lifetime for async hook results.
const DefaultAsyncHookTTL = 60 * time.Second

const (
	// HookStatusOK marks a successful async hook completion.
	HookStatusOK = "ok"
	// HookStatusError marks a failed async hook completion.
	HookStatusError = "error"
)

// CompletedHook describes a finished async hook waiting to be injected.
type CompletedHook struct {
	ID          string
	Sequence    uint64
	Event       string
	Name        string
	Status      string
	Output      string
	CompletedAt time.Time
}

// PendingHook tracks an in-flight async hook until it completes or expires.
type PendingHook struct {
	ID        string
	Sequence  uint64
	Event     string
	Name      string
	StartedAt time.Time
	ResultCh  chan CompletedHook
	DoneCh    chan struct{}

	cancel   context.CancelFunc
	timer    *time.Timer
	done     bool
	result   *CompletedHook
	doneOnce sync.Once
}

func (h *PendingHook) closeDone() {
	h.doneOnce.Do(func() {
		close(h.DoneCh)
	})
}

// HookExecutor runs a single hook configuration.
type HookExecutor func(ctx context.Context, cfg HookConfig, input HookInput) (*HookOutput, error)

// PendingHooks stores async hooks that are still running or waiting to be drained.
type PendingHooks struct {
	mu      sync.Mutex
	pending map[string]*PendingHook
	nextID  atomic.Uint64
}

// NewPendingHooks creates an empty async hook registry.
func NewPendingHooks() *PendingHooks {
	return &PendingHooks{
		pending: make(map[string]*PendingHook),
	}
}

// Dispatch starts a hook in the background and tracks it until completion or expiry.
func (p *PendingHooks) Dispatch(parent context.Context, event string, cfg HookConfig, input HookInput, exec HookExecutor) string {
	if exec == nil {
		return ""
	}
	if parent == nil {
		parent = context.Background()
	}

	sequence := p.nextID.Add(1)
	hookName := cfg.Name()
	hookID := fmt.Sprintf("%s/%s/%d", event, hookName, sequence)
	ctx, cancel := context.WithCancel(parent)

	pending := &PendingHook{
		ID:        hookID,
		Sequence:  sequence,
		Event:     event,
		Name:      hookName,
		StartedAt: time.Now(),
		ResultCh:  make(chan CompletedHook, 1),
		DoneCh:    make(chan struct{}),
		cancel:    cancel,
	}

	ttl := cfg.EffectiveTTL()
	pending.timer = time.AfterFunc(ttl, func() {
		p.expire(hookID, ttl)
	})

	p.mu.Lock()
	p.pending[hookID] = pending
	p.mu.Unlock()

	go func() {
		out, err := exec(ctx, cfg, input)
		p.complete(hookID, buildCompletedHook(hookID, sequence, event, hookName, out, err))
	}()

	return hookID
}

// DrainCompleted removes completed hooks from the registry and returns them in start order.
func (p *PendingHooks) DrainCompleted() []CompletedHook {
	p.mu.Lock()
	defer p.mu.Unlock()

	results := make([]CompletedHook, 0)
	for hookID, pending := range p.pending {
		if pending.result == nil {
			continue
		}
		results = append(results, *pending.result)
		delete(p.pending, hookID)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Sequence < results[j].Sequence
	})

	return results
}

// PendingCount returns the number of tracked async hooks, including completed ones not yet drained.
func (p *PendingHooks) PendingCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.pending)
}

// CompletedCount returns the number of completed async hooks waiting to be drained.
func (p *PendingHooks) CompletedCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	count := 0
	for _, pending := range p.pending {
		if pending.result != nil {
			count++
		}
	}
	return count
}

// Close cancels all tracked async hooks and clears the registry.
func (p *PendingHooks) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for hookID, pending := range p.pending {
		if pending.timer != nil {
			pending.timer.Stop()
		}
		if pending.cancel != nil {
			pending.cancel()
		}
		pending.closeDone()
		delete(p.pending, hookID)
	}
}

func (p *PendingHooks) complete(hookID string, result CompletedHook) {
	p.mu.Lock()
	defer p.mu.Unlock()

	pending, ok := p.pending[hookID]
	if !ok || pending.done {
		return
	}

	pending.done = true
	pending.result = &result
	if pending.timer != nil {
		pending.timer.Stop()
	}
	select {
	case pending.ResultCh <- result:
	default:
	}
	pending.closeDone()
}

func (p *PendingHooks) expire(hookID string, ttl time.Duration) {
	p.mu.Lock()
	pending, ok := p.pending[hookID]
	if !ok || pending.done {
		p.mu.Unlock()
		return
	}
	delete(p.pending, hookID)
	p.mu.Unlock()

	if pending.cancel != nil {
		pending.cancel()
	}
	pending.closeDone()
	log.Printf("async hook ttl expired: id=%s event=%s name=%s ttl=%s", pending.ID, pending.Event, pending.Name, ttl)
}

func buildCompletedHook(hookID string, sequence uint64, event, name string, out *HookOutput, err error) CompletedHook {
	status := HookStatusOK
	if err != nil {
		status = HookStatusError
	}

	return CompletedHook{
		ID:          hookID,
		Sequence:    sequence,
		Event:       event,
		Name:        name,
		Status:      status,
		Output:      formatHookExecutionOutput(out, err),
		CompletedAt: time.Now(),
	}
}

func formatHookExecutionOutput(out *HookOutput, err error) string {
	hookText := formatHookOutput(out)
	if err == nil {
		return hookText
	}
	if hookText == "" {
		return err.Error()
	}
	if strings.Contains(hookText, err.Error()) {
		return hookText
	}
	return err.Error() + "\n" + hookText
}

func formatHookOutput(out *HookOutput) string {
	if out == nil {
		return ""
	}

	var parts []string
	if out.SystemMessage != "" {
		parts = append(parts, out.SystemMessage)
	}
	if out.Reason != "" {
		parts = append(parts, out.Reason)
	}
	if out.StopReason != "" {
		parts = append(parts, out.StopReason)
	}
	if out.Decision != "" {
		parts = append(parts, fmt.Sprintf("decision=%s", out.Decision))
	}
	if out.Continue != nil {
		parts = append(parts, fmt.Sprintf("continue=%t", *out.Continue))
	}
	if out.SuppressOutput {
		parts = append(parts, "suppress_output=true")
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n")
	}

	data, err := json.Marshal(out)
	if err != nil || string(data) == "{}" {
		return ""
	}
	return string(data)
}

// EffectiveTTL returns the configured TTL or the default when unset.
func (c HookConfig) EffectiveTTL() time.Duration {
	if c.TTL <= 0 {
		return DefaultAsyncHookTTL
	}
	return c.TTL
}

// Name returns a stable display name for the hook configuration.
func (c HookConfig) Name() string {
	if c.Command != "" {
		fields := strings.Fields(c.Command)
		if len(fields) > 0 {
			name := filepath.Base(fields[0])
			name = strings.TrimSuffix(name, filepath.Ext(name))
			if name != "" {
				return name
			}
		}
		return "shell"
	}
	if c.URL != "" {
		parsed, err := url.Parse(c.URL)
		if err == nil {
			segment := strings.Trim(parsed.EscapedPath(), "/")
			if segment != "" {
				name := path.Base(segment)
				name = strings.TrimSuffix(name, path.Ext(name))
				if name != "" && name != "." && name != "/" {
					return name
				}
			}
			if host := parsed.Hostname(); host != "" {
				return host
			}
		}
		return "http"
	}
	return "hook"
}

// ResolveHookConfigs overlays async-aware hook definitions from local config files.
func ResolveHookConfigs(workDir string, fallback map[string][]HookConfig) map[string][]HookConfig {
	resolved := cloneHookConfigs(fallback)
	if workDir == "" {
		return resolved
	}

	raw := loadRawTOMLHooks(workDir)
	if len(raw) == 0 {
		raw = loadRawClaudeSettingsHooks(workDir)
	}
	if len(raw) == 0 {
		return resolved
	}

	for event, entries := range raw {
		resolved[event] = entries
	}
	return resolved
}

type rawHookEntry struct {
	Command string `toml:"command" json:"command,omitempty"`
	URL     string `toml:"url" json:"url,omitempty"`
	Timeout int    `toml:"timeout" json:"timeout,omitempty"`
	Async   bool   `toml:"async" json:"async,omitempty"`
	TTL     int    `toml:"ttl" json:"ttl,omitempty"`
	TTLMS   int    `toml:"ttl_ms" json:"ttl_ms,omitempty"`
}

type rawHooksConfig struct {
	Hooks map[string][]rawHookEntry `toml:"hooks"`
}

type rawClaudeSettings struct {
	Hooks map[string]any `json:"hooks"`
}

func cloneHookConfigs(source map[string][]HookConfig) map[string][]HookConfig {
	if len(source) == 0 {
		return make(map[string][]HookConfig)
	}

	cloned := make(map[string][]HookConfig, len(source))
	for event, entries := range source {
		copied := make([]HookConfig, len(entries))
		copy(copied, entries)
		cloned[event] = copied
	}
	return cloned
}

func loadRawTOMLHooks(workDir string) map[string][]HookConfig {
	home, _ := os.UserHomeDir()
	paths := []string{
		filepath.Join(home, ".providence", "config.toml"),
		filepath.Join(workDir, ".providence", "config.toml"),
		filepath.Join(workDir, ".providence", "config.local.toml"),
		filepath.Join(workDir, ".claude", "config.toml"),
		"/Library/Application Support/Providence/managed-settings.toml",
	}

	merged := make(map[string][]HookConfig)
	for _, configPath := range paths {
		loaded := loadRawTOMLHooksFile(configPath)
		for event, entries := range loaded {
			merged[event] = entries
		}
	}
	return merged
}

func loadRawTOMLHooksFile(configPath string) map[string][]HookConfig {
	if configPath == "" {
		return nil
	}
	if _, err := os.Stat(configPath); err != nil {
		return nil
	}

	var raw rawHooksConfig
	if _, err := toml.DecodeFile(configPath, &raw); err != nil {
		return nil
	}

	return convertRawHooks(raw.Hooks)
}

func loadRawClaudeSettingsHooks(workDir string) map[string][]HookConfig {
	settingsPath := filepath.Join(workDir, ".claude", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return nil
	}

	var raw rawClaudeSettings
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}

	converted := make(map[string][]HookConfig)
	for event, value := range raw.Hooks {
		entries := parseRawHookEntries(value)
		if len(entries) == 0 {
			continue
		}
		converted[event] = entries
	}
	return converted
}

func parseRawHookEntries(value any) []HookConfig {
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}

	var entries []rawHookEntry
	if err := json.Unmarshal(data, &entries); err == nil {
		return convertRawHookEntries(entries)
	}

	var single rawHookEntry
	if err := json.Unmarshal(data, &single); err == nil && (single.Command != "" || single.URL != "") {
		return convertRawHookEntries([]rawHookEntry{single})
	}

	return nil
}

func convertRawHooks(raw map[string][]rawHookEntry) map[string][]HookConfig {
	converted := make(map[string][]HookConfig, len(raw))
	for event, entries := range raw {
		hooks := convertRawHookEntries(entries)
		if len(hooks) == 0 {
			continue
		}
		converted[event] = hooks
	}
	return converted
}

func convertRawHookEntries(entries []rawHookEntry) []HookConfig {
	converted := make([]HookConfig, 0, len(entries))
	for _, entry := range entries {
		if entry.Command == "" && entry.URL == "" {
			continue
		}

		hook := HookConfig{
			Command: entry.Command,
			URL:     entry.URL,
			Async:   entry.Async,
		}
		if entry.Timeout > 0 {
			hook.Timeout = time.Duration(entry.Timeout) * time.Millisecond
		}
		switch {
		case entry.TTLMS > 0:
			hook.TTL = time.Duration(entry.TTLMS) * time.Millisecond
		case entry.TTL > 0:
			hook.TTL = time.Duration(entry.TTL) * time.Millisecond
		}
		converted = append(converted, hook)
	}
	return converted
}
