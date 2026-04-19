package permissions

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withTempStore(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(storeEnvVar, dir)
	return dir
}

func TestSaveAndLoadRulesRoundtrip(t *testing.T) {
	withTempStore(t)

	rules := []Rule{
		{Pattern: "Bash(git *)", Behavior: Allow, Source: "userSettings"},
		{Pattern: "Write", Behavior: Deny, Source: "projectSettings"},
	}

	require.NoError(t, SaveRules("/home/user/project-a", rules))

	loaded, err := LoadRules("/home/user/project-a")
	require.NoError(t, err)
	assert.Equal(t, rules, loaded)
}

func TestLoadRulesMissingFileReturnsEmpty(t *testing.T) {
	withTempStore(t)

	loaded, err := LoadRules("/tmp/never-saved")
	require.NoError(t, err)
	assert.Nil(t, loaded)
}

func TestSaveRulesDifferentProjectsDoNotCollide(t *testing.T) {
	dir := withTempStore(t)

	require.NoError(t, SaveRules("/a", []Rule{{Pattern: "Read"}}))
	require.NoError(t, SaveRules("/b", []Rule{{Pattern: "Write"}}))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	// Expect exactly two payload files (ignore tmp residue on clean runs).
	count := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			count++
		}
	}
	assert.Equal(t, 2, count)

	a, err := LoadRules("/a")
	require.NoError(t, err)
	b, err := LoadRules("/b")
	require.NoError(t, err)
	require.Len(t, a, 1)
	require.Len(t, b, 1)
	assert.Equal(t, "Read", a[0].Pattern)
	assert.Equal(t, "Write", b[0].Pattern)
}

func TestSaveRulesAtomicOverwrite(t *testing.T) {
	withTempStore(t)
	project := "/home/user/overwrite"

	require.NoError(t, SaveRules(project, []Rule{{Pattern: "Read"}}))
	require.NoError(t, SaveRules(project, []Rule{{Pattern: "Write"}, {Pattern: "Edit"}}))

	loaded, err := LoadRules(project)
	require.NoError(t, err)
	require.Len(t, loaded, 2)
	assert.Equal(t, "Write", loaded[0].Pattern)
	assert.Equal(t, "Edit", loaded[1].Pattern)
}

func TestLoadRulesCorruptFileReturnsError(t *testing.T) {
	dir := withTempStore(t)
	project := "/home/user/corrupt"

	// Save valid first so we know the filename, then corrupt it.
	require.NoError(t, SaveRules(project, []Rule{{Pattern: "Read"}}))
	target := ruleFilePath(dir, project)
	require.NoError(t, os.WriteFile(target, []byte("not json{{{"), 0o600))

	_, err := LoadRules(project)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode rules file")
}
