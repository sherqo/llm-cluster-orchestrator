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
	tabEvents
)

var tabLabels = []string{"Workers", "Agents", "In-Flight", "Logs", "Stats", "AutoScale", "Events"}

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
	events       []monitoring.LogEntry
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

	// scroll viewport
	vp      viewport.Model
	vpReady bool

	// autoscaling history & zoom
	hist      []histPoint
	zoomStart int
}

// ── Msg types ────────────────────────────────────────────────────────────────

type tickMsg time.Time
type refreshMsg snapshot

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
//
// Design rule: every case MUST return explicitly. The fallthrough-to-viewport
// pattern was causing double updates and phantom content.

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	// ── Window resize ─────────────────────────────────────────────────────
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		vpH := m.vpHeight()
		if !m.vpReady {
			m.vp = viewport.New(m.width, vpH)
			m.vpReady = true
		} else {
			m.vp.Width = m.width
			m.vp.Height = vpH
		}
		m.vp.SetContent(m.renderBody())
		return m, nil

	// ── Tick → fetch ──────────────────────────────────────────────────────
	case tickMsg:
		return m, tea.Batch(
			m.fetchSnapshotCmd(),
			tea.Tick(m.tickEvery, func(t time.Time) tea.Msg { return tickMsg(t) }),
		)

	// ── Data refresh ──────────────────────────────────────────────────────
	case refreshMsg:
		m.snap = snapshot(msg)
		if m.selected >= len(m.snap.workers) {
			m.selected = max(0, len(m.snap.workers)-1)
		}
		// Always append history point
		m.hist = append(m.hist, histPoint{
			queueDepth:  m.snap.autoscaling.Metrics.QueueDepth,
			utilization: m.snap.autoscaling.Metrics.WorkerUtilization,
			latency:     m.snap.autoscaling.Metrics.P95Latency,
			rps:         m.snap.autoscaling.Metrics.RequestsPerSec,
			time:        time.Now(),
		})
		if len(m.hist) > maxHistory {
			m.hist = m.hist[len(m.hist)-maxHistory:]
		}
		if m.zoomStart >= 0 && m.zoomStart >= len(m.hist) {
			m.zoomStart = max(0, len(m.hist)-1)
		}
		if m.vpReady {
			// Preserve scroll position so live updates don't jump the user around
			prevY := m.vp.YOffset
			m.vp.SetContent(m.renderBody())
			m.vp.SetYOffset(prevY)
		}
		return m, nil

	// ── Keyboard ──────────────────────────────────────────────────────────
	case tea.KeyMsg:
		if m.inputMode != "" {
			return m.handleInputMode(msg)
		}
		return m.handleKey(msg)

	// ── Mouse wheel (pass to viewport) ────────────────────────────────────
	case tea.MouseMsg:
		if m.vpReady {
			var cmd tea.Cmd
			m.vp, cmd = m.vp.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := msg.String()

	switch s {
	case "ctrl+c", "q":
		return m, tea.Quit

	// Tab switching — reset scroll to top on each switch
	case "1":
		return m.switchTab(tabWorkers)
	case "2":
		return m.switchTab(tabAgents)
	case "3":
		return m.switchTab(tabInflight)
	case "4":
		return m.switchTab(tabLogs)
	case "5":
		return m.switchTab(tabStats)
	case "6":
		return m.switchTab(tabAutoscaling)
	case "7":
		return m.switchTab(tabEvents)

	// Worker row selection (only on workers tab)
	case "up", "k":
		if m.activeTab == tabWorkers {
			if m.selected > 0 {
				m.selected--
				m.setVPContent()
			}
			return m, nil
		}
		// Otherwise scroll viewport
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd

	case "down", "j":
		if m.activeTab == tabWorkers {
			if m.selected < len(m.snap.workers)-1 {
				m.selected++
				m.setVPContent()
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd

	// Scroll keys — forward directly to viewport
	case "pgup", "ctrl+u", "b":
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd

	case "pgdn", "ctrl+d", "f":
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd

	// Autoscaling zoom
	case "z":
		if m.activeTab == tabAutoscaling {
			if m.zoomStart < 0 {
				m.zoomStart = max(0, len(m.hist)-60)
				m.status = "zoom: 60s window"
			} else {
				m.zoomStart = -1
				m.status = "zoom: all"
			}
			m.setVPContent()
		}
		return m, nil

	case "left", "h":
		if m.activeTab == tabAutoscaling && m.zoomStart > 0 {
			m.zoomStart = max(0, m.zoomStart-10)
			m.setVPContent()
		}
		return m, nil

	case "right", "l":
		if m.activeTab == tabAutoscaling && m.zoomStart >= 0 {
			m.zoomStart = min(m.zoomStart+10, max(0, len(m.hist)-1))
			m.setVPContent()
		}
		return m, nil

	// Actions
	case "r":
		m.status = "refreshing…"
		return m, m.fetchSnapshotCmd()

	case "a":
		m.inputMode = "add"
		m.inputValue = ""
		m.status = "enter worker address (host:port), then Enter"
		return m, nil

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
		return m, nil

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
		return m, nil

	case "s":
		m.cycleStrategy()
		return m, m.fetchSnapshotCmd()
	}

	return m, nil
}

// switchTab changes the active tab and resets the viewport scroll to top.
// Must be a pointer receiver so setVPContent mutates the real viewport.
func (m *model) switchTab(t tab) (tea.Model, tea.Cmd) {
	m.activeTab = t
	m.vp.SetContent(m.renderBody())
	m.vp.GotoTop()
	return m, nil
}

// setVPContent re-renders the body into the viewport.
func (m *model) setVPContent() {
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
	vpView := m.vp.View()

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		tabs,
		vpView,
		footer,
	)

	// Count actual lines rendered.
	// We MUST output exactly m.height lines every frame — if we output fewer,
	// the terminal leaves old content from the previous frame visible ("sticky" lines).
	// If we output more, the terminal scrolls and creates the flicker the user sees.
	lines := strings.Split(content, "\n")
	if len(lines) < m.height {
		// Pad with blank lines so Bubble Tea overwrites every row.
		blank := strings.Repeat(" ", m.width)
		for len(lines) < m.height {
			lines = append(lines, blank)
		}
		content = strings.Join(lines, "\n")
	} else if len(lines) > m.height {
		// Hard-clip so we never push into terminal scroll history.
		content = strings.Join(lines[:m.height], "\n")
	}

	return content
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
				m.lastError = ""
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
		logs := monitoring.LogSnapshot(500)
		events := monitoring.EventsSnapshot(500)
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
			events:       events,
			recent:       recent,
			strategy:     strategy,
			autoscaling:  autoscaling,
			startupStats: startupStats,
			uptime:       time.Since(m.start),
		})
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

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
	case tabEvents:
		return m.renderEvents()
	default:
		return m.renderWorkers()
	}
}

// vpHeight computes the viewport height from terminal dimensions.
// Header=1, tabs=1, divider=1, footer=1 = 4 rows of chrome.
func (m model) vpHeight() int {
	h := m.height - 4
	if h < 5 {
		h = 5
	}
	return h
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
