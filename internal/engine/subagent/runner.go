package subagent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Executor is the function signature for running a subagent's prompt against an engine.
type Executor func(ctx context.Context, prompt string, agentType AgentType) (string, error)

// ContextExecutor is like Executor but receives conversation state to restore
// before sending the prompt. Used by /fork for full context inheritance.
type ContextExecutor func(ctx context.Context, prompt string, agentType AgentType, state *ConversationState) (string, error)

// ConversationState is a lightweight wrapper re-exported from the engine
// portability layer so the subagent package can reference it without importing
// engine (which would create a cycle). The actual serialization lives in
// engine.ConversationState; callers pass a pointer through this alias.
type ConversationState struct {
	Messages     []PortableMessage `json:"messages"`
	SystemPrompt string            `json:"system_prompt"`
	Model        string            `json:"model"`
	Engine       string            `json:"engine"`
}

// PortableMessage mirrors engine.PortableMessage for cycle-free usage.
type PortableMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Runner manages subagent goroutines.
type Runner struct {
	agents map[string]*RunningAgent
	mu     sync.RWMutex
}

// RunningAgent tracks the state of an in-flight subagent.
type RunningAgent struct {
	ID          string
	Name        string
	Type        string
	Status      string // running, completed, failed, killed
	StartedAt   time.Time
	CompletedAt time.Time // zero value if still running
	Cancel      context.CancelFunc
	Result      *TaskResult
	Done        chan struct{}
	Inbox       chan string // messages from SendMessage tool
}

// NewRunner creates a Runner.
func NewRunner() *Runner {
	return &Runner{agents: make(map[string]*RunningAgent)}
}

// Spawn creates a new subagent goroutine. For sync agents it blocks until completion.
// For async agents (RunInBG=true) it returns immediately with the agent ID.
func (r *Runner) Spawn(ctx context.Context, input TaskInput, agentType AgentType, executor Executor) (string, error) {
	r.reapCompleted()

	agentID := "agent-" + uuid.New().String()[:8]
	ctx, cancel := context.WithCancel(ctx) //nolint:gosec // cancel stored in agent.Cancel, called by Kill()

	agent := &RunningAgent{
		ID:        agentID,
		Name:      input.Name,
		Type:      input.SubagentType,
		Status:    "running",
		StartedAt: time.Now(),
		Cancel:    cancel,
		Done:      make(chan struct{}),
		Inbox:     make(chan string, 16),
	}

	r.mu.Lock()
	r.agents[agentID] = agent
	r.mu.Unlock()

	if input.RunInBG {
		go r.runAgent(ctx, agent, input, agentType, executor)
		return agentID, nil
	}

	// Sync: run and wait.
	r.runAgent(ctx, agent, input, agentType, executor)
	return agentID, nil
}

// SpawnWithContext creates a new subagent with full conversation context inherited
// from the parent. The ContextExecutor restores history before running the prompt.
func (r *Runner) SpawnWithContext(ctx context.Context, input TaskInput, agentType AgentType, executor ContextExecutor, state *ConversationState) (string, error) {
	agentID := "agent-" + uuid.New().String()[:8]
	ctx, cancel := context.WithCancel(ctx) //nolint:gosec // cancel stored in agent.Cancel, called by Kill()

	agent := &RunningAgent{
		ID:        agentID,
		Name:      input.Name,
		Type:      input.SubagentType,
		Status:    "running",
		StartedAt: time.Now(),
		Cancel:    cancel,
		Done:      make(chan struct{}),
		Inbox:     make(chan string, 16),
	}

	r.mu.Lock()
	r.agents[agentID] = agent
	r.mu.Unlock()

	run := func() {
		defer close(agent.Done)
		start := time.Now()

		result, err := executor(ctx, input.Prompt, agentType, state)

		r.mu.Lock()
		defer r.mu.Unlock()

		now := time.Now()

		if ctx.Err() != nil && agent.Status == "killed" {
			if agent.Result == nil {
				agent.Result = &TaskResult{
					AgentID:    agent.ID,
					Status:     "killed",
					Result:     "agent was killed",
					DurationMS: time.Since(start).Milliseconds(),
				}
			}
			agent.CompletedAt = now
			return
		}

		if err != nil {
			agent.Status = "failed"
			agent.Result = &TaskResult{
				AgentID:    agent.ID,
				Status:     "failed",
				Result:     err.Error(),
				DurationMS: time.Since(start).Milliseconds(),
			}
		} else {
			agent.Status = "completed"
			agent.Result = &TaskResult{
				AgentID:    agent.ID,
				Status:     "completed",
				Result:     result,
				DurationMS: time.Since(start).Milliseconds(),
			}
		}
		agent.CompletedAt = now
	}

	if input.RunInBG {
		go run()
		return agentID, nil
	}

	run()
	return agentID, nil
}

// SendTo delivers a message to a running agent's inbox.
func (r *Runner) SendTo(agentID, message string) error {
	r.mu.RLock()
	agent, ok := r.agents[agentID]
	r.mu.RUnlock()

	if !ok {
		return fmt.Errorf("agent %s not found", agentID)
	}
	if agent.Status != "running" {
		return fmt.Errorf("agent %s is not running (status: %s)", agentID, agent.Status)
	}

	select {
	case agent.Inbox <- message:
		return nil
	default:
		return fmt.Errorf("agent %s inbox is full", agentID)
	}
}

// FindByName returns the first agent with a matching name, or nil.
func (r *Runner) FindByName(name string) *RunningAgent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, a := range r.agents {
		if a.Name == name {
			return a
		}
	}
	return nil
}

func (r *Runner) runAgent(ctx context.Context, agent *RunningAgent, input TaskInput, agentType AgentType, executor Executor) {
	defer close(agent.Done)
	start := time.Now()

	result, err := executor(ctx, input.Prompt, agentType)

	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()

	if ctx.Err() != nil && agent.Status == "killed" {
		// Already killed, preserve killed status.
		if agent.Result == nil {
			agent.Result = &TaskResult{
				AgentID:    agent.ID,
				Status:     "killed",
				Result:     "agent was killed",
				DurationMS: time.Since(start).Milliseconds(),
			}
		}
		agent.CompletedAt = now
		return
	}

	if err != nil {
		agent.Status = "failed"
		agent.Result = &TaskResult{
			AgentID:    agent.ID,
			Status:     "failed",
			Result:     err.Error(),
			DurationMS: time.Since(start).Milliseconds(),
		}
	} else {
		agent.Status = "completed"
		agent.Result = &TaskResult{
			AgentID:    agent.ID,
			Status:     "completed",
			Result:     result,
			DurationMS: time.Since(start).Milliseconds(),
		}
	}
	agent.CompletedAt = now
}

// Kill stops a running agent by cancelling its context.
func (r *Runner) Kill(agentID string) error {
	r.mu.Lock()
	agent, ok := r.agents[agentID]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("agent %s not found", agentID)
	}
	if agent.Status != "running" {
		r.mu.Unlock()
		return fmt.Errorf("agent %s is not running (status: %s)", agentID, agent.Status)
	}
	agent.Status = "killed"
	agent.Result = &TaskResult{
		AgentID:    agentID,
		Status:     "killed",
		Result:     "agent was killed by coordinator",
		DurationMS: time.Since(agent.StartedAt).Milliseconds(),
	}
	r.mu.Unlock()

	agent.Cancel()
	return nil
}

// Get returns agent state by ID.
func (r *Runner) Get(agentID string) (*RunningAgent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.agents[agentID]
	return a, ok
}

// KillAll cancels all running agents and clears the map.
func (r *Runner) KillAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, agent := range r.agents {
		if agent.Status == "running" {
			agent.Status = "killed"
			agent.Result = &TaskResult{
				AgentID:    agent.ID,
				Status:     "killed",
				Result:     "killed by KillAll",
				DurationMS: time.Since(agent.StartedAt).Milliseconds(),
			}
			agent.CompletedAt = time.Now()
			agent.Cancel()
		}
	}
	r.agents = make(map[string]*RunningAgent)
}

// Close kills all agents and releases resources.
func (r *Runner) Close() {
	r.KillAll()
}

// reapCompleted removes agents from the map that completed more than 5 minutes ago.
// Called on Spawn and List to prevent unbounded map growth.
func (r *Runner) reapCompleted() {
	r.mu.Lock()
	defer r.mu.Unlock()

	cutoff := time.Now().Add(-5 * time.Minute)
	for id, agent := range r.agents {
		if agent.Status != "running" && !agent.CompletedAt.IsZero() && agent.CompletedAt.Before(cutoff) {
			delete(r.agents, id)
		}
	}
}

// List returns all agents (both running and completed).
func (r *Runner) List() []*RunningAgent {
	r.reapCompleted()

	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*RunningAgent, 0, len(r.agents))
	for _, a := range r.agents {
		out = append(out, a)
	}
	return out
}

// WaitFor blocks until the specified agent completes and returns its result.
// Returns nil if the agent is not found.
func (r *Runner) WaitFor(agentID string) *TaskResult {
	r.mu.RLock()
	agent, ok := r.agents[agentID]
	r.mu.RUnlock()
	if !ok {
		return nil
	}
	<-agent.Done

	r.mu.RLock()
	defer r.mu.RUnlock()
	return agent.Result
}
