package direct

import (
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func TestExtractSessionMemory_BelowThreshold(t *testing.T) {
	e := &DirectEngine{
		history: NewConversationHistory(),
		workDir: "/tmp/test-project",
	}
	// Only 2 user turns - below threshold.
	e.history.AddUser("hello")
	e.history.AddUser("bye")

	got := e.extractSessionMemory()
	if got != "" {
		t.Errorf("expected empty for <5 turns, got %q", got)
	}
}

func TestExtractSessionMemory_AboveThreshold(t *testing.T) {
	e := &DirectEngine{
		history: NewConversationHistory(),
		workDir: "/tmp/test-project",
	}
	// 6 user turns.
	for i := 0; i < 6; i++ {
		e.history.AddUser("do something")
	}

	got := e.extractSessionMemory()
	if got == "" {
		t.Error("expected non-empty for 6 turns")
	}
	if !strings.Contains(got, "## Session") {
		t.Errorf("expected session header, got %q", got)
	}
}

func TestExtractSessionMemory_CapturesModifiedFiles(t *testing.T) {
	e := &DirectEngine{
		history: NewConversationHistory(),
		workDir: "/tmp/test-project",
	}
	// Add enough user turns.
	for i := 0; i < 5; i++ {
		e.history.AddUser("fix it")
	}

	// Add an assistant message with a Write tool call.
	e.history.mu.Lock()
	e.history.messages = append(e.history.messages, anthropic.NewAssistantMessage(
		anthropic.NewToolUseBlock("t1", map[string]any{
			"file_path": "/src/main.go",
			"content":   "package main",
		}, "Write"),
		anthropic.NewToolUseBlock("t2", map[string]any{
			"file_path":  "/src/util.go",
			"old_string": "foo",
			"new_string": "bar",
		}, "Edit"),
	))
	e.history.mu.Unlock()

	got := e.extractSessionMemory()
	if !strings.Contains(got, "/src/main.go") {
		t.Errorf("expected main.go in files modified, got %q", got)
	}
	if !strings.Contains(got, "/src/util.go") {
		t.Errorf("expected util.go in files modified, got %q", got)
	}
	if !strings.Contains(got, "Write(1)") {
		t.Errorf("expected Write(1) in tool counts, got %q", got)
	}
	if !strings.Contains(got, "Edit(1)") {
		t.Errorf("expected Edit(1) in tool counts, got %q", got)
	}
}

func TestExtractSessionMemory_CapturesDecisions(t *testing.T) {
	e := &DirectEngine{
		history: NewConversationHistory(),
		workDir: "/tmp/test-project",
	}
	e.history.AddUser("let's use postgres")
	e.history.AddUser("yes do it")
	e.history.AddUser("don't add tests")
	e.history.AddUser("something unrelated")
	e.history.AddUser("another message")

	got := e.extractSessionMemory()
	if !strings.Contains(got, "let's use postgres") {
		t.Errorf("expected decision captured, got %q", got)
	}
	if !strings.Contains(got, "yes do it") {
		t.Errorf("expected 'yes do it' captured, got %q", got)
	}
}

func TestProjectSlug(t *testing.T) {
	tests := []struct {
		input string
		want  string // just check it's non-empty and has no slashes
	}{
		{"/Users/test/Code/myproject", ""},  // won't match home
		{"/tmp/foo/bar", ""},                // won't match home either
	}
	for _, tt := range tests {
		got := projectSlug(tt.input)
		if strings.Contains(got, "/") {
			t.Errorf("slug should not contain slashes: %q", got)
		}
	}
}

func TestTruncateTitle(t *testing.T) {
	short := "hello world"
	if got := truncateTitle(short, 60); got != short {
		t.Errorf("expected %q, got %q", short, got)
	}

	long := strings.Repeat("word ", 20)
	got := truncateTitle(long, 30)
	if len(got) > 34 { // 30 + "..."
		t.Errorf("expected truncated, got len=%d: %q", len(got), got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("expected ... suffix, got %q", got)
	}
}

func TestIsDecision(t *testing.T) {
	yes := []string{"yes", "no thanks", "let's go", "don't do that", "skip it", "use redis"}
	for _, s := range yes {
		if !isDecision(s) {
			t.Errorf("expected %q to be decision", s)
		}
	}
	no := []string{"what is this", "how does it work", "show me the code"}
	for _, s := range no {
		if isDecision(s) {
			t.Errorf("expected %q to NOT be decision", s)
		}
	}
}
