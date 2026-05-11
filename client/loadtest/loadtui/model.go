package loadtui

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tab int

const (
	tabDashboard tab = iota
	tabLogs
	tabConfig
)

type sendMsg struct {
	ID      string
	UserID  string
	Tier    string
	Prompt  string
	Started time.Time
}

type doneMsg struct {
	ID       string
	UserID   string
	Tier     string
	Prompt   string
	Reply    string
	Latency  time.Duration
	Err      error
	Finished time.Time
}

type tickMsg time.Time

type Model struct {
	cfg     Config
	active  tab
	width   int
	height  int
	logs    []LogEntry
	metrics *Metrics
	logVP   viewport.Model
	paused  bool
	mode    string // burst|rate
	seed    int64
	lastRate time.Time
}

func New(cfg Config) Model {
	vp := viewport.New(80, 20)
	vp.SetContent("")
	return Model{
		cfg:     cfg,
		active:  tabDashboard,
		metrics: NewMetrics(),
		logVP:   vp,
		paused:  true,
		mode:    "burst",
		seed:    time.Now().UnixNano(),
	}
}

func (m Model) Init() tea.Cmd {
	return tick()
}

func tick() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.relayout()
		m.refreshLogs()
		return m, nil

	case tickMsg:
		if !m.paused && m.mode == "rate" {
			if m.lastRate.IsZero() || time.Since(m.lastRate) >= m.cfg.RateEvery {
				m.lastRate = time.Now()
				cmds = append(cmds, m.scheduleBurst(m.cfg.RateCount))
			}
		}
		cmds = append(cmds, tick())
		return m, tea.Batch(cmds...)

	case sendMsg:
		m.metrics.MarkSent()
		return m, nil

	case doneMsg:
		if msg.Err != nil {
			m.metrics.MarkFail(msg.Latency)
		} else {
			m.metrics.MarkOK(msg.Latency)
		}
		m.appendLog(msg)
		m.refreshLogs()
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "ctrl+q":
			return m, tea.Quit
		case "1":
			m.active = tabDashboard
		case "2":
			m.active = tabLogs
		case "3":
			m.active = tabConfig
	case "b":
		cmds = append(cmds, m.scheduleBurst(m.cfg.BurstCount))
		case "r":
			m.mode = "rate"
			m.paused = false
			m.lastRate = time.Now()
		case "p":
			m.paused = true
		case "+":
			m.cfg.BurstCount += 10
		case "-":
			if m.cfg.BurstCount > 1 {
				m.cfg.BurstCount -= 10
			}
		case "]":
			m.cfg.RateCount += 1
		case "[":
			if m.cfg.RateCount > 1 {
				m.cfg.RateCount -= 1
			}
		case "=":
			m.cfg.FreePct = clampPct(m.cfg.FreePct + 5)
		case "_":
			m.cfg.FreePct = clampPct(m.cfg.FreePct - 5)
		case "t":
			if m.cfg.PaidTier == "pro" {
				m.cfg.PaidTier = "elite"
			} else {
				m.cfg.PaidTier = "pro"
			}
		case "s":
			m.mode = "burst"
			m.paused = true
		}
	}

	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	if m.width == 0 {
		return "Loading…"
	}

	header := m.renderHeader()
	var body string
	switch m.active {
	case tabDashboard:
		body = m.renderDashboard()
	case tabLogs:
		body = m.renderLogs()
	case tabConfig:
		body = m.renderConfig()
	}
	footer := m.renderFooter()

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m *Model) relayout() {
	bodyHeight := m.height - 4
	if bodyHeight < 6 {
		bodyHeight = 6
	}
	m.logVP.Width = m.width - 4
	m.logVP.Height = bodyHeight - 2
}

func (m *Model) refreshLogs() {
	if m.active != tabLogs {
		return
	}
	m.logVP.SetContent(m.renderLogContent())
	m.logVP.GotoBottom()
}

func (m *Model) scheduleBurst(n int) tea.Cmd {
	if n <= 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, n)
	for i := 0; i < n; i++ {
		cmds = append(cmds, m.sendOne())
	}
	return tea.Batch(cmds...)
}

func (m *Model) sendOne() tea.Cmd {
	seed := m.seed
	m.seed++
	userID, tier := m.pickUser(seed)
	prompt := randomPrompt(seed)
	id := fmt.Sprintf("req-%d", time.Now().UnixNano())
	started := time.Now()
	send := func() tea.Msg {
		return sendMsg{ID: id, UserID: userID, Tier: tier, Prompt: prompt, Started: started}
	}
	run := func() tea.Msg {
		return runRequest(m.cfg.MasterURL, id, userID, tier, prompt, started)
	}
	return tea.Batch(send, run)
}

func runRequest(masterURL, id, userID, tier, prompt string, started time.Time) tea.Msg {
	maxRetries := 5
	var lastErr error
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	for attempt := 0; attempt < maxRetries; attempt++ {
		resp, err := sendChat(ctx, masterURL, userID, tier, prompt)
		if err == nil {
			return doneMsg{ID: id, UserID: userID, Tier: tier, Prompt: prompt, Reply: resp.Reply, Latency: time.Since(started), Finished: time.Now()}
		}
		lastErr = err
		// Don't retry on client errors (4xx) - server rejected it
		if strings.Contains(err.Error(), "server error 4") {
			break
		}
		// Brief pause before retry
		time.Sleep(100 * time.Millisecond)
	}

	return doneMsg{ID: id, UserID: userID, Tier: tier, Prompt: prompt, Err: lastErr, Latency: time.Since(started), Finished: time.Now()}
}

func (m Model) pickUser(seed int64) (string, string) {
	r := rand.New(rand.NewSource(seed))
	if r.Intn(100) < m.cfg.FreePct {
		return fmt.Sprintf("%s-free-%d", m.cfg.UserIDBase, r.Intn(100000)), "free"
	}
	return fmt.Sprintf("%s-paid-%d", m.cfg.UserIDBase, r.Intn(100000)), m.cfg.PaidTier
}

func randomPrompt(seed int64) string {
	words := []string{
		"Explain", "summarize", "compare", "list", "outline", "design",
		"distributed", "systems", "load", "balancing", "GPU", "cluster",
		"RAG", "retrieval", "latency", "throughput", "autoscaling",
		"fault", "tolerance", "health", "check", "queue",
	}
	r := rand.New(rand.NewSource(seed))
	count := 6 + r.Intn(6)
	var b strings.Builder
	for i := 0; i < count; i++ {
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString(words[r.Intn(len(words))])
	}
	return b.String()
}

func (m *Model) appendLog(msg doneMsg) {
	reply := msg.Reply
	if reply == "" && msg.Err != nil {
		reply = msg.Err.Error()
	}
	short := truncate(reply, 60)
	status := "ok"
	if msg.Err != nil {
		status = "err"
	}
	entry := LogEntry{
		At:         msg.Finished,
		UserID:     msg.UserID,
		Tier:       msg.Tier,
		Prompt:     truncate(msg.Prompt, 60),
		ReplyShort: short,
		Latency:    msg.Latency,
		Status:     status,
	}
	if msg.Err != nil {
		entry.Error = msg.Err.Error()
	}
	m.logs = append(m.logs, entry)
	if len(m.logs) > 1000 {
		m.logs = m.logs[len(m.logs)-1000:]
	}
}

func (m Model) renderHeader() string {
	label := "Load Test Client"
	left := styleHeader.Render(label)
	active := []string{"Dashboard", "Logs", "Config"}
	var tabs []string
	for i, name := range active {
		if tab(i) == m.active {
			tabs = append(tabs, styleTabActive.Render(name))
		} else {
			tabs = append(tabs, styleTab.Render(name))
		}
	}
	right := strings.Join(tabs, " ")
	gap := max(1, m.width-lipgloss.Width(left)-lipgloss.Width(right)-2)
	return lipgloss.JoinHorizontal(lipgloss.Left, left, strings.Repeat(" ", gap), right)
}

func (m Model) renderDashboard() string {
	stats := m.metrics.Counters()
	lat := computeLatency(m.metrics.latencies)
	rate := computeRates(m.metrics.sentWindow, m.metrics.completedWindow, m.metrics.sent, m.metrics.ok+m.metrics.fail, time.Since(m.metrics.start))

	status := "paused"
	if !m.paused && m.mode == "rate" {
		status = "rate"
	} else if !m.paused && m.mode == "burst" {
		status = "burst"
	}

	lines := []string{
		fmt.Sprintf("Master: %s", m.cfg.MasterURL),
		fmt.Sprintf("Mode: %s  |  Free: %d%%  |  Paid tier: %s", status, m.cfg.FreePct, m.cfg.PaidTier),
		fmt.Sprintf("Burst size: %d  |  Rate: %d every %s", m.cfg.BurstCount, m.cfg.RateCount, m.cfg.RateEvery),
		"",
		fmt.Sprintf("In-flight: %d  OK: %d  Failed: %d  Total: %d", stats.Waiting, stats.OK, stats.Failed, stats.Total),
		fmt.Sprintf("Latency avg: %s  p50: %s  p95: %s  p99: %s", fmtDuration(lat.Avg), fmtDuration(lat.P50), fmtDuration(lat.P95), fmtDuration(lat.P99)),
		fmt.Sprintf("Latency min: %s  max: %s", fmtDuration(lat.Min), fmtDuration(lat.Max)),
		fmt.Sprintf("Sent/s: %.2f  Done/s: %.2f  (window %.0fs)", rate.SentPerSec, rate.CompletedPerSec, rate.WindowSeconds),
		fmt.Sprintf("Overall sent/s: %.2f  Overall done/s: %.2f", rate.SentPerSecOverall, rate.CompletedPerSecOverall),
	}

	panel := stylePanel.Width(m.width - 2).Render(strings.Join(lines, "\n"))
	return panel
}

func (m Model) renderLogs() string {
	content := m.logVP.View()
	if strings.TrimSpace(content) == "" {
		content = "No logs yet."
	}
	return stylePanel.Width(m.width - 2).Render(content)
}

func (m Model) renderLogContent() string {
	var b strings.Builder
	for _, entry := range m.logs {
		timeStr := entry.At.Format("15:04:05")
		status := styleOK.Render("OK")
		if entry.Status != "ok" {
			status = styleErr.Render("ERR")
		}
		line := fmt.Sprintf("%s %s %s %s | %s | %s | %s",
			timeStr,
			status,
			pad(entry.Tier, 5),
			pad(entry.UserID, 18),
			pad(entry.Prompt, 40),
			pad(entry.ReplyShort, 60),
			fmtDuration(entry.Latency),
		)
		b.WriteString(line + "\n")
	}
	return b.String()
}

func (m Model) renderConfig() string {
	lines := []string{
		"Controls:",
		"  1/2/3  → switch tabs",
		"  b      → send burst",
		"  r      → start rate mode",
		"  s      → stop rate mode",
		"  p      → pause (stop sending)",
		"  + / -  → burst size +/-10",
		"  [ / ]  → rate size +/-1",
		"  = / _  → free percent +/-5",
		"  t      → toggle paid tier (pro/elite)",
		"  ctrl+c → quit and export report",
		"",
		"Config:",
		fmt.Sprintf("  Free percent: %d%%", m.cfg.FreePct),
		fmt.Sprintf("  Paid tier: %s", m.cfg.PaidTier),
		fmt.Sprintf("  Burst: %d", m.cfg.BurstCount),
		fmt.Sprintf("  Rate: %d every %s", m.cfg.RateCount, m.cfg.RateEvery),
		"",
		"Notes:",
		"  - Logs show prompt + reply snippets",
		"  - Report saved to ./client-load-report.json on exit",
	}
	return stylePanel.Width(m.width - 2).Render(strings.Join(lines, "\n"))
}

func (m Model) renderFooter() string {
	keys := "1-3 tabs  |  b burst  r start rate  s stop  p pause  t tier  ctrl+c quit"
	return styleKeyHint.Render(keys)
}

func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n <= 1 {
		return string(runes[:n])
	}
	return string(runes[:n-1]) + "…"
}

func pad(s string, n int) string {
	if utf8.RuneCountInString(s) >= n {
		return truncate(s, n)
	}
	return s + strings.Repeat(" ", n-utf8.RuneCountInString(s))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func fmtDuration(d time.Duration) string {
	if d <= 0 {
		return "0ms"
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
	return fmt.Sprintf("%.1fm", d.Minutes())
}

func clampPct(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func (m Model) Report() *SystemStats {
	stats := m.metrics.Snapshot(m.cfg)
	return &stats
}
