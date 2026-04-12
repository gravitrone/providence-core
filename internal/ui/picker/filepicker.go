package picker

import (
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// maxResults is the maximum number of fuzzy-filtered results shown.
const maxResults = 15

// refreshInterval is how often we check if the git index changed.
const refreshInterval = 5 * time.Second

// --- Messages ---

// FilesLoadedMsg is sent when the initial git ls-files completes.
type FilesLoadedMsg struct {
	Files []string
}

// FilesRefreshMsg is sent by the periodic refresh tick.
type FilesRefreshMsg struct{}

// --- FilePickerModel ---

// FilePickerModel provides fuzzy file search triggered by @ in input.
type FilePickerModel struct {
	files      []string // all project files
	filtered   []string // fuzzy-filtered results
	query      string   // text after @
	selected   int      // cursor position in filtered list
	active     bool     // @ was detected, popup visible
	tokenStart int      // byte offset of @ in input
	maxResults int
	width      int

	// Git index mtime tracking for refresh.
	cwd          string
	lastIndexMod time.Time
}

// NewFilePickerModel creates a new file picker.
func NewFilePickerModel(cwd string, width int) FilePickerModel {
	return FilePickerModel{
		cwd:        cwd,
		maxResults: maxResults,
		width:      width,
		selected:   0,
	}
}

// Init returns the initial command to load files.
func (m FilePickerModel) Init() tea.Cmd {
	return tea.Batch(loadFiles(m.cwd), tickRefresh())
}

// Active returns true if the picker popup is visible.
func (m FilePickerModel) Active() bool {
	return m.active
}

// SetWidth updates the display width.
func (m *FilePickerModel) SetWidth(w int) {
	m.width = w
}

// Update handles messages for the file picker.
func (m FilePickerModel) Update(msg tea.Msg) (FilePickerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case FilesLoadedMsg:
		m.files = msg.Files
		m.refilter()
		return m, nil

	case FilesRefreshMsg:
		cmd := tickRefresh()
		// Check if git index changed.
		info, err := os.Stat(m.cwd + "/.git/index")
		if err == nil && info.ModTime().After(m.lastIndexMod) {
			m.lastIndexMod = info.ModTime()
			return m, tea.Batch(cmd, loadFiles(m.cwd))
		}
		return m, cmd
	}
	return m, nil
}

// HandleInput processes the current input text and returns updated state.
// Call this whenever the text input changes.
func (m *FilePickerModel) HandleInput(input string) {
	// Find last @ after whitespace (or at position 0).
	atIdx := -1
	for i := len(input) - 1; i >= 0; i-- {
		if input[i] == '@' {
			// Valid trigger: start of input or preceded by whitespace.
			if i == 0 || input[i-1] == ' ' || input[i-1] == '\t' || input[i-1] == '\n' {
				atIdx = i
				break
			}
		}
	}

	if atIdx < 0 {
		m.active = false
		m.query = ""
		m.tokenStart = 0
		m.selected = 0
		return
	}

	m.active = true
	m.tokenStart = atIdx
	m.query = input[atIdx+1:]
	m.selected = 0
	m.refilter()
}

// HandleKey processes navigation keys when the picker is active.
// Returns: accepted (bool), replacement text if accepted.
func (m *FilePickerModel) HandleKey(key string) (accepted bool, replacement string) {
	if !m.active {
		return false, ""
	}

	switch key {
	case "tab", "enter":
		if len(m.filtered) == 0 {
			return false, ""
		}
		chosen := m.filtered[m.selected]
		m.active = false
		m.query = ""
		return true, m.formatAccept(chosen)

	case "up":
		if m.selected > 0 {
			m.selected--
		} else {
			m.selected = len(m.filtered) - 1
		}
		return false, ""

	case "down":
		if m.selected < len(m.filtered)-1 {
			m.selected++
		} else {
			m.selected = 0
		}
		return false, ""

	case "esc":
		m.active = false
		m.query = ""
		return false, ""
	}

	return false, ""
}

// TokenStart returns the byte offset of the @ trigger in the input.
func (m FilePickerModel) TokenStart() int {
	return m.tokenStart
}

// Filtered returns the current filtered file list.
func (m FilePickerModel) Filtered() []string {
	return m.filtered
}

// Selected returns the current selection index.
func (m FilePickerModel) Selected() int {
	return m.selected
}

// Query returns the current query string (text after @).
func (m FilePickerModel) Query() string {
	return m.query
}

// --- Internal ---

func (m *FilePickerModel) refilter() {
	if m.query == "" {
		// Show first N files when no query.
		limit := m.maxResults
		if limit > len(m.files) {
			limit = len(m.files)
		}
		m.filtered = m.files[:limit]
		return
	}

	type scored struct {
		path  string
		score int
	}

	var results []scored
	q := strings.ToLower(m.query)

	for _, f := range m.files {
		s := fuzzyScore(q, strings.ToLower(f))
		if s > 0 {
			results = append(results, scored{path: f, score: s})
		}
	}

	// Sort by score descending, then path length ascending.
	sort.Slice(results, func(i, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score > results[j].score
		}
		return len(results[i].path) < len(results[j].path)
	})

	limit := m.maxResults
	if limit > len(results) {
		limit = len(results)
	}
	m.filtered = make([]string, limit)
	for i := 0; i < limit; i++ {
		m.filtered[i] = results[i].path
	}

	// Clamp selected.
	if m.selected >= len(m.filtered) {
		m.selected = len(m.filtered) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
}

// formatAccept returns the replacement string for an accepted file.
func (m FilePickerModel) formatAccept(path string) string {
	if strings.Contains(path, " ") {
		return `@"` + path + `" `
	}
	return "@" + path + " "
}

// --- Fuzzy matching ---

// fuzzyScore returns a positive score if query is a subsequence of target, 0 otherwise.
// Higher scores = better match. Consecutive char matches and shorter paths score higher.
func fuzzyScore(query, target string) int {
	if len(query) == 0 {
		return 1
	}
	if len(query) > len(target) {
		return 0
	}

	qi := 0
	score := 0
	consecutive := 0
	lastMatch := -1

	for ti := 0; ti < len(target) && qi < len(query); ti++ {
		if target[ti] == query[qi] {
			qi++
			if lastMatch == ti-1 {
				consecutive++
				score += consecutive * 2 // bonus for consecutive matches
			} else {
				consecutive = 0
			}
			score++ // base point per match
			lastMatch = ti

			// Bonus for matching after separator (/, -, _).
			if ti > 0 {
				prev := target[ti-1]
				if prev == '/' || prev == '-' || prev == '_' || prev == '.' {
					score += 3
				}
			}
			// Bonus for matching at start.
			if ti == 0 {
				score += 3
			}
		}
	}

	if qi < len(query) {
		return 0 // not all query chars found
	}

	// Penalize longer paths slightly.
	score += 100 / (len(target) + 1)

	return score
}

// --- Tea commands ---

func loadFiles(cwd string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("git", "ls-files")
		cmd.Dir = cwd
		out, err := cmd.Output()
		if err != nil {
			return FilesLoadedMsg{Files: nil}
		}

		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		var files []string
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if l != "" {
				files = append(files, l)
			}
		}
		return FilesLoadedMsg{Files: files}
	}
}

func tickRefresh() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg {
		return FilesRefreshMsg{}
	})
}
