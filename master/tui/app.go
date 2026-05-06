package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"master/lib"
	"master/monitoring"
)

type tab int

const (
	tabWorkers tab = iota
	tabInflight
	tabLogs
	tabStats
)

type snapshot struct {
	workers  []lib.WorkerSnapshot
	inflight []monitoring.InFlight
	logs     []monitoring.LogEntry
	recent   []monitoring.CompletedFlight
	strategy lib.Strategy
	uptime   time.Duration
}

type model struct {
	router     *lib.Router
	start      time.Time
	activeTab  tab
	selected   int
	inputMode  string
	inputValue string
	status     string
	lastError  string
	snap       snapshot
	width      int
	height     int
	tickEvery  time.Duration
}

type tickMsg time.Time
type refreshMsg snapshot
type errMsg error

func NewModel(router *lib.Router) tea.Model {
	return model{
		router:    router,
		start:     time.Now(),
		activeTab: tabWorkers,
		tickEvery: 700 * time.Millisecond,
		status:    "ready",
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.fetchSnapshotCmd(), tea.Tick(m.tickEvery, func(t time.Time) tea.Msg { return tickMsg(t) }))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tickMsg:
		return m, tea.Batch(m.fetchSnapshotCmd(), tea.Tick(m.tickEvery, func(t time.Time) tea.Msg { return tickMsg(t) }))
	case refreshMsg:
		m.snap = snapshot(msg)
		if m.selected >= len(m.snap.workers) {
			m.selected = max(0, len(m.snap.workers)-1)
		}
		return m, nil
	case errMsg:
		m.lastError = msg.Error()
		return m, nil
	case tea.KeyMsg:
		if m.inputMode != "" {
			return m.handleInputMode(msg)
		}

		s := msg.String()
		switch s {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "1":
			m.activeTab = tabWorkers
		case "2":
			m.activeTab = tabInflight
		case "3":
			m.activeTab = tabLogs
		case "4":
			m.activeTab = tabStats
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected < len(m.snap.workers)-1 {
				m.selected++
			}
		case "r":
			m.status = "refreshing"
			return m, m.fetchSnapshotCmd()
		case "a":
			m.inputMode = "add"
			m.inputValue = ""
			m.status = "enter worker address (host:port), then Enter"
		case "d":
			if len(m.snap.workers) == 0 {
				return m, nil
			}
			w := m.snap.workers[m.selected]
			if err := m.router.DrainWorker(w.ID); err != nil {
				m.lastError = err.Error()
			} else {
				m.status = "draining " + w.ID
			}
			return m, m.fetchSnapshotCmd()
		case "x":
			if len(m.snap.workers) == 0 {
				return m, nil
			}
			w := m.snap.workers[m.selected]
			if err := m.router.RemoveWorker(w.ID); err != nil {
				m.lastError = err.Error()
			} else {
				m.status = "removed " + w.ID
			}
			return m, m.fetchSnapshotCmd()
		case "s":
			m.cycleStrategy()
			return m, m.fetchSnapshotCmd()
		}
	}

	return m, nil
}

func (m model) View() string {
	header := m.renderHeader()
	tabs := m.renderTabs()
	body := m.renderBody()
	footer := m.renderFooter()

	return lipgloss.JoinVertical(lipgloss.Left, header, tabs, body, footer)
}

func (m model) handleInputMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := msg.String()
	switch s {
	case "esc":
		m.inputMode = ""
		m.inputValue = ""
		m.status = "cancelled"
		return m, nil
	case "enter":
		if m.inputMode == "add" {
			addr := strings.TrimSpace(m.inputValue)
			if addr == "" {
				m.lastError = "address cannot be empty"
				return m, nil
			}
			if err := m.router.AddWorkerWithWeight(addr, 1); err != nil {
				m.lastError = err.Error()
			} else {
				m.status = "added worker-" + addr
			}
		}
		m.inputMode = ""
		m.inputValue = ""
		return m, m.fetchSnapshotCmd()
	case "backspace":
		if len(m.inputValue) > 0 {
			m.inputValue = m.inputValue[:len(m.inputValue)-1]
		}
		return m, nil
	default:
		if len(s) == 1 {
			m.inputValue += s
		}
		return m, nil
	}
}

func (m model) fetchSnapshotCmd() tea.Cmd {
	return func() tea.Msg {
		workers := m.router.WorkersSnapshot()
		inflight := m.router.InFlightSnapshot()
		logs := monitoring.LogSnapshot(250)
		recent := m.router.InFlightRecent(300)
		strategy := m.router.Strategy()

		sort.Slice(inflight, func(i, j int) bool {
			return inflight[i].StartedAt.Before(inflight[j].StartedAt)
		})

		return refreshMsg(snapshot{
			workers:  workers,
			inflight: inflight,
			logs:     logs,
			recent:   recent,
			strategy: strategy,
			uptime:   time.Since(m.start),
		})
	}
}

func (m *model) cycleStrategy() {
	switch m.router.Strategy() {
	case lib.StrategyLeastConnections:
		m.router.SetStrategy(lib.StrategyRoundRobin)
		m.status = "strategy: round_robin"
	case lib.StrategyRoundRobin:
		m.router.SetStrategy(lib.StrategyWeightedLeastLoad)
		m.status = "strategy: weighted_least_load"
	default:
		m.router.SetStrategy(lib.StrategyLeastConnections)
		m.status = "strategy: least_connections"
	}
}

func (m model) renderHeader() string {
	workerCount := len(m.snap.workers)
	line := fmt.Sprintf(" Master Control  | workers: %d | in-flight: %d | strategy: %s | uptime: %s ",
		workerCount,
		len(m.snap.inflight),
		m.snap.strategy,
		m.snap.uptime.Round(time.Second),
	)
	return lipgloss.NewStyle().Bold(true).Padding(0, 1).Background(lipgloss.Color("24")).Foreground(lipgloss.Color("255")).Render(line)
}

func (m model) renderTabs() string {
	titles := []string{"1 Workers", "2 InFlight", "3 Logs", "4 Stats"}
	parts := make([]string, 0, len(titles))
	for i, t := range titles {
		style := lipgloss.NewStyle().Padding(0, 1)
		if i == int(m.activeTab) {
			style = style.Bold(true).Foreground(lipgloss.Color("229")).Background(lipgloss.Color("62"))
		} else {
			style = style.Foreground(lipgloss.Color("245"))
		}
		parts = append(parts, style.Render(t))
	}
	return strings.Join(parts, " ")
}

func (m model) renderBody() string {
	switch m.activeTab {
	case tabWorkers:
		return m.renderWorkers()
	case tabInflight:
		return m.renderInflight()
	case tabLogs:
		return m.renderLogs()
	case tabStats:
		return m.renderStats()
	default:
		return m.renderWorkers()
	}
}

func (m model) renderWorkers() string {
	var b strings.Builder
	b.WriteString("\nWorkers\n")
	b.WriteString("--------------------------------------------------------------------------------\n")
	b.WriteString("Sel  ID                          Addr                Status    Circuit   Active\n")
	for i, w := range m.snap.workers {
		sel := " "
		if i == m.selected {
			sel = ">"
		}
		b.WriteString(fmt.Sprintf("%s    %-27s %-19s %-9s %-9s %6d\n",
			sel, trim(w.ID, 27), trim(w.Addr, 19), w.Status, w.CircuitState, w.ActiveRequests))
	}
	if len(m.snap.workers) == 0 {
		b.WriteString("(no workers)\n")
	}
	return b.String()
}

func (m model) renderInflight() string {
	var b strings.Builder
	b.WriteString("\nIn-Flight Requests\n")
	b.WriteString("--------------------------------------------------------------------------------\n")
	b.WriteString("RequestID                             Worker               Since\n")
	now := time.Now()
	for _, req := range m.snap.inflight {
		b.WriteString(fmt.Sprintf("%-36s %-20s %8s\n", trim(req.RequestID, 36), trim(req.Worker, 20), now.Sub(req.StartedAt).Round(time.Millisecond)))
	}
	if len(m.snap.inflight) == 0 {
		b.WriteString("(none)\n")
	}

	b.WriteString("\nRecent Completed\n")
	b.WriteString("--------------------------------------------------------------------------------\n")
	b.WriteString("RequestID                             Worker               Duration\n")
	start := 0
	if len(m.snap.recent) > 25 {
		start = len(m.snap.recent) - 25
	}
	for _, req := range m.snap.recent[start:] {
		b.WriteString(fmt.Sprintf("%-36s %-20s %8s\n", trim(req.RequestID, 36), trim(req.Worker, 20), req.Duration.Round(time.Millisecond)))
	}
	if len(m.snap.recent) == 0 {
		b.WriteString("(none)\n")
	}
	return b.String()
}

func (m model) renderLogs() string {
	var b strings.Builder
	b.WriteString("\nLogs\n")
	b.WriteString("--------------------------------------------------------------------------------\n")
	start := 0
	if len(m.snap.logs) > 50 {
		start = len(m.snap.logs) - 50
	}
	for _, l := range m.snap.logs[start:] {
		b.WriteString(fmt.Sprintf("%s %-10s %s\n", l.Time.Format("15:04:05"), trim(l.Place, 10), l.Msg))
	}
	if len(m.snap.logs) == 0 {
		b.WriteString("(no logs yet)\n")
	}
	return b.String()
}

func (m model) renderStats() string {
	var b strings.Builder
	var totalActive int64
	var totalFails int64
	var totalSuccess int64
	for _, w := range m.snap.workers {
		totalActive += w.ActiveRequests
		totalFails += w.Failures
		totalSuccess += w.Successes
	}
	b.WriteString("\nSystem Stats\n")
	b.WriteString("--------------------------------------------------------------------------------\n")
	b.WriteString(fmt.Sprintf("Workers: %d\n", len(m.snap.workers)))
	b.WriteString(fmt.Sprintf("In-Flight: %d\n", len(m.snap.inflight)))
	b.WriteString(fmt.Sprintf("Total Active Requests: %d\n", totalActive))
	b.WriteString(fmt.Sprintf("Cumulative Successes: %d\n", totalSuccess))
	b.WriteString(fmt.Sprintf("Cumulative Failures: %d\n", totalFails))
	b.WriteString(fmt.Sprintf("Current Strategy: %s\n", m.snap.strategy))
	return b.String()
}

func (m model) renderFooter() string {
	err := ""
	if m.lastError != "" {
		err = " | error: " + m.lastError
	}
	actions := " | keys: 1..4 tabs, j/k select, a add, d drain, x remove, s strategy, r refresh, q quit"
	if m.inputMode == "add" {
		actions = " | add worker address: " + m.inputValue + " (Enter to confirm, Esc cancel)"
	}
	line := " " + m.status + err + actions + " "
	return lipgloss.NewStyle().Padding(0, 1).Foreground(lipgloss.Color("255")).Background(lipgloss.Color("236")).Render(line)
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func Run(router *lib.Router) error {
	p := tea.NewProgram(NewModel(router), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
