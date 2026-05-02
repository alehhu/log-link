package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hpcloud/tail"
)

var (
	entityRegex      = regexp.MustCompile(`([0-9a-fA-F-]{8,}|req-[a-zA-Z0-9]+|\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})`)
	tsRegex          = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}|\d{2}:\d{2}:\d{2}|\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2})`)
	fileLineRegex    = regexp.MustCompile(`([a-zA-Z0-9._/-]+\.[a-z]{1,4}):(\d+)`)
	// labeledSourceRe matches "label=source" where label is a plain identifier.
	// Splitting only on first = means kubectl selectors inside the source part are preserved.
	labeledSourceRe  = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9_-]*)=(.+)$`)
)

// logSource pairs a human-readable label with the actual file path or shell command.
type logSource struct {
	label string // shown in source column; empty = derive from arg
	arg   string // file path or shell command
}

// --- Session persistence --------------------------------------------------

type sessionSource struct {
	Label string `json:"label,omitempty"`
	Arg   string `json:"arg"`
}

type historicIncident struct {
	TotalCount   int       `json:"total_count"`
	SessionCount int       `json:"session_count"`
	FirstSeen    time.Time `json:"first_seen"`
	LastSeen     time.Time `json:"last_seen"`
}

type storedSession struct {
	Sources         []sessionSource              `json:"sources,omitempty"`
	IncidentMode    bool                         `json:"incident_mode,omitempty"`
	PulseCmd        string                       `json:"pulse_cmd,omitempty"`
	PulseInterval   int                          `json:"pulse_interval,omitempty"`
	Description     string                       `json:"description,omitempty"`
	LastUsed        time.Time                    `json:"last_used,omitempty"`
	IncidentHistory map[string]*historicIncident `json:"incident_history,omitempty"`
}

type sessionStore struct {
	Sessions map[string]storedSession `json:"sessions"`
}

func sessionFilePath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "loglink", "sessions.json")
}

func loadSessionStore() sessionStore {
	data, err := os.ReadFile(sessionFilePath())
	if err != nil {
		return sessionStore{Sessions: map[string]storedSession{}}
	}
	var store sessionStore
	if err := json.Unmarshal(data, &store); err != nil {
		return sessionStore{Sessions: map[string]storedSession{}}
	}
	if store.Sessions == nil {
		store.Sessions = map[string]storedSession{}
	}
	return store
}

func saveSession(name string, sources []logSource, incidentMode bool, pulseCmd string, pulseInterval int, newDesc string, incidents map[string]*Incident) error {
	store := loadSessionStore()

	ss := make([]sessionSource, len(sources))
	for i, src := range sources {
		ss[i] = sessionSource{Label: src.label, Arg: src.arg}
	}

	// Preserve existing description unless caller provides a new one.
	existing := store.Sessions[name]
	desc := existing.Description
	if newDesc != "" {
		desc = newDesc
	}

	// Merge current-session incidents into the persistent history.
	history := existing.IncidentHistory
	if history == nil {
		history = map[string]*historicIncident{}
	}
	for sig, inc := range incidents {
		if hi, ok := history[sig]; ok {
			hi.TotalCount += inc.Count
			hi.SessionCount++
			if inc.FirstSeen.Before(hi.FirstSeen) {
				hi.FirstSeen = inc.FirstSeen
			}
			if inc.LastSeen.After(hi.LastSeen) {
				hi.LastSeen = inc.LastSeen
			}
		} else {
			history[sig] = &historicIncident{
				TotalCount:   inc.Count,
				SessionCount: 1,
				FirstSeen:    inc.FirstSeen,
				LastSeen:     inc.LastSeen,
			}
		}
	}

	store.Sessions[name] = storedSession{
		Sources:         ss,
		IncidentMode:    incidentMode,
		PulseCmd:        pulseCmd,
		PulseInterval:   pulseInterval,
		Description:     desc,
		LastUsed:        time.Now(),
		IncidentHistory: history,
	}

	path := sessionFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// program is set in main() before p.Run() so that in-app source additions
// can start goroutines that send messages back to the TUI.
var program *tea.Program

type keyMap struct {
	Up         key.Binding
	Down       key.Binding
	PageUp     key.Binding
	PageDown   key.Binding
	Top        key.Binding
	Bottom     key.Binding
	Follow     key.Binding
	Highlight  key.Binding
	Focus      key.Binding
	Bookmark   key.Binding
	NextMark   key.Binding
	PrevMark   key.Binding
	Source     key.Binding
	Pulse      key.Binding
	ScrubBack  key.Binding
	ScrubFwd   key.Binding
	Sidebar    key.Binding
	Search     key.Binding
	SearchNext key.Binding
	SearchPrev key.Binding
	AddSource  key.Binding
	ZoomOut     key.Binding
	ZoomIn      key.Binding
	PulseExpand key.Binding
	Help        key.Binding
	Quit        key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Search, k.AddSource, k.Help, k.Sidebar, k.Follow, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.PageUp, k.PageDown, k.Top, k.Bottom},
		{k.Highlight, k.Focus, k.Bookmark, k.NextMark, k.PrevMark},
		{k.Search, k.SearchNext, k.SearchPrev, k.AddSource},
		{k.Pulse, k.ScrubBack, k.ScrubFwd, k.ZoomOut, k.ZoomIn, k.PulseExpand},
		{k.Source, k.Sidebar, k.Follow, k.Quit},
	}
}

var keys = keyMap{
	Up:        key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:      key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	PageUp:    key.NewBinding(key.WithKeys("pgup", "ctrl+u", "u"), key.WithHelp("u/ctrl+u", "pg up")),
	PageDown:  key.NewBinding(key.WithKeys("pgdown", "ctrl+d", "d"), key.WithHelp("d/ctrl+d", "pg down")),
	Top:       key.NewBinding(key.WithKeys("g"), key.WithHelp("gg", "top")),
	Bottom:    key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "now")),
	Follow:    key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "follow")),
	Highlight: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "highlight")),
	Focus:     key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "focus")),
	Bookmark:  key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "mark")),
	NextMark:  key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "next mark")),
	PrevMark:  key.NewBinding(key.WithKeys("["), key.WithHelp("[", "prev mark")),
	Source:    key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "source")),
	Pulse:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "pulse focus")),
	ScrubBack: key.NewBinding(key.WithKeys("h", "left"), key.WithHelp("h/←", "scrub back")),
	ScrubFwd:  key.NewBinding(key.WithKeys("l", "right"), key.WithHelp("l/→", "scrub fwd")),
	Sidebar:    key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "details")),
	Search:     key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
	SearchNext: key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "next match")),
	SearchPrev: key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "prev match")),
	AddSource:  key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add source")),
	ZoomOut:     key.NewBinding(key.WithKeys("=", "+"), key.WithHelp("=", "zoom out")),
	ZoomIn:      key.NewBinding(key.WithKeys("-"), key.WithHelp("-", "zoom in")),
	PulseExpand: key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "pulse fullscreen")),
	Help:        key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Quit:       key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
}

type MetricEntry struct {
	Timestamp time.Time
	Value     float64
}

type Incident struct {
	Signature string
	Count     int
	FirstSeen time.Time
	LastSeen  time.Time
	Sources   map[string]int
}

type LogEntry struct {
	Timestamp time.Time
	Source    string
	Content   string
	Entities  []string
	Level     string // canonical lowercase level: "error","warn","debug","info","fatal","panic","critical","trace"
}

// logEntryBatch is a slice of LogEntry sent as a single message to cap render rate.
type logEntryBatch []LogEntry

const (
	// batchCooldown is the window after the leading-edge send during which
	// additional lines are accumulated before a second flush. 50 ms caps renders
	// at ~20 fps regardless of log volume while remaining imperceptible to users.
	batchCooldown = 50 * time.Millisecond
	// scannerBufSize is 1 MB — prevents silent drops of long JSON log lines.
	scannerBufSize = 1 << 20
)

type model struct {
	entries         []LogEntry
	filteredEntries []int
	metrics         []MetricEntry
	bookmarks       []int
	cursor          int
	viewportH       int
	viewportW       int
	ready           bool
	highlightID     string
	focusID         string
	follow          bool
	pulseFocus      bool
	pulseCursor     int
	sidebarOpen     bool
	showHelp        bool
	incidentMode    bool
	incidents       map[string]*Incident
	startedAt       time.Time
	help            help.Model
	lastKey         string
	searchMode      bool
	searchQuery     string
	searchRe        *regexp.Regexp
	addSourceMode   bool
	addSourceInput  string
	activeSources    []logSource
	sessionName      string
	sessionDesc      string
	incidentHistory  map[string]*historicIncident
	pulseWindow      int  // data points shown in sparkline (0 = auto)
	pulseIntervalSec int  // seconds between pulse samples; used for time labels
	pulseFullscreen  bool // P key toggles fullscreen pulse chart
}

func initialModel(incidentMode bool, sessionName, sessionDesc string, sources []logSource, history map[string]*historicIncident, pulseIntervalSec int) model {
	return model{
		entries:          []LogEntry{},
		cursor:           0,
		follow:           true,
		sidebarOpen:      false,
		incidentMode:     incidentMode,
		incidents:        map[string]*Incident{},
		startedAt:        time.Now(),
		help:             help.New(),
		lastKey:          "",
		sessionName:      sessionName,
		sessionDesc:      sessionDesc,
		activeSources:    append([]logSource(nil), sources...),
		incidentHistory:  history,
		pulseIntervalSec: pulseIntervalSec,
	}
}

func (m *model) updateFilteredEntries() {
	if m.focusID == "" {
		m.filteredEntries = nil
		return
	}
	m.filteredEntries = []int{}
	for i, entry := range m.entries {
		if strings.Contains(entry.Content, m.focusID) {
			m.filteredEntries = append(m.filteredEntries, i)
		}
	}
}

func (m model) currentEntries() []LogEntry {
	if m.focusID == "" {
		return m.entries
	}
	res := make([]LogEntry, len(m.filteredEntries))
	for i, idx := range m.filteredEntries {
		res[i] = m.entries[idx]
	}
	return res
}

func (m *model) updateSearchRe() {
	if m.searchQuery == "" {
		m.searchRe = nil
		return
	}
	m.searchRe = regexp.MustCompile("(?i)" + regexp.QuoteMeta(m.searchQuery))
}

func (m *model) searchJump(forward bool) {
	if m.searchRe == nil {
		return
	}
	ce := m.currentEntries()
	if len(ce) == 0 {
		return
	}
	if forward {
		for i := m.cursor + 1; i < len(ce); i++ {
			if m.searchRe.MatchString(ce[i].Content) {
				m.cursor = i
				m.follow = false
				return
			}
		}
		for i := 0; i <= m.cursor; i++ {
			if m.searchRe.MatchString(ce[i].Content) {
				m.cursor = i
				m.follow = false
				return
			}
		}
	} else {
		for i := m.cursor - 1; i >= 0; i-- {
			if m.searchRe.MatchString(ce[i].Content) {
				m.cursor = i
				m.follow = false
				return
			}
		}
		for i := len(ce) - 1; i >= m.cursor; i-- {
			if m.searchRe.MatchString(ce[i].Content) {
				m.cursor = i
				m.follow = false
				return
			}
		}
	}
}

func applySearchHighlight(content string, re *regexp.Regexp, baseStyle lipgloss.Style) string {
	indices := re.FindAllStringIndex(content, -1)
	if len(indices) == 0 {
		return baseStyle.Render(content)
	}
	var sb strings.Builder
	last := 0
	for _, idx := range indices {
		if idx[0] > last {
			sb.WriteString(baseStyle.Render(content[last:idx[0]]))
		}
		sb.WriteString(searchMatchStyle.Render(content[idx[0]:idx[1]]))
		last = idx[1]
	}
	if last < len(content) {
		sb.WriteString(baseStyle.Render(content[last:]))
	}
	return sb.String()
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Add-source mode: capture all keystrokes as text input.
		if m.addSourceMode {
			switch msg.Type {
			case tea.KeyCtrlC:
				return m, tea.Quit
			case tea.KeyEsc:
				m.addSourceMode = false
				m.addSourceInput = ""
			case tea.KeyEnter:
				input := strings.TrimSpace(m.addSourceInput)
				if input != "" {
					src := logSource{arg: input}
					if mm := labeledSourceRe.FindStringSubmatch(input); mm != nil {
						src.label = mm[1]
						src.arg = mm[2]
					}
					m.activeSources = append(m.activeSources, src)
					go startSource(program, src)
				}
				m.addSourceMode = false
				m.addSourceInput = ""
			case tea.KeyBackspace, tea.KeyCtrlH:
				if len(m.addSourceInput) > 0 {
					runes := []rune(m.addSourceInput)
					m.addSourceInput = string(runes[:len(runes)-1])
				}
			case tea.KeyRunes:
				m.addSourceInput += string(msg.Runes)
			}
			return m, nil
		}

		// Search mode: capture all keystrokes as text input.
		if m.searchMode {
			switch msg.Type {
			case tea.KeyCtrlC:
				return m, tea.Quit
			case tea.KeyEsc:
				m.searchMode = false
				m.searchQuery = ""
				m.searchRe = nil
			case tea.KeyEnter:
				m.searchMode = false
				if m.searchRe != nil {
					m.searchJump(true)
				}
			case tea.KeyBackspace, tea.KeyCtrlH:
				if len(m.searchQuery) > 0 {
					runes := []rune(m.searchQuery)
					m.searchQuery = string(runes[:len(runes)-1])
					m.updateSearchRe()
				}
			case tea.KeyRunes:
				m.searchQuery += string(msg.Runes)
				m.updateSearchRe()
			}
			return m, nil
		}

		if m.showHelp {
			m.showHelp = false
			return m, nil
		}
		currEntries := m.currentEntries()
		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, keys.Help):
			m.showHelp = !m.showHelp
		case key.Matches(msg, keys.Sidebar):
			m.sidebarOpen = !m.sidebarOpen
		case key.Matches(msg, keys.Pulse):
			m.pulseFocus = !m.pulseFocus
			if m.pulseFocus && len(m.metrics) > 0 {
				m.pulseCursor = len(m.metrics) - 1
			}
		case key.Matches(msg, keys.ScrubBack):
			if m.pulseFocus && len(m.metrics) > 0 && m.pulseCursor > 0 {
				m.pulseCursor--
				m.jumpToTime(m.metrics[m.pulseCursor].Timestamp)
			}
		case key.Matches(msg, keys.ScrubFwd):
			if m.pulseFocus && len(m.metrics) > 0 && m.pulseCursor < len(m.metrics)-1 {
				m.pulseCursor++
				m.jumpToTime(m.metrics[m.pulseCursor].Timestamp)
			}
		case key.Matches(msg, keys.Up):
			if m.cursor > 0 {
				m.cursor--
				m.follow = false
			}
		case key.Matches(msg, keys.Down):
			if m.cursor < len(currEntries)-1 {
				m.cursor++
			} else {
				m.follow = true
			}
		case key.Matches(msg, keys.PageUp):
			m.cursor -= m.viewportH / 2
			if m.cursor < 0 {
				m.cursor = 0
			}
			m.follow = false
		case key.Matches(msg, keys.PageDown):
			m.cursor += m.viewportH / 2
			if m.cursor >= len(currEntries) {
				m.cursor = len(currEntries) - 1
			}
			if m.cursor < 0 {
				m.cursor = 0
			}
		case key.Matches(msg, keys.Top):
			if m.lastKey == "g" {
				m.cursor = 0
				m.follow = false
				m.lastKey = ""
			} else {
				m.lastKey = "g"
				// Clear lastKey after a short delay if no second g is pressed
				return m, tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
					return "clearLastKey"
				})
			}
		case key.Matches(msg, keys.Bottom):
			if len(currEntries) > 0 {
				m.cursor = len(currEntries) - 1
				m.follow = true
			}
		case key.Matches(msg, keys.Bookmark):
			if len(currEntries) > 0 {
				globalIdx := m.getGlobalIdx(m.cursor)
				found := -1
				for i, b := range m.bookmarks {
					if b == globalIdx {
						found = i
						break
					}
				}
				if found != -1 {
					m.bookmarks = append(m.bookmarks[:found], m.bookmarks[found+1:]...)
				} else {
					m.bookmarks = append(m.bookmarks, globalIdx)
				}
			}
		case key.Matches(msg, keys.PrevMark):
			if len(m.bookmarks) > 0 && len(currEntries) > 0 {
				m.jumpToNearestBookmark(false)
			}
		case key.Matches(msg, keys.NextMark):
			if len(m.bookmarks) > 0 && len(currEntries) > 0 {
				m.jumpToNearestBookmark(true)
			}
		case key.Matches(msg, keys.Highlight):
			if len(currEntries) > 0 {
				entry := currEntries[m.cursor]
				if len(entry.Entities) > 0 {
					if m.highlightID == entry.Entities[0] {
						m.highlightID = ""
					} else {
						m.highlightID = entry.Entities[0]
					}
				}
			}
		case key.Matches(msg, keys.Source):
			if len(currEntries) > 0 {
				entry := currEntries[m.cursor]
				match := fileLineRegex.FindStringSubmatch(entry.Content)
				if len(match) > 2 {
					filename := match[1]
					line := match[2]
					if _, err := os.Stat(filename); err == nil {
						editor := os.Getenv("EDITOR")
						if editor == "" {
							editor = "vi"
						}
						cmd := exec.Command(editor, fmt.Sprintf("+%s", line), filename)
						return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
							if err != nil {
								return LogEntry{Content: fmt.Sprintf("Editor error: %v", err), Source: "SYSTEM", Timestamp: time.Now()}
							}
							return nil
						})
					}
				}
			}
		case key.Matches(msg, keys.Focus):
			if len(currEntries) > 0 {
				entry := currEntries[m.cursor]
				if len(entry.Entities) > 0 {
					if m.focusID == entry.Entities[0] {
						m.focusID = ""
					} else {
						m.focusID = entry.Entities[0]
					}
					m.updateFilteredEntries()
					m.cursor = 0
				}
			}
		case key.Matches(msg, keys.Follow):
			m.follow = !m.follow
			if m.follow {
				m.pulseFocus = false
				if len(currEntries) > 0 {
					m.cursor = len(currEntries) - 1
				}
			}
		case key.Matches(msg, keys.PulseExpand):
			if len(m.metrics) > 0 {
				m.pulseFullscreen = !m.pulseFullscreen
			}
		case key.Matches(msg, keys.ZoomOut):
			if len(m.metrics) > 0 {
				step := m.viewportW - 20
				if step < 10 {
					step = 10
				}
				if m.pulseWindow == 0 {
					m.pulseWindow = min(len(m.metrics), step)
				}
				m.pulseWindow += step
				if m.pulseWindow > len(m.metrics) {
					m.pulseWindow = len(m.metrics)
				}
			}
		case key.Matches(msg, keys.ZoomIn):
			if m.pulseWindow > 0 {
				step := m.viewportW - 20
				if step < 10 {
					step = 10
				}
				m.pulseWindow -= step
				if m.pulseWindow <= 0 {
					m.pulseWindow = 0 // back to auto (fit width)
				}
			}
		case key.Matches(msg, keys.AddSource):
			m.addSourceMode = true
			m.addSourceInput = ""
		case key.Matches(msg, keys.Search):
			m.searchMode = true
			m.searchQuery = ""
			m.searchRe = nil
		case key.Matches(msg, keys.SearchNext):
			m.searchJump(true)
		case key.Matches(msg, keys.SearchPrev):
			m.searchJump(false)
		case msg.String() == "esc":
			m.highlightID = ""
			m.focusID = ""
			m.pulseFocus = false
			m.pulseFullscreen = false
			m.showHelp = false
			m.searchMode = false
			m.searchQuery = ""
			m.searchRe = nil
			m.addSourceMode = false
			m.addSourceInput = ""
			m.updateFilteredEntries()
		}

	case tea.WindowSizeMsg:
		m.viewportH = msg.Height
		m.viewportW = msg.Width
		m.ready = true

	case string:
		if msg == "clearLastKey" {
			m.lastKey = ""
		}

	case logEntryBatch:
		for _, entry := range msg {
			m.insertEntry(entry)
		}
		if m.follow {
			if ce := m.currentEntries(); len(ce) > 0 {
				m.cursor = len(ce) - 1
			}
		}
		return m, nil

	case LogEntry:
		m.insertEntry(msg)
		if m.follow {
			if ce := m.currentEntries(); len(ce) > 0 {
				m.cursor = len(ce) - 1
			}
		}
		return m, nil

	case MetricEntry:
		m.metrics = append(m.metrics, msg)
		if len(m.metrics) > 2000 {
			m.metrics = m.metrics[1:]
			if m.pulseCursor > 0 {
				m.pulseCursor--
			}
		}
		if m.follow {
			m.pulseCursor = len(m.metrics) - 1
		}
		return m, nil
	}

	return m, nil
}

func (m model) getGlobalIdx(viewIdx int) int {
	if m.focusID == "" {
		return viewIdx
	}
	if viewIdx < 0 || viewIdx >= len(m.filteredEntries) {
		return -1
	}
	return m.filteredEntries[viewIdx]
}

func (m *model) insertEntry(entry LogEntry) {
	// Binary search: insert after all entries with timestamp <= entry.Timestamp (stable sort).
	idx := sort.Search(len(m.entries), func(i int) bool {
		return m.entries[i].Timestamp.After(entry.Timestamp)
	})

	// Adjust bookmarks.
	for i := range m.bookmarks {
		if m.bookmarks[i] >= idx {
			m.bookmarks[i]++
		}
	}

	// Incrementally maintain filteredEntries: shift existing indices and conditionally
	// add the new entry. This replaces the O(n) full rebuild in the hot path.
	if m.focusID != "" {
		for i := range m.filteredEntries {
			if m.filteredEntries[i] >= idx {
				m.filteredEntries[i]++
			}
		}
		if strings.Contains(entry.Content, m.focusID) {
			pos := sort.SearchInts(m.filteredEntries, idx)
			m.filteredEntries = append(m.filteredEntries, 0)
			copy(m.filteredEntries[pos+1:], m.filteredEntries[pos:])
			m.filteredEntries[pos] = idx
		}
	}

	if idx == len(m.entries) {
		m.entries = append(m.entries, entry)
	} else {
		m.entries = append(m.entries, LogEntry{})
		copy(m.entries[idx+1:], m.entries[idx:])
		m.entries[idx] = entry
	}
	if m.incidentMode {
		m.updateIncidents(entry)
	}
}

func (m *model) jumpToNearestBookmark(forward bool) {
	globalIdx := m.getGlobalIdx(m.cursor)
	targetGlobalIdx := -1
	if forward {
		for _, b := range m.bookmarks {
			if b > globalIdx {
				if targetGlobalIdx == -1 || b < targetGlobalIdx {
					targetGlobalIdx = b
				}
			}
		}
	} else {
		for _, b := range m.bookmarks {
			if b < globalIdx {
				if targetGlobalIdx == -1 || b > targetGlobalIdx {
					targetGlobalIdx = b
				}
			}
		}
	}
	if targetGlobalIdx != -1 {
		ce := m.currentEntries()
		for i := range ce {
			if m.getGlobalIdx(i) == targetGlobalIdx {
				m.cursor = i
				m.follow = false
				return
			}
		}
	}
}

func (m *model) updateIncidents(entry LogEntry) {
	isError := entry.Level == "error" || entry.Level == "fatal" || entry.Level == "panic" || entry.Level == "critical"
	if !isError {
		lower := strings.ToLower(entry.Content)
		isError = strings.Contains(lower, "error") || strings.Contains(lower, "panic") ||
			strings.Contains(lower, "fatal") || strings.Contains(lower, "timeout")
	}
	if !isError {
		return
	}
	sig := incidentSignature(entry.Content)
	if sig == "" {
		return
	}
	inc, ok := m.incidents[sig]
	if !ok {
		inc = &Incident{Signature: sig, Sources: map[string]int{}, FirstSeen: entry.Timestamp}
		m.incidents[sig] = inc
	}
	inc.Count++
	inc.LastSeen = entry.Timestamp
	inc.Sources[entry.Source]++
}

func incidentSignature(content string) string {
	normalized := strings.ToLower(content)
	normalized = entityRegex.ReplaceAllString(normalized, "<id>")
	if len(normalized) > 120 {
		normalized = normalized[:120]
	}
	return strings.TrimSpace(normalized)
}

func (m model) topIncidents(limit int) []*Incident {
	if len(m.incidents) == 0 {
		return nil
	}
	all := make([]*Incident, 0, len(m.incidents))
	for _, inc := range m.incidents {
		all = append(all, inc)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Count == all[j].Count {
			return all[i].LastSeen.After(all[j].LastSeen)
		}
		return all[i].Count > all[j].Count
	})
	if len(all) > limit {
		all = all[:limit]
	}
	return all
}

func (m *model) jumpToTime(t time.Time) {
	ce := m.currentEntries()
	if len(ce) == 0 {
		return
	}
	bestIdx := 0
	minDiff := t.Sub(ce[0].Timestamp)
	if minDiff < 0 {
		minDiff = -minDiff
	}
	for i, entry := range ce {
		diff := t.Sub(entry.Timestamp)
		if diff < 0 {
			diff = -diff
		}
		if diff < minDiff {
			minDiff = diff
			bestIdx = i
		}
	}
	m.cursor = bestIdx
	m.follow = false
}

var (
	subtleStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	sourceStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("63")).Bold(true)
	contentStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	highlightStyle    = lipgloss.NewStyle().Background(lipgloss.Color("57")).Foreground(lipgloss.Color("255"))
	selectedStyle     = lipgloss.NewStyle().Background(lipgloss.Color("236"))
	focusStyle        = lipgloss.NewStyle().Background(lipgloss.Color("212")).Foreground(lipgloss.Color("0")).Bold(true)
	sidebarStyle      = lipgloss.NewStyle().Padding(1).Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("62"))
	titleStyle        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229")).Background(lipgloss.Color("62")).Padding(0, 1)
	badgeOnStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("42")).Bold(true).Padding(0, 1)
	badgeOffStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Background(lipgloss.Color("240")).Padding(0, 1)
	levelErrorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	levelWarnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	levelDebugStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	searchMatchStyle  = lipgloss.NewStyle().Background(lipgloss.Color("226")).Foreground(lipgloss.Color("0")).Bold(true)
	searchBarStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("226")).Bold(true)
	addSourceBarStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true)
)

// errorLevelRe matches [ERROR], ERROR:, level=error, "level":"error" and equivalents for fatal/panic/critical.
var errorLevelRe = regexp.MustCompile(`(?i)(?:\[(?:error|fatal|panic|critical)\]|(?:error|fatal|panic|critical):|level[=:]["']?(?:error|fatal|panic|critical))`)

// warnLevelRe matches [WARN], WARN:, level=warn, "level":"warning" etc.
var warnLevelRe = regexp.MustCompile(`(?i)(?:\[warn(?:ing)?\]|warn(?:ing)?:|level[=:]["']?warn(?:ing)?)`)

// debugLevelRe matches [DEBUG], DEBUG:, level=debug, level=trace etc.
var debugLevelRe = regexp.MustCompile(`(?i)(?:\[(?:debug|trace)\]|(?:debug|trace):|level[=:]["']?(?:debug|trace))`)

func detectLogLevel(content string) string {
	if errorLevelRe.MatchString(content) {
		return "error"
	}
	if warnLevelRe.MatchString(content) {
		return "warn"
	}
	if debugLevelRe.MatchString(content) {
		return "debug"
	}
	return "info"
}

func (m model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	if m.showHelp {
		return m.renderHelp()
	}

	if m.pulseFullscreen && len(m.metrics) > 0 {
		return m.renderPulseFullscreen()
	}

	var s strings.Builder

	// Compact pulse chart (stats header + 3-row bars).
	if len(m.metrics) > 0 {
		s.WriteString(m.renderSparklineCompact())
	}

	// Main Layout
	mainView := m.renderLogs()
	s.WriteString(mainView)

	// Persistent Incident Leaderboard (if active)
	if m.incidentMode && len(m.incidents) > 0 {
		s.WriteString("\n" + m.renderIncidentBar())
	}

	// Footer
	if m.addSourceMode {
		s.WriteString("\n" + addSourceBarStyle.Render("+ add source (label=cmd or path): "+m.addSourceInput+"█"))
	} else if m.searchMode {
		s.WriteString("\n" + searchBarStyle.Render("/ "+m.searchQuery+"█"))
	} else {
		s.WriteString("\n" + m.help.View(keys))
	}

	// Overlay Modal if open
	if m.sidebarOpen {
		return lipgloss.Place(m.viewportW, m.viewportH, lipgloss.Center, lipgloss.Center, m.renderModal())
	}

	return s.String()
}

func (m model) renderHelp() string {
	helpText := m.help.View(keys)
	return lipgloss.Place(m.viewportW, m.viewportH, lipgloss.Center, lipgloss.Center, helpText)
}

func (m model) renderLogs() string {
	var s strings.Builder
	sessionLabel := m.sessionName
	if sessionLabel == "" {
		sessionLabel = "default"
	}
	if m.sessionDesc != "" {
		sessionLabel += ": " + m.sessionDesc
	}
	header := titleStyle.Render(" LOGLINK ") + subtleStyle.Render(" ["+sessionLabel+"] ")
	currEntries := m.currentEntries()
	followBadge := badgeOffStyle.Render("FOLLOW OFF")
	if m.follow {
		followBadge = badgeOnStyle.Render("FOLLOW ON")
	}
	pulseBadge := badgeOffStyle.Render("PULSE FREE")
	if m.pulseFocus {
		pulseBadge = badgeOnStyle.Render("PULSE FOCUS")
	}
	stats := fmt.Sprintf("  %d/%d entries  %s  %s", len(currEntries), len(m.entries), followBadge, pulseBadge)
	if m.focusID != "" {
		stats += fmt.Sprintf(" | FOCUS: %s", focusStyle.Render(m.focusID))
	}
	if m.searchQuery != "" {
		stats += fmt.Sprintf(" | SEARCH: %s", searchBarStyle.Render("/"+m.searchQuery))
	}
	s.WriteString(header + stats + "\n\n")

	width := m.viewportW
	height := m.viewportH - 6
	if len(m.metrics) > 0 {
		height -= compactPulseHeight + 1 // 3 chart rows + 1 stats header
	}
	if m.incidentMode && len(m.incidents) > 0 {
		height -= 1
	}
	if height <= 0 {
		return ""
	}
	if len(currEntries) == 0 {
		return subtleStyle.Render("No log entries yet. Waiting for streams...") + "\n"
	}

	start := m.cursor - (height / 2)
	if start < 0 {
		start = 0
	}
	end := start + height
	if end > len(currEntries) {
		end = len(currEntries)
		start = end - height
		if start < 0 {
			start = 0
		}
	}

	for i := start; i < end; i++ {
		entry := currEntries[i]
		lineStyle := lipgloss.NewStyle().Width(width).MaxWidth(width)
		if i == m.cursor {
			lineStyle = selectedStyle.Copy().Width(width).MaxWidth(width)
		}

		isHighlighted := (m.highlightID != "" && strings.Contains(entry.Content, m.highlightID)) ||
			(m.focusID != "" && strings.Contains(entry.Content, m.focusID))

		isBookmarked := false
		globalIdx := m.getGlobalIdx(i)
		for _, b := range m.bookmarks {
			if b == globalIdx {
				isBookmarked = true
				break
			}
		}

		bookmarkIcon := " "
		if isBookmarked {
			bookmarkIcon = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Render("🔖")
		}

		ts := subtleStyle.Render(entry.Timestamp.Format("15:04:05"))
		srcColor := lipgloss.Color(fmt.Sprint(hashString(entry.Source)%15 + 1))
		src := lipgloss.NewStyle().Foreground(srcColor).Bold(true).Render(fmt.Sprintf("%-12s", truncate(entry.Source, 12)))

		content := entry.Content
		if isHighlighted {
			content = highlightStyle.Render(content)
		} else {
			lvl := entry.Level // always set at parse time by newLogEntry
			base := contentStyle
			switch lvl {
			case "error", "fatal", "panic", "critical":
				base = levelErrorStyle
			case "warn", "warning":
				base = levelWarnStyle
			case "debug", "trace":
				base = levelDebugStyle
			}
			if m.searchRe != nil {
				content = applySearchHighlight(content, m.searchRe, base)
			} else {
				content = base.Render(content)
			}
		}

		line := fmt.Sprintf("%s %s %s %s", bookmarkIcon, ts, src, content)
		s.WriteString(lineStyle.Render(line) + "\n")
	}

	return s.String()
}

func (m model) renderIncidentBar() string {
	top := m.topIncidents(3)
	var items []string
	for _, inc := range top {
		items = append(items, fmt.Sprintf("🔥 %dx %s", inc.Count, truncate(inc.Signature, 30)))
	}
	barStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("235")).
		Foreground(lipgloss.Color("214")).
		Italic(true).
		Padding(0, 1).
		Width(m.viewportW)
	return barStyle.Render("TOP INCIDENTS: " + strings.Join(items, " | "))
}

func (m model) renderModal() string {
	currEntries := m.currentEntries()
	if len(currEntries) == 0 {
		return ""
	}
	entry := currEntries[m.cursor]

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213")).Underline(true).Render("LOG ENTRY DETAILS") + "\n\n")
	b.WriteString(subtleStyle.Render("Source: ") + entry.Source + "\n")
	b.WriteString(subtleStyle.Render("Time:   ") + entry.Timestamp.Format("2006-01-02 15:04:05.000") + "\n")
	if entry.Level != "" {
		lvlStyle := contentStyle
		switch entry.Level {
		case "error", "fatal", "panic", "critical":
			lvlStyle = levelErrorStyle
		case "warn", "warning":
			lvlStyle = levelWarnStyle
		case "debug", "trace":
			lvlStyle = levelDebugStyle
		}
		b.WriteString(subtleStyle.Render("Level:  ") + lvlStyle.Render(strings.ToUpper(entry.Level)) + "\n")
	}
	b.WriteString("\n")

	if len(entry.Entities) > 0 {
		b.WriteString(lipgloss.NewStyle().Bold(true).Render("ENTITIES DETECTED:") + "\n")
		for _, e := range entry.Entities {
			b.WriteString("• " + e + "\n")
		}
		b.WriteString("\n")
	}

	// Cross-session incident history
	if len(m.incidentHistory) > 0 {
		type histEntry struct {
			sig string
			hi  *historicIncident
		}
		hist := make([]histEntry, 0, len(m.incidentHistory))
		for sig, hi := range m.incidentHistory {
			hist = append(hist, histEntry{sig, hi})
		}
		sort.Slice(hist, func(i, j int) bool {
			return hist[i].hi.TotalCount > hist[j].hi.TotalCount
		})
		if len(hist) > 5 {
			hist = hist[:5]
		}
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("208")).Render("INCIDENT HISTORY (all sessions):") + "\n")
		for _, h := range hist {
			age := time.Since(h.hi.LastSeen).Round(time.Minute)
			b.WriteString(fmt.Sprintf("  %s  %dx across %d sessions  last: %s ago\n",
				levelErrorStyle.Render("▸"),
				h.hi.TotalCount,
				h.hi.SessionCount,
				age,
			))
			b.WriteString("    " + subtleStyle.Render(truncate(h.sig, 70)) + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString(lipgloss.NewStyle().Bold(true).Render("CONTENT:") + "\n")
	contentStyle := lipgloss.NewStyle().
		Width(int(float64(m.viewportW)*0.7) - 4).
		Foreground(lipgloss.Color("255"))
	b.WriteString(contentStyle.Render(entry.Content))

	b.WriteString("\n\n" + subtleStyle.Render("Press 'd' or 'Esc' to close this window"))

	modalWidth := int(float64(m.viewportW) * 0.7)
	modalHeight := int(float64(m.viewportH) * 0.6)

	return lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1).
		Width(modalWidth).
		Height(modalHeight).
		Background(lipgloss.Color("234")).
		Render(b.String())
}

var sparklineChars = []string{" ", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

// barEighths[n] = block char that is n/8 full from the bottom (for multi-row bar charts).
var barEighths = []string{" ", "▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

// compactPulseHeight is the number of chart rows in the compact (non-fullscreen) sparkline.
const compactPulseHeight = 3

// downsampleMax compresses src to targetLen elements by taking the maximum
// value in each bucket. Spikes are preserved rather than averaged away.
func downsampleMax(src []MetricEntry, targetLen int) []MetricEntry {
	if len(src) <= targetLen {
		return src
	}
	out := make([]MetricEntry, targetLen)
	bucketSize := float64(len(src)) / float64(targetLen)
	for i := 0; i < targetLen; i++ {
		start := int(float64(i) * bucketSize)
		end := int(float64(i+1) * bucketSize)
		if end > len(src) {
			end = len(src)
		}
		best := src[start]
		for j := start + 1; j < end; j++ {
			if src[j].Value > best.Value {
				best = src[j]
			}
		}
		out[i] = best
	}
	return out
}

// renderSparklineCompact renders a stats header line + compactPulseHeight-row bar chart.
func (m model) renderSparklineCompact() string {
	if len(m.metrics) == 0 {
		return ""
	}

	chartWidth := m.viewportW
	if chartWidth < 10 {
		chartWidth = 10
	}

	windowSize := m.pulseWindow
	if windowSize <= 0 || windowSize > len(m.metrics) {
		windowSize = len(m.metrics)
	}

	rawWindow := m.metrics[len(m.metrics)-windowSize:]
	downsampled := len(rawWindow) > chartWidth
	data := rawWindow
	if downsampled {
		data = downsampleMax(rawWindow, chartWidth)
	}

	pulseCursor := m.pulseCursor
	if pulseCursor < 0 {
		pulseCursor = 0
	}
	if pulseCursor >= len(m.metrics) {
		pulseCursor = len(m.metrics) - 1
	}

	maxVal, minVal, curVal, sum := 0.0, math.MaxFloat64, 0.0, 0.0
	for _, mt := range data {
		if mt.Value > maxVal {
			maxVal = mt.Value
		}
		if mt.Value < minVal {
			minVal = mt.Value
		}
		curVal = mt.Value
		sum += mt.Value
	}
	if minVal == math.MaxFloat64 {
		minVal = 0
	}
	avg := sum / float64(max(1, len(data)))

	windowLabel := ""
	if m.pulseIntervalSec > 0 {
		secs := windowSize * m.pulseIntervalSec
		if secs < 60 {
			windowLabel = fmt.Sprintf(" [%ds]", secs)
		} else {
			windowLabel = fmt.Sprintf(" [%dm]", secs/60)
		}
	}

	statsStr := fmt.Sprintf("Pulse  cur:%.2f  min:%.1f  max:%.1f  avg:%.1f%s  =/-:zoom  tab:scrub  P:expand",
		curVal, minVal, maxVal, avg, windowLabel)
	statsLine := subtleStyle.Render(statsStr)

	barStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	cursorStyle := lipgloss.NewStyle().Background(lipgloss.Color("212")).Foreground(lipgloss.Color("0"))

	// Build compactPulseHeight chart rows simultaneously, column by column.
	rowBuilders := make([]strings.Builder, compactPulseHeight)
	for i, met := range data {
		v := 0.5
		if maxVal > minVal {
			v = (met.Value - minVal) / (maxVal - minVal)
		}
		total := compactPulseHeight * 8
		filled := int(math.Round(v * float64(total)))
		if filled > total {
			filled = total
		}

		isCursor := !downsampled && m.pulseFocus && i == pulseCursor-(len(m.metrics)-windowSize)

		for row := 0; row < compactPulseHeight; row++ {
			bottomOffset := (compactPulseHeight - 1 - row) * 8
			var char string
			switch {
			case filled <= bottomOffset:
				char = " "
			case filled >= bottomOffset+8:
				char = "█"
			default:
				char = barEighths[filled-bottomOffset]
			}
			if isCursor {
				rowBuilders[row].WriteString(cursorStyle.Render(char))
			} else {
				rowBuilders[row].WriteString(barStyle.Render(char))
			}
		}
	}

	var s strings.Builder
	s.WriteString(statsLine + "\n")
	for row := 0; row < compactPulseHeight; row++ {
		s.WriteString(rowBuilders[row].String() + "\n")
	}
	return s.String()
}

// renderPulseFullscreen renders a full-viewport pulse chart with Y-axis, gridlines,
// X-axis timestamps, and a stats header. Toggled with P.
func (m model) renderPulseFullscreen() string {
	const yAxisWidth = 8 // " XXXX.X ┤" — 6-char value + space + box char
	chartWidth := m.viewportW - yAxisWidth
	chartHeight := m.viewportH - 7 // stats + blank + chart + x-sep + x-labels + blank + footer
	if chartWidth < 5 {
		chartWidth = 5
	}
	if chartHeight < 4 {
		chartHeight = 4
	}

	windowSize := m.pulseWindow
	if windowSize <= 0 || windowSize > len(m.metrics) {
		windowSize = len(m.metrics)
	}

	rawWindow := m.metrics[len(m.metrics)-windowSize:]
	downsampled := len(rawWindow) > chartWidth
	data := rawWindow
	if downsampled {
		data = downsampleMax(rawWindow, chartWidth)
	}

	pulseCursor := m.pulseCursor
	if pulseCursor < 0 {
		pulseCursor = 0
	}
	if pulseCursor >= len(m.metrics) {
		pulseCursor = len(m.metrics) - 1
	}

	maxVal, minVal, curVal, sum := 0.0, math.MaxFloat64, 0.0, 0.0
	for _, mt := range data {
		if mt.Value > maxVal {
			maxVal = mt.Value
		}
		if mt.Value < minVal {
			minVal = mt.Value
		}
		curVal = mt.Value
		sum += mt.Value
	}
	if minVal == math.MaxFloat64 {
		minVal = 0
	}
	avg := sum / float64(max(1, len(data)))

	windowLabel := ""
	if m.pulseIntervalSec > 0 {
		secs := windowSize * m.pulseIntervalSec
		if secs < 60 {
			windowLabel = fmt.Sprintf("[%ds] ", secs)
		} else {
			windowLabel = fmt.Sprintf("[%dm] ", secs/60)
		}
	}

	cursorInfo := ""
	if m.pulseFocus && pulseCursor < len(m.metrics) {
		cursorInfo = "  cursor:" + m.metrics[pulseCursor].Timestamp.Format("15:04:05")
	}

	statsLine := titleStyle.Render(" PULSE ") + "  " +
		subtleStyle.Render(fmt.Sprintf("cur:%.2f  min:%.1f  max:%.1f  avg:%.1f  %s%s",
			curVal, minVal, maxVal, avg, windowLabel, cursorInfo))

	// Rows with Y-axis labels (top, 33%, 67%, bottom) and gridlines at 33%/67%.
	label33row := chartHeight * 2 / 3
	label67row := chartHeight / 3
	labeledRows := map[int]float64{
		0:              maxVal,
		label67row:     minVal + (maxVal-minVal)*0.67,
		label33row:     minVal + (maxVal-minVal)*0.33,
		chartHeight - 1: minVal,
	}
	gridRows := map[int]bool{label33row: true, label67row: true}

	// Precompute filled eighths and cursor flag per column.
	type col struct {
		filled   int
		isCursor bool
	}
	cols := make([]col, len(data))
	for i, mt := range data {
		v := 0.5
		if maxVal > minVal {
			v = (mt.Value - minVal) / (maxVal - minVal)
		}
		total := chartHeight * 8
		filled := int(math.Round(v * float64(total)))
		if filled > total {
			filled = total
		}
		cols[i] = col{
			filled:   filled,
			isCursor: !downsampled && m.pulseFocus && i == pulseCursor-(len(m.metrics)-windowSize),
		}
	}

	barStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	cursorStyle := lipgloss.NewStyle().Background(lipgloss.Color("212")).Foreground(lipgloss.Color("0"))
	gridStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("238"))

	var sb strings.Builder
	sb.WriteString(statsLine + "\n\n")

	for row := 0; row < chartHeight; row++ {
		isGrid := gridRows[row]

		// Y-axis prefix.
		if lv, ok := labeledRows[row]; ok {
			if isGrid {
				sb.WriteString(subtleStyle.Render(fmt.Sprintf("%6.1f ┼", lv)))
			} else {
				sb.WriteString(subtleStyle.Render(fmt.Sprintf("%6.1f ┤", lv)))
			}
		} else if isGrid {
			sb.WriteString(subtleStyle.Render("       ┼"))
		} else {
			sb.WriteString(subtleStyle.Render("       │"))
		}

		// Chart area.
		for ci := 0; ci < len(cols); ci++ {
			cd := cols[ci]
			bottomOffset := (chartHeight - 1 - row) * 8
			var char string
			switch {
			case cd.filled <= bottomOffset:
				if isGrid {
					char = "·"
				} else {
					char = " "
				}
			case cd.filled >= bottomOffset+8:
				char = "█"
			default:
				char = barEighths[cd.filled-bottomOffset]
			}
			switch {
			case cd.isCursor:
				sb.WriteString(cursorStyle.Render(char))
			case isGrid && char == "·":
				sb.WriteString(gridStyle.Render(char))
			default:
				sb.WriteString(barStyle.Render(char))
			}
		}
		// Pad to full chart width.
		for ci := len(cols); ci < chartWidth; ci++ {
			if isGrid {
				sb.WriteString(gridStyle.Render("·"))
			} else {
				sb.WriteString(" ")
			}
		}
		sb.WriteString("\n")
	}

	// X-axis separator.
	sb.WriteString(subtleStyle.Render("       └"+strings.Repeat("─", chartWidth)) + "\n")

	// X-axis timestamp labels.
	xLabels := make([]byte, yAxisWidth+chartWidth)
	for i := range xLabels {
		xLabels[i] = ' '
	}
	if len(data) > 1 {
		numLabels := chartWidth / 12
		if numLabels < 2 {
			numLabels = 2
		}
		if numLabels > 7 {
			numLabels = 7
		}
		for i := 0; i <= numLabels; i++ {
			ci := i * (len(data) - 1) / numLabels
			if ci >= len(data) {
				ci = len(data) - 1
			}
			ts := data[ci].Timestamp.Format("15:04")
			pos := yAxisWidth + ci - 2
			if pos < yAxisWidth {
				pos = yAxisWidth
			}
			for j := 0; j < len(ts) && pos+j < len(xLabels); j++ {
				xLabels[pos+j] = ts[j]
			}
		}
	}
	sb.WriteString(subtleStyle.Render(string(xLabels)) + "\n\n")

	// Footer.
	sb.WriteString(subtleStyle.Render("P/Esc: exit   Tab: scrub   h/l: move cursor   =/-: zoom"))

	return sb.String()
}

func hashString(s string) int {
	h := 0
	for _, c := range s {
		h = 31*h + int(c)
	}
	if h < 0 {
		h = -h
	}
	return h
}

func truncate(s string, l int) string {
	if len(s) <= l {
		return s
	}
	return s[:l-1] + "…"
}

type cliConfig struct {
	pulseCmd      string
	sources       []logSource
	incidentMode  bool
	exportPath    string
	exportFormat  string
	demo          bool
	dockerService string
	kubeSelector  string
	journalUnit   string
	ghaRun        string
	session       string
	desc          string
	pulseInterval int
}

func parseCLI(args []string) (cliConfig, error) {
	cfg := cliConfig{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--pulse", "-p":
			if i+1 >= len(args) {
				return cfg, fmt.Errorf("missing value for %s", args[i])
			}
			cfg.pulseCmd = args[i+1]
			i++
		case "--incident-mode":
			cfg.incidentMode = true
		case "--export":
			if i+1 >= len(args) {
				return cfg, fmt.Errorf("missing value for --export")
			}
			cfg.exportPath = args[i+1]
			i++
		case "--format":
			if i+1 >= len(args) {
				return cfg, fmt.Errorf("missing value for --format")
			}
			cfg.exportFormat = strings.ToLower(args[i+1])
			i++
		case "--demo":
			cfg.demo = true
		case "--docker":
			if i+1 >= len(args) {
				return cfg, fmt.Errorf("missing value for --docker")
			}
			cfg.dockerService = args[i+1]
			i++
		case "--kube-selector":
			if i+1 >= len(args) {
				return cfg, fmt.Errorf("missing value for --kube-selector")
			}
			cfg.kubeSelector = args[i+1]
			i++
		case "--journal-unit":
			if i+1 >= len(args) {
				return cfg, fmt.Errorf("missing value for --journal-unit")
			}
			cfg.journalUnit = args[i+1]
			i++
		case "--gha-run":
			if i+1 >= len(args) {
				return cfg, fmt.Errorf("missing value for --gha-run")
			}
			cfg.ghaRun = args[i+1]
			i++
		case "--session":
			if i+1 >= len(args) {
				return cfg, fmt.Errorf("missing value for --session")
			}
			cfg.session = args[i+1]
			i++
		case "--desc":
			if i+1 >= len(args) {
				return cfg, fmt.Errorf("missing value for --desc")
			}
			cfg.desc = args[i+1]
			i++
		case "--pulse-interval":
			if i+1 >= len(args) {
				return cfg, fmt.Errorf("missing value for --pulse-interval")
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n < 1 {
				return cfg, fmt.Errorf("--pulse-interval must be a positive integer")
			}
			cfg.pulseInterval = n
			i++
		default:
			src := logSource{arg: args[i]}
			if m := labeledSourceRe.FindStringSubmatch(args[i]); m != nil {
				src.label = m[1]
				src.arg = m[2]
			}
			cfg.sources = append(cfg.sources, src)
		}
	}
	if cfg.exportPath != "" && cfg.exportFormat == "" {
		cfg.exportFormat = "json"
	}
	if cfg.exportFormat != "" && cfg.exportFormat != "json" && cfg.exportFormat != "txt" {
		return cfg, fmt.Errorf("invalid --format %q (use json or txt)", cfg.exportFormat)
	}
	cfg.sources = append(cfg.sources, integrationSources(cfg)...)
	return cfg, nil
}

func integrationSources(cfg cliConfig) []logSource {
	var sources []logSource
	if cfg.dockerService != "" {
		sources = append(sources, logSource{
			label: cfg.dockerService,
			arg:   fmt.Sprintf("docker logs -f %s", cfg.dockerService),
		})
	}
	if cfg.kubeSelector != "" {
		sources = append(sources, logSource{
			arg: fmt.Sprintf(`kubectl logs -f -l "%s" --all-containers=true --prefix=true`, cfg.kubeSelector),
		})
	}
	if cfg.journalUnit != "" {
		sources = append(sources, logSource{
			label: cfg.journalUnit,
			arg:   fmt.Sprintf("journalctl -f -u %s -o short-iso", cfg.journalUnit),
		})
	}
	if cfg.ghaRun != "" {
		sources = append(sources, logSource{
			arg: fmt.Sprintf("gh run view %s --log", cfg.ghaRun),
		})
	}
	return sources
}

// batchSource reads from lineCh and forwards to the TUI using a leading-edge
// + cooldown strategy: the first line of any burst is sent immediately (0ms
// latency), subsequent lines within the batchCooldown window are accumulated
// and sent as a single logEntryBatch (capping renders at ~20 fps).
func batchSource(p *tea.Program, source string, lineCh <-chan string) {
	var batch logEntryBatch
	var cooldownC <-chan time.Time
	for {
		select {
		case txt, ok := <-lineCh:
			if !ok {
				if len(batch) > 0 {
					p.Send(batch)
				}
				return
			}
			entry := newLogEntry(source, txt)
			if cooldownC == nil {
				// Leading edge: send this line immediately, start cooldown.
				p.Send(logEntryBatch{entry})
				cooldownC = time.After(batchCooldown)
			} else {
				batch = append(batch, entry)
			}
		case <-cooldownC:
			if len(batch) > 0 {
				p.Send(batch)
				batch = nil
			}
			cooldownC = nil
		}
	}
}

func startSource(p *tea.Program, src logSource) {
	info, err := os.Stat(src.arg)
	if err == nil && !info.IsDir() {
		label := src.label
		if label == "" {
			label = filepath.Base(src.arg)
		}
		go func(filename, source string) {
			t, _ := tail.TailFile(filename, tail.Config{Follow: true, ReOpen: true, Logger: tail.DiscardingLogger, MustExist: true})
			lineCh := make(chan string, 512)
			go func() {
				for line := range t.Lines {
					lineCh <- line.Text
				}
				close(lineCh)
			}()
			batchSource(p, source, lineCh)
		}(src.arg, label)
		return
	}
	label := src.label
	if label == "" {
		label = src.arg
	}
	go func(cmdLine, source string) {
		cmd := exec.Command("sh", "-c", cmdLine)
		stdout, _ := cmd.StdoutPipe()
		cmd.Stderr = cmd.Stdout
		cmd.Start()
		lineCh := make(chan string, 512)
		go func() {
			scanner := bufio.NewScanner(stdout)
			scanner.Buffer(make([]byte, scannerBufSize), scannerBufSize)
			for scanner.Scan() {
				lineCh <- scanner.Text()
			}
			close(lineCh)
		}()
		batchSource(p, source, lineCh)
		cmd.Wait()
	}(src.arg, label)
}

func runInternalDemo(p *tea.Program) {
	endpoints := []string{"/v1/user", "/v1/auth", "/v1/payment", "/v1/search", "/v1/health"}
	methods := []string{"GET", "POST", "PUT", "DELETE"}
	sources := []string{"api.log", "db.log", "worker.log"}

	// Metrics loop
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		for t := range ticker.C {
			val := 40 + 30*math.Sin(float64(t.Unix())/10.0) + rand.Float64()*5
			p.Send(MetricEntry{Timestamp: time.Now(), Value: val})
		}
	}()

	// Logs loop
	go func() {
		for {
			reqID := fmt.Sprintf("req-%d", rand.Intn(1000000))
			userID := fmt.Sprintf("user-%d", rand.Intn(1000))
			ip := fmt.Sprintf("192.168.1.%d", rand.Intn(254))

			// API Request
			p.Send(LogEntry{
				Timestamp: time.Now(),
				Source:    sources[0],
				Content:   fmt.Sprintf("[INFO] %s %s - remote_addr=%s request_id=%s user_id=%s", randHelper.Choice(methods), randHelper.Choice(endpoints), ip, reqID, userID),
				Entities:  []string{reqID, userID, ip},
			})

			time.Sleep(time.Duration(rand.Intn(200)) * time.Millisecond)

			// DB Query
			if rand.Float32() > 0.1 {
				p.Send(LogEntry{
					Timestamp: time.Now(),
					Source:    sources[1],
					Content:   fmt.Sprintf("[DEBUG] query=\"SELECT * FROM accounts WHERE id='%s'\" request_id=%s", userID, reqID),
					Entities:  []string{reqID, userID},
				})
			} else {
				p.Send(LogEntry{
					Timestamp: time.Now(),
					Source:    sources[1],
					Content:   fmt.Sprintf("[ERROR] connection pool exhausted for request_id=%s", reqID),
					Entities:  []string{reqID},
				})
			}

			if rand.Float32() > 0.7 {
				time.Sleep(100 * time.Millisecond)
				p.Send(LogEntry{
					Timestamp: time.Now(),
					Source:    sources[2],
					Content:   fmt.Sprintf("[INFO] processing background task for request_id=%s", reqID),
					Entities:  []string{reqID},
				})
			}

			time.Sleep(time.Duration(rand.Intn(800)) * time.Millisecond)
		}
	}()
}

// Add a simple Choice helper for random strings
type randomHelper struct{}

func (randomHelper) Choice(s []string) string {
	return s[rand.Intn(len(s))]
}

var randHelper = randomHelper{}

type exportIncident struct {
	Signature string         `json:"signature"`
	Count     int            `json:"count"`
	LastSeen  time.Time      `json:"last_seen"`
	Sources   map[string]int `json:"sources"`
}

type exportPayload struct {
	GeneratedAt  time.Time        `json:"generated_at"`
	Entries      int              `json:"entries"`
	Sources      int              `json:"sources"`
	IncidentMode bool             `json:"incident_mode"`
	Incidents    []exportIncident `json:"incidents,omitempty"`
}

func writeExport(path, format string, m model) error {
	sourceSet := map[string]struct{}{}
	for _, e := range m.entries {
		sourceSet[e.Source] = struct{}{}
	}
	payload := exportPayload{
		GeneratedAt:  time.Now(),
		Entries:      len(m.entries),
		Sources:      len(sourceSet),
		IncidentMode: m.incidentMode,
	}
	for _, inc := range m.topIncidents(1000) {
		payload.Incidents = append(payload.Incidents, exportIncident{
			Signature: inc.Signature,
			Count:     inc.Count,
			LastSeen:  inc.LastSeen,
			Sources:   inc.Sources,
		})
	}

	switch format {
	case "json":
		b, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(path, b, 0o644)
	case "txt":
		var b strings.Builder
		b.WriteString("LogLink Export\n")
		b.WriteString(fmt.Sprintf("generated_at: %s\nentries: %d\nsources: %d\nincident_mode: %v\n", payload.GeneratedAt.Format(time.RFC3339), payload.Entries, payload.Sources, payload.IncidentMode))
		if len(payload.Incidents) > 0 {
			b.WriteString("\nTop incidents:\n")
			for _, inc := range payload.Incidents {
				b.WriteString(fmt.Sprintf("- %dx %s (last_seen=%s)\n", inc.Count, inc.Signature, inc.LastSeen.Format(time.RFC3339)))
			}
		}
		return os.WriteFile(path, []byte(b.String()), 0o644)
	default:
		return fmt.Errorf("unsupported export format %q", format)
	}
}

func main() {
	rand.Seed(time.Now().UnixNano())
	showHelp := false
	for _, arg := range os.Args[1:] {
		if arg == "--help" || arg == "-h" {
			showHelp = true
			break
		}
	}

	if showHelp {
		fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Bold(true).Render("🚀 LogLink - Semantic Log Aggregator & Pulse Sync"))
		fmt.Println("\nLogLink is a high-performance terminal tool for aggregating logs from multiple")
		fmt.Println("sources and correlating them with real-time metrics (Pulse).")

		fmt.Println(lipgloss.NewStyle().Bold(true).Underline(true).Render("\nUSAGE:"))
		fmt.Println("  loglink [source1] [source2] ... [flags]")

		fmt.Println(lipgloss.NewStyle().Bold(true).Underline(true).Render("\nLOG SOURCES:"))
		fmt.Println("  Sources can be file paths or shell commands, optionally prefixed with a label.")
		fmt.Println("  - File path:         loglink /var/log/syslog")
		fmt.Println("  - Labeled file:      loglink api=/var/log/api.log")
		fmt.Println("  - Shell command:     loglink \"ssh host 'tail -f /app/api.log'\"")
		fmt.Println("  - Labeled command:   loglink api=\"kubectl logs -f deploy/api\"")

		fmt.Println(lipgloss.NewStyle().Bold(true).Underline(true).Render("\nFLAGS:"))
		fmt.Println("  --pulse, -p <cmd>        Execute <cmd> periodically and plot numeric output as a sparkline.")
		fmt.Println("  --pulse-interval <n>     Pulse polling interval in seconds (default: 2). Saves battery.")
		fmt.Println("  --incident-mode          Cluster error patterns automatically and show in sidebar.")
		fmt.Println("  --session <name>         Load/save a named source session (default: \"default\").")
		fmt.Println("  --desc <text>            Set a human-readable description for the session.")
		fmt.Println("  --export <path>          Save a session summary (incidents, counts) to <path> on exit.")
		fmt.Println("  --format <json|txt>      Set export format (default: json).")
		fmt.Println("  --demo                   Launch with built-in log simulator and dummy metrics.")

		fmt.Println(lipgloss.NewStyle().Bold(true).Underline(true).Render("\nINTEGRATIONS:"))
		fmt.Println("  --docker <container>     Shortcut for 'docker logs -f <container>'")
		fmt.Println("  --kube-selector <label>  Shortcut for 'kubectl logs -f -l <label> --all-containers'")
		fmt.Println("  --journal-unit <unit>    Shortcut for 'journalctl -f -u <unit> -o short-iso'")
		fmt.Println("  --gha-run <id>           Shortcut for 'gh run view <id> --log'")

		fmt.Println(lipgloss.NewStyle().Bold(true).Underline(true).Render("\nEXAMPLES:"))
		fmt.Println("  # Label sources for a readable source column")
		fmt.Println("  loglink api=api.log worker=worker.log db=db.log")
		fmt.Println("\n  # Mix labeled files and labeled commands")
		fmt.Println("  loglink api=\"kubectl logs -f deploy/api\" db=\"kubectl logs -f deploy/postgres\"")
		fmt.Println("\n  # Debug K8s pods with incident detection")
		fmt.Println("  loglink --kube-selector app=api --incident-mode")
		fmt.Println("\n  # Correlate Docker logs with container CPU usage")
		fmt.Println("  loglink --docker web --pulse \"docker stats web --no-stream --format '{{.CPUPerc}}' | tr -d '%'\"")

		fmt.Println("\nPress '?' inside the application for a full list of keybindings.")
		os.Exit(0)
	}

	cfg, err := parseCLI(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Resolve session name (default: "default").
	sessionName := cfg.session
	if sessionName == "" {
		sessionName = "default"
	}

	// Load the stored session — used for restoring sources, description, history.
	store := loadSessionStore()
	storedSess := store.Sessions[sessionName]
	sessionDesc := storedSess.Description
	incidentHistory := storedSess.IncidentHistory

	// If no sources were given on the CLI, restore from the saved session.
	if len(cfg.sources) == 0 && !cfg.demo {
		for _, ss := range storedSess.Sources {
			cfg.sources = append(cfg.sources, logSource{label: ss.Label, arg: ss.Arg})
		}
		if !cfg.incidentMode {
			cfg.incidentMode = storedSess.IncidentMode
		}
		if cfg.pulseCmd == "" {
			cfg.pulseCmd = storedSess.PulseCmd
		}
		if cfg.pulseInterval == 0 {
			cfg.pulseInterval = storedSess.PulseInterval
		}
		if len(cfg.sources) == 0 {
			fmt.Fprintf(os.Stderr, "Session %q has no sources. Provide sources on the command line or press 'a' inside the app.\n", sessionName)
			fmt.Fprintf(os.Stderr, "Run 'loglink --help' for usage.\n")
			os.Exit(1)
		}
	}

	p := tea.NewProgram(initialModel(cfg.incidentMode, sessionName, sessionDesc, cfg.sources, incidentHistory, cfg.pulseInterval), tea.WithAltScreen())
	program = p // must be set before goroutines call startSource

	if cfg.demo {
		runInternalDemo(p)
	}

	if cfg.pulseCmd != "" {
		if cfg.pulseInterval < 1 {
			cfg.pulseInterval = 2
		}
		go func(cmdLine string, interval time.Duration) {
			ticker := time.NewTicker(interval)
			for range ticker.C {
				cmd := exec.Command("sh", "-c", cmdLine)
				out, err := cmd.Output()
				if err == nil {
					val, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
					if err == nil {
						p.Send(MetricEntry{Timestamp: time.Now(), Value: val})
					}
				}
			}
		}(cfg.pulseCmd, time.Duration(cfg.pulseInterval)*time.Second)
	}

	for _, src := range cfg.sources {
		startSource(p, src)
	}

	finalModel, _ := p.Run()
	if fm, ok := finalModel.(model); ok {
		// Save session — sources, incidents, pulse cmd, description.
		if err := saveSession(sessionName, fm.activeSources, fm.incidentMode, cfg.pulseCmd, cfg.pulseInterval, cfg.desc, fm.incidents); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not save session: %v\n", err)
		}
		if cfg.exportPath != "" {
			if err := writeExport(cfg.exportPath, cfg.exportFormat, fm); err != nil {
				fmt.Fprintf(os.Stderr, "Export failed: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Export written to %s\n", cfg.exportPath)
		}
	}
}

func findEntities(content string) []string {
	return entityRegex.FindAllString(content, -1)
}

func normalizeLevel(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "error", "err":
		return "error"
	case "fatal":
		return "fatal"
	case "panic":
		return "panic"
	case "critical", "crit":
		return "critical"
	case "warn", "warning":
		return "warn"
	case "debug":
		return "debug"
	case "trace":
		return "trace"
	default:
		return "info"
	}
}

func formatJSONValue(v interface{}) string {
	switch tv := v.(type) {
	case string:
		return tv
	case float64:
		if tv == float64(int64(tv)) {
			return strconv.FormatInt(int64(tv), 10)
		}
		return strconv.FormatFloat(tv, 'f', -1, 64)
	case bool:
		if tv {
			return "true"
		}
		return "false"
	case nil:
		return ""
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

// tryParseJSON detects a JSON object anywhere in the line (handles kubectl --prefix=true prefixes),
// extracts structured fields, and returns a human-readable display string + metadata.
func tryParseJSON(content string) (msg, level string, ts time.Time, hasTS bool, entities []string, display string, ok bool) {
	idx := strings.IndexByte(content, '{')
	if idx == -1 {
		return
	}
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(content[idx:]), &raw); err != nil {
		return
	}

	seen := make(map[string]bool)

	// Message
	for _, k := range []string{"msg", "message", "MESSAGE", "log", "body", "text"} {
		if v, okv := raw[k]; okv {
			msg = fmt.Sprintf("%v", v)
			seen[k] = true
			break
		}
	}

	// Level
	for _, k := range []string{"level", "Level", "LEVEL", "severity", "lvl"} {
		if v, okv := raw[k]; okv {
			level = normalizeLevel(fmt.Sprintf("%v", v))
			seen[k] = true
			break
		}
	}

	// Timestamp
	for _, k := range []string{"ts", "time", "timestamp", "TIME", "@timestamp"} {
		if v, okv := raw[k]; okv {
			seen[k] = true
			switch tv := v.(type) {
			case string:
				for _, f := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
					if t, err := time.Parse(f, tv); err == nil {
						ts = t
						hasTS = true
						break
					}
				}
			case float64:
				ts = time.Unix(int64(tv), 0)
				hasTS = true
			}
			break
		}
	}

	// Error field
	errMsg := ""
	for _, k := range []string{"error", "err", "Error"} {
		if v, okv := raw[k]; okv && v != nil {
			s := fmt.Sprintf("%v", v)
			if s != "" && s != "<nil>" {
				errMsg = s
				seen[k] = true
			}
			break
		}
	}

	// Correlation/trace entities (extracted for causal linking)
	for _, k := range []string{"trace_id", "traceID", "traceId", "traceid", "request_id", "requestId", "req_id", "span_id", "spanId", "spanID", "correlation_id"} {
		if v, okv := raw[k]; okv {
			val := fmt.Sprintf("%v", v)
			if val != "" && val != "<nil>" {
				entities = append(entities, val)
				seen[k] = true
			}
		}
	}

	// Build display: msg  [err=...]  key=val key=val (remaining fields, sorted)
	var sb strings.Builder
	if msg != "" {
		sb.WriteString(msg)
	} else if idx > 0 {
		// Preserve any non-JSON prefix (e.g. kubectl pod prefix)
		sb.WriteString(strings.TrimSpace(content[:idx]))
	}
	if errMsg != "" {
		if sb.Len() > 0 {
			sb.WriteString("  ")
		}
		sb.WriteString("err=" + errMsg)
	}

	remaining := make([]string, 0, len(raw))
	for k := range raw {
		if !seen[k] {
			remaining = append(remaining, k)
		}
	}
	sort.Strings(remaining)
	for _, k := range remaining {
		val := formatJSONValue(raw[k])
		if val == "" {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString("  ")
		}
		sb.WriteString(k + "=" + val)
	}

	if sb.Len() == 0 {
		return msg, level, ts, hasTS, entities, "", false
	}
	display = sb.String()
	ok = true
	return
}

func newLogEntry(source, text string) LogEntry {
	ts := parseTimestamp(text)
	entities := findEntities(text)
	content := text
	level := ""

	if _, parsedLevel, parsedTS, hasTS, traceEntities, display, ok := tryParseJSON(text); ok {
		content = display
		level = parsedLevel
		if hasTS {
			ts = parsedTS
		}
		// Prepend parsed trace entities so they're first in the list (highest priority for Enter/s)
		entities = append(traceEntities, entities...)
	}
	// Always resolve level at parse time so renderLogs never calls detectLogLevel per frame.
	if level == "" {
		level = detectLogLevel(content)
	}

	return LogEntry{
		Timestamp: ts,
		Source:    source,
		Content:   content,
		Entities:  entities,
		Level:     level,
	}
}

func parseTimestamp(content string) time.Time {
	match := tsRegex.FindString(content)
	if match == "" {
		return time.Now()
	}
	formats := []string{time.RFC3339, "2006-01-02T15:04:05", "15:04:05", "2006/01/02 15:04:05"}
	for _, f := range formats {
		t, err := time.Parse(f, match)
		if err == nil {
			if f == "15:04:05" {
				now := time.Now()
				return time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.Local)
			}
			return t
		}
	}
	return time.Now()
}
