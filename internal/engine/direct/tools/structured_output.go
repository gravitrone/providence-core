package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// StructuredOutputTool validates and stores structured JSON output for headless mode.
// Only meaningful in headless mode where downstream consumers need typed data.
type StructuredOutputTool struct {
	mu     sync.Mutex
	result map[string]any // last validated output
}

// NewStructuredOutputTool creates a StructuredOutputTool.
func NewStructuredOutputTool() *StructuredOutputTool {
	return &StructuredOutputTool{}
}

func (s *StructuredOutputTool) Name() string { return "StructuredOutput" }
func (s *StructuredOutputTool) Description() string {
	return "Output structured JSON data in headless mode. Validates data against the provided schema and stores it as the turn's structured result."
}
func (s *StructuredOutputTool) ReadOnly() bool { return false }

func (s *StructuredOutputTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"schema": map[string]any{
				"type":        "object",
				"description": "JSON Schema describing the expected output shape",
			},
			"data": map[string]any{
				"type":        "object",
				"description": "The structured data to output, must conform to the schema",
			},
		},
		"required": []string{"schema", "data"},
	}
}

// LastResult returns the most recent validated structured output, or nil.
func (s *StructuredOutputTool) LastResult() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.result == nil {
		return nil
	}
	// Return a copy to prevent external mutation.
	cp := make(map[string]any, len(s.result))
	for k, v := range s.result {
		cp[k] = v
	}
	return cp
}

// Execute validates data against the schema and stores the result.
func (s *StructuredOutputTool) Execute(_ context.Context, input map[string]any) ToolResult {
	schemaRaw, ok := input["schema"]
	if !ok {
		return ToolResult{Content: "schema is required", IsError: true}
	}
	schema, ok := schemaRaw.(map[string]any)
	if !ok {
		return ToolResult{Content: "schema must be a JSON object", IsError: true}
	}

	dataRaw, ok := input["data"]
	if !ok {
		return ToolResult{Content: "data is required", IsError: true}
	}
	data, ok := dataRaw.(map[string]any)
	if !ok {
		return ToolResult{Content: "data must be a JSON object", IsError: true}
	}

	// Validate required fields from schema if specified.
	if err := validateRequiredFields(schema, data); err != nil {
		return ToolResult{Content: fmt.Sprintf("validation failed: %s", err), IsError: true}
	}

	// Store the validated output.
	s.mu.Lock()
	s.result = data
	s.mu.Unlock()

	resp, _ := json.Marshal(map[string]any{
		"status": "stored",
		"fields": len(data),
	})
	return ToolResult{
		Content:  string(resp),
		Metadata: map[string]any{"structured_output": data},
	}
}

// validateRequiredFields checks that all required fields from the schema exist in data.
func validateRequiredFields(schema, data map[string]any) error {
	required, ok := schema["required"]
	if !ok {
		return nil
	}

	reqSlice, ok := required.([]any)
	if !ok {
		return nil
	}

	for _, r := range reqSlice {
		key, ok := r.(string)
		if !ok {
			continue
		}
		if _, exists := data[key]; !exists {
			return fmt.Errorf("missing required field: %s", key)
		}
	}

	return nil
}
