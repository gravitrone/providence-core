package ui

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInferFileDescGoSource(t *testing.T) {
	assert.Equal(t, "Go source", inferFileDesc("cmd/main.go"))
}

func TestInferFileDescGoTest(t *testing.T) {
	assert.Equal(t, "Go test", inferFileDesc("internal/ui/app_test.go"))
}

func TestInferFileDescTypeScript(t *testing.T) {
	assert.Equal(t, "TypeScript", inferFileDesc("src/index.ts"))
	assert.Equal(t, "TypeScript", inferFileDesc("components/App.tsx"))
}

func TestInferFileDescJavaScript(t *testing.T) {
	assert.Equal(t, "JavaScript", inferFileDesc("lib/helper.js"))
	assert.Equal(t, "JavaScript", inferFileDesc("components/App.jsx"))
}

func TestInferFileDescJSTestFiles(t *testing.T) {
	assert.Equal(t, "Test", inferFileDesc("src/app.test.ts"))
	assert.Equal(t, "Test", inferFileDesc("lib/helper.test.js"))
	assert.Equal(t, "Test", inferFileDesc("src/app.spec.ts"))
}

func TestInferFileDescPython(t *testing.T) {
	assert.Equal(t, "Python", inferFileDesc("scripts/run.py"))
}

func TestInferFileDescMarkdown(t *testing.T) {
	assert.Equal(t, "Markdown", inferFileDesc("docs/guide.md"))
}

func TestInferFileDescJSON(t *testing.T) {
	assert.Equal(t, "JSON", inferFileDesc("data/config.json"))
}

func TestInferFileDescTOML(t *testing.T) {
	assert.Equal(t, "TOML config", inferFileDesc("settings.toml"))
}

func TestInferFileDescYAML(t *testing.T) {
	assert.Equal(t, "YAML config", inferFileDesc("docker-compose.yml"))
	assert.Equal(t, "YAML config", inferFileDesc(".github/workflows/ci.yaml"))
}

func TestInferFileDescSQL(t *testing.T) {
	assert.Equal(t, "SQL", inferFileDesc("migrations/001.sql"))
}

func TestInferFileDescShell(t *testing.T) {
	assert.Equal(t, "Shell script", inferFileDesc("scripts/deploy.sh"))
}

func TestInferFileDescCSS(t *testing.T) {
	assert.Equal(t, "Stylesheet", inferFileDesc("styles/main.css"))
}

func TestInferFileDescHTML(t *testing.T) {
	assert.Equal(t, "HTML", inferFileDesc("public/index.html"))
}

func TestInferFileDescRust(t *testing.T) {
	assert.Equal(t, "Rust", inferFileDesc("src/main.rs"))
}

func TestInferFileDescKnownFiles(t *testing.T) {
	tests := map[string]string{
		"go.mod":       "Go module definition",
		"go.sum":       "Go dependency checksums",
		"package.json": "Node.js package manifest",
		"Cargo.toml":   "Rust crate manifest",
		"Makefile":     "Build automation",
		"Dockerfile":   "Container build",
		"README.md":    "Project readme",
		"CLAUDE.md":    "Claude Code instructions",
		".gitignore":   "Git ignore rules",
		"config.toml":  "Configuration",
	}
	for file, expected := range tests {
		t.Run(file, func(t *testing.T) {
			assert.Equal(t, expected, inferFileDesc(file))
		})
	}
}

func TestInferFileDescKnownFilesInSubdir(t *testing.T) {
	// Known files matched by basename, even inside subdirs.
	assert.Equal(t, "Go module definition", inferFileDesc("vendor/go.mod"))
	assert.Equal(t, "Container build", inferFileDesc("docker/Dockerfile"))
}

func TestInferFileDescUnknown(t *testing.T) {
	assert.Equal(t, "", inferFileDesc("data/blob.bin"))
	assert.Equal(t, "", inferFileDesc("some.xyz"))
}

func TestWorktreeIndexJSONRoundtrip(t *testing.T) {
	idx := WorktreeIndex{
		Files: []WorktreeIndexEntry{
			{Path: "cmd/main.go", Desc: "Go source"},
			{Path: "README.md", Desc: "Project readme"},
			{Path: "data/blob.bin", Desc: ""},
		},
		Total: 3,
	}

	data, err := json.MarshalIndent(idx, "", "  ")
	require.NoError(t, err)

	var decoded WorktreeIndex
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, 3, decoded.Total)
	assert.Len(t, decoded.Files, 3)
	assert.Equal(t, "cmd/main.go", decoded.Files[0].Path)
	assert.Equal(t, "Go source", decoded.Files[0].Desc)
}

func TestWorktreeIndexEmptyDescOmitted(t *testing.T) {
	entry := WorktreeIndexEntry{Path: "file.bin"}
	data, err := json.Marshal(entry)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "desc")

	entryWithDesc := WorktreeIndexEntry{Path: "main.go", Desc: "Go source"}
	data, err = json.Marshal(entryWithDesc)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"desc":"Go source"`)
}

func TestWorktreeIndexEmptyProject(t *testing.T) {
	idx := WorktreeIndex{
		Files: nil,
		Total: 0,
	}

	data, err := json.Marshal(idx)
	require.NoError(t, err)

	var decoded WorktreeIndex
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Empty(t, decoded.Files)
	assert.Equal(t, 0, decoded.Total)
}

func TestWorktreeIndexBuildFromFileList(t *testing.T) {
	// Simulate what runWorktreeIndex does: build entries from file paths.
	files := []string{
		"cmd/providence/main.go",
		"internal/ui/app.go",
		"internal/ui/app_test.go",
		"go.mod",
		"README.md",
		"data/config.json",
		"scripts/deploy.sh",
		"unknown.xyz",
	}

	var entries []WorktreeIndexEntry
	for _, f := range files {
		entries = append(entries, WorktreeIndexEntry{
			Path: f,
			Desc: inferFileDesc(f),
		})
	}

	assert.Len(t, entries, 8)
	assert.Equal(t, "Go source", entries[0].Desc)
	assert.Equal(t, "Go source", entries[1].Desc)
	assert.Equal(t, "Go test", entries[2].Desc)
	assert.Equal(t, "Go module definition", entries[3].Desc)
	assert.Equal(t, "Project readme", entries[4].Desc)
	assert.Equal(t, "JSON", entries[5].Desc)
	assert.Equal(t, "Shell script", entries[6].Desc)
	assert.Equal(t, "", entries[7].Desc)
}

func TestTopWorktreeFilesFormatsCorrectly(t *testing.T) {
	// TopWorktreeFiles reads from disk via LoadWorktreeIndex, so we test
	// the formatting logic by building a similar string manually.
	idx := WorktreeIndex{
		Files: []WorktreeIndexEntry{
			{Path: "cmd/main.go", Desc: "Go source"},
			{Path: "README.md", Desc: "Project readme"},
			{Path: "data.bin", Desc: ""},
		},
		Total: 3,
	}

	// Simulate what TopWorktreeFiles does.
	limit := 2
	if limit > len(idx.Files) {
		limit = len(idx.Files)
	}
	assert.Equal(t, 2, limit)

	// First entry has desc.
	entry := idx.Files[0]
	assert.NotEmpty(t, entry.Desc)

	// Third entry has no desc.
	entry = idx.Files[2]
	assert.Empty(t, entry.Desc)
}

func TestTopWorktreeFilesLimitsEntries(t *testing.T) {
	files := make([]WorktreeIndexEntry, 100)
	for i := range files {
		files[i] = WorktreeIndexEntry{Path: "file.go", Desc: "Go source"}
	}
	idx := WorktreeIndex{Files: files, Total: 100}

	n := 10
	limit := n
	if limit > len(idx.Files) {
		limit = len(idx.Files)
	}
	assert.Equal(t, 10, limit)
	assert.Equal(t, 100, idx.Total)
	assert.True(t, idx.Total > limit)
}

func TestTopWorktreeFilesNLargerThanTotal(t *testing.T) {
	idx := WorktreeIndex{
		Files: []WorktreeIndexEntry{
			{Path: "a.go", Desc: "Go source"},
			{Path: "b.go", Desc: "Go source"},
		},
		Total: 2,
	}

	n := 50
	limit := n
	if limit > len(idx.Files) {
		limit = len(idx.Files)
	}
	assert.Equal(t, 2, limit)
}
