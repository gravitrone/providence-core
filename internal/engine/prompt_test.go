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
	assert.Contains(t, prompt, "flame")
}

func TestBuildSystemBlocksReturnsBlocks(t *testing.T) {
	blocks := BuildSystemBlocks(nil)
	require.NotEmpty(t, blocks)
	assert.GreaterOrEqual(t, len(blocks), 1)
	assert.NotEmpty(t, blocks[0].Text)
}

func TestBuildSystemBlocksStaticCacheable(t *testing.T) {
	blocks := BuildSystemBlocks(nil)
	require.NotEmpty(t, blocks)
	// With nil config, all blocks should be static and cacheable.
	for i, block := range blocks {
		assert.True(t, block.Cacheable, "block %d should be cacheable", i)
		assert.NotEmpty(t, block.Text, "block %d should not be empty", i)
	}
}

func TestBuildSystemBlocksWithConfigHasDynamicBlocks(t *testing.T) {
	cfg := &PromptConfig{
		EnvInfo: &EnvInfo{
			CWD:       "/tmp/test",
			Platform:  "darwin",
			Shell:     "zsh",
			OSVersion: "Darwin 25.3.0",
			ModelName: "TestModel",
			ModelID:   "test-model-1",
			IsGitRepo: true,
		},
		Reminders: ReminderState{},
	}
	blocks := BuildSystemBlocks(cfg)
	require.NotEmpty(t, blocks)

	// Should have more blocks than nil config (dynamic ones added).
	nilBlocks := BuildSystemBlocks(nil)
	assert.Greater(t, len(blocks), len(nilBlocks), "config should add dynamic blocks")

	// Last blocks should include env context (not cacheable).
	hasEnv := false
	for _, b := range blocks {
		if strings.Contains(b.Text, "/tmp/test") {
			hasEnv = true
			assert.False(t, b.Cacheable, "env block should not be cacheable")
		}
	}
	assert.True(t, hasEnv, "should contain env context block")
}

func TestBuildSystemBlocksSection9StaticBlocks(t *testing.T) {
	blocks := BuildSystemBlocks(nil)
	// Should have exactly 10 static blocks (identity, system, actions, tools, coding, dev-discipline, output, git, ember, viz).
	assert.Equal(t, 10, len(blocks), "expected 10 static blocks, got %d", len(blocks))
}

func TestBuildSystemBlocksSectionOrder(t *testing.T) {
	blocks := BuildSystemBlocks(nil)
	require.Equal(t, 10, len(blocks))

	// Verify section content ordering.
	assert.Contains(t, blocks[0].Text, "Providence", "block 0: identity")
	assert.Contains(t, blocks[1].Text, "# System", "block 1: system framework")
	assert.Contains(t, blocks[2].Text, "reversibility", "block 2: action safety")
	assert.Contains(t, blocks[3].Text, "Using your tools", "block 3: tool usage")
	assert.Contains(t, blocks[4].Text, "Doing tasks", "block 4: coding guidelines")
	assert.Contains(t, blocks[5].Text, "Development discipline", "block 5.5: dev discipline")
	assert.Contains(t, blocks[6].Text, "Output efficiency", "block 6: output efficiency")
	assert.Contains(t, blocks[7].Text, "Git safety", "block 7: git safety")
	assert.Contains(t, blocks[8].Text, "Ember", "block 8: ember protocol")
	assert.Contains(t, blocks[9].Text, "providence-viz", "block 9: viz examples")
}

func TestBuildSystemPromptStillWorks(t *testing.T) {
	assert.NotEmpty(t, BuildSystemPrompt(nil))
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

func TestBuildSystemPromptContainsNewSections(t *testing.T) {
	prompt := BuildSystemPrompt(nil)

	// System framework section.
	assert.Contains(t, prompt, "permission mode")
	assert.Contains(t, prompt, "<system-reminder>")

	// Action safety section.
	assert.Contains(t, prompt, "reversibility")
	assert.Contains(t, prompt, "blast radius")

	// Tool usage section.
	assert.Contains(t, prompt, "Read a file: Read")
	assert.Contains(t, prompt, "Search file contents: Grep")

	// Coding guidelines extended.
	assert.Contains(t, prompt, "Read before you edit")

	// Output efficiency section.
	assert.Contains(t, prompt, "Go straight to the point")
	assert.Contains(t, prompt, "Never echo tool results")

	// Git safety section.
	assert.Contains(t, prompt, "Never edit git config")
	assert.Contains(t, prompt, "Never add Co-Authored-By")

	// Ember section (inactive).
	assert.Contains(t, prompt, "Ember autonomous mode is currently inactive")
}

func TestEmberActiveContent(t *testing.T) {
	cfg := &PromptConfig{EmberActive: true}
	blocks := BuildSystemBlocks(cfg)

	var emberText string
	for _, b := range blocks {
		if strings.Contains(b.Text, "Ember") {
			emberText = b.Text
			break
		}
	}
	require.NotEmpty(t, emberText)
	assert.Contains(t, emberText, "<tick>")
	assert.Contains(t, emberText, "Sleep tool")
	assert.NotContains(t, emberText, "currently inactive")
}

func TestEmberInactiveContent(t *testing.T) {
	blocks := BuildSystemBlocks(nil)

	var emberText string
	for _, b := range blocks {
		if strings.Contains(b.Text, "Ember") {
			emberText = b.Text
			break
		}
	}
	require.NotEmpty(t, emberText)
	assert.Contains(t, emberText, "currently inactive")
}

func TestFlattenBlocks(t *testing.T) {
	blocks := []SystemBlock{
		{Text: "A", Cacheable: true},
		{Text: "", Cacheable: false},
		{Text: "B", Cacheable: false},
	}
	assert.Equal(t, "A\n\nB", FlattenBlocks(blocks))
}

func TestFlattenBlocksEmpty(t *testing.T) {
	assert.Equal(t, "", FlattenBlocks(nil))
}

func TestEnvInfoFormat(t *testing.T) {
	env := &EnvInfo{
		CWD:       "/home/user/project",
		Platform:  "linux",
		Shell:     "/bin/bash",
		OSVersion: "Linux 6.6.4",
		ModelName: "Claude Sonnet 4.6",
		ModelID:   "claude-sonnet-4-6",
		IsGitRepo: true,
	}
	text := formatEnvInfo(env)
	assert.Contains(t, text, "/home/user/project")
	assert.Contains(t, text, "linux")
	assert.Contains(t, text, "/bin/bash")
	assert.Contains(t, text, "Claude Sonnet 4.6")
	assert.Contains(t, text, "Yes")
}

func TestOutputStyleInjection(t *testing.T) {
	cfg := &PromptConfig{
		OutputStyle:       "concise",
		OutputStylePrompt: "Be very concise in all responses.",
	}
	blocks := BuildSystemBlocks(cfg)

	hasStyle := false
	for _, b := range blocks {
		if strings.Contains(b.Text, "Output Style: concise") {
			hasStyle = true
			assert.False(t, b.Cacheable, "output style block should be dynamic")
		}
	}
	assert.True(t, hasStyle, "should contain output style block")
}

func TestMCPInstructionsInjection(t *testing.T) {
	cfg := &PromptConfig{
		MCPInstructions: "# MCP Server Instructions\n\n## filesystem\nUse this to read files.",
	}
	blocks := BuildSystemBlocks(cfg)

	hasMCP := false
	for _, b := range blocks {
		if strings.Contains(b.Text, "MCP Server Instructions") {
			hasMCP = true
			assert.False(t, b.Cacheable, "MCP instructions block should be dynamic")
			assert.Contains(t, b.Text, "filesystem")
		}
	}
	assert.True(t, hasMCP, "should contain MCP instructions block")
}

func TestMCPInstructionsEmptyOmitted(t *testing.T) {
	cfg := &PromptConfig{
		MCPInstructions: "",
	}
	blocks := BuildSystemBlocks(cfg)
	for _, b := range blocks {
		assert.NotContains(t, b.Text, "MCP Server Instructions")
	}
}

func TestNoCyberRiskInstruction(t *testing.T) {
	prompt := BuildSystemPrompt(nil)
	assert.NotContains(t, prompt, "CYBER_RISK")
	assert.NotContains(t, prompt, "cyberweapon")
}

func TestNoURLBan(t *testing.T) {
	prompt := BuildSystemPrompt(nil)
	assert.NotContains(t, prompt, "NEVER generate or guess URLs")
}

func TestNoCoAuthorTag(t *testing.T) {
	prompt := BuildSystemPrompt(nil)
	// Git safety section should prohibit co-author tags.
	assert.Contains(t, prompt, "Never add Co-Authored-By")
}
