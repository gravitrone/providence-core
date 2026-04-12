package subagent

// AgentType defines a reusable agent configuration that can be instantiated
// as a subagent. Built-in types, custom user types, and background agents
// all share this shape.
type AgentType struct {
	Name           string   `yaml:"name"`
	Description    string   `yaml:"description"`
	Tools          []string `yaml:"tools"`
	DisallowedTools []string `yaml:"disallowedTools"`
	Model          string   `yaml:"model"`
	Engine         string   `yaml:"engine"`
	Effort         string   `yaml:"effort"`
	MaxTurns       int      `yaml:"maxTurns"`
	PermissionMode string   `yaml:"permissionMode"`
	Background     bool     `yaml:"background"`
	Isolation      string   `yaml:"isolation"`
	SystemPrompt   string   `yaml:"-"`
}
