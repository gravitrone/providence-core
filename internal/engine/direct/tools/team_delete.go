package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gravitrone/providence-core/internal/engine/teams"
)

// TeamDeleteTool allows the model to delete agent teams.
type TeamDeleteTool struct {
	store *teams.Store
}

// NewTeamDeleteTool creates a TeamDeleteTool backed by the given store.
func NewTeamDeleteTool(store *teams.Store) *TeamDeleteTool {
	return &TeamDeleteTool{store: store}
}

func (t *TeamDeleteTool) Name() string { return "TeamDelete" }
func (t *TeamDeleteTool) Description() string {
	return "Delete an agent team. Checks for active members before deletion - teams with active agents cannot be deleted."
}
func (t *TeamDeleteTool) ReadOnly() bool { return false }

func (t *TeamDeleteTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"team_name": map[string]any{
				"type":        "string",
				"description": "Name of the team to delete",
			},
		},
		"required": []string{"team_name"},
	}
}

// Execute deletes a team after checking for active members.
func (t *TeamDeleteTool) Execute(_ context.Context, input map[string]any) ToolResult {
	name := paramString(input, "team_name", "")
	if name == "" {
		return ToolResult{Content: "team_name is required", IsError: true}
	}

	// Load team to check for active members.
	team, err := t.store.Load(name)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("team not found: %v", err), IsError: true}
	}

	if team.HasActiveMember() {
		resp := map[string]any{
			"status":         "rejected",
			"reason":         "team has active members",
			"active_members": team.ActiveCount(),
			"team_name":      name,
		}
		raw, _ := json.Marshal(resp)
		return ToolResult{Content: string(raw), IsError: true}
	}

	if err := t.store.Delete(name); err != nil {
		return ToolResult{Content: fmt.Sprintf("failed to delete team: %v", err), IsError: true}
	}

	resp := map[string]any{
		"status":    "deleted",
		"team_name": name,
	}
	raw, _ := json.Marshal(resp)
	return ToolResult{Content: string(raw)}
}
