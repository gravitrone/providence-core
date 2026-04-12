package picker

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFuzzyScore_SubsequenceMatch(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		target   string
		positive bool
	}{
		{"exact match", "main.go", "main.go", true},
		{"subsequence", "mgo", "main.go", true},
		{"prefix", "mai", "main.go", true},
		{"path match", "uimg", "internal/ui/image.go", true},
		{"no match", "xyz", "main.go", false},
		{"query longer than target", "main.go.extra", "main.go", false},
		{"empty query", "", "main.go", true},
		{"case matches lowercase", "readme", "readme.md", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := fuzzyScore(tt.query, tt.target)
			if tt.positive {
				assert.Greater(t, score, 0, "expected positive score for %q in %q", tt.query, tt.target)
			} else {
				assert.Equal(t, 0, score, "expected zero score for %q in %q", tt.query, tt.target)
			}
		})
	}
}

func TestFuzzyScore_ConsecutiveBonus(t *testing.T) {
	// "main" in "main.go" should score higher than "main" in "m_a_i_n.go"
	// because consecutive matches get bonus points.
	exact := fuzzyScore("main", "main.go")
	spread := fuzzyScore("main", "m_a_i_n.go")
	assert.Greater(t, exact, spread, "consecutive matches should score higher")
}

func TestFuzzyScore_ShorterPathBonus(t *testing.T) {
	short := fuzzyScore("ag", "ag.go")
	long := fuzzyScore("ag", "some/very/deep/path/agent.go")
	assert.Greater(t, short, long, "shorter paths should score higher for same query")
}

func TestFilePickerModel_HandleInput_DetectsAt(t *testing.T) {
	m := NewFilePickerModel("/tmp", 80)
	m.files = []string{"main.go", "internal/ui/app.go", "README.md"}

	m.HandleInput("look at @mai")
	assert.True(t, m.Active(), "should be active after @")
	assert.Equal(t, "mai", m.Query())
	assert.Equal(t, 8, m.TokenStart(), "@ is at byte offset 8")
}

func TestFilePickerModel_HandleInput_AtStart(t *testing.T) {
	m := NewFilePickerModel("/tmp", 80)
	m.files = []string{"main.go"}

	m.HandleInput("@m")
	assert.True(t, m.Active())
	assert.Equal(t, "m", m.Query())
	assert.Equal(t, 0, m.TokenStart())
}

func TestFilePickerModel_HandleInput_NoAt(t *testing.T) {
	m := NewFilePickerModel("/tmp", 80)
	m.files = []string{"main.go"}

	m.HandleInput("just regular text")
	assert.False(t, m.Active())
}

func TestFilePickerModel_HandleInput_AtNotAfterWhitespace(t *testing.T) {
	m := NewFilePickerModel("/tmp", 80)
	m.files = []string{"main.go"}

	// @ in the middle of a word should not trigger.
	m.HandleInput("email@example.com")
	assert.False(t, m.Active())
}

func TestFilePickerFilter_QueryFiltersFiles(t *testing.T) {
	m := NewFilePickerModel("/tmp", 80)
	m.files = []string{
		"main.go",
		"internal/ui/app.go",
		"internal/ui/image.go",
		"internal/engine/claude/session.go",
		"README.md",
	}

	m.HandleInput("@img")
	require.True(t, m.Active())

	filtered := m.Filtered()
	require.NotEmpty(t, filtered)
	// "image.go" should be in results since "img" is a subsequence of "image".
	found := false
	for _, f := range filtered {
		if strings.Contains(f, "image") {
			found = true
			break
		}
	}
	assert.True(t, found, "image.go should appear in filtered results for query 'img'")
}

func TestFilePickerFilter_MaxResults(t *testing.T) {
	m := NewFilePickerModel("/tmp", 80)
	// Create many files.
	for i := 0; i < 100; i++ {
		m.files = append(m.files, "file"+string(rune('a'+i%26))+".go")
	}

	m.HandleInput("@file")
	assert.LessOrEqual(t, len(m.Filtered()), maxResults, "should cap at maxResults")
}

func TestFilePickerAccept_ReplacesAtQuery(t *testing.T) {
	m := NewFilePickerModel("/tmp", 80)
	m.files = []string{"internal/ui/image.go", "main.go"}

	m.HandleInput("check @ima")
	require.True(t, m.Active())

	accepted, replacement := m.HandleKey("enter")
	require.True(t, accepted)
	assert.True(t, strings.HasPrefix(replacement, "@"), "replacement should start with @")
	assert.True(t, strings.HasSuffix(replacement, " "), "replacement should end with trailing space")
	assert.False(t, m.Active(), "picker should be dismissed after accept")
}

func TestFilePickerAccept_SpacesInPath(t *testing.T) {
	m := NewFilePickerModel("/tmp", 80)
	m.files = []string{"path with spaces/file.go"}

	m.HandleInput("@path")
	require.True(t, m.Active())

	accepted, replacement := m.HandleKey("enter")
	require.True(t, accepted)
	assert.Contains(t, replacement, `@"path with spaces/file.go"`, "paths with spaces should be quoted")
}

func TestFilePickerNavigation(t *testing.T) {
	m := NewFilePickerModel("/tmp", 80)
	m.files = []string{"a.go", "b.go", "c.go"}

	m.HandleInput("@")
	require.True(t, m.Active())
	assert.Equal(t, 0, m.Selected())

	m.HandleKey("down")
	assert.Equal(t, 1, m.Selected())

	m.HandleKey("down")
	assert.Equal(t, 2, m.Selected())

	// Wrap around.
	m.HandleKey("down")
	assert.Equal(t, 0, m.Selected())

	m.HandleKey("up")
	assert.Equal(t, 2, m.Selected())
}

func TestFilePickerDismiss(t *testing.T) {
	m := NewFilePickerModel("/tmp", 80)
	m.files = []string{"a.go"}

	m.HandleInput("@a")
	require.True(t, m.Active())

	accepted, _ := m.HandleKey("esc")
	assert.False(t, accepted)
	assert.False(t, m.Active())
}

func TestFilePickerAccept_EmptyFiltered(t *testing.T) {
	m := NewFilePickerModel("/tmp", 80)
	m.files = []string{"a.go"}

	m.HandleInput("@zzzzz")
	require.True(t, m.Active())
	require.Empty(t, m.Filtered())

	accepted, _ := m.HandleKey("enter")
	assert.False(t, accepted, "should not accept when no results")
}
