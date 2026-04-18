package subagent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withHome points os.UserHomeDir() at t.TempDir() for the duration of the test.
func withHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func TestLoadAgentMemoryReturnsAllThreeScopes(t *testing.T) {
	home := withHome(t)
	project := t.TempDir()

	writeFile(t, filepath.Join(home, ".providence", "agent-memory", "researcher", "user.md"), "user lesson")
	writeFile(t, filepath.Join(project, ".providence", "agent-memory", "researcher", "project.md"), "project note")
	writeFile(t, filepath.Join(project, ".providence", "agent-memory", "researcher", "local.md"), "local draft")

	blocks := LoadAgentMemory("researcher", project)
	require.Len(t, blocks, 3)

	assert.Equal(t, MemoryScopeUser, blocks[0].Scope)
	assert.Equal(t, "user lesson", blocks[0].Content)
	assert.Equal(t, MemoryScopeProject, blocks[1].Scope)
	assert.Equal(t, "project note", blocks[1].Content)
	assert.Equal(t, MemoryScopeLocal, blocks[2].Scope)
	assert.Equal(t, "local draft", blocks[2].Content)
}

func TestLoadAgentMemoryMissingScopeReturnsEmpty(t *testing.T) {
	_ = withHome(t)
	project := t.TempDir()

	blocks := LoadAgentMemory("researcher", project)
	assert.Empty(t, blocks)
}

func TestLoadAgentMemorySkipsEmptyFiles(t *testing.T) {
	home := withHome(t)
	project := t.TempDir()

	writeFile(t, filepath.Join(home, ".providence", "agent-memory", "default", "user.md"), "   \n\n")
	writeFile(t, filepath.Join(project, ".providence", "agent-memory", "default", "project.md"), "real content")

	blocks := LoadAgentMemory("", project)
	require.Len(t, blocks, 1)
	assert.Equal(t, MemoryScopeProject, blocks[0].Scope)
	assert.Equal(t, "real content", blocks[0].Content)
}

func TestWriteAgentMemoryAppend(t *testing.T) {
	home := withHome(t)
	project := t.TempDir()

	require.NoError(t, WriteAgentMemoryScope(MemoryScopeUser, "reviewer", project, "first lesson", OperationAppend))
	require.NoError(t, WriteAgentMemoryScope(MemoryScopeUser, "reviewer", project, "second lesson", OperationAppend))

	data, err := os.ReadFile(filepath.Join(home, ".providence", "agent-memory", "reviewer", "user.md"))
	require.NoError(t, err)

	body := string(data)
	assert.Contains(t, body, "first lesson")
	assert.Contains(t, body, "second lesson")
	// Appends should yield two timestamped headers.
	assert.Equal(t, 2, strings.Count(body, "## "))
}

func TestWriteAgentMemoryReplace(t *testing.T) {
	_ = withHome(t)
	project := t.TempDir()

	require.NoError(t, WriteAgentMemoryScope(MemoryScopeProject, "reviewer", project, "old content", OperationAppend))
	require.NoError(t, WriteAgentMemoryScope(MemoryScopeProject, "reviewer", project, "fresh replacement", OperationReplace))

	data, err := os.ReadFile(filepath.Join(project, ".providence", "agent-memory", "reviewer", "project.md"))
	require.NoError(t, err)

	body := string(data)
	assert.Equal(t, "fresh replacement", body)
	assert.NotContains(t, body, "old content")
}

func TestWriteAgentMemoryRejectsLocalScope(t *testing.T) {
	_ = withHome(t)
	project := t.TempDir()

	err := WriteAgentMemoryScope(MemoryScopeLocal, "reviewer", project, "sneaky write", OperationAppend)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read-only")

	// Nothing should have been written.
	_, statErr := os.Stat(filepath.Join(project, ".providence", "agent-memory", "reviewer", "local.md"))
	assert.True(t, os.IsNotExist(statErr), "local.md must not exist after rejected write")
}

func TestWriteAgentMemoryRejectsEmptyContent(t *testing.T) {
	_ = withHome(t)
	project := t.TempDir()

	err := WriteAgentMemoryScope(MemoryScopeUser, "reviewer", project, "   \n\t", OperationAppend)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestWriteAgentMemoryTruncatesAtSizeCap(t *testing.T) {
	home := withHome(t)
	project := t.TempDir()

	// Seed a ~40KB file with three entries, then push two more over the cap.
	bigChunk := strings.Repeat("a", 20*1024)
	require.NoError(t, WriteAgentMemoryScope(MemoryScopeUser, "bulk", project, bigChunk, OperationAppend))
	require.NoError(t, WriteAgentMemoryScope(MemoryScopeUser, "bulk", project, bigChunk, OperationAppend))
	require.NoError(t, WriteAgentMemoryScope(MemoryScopeUser, "bulk", project, "marker-latest", OperationAppend))

	path := filepath.Join(home, ".providence", "agent-memory", "bulk", "user.md")
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	assert.LessOrEqual(t, len(data), MemoryScopeSizeCap, "final file must respect the size cap")
	assert.Contains(t, string(data), "marker-latest", "newest entry must survive truncation")
}

func TestWriteAgentMemoryUnknownScopeErrors(t *testing.T) {
	_ = withHome(t)
	project := t.TempDir()

	err := WriteAgentMemoryScope(MemoryScope("bogus"), "reviewer", project, "content", OperationAppend)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown memory scope")
}

func TestWriteAgentMemoryUnknownOperationErrors(t *testing.T) {
	_ = withHome(t)
	project := t.TempDir()

	err := WriteAgentMemoryScope(MemoryScopeUser, "reviewer", project, "content", Operation("wut"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown operation")
}

func TestRenderMemoryBlocksFormat(t *testing.T) {
	blocks := []MemoryBlock{
		{Scope: MemoryScopeUser, Content: "u"},
		{Scope: MemoryScopeProject, Content: "p"},
	}
	rendered := RenderMemoryBlocks(blocks)
	assert.Contains(t, rendered, `<agent-memory scope="user">`)
	assert.Contains(t, rendered, `<agent-memory scope="project">`)
	assert.Contains(t, rendered, "u")
	assert.Contains(t, rendered, "p")
}

func TestInjectAgentMemoryNoBlocks(t *testing.T) {
	_ = withHome(t)
	project := t.TempDir()

	out := InjectAgentMemory("base prompt", "researcher", project)
	assert.Equal(t, "base prompt", out)
}

func TestInjectAgentMemoryWithBlocks(t *testing.T) {
	home := withHome(t)
	project := t.TempDir()

	writeFile(t, filepath.Join(home, ".providence", "agent-memory", "researcher", "user.md"), "u-lesson")
	writeFile(t, filepath.Join(project, ".providence", "agent-memory", "researcher", "project.md"), "p-lesson")

	out := InjectAgentMemory("base prompt", "researcher", project)
	assert.Contains(t, out, "base prompt")
	assert.Contains(t, out, `<agent-memory scope="user">`)
	assert.Contains(t, out, "u-lesson")
	assert.Contains(t, out, `<agent-memory scope="project">`)
	assert.Contains(t, out, "p-lesson")
}

func TestSubagentSpawnInjectsMemoryBlocks(t *testing.T) {
	home := withHome(t)
	project := t.TempDir()

	writeFile(t, filepath.Join(home, ".providence", "agent-memory", "researcher", "user.md"), "remember async is hard")
	writeFile(t, filepath.Join(project, ".providence", "agent-memory", "researcher", "project.md"), "repo uses asyncpg")

	r := NewRunnerWithWorkDir(project)

	var capturedSystemPrompt string
	var mu sync.Mutex
	exec := func(ctx context.Context, prompt string, agentType AgentType) (string, error) {
		mu.Lock()
		capturedSystemPrompt = agentType.SystemPrompt
		mu.Unlock()
		return "done", nil
	}

	agentType := AgentType{
		Name:         "researcher",
		SystemPrompt: "You are a researcher.",
	}
	input := TaskInput{
		Description:  "research task",
		Prompt:       "dig in",
		SubagentType: "researcher",
	}

	_, err := r.Spawn(context.Background(), input, agentType, exec)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, capturedSystemPrompt, "You are a researcher.")
	assert.Contains(t, capturedSystemPrompt, `<agent-memory scope="user">`)
	assert.Contains(t, capturedSystemPrompt, "remember async is hard")
	assert.Contains(t, capturedSystemPrompt, `<agent-memory scope="project">`)
	assert.Contains(t, capturedSystemPrompt, "repo uses asyncpg")
}

func TestSubagentSpawnNoMemoryLeavesPromptUnchanged(t *testing.T) {
	_ = withHome(t)
	project := t.TempDir()

	r := NewRunnerWithWorkDir(project)

	var capturedSystemPrompt string
	var mu sync.Mutex
	exec := func(ctx context.Context, prompt string, agentType AgentType) (string, error) {
		mu.Lock()
		capturedSystemPrompt = agentType.SystemPrompt
		mu.Unlock()
		return "ok", nil
	}

	agentType := AgentType{
		Name:         "reviewer",
		SystemPrompt: "You review code.",
	}
	input := TaskInput{
		Prompt:       "review",
		SubagentType: "reviewer",
	}

	_, err := r.Spawn(context.Background(), input, agentType, exec)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "You review code.", capturedSystemPrompt)
}

func TestNormalizeAgentTypeStripsTraversal(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "default"},
		{"researcher", "researcher"},
		{"../etc", "etc"},
		{"../../passwd", "passwd"},
		{"weird/slash", "weirdslash"},
		{"ok_name-123", "ok_name-123"},
		{"!!!", "default"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, normalizeAgentType(tc.in))
		})
	}
}

func TestFormatAppendEntryIncludesTimestamp(t *testing.T) {
	ts := time.Date(2026, 4, 19, 12, 30, 45, 0, time.UTC)
	entry := formatAppendEntry(ts, "hello")
	assert.Contains(t, entry, "## 2026-04-19T12:30:45Z")
	assert.Contains(t, entry, "hello")
}

// --- Helpers ---

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}
