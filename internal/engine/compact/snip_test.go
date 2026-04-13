package compact

import (
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/assert"
)

func makeMessages(n int) []anthropic.MessageParam {
	msgs := make([]anthropic.MessageParam, n)
	for i := range msgs {
		if i%2 == 0 {
			msgs[i] = anthropic.NewUserMessage(anthropic.NewTextBlock("user msg"))
		} else {
			msgs[i] = anthropic.NewAssistantMessage(anthropic.NewTextBlock("assistant msg"))
		}
	}
	return msgs
}

func TestSnipOldMessagesUnderLimit(t *testing.T) {
	msgs := makeMessages(10)
	result := SnipOldMessages(msgs, 20)
	assert.Len(t, result, 10, "should not snip when under limit")
}

func TestSnipOldMessagesAtLimit(t *testing.T) {
	msgs := makeMessages(40) // 20 pairs = 40 messages
	result := SnipOldMessages(msgs, 20)
	assert.Len(t, result, 40, "should not snip when exactly at limit")
}

func TestSnipOldMessagesOverLimit(t *testing.T) {
	msgs := makeMessages(60) // 30 pairs = 60 messages
	result := SnipOldMessages(msgs, 20)
	assert.Len(t, result, 40, "should snip to 20 pairs = 40 messages")
	// Verify we kept the recent tail.
	assert.Equal(t, msgs[20:], result)
}

func TestSnipOldMessagesDefault(t *testing.T) {
	msgs := makeMessages(100)
	result := SnipOldMessages(msgs, 0) // 0 uses default (20 pairs = 40 messages)
	assert.Len(t, result, 40, "default should keep 20 pairs")
}

func TestSnipOldMessagesSmallKeep(t *testing.T) {
	msgs := makeMessages(20)
	result := SnipOldMessages(msgs, 5) // 5 pairs = 10 messages
	assert.Len(t, result, 10, "should snip to 5 pairs = 10 messages")
	assert.Equal(t, msgs[10:], result)
}
