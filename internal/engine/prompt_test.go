package engine

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSystemPromptContainsIdentity(t *testing.T) {
	prompt := BuildSystemPrompt(nil)
	require.NotEmpty(t, prompt)
	assert.Contains(t, prompt, "Providence")
	assert.Contains(t, prompt, "Profaned")
}

func TestBuildSystemPromptContainsViz(t *testing.T) {
	prompt := BuildSystemPrompt(nil)
	assert.Contains(t, prompt, "providence-viz")
	assert.Contains(t, prompt, `"type": "bar"`)
	assert.Contains(t, prompt, `"type": "table"`)
	assert.Contains(t, prompt, `"type": "sparkline"`)
	assert.Contains(t, prompt, `"type": "tree"`)
	assert.Contains(t, prompt, `"type": "heatmap"`)
	assert.Contains(t, prompt, `"type": "timeline"`)
	assert.Contains(t, prompt, `"type": "stat"`)
	assert.Contains(t, prompt, `"type": "diff"`)
}

func TestBuildSystemPromptSourcesIgnored(t *testing.T) {
	withSources := BuildSystemPrompt([]string{"https://example.com"})
	withoutSources := BuildSystemPrompt(nil)
	assert.Equal(t, withSources, withoutSources)
}

func TestBuildSystemPromptIsNonEmpty(t *testing.T) {
	prompt := BuildSystemPrompt(nil)
	assert.Greater(t, len(prompt), 500, "prompt should be substantive")
}

func TestBuildSystemPromptIsDeterministic(t *testing.T) {
	assert.Equal(t, BuildSystemPrompt(nil), BuildSystemPrompt(nil))
}

func TestBuildSystemPromptTone(t *testing.T) {
	prompt := BuildSystemPrompt(nil)
	assert.True(t, strings.Contains(prompt, "flame") || strings.Contains(prompt, "fire"),
		"prompt should have flame/fire theme language")
}
