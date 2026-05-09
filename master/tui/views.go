package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"master/lib"
)

// ── Workers tab ───────────────────────────────────────────────────────────────

func (m model) renderWorkers() string {
	var b strings.Builder
	w := m.usableWidth()

	b.WriteString("\n")
	b.WriteString(sectionHeader("Workers", fmt.Sprintf("%d total", len(m.snap.workers)), w))
	b.WriteString("\n")

	// Column header
	b.WriteString(lipgloss.NewStyle().Foreground(clrMuted).Render(
		fmt.Sprintf("  %-3s %-24s %-20s %-10s %7s  %-16s\n",
			"", "ID", "Address", "State", "Active", "Agent"),
	))
	b.WriteString(dimLine(w))

	for i, wk := range m.snap.workers {
		sel := "   "
		rowStyle := lipgloss.NewStyle().Foreground(clrText)
		if i == m.selected {
			sel = " ▶ "
			rowStyle = rowStyle.Background(lipgloss.Color("#1c2431"))
		}

		stateStr, stateClr := lifecycleStyle(wk.Lifecycle.String())
		state := lipgloss.NewStyle().Foreground(stateClr).Bold(true).Render(stateStr)
		active := lipgloss.NewStyle().Foreground(clrYellow).Render(fmt.Sprintf("%7d", wk.ActiveRequests))
		id := trim(wk.ID, 24)
		addr := trim(wk.Addr, 20)
		agent := trim(wk.AgentID, 16)
		if agent == "" {
			agent = lipgloss.NewStyle().Foreground(clrMuted).Render("(manual)")
		}
		line := fmt.Sprintf("%s%-24s %-20s %-18s %s  %s", sel, id, addr, state, active, agent)
		b.WriteString(rowStyle.Render(line) + "\n")
	}

	if len(m.snap.workers) == 0 {
		b.WriteString(emptyState("No workers registered"))
	}

	return b.String()
}

// ── Agents tab ────────────────────────────────────────────────────────────────

func (m model) renderAgents() string {
	var b strings.Builder
	w := m.usableWidth()

	b.WriteString("\n")
	b.WriteString(sectionHeader("Registered Agents", fmt.Sprintf("%d total", len(m.snap.agents)), w))
	b.WriteString("\n")

	b.WriteString(lipgloss.NewStyle().Foreground(clrMuted).Render(
		fmt.Sprintf("  %-30s %-24s %8s  %s\n", "Agent ID", "Address", "Workers", "Uptime"),
	))
	b.WriteString(dimLine(w))

	for _, a := range m.snap.agents {
		addr := fmt.Sprintf("%s:%d", a.Host, a.Port)
		uptime := time.Since(a.AddedAt).Round(time.Second)
		line := fmt.Sprintf("  %-30s %-24s %8d  %s",
			trim(a.AgentID, 30), trim(addr, 24), a.WorkerCount, uptime)
		b.WriteString(line + "\n")
	}
	if len(m.snap.agents) == 0 {
		b.WriteString(emptyState("No agents registered"))
	}

	b.WriteString("\n")
	b.WriteString(sectionHeader("Workers by Agent", "", w))
	b.WriteString("\n")

	agentWorkers := make(map[string][]lib.WorkerSnapshot)
	for _, wk := range m.snap.workers {
		aid := wk.AgentID
		if aid == "" {
			aid = "(manual)"
		}
		agentWorkers[aid] = append(agentWorkers[aid], wk)
	}

	for _, a := range m.snap.agents {
		workers := agentWorkers[a.AgentID]
		agentLabel := lipgloss.NewStyle().Foreground(clrCyan).Bold(true).Render(trim(a.AgentID, 40))
		b.WriteString(fmt.Sprintf("  %s  —  %d worker(s)\n", agentLabel, len(workers)))
		for j, wk := range workers {
			connector := "├─"
			if j == len(workers)-1 {
				connector = "└─"
			}
			stateStr, stateClr := lifecycleStyle(wk.Lifecycle.String())
			state := lipgloss.NewStyle().Foreground(stateClr).Render(stateStr)
			b.WriteString(fmt.Sprintf("    %s %-26s %-10s active=%d\n",
				connector, trim(wk.Addr, 26), state, wk.ActiveRequests))
		}
	}
	if manual, ok := agentWorkers["(manual)"]; ok {
		b.WriteString(fmt.Sprintf("  %s  —  %d worker(s)\n",
			lipgloss.NewStyle().Foreground(clrMuted).Render("(manual)"), len(manual)))
		for _, wk := range manual {
			stateStr, stateClr := lifecycleStyle(wk.Lifecycle.String())
			state := lipgloss.NewStyle().Foreground(stateClr).Render(stateStr)
			b.WriteString(fmt.Sprintf("    └─ %-26s %-10s active=%d\n",
				trim(wk.Addr, 26), state, wk.ActiveRequests))
		}
	}

	return b.String()
}

// ── Inflight tab ──────────────────────────────────────────────────────────────

func (m model) renderInflight() string {
	var b strings.Builder
	w := m.usableWidth()
	now := time.Now()

	b.WriteString("\n")
	b.WriteString(sectionHeader("In-Flight Requests", fmt.Sprintf("%d active", len(m.snap.inflight)), w))
	b.WriteString("\n")

	b.WriteString(lipgloss.NewStyle().Foreground(clrMuted).Render(
		fmt.Sprintf("  %-36s %-22s %10s\n", "Request ID", "Worker", "Elapsed"),
	))
	b.WriteString(dimLine(w))

	for _, req := range m.snap.inflight {
		elapsed := now.Sub(req.StartedAt).Round(time.Millisecond)
		var elClr lipgloss.Color
		switch {
		case elapsed > 10*time.Second:
			elClr = clrRed
		case elapsed > 3*time.Second:
			elClr = clrYellow
		default:
			elClr = clrGreen
		}
		elStr := lipgloss.NewStyle().Foreground(elClr).Render(fmt.Sprintf("%10s", elapsed))
		b.WriteString(fmt.Sprintf("  %-36s %-22s %s\n",
			trim(req.RequestID, 36), trim(req.Worker, 22), elStr))
	}
	if len(m.snap.inflight) == 0 {
		b.WriteString(emptyState("No requests in flight"))
	}

	b.WriteString("\n")
	b.WriteString(sectionHeader("Recent Completed", fmt.Sprintf("%d shown", min(25, len(m.snap.recent))), w))
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Foreground(clrMuted).Render(
		fmt.Sprintf("  %-36s %-22s %10s\n", "Request ID", "Worker", "Duration"),
	))
	b.WriteString(dimLine(w))

	start := 0
	if len(m.snap.recent) > 25 {
		start = len(m.snap.recent) - 25
	}
	for _, req := range m.snap.recent[start:] {
		b.WriteString(fmt.Sprintf("  %-36s %-22s %10s\n",
			trim(req.RequestID, 36), trim(req.Worker, 22), req.Duration.Round(time.Millisecond)))
	}
	if len(m.snap.recent) == 0 {
		b.WriteString(emptyState("No completed requests yet"))
	}
	return b.String()
}

// ── Logs tab ──────────────────────────────────────────────────────────────────

func (m model) renderLogs() string {
	var b strings.Builder
	w := m.usableWidth()

	b.WriteString("\n")
	b.WriteString(sectionHeader("System Logs", fmt.Sprintf("%d entries", len(m.snap.logs)), w))
	b.WriteString("\n")

	// Show last 200 entries; viewport gives real scrolling
	start := 0
	if len(m.snap.logs) > 200 {
		start = len(m.snap.logs) - 200
	}
	for _, l := range m.snap.logs[start:] {
		ts := lipgloss.NewStyle().Foreground(clrMuted).Render(l.Time.Format("15:04:05"))
		place := lipgloss.NewStyle().Foreground(clrCyan).Render(fmt.Sprintf("%-12s", trim(l.Place, 12)))
		msg := l.Msg
		b.WriteString(fmt.Sprintf("  %s  %s  %s\n", ts, place, msg))
	}
	if len(m.snap.logs) == 0 {
		b.WriteString(emptyState("No logs yet"))
	}
	return b.String()
}

// ── Stats tab ─────────────────────────────────────────────────────────────────

func (m model) renderStats() string {
	var b strings.Builder
	w := m.usableWidth()

	var totalActive, totalFails, totalSuccess int64
	healthyCount, drainingCount, startingCount := 0, 0, 0

	for _, wk := range m.snap.workers {
		totalActive += wk.ActiveRequests
		totalFails += wk.Failures
		totalSuccess += wk.Successes
		switch wk.Lifecycle.String() {
		case "healthy":
			healthyCount++
		case "draining":
			drainingCount++
		case "starting", "warming":
			startingCount++
		}
	}

	b.WriteString("\n")
	b.WriteString(sectionHeader("Cluster Overview", "", w))
	b.WriteString("\n")

	// Two-column layout
	leftCol := []string{
		statRow("Total Workers", strconv.Itoa(len(m.snap.workers))),
		statRow("  ├ Healthy", greenVal(strconv.Itoa(healthyCount))),
		statRow("  ├ Starting", yellowVal(strconv.Itoa(startingCount))),
		statRow("  └ Draining", redVal(strconv.Itoa(drainingCount))),
		statRow("Agents", strconv.Itoa(len(m.snap.agents))),
		statRow("In-Flight", strconv.Itoa(len(m.snap.inflight))),
		statRow("Strategy", string(m.snap.strategy)),
		statRow("Uptime", m.snap.uptime.Round(time.Second).String()),
	}
	rightCol := []string{
		statRow("Successes", greenVal(strconv.FormatInt(totalSuccess, 10))),
		statRow("Failures", redVal(strconv.FormatInt(totalFails, 10))),
		statRow("Active Reqs", yellowVal(strconv.FormatInt(totalActive, 10))),
	}

	if len(m.snap.recent) > 0 {
		var sum float64
		durations := make([]float64, 0, len(m.snap.recent))
		for _, r := range m.snap.recent {
			d := r.Duration.Seconds()
			sum += d
			durations = append(durations, d)
		}
		avg := sum / float64(len(durations))
		sort.Float64s(durations)
		p95Idx := int(float64(len(durations)) * 0.95)
		if p95Idx >= len(durations) {
			p95Idx = len(durations) - 1
		}
		rightCol = append(rightCol,
			statRow("Avg Latency", fmt.Sprintf("%.3fs", avg)),
			statRow("P95 Latency", fmt.Sprintf("%.3fs", durations[p95Idx])),
			statRow("Samples", strconv.Itoa(len(durations))),
		)
	}

	colW := (w - 4) / 2
	maxRows := max(len(leftCol), len(rightCol))
	for i := 0; i < maxRows; i++ {
		l := ""
		r := ""
		if i < len(leftCol) {
			l = leftCol[i]
		}
		if i < len(rightCol) {
			r = rightCol[i]
		}
		lPad := lipgloss.NewStyle().Width(colW).Render(l)
		b.WriteString("  " + lPad + "  " + r + "\n")
	}

	b.WriteString("\n")
	b.WriteString(sectionHeader("Agents", "", w))
	b.WriteString("\n")
	for _, a := range m.snap.agents {
		addr := a.Host + ":" + strconv.Itoa(a.Port)
		b.WriteString(fmt.Sprintf("  %-32s  %-22s  workers=%d\n",
			trim(a.AgentID, 32), trim(addr, 22), a.WorkerCount))
	}
	if len(m.snap.agents) == 0 {
		b.WriteString(emptyState("No agents"))
	}

	return b.String()
}

// ── Shared helpers ────────────────────────────────────────────────────────────

func sectionHeader(title, meta string, width int) string {
	t := lipgloss.NewStyle().Bold(true).Foreground(clrAccent).Render("  " + title)
	m := ""
	if meta != "" {
		m = "  " + lipgloss.NewStyle().Foreground(clrMuted).Render(meta)
	}
	return t + m + "\n" + dimLine(width)
}

func dimLine(width int) string {
	if width <= 0 {
		width = 80
	}
	return lipgloss.NewStyle().Foreground(clrBorder).Render(strings.Repeat("─", width)) + "\n"
}

func emptyState(msg string) string {
	return lipgloss.NewStyle().Foreground(clrMuted).Italic(true).Render("  "+msg) + "\n"
}

func statRow(label, val string) string {
	l := lipgloss.NewStyle().Foreground(clrMuted).Render(label)
	v := lipgloss.NewStyle().Foreground(clrText).Bold(true).Render(val)
	return fmt.Sprintf("%-22s  %s", l, v)
}

func greenVal(s string) string  { return lipgloss.NewStyle().Foreground(clrGreen).Render(s) }
func yellowVal(s string) string { return lipgloss.NewStyle().Foreground(clrYellow).Render(s) }
func redVal(s string) string    { return lipgloss.NewStyle().Foreground(clrRed).Render(s) }

func lifecycleStyle(state string) (string, lipgloss.Color) {
	switch state {
	case "healthy":
		return "● healthy", clrGreen
	case "starting":
		return "◌ starting", clrYellow
	case "warming":
		return "◑ warming", clrCyan
	case "draining":
		return "◐ draining", clrOrange
	default:
		return "✗ " + state, clrRed
	}
}
