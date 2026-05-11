package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"master/lib"
)

// ── Workers tab ───────────────────────────────────────────────────────────────

func (m model) renderWorkers() string {
	var b strings.Builder
	w := m.usableWidth()

	b.WriteString(sectionTitle("Workers", fmt.Sprintf("%d total", len(m.snap.workers)), w))

	// Column headers
	hdr := fmt.Sprintf("  %-2s  %-24s  %-20s  %-10s  %7s  %s",
		"", "ID", "Address", "State", "Active", "Agent")
	b.WriteString(lipgloss.NewStyle().Foreground(clrMuted).Render(hdr) + "\n")
	b.WriteString(rule(w))

	for i, wk := range m.snap.workers {
		cursor := "  "
		bg := lipgloss.NewStyle()
		if i == m.selected {
			cursor = "▶ "
			bg = bg.Background(lipgloss.Color("#1c2431"))
		}

		stateStr, stateClr := lifecycleStyle(wk.Lifecycle.String())
		state := lipgloss.NewStyle().Foreground(stateClr).Bold(true).Width(10).Render(stateStr)
		active := lipgloss.NewStyle().Foreground(clrYellow).Render(fmt.Sprintf("%7d", wk.ActiveRequests))
		agent := trim(wk.AgentID, 20)
		if agent == "" {
			agent = lipgloss.NewStyle().Foreground(clrMuted).Italic(true).Render("(manual)")
		}

		line := fmt.Sprintf("  %s%-24s  %-20s  %s  %s  %s",
			cursor, trim(wk.ID, 24), trim(wk.Addr, 20), state, active, agent)
		b.WriteString(bg.Render(line) + "\n")
	}
	if len(m.snap.workers) == 0 {
		b.WriteString(empty("No workers registered"))
	}
	return b.String()
}

// ── Agents tab ────────────────────────────────────────────────────────────────

func (m model) renderAgents() string {
	var b strings.Builder
	w := m.usableWidth()

	b.WriteString(sectionTitle("Agents", fmt.Sprintf("%d registered", len(m.snap.agents)), w))
	b.WriteString(lipgloss.NewStyle().Foreground(clrMuted).Render(
		fmt.Sprintf("  %-30s  %-24s  %8s  %s\n", "Agent ID", "Address", "Workers", "Uptime"),
	))
	b.WriteString(rule(w))

	for _, a := range m.snap.agents {
		addr := fmt.Sprintf("%s:%d", a.Host, a.Port)
		uptime := time.Since(a.AddedAt).Round(time.Second)
		line := fmt.Sprintf("  %-30s  %-24s  %8d  %s",
			trim(a.AgentID, 30), trim(addr, 24), a.WorkerCount, uptime)
		b.WriteString(line + "\n")
	}
	if len(m.snap.agents) == 0 {
		b.WriteString(empty("No agents registered"))
	}

	b.WriteString("\n")
	b.WriteString(sectionTitle("Workers by Agent", "", w))

	agentWorkers := make(map[string][]lib.WorkerSnapshot)
	for _, wk := range m.snap.workers {
		aid := wk.AgentID
		if aid == "" {
			aid = "(manual)"
		}
		agentWorkers[aid] = append(agentWorkers[aid], wk)
	}

	renderGroup := func(label string, workers []lib.WorkerSnapshot) {
		b.WriteString(fmt.Sprintf("  %s  —  %d worker(s)\n",
			lipgloss.NewStyle().Foreground(clrCyan).Bold(true).Render(label),
			len(workers)))
		for j, wk := range workers {
			con := "├─"
			if j == len(workers)-1 {
				con = "└─"
			}
			stateStr, stateClr := lifecycleStyle(wk.Lifecycle.String())
			state := lipgloss.NewStyle().Foreground(stateClr).Render(stateStr)
			b.WriteString(fmt.Sprintf("    %s %-26s  %-12s  active=%d\n",
				con, trim(wk.Addr, 26), state, wk.ActiveRequests))
		}
	}

	for _, a := range m.snap.agents {
		renderGroup(trim(a.AgentID, 40), agentWorkers[a.AgentID])
	}
	if manual, ok := agentWorkers["(manual)"]; ok {
		renderGroup("(manual)", manual)
	}
	if len(m.snap.workers) == 0 && len(m.snap.agents) == 0 {
		b.WriteString(empty("No workers or agents"))
	}

	return b.String()
}

// ── InFlight tab ──────────────────────────────────────────────────────────────

func (m model) renderInflight() string {
	var b strings.Builder
	w := m.usableWidth()
	now := time.Now()

	b.WriteString(sectionTitle("In-Flight Requests", fmt.Sprintf("%d active", len(m.snap.inflight)), w))
	b.WriteString(lipgloss.NewStyle().Foreground(clrMuted).Render(
		fmt.Sprintf("  %-36s  %-22s  %10s\n", "Request ID", "Worker", "Elapsed"),
	))
	b.WriteString(rule(w))

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
		el := lipgloss.NewStyle().Foreground(elClr).Render(fmt.Sprintf("%10s", elapsed))
		b.WriteString(fmt.Sprintf("  %-36s  %-22s  %s\n",
			trim(req.RequestID, 36), trim(req.Worker, 22), el))
	}
	if len(m.snap.inflight) == 0 {
		b.WriteString(empty("No requests in flight"))
	}

	b.WriteString("\n")
	b.WriteString(sectionTitle("Recent Completed", fmt.Sprintf("last %d", min(25, len(m.snap.recent))), w))
	b.WriteString(lipgloss.NewStyle().Foreground(clrMuted).Render(
		fmt.Sprintf("  %-36s  %-22s  %10s\n", "Request ID", "Worker", "Duration"),
	))
	b.WriteString(rule(w))

	start := 0
	if len(m.snap.recent) > 25 {
		start = len(m.snap.recent) - 25
	}
	for _, req := range m.snap.recent[start:] {
		b.WriteString(fmt.Sprintf("  %-36s  %-22s  %10s\n",
			trim(req.RequestID, 36), trim(req.Worker, 22), req.Duration.Round(time.Millisecond)))
	}
	if len(m.snap.recent) == 0 {
		b.WriteString(empty("No completed requests yet"))
	}
	return b.String()
}

// ── Logs tab ──────────────────────────────────────────────────────────────────
// Lines are word-wrapped to terminal width so nothing is cut off.

func (m model) renderLogs() string {
	var b strings.Builder
	w := m.usableWidth()

	b.WriteString(sectionTitle("System Logs", fmt.Sprintf("%d entries (scroll ↑↓)", len(m.snap.logs)), w))

	// Reserve space for timestamp + place prefix
	const prefix = "  00:00:00  placeholder     "
	prefixW := utf8.RuneCountInString(prefix)
	wrapW := w - prefixW
	if wrapW < 20 {
		wrapW = 20
	}

	// Show all logs; viewport provides the scrolling
	start := 0
	if len(m.snap.logs) > 500 {
		start = len(m.snap.logs) - 500
	}
	for _, l := range m.snap.logs[start:] {
		ts := lipgloss.NewStyle().Foreground(clrMuted).Render(l.Time.Format("15:04:05"))
		place := lipgloss.NewStyle().Foreground(clrCyan).
			Width(14).Render(trim(l.Place, 14))

		// Word-wrap the message
		words := strings.Fields(l.Msg)
		lines := wordWrap(words, wrapW)
		for i, line := range lines {
			if i == 0 {
				b.WriteString(fmt.Sprintf("  %s  %s  %s\n", ts, place, line))
			} else {
				b.WriteString(fmt.Sprintf("  %s  %s  %s\n",
					strings.Repeat(" ", 8),
					strings.Repeat(" ", 14),
					line))
			}
		}
	}
	if len(m.snap.logs) == 0 {
		b.WriteString(empty("No logs yet"))
	}
	return b.String()
}

// ── Notifications/Events tab ──────────────────────────────────────────────────

func (m model) renderEvents() string {
	var b strings.Builder
	w := m.usableWidth()

	b.WriteString(sectionTitle("System Notifications", fmt.Sprintf("%d events (scroll ↑↓)", len(m.snap.events)), w))

	const prefix = "  00:00:00  [system]        "
	prefixW := utf8.RuneCountInString(prefix)
	wrapW := w - prefixW
	if wrapW < 20 {
		wrapW = 20
	}

	start := 0
	if len(m.snap.events) > 500 {
		start = len(m.snap.events) - 500
	}
	
	for _, e := range m.snap.events[start:] {
		ts := lipgloss.NewStyle().Foreground(clrMuted).Render(e.Time.Format("15:04:05"))
		place := lipgloss.NewStyle().Foreground(clrPurple).Bold(true).Width(14).Render("[" + trim(e.Place, 12) + "]")

		words := strings.Fields(e.Msg)
		lines := wordWrap(words, wrapW)
		for i, line := range lines {
			if i == 0 {
				b.WriteString(fmt.Sprintf("  %s  %s  %s\n", ts, place, lipgloss.NewStyle().Foreground(clrText).Render(line)))
			} else {
				b.WriteString(fmt.Sprintf("  %s  %s  %s\n",
					strings.Repeat(" ", 8),
					strings.Repeat(" ", 14),
					lipgloss.NewStyle().Foreground(clrText).Render(line)))
			}
		}
	}
	
	if len(m.snap.events) == 0 {
		b.WriteString(empty("No system notifications yet"))
	}
	return b.String()
}

// wordWrap splits words into lines of at most maxW characters.
func wordWrap(words []string, maxW int) []string {
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	current := ""
	for _, word := range words {
		if current == "" {
			current = word
		} else if utf8.RuneCountInString(current)+1+utf8.RuneCountInString(word) <= maxW {
			current += " " + word
		} else {
			lines = append(lines, current)
			current = word
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
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

	// ── Cluster box ──────────────────────────────────────────────────────────
	b.WriteString(sectionTitle("Cluster Overview", "", w))

	col := (w - 4) / 2
	left := []string{
		kv("Total Workers", strconv.Itoa(len(m.snap.workers)), clrText),
		kv("  ├ Healthy", strconv.Itoa(healthyCount), clrGreen),
		kv("  ├ Starting/Warming", strconv.Itoa(startingCount), clrYellow),
		kv("  └ Draining", strconv.Itoa(drainingCount), clrOrange),
		"",
		kv("Agents", strconv.Itoa(len(m.snap.agents)), clrText),
		kv("In-Flight", strconv.Itoa(len(m.snap.inflight)), clrText),
		kv("Active Requests", strconv.FormatInt(totalActive, 10), clrYellow),
		kv("Strategy", string(m.snap.strategy), clrPurple),
		kv("Uptime", m.snap.uptime.Round(time.Second).String(), clrMuted),
	}
	right := []string{
		kv("Successes", strconv.FormatInt(totalSuccess, 10), clrGreen),
		kv("Failures", strconv.FormatInt(totalFails, 10), clrRed),
	}

	if len(m.snap.recent) > 0 {
		var sum float64
		durs := make([]float64, 0, len(m.snap.recent))
		for _, r := range m.snap.recent {
			d := r.Duration.Seconds()
			sum += d
			durs = append(durs, d)
		}
		sort.Float64s(durs)
		avg := sum / float64(len(durs))
		p95 := durs[min(int(float64(len(durs))*0.95), len(durs)-1)]
		right = append(right, "",
			kv("Avg Latency", fmt.Sprintf("%.3fs", avg), clrText),
			kv("P95 Latency", fmt.Sprintf("%.3fs", p95), clrText),
			kv("Samples", strconv.Itoa(len(durs)), clrMuted),
		)
	}

	maxR := max(len(left), len(right))
	for i := 0; i < maxR; i++ {
		lStr, rStr := "", ""
		if i < len(left) {
			lStr = left[i]
		}
		if i < len(right) {
			rStr = right[i]
		}
		lPad := lipgloss.NewStyle().Width(col).Render(lStr)
		b.WriteString("  " + lPad + "  " + rStr + "\n")
	}

	// ── Agents ───────────────────────────────────────────────────────────────
	b.WriteString("\n")
	b.WriteString(sectionTitle("Agents", "", w))
	for _, a := range m.snap.agents {
		addr := a.Host + ":" + strconv.Itoa(a.Port)
		b.WriteString(fmt.Sprintf("  %-32s  %-24s  workers=%d\n",
			trim(a.AgentID, 32), trim(addr, 24), a.WorkerCount))
	}
	if len(m.snap.agents) == 0 {
		b.WriteString(empty("No agents"))
	}

	return b.String()
}

// ── Shared section helpers ────────────────────────────────────────────────────

// sectionTitle renders a labelled rule: "  Title   meta\n────────\n"
func sectionTitle(title, meta string, width int) string {
	t := lipgloss.NewStyle().Bold(true).Foreground(clrAccent).Render(title)
	m := ""
	if meta != "" {
		m = "  " + lipgloss.NewStyle().Foreground(clrMuted).Render(meta)
	}
	return "\n  " + t + m + "\n" + rule(width)
}

func rule(width int) string {
	if width <= 0 {
		width = 80
	}
	return lipgloss.NewStyle().Foreground(clrBorder).Render(strings.Repeat("─", width)) + "\n"
}

func empty(msg string) string {
	return "  " + lipgloss.NewStyle().Foreground(clrMuted).Italic(true).Render(msg) + "\n"
}

func kv(label, val string, clr lipgloss.Color) string {
	l := lipgloss.NewStyle().Foreground(clrMuted).Render(fmt.Sprintf("%-24s", label))
	v := lipgloss.NewStyle().Foreground(clr).Bold(true).Render(val)
	return l + v
}

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
	case "stopping", "dead":
		return "✗ " + state, clrRed
	default:
		return state, clrMuted
	}
}
