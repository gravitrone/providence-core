package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gravitrone/providence-core/internal/engine/teams"
)

// TeamCreateTool allows the model to create agent teams.
type TeamCreateTool struct {
	store *teams.Store
}

// NewTeamCreateTool creates a TeamCreateTool backed by the given store.
func NewTeamCreateTool(store *teams.Store) *TeamCreateTool {
	return &TeamCreateTool{store: store}
}

func (t *TeamCreateTool) Name() string { return "TeamCreate" }
func (t *TeamCreateTool) Description() string {
	return "Create a named team of agents with a shared task list and file-based mailbox. Teams coordinate multiple agents for complex multi-step workflows."
}
func (t *TeamCreateTool) ReadOnly() bool { return false }

func (t *TeamCreateTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"team_name": map[string]any{
				"type":        "string",
				"description": "Name for the team (alphanumeric, hyphens, underscores)",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "Brief description of the team's purpose",
			},
		},
		"required": []string{"team_name"},
	}
}

// Prompt implements ToolPrompter.
func (t *TeamCreateTool) Prompt() string {
	return `Create a team for coordinating multiple agents on a shared task. Teams have:
- A shared task list directory for file-based coordination
- Per-member inboxes for asynchronous messaging
- A team config persisted at ~/.claude/teams/{name}/config.json

Use TeamCreate when you need multiple agents working together on a complex task. After creating the team, spawn agents and assign them as members.`
}

// Execute creates a new team.
func (t *TeamCreateTool) Execute(_ context.Context, input map[string]any) ToolResult {
	name := paramString(input, "team_name", "")
	if name == "" {
		return ToolResult{Content: "team_name is required", IsError: true}
	}

	description := paramString(input, "description", "")

	team, err := t.store.CreateTeam(name, description)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("failed to create team: %v", err), IsError: true}
	}

	resp := map[string]any{
		"status":        "created",
		"team_name":     team.Name,
		"description":   team.Description,
		"created_at":    team.CreatedAt.Format(time.RFC3339),
		"task_list_dir": team.TaskListDir,
	}
	raw, _ := json.Marshal(resp)
	return ToolResult{Content: string(raw)}
}
