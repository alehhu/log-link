package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/exec"
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
	entityRegex   = regexp.MustCompile(`([0-9a-fA-F-]{8,}|req-[a-zA-Z0-9]+|\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})`)
	tsRegex       = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}|\d{2}:\d{2}:\d{2}|\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2})`)
	fileLineRegex = regexp.MustCompile(`([a-zA-Z0-9._/-]+\.[a-z]{1,4}):(\d+)`)
)

type keyMap struct {
	Up        key.Binding
	Down      key.Binding
	PageUp    key.Binding
	PageDown  key.Binding
	Top       key.Binding
	Bottom    key.Binding
	Follow    key.Binding
	Highlight key.Binding
	Focus     key.Binding
	Bookmark  key.Binding
	NextMark  key.Binding
	PrevMark  key.Binding
	Source    key.Binding
	Pulse     key.Binding
	ScrubBack key.Binding
	ScrubFwd  key.Binding
	Sidebar   key.Binding
	Help      key.Binding
	Quit      key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Help, k.Sidebar, k.Follow, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.PageUp, k.PageDown, k.Top, k.Bottom},
		{k.Highlight, k.Focus, k.Bookmark, k.NextMark, k.PrevMark},
		{k.Pulse, k.ScrubBack, k.ScrubFwd, k.Source, k.Sidebar, k.Follow, k.Quit},
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
	Sidebar:   key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "details")),
	Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
}

type MetricEntry struct {
	Timestamp time.Time
	Value     float64
}

type Incident struct {
	Signature string
	Count     int
	LastSeen  time.Time
	Sources   map[string]int
}

type LogEntry struct {
	Timestamp time.Time
	Source    string
	Content   string
	Entities  []string
}

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
}

func initialModel(incidentMode bool) model {
	return model{
		entries:      []LogEntry{},
		cursor:       0,
		follow:       true,
		sidebarOpen:  false,
		incidentMode: incidentMode,
		incidents:    map[string]*Incident{},
		startedAt:    time.Now(),
		help:         help.New(),
		lastKey:      "",
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

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
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
		case msg.String() == "esc":
			m.highlightID = ""
			m.focusID = ""
			m.pulseFocus = false
			m.showHelp = false
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

	case LogEntry:
		idx := len(m.entries)
		for i := len(m.entries) - 1; i >= 0; i-- {
			if m.entries[i].Timestamp.Before(msg.Timestamp) || m.entries[i].Timestamp.Equal(msg.Timestamp) {
				idx = i + 1
				break
			}
			if i == 0 {
				idx = 0
			}
		}
		for i := range m.bookmarks {
			if m.bookmarks[i] >= idx {
				m.bookmarks[i]++
			}
		}
		if idx == len(m.entries) {
			m.entries = append(m.entries, msg)
		} else {
			m.entries = append(m.entries, LogEntry{})
			copy(m.entries[idx+1:], m.entries[idx:])
			m.entries[idx] = msg
		}
		if m.incidentMode {
			m.updateIncidents(msg)
		}
		m.updateFilteredEntries()
		ce := m.currentEntries()
		if m.follow && len(ce) > 0 {
			m.cursor = len(ce) - 1
		}
		return m, nil

	case MetricEntry:
		m.metrics = append(m.metrics, msg)
		if len(m.metrics) > 200 {
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
	lower := strings.ToLower(entry.Content)
	if !(strings.Contains(lower, "error") || strings.Contains(lower, "panic") || strings.Contains(lower, "fatal") || strings.Contains(lower, "timeout")) {
		return
	}
	sig := incidentSignature(entry.Content)
	if sig == "" {
		return
	}
	inc, ok := m.incidents[sig]
	if !ok {
		inc = &Incident{Signature: sig, Sources: map[string]int{}}
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
	subtleStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	sourceStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("63")).Bold(true)
	contentStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	highlightStyle = lipgloss.NewStyle().Background(lipgloss.Color("57")).Foreground(lipgloss.Color("255"))
	selectedStyle  = lipgloss.NewStyle().Background(lipgloss.Color("236"))
	focusStyle     = lipgloss.NewStyle().Background(lipgloss.Color("212")).Foreground(lipgloss.Color("0")).Bold(true)
	sidebarStyle   = lipgloss.NewStyle().Padding(1).Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("62"))
	titleStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229")).Background(lipgloss.Color("62")).Padding(0, 1)
	badgeOnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("42")).Bold(true).Padding(0, 1)
	badgeOffStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Background(lipgloss.Color("240")).Padding(0, 1)
)

func (m model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	if m.showHelp {
		return m.renderHelp()
	}

	var s strings.Builder

	// Pulse Graph
	if len(m.metrics) > 0 {
		s.WriteString(m.renderSparkline())
		s.WriteString("\n")
	}

	// Main Layout
	mainView := m.renderLogs()
	s.WriteString(mainView)

	// Persistent Incident Leaderboard (if active)
	if m.incidentMode && len(m.incidents) > 0 {
		s.WriteString("\n" + m.renderIncidentBar())
	}

	// Footer
	s.WriteString("\n" + m.help.View(keys))

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
	header := titleStyle.Render(" LOGLINK ")
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
	s.WriteString(header + stats + "\n\n")

	width := m.viewportW
	height := m.viewportH - 6
	if len(m.metrics) > 0 {
		height -= 2
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
	b.WriteString(subtleStyle.Render("Time:   ") + entry.Timestamp.Format("2006-01-02 15:04:05.000") + "\n\n")

	if len(entry.Entities) > 0 {
		b.WriteString(lipgloss.NewStyle().Bold(true).Render("ENTITIES DETECTED:") + "\n")
		for _, e := range entry.Entities {
			b.WriteString("• " + e + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString(lipgloss.NewStyle().Bold(true).Render("RAW CONTENT:") + "\n")
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

func (m model) renderSparkline() string {
	if len(m.metrics) == 0 {
		return ""
	}

	width := m.viewportW - 20
	if width < 10 {
		width = 10
	}
	data := m.metrics
	if len(data) > width {
		data = data[len(data)-width:]
	}
	if m.pulseCursor < 0 {
		m.pulseCursor = 0
	}
	if m.pulseCursor >= len(m.metrics) {
		m.pulseCursor = len(m.metrics) - 1
	}

	maxVal, minVal, curVal := 0.0, 999999.0, 0.0
	for _, mt := range data {
		if mt.Value > maxVal {
			maxVal = mt.Value
		}
		if mt.Value < minVal {
			minVal = mt.Value
		}
		curVal = mt.Value
	}

	var s strings.Builder
	s.WriteString(fmt.Sprintf("Pulse: %0.2f [min:%0.1f max:%0.1f] ", curVal, minVal, maxVal))
	for i, met := range data {
		charIdx := 0
		if maxVal-minVal > 0 {
			charIdx = int(((met.Value - minVal) / (maxVal - minVal)) * float64(len(sparklineChars)-1))
		}
		char := sparklineChars[charIdx]
		style := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
		isCursor := (m.pulseFocus && i == m.pulseCursor-(len(m.metrics)-len(data)))
		if isCursor {
			style = style.Background(lipgloss.Color("212")).Foreground(lipgloss.Color("0"))
		}
		s.WriteString(style.Render(char))
	}
	return s.String()
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
	sources       []string
	incidentMode  bool
	exportPath    string
	exportFormat  string
	demo          bool
	dockerService string
	kubeSelector  string
	journalUnit   string
	ghaRun        string
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
		default:
			cfg.sources = append(cfg.sources, args[i])
		}
	}
	if cfg.exportPath != "" && cfg.exportFormat == "" {
		cfg.exportFormat = "json"
	}
	if cfg.exportFormat != "" && cfg.exportFormat != "json" && cfg.exportFormat != "txt" {
		return cfg, fmt.Errorf("invalid --format %q (use json or txt)", cfg.exportFormat)
	}
	cfg.sources = append(cfg.sources, integrationSources(cfg)...)
	if len(cfg.sources) == 0 && !cfg.demo {
		return cfg, fmt.Errorf("no log sources provided")
	}
	return cfg, nil
}

func integrationSources(cfg cliConfig) []string {
	sources := []string{}
	if cfg.dockerService != "" {
		sources = append(sources, fmt.Sprintf("docker logs -f %s", cfg.dockerService))
	}
	if cfg.kubeSelector != "" {
		sources = append(sources, fmt.Sprintf(`kubectl logs -f -l "%s" --all-containers=true --prefix=true`, cfg.kubeSelector))
	}
	if cfg.journalUnit != "" {
		sources = append(sources, fmt.Sprintf("journalctl -f -u %s -o short-iso", cfg.journalUnit))
	}
	if cfg.ghaRun != "" {
		sources = append(sources, fmt.Sprintf("gh run view %s --log", cfg.ghaRun))
	}
	return sources
}

func startSource(p *tea.Program, arg string) {
	info, err := os.Stat(arg)
	if err == nil && !info.IsDir() {
		go func(filename string) {
			t, _ := tail.TailFile(filename, tail.Config{Follow: true, ReOpen: true, Logger: tail.DiscardingLogger, MustExist: true})
			for line := range t.Lines {
				p.Send(LogEntry{Timestamp: parseTimestamp(line.Text), Source: filename, Content: line.Text, Entities: findEntities(line.Text)})
			}
		}(arg)
		return
	}
	go func(cmdLine string) {
		cmd := exec.Command("sh", "-c", cmdLine)
		stdout, _ := cmd.StdoutPipe()
		cmd.Stderr = cmd.Stdout
		cmd.Start()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			txt := scanner.Text()
			p.Send(LogEntry{Timestamp: parseTimestamp(txt), Source: cmdLine, Content: txt, Entities: findEntities(txt)})
		}
		cmd.Wait()
	}(arg)
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
	if len(os.Args) < 2 {
		showHelp = true
	}
	for _, arg := range os.Args {
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
		fmt.Println("  Sources can be file paths, shell commands, or integration shortcuts.")
		fmt.Println("  - File path:       loglink /var/log/syslog")
		fmt.Println("  - Shell command:   loglink \"ssh remote-host 'tail -f /app/api.log'\"")

		fmt.Println(lipgloss.NewStyle().Bold(true).Underline(true).Render("\nFLAGS:"))
		fmt.Println("  --pulse, -p <cmd>        Execute <cmd> every second and plot numeric output as a sparkline.")
		fmt.Println("  --incident-mode          Cluster error patterns automatically and show in sidebar.")
		fmt.Println("  --export <path>          Save a session summary (incidents, counts) to <path> on exit.")
		fmt.Println("  --format <json|txt>      Set export format (default: json).")
		fmt.Println("  --demo                   Launch with built-in log simulator and dummy metrics.")

		fmt.Println(lipgloss.NewStyle().Bold(true).Underline(true).Render("\nINTEGRATIONS:"))
		fmt.Println("  --docker <container>     Shortcut for 'docker logs -f <container>'")
		fmt.Println("  --kube-selector <label>  Shortcut for 'kubectl logs -f -l <label> --all-containers'")
		fmt.Println("  --journal-unit <unit>    Shortcut for 'journalctl -f -u <unit> -o short-iso'")
		fmt.Println("  --gha-run <id>           Shortcut for 'gh run view <id> --log'")

		fmt.Println(lipgloss.NewStyle().Bold(true).Underline(true).Render("\nEXAMPLES:"))
		fmt.Println("  # Monitor local and remote logs together")
		fmt.Println("  loglink app.log \"ssh worker-1 'tail -f /var/log/worker.log'\"")
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

	p := tea.NewProgram(initialModel(cfg.incidentMode), tea.WithAltScreen())

	if cfg.demo {
		runInternalDemo(p)
	}

	if cfg.pulseCmd != "" {
		go func(cmdLine string) {
			ticker := time.NewTicker(1 * time.Second)
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
		}(cfg.pulseCmd)
	}

	for _, arg := range cfg.sources {
		startSource(p, arg)
	}

	finalModel, _ := p.Run()
	if cfg.exportPath != "" {
		if fm, ok := finalModel.(model); ok {
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
