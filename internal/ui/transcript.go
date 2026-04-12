package ui

import (
	"strings"
)

// --- Focus Arbiter ---

// Focus identifies which sub-model owns keyboard input.
type Focus int

const (
	// FocusInput routes keys to the text input (default).
	FocusInput Focus = iota
	// FocusTranscript routes keys to the transcript scroll/freeze mode.
	FocusTranscript
	// FocusModal routes keys to an active modal/dialog.
	FocusModal
	// FocusDashboard routes keys to the dashboard panel.
	FocusDashboard
)

// --- Transcript Model ---

// TranscriptModel manages virtual-scrolled message rendering with height
// caching. Only messages visible in the viewport are rendered on each frame.
type TranscriptModel struct {
	messages      []ChatMessage
	renderedCache map[int]string // index -> rendered string
	heightCache   map[int]int    // index -> line count
	scrollTop     int            // row offset into rendered content
	contentHeight int            // total lines of all messages
	viewportH     int            // visible rows
	viewportW     int            // available width for rendering
	sticky        bool           // auto-scroll to bottom on new messages
	frozen        bool           // ctrl+o freeze mode
	searchQuery   string         // search in freeze mode
	searchHits    []int          // matching message indices
	searchIdx     int            // current search hit
	searchActive  bool           // whether the search input is open
}

// NewTranscriptModel creates a TranscriptModel with sensible defaults.
func NewTranscriptModel() TranscriptModel {
	return TranscriptModel{
		renderedCache: make(map[int]string),
		heightCache:   make(map[int]int),
		sticky:        true,
	}
}

// MessageCount returns the number of messages in the transcript.
func (t *TranscriptModel) MessageCount() int {
	return len(t.messages)
}

// Messages returns the message slice (read-only intent).
func (t *TranscriptModel) Messages() []ChatMessage {
	return t.messages
}

// SetMessages replaces the full message list and invalidates all caches.
func (t *TranscriptModel) SetMessages(msgs []ChatMessage) {
	t.messages = msgs
	t.InvalidateAll()
}

// AddMessage appends a message, caches nothing yet (rendered on demand),
// and auto-scrolls if sticky.
func (t *TranscriptModel) AddMessage(msg ChatMessage) {
	t.messages = append(t.messages, msg)
	// Don't pre-render; View() renders on demand. But update contentHeight
	// optimistically so sticky scroll works.
	if t.sticky {
		t.scrollToEnd()
	}
}

// UpdateMessage replaces the message at index i and invalidates its cache.
func (t *TranscriptModel) UpdateMessage(i int, msg ChatMessage) {
	if i < 0 || i >= len(t.messages) {
		return
	}
	t.messages[i] = msg
	t.InvalidateMessage(i)
}

// LastMessage returns a pointer to the last message, or nil if empty.
func (t *TranscriptModel) LastMessage() *ChatMessage {
	if len(t.messages) == 0 {
		return nil
	}
	return &t.messages[len(t.messages)-1]
}

// InvalidateMessage clears the cached render and height for a single message.
func (t *TranscriptModel) InvalidateMessage(i int) {
	delete(t.renderedCache, i)
	delete(t.heightCache, i)
	t.recomputeContentHeight()
}

// InvalidateAll clears all caches (e.g. on width change).
func (t *TranscriptModel) InvalidateAll() {
	t.renderedCache = make(map[int]string)
	t.heightCache = make(map[int]int)
	t.recomputeContentHeight()
}

// SetViewport updates viewport dimensions. If width changed, all caches
// are invalidated since line wrapping may differ.
func (t *TranscriptModel) SetViewport(w, h int) {
	if w != t.viewportW {
		t.viewportW = w
		t.viewportH = h
		t.InvalidateAll()
		return
	}
	t.viewportH = h
}

// ScrollBy adjusts scrollTop by delta lines. Positive = down, negative = up.
// Clears sticky when scrolling up.
func (t *TranscriptModel) ScrollBy(delta int) {
	t.scrollTop += delta
	t.clampScroll()
	if delta < 0 {
		t.sticky = false
	}
	// If we scroll to the very bottom, re-pin sticky.
	if t.scrollTop >= t.contentHeight-t.viewportH {
		t.sticky = true
	}
}

// ScrollToBottom re-pins sticky and jumps to the end.
func (t *TranscriptModel) ScrollToBottom() {
	t.sticky = true
	t.scrollToEnd()
}

// Sticky returns whether auto-scroll is active.
func (t *TranscriptModel) Sticky() bool {
	return t.sticky
}

// Frozen returns whether freeze mode is active.
func (t *TranscriptModel) Frozen() bool {
	return t.frozen
}

// SetFrozen sets or clears freeze mode.
func (t *TranscriptModel) SetFrozen(v bool) {
	t.frozen = v
	if !v {
		// Exiting freeze: clear search, re-pin sticky.
		t.searchQuery = ""
		t.searchHits = nil
		t.searchIdx = 0
		t.searchActive = false
		t.sticky = true
		t.scrollToEnd()
	}
}

// SearchActive returns whether the search input is open in freeze mode.
func (t *TranscriptModel) SearchActive() bool {
	return t.searchActive
}

// SetSearchActive opens or closes the search input in freeze mode.
func (t *TranscriptModel) SetSearchActive(v bool) {
	t.searchActive = v
	if !v {
		t.searchQuery = ""
		t.searchHits = nil
		t.searchIdx = 0
	}
}

// SearchQuery returns the current search query.
func (t *TranscriptModel) SearchQuery() string {
	return t.searchQuery
}

// SetSearchQuery updates the search query and recomputes hits.
func (t *TranscriptModel) SetSearchQuery(q string) {
	t.searchQuery = q
	t.recomputeSearchHits()
}

// SearchNext advances to the next search hit and scrolls to it.
func (t *TranscriptModel) SearchNext() {
	if len(t.searchHits) == 0 {
		return
	}
	t.searchIdx = (t.searchIdx + 1) % len(t.searchHits)
	t.scrollToMessage(t.searchHits[t.searchIdx])
}

// SearchPrev goes to the previous search hit and scrolls to it.
func (t *TranscriptModel) SearchPrev() {
	if len(t.searchHits) == 0 {
		return
	}
	t.searchIdx--
	if t.searchIdx < 0 {
		t.searchIdx = len(t.searchHits) - 1
	}
	t.scrollToMessage(t.searchHits[t.searchIdx])
}

// SearchHitCount returns the number of search matches.
func (t *TranscriptModel) SearchHitCount() int {
	return len(t.searchHits)
}

// SearchCurrentIdx returns the current search hit index (0-based).
func (t *TranscriptModel) SearchCurrentIdx() int {
	return t.searchIdx
}

// View renders only the messages visible within the current viewport.
// The renderFn callback renders a single message by index. This keeps
// the TranscriptModel decoupled from the actual rendering logic in AgentTab.
func (t *TranscriptModel) View(renderFn func(idx int) string) string {
	if len(t.messages) == 0 {
		return ""
	}

	// Ensure all heights are cached (render on demand).
	for i := range t.messages {
		if _, ok := t.heightCache[i]; !ok {
			rendered := renderFn(i)
			t.renderedCache[i] = rendered
			t.heightCache[i] = countLines(rendered)
		}
	}
	t.recomputeContentHeight()

	// If sticky, ensure we're at the bottom.
	if t.sticky {
		t.scrollToEnd()
	}

	// Find visible messages based on scrollTop and viewportH.
	var visible []string
	rowAccum := 0
	for i := range t.messages {
		h := t.heightCache[i]
		msgStart := rowAccum
		msgEnd := rowAccum + h

		// Message is visible if it overlaps the viewport window.
		vpEnd := t.scrollTop + t.viewportH
		if msgEnd > t.scrollTop && msgStart < vpEnd {
			// Ensure we have a cached render.
			if _, ok := t.renderedCache[i]; !ok {
				rendered := renderFn(i)
				t.renderedCache[i] = rendered
				t.heightCache[i] = countLines(rendered)
			}
			visible = append(visible, t.renderedCache[i])
		}

		rowAccum += h
	}

	return strings.Join(visible, "\n")
}

// VisibleCount returns how many messages overlap the current viewport.
// Useful for testing that virtual scroll actually limits rendering.
func (t *TranscriptModel) VisibleCount(renderFn func(idx int) string) int {
	if len(t.messages) == 0 {
		return 0
	}

	// Ensure all heights are cached.
	for i := range t.messages {
		if _, ok := t.heightCache[i]; !ok {
			rendered := renderFn(i)
			t.renderedCache[i] = rendered
			t.heightCache[i] = countLines(rendered)
		}
	}
	t.recomputeContentHeight()

	if t.sticky {
		t.scrollToEnd()
	}

	count := 0
	rowAccum := 0
	for i := range t.messages {
		h := t.heightCache[i]
		msgStart := rowAccum
		msgEnd := rowAccum + h
		vpEnd := t.scrollTop + t.viewportH
		if msgEnd > t.scrollTop && msgStart < vpEnd {
			count++
		}
		rowAccum += h
	}
	return count
}

// --- Internal helpers ---

// recomputeContentHeight sums all known heights. Unknown heights are 0
// until rendered.
func (t *TranscriptModel) recomputeContentHeight() {
	total := 0
	for i := range t.messages {
		if h, ok := t.heightCache[i]; ok {
			total += h
		}
	}
	t.contentHeight = total
}

// scrollToEnd sets scrollTop so the bottom of content is at the bottom
// of the viewport.
func (t *TranscriptModel) scrollToEnd() {
	t.scrollTop = t.contentHeight - t.viewportH
	if t.scrollTop < 0 {
		t.scrollTop = 0
	}
}

// clampScroll keeps scrollTop within valid bounds.
func (t *TranscriptModel) clampScroll() {
	maxScroll := t.contentHeight - t.viewportH
	if maxScroll < 0 {
		maxScroll = 0
	}
	if t.scrollTop > maxScroll {
		t.scrollTop = maxScroll
	}
	if t.scrollTop < 0 {
		t.scrollTop = 0
	}
}

// scrollToMessage scrolls so that message at index i is visible.
func (t *TranscriptModel) scrollToMessage(i int) {
	if i < 0 || i >= len(t.messages) {
		return
	}
	rowAccum := 0
	for j := 0; j < i; j++ {
		if h, ok := t.heightCache[j]; ok {
			rowAccum += h
		}
	}
	// Put the target message near the top of the viewport.
	t.scrollTop = rowAccum
	t.clampScroll()
	t.sticky = false
}

// recomputeSearchHits finds all message indices whose content contains
// the search query (case-insensitive).
func (t *TranscriptModel) recomputeSearchHits() {
	t.searchHits = nil
	t.searchIdx = 0
	if t.searchQuery == "" {
		return
	}
	q := strings.ToLower(t.searchQuery)
	for i, msg := range t.messages {
		if strings.Contains(strings.ToLower(msg.Content), q) ||
			strings.Contains(strings.ToLower(msg.ToolName), q) ||
			strings.Contains(strings.ToLower(msg.ToolBody), q) ||
			strings.Contains(strings.ToLower(msg.ToolOutput), q) {
			t.searchHits = append(t.searchHits, i)
		}
	}
}

// countLines returns the number of lines in a rendered string.
// An empty string counts as 0 lines. A string with no newlines counts as 1.
func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}
