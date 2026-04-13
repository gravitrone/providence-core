package compact

import "github.com/anthropics/anthropic-sdk-go"

// DefaultKeepRecentPairs is the default number of user+assistant pairs
// to preserve when snipping old messages.
const DefaultKeepRecentPairs = 20

// SnipOldMessages removes old user+assistant pairs beyond keepRecent pairs
// (default 20 pairs = 40 messages). This runs BEFORE microcompact and
// autocompact as a cheap first pass to keep message count bounded.
//
// It preserves the most recent messages and drops the oldest ones.
// A keepRecent of 0 uses DefaultKeepRecentPairs.
func SnipOldMessages(messages []anthropic.MessageParam, keepRecent int) []anthropic.MessageParam {
	if keepRecent <= 0 {
		keepRecent = DefaultKeepRecentPairs
	}

	// Each "pair" is roughly 2 messages (user + assistant), so keep 2*keepRecent.
	keepCount := keepRecent * 2

	if len(messages) <= keepCount {
		return messages
	}

	// Keep only the most recent keepCount messages.
	return messages[len(messages)-keepCount:]
}
