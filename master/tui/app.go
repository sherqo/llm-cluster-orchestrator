package tui

import (
	"fmt"
	"sort"
	"strconv"
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
	tabAgents
	tabInflight
	tabLogs
	tabStats
)

type snapshot struct {
	workers  []lib.WorkerSnapshot
	agents   []lib.AgentSnapshot
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
			m.activeTab = tabAgents
		case "3":
			m.activeTab = tabInflight
		case "4":
			m.activeTab = tabLogs
		case "5":
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
			if err := m.router.AddWorker(addr); err != nil {
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
		agents := m.router.AgentsSnapshot()
		inflight := m.router.InFlightSnapshot()
		logs := monitoring.LogSnapshot(250)
		recent := m.router.InFlightRecent(300)
		strategy := m.router.Strategy()

		sort.Slice(inflight, func(i, j int) bool {
			return inflight[i].StartedAt.Before(inflight[j].StartedAt)
		})

		sort.Slice(agents, func(i, j int) bool {
			return agents[i].AgentID < agents[j].AgentID
		})

		return refreshMsg(snapshot{
			workers:  workers,
			agents:   agents,
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
		m.router.SetStrategy(lib.StrategyLeastConnections)
		m.status = "strategy: least_connections"
	}
}

// ---------------------------------------------------------------------------
// Header
// ---------------------------------------------------------------------------

func (m model) renderHeader() string {
	workerCount := len(m.snap.workers)
	healthyCount := 0
	for _, w := range m.snap.workers {
		if w.Lifecycle == lib.StateHealthy {
			healthyCount++
		}
	}

	line := fmt.Sprintf(" Master Control  | workers: %d/%d healthy | agents: %d | in-flight: %d | strategy: %s | uptime: %s ",
		healthyCount, workerCount,
		len(m.snap.agents),
		len(m.snap.inflight),
		m.snap.strategy,
		m.snap.uptime.Round(time.Second),
	)
	return lipgloss.NewStyle().Bold(true).Padding(0, 1).Background(lipgloss.Color("24")).Foreground(lipgloss.Color("255")).Render(line)
}

// ---------------------------------------------------------------------------
// Tabs
// ---------------------------------------------------------------------------

func (m model) renderTabs() string {
	titles := []string{"1 Workers", "2 Agents", "3 InFlight", "4 Logs", "5 Stats"}
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

// ---------------------------------------------------------------------------
// Body
// ---------------------------------------------------------------------------

func (m model) renderBody() string {
	switch m.activeTab {
	case tabWorkers:
		return m.renderWorkers()
	case tabAgents:
		return m.renderAgents()
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

// ---------------------------------------------------------------------------
// Workers tab
// ---------------------------------------------------------------------------

func (m model) renderWorkers() string {
	var b strings.Builder
	b.WriteString("\nWorkers\n")
	b.WriteString(m.separator())
	b.WriteString("Sel  ID                          Addr                State     Active  Agent\n")
	for i, w := range m.snap.workers {
		sel := " "
		if i == m.selected {
			sel = ">"
		}
		line := fmt.Sprintf("%s    %-27s %-19s %-9s %6d  %s",
			sel, trim(w.ID, 27), trim(w.Addr, 19), w.Lifecycle.String(), w.ActiveRequests, w.AgentID)
		b.WriteString(m.wrapLine(line) + "\n")
	}
	if len(m.snap.workers) == 0 {
		b.WriteString("(no workers)\n")
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Agents tab
// ---------------------------------------------------------------------------

func (m model) renderAgents() string {
	var b strings.Builder
	b.WriteString("\nRegistered Agents\n")
	b.WriteString(m.separator())
	b.WriteString(fmt.Sprintf("%-30s %-22s %-8s %-10s\n",
		"Agent ID", "Address", "Workers", "Uptime"))
	b.WriteString(m.separator())

	for _, a := range m.snap.agents {
		addr := fmt.Sprintf("%s:%d", a.Host, a.Port)
		uptime := time.Since(a.AddedAt).Round(time.Second)
		line := fmt.Sprintf("%-30s %-22s %-8d %-10s",
			trim(a.AgentID, 30), trim(addr, 22), a.WorkerCount, uptime)
		b.WriteString(m.wrapLine(line) + "\n")
	}

	if len(m.snap.agents) == 0 {
		b.WriteString("(no agents registered)\n")
	}

	// Show workers grouped by agent
	b.WriteString("\n")
	b.WriteString("Workers by Agent\n")
	b.WriteString(m.separator())

	agentWorkers := make(map[string][]lib.WorkerSnapshot)
	for _, w := range m.snap.workers {
		aid := w.AgentID
		if aid == "" {
			aid = "(manual)"
		}
		agentWorkers[aid] = append(agentWorkers[aid], w)
	}

	for _, a := range m.snap.agents {
		workers, ok := agentWorkers[a.AgentID]
		if !ok {
			b.WriteString(fmt.Sprintf("  %s: (no workers)\n", trim(a.AgentID, 40)))
			continue
		}
		b.WriteString(fmt.Sprintf("  %s: %d workers\n", trim(a.AgentID, 40), len(workers)))
		for _, w := range workers {
			line := fmt.Sprintf("    ├─ %-25s %-9s active=%d",
				trim(w.Addr, 25), w.Lifecycle.String(), w.ActiveRequests)
			b.WriteString(m.wrapLine(line) + "\n")
		}
	}

	// Show manual workers (no agent)
	if manual, ok := agentWorkers["(manual)"]; ok {
		b.WriteString(fmt.Sprintf("  (manual): %d workers\n", len(manual)))
		for _, w := range manual {
			line := fmt.Sprintf("    ├─ %-25s %-9s active=%d",
				trim(w.Addr, 25), w.Lifecycle.String(), w.ActiveRequests)
			b.WriteString(m.wrapLine(line) + "\n")
		}
	}

	return b.String()
}

// ---------------------------------------------------------------------------
// InFlight tab
// ---------------------------------------------------------------------------

func (m model) renderInflight() string {
	var b strings.Builder
	b.WriteString("\nIn-Flight Requests\n")
	b.WriteString(m.separator())
	b.WriteString(fmt.Sprintf("%-36s %-20s %8s\n", "RequestID", "Worker", "Since"))
	now := time.Now()
	for _, req := range m.snap.inflight {
		line := fmt.Sprintf("%-36s %-20s %8s",
			trim(req.RequestID, 36), trim(req.Worker, 20), now.Sub(req.StartedAt).Round(time.Millisecond))
		b.WriteString(m.wrapLine(line) + "\n")
	}
	if len(m.snap.inflight) == 0 {
		b.WriteString("(none)\n")
	}

	b.WriteString("\nRecent Completed\n")
	b.WriteString(m.separator())
	b.WriteString(fmt.Sprintf("%-36s %-20s %10s\n", "RequestID", "Worker", "Duration"))
	start := 0
	if len(m.snap.recent) > 25 {
		start = len(m.snap.recent) - 25
	}
	for _, req := range m.snap.recent[start:] {
		line := fmt.Sprintf("%-36s %-20s %10s",
			trim(req.RequestID, 36), trim(req.Worker, 20), req.Duration.Round(time.Millisecond))
		b.WriteString(m.wrapLine(line) + "\n")
	}
	if len(m.snap.recent) == 0 {
		b.WriteString("(none)\n")
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Logs tab — with text wrapping
// ---------------------------------------------------------------------------

func (m model) renderLogs() string {
	var b strings.Builder
	b.WriteString("\nLogs\n")
	b.WriteString(m.separator())

	maxLines := m.bodyHeight()
	if maxLines < 10 {
		maxLines = 50
	}

	start := 0
	if len(m.snap.logs) > maxLines {
		start = len(m.snap.logs) - maxLines
	}
	for _, l := range m.snap.logs[start:] {
		line := fmt.Sprintf("%s %-10s %s",
			l.Time.Format("15:04:05"), trim(l.Place, 10), l.Msg)
		b.WriteString(m.wrapLine(line) + "\n")
	}
	if len(m.snap.logs) == 0 {
		b.WriteString("(no logs yet)\n")
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Stats tab — with more context
// ---------------------------------------------------------------------------

func (m model) renderStats() string {
	var b strings.Builder
	var totalActive int64
	var totalFails int64
	var totalSuccess int64
	healthyCount := 0
	drainingCount := 0
	startingCount := 0

	for _, w := range m.snap.workers {
		totalActive += w.ActiveRequests
		totalFails += w.Failures
		totalSuccess += w.Successes
		switch w.Lifecycle {
		case lib.StateHealthy:
			healthyCount++
		case lib.StateDraining:
			drainingCount++
		case lib.StateStarting, lib.StateWarming:
			startingCount++
		}
	}

	b.WriteString("\nCluster Overview\n")
	b.WriteString(m.separator())
	b.WriteString(fmt.Sprintf("  Total Workers:      %d\n", len(m.snap.workers)))
	b.WriteString(fmt.Sprintf("  ├─ Healthy:         %d\n", healthyCount))
	b.WriteString(fmt.Sprintf("  ├─ Starting/Warm:   %d\n", startingCount))
	b.WriteString(fmt.Sprintf("  └─ Draining:        %d\n", drainingCount))
	b.WriteString(fmt.Sprintf("  Agents:             %d\n", len(m.snap.agents)))
	b.WriteString(fmt.Sprintf("  In-Flight:          %d\n", len(m.snap.inflight)))
	b.WriteString(fmt.Sprintf("  Total Active:       %d\n", totalActive))
	b.WriteString(fmt.Sprintf("  Strategy:           %s\n", m.snap.strategy))
	b.WriteString(fmt.Sprintf("  Uptime:             %s\n", m.snap.uptime.Round(time.Second)))

	b.WriteString("\nRequest Stats\n")
	b.WriteString(m.separator())
	b.WriteString(fmt.Sprintf("  Cumulative Success: %d\n", totalSuccess))
	b.WriteString(fmt.Sprintf("  Cumulative Fails:   %d\n", totalFails))

	if len(m.snap.recent) > 0 {
		// Compute avg/P95 latency from recent
		var sum float64
		durations := make([]float64, 0, len(m.snap.recent))
		for _, r := range m.snap.recent {
			d := r.Duration.Seconds()
			sum += d
			durations = append(durations, d)
		}
		avg := sum / float64(len(durations))

		// Simple P95
		sort.Float64s(durations)
		p95Idx := int(float64(len(durations)) * 0.95)
		if p95Idx >= len(durations) {
			p95Idx = len(durations) - 1
		}
		p95 := durations[p95Idx]

		b.WriteString(fmt.Sprintf("  Avg Latency:        %.2fs  (last %d)\n", avg, len(durations)))
		b.WriteString(fmt.Sprintf("  P95 Latency:        %.2fs\n", p95))
	}

	b.WriteString("\nAgents\n")
	b.WriteString(m.separator())
	for _, a := range m.snap.agents {
		addr := a.Host + ":" + strconv.Itoa(a.Port)
		b.WriteString(fmt.Sprintf("  %-30s  %s  workers=%d\n",
			trim(a.AgentID, 30), trim(addr, 22), a.WorkerCount))
	}
	if len(m.snap.agents) == 0 {
		b.WriteString("  (no agents)\n")
	}

	return b.String()
}

// ---------------------------------------------------------------------------
// Footer
// ---------------------------------------------------------------------------

func (m model) renderFooter() string {
	err := ""
	if m.lastError != "" {
		err = " | error: " + m.lastError
	}
	actions := " | keys: 1..5 tabs, j/k select, a add, d drain, x remove, s strategy, r refresh, q quit"
	if m.inputMode == "add" {
		actions = " | add worker address: " + m.inputValue + " (Enter to confirm, Esc cancel)"
	}
	line := " " + m.status + err + actions + " "

	// Wrap footer to terminal width
	if m.width > 0 && len(line) > m.width {
		line = line[:m.width-1]
	}
	return lipgloss.NewStyle().Padding(0, 1).Foreground(lipgloss.Color("255")).Background(lipgloss.Color("236")).Render(line)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (m model) separator() string {
	w := m.width
	if w <= 0 {
		w = 80
	}
	return strings.Repeat("─", w) + "\n"
}

// wrapLine wraps a long line to fit terminal width.
func (m model) wrapLine(line string) string {
	w := m.width
	if w <= 0 || len(line) <= w {
		return line
	}

	var b strings.Builder
	for len(line) > 0 {
		end := w
		if end > len(line) {
			end = len(line)
		}
		if b.Len() > 0 {
			b.WriteString("\n    ") // indent continuation
		}
		b.WriteString(line[:end])
		line = line[end:]
	}
	return b.String()
}

// bodyHeight estimates how many lines are available for content.
func (m model) bodyHeight() int {
	if m.height <= 0 {
		return 50
	}
	// header(1) + tabs(1) + footer(1) + margins(2) = 5
	return m.height - 5
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
