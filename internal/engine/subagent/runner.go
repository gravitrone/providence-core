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

// Runner manages subagent goroutines.
type Runner struct {
	agents map[string]*RunningAgent
	mu     sync.RWMutex
}

// RunningAgent tracks the state of an in-flight subagent.
type RunningAgent struct {
	ID        string
	Name      string
	Type      string
	Status    string // running, completed, failed, killed
	StartedAt time.Time
	Cancel    context.CancelFunc
	Result    *TaskResult
	Done      chan struct{}
}

// NewRunner creates a Runner.
func NewRunner() *Runner {
	return &Runner{agents: make(map[string]*RunningAgent)}
}

// Spawn creates a new subagent goroutine. For sync agents it blocks until completion.
// For async agents (RunInBG=true) it returns immediately with the agent ID.
func (r *Runner) Spawn(ctx context.Context, input TaskInput, agentType AgentType, executor Executor) (string, error) {
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

func (r *Runner) runAgent(ctx context.Context, agent *RunningAgent, input TaskInput, agentType AgentType, executor Executor) {
	defer close(agent.Done)
	start := time.Now()

	result, err := executor(ctx, input.Prompt, agentType)

	r.mu.Lock()
	defer r.mu.Unlock()

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

// List returns all agents (both running and completed).
func (r *Runner) List() []*RunningAgent {
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
