package engine

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// ConversationState is the serialization unit for cross-engine handoff.
// Serialize from one engine, restore into another to switch mid-session.
type ConversationState struct {
	Messages     []PortableMessage  `json:"messages"`
	SystemPrompt string             `json:"system_prompt"`
	Model        string             `json:"model"`
	Engine       string             `json:"engine"`
	SessionID    string             `json:"session_id"`
	TokenCount   int                `json:"token_count"`
	FileState    map[string]int64   `json:"file_state"`
}

// PortableMessage is an engine-agnostic message representation.
// All engines must be able to consume and produce this format.
type PortableMessage struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	ToolInput  string `json:"tool_input,omitempty"`
	UUID       string `json:"uuid"`
}

// SerializeState captures the current engine state for handoff to another engine.
// It converts engine history into portable messages via RestoreHistory's inverse.
func SerializeState(e Engine, messages []RestoredMessage, systemPrompt, model, engineType string) (*ConversationState, error) {
	if e == nil {
		return nil, fmt.Errorf("cannot serialize nil engine")
	}

	portable := make([]PortableMessage, 0, len(messages))
	for _, m := range messages {
		pm := PortableMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
			ToolName:   m.ToolName,
			ToolInput:  m.ToolInput,
			UUID:       uuid.New().String(),
		}
		portable = append(portable, pm)
	}

	return &ConversationState{
		Messages:     portable,
		SystemPrompt: systemPrompt,
		Model:        model,
		Engine:       engineType,
		SessionID:    uuid.New().String(),
		FileState:    make(map[string]int64),
	}, nil
}

// RestoreState applies a ConversationState to an engine by converting portable
// messages back to RestoredMessage format and calling RestoreHistory on
// engines that implement HistoryRestorer. Engines without history injection
// accept the restore as a no-op so resumed sessions still load their UI
// history even if the model-side memory can't be rehydrated.
func RestoreState(e Engine, state *ConversationState) error {
	if e == nil {
		return fmt.Errorf("cannot restore into nil engine")
	}
	if state == nil {
		return fmt.Errorf("cannot restore nil state")
	}

	hr, ok := e.(HistoryRestorer)
	if !ok {
		// engine doesn't support history injection - UI state is still
		// repopulated by the caller, so this isn't an error.
		return nil
	}

	restored := make([]RestoredMessage, 0, len(state.Messages))
	for _, pm := range state.Messages {
		restored = append(restored, RestoredMessage{
			Role:       pm.Role,
			Content:    pm.Content,
			ToolCallID: pm.ToolCallID,
			ToolName:   pm.ToolName,
			ToolInput:  pm.ToolInput,
		})
	}

	return hr.RestoreHistory(restored)
}

// MarshalState serializes a ConversationState to JSON.
func MarshalState(state *ConversationState) ([]byte, error) {
	return json.Marshal(state)
}

// UnmarshalState deserializes a ConversationState from JSON.
func UnmarshalState(data []byte) (*ConversationState, error) {
	var state ConversationState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshal conversation state: %w", err)
	}
	return &state, nil
}
