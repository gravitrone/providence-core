package subagent

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Executor is the function signature for running a subagent's prompt against an engine.
type Executor func(ctx context.Context, prompt string, agentType AgentType) (string, error)

// ContextExecutor is like Executor but receives conversation state to restore
// before sending the prompt. Used by /fork for full context inheritance.
type ContextExecutor func(ctx context.Context, prompt string, agentType AgentType, state *ConversationState) (string, error)

// ConversationState is a lightweight wrapper around portable engine state.
// The actual serialization lives in the engine portability layer; callers pass
// a pointer through this alias so the subagent API stays cycle-free.
type ConversationState struct {
	Messages     []PortableMessage `json:"messages"`
	SystemPrompt string            `json:"system_prompt"`
	Model        string            `json:"model"`
	Engine       string            `json:"engine"`

	// CacheSafeSystemBlocks holds the parent's pre-built system prompt blocks.
	// When present, the child engine reuses these exact bytes instead of
	// rebuilding from SystemPrompt, ensuring the Anthropic API prompt cache
	// key matches across parent and child (near-zero extra input cost).
	CacheSafeSystemBlocks []SystemBlock `json:"-"`
}

// SystemBlock is a cycle-free mirror of engine.SystemBlock for subagent usage.
type SystemBlock struct {
	Text      string
	Cacheable bool
}

// PortableMessage mirrors engine.PortableMessage for cycle-free usage.
type PortableMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// WorktreeCallback is fired after worktree lifecycle events.
type WorktreeCallback func(event string, path string, branch string)

// Runner manages subagent goroutines.
type Runner struct {
	agents            map[string]*RunningAgent
	mu                sync.RWMutex
	WorkDir           string             // Current working directory (repo root for worktrees)
	WorktreeCallbacks []WorktreeCallback // Called on worktree create/remove
}

// RunningAgent tracks the state of an in-flight subagent.
type RunningAgent struct {
	ID             string
	Name           string
	Type           string
	Status         string // running, completed, failed, killed
	StartedAt      time.Time
	CompletedAt    time.Time // zero value if still running
	Cancel         context.CancelFunc
	Result         *TaskResult
	Done           chan struct{}
	Inbox          chan string // compatibility mirror for SendMessage observers/tests
	delivery       chan string
	WorktreePath   string // set when isolation=worktree
	WorktreeBranch string // set when isolation=worktree
	RepoRoot       string // original repo root for worktree cleanup
}

// --- Constructors ---

// NewRunner creates a Runner.
func NewRunner() *Runner {
	return &Runner{agents: make(map[string]*RunningAgent)}
}

// NewRunnerWithWorkDir creates a Runner with a working directory set for worktree support.
func NewRunnerWithWorkDir(workDir string) *Runner {
	return &Runner{
		agents:  make(map[string]*RunningAgent),
		WorkDir: workDir,
	}
}

// --- Public API ---

// Spawn creates a new subagent goroutine. For sync agents it blocks until completion.
// For async agents (RunInBG=true) it returns immediately with the agent ID.
func (r *Runner) Spawn(ctx context.Context, input TaskInput, agentType AgentType, executor Executor) (string, error) {
	r.reapCompleted()
	ctx, agent, input, agentType, err := r.prepareAgent(ctx, input, agentType)
	if err != nil {
		return "", err
	}
	r.addAgent(agent)

	if input.RunInBG {
		go r.runAgent(ctx, agent, input, agentType, executor)
		return agent.ID, nil
	}

	r.runAgent(ctx, agent, input, agentType, executor)
	return agent.ID, nil
}

// SpawnWithContext creates a new subagent with full conversation context inherited
// from the parent. The ContextExecutor restores history before running the prompt.
func (r *Runner) SpawnWithContext(ctx context.Context, input TaskInput, agentType AgentType, executor ContextExecutor, state *ConversationState) (string, error) {
	r.reapCompleted()
	ctx, agent, input, agentType, err := r.prepareAgent(ctx, input, agentType)
	if err != nil {
		return "", err
	}
	r.addAgent(agent)

	if input.RunInBG {
		go r.runAgentWithContext(ctx, agent, input, agentType, executor, state)
		return agent.ID, nil
	}

	r.runAgentWithContext(ctx, agent, input, agentType, executor, state)
	return agent.ID, nil
}

// SendTo delivers a message to a running agent's inbox.
func (r *Runner) SendTo(agentID, message string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	agent, ok := r.agents[agentID]
	if !ok {
		return fmt.Errorf("agent %s not found", agentID)
	}
	if agent.Status != "running" {
		return fmt.Errorf("agent %s is not running (status: %s)", agentID, agent.Status)
	}

	select {
	case agent.delivery <- message:
		select {
		case agent.Inbox <- message:
		default:
		}
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

// --- Internal ---

func (r *Runner) prepareAgent(ctx context.Context, input TaskInput, agentType AgentType) (context.Context, *RunningAgent, TaskInput, AgentType, error) {
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
		delivery:  make(chan string, 16),
	}

	if err := r.prepareWorktree(agent, &input, &agentType); err != nil {
		cancel()
		return nil, nil, input, agentType, err
	}

	r.injectAgentMemory(&input, &agentType)

	return ctx, agent, input, agentType, nil
}

// injectAgentMemory loads the three per-type memory scopes and appends them to
// the agent's system prompt as <agent-memory> blocks. The project root used for
// project and local scopes is the agent's effective working directory (worktree
// path when isolation=worktree, otherwise the runner's WorkDir).
func (r *Runner) injectAgentMemory(input *TaskInput, agentType *AgentType) {
	projectRoot := agentType.WorkDir
	if projectRoot == "" {
		projectRoot = r.WorkDir
	}
	agentType.SystemPrompt = InjectAgentMemory(agentType.SystemPrompt, input.SubagentType, projectRoot)
}

func (r *Runner) prepareWorktree(agent *RunningAgent, input *TaskInput, agentType *AgentType) error {
	isolation := input.Isolation
	if isolation == "" {
		isolation = agentType.Isolation
	}
	if isolation != "worktree" || r.WorkDir == "" {
		return nil
	}

	slug := Slugify(input.Name)
	if slug == "agent" && input.SubagentType != "" {
		slug = Slugify(input.SubagentType)
	}

	wtPath, wtBranch, err := CreateWorktree(r.WorkDir, slug)
	if err != nil {
		return fmt.Errorf("create worktree: %w", err)
	}

	agent.WorktreePath = wtPath
	agent.WorktreeBranch = wtBranch
	agent.RepoRoot = r.WorkDir

	agentType.WorkDir = wtPath

	notice := BuildWorktreeNotice(r.WorkDir, wtPath)
	input.Prompt = notice + "\n\n" + input.Prompt

	for _, cb := range r.WorktreeCallbacks {
		cb("create", wtPath, wtBranch)
	}

	return nil
}

func (r *Runner) addAgent(agent *RunningAgent) {
	r.mu.Lock()
	r.agents[agent.ID] = agent
	r.mu.Unlock()
}

func (r *Runner) runAgent(ctx context.Context, agent *RunningAgent, input TaskInput, agentType AgentType, executor Executor) {
	defer close(agent.Done)
	start := time.Now()

	result, err := r.runTurns(ctx, agent, input.Prompt, func(turnCtx context.Context, prompt string) (string, error) {
		return executor(turnCtx, prompt, agentType)
	})

	r.finishAgent(ctx, agent, start, result, err)
}

func (r *Runner) runAgentWithContext(ctx context.Context, agent *RunningAgent, input TaskInput, agentType AgentType, executor ContextExecutor, state *ConversationState) {
	defer close(agent.Done)
	start := time.Now()

	turnState := state
	result, err := r.runTurns(ctx, agent, input.Prompt, func(turnCtx context.Context, prompt string) (string, error) {
		return executor(turnCtx, prompt, agentType, turnState)
	})

	r.finishAgent(ctx, agent, start, result, err)
}

func (r *Runner) runTurns(ctx context.Context, agent *RunningAgent, prompt string, runTurn func(context.Context, string) (string, error)) (string, error) {
	pending := newInboxBuffer()
	drainerCtx, stopDrainer := context.WithCancel(ctx)
	waitForDrainer := r.startInboxDrainer(drainerCtx, agent, pending.add)
	defer func() {
		stopDrainer()
		waitForDrainer()
	}()

	nextPrompt := prompt
	lastResult := ""

	for {
		result, err := runTurn(ctx, nextPrompt)
		if err != nil {
			return "", err
		}

		lastResult = result
		messages := pending.takeAll()
		if len(messages) == 0 {
			return lastResult, nil
		}

		nextPrompt = buildInboxTurnPrompt(lastResult, messages)
	}
}

func (r *Runner) startInboxDrainer(ctx context.Context, agent *RunningAgent, onMessage func(string)) func() {
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-agent.delivery:
				if onMessage == nil {
					log.Printf("subagent: dropped inbox message for %s", agent.ID)
					continue
				}
				onMessage(msg)
			}
		}
	}()

	return wg.Wait
}

func (r *Runner) finishAgent(ctx context.Context, agent *RunningAgent, start time.Time, result string, err error) {
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
		r.cleanupWorktree(agent)
		r.drainInboxMirror(agent)
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
	r.cleanupWorktree(agent)
	r.drainInboxMirror(agent)
}

func buildInboxTurnPrompt(previousResult string, messages []string) string {
	prompt := "Continue your task. New inbox messages arrived while you were working.\n\n"
	if previousResult != "" {
		prompt += "<previous-turn-result>\n" + previousResult + "\n</previous-turn-result>\n\n"
	}

	prompt += "<inbox>\n"
	for _, msg := range messages {
		prompt += "- " + msg + "\n"
	}
	prompt += "</inbox>\n\n"
	prompt += "Address the inbox messages and keep going."

	return prompt
}

type inboxBuffer struct {
	mu       sync.Mutex
	messages []string
}

func newInboxBuffer() *inboxBuffer {
	return &inboxBuffer{}
}

func (b *inboxBuffer) add(message string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.messages = append(b.messages, message)
}

func (b *inboxBuffer) takeAll() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.messages) == 0 {
		return nil
	}

	out := make([]string, len(b.messages))
	copy(out, b.messages)
	b.messages = nil
	return out
}

func (r *Runner) drainInboxMirror(agent *RunningAgent) {
	for {
		select {
		case <-agent.Inbox:
		default:
			return
		}
	}
}

// cleanupWorktree checks for changes in the agent's worktree and handles cleanup.
// If changes exist, the worktree path and branch are included in the result for
// the coordinator to handle. If no changes, the worktree is auto-removed.
// Must be called with r.mu held.
func (r *Runner) cleanupWorktree(agent *RunningAgent) {
	if agent.WorktreePath == "" {
		return
	}

	if HasWorktreeChanges(agent.WorktreePath) {
		if agent.Result != nil {
			agent.Result.WorktreePath = agent.WorktreePath
			agent.Result.WorktreeBranch = agent.WorktreeBranch
		}
		return
	}

	_ = RemoveWorktree(agent.RepoRoot, agent.WorktreePath, agent.WorktreeBranch)

	for _, cb := range r.WorktreeCallbacks {
		cb("remove", agent.WorktreePath, agent.WorktreeBranch)
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
