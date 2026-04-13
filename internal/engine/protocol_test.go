package engine

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUsageUpdateEventJSON(t *testing.T) {
	evt := UsageUpdateEvent{
		Type:              "usage",
		InputTokens:       1000,
		OutputTokens:      500,
		TotalTokens:       1500,
		CacheReadTokens:   200,
		CacheCreateTokens: 100,
	}

	data, err := json.Marshal(evt)
	require.NoError(t, err)

	var restored UsageUpdateEvent
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	assert.Equal(t, evt, restored)
}

func TestUsageUpdateEventJSON_OmitEmpty(t *testing.T) {
	evt := UsageUpdateEvent{
		Type:         "usage",
		InputTokens:  500,
		OutputTokens: 200,
		TotalTokens:  700,
	}

	data, err := json.Marshal(evt)
	require.NoError(t, err)

	// cache fields omitted when zero
	assert.NotContains(t, string(data), "cache_read_tokens")
	assert.NotContains(t, string(data), "cache_create_tokens")
}

func TestCompactionEventJSON(t *testing.T) {
	evt := CompactionEvent{
		Type:         "compaction",
		Phase:        "completed",
		TokensBefore: 50000,
		TokensAfter:  12000,
		Err:          nil,
	}

	// CompactionEvent has no json tags, so marshal by field name.
	data, err := json.Marshal(evt)
	require.NoError(t, err)

	var restored CompactionEvent
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	assert.Equal(t, evt.Type, restored.Type)
	assert.Equal(t, evt.Phase, restored.Phase)
	assert.Equal(t, evt.TokensBefore, restored.TokensBefore)
	assert.Equal(t, evt.TokensAfter, restored.TokensAfter)
}

func TestTombstoneEventJSON(t *testing.T) {
	evt := TombstoneEvent{
		Type:         "tombstone",
		MessageIndex: 7,
	}

	data, err := json.Marshal(evt)
	require.NoError(t, err)

	var restored TombstoneEvent
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	assert.Equal(t, evt, restored)
}

func TestSystemMessageEventJSON(t *testing.T) {
	evt := SystemMessageEvent{
		Type:    "system_message",
		Content: "Context compacted to 12k tokens",
	}

	data, err := json.Marshal(evt)
	require.NoError(t, err)

	var restored SystemMessageEvent
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	assert.Equal(t, evt, restored)
}

func TestToolResultEventJSON(t *testing.T) {
	evt := ToolResultEvent{
		Type:       "tool_result",
		ToolCallID: "tc_99",
		ToolName:   "Bash",
		Output:     "hello world",
		IsError:    false,
	}

	data, err := json.Marshal(evt)
	require.NoError(t, err)

	var restored ToolResultEvent
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	assert.Equal(t, evt, restored)
}

func TestResultEventJSON(t *testing.T) {
	evt := ResultEvent{
		Type:         "result",
		Subtype:      "success",
		Result:       "Done",
		SessionID:    "sess-42",
		TotalCostUSD: 0.0123,
		IsError:      false,
	}

	data, err := json.Marshal(evt)
	require.NoError(t, err)

	var restored ResultEvent
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	assert.Equal(t, evt, restored)
}
