package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/guptarohit/asciigraph"

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
	tabAutoscaling
)

type histPoint struct {
	queueDepth  float64
	utilization float64
	latency     float64
	rps         float64
	time        time.Time
}

const maxHistory = 180

type snapshot struct {
	workers     []lib.WorkerSnapshot
	agents      []lib.AgentSnapshot
	inflight    []monitoring.InFlight
	logs        []monitoring.LogEntry
	recent      []monitoring.CompletedFlight
	strategy    lib.Strategy
	autoscaling lib.AutoscalerSnapshot
	startupStats lib.StartupStats
	uptime      time.Duration
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

	// Autoscaling history
	hist      []histPoint
	zoomStart int // start index of zoom window (-1 = show all)
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
		// Append to autoscaling history
		pt := histPoint{
			queueDepth:  m.snap.autoscaling.Metrics.QueueDepth,
			utilization: m.snap.autoscaling.Metrics.WorkerUtilization,
			latency:     m.snap.autoscaling.Metrics.P95Latency,
			rps:         m.snap.autoscaling.Metrics.RequestsPerSec,
			time:        time.Now(),
		}
		m.hist = append(m.hist, pt)
		if len(m.hist) > maxHistory {
			m.hist = m.hist[len(m.hist)-maxHistory:]
		}
		if m.zoomStart > 0 && m.zoomStart >= len(m.hist) {
			m.zoomStart = len(m.hist) - 1
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
		case "6":
			m.activeTab = tabAutoscaling
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected < len(m.snap.workers)-1 {
				m.selected++
			}
		case "z":
			if m.activeTab == tabAutoscaling {
				if m.zoomStart < 0 {
					m.zoomStart = max(0, len(m.hist)-60)
					m.status = "zoom: 60s window"
				} else {
					m.zoomStart = -1
					m.status = "zoom: off"
				}
			}
		case "h", "left":
			if m.activeTab == tabAutoscaling && m.zoomStart > 0 {
				m.zoomStart = max(0, m.zoomStart-10)
			}
		case "l", "right":
			if m.activeTab == tabAutoscaling && m.zoomStart >= 0 {
				m.zoomStart = min(m.zoomStart+10, max(0, len(m.hist)-1))
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
		autoscaling := m.router.AutoscalerSnapshot()
		startupStats := m.router.GetStartupStats()

		sort.Slice(inflight, func(i, j int) bool {
			return inflight[i].StartedAt.Before(inflight[j].StartedAt)
		})

		sort.Slice(agents, func(i, j int) bool {
			return agents[i].AgentID < agents[j].AgentID
		})

		return refreshMsg(snapshot{
			workers:      workers,
			agents:       agents,
			inflight:     inflight,
			logs:         logs,
			recent:       recent,
			strategy:     strategy,
			autoscaling:  autoscaling,
			startupStats: startupStats,
			uptime:       time.Since(m.start),
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
	titles := []string{"1 Workers", "2 Agents", "3 InFlight", "4 Logs", "5 Stats", "6 AutoScale"}
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
	case tabAutoscaling:
		return m.renderAutoscaling()
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
// Auto Scaling tab
// ---------------------------------------------------------------------------

func (m model) renderAutoscaling() string {
	a := m.snap.autoscaling
	mets := a.Metrics
	cfg := a.Config
	stats := m.snap.startupStats
	var b strings.Builder
	gw := m.graphWidth()

	b.WriteString("\n")
	b.WriteString(m.separator())

	// ── Status ──
	statusClr := lipgloss.Color("34") // green
	if a.ScalingInOp || a.ScalingDecision == "scale_up" {
		statusClr = lipgloss.Color("196") // red
	} else if a.ScalingDecision == "scale_down" {
		statusClr = lipgloss.Color("214") // orange
	}
	status := lipgloss.NewStyle().Bold(true).Foreground(statusClr).Render(strings.ToUpper(a.ScalingDecision))

	oscWarn := ""
	if m.detectOscillation() {
		oscWarn = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")).Render(" ⚠ OSCILLATING")
	}
	overWarn := ""
	if m.detectOverscaling() {
		overWarn = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214")).Render(" ⚠ OVERSCALING")
	}

	b.WriteString(fmt.Sprintf("  Status: %s%s%s\n", status, oscWarn, overWarn))
	b.WriteString(fmt.Sprintf("  Workers: %d total, %d healthy | In-Flight: %d | Queue: %.0f (raw: %d) | RPS: %.1f\n",
		mets.TotalWorkers, mets.HealthyWorkers, mets.InFlight, mets.QueueDepth, mets.QueueSize, mets.RequestsPerSec))
	b.WriteString(m.separator())

	// ── Graphs ──
	window := m.hist
	start := 0
	if m.zoomStart >= 0 && m.zoomStart < len(m.hist) {
		start = m.zoomStart
		window = m.hist[start:]
	}
	if len(window) < 2 {
		b.WriteString("  (collecting data...)\n\n")
		window = nil
	}

	if len(window) >= 2 {
		// Utilization + Hysteresis Graph
		b.WriteString(m.renderUtilizationGraph(window, gw, cfg))

		// Queue Depth + Scaling Markers Graph
		b.WriteString(m.renderQueueDepthGraph(window, gw, a))

		// Latency Overlay Graph
		b.WriteString(m.renderLatencyGraph(window, gw))
	}

	// ── Worker Startup Times ──
	b.WriteString("Worker Startup Times\n")
	b.WriteString(m.separator())
	avgStr := "N/A"
	p95Str := "N/A"
	if stats.TotalStartups > 0 {
		avgStr = stats.AvgDuration.Round(time.Second).String()
		p95Str = stats.P95Duration.Round(time.Second).String()
	}
	b.WriteString(fmt.Sprintf("  Avg:  %s\n  P95:  %s\n  Failed: %d / %d total\n",
		avgStr, p95Str, stats.FailedStartups, stats.TotalStartups))
	b.WriteString(m.separator())

	// ── Decision Debugging ──
	b.WriteString("Autoscaler Decision Debugging\n")
	b.WriteString(m.separator())

	utilBar := m.progressBar(mets.WorkerUtilization, gw-25)
	targetBar := m.progressBar(cfg.TargetUtilization, gw-25)
	upBar := m.progressBar(cfg.ScaleUpThreshold, gw-25)
	downBar := m.progressBar(cfg.ScaleDownThreshold, gw-25)

	b.WriteString(fmt.Sprintf("  Current Utilization:  %s  (%.1f%%)\n", utilBar, mets.WorkerUtilization*100))
	b.WriteString(fmt.Sprintf("  Target Utilization:   %s  (%.0f%%)\n", targetBar, cfg.TargetUtilization*100))
	b.WriteString(fmt.Sprintf("  Scale-Up Threshold:   %s  (%.0f%%)\n", upBar, cfg.ScaleUpThreshold*100))
	b.WriteString(fmt.Sprintf("  Scale-Down Threshold: %s  (%.0f%%)\n", downBar, cfg.ScaleDownThreshold*100))

	// Hysteresis band visualization
	b.WriteString(fmt.Sprintf("  Hysteresis Band: %s\n", m.hysteresisBand(cfg, mets.WorkerUtilization, gw-18)))

	// Cooldown status
	cd := a.CooldownRemaining
	cdStr := "none"
	cdClr := "34"
	if cd > 0 {
		cdStr = cd.Round(time.Second).String()
		cdClr = "214"
		if a.ScalingDecision == "scale_up" {
			cdClr = "196"
		}
	}
	b.WriteString(fmt.Sprintf("  Cooldown Remaining:   %s\n",
		lipgloss.NewStyle().Foreground(lipgloss.Color(cdClr)).Render(cdStr)))

	// Sustained timers
	if a.HighLoadSet {
		elapsed := time.Since(a.HighLoadSince).Round(time.Second)
		remaining := (cfg.ScaleUpSustained - time.Since(a.HighLoadSince)).Round(time.Second)
		if remaining < 0 {
			remaining = 0
		}
		timerBar := m.progressBar(1.0-float64(remaining)/float64(cfg.ScaleUpSustained), gw-25)
		b.WriteString(fmt.Sprintf("  High Load Timer:      %s  (%s / %s sustained)\n", timerBar, elapsed, cfg.ScaleUpSustained))
	} else {
		b.WriteString("  High Load Timer:      inactive\n")
	}
	if a.LowLoadSet {
		elapsed := time.Since(a.LowLoadSince).Round(time.Second)
		remaining := (cfg.ScaleDownSustained - time.Since(a.LowLoadSince)).Round(time.Second)
		if remaining < 0 {
			remaining = 0
		}
		timerBar := m.progressBar(1.0-float64(remaining)/float64(cfg.ScaleDownSustained), gw-25)
		b.WriteString(fmt.Sprintf("  Low Load Timer:       %s  (%s / %s sustained)\n", timerBar, elapsed, cfg.ScaleDownSustained))
	} else {
		b.WriteString("  Low Load Timer:       inactive\n")
	}

	b.WriteString(fmt.Sprintf("  Desired Workers:      %d (current healthy: %d)\n", a.DesiredWorkers, mets.HealthyWorkers))

	// Decision reason
	reasonClr := lipgloss.Color("15") // white default
	if a.ScalingDecision == "scale_up" {
		reasonClr = lipgloss.Color("196")
	} else if a.ScalingDecision == "scale_down" {
		reasonClr = lipgloss.Color("214")
	}
	b.WriteString(fmt.Sprintf("  Decision:             %s\n",
		lipgloss.NewStyle().Bold(true).Foreground(reasonClr).Render(a.ScalingDecision)))
	b.WriteString(fmt.Sprintf("  Reason:               %s\n",
		lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Render(a.DecisionReason)))
	b.WriteString(m.separator())

	// ── Recent Scaling Actions ──
	if len(a.RecentActions) > 0 {
		b.WriteString("Recent Scaling Actions\n")
		b.WriteString(m.separator())
		startIdx := 0
		if len(a.RecentActions) > 8 {
			startIdx = len(a.RecentActions) - 8
		}
		for _, act := range a.RecentActions[startIdx:] {
			icon := "•"
			actClr := lipgloss.Color("15")
			if act.Action == "up" {
				icon = "▲"
				actClr = lipgloss.Color("196")
			} else if act.Action == "down" {
				icon = "▼"
				actClr = lipgloss.Color("46")
			}
			dir := lipgloss.NewStyle().Foreground(actClr).Render(fmt.Sprintf("%s %s", icon, act.Action))
			b.WriteString(fmt.Sprintf("  %s  %s +%d  %s\n",
				act.Time.Format("15:04:05"), dir, act.Count, trim(act.Reason, gw-30)))
		}
		b.WriteString(m.separator())
	}

	// ── Agent Health ──
	if len(a.AgentHealth) > 0 {
		b.WriteString("Agent Health\n")
		b.WriteString(m.separator())
		for id, h := range a.AgentHealth {
			pen := "ok"
			penClr := lipgloss.Color("34")
			if time.Now().Before(h.PenalizedUntil) {
				pen = fmt.Sprintf("penalized %s", time.Until(h.PenalizedUntil).Round(time.Second))
				penClr = lipgloss.Color("196")
			}
			if h.ConsecutiveFailures > 0 {
				failClr := lipgloss.Color("214")
				if h.ConsecutiveFailures >= 3 {
					failClr = lipgloss.Color("196")
				}
				b.WriteString(fmt.Sprintf("  %s  fails=%s  %s\n",
					trim(id, 40),
					lipgloss.NewStyle().Foreground(failClr).Render(strconv.Itoa(h.ConsecutiveFailures)),
					lipgloss.NewStyle().Foreground(penClr).Render(pen)))
			} else {
				b.WriteString(fmt.Sprintf("  %s  ok\n", trim(id, 40)))
			}
		}
		b.WriteString(m.separator())
	}

	// ── Configuration (compact) ──
	b.WriteString("Configuration\n")
	b.WriteString(m.separator())
	b.WriteString(fmt.Sprintf("  Cooldowns: up=%s down=%s | Sustained: up=%s down=%s\n",
		cfg.ScaleUpCooldown, cfg.ScaleDownCooldown, cfg.ScaleUpSustained, cfg.ScaleDownSustained))
	b.WriteString(fmt.Sprintf("  Max Step: up=%d down=%d | Workers: min=%d max=%d\n",
		cfg.MaxScaleUpStep, cfg.MaxScaleDownStep, cfg.MinWorkers, cfg.MaxWorkers))
	b.WriteString(fmt.Sprintf("  P95 Ceiling: %.1fs | Error Rate Ceiling: %.0f%% | Predictive: trend=%.1f prewarm=%d\n",
		cfg.P95LatencyCeiling, cfg.ErrorRateCeiling*100, cfg.RPSTrendThreshold, cfg.PrewarmWorkers))

	return b.String()
}

// detectOscillation checks if recent scaling actions show an alternating pattern.
func (m model) detectOscillation() bool {
	a := m.snap.autoscaling
	if len(a.RecentActions) < 6 {
		return false
	}
	last := a.RecentActions
	if len(last) > 12 {
		last = last[len(last)-12:]
	}
	altCount := 0
	for i := 1; i < len(last); i++ {
		if last[i].Action != last[i-1].Action {
			altCount++
		}
	}
	// If >70% alternation in recent actions, it's oscillating
	return float64(altCount)/float64(len(last)-1) > 0.7 && len(last) >= 6
}

// detectOverscaling checks if scale-up was immediately followed by scale-down.
func (m model) detectOverscaling() bool {
	a := m.snap.autoscaling
	if len(a.RecentActions) < 4 {
		return false
	}
	last := a.RecentActions[len(a.RecentActions)-4:]
	for i := 1; i < len(last); i++ {
		if last[i].Action == "down" && last[i-1].Action == "up" {
			// Was the scale-down within 2 minutes of the scale-up?
			if last[i].Time.Sub(last[i-1].Time) < 2*time.Minute {
				return true
			}
		}
	}
	return false
}

// renderUtilizationGraph draws worker utilization with thresholds and hysteresis bands.
func (m model) renderUtilizationGraph(window []histPoint, width int, cfg lib.AutoscalerConfig) string {
	if len(window) < 2 {
		return ""
	}

	vals := make([]float64, len(window))
	for i, p := range window {
		vals[i] = p.utilization * 100
	}

	// Add threshold lines as separate series
	upLine := make([]float64, len(window))
	downLine := make([]float64, len(window))
	targetLine := make([]float64, len(window))
	for i := range window {
		upLine[i] = cfg.ScaleUpThreshold * 100
		downLine[i] = cfg.ScaleDownThreshold * 100
		targetLine[i] = cfg.TargetUtilization * 100
	}

	graph := asciigraph.PlotMany(
		[][]float64{vals, upLine, downLine, targetLine},
		asciigraph.Height(7),
		asciigraph.Width(width),
		asciigraph.Caption("Worker Utilization with Hysteresis Bands"),
		asciigraph.SeriesLegends("Util", "Up↑", "Down↓", "Tgt"),
		asciigraph.SeriesColors(
			asciigraph.ColorNames["lime"],
			asciigraph.ColorNames["red"],
			asciigraph.ColorNames["teal"],
			asciigraph.ColorNames["yellow"],
		),
	)

	// Indent the graph
	lines := strings.Split(graph, "\n")
	for i := range lines {
		lines[i] = "  " + lines[i]
	}
	return strings.Join(lines, "\n") + "\n"
}

// renderQueueDepthGraph draws queue depth with scaling action markers.
func (m model) renderQueueDepthGraph(window []histPoint, width int, a lib.AutoscalerSnapshot) string {
	if len(window) < 2 {
		return ""
	}

	vals := make([]float64, len(window))
	for i, p := range window {
		v := p.queueDepth
		if v < 0 {
			v = 0
		}
		vals[i] = v
	}

	// Overlay scaling action markers
	markers := make([]float64, len(window))
	_ = copy(markers, vals)
	for i := range markers {
		markers[i] = 0
	}
	for i := range window {
		markers[i] = 0
	}
	// Mark scaling action times with spikes
	for _, act := range a.RecentActions {
		for i, p := range window {
			if !act.Time.Before(p.time) && act.Time.Sub(p.time) < 3*time.Second {
				if act.Action == "up" {
					markers[i] = vals[i] + 2
				} else {
					if markers[i] < vals[i]+1 {
						markers[i] = vals[i] + 1
					}
				}
			}
		}
	}

	graph := asciigraph.PlotMany(
		[][]float64{vals, markers},
		asciigraph.Height(6),
		asciigraph.Width(width),
		asciigraph.Caption("Queue Depth (▲=scale-up)"),
		asciigraph.SeriesLegends("Queue", "Act"),
		asciigraph.SeriesColors(
			asciigraph.ColorNames["cyan"],
			asciigraph.ColorNames["red"],
		),
	)

	lines := strings.Split(graph, "\n")
	for i := range lines {
		lines[i] = "  " + lines[i]
	}
	return strings.Join(lines, "\n") + "\n"
}

// renderLatencyGraph draws P95 latency overlay.
func (m model) renderLatencyGraph(window []histPoint, width int) string {
	if len(window) < 2 {
		return ""
	}

	vals := make([]float64, len(window))
	for i, p := range window {
		v := p.latency
		if v < 0 {
			v = 0
		}
		vals[i] = v
	}

	graph := asciigraph.Plot(
		vals,
		asciigraph.Height(6),
		asciigraph.Width(width),
		asciigraph.Caption("P95 Latency (s)"),
	)

	lines := strings.Split(graph, "\n")
	for i := range lines {
		lines[i] = "  " + lines[i]
	}
	return strings.Join(lines, "\n") + "\n"
}

// progressBar renders a colored text progress bar.
func (m model) progressBar(ratio float64, width int) string {
	if width < 10 {
		width = 10
	}
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio * float64(width-2))
	if filled > width-2 {
		filled = width - 2
	}
	empty := (width - 2) - filled

	fillChar := "█"
	emptyChar := "░"

	barColor := lipgloss.Color("34") // green
	if ratio > 0.75 {
		barColor = lipgloss.Color("196") // red
	} else if ratio > 0.5 {
		barColor = lipgloss.Color("214") // orange
	}

	fill := lipgloss.NewStyle().Foreground(barColor).Render(strings.Repeat(fillChar, filled))
	emp := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(strings.Repeat(emptyChar, empty))
	return "│" + fill + emp + "│"
}

// hysteresisBand renders a visual representation of the hysteresis bands.
func (m model) hysteresisBand(cfg lib.AutoscalerConfig, currentUtil float64, width int) string {
	if width < 20 {
		width = 20
	}

	// Divide the bar into zones
	// 0% to scale-down threshold: cool-down zone (blue)
	// scale-down to scale-up: hysteresis band (yellow)
	// scale-up to 100%: escalation zone (red)

	downAt := int(cfg.ScaleDownThreshold * float64(width))
	upAt := int(cfg.ScaleUpThreshold * float64(width))
	if downAt > width {
		downAt = width
	}
	if upAt > width {
		upAt = width
	}
	currAt := int(currentUtil * float64(width))
	if currAt > width {
		currAt = width
	}

	var b strings.Builder
	b.WriteString("[")
	for i := 0; i < width; i++ {
		ch := "─"
		if i == currAt {
			ch = "◆"
		} else if i < downAt {
			// cool zone
			ch = lipgloss.NewStyle().Foreground(lipgloss.Color("69")).Render("·")
		} else if i < upAt {
			// hysteresis band
			ch = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Render("▒")
		} else {
			// escalation
			ch = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("▓")
		}
		b.WriteString(ch)
	}
	b.WriteString("]")
	// Legend
	b.WriteString(fmt.Sprintf(" cool↓ ┃ tgt=%.0f%% ┃ warm↑", cfg.TargetUtilization*100))
	return b.String()
}

// graphWidth returns the usable width for ASCII graphs.
func (m model) graphWidth() int {
	w := m.width
	if w <= 0 {
		w = 80
	}
	w -= 4 // 2 chars indent + margins
	if w < 30 {
		w = 30
	}
	return w
}

// ---------------------------------------------------------------------------
// Footer
// ---------------------------------------------------------------------------

func (m model) renderFooter() string {
	err := ""
	if m.lastError != "" {
		err = " | error: " + m.lastError
	}
	actions := " | keys: 1..6 tabs, j/k select, a add, d drain, x remove, s strategy, r refresh, q quit"
	if m.activeTab == tabAutoscaling {
		actions = " | 1..6 tabs, z zoom, ← → scroll zoom, r refresh, q quit"
	}
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func Run(router *lib.Router) error {
	p := tea.NewProgram(NewModel(router), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
