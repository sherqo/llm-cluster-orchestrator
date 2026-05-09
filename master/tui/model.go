package tui

import (
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"master/lib"
	"master/monitoring"
)

// ── Tab identifiers ──────────────────────────────────────────────────────────

type tab int

const (
	tabWorkers tab = iota
	tabAgents
	tabInflight
	tabLogs
	tabStats
	tabAutoscaling
)

var tabLabels = []string{"  Workers ", "  Agents ", "  In-Flight ", "  Logs ", "  Stats ", "  AutoScale "}

// ── History ──────────────────────────────────────────────────────────────────

type histPoint struct {
	queueDepth  float64
	utilization float64
	latency     float64
	rps         float64
	time        time.Time
}

const maxHistory = 180

// ── Snapshot ─────────────────────────────────────────────────────────────────

type snapshot struct {
	workers      []lib.WorkerSnapshot
	agents       []lib.AgentSnapshot
	inflight     []monitoring.InFlight
	logs         []monitoring.LogEntry
	recent       []monitoring.CompletedFlight
	strategy     lib.Strategy
	autoscaling  lib.AutoscalerSnapshot
	startupStats lib.StartupStats
	uptime       time.Duration
}

// ── Model ────────────────────────────────────────────────────────────────────

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

	// scroll
	vp        viewport.Model
	vpReady   bool

	// autoscaling history & zoom
	hist      []histPoint
	zoomStart int
}

// ── Msg types ────────────────────────────────────────────────────────────────

type tickMsg time.Time
type refreshMsg snapshot
type errMsg error

// ── Constructor ──────────────────────────────────────────────────────────────

func NewModel(router *lib.Router) tea.Model {
	return model{
		router:    router,
		start:     time.Now(),
		activeTab: tabWorkers,
		tickEvery: 700 * time.Millisecond,
		status:    "ready",
		zoomStart: -1,
	}
}

// ── Init ─────────────────────────────────────────────────────────────────────

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.fetchSnapshotCmd(),
		tea.Tick(m.tickEvery, func(t time.Time) tea.Msg { return tickMsg(t) }),
	)
}

// ── Update ───────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		headerH := 3 // header + tabs + divider
		footerH := 1
		vpH := m.height - headerH - footerH
		if vpH < 5 {
			vpH = 5
		}
		if !m.vpReady {
			m.vp = viewport.New(m.width, vpH)
			m.vpReady = true
		} else {
			m.vp.Width = m.width
			m.vp.Height = vpH
		}
		m.vp.SetContent(m.renderBody())
		return m, nil

	case tickMsg:
		return m, tea.Batch(
			m.fetchSnapshotCmd(),
			tea.Tick(m.tickEvery, func(t time.Time) tea.Msg { return tickMsg(t) }),
		)

	case refreshMsg:
		m.snap = snapshot(msg)
		if m.selected >= len(m.snap.workers) {
			m.selected = max(0, len(m.snap.workers)-1)
		}
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
		if m.vpReady {
			m.vp.SetContent(m.renderBody())
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
			m.refreshVP()
		case "2":
			m.activeTab = tabAgents
			m.refreshVP()
		case "3":
			m.activeTab = tabInflight
			m.refreshVP()
		case "4":
			m.activeTab = tabLogs
			m.refreshVP()
		case "5":
			m.activeTab = tabStats
			m.refreshVP()
		case "6":
			m.activeTab = tabAutoscaling
			m.refreshVP()
		case "up", "k":
			if m.activeTab == tabWorkers && m.selected > 0 {
				m.selected--
				m.refreshVP()
			} else {
				var vpCmd tea.Cmd
				m.vp, vpCmd = m.vp.Update(msg)
				cmds = append(cmds, vpCmd)
			}
		case "down", "j":
			if m.activeTab == tabWorkers && m.selected < len(m.snap.workers)-1 {
				m.selected++
				m.refreshVP()
			} else {
				var vpCmd tea.Cmd
				m.vp, vpCmd = m.vp.Update(msg)
				cmds = append(cmds, vpCmd)
			}
		case "pgup", "ctrl+u":
			var vpCmd tea.Cmd
			m.vp, vpCmd = m.vp.Update(msg)
			cmds = append(cmds, vpCmd)
		case "pgdn", "ctrl+d":
			var vpCmd tea.Cmd
			m.vp, vpCmd = m.vp.Update(msg)
			cmds = append(cmds, vpCmd)
		case "z":
			if m.activeTab == tabAutoscaling {
				if m.zoomStart < 0 {
					m.zoomStart = max(0, len(m.hist)-60)
					m.status = "zoom: 60s window"
				} else {
					m.zoomStart = -1
					m.status = "zoom: off"
				}
				m.refreshVP()
			}
		case "h", "left":
			if m.activeTab == tabAutoscaling && m.zoomStart > 0 {
				m.zoomStart = max(0, m.zoomStart-10)
				m.refreshVP()
			}
		case "l", "right":
			if m.activeTab == tabAutoscaling && m.zoomStart >= 0 {
				m.zoomStart = min(m.zoomStart+10, max(0, len(m.hist)-1))
				m.refreshVP()
			}
		case "r":
			m.status = "refreshing…"
			return m, m.fetchSnapshotCmd()
		case "a":
			m.inputMode = "add"
			m.inputValue = ""
			m.status = "enter worker address (host:port), then Enter"
		case "d":
			if len(m.snap.workers) > 0 {
				w := m.snap.workers[m.selected]
				if err := m.router.DrainWorker(w.ID); err != nil {
					m.lastError = err.Error()
				} else {
					m.status = "draining " + w.ID
				}
				return m, m.fetchSnapshotCmd()
			}
		case "x":
			if len(m.snap.workers) > 0 {
				w := m.snap.workers[m.selected]
				if err := m.router.RemoveWorker(w.ID); err != nil {
					m.lastError = err.Error()
				} else {
					m.status = "removed " + w.ID
				}
				return m, m.fetchSnapshotCmd()
			}
		case "s":
			m.cycleStrategy()
			return m, m.fetchSnapshotCmd()
		}
	}

	// propagate mouse/wheel to viewport
	if m.vpReady {
		var vpCmd tea.Cmd
		m.vp, vpCmd = m.vp.Update(msg)
		cmds = append(cmds, vpCmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *model) refreshVP() {
	if m.vpReady {
		m.vp.SetContent(m.renderBody())
	}
}

// ── View ─────────────────────────────────────────────────────────────────────

func (m model) View() string {
	if !m.vpReady {
		return "initializing…\n"
	}
	header := m.renderHeader()
	tabs := m.renderTabs()
	footer := m.renderFooter()
	return lipgloss.JoinVertical(lipgloss.Left, header, tabs, m.vp.View(), footer)
}

// ── Input mode ───────────────────────────────────────────────────────────────

func (m model) handleInputMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := msg.String()
	switch s {
	case "esc":
		m.inputMode = ""
		m.inputValue = ""
		m.status = "cancelled"
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
				m.status = "added " + addr
			}
		}
		m.inputMode = ""
		m.inputValue = ""
		return m, m.fetchSnapshotCmd()
	case "backspace":
		if len(m.inputValue) > 0 {
			m.inputValue = m.inputValue[:len(m.inputValue)-1]
		}
	default:
		if len(s) == 1 {
			m.inputValue += s
		}
	}
	return m, nil
}

// ── Fetch ────────────────────────────────────────────────────────────────────

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

// ── Misc ─────────────────────────────────────────────────────────────────────

func (m *model) cycleStrategy() {
	switch m.router.Strategy() {
	case lib.StrategyLeastConnections:
		m.router.SetStrategy(lib.StrategyRoundRobin)
		m.status = "strategy → round_robin"
	case lib.StrategyRoundRobin:
		m.router.SetStrategy(lib.StrategyLeastConnections)
		m.status = "strategy → least_connections"
	}
}

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

// Run starts the TUI program.
func Run(router *lib.Router) error {
	p := tea.NewProgram(
		NewModel(router),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	_, err := p.Run()
	return err
}
