package compact

import (
	"log"
	"strings"
)

// SessionMemoryReader returns the authoritative session memory string for the
// current session, or an empty string when no usable memory exists. Readers
// MUST NOT return stale memory; callers treat any returned string as live and
// trusted. An implementation error should be logged inside the reader and a
// miss returned, because the orchestrator falls back to raw-history
// compaction without memory.
type SessionMemoryReader func() (content string, err error)

// sessionMemoryAttachmentHeader wraps memory content as a clearly fenced
// system-level attachment that the main model can trust as the authoritative
// record of the session up to this point.
const sessionMemoryAttachmentHeader = "<session-memory>\n"
const sessionMemoryAttachmentFooter = "\n</session-memory>"

// buildMemoryAugmentedSummary prepends the session memory as a fenced block
// above the compactor-generated summary. The memory is authoritative; the
// summary covers gaps not captured in memory.
func buildMemoryAugmentedSummary(memory, summary string) string {
	memory = strings.TrimSpace(memory)
	summary = strings.TrimSpace(summary)

	if memory == "" {
		return summary
	}
	if summary == "" {
		return sessionMemoryAttachmentHeader + memory + sessionMemoryAttachmentFooter
	}
	return sessionMemoryAttachmentHeader + memory + sessionMemoryAttachmentFooter +
		"\n\n<context-summary>\n" + summary + "\n</context-summary>"
}

// SetMemoryReader wires a session-memory reader onto the orchestrator. When
// set, the orchestrator reads memory first at restore time and injects it as
// the authoritative prefix of the replacement summary. Passing nil clears the
// reader and disables memory-first restore.
func (o *Orchestrator) SetMemoryReader(reader SessionMemoryReader) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.memoryReader = reader
}

// readMemoryOrLog runs the configured memory reader. On error the error is
// logged and treated as a miss so the orchestrator falls through cleanly.
func (o *Orchestrator) readMemoryOrLog() string {
	o.mu.Lock()
	reader := o.memoryReader
	o.mu.Unlock()

	if reader == nil {
		return ""
	}
	content, err := reader()
	if err != nil {
		// Log and treat as a miss. Memory is best-effort by design.
		log.Printf("compact: read session memory failed: %v", err)
		return ""
	}
	return content
}
