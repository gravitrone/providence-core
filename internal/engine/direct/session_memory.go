package direct

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/gravitrone/providence-core/internal/engine/session"
	"github.com/gravitrone/providence-core/internal/engine/subagent"
)

// --- Session memory ---

// sessionMemoryWriterTimeout caps the fire-and-forget memory writer so it
// cannot hang a goroutine indefinitely if the child engine stalls.
const sessionMemoryWriterTimeout = 2 * time.Minute

// readSessionMemoryForCompactor is the compactor-facing adapter that reads the
// persisted session memory. A miss returns ("", nil); stale memory is logged
// and treated as a miss so the compactor's raw-history fall-through runs
// normally. The returned error is only set for unexpected read failures that
// callers may wish to surface, but the compactor treats any error as a miss.
func (e *DirectEngine) readSessionMemoryForCompactor() (string, error) {
	if !e.memoryEnabled {
		return "", nil
	}
	content, err := session.ReadSessionMemory(e.sessionID)
	if err != nil {
		if errors.Is(err, session.ErrMemoryStale) {
			log.Printf("direct: session memory for %s is stale, ignoring", e.sessionID)
			return "", nil
		}
		return "", fmt.Errorf("read session memory: %w", err)
	}
	return strings.TrimSpace(content), nil
}

// maybeDispatchSessionMemoryWriter increments the turn counter and, when the
// configured interval is hit, spawns a fire-and-forget fork subagent that
// summarizes the last N turns into a session memory file. All failures in the
// writer path are logged and swallowed; the main session must not be affected
// by memory write issues.
func (e *DirectEngine) maybeDispatchSessionMemoryWriter() {
	if !e.memoryEnabled {
		return
	}
	interval := e.memoryTurnInterval
	if interval <= 0 {
		interval = session.DefaultMemoryTurnInterval
	}

	count := atomic.AddInt64(&e.turnCount, 1)
	if count%int64(interval) != 0 {
		return
	}

	if e.subagentRunner == nil {
		return
	}
	executor := e.memoryWriterExecutor()
	if executor == nil {
		return
	}

	// Snapshot the current conversation window the writer will summarize.
	window := e.recentTurnsForMemory(interval)
	if strings.TrimSpace(window) == "" {
		return
	}

	prompt := buildSessionMemoryPrompt(interval, window)
	sessionID := e.sessionID

	e.memoryWritersInFlight.Add(1)
	go func() {
		defer e.memoryWritersInFlight.Done()
		// Independent context so the writer never inherits a cancelled turn
		// context. The writer is strictly best-effort.
		ctx, cancel := context.WithTimeout(context.Background(), sessionMemoryWriterTimeout)
		defer cancel()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("direct: session memory writer panicked: %v", r)
			}
		}()

		input := subagent.TaskInput{
			Description:  "write session memory",
			Prompt:       prompt,
			SubagentType: "default",
			Name:         "session-memory-writer",
			RunInBG:      false,
		}
		agentType := subagent.DefaultAgentType()
		agentType.MaxTurns = 1
		agentType.SystemPrompt = session.MemorySummarizationPrompt
		agentType.Tools = nil // no tools; this is a pure summarization turn
		agentType.Isolation = ""

		// SpawnWithContext blocks this goroutine until the subagent completes.
		// That is fine because we are already detached from the main turn.
		agentID, err := e.subagentRunner.SpawnWithContext(ctx, input, agentType, executor, nil)
		if err != nil {
			log.Printf("direct: dispatch session memory writer failed: %v", err)
			return
		}

		result := e.subagentRunner.WaitFor(agentID)
		if result == nil || result.Status != "completed" {
			log.Printf("direct: session memory writer did not complete cleanly (session %s)", sessionID)
			return
		}

		body := strings.TrimSpace(result.Result)
		if body == "" {
			return
		}

		if writeErr := session.WriteSessionMemory(sessionID, body); writeErr != nil {
			log.Printf("direct: persist session memory failed: %v", writeErr)
		}
	}()
}

// recentTurnsForMemory returns a plain-text rendering of the last `interval`
// user/assistant exchanges. Used as the writer's input window. Empty string
// means the engine is not running an anthropic-style history (codex /
// openrouter) and this writer is currently a no-op.
func (e *DirectEngine) recentTurnsForMemory(interval int) string {
	if e.codexMode || e.openrouterMode {
		return ""
	}
	if e.history == nil {
		return ""
	}

	msgs := e.history.Messages()
	// Take a window of roughly 2*interval messages (user+assistant pairs).
	window := interval * 2
	if window <= 0 || window > len(msgs) {
		window = len(msgs)
	}
	tail := msgs[len(msgs)-window:]

	var b strings.Builder
	for i, m := range tail {
		if i > 0 {
			b.WriteString("\n\n")
		}
		if m.Role == anthropic.MessageParamRoleAssistant {
			b.WriteString("ASSISTANT:\n")
		} else {
			b.WriteString("USER:\n")
		}
		for _, block := range m.Content {
			if block.OfText != nil {
				b.WriteString(strings.TrimSpace(block.OfText.Text))
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

// memoryWriterExecutor returns the context executor that should run the
// memory-writer fork subagent. The test override wins when set; otherwise
// the engine's own subagentContextExecutor method is used.
func (e *DirectEngine) memoryWriterExecutor() subagent.ContextExecutor {
	if e.memoryExecutorOverride != nil {
		return e.memoryExecutorOverride
	}
	return e.subagentContextExecutor
}

// buildSessionMemoryPrompt formats the writer's turn prompt. The fixed
// system prompt lives in session.MemorySummarizationPrompt; here we pass the
// window itself inside a clearly fenced block.
func buildSessionMemoryPrompt(interval int, window string) string {
	return fmt.Sprintf(
		"Summarize the following last-%d-turn transcript into the session memory markdown per your system instructions.\n\n<transcript>\n%s\n</transcript>",
		interval,
		strings.TrimSpace(window),
	)
}
