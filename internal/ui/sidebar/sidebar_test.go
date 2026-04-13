package sidebar

import (
	"testing"
	"time"

	"github.com/gravitrone/providence-core/internal/engine/subagent"
)

func TestNewSidebar(t *testing.T) {
	s := New()
	if s.HasAgents() {
		t.Fatal("new sidebar should have no agents")
	}
	if s.Focused {
		t.Fatal("new sidebar should not be focused")
	}
}

func TestFocusAndUnfocus(t *testing.T) {
	s := New()
	s.Agents = []AgentCard{{ID: "a1", Name: "test", Status: "running"}}

	s.FocusSidebar()
	if !s.Focused {
		t.Fatal("expected focused after FocusSidebar")
	}
	if s.Cursor != 0 {
		t.Fatalf("expected cursor=0, got %d", s.Cursor)
	}

	s.Unfocus()
	if s.Focused {
		t.Fatal("expected not focused after Unfocus")
	}
	if s.Expanded {
		t.Fatal("expected expanded=false after Unfocus")
	}
}

func TestHandleKeyNavigation(t *testing.T) {
	s := New()
	s.Agents = []AgentCard{
		{ID: "a1", Name: "first", Status: "running"},
		{ID: "a2", Name: "second", Status: "running"},
		{ID: "a3", Name: "third", Status: "completed"},
	}
	s.FocusSidebar()

	// Down wraps.
	s.HandleKey("j")
	if s.Cursor != 1 {
		t.Fatalf("expected cursor=1 after j, got %d", s.Cursor)
	}
	s.HandleKey("down")
	if s.Cursor != 2 {
		t.Fatalf("expected cursor=2 after down, got %d", s.Cursor)
	}
	s.HandleKey("j")
	if s.Cursor != 0 {
		t.Fatalf("expected cursor=0 after wrap, got %d", s.Cursor)
	}

	// Up wraps.
	s.HandleKey("k")
	if s.Cursor != 2 {
		t.Fatalf("expected cursor=2 after k wrap, got %d", s.Cursor)
	}
	s.HandleKey("up")
	if s.Cursor != 1 {
		t.Fatalf("expected cursor=1 after up, got %d", s.Cursor)
	}
}

func TestHandleKeyUnfocus(t *testing.T) {
	s := New()
	s.Agents = []AgentCard{{ID: "a1", Status: "running"}}
	s.FocusSidebar()

	for _, key := range []string{"right", "esc", "q"} {
		s.FocusSidebar()
		action := s.HandleKey(key)
		if action != "unfocus" {
			t.Fatalf("expected unfocus for key %q, got %q", key, action)
		}
		if s.Focused {
			t.Fatalf("expected focused=false after %q", key)
		}
	}
}

func TestHandleKeyExpand(t *testing.T) {
	s := New()
	s.Agents = []AgentCard{{ID: "a1", Status: "completed"}}
	s.FocusSidebar()

	action := s.HandleKey("enter")
	if action != "expand" {
		t.Fatalf("expected expand, got %q", action)
	}
	if !s.Expanded {
		t.Fatal("expected expanded=true")
	}

	// Esc collapses detail view.
	s.HandleKey("esc")
	if s.Expanded {
		t.Fatal("expected expanded=false after esc in detail")
	}
}

func TestHandleKeyKill(t *testing.T) {
	s := New()
	s.Agents = []AgentCard{{ID: "a1", Status: "running"}}
	s.FocusSidebar()

	action := s.HandleKey("x")
	if action != "kill" {
		t.Fatalf("expected kill, got %q", action)
	}

	// Kill on completed agent does nothing.
	s.Agents[0].Status = "completed"
	action = s.HandleKey("x")
	if action != "" {
		t.Fatalf("expected empty action for kill on completed, got %q", action)
	}
}

func TestHandleKeySend(t *testing.T) {
	s := New()
	s.Agents = []AgentCard{{ID: "a1", Status: "running"}}
	s.FocusSidebar()

	action := s.HandleKey("s")
	if action != "" {
		t.Fatalf("expected empty (opens input), got %q", action)
	}
	if !s.SendActive {
		t.Fatal("expected SendActive=true after s")
	}

	// Type a message.
	s.HandleKey("h")
	s.HandleKey("i")
	if s.SendBuffer != "hi" {
		t.Fatalf("expected buffer=hi, got %q", s.SendBuffer)
	}

	// Backspace.
	s.HandleKey("backspace")
	if s.SendBuffer != "h" {
		t.Fatalf("expected buffer=h after backspace, got %q", s.SendBuffer)
	}

	// Enter sends.
	s.HandleKey("i")
	action = s.HandleKey("enter")
	if action != "send" {
		t.Fatalf("expected send, got %q", action)
	}

	msg := s.SendMessage()
	if msg != "hi" {
		t.Fatalf("expected message=hi, got %q", msg)
	}
}

func TestHandleKeySendEscape(t *testing.T) {
	s := New()
	s.Agents = []AgentCard{{ID: "a1", Status: "running"}}
	s.FocusSidebar()
	s.HandleKey("s")
	s.HandleKey("a")

	action := s.HandleKey("esc")
	if action != "" {
		t.Fatalf("expected empty on esc, got %q", action)
	}
	if s.SendActive {
		t.Fatal("expected SendActive=false after esc")
	}
	if s.SendBuffer != "" {
		t.Fatalf("expected empty buffer after esc, got %q", s.SendBuffer)
	}
}

func TestSyncFromRunner(t *testing.T) {
	s := New()

	agents := []*subagent.RunningAgent{
		{ID: "a1", Name: "explorer", Status: "running", StartedAt: time.Now()},
		{ID: "a2", Name: "reviewer", Status: "completed", StartedAt: time.Now(), CompletedAt: time.Now(),
			Result: &subagent.TaskResult{Status: "completed", Result: "ok", TotalTokens: 500, ToolUses: 3}},
	}

	s.Sync(agents)
	if len(s.Agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(s.Agents))
	}
	if s.Agents[0].Name != "explorer" {
		t.Fatalf("expected explorer, got %s", s.Agents[0].Name)
	}
	if s.Agents[1].Tokens != 500 {
		t.Fatalf("expected 500 tokens, got %d", s.Agents[1].Tokens)
	}
}

func TestTickEviction(t *testing.T) {
	s := New()
	past := time.Now().Add(-2 * time.Minute)
	s.Agents = []AgentCard{
		{ID: "a1", Status: "completed", Started: past, Completed: past},
		{ID: "a2", Status: "running", Started: time.Now()},
	}

	s.Tick()
	if len(s.Agents) != 1 {
		t.Fatalf("expected 1 agent after eviction, got %d", len(s.Agents))
	}
	if s.Agents[0].ID != "a2" {
		t.Fatalf("expected a2 to remain, got %s", s.Agents[0].ID)
	}
}

func TestTickEvictionProtectsViewed(t *testing.T) {
	s := New()
	past := time.Now().Add(-2 * time.Minute)
	s.Agents = []AgentCard{
		{ID: "a1", Status: "completed", Started: past, Completed: past},
	}
	s.Focused = true
	s.Cursor = 0

	s.Tick()
	if len(s.Agents) != 1 {
		t.Fatalf("expected viewed agent to be protected, got %d agents", len(s.Agents))
	}
}

func TestHandleKeyEmptyAgents(t *testing.T) {
	s := New()
	s.FocusSidebar()
	action := s.HandleKey("j")
	if action != "unfocus" {
		t.Fatalf("expected unfocus on empty, got %q", action)
	}
}

func TestViewRenders(t *testing.T) {
	s := New()
	s.Agents = []AgentCard{
		{ID: "a1", Name: "test-agent", Status: "running", Started: time.Now()},
	}
	v := s.View(40, 20, 0)
	if v == "" {
		t.Fatal("expected non-empty view")
	}
	if !containsText(v, "Agents") {
		t.Fatal("expected Agents header in view")
	}
	if !containsText(v, "test-agent") {
		t.Fatal("expected agent name in view")
	}
}

func TestDetailRenders(t *testing.T) {
	agent := AgentCard{
		ID:     "a1",
		Name:   "detail-agent",
		Model:  "opus",
		Type:   "explorer",
		Status: "completed",
		Result: "found 10 files",
		ToolCalls: []ToolEntry{
			{Name: "Read", Args: "main.go", Status: "success"},
			{Name: "Bash", Args: "ls", Status: "error"},
		},
		Tokens:    1500,
		ToolCount: 2,
		Started:   time.Now().Add(-10 * time.Second),
		Completed: time.Now(),
	}

	colors := detailColors{
		PrimaryHex:   "#FFA600",
		SecondaryHex: "#D77757",
		MutedHex:     "#6b5040",
		TextHex:      "#e0d0c0",
		BorderHex:    "#3a2518",
		SuccessHex:   "#19FA19",
		ErrorHex:     "#ff5555",
	}

	v := RenderDetail(agent, 60, 30, 0, 0, colors)
	if v == "" {
		t.Fatal("expected non-empty detail view")
	}
	if !containsText(v, "detail-agent") {
		t.Fatal("expected agent name in detail")
	}
	if !containsText(v, "Read") {
		t.Fatal("expected tool name in detail")
	}
}

func containsText(rendered, text string) bool {
	// Strip ANSI escape codes for simple text matching.
	plain := stripAnsi(rendered)
	return len(plain) > 0 && contains(plain, text)
}

func stripAnsi(s string) string {
	var result []byte
	inEscape := false
	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z') {
				inEscape = false
			}
			continue
		}
		result = append(result, s[i])
	}
	return string(result)
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || findSubstring(s, sub))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
