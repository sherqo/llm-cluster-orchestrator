package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/guptarohit/asciigraph"

	"master/lib"
)

// renderAutoscaling is the completely redesigned autoscaling tab.
// Layout (top→bottom, all scrollable via viewport):
//
//  ┌─ Status Bar ─────────────────────────────────────────────────────┐
//  │  Decision badge │ warnings │ live metrics row                    │
//  └──────────────────────────────────────────────────────────────────┘
//  ┌─ Graphs ─────────────────────────────────────────────────────────┐
//  │  Utilization (with threshold bands)                              │
//  │  Queue Depth + RPS                                               │
//  │  P95 Latency                                                     │
//  └──────────────────────────────────────────────────────────────────┘
//  ┌─ Decision Engine ────────────────────────────────────────────────┐
//  │  progress bars + timers + cooldown                               │
//  └──────────────────────────────────────────────────────────────────┘
//  ┌─ Recent Actions ─────────────────────────────────────────────────┐
//  └──────────────────────────────────────────────────────────────────┘
//  ┌─ Config Summary ─────────────────────────────────────────────────┐
//  └──────────────────────────────────────────────────────────────────┘

func (m model) renderAutoscaling() string {
	a := m.snap.autoscaling
	mets := a.Metrics
	cfg := a.Config
	stats := m.snap.startupStats
	w := m.usableWidth()
	gw := m.graphWidth()

	var b strings.Builder

	// ── 1. Status Bar ────────────────────────────────────────────────────────
	b.WriteString(sectionTitle("AutoScale — Live Status", "", w))
	b.WriteString(m.renderStatusBar(a, mets, w))

	// ── 2. Graphs ─────────────────────────────────────────────────────────────
	window := m.hist
	if m.zoomStart >= 0 && m.zoomStart < len(m.hist) {
		window = m.hist[m.zoomStart:]
	}

	zoomLabel := ""
	if m.zoomStart >= 0 {
		zoomLabel = fmt.Sprintf(" · zoom: %d pts", len(window))
	}
	b.WriteString(sectionTitle("Graphs"+zoomLabel, "[z] zoom  [←→] shift", w))

	if len(window) < 2 {
		b.WriteString(empty(fmt.Sprintf("Collecting history… (%d/2 ticks)", len(m.hist))))
	} else {
		b.WriteString(m.renderUtilizationGraph(window, gw, cfg))
		b.WriteString(m.renderQueueRPSGraph(window, gw))
		b.WriteString(m.renderLatencyGraph(window, gw))
	}

	// ── 3. Decision Engine ────────────────────────────────────────────────────
	b.WriteString(sectionTitle("Decision Engine", "", w))
	b.WriteString(m.renderDecisionPanel(a, mets, cfg, gw))

	// ── 4. Startup Stats ──────────────────────────────────────────────────────
	b.WriteString(sectionTitle("Worker Startup Times", "", w))
	b.WriteString(m.renderStartupStats(stats))

	// ── 5. Recent Actions ─────────────────────────────────────────────────────
	if len(a.RecentActions) > 0 {
		b.WriteString(sectionTitle("Recent Scaling Actions", fmt.Sprintf("%d total", len(a.RecentActions)), w))
		b.WriteString(m.renderRecentActions(a, gw))
	}

	// ── 6. Agent Health ───────────────────────────────────────────────────────
	if len(a.AgentHealth) > 0 {
		b.WriteString(sectionTitle("Agent Health", "", w))
		b.WriteString(m.renderAgentHealth(a))
	}

	// ── 7. Config ─────────────────────────────────────────────────────────────
	b.WriteString(sectionTitle("Configuration", "", w))
	b.WriteString(m.renderConfig(cfg))

	return b.String()
}

// ── Status Bar ───────────────────────────────────────────────────────────────

func (m model) renderStatusBar(a lib.AutoscalerSnapshot, mets lib.ClusterMetrics, w int) string {
	// Decision badge
	var decisionBg lipgloss.Color
	switch a.ScalingDecision {
	case "scale_up":
		decisionBg = clrRed
	case "scale_down":
		decisionBg = clrYellow
	default:
		decisionBg = clrGreen
	}
	decision := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#0d1117")).
		Background(decisionBg).
		Padding(0, 2).
		Render(" " + strings.ToUpper(a.ScalingDecision) + " ")

	warnings := ""
	if m.detectOscillation() {
		warnings += " " + lipgloss.NewStyle().Bold(true).Foreground(clrRed).Render("⚠ OSCILLATING")
	}
	if m.detectOverscaling() {
		warnings += " " + lipgloss.NewStyle().Bold(true).Foreground(clrYellow).Render("⚠ OVERSCALING")
	}

	// Compact metric pills on same line
	pills := []string{
		metricPill("Workers", fmt.Sprintf("%d/%d", mets.HealthyWorkers, mets.TotalWorkers), clrGreen),
		metricPill("In-Flight", fmt.Sprintf("%d", mets.InFlight), clrYellow),
		metricPill("Queue", fmt.Sprintf("%.0f", mets.QueueDepth), clrCyan),
		metricPill("RPS", fmt.Sprintf("%.1f", mets.RequestsPerSec), clrPurple),
		metricPill("P95", fmt.Sprintf("%.2fs", mets.P95Latency), clrOrange),
		metricPill("Util", fmt.Sprintf("%.0f%%", mets.WorkerUtilization*100), clrAccent),
	}
	pillRow := strings.Join(pills, "  ")

	reasonClr := clrText
	if a.ScalingDecision == "scale_up" {
		reasonClr = clrRed
	} else if a.ScalingDecision == "scale_down" {
		reasonClr = clrYellow
	}
	reason := "  " + lipgloss.NewStyle().Foreground(reasonClr).Italic(true).Render(a.DecisionReason)

	return fmt.Sprintf("  %s%s\n  %s%s\n", decision, warnings, pillRow, reason)
}

func metricPill(label, val string, clr lipgloss.Color) string {
	l := lipgloss.NewStyle().Foreground(clrMuted).Render(label + ":")
	v := lipgloss.NewStyle().Foreground(clr).Bold(true).Render(val)
	return l + " " + v
}

// ── Decision Panel ───────────────────────────────────────────────────────────

func (m model) renderDecisionPanel(a lib.AutoscalerSnapshot, mets lib.ClusterMetrics, cfg lib.AutoscalerConfig, gw int) string {
	var b strings.Builder
	barW := gw - 30
	if barW < 20 {
		barW = 20
	}

	// Utilization bars
	b.WriteString(decisionRow("Current Util ", progressBar(mets.WorkerUtilization, barW), fmt.Sprintf("%.1f%%", mets.WorkerUtilization*100)))
	b.WriteString(decisionRow("Target Util  ", progressBar(cfg.TargetUtilization, barW), fmt.Sprintf("%.0f%%", cfg.TargetUtilization*100)))
	b.WriteString(decisionRow("Scale-Up ↑   ", progressBar(cfg.ScaleUpThreshold, barW), fmt.Sprintf("%.0f%%", cfg.ScaleUpThreshold*100)))
	b.WriteString(decisionRow("Scale-Down ↓ ", progressBar(cfg.ScaleDownThreshold, barW), fmt.Sprintf("%.0f%%", cfg.ScaleDownThreshold*100)))

	// Hysteresis band
	b.WriteString(fmt.Sprintf("  %s  %s\n",
		lipgloss.NewStyle().Foreground(clrMuted).Render("Hysteresis   "),
		hysteresisBand(cfg, mets.WorkerUtilization, barW+12),
	))

	// Cooldown
	cd := a.CooldownRemaining
	cdStr := lipgloss.NewStyle().Foreground(clrGreen).Render("none (ready)")
	if cd > 0 {
		cdStr = lipgloss.NewStyle().Foreground(clrRed).Render(cd.Round(time.Second).String() + " remaining")
	}
	b.WriteString(fmt.Sprintf("  %s  %s\n",
		lipgloss.NewStyle().Foreground(clrMuted).Render("Cooldown     "),
		cdStr,
	))

	// Sustained timers
	if a.HighLoadSet {
		elapsed := time.Since(a.HighLoadSince).Round(time.Second)
		ratio := float64(elapsed) / float64(cfg.ScaleUpSustained)
		remaining := cfg.ScaleUpSustained - elapsed
		if remaining < 0 {
			remaining = 0
		}
		b.WriteString(decisionRow("High Load ↑  ", progressBar(ratio, barW),
			fmt.Sprintf("%s / %s", elapsed, cfg.ScaleUpSustained)))
	} else {
		b.WriteString(fmt.Sprintf("  %s  %s\n",
			lipgloss.NewStyle().Foreground(clrMuted).Render("High Load ↑  "),
			lipgloss.NewStyle().Foreground(clrMuted).Render("inactive"),
		))
	}
	if a.LowLoadSet {
		elapsed := time.Since(a.LowLoadSince).Round(time.Second)
		ratio := float64(elapsed) / float64(cfg.ScaleDownSustained)
		remaining := cfg.ScaleDownSustained - elapsed
		if remaining < 0 {
			remaining = 0
		}
		b.WriteString(decisionRow("Low Load ↓   ", progressBar(ratio, barW),
			fmt.Sprintf("%s / %s", elapsed, cfg.ScaleDownSustained)))
	} else {
		b.WriteString(fmt.Sprintf("  %s  %s\n",
			lipgloss.NewStyle().Foreground(clrMuted).Render("Low Load ↓   "),
			lipgloss.NewStyle().Foreground(clrMuted).Render("inactive"),
		))
	}

	// Desired vs actual
	b.WriteString(fmt.Sprintf("  %s  %s  %s\n",
		lipgloss.NewStyle().Foreground(clrMuted).Render("Workers      "),
		lipgloss.NewStyle().Foreground(clrText).Bold(true).Render(fmt.Sprintf("desired=%d", a.DesiredWorkers)),
		lipgloss.NewStyle().Foreground(clrGreen).Render(fmt.Sprintf("healthy=%d", mets.HealthyWorkers)),
	))

	return b.String()
}

func decisionRow(label, bar, val string) string {
	l := lipgloss.NewStyle().Foreground(clrMuted).Render(label)
	v := lipgloss.NewStyle().Foreground(clrText).Render(val)
	return fmt.Sprintf("  %s  %s  %s\n", l, bar, v)
}

// ── Startup Stats ─────────────────────────────────────────────────────────────

func (m model) renderStartupStats(stats lib.StartupStats) string {
	if stats.TotalStartups == 0 {
		return empty("No startup data yet")
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("  %s  %s   %s  %s   %s  %s/%s\n",
		lipgloss.NewStyle().Foreground(clrMuted).Render("Avg:"),
		lipgloss.NewStyle().Foreground(clrGreen).Bold(true).Render(stats.AvgDuration.Round(time.Second).String()),
		lipgloss.NewStyle().Foreground(clrMuted).Render("P95:"),
		lipgloss.NewStyle().Foreground(clrYellow).Bold(true).Render(stats.P95Duration.Round(time.Second).String()),
		lipgloss.NewStyle().Foreground(clrMuted).Render("Failed:"),
		lipgloss.NewStyle().Foreground(clrRed).Render(fmt.Sprintf("%d", stats.FailedStartups)),
		lipgloss.NewStyle().Foreground(clrText).Render(fmt.Sprintf("%d", stats.TotalStartups)),
	))
	return b.String()
}

// ── Recent Actions ────────────────────────────────────────────────────────────

func (m model) renderRecentActions(a lib.AutoscalerSnapshot, gw int) string {
	var b strings.Builder
	start := 0
	if len(a.RecentActions) > 10 {
		start = len(a.RecentActions) - 10
	}
	for _, act := range a.RecentActions[start:] {
		icon := "●"
		clr := clrText
		switch act.Action {
		case "up":
			icon = "▲"
			clr = clrRed
		case "down":
			icon = "▼"
			clr = clrGreen
		}
		ts := lipgloss.NewStyle().Foreground(clrMuted).Render(act.Time.Format("15:04:05"))
		dir := lipgloss.NewStyle().Foreground(clr).Bold(true).Render(fmt.Sprintf("%s %-4s +%d", icon, act.Action, act.Count))
		reason := lipgloss.NewStyle().Foreground(clrText).Render(trim(act.Reason, gw-32))
		b.WriteString(fmt.Sprintf("  %s  %s  %s\n", ts, dir, reason))
	}
	return b.String()
}

// ── Agent Health ──────────────────────────────────────────────────────────────

func (m model) renderAgentHealth(a lib.AutoscalerSnapshot) string {
	var b strings.Builder
	for id, h := range a.AgentHealth {
		status := lipgloss.NewStyle().Foreground(clrGreen).Render("● ok")
		if time.Now().Before(h.PenalizedUntil) {
			remaining := time.Until(h.PenalizedUntil).Round(time.Second)
			status = lipgloss.NewStyle().Foreground(clrRed).Render(fmt.Sprintf("⛔ penalized %s", remaining))
		} else if h.ConsecutiveFailures > 0 {
			failClr := clrYellow
			if h.ConsecutiveFailures >= 3 {
				failClr = clrRed
			}
			status = lipgloss.NewStyle().Foreground(failClr).Render(fmt.Sprintf("⚠ %d consecutive failures", h.ConsecutiveFailures))
		}
		b.WriteString(fmt.Sprintf("  %-40s  %s\n", trim(id, 40), status))
	}
	return b.String()
}

// ── Config Summary ────────────────────────────────────────────────────────────

func (m model) renderConfig(cfg lib.AutoscalerConfig) string {
	var b strings.Builder
	// 3-column compact table
	rows := [][3]string{
		{"Min Workers", fmt.Sprintf("%d", cfg.MinWorkers), "Max Workers  " + fmt.Sprintf("%d", cfg.MaxWorkers)},
		{"Up Step", fmt.Sprintf("%d", cfg.MaxScaleUpStep), "Down Step    " + fmt.Sprintf("%d", cfg.MaxScaleDownStep)},
		{"Up Cooldown", cfg.ScaleUpCooldown.String(), "Down Cooldown  " + cfg.ScaleDownCooldown.String()},
		{"Up Sustained", cfg.ScaleUpSustained.String(), "Down Sustained " + cfg.ScaleDownSustained.String()},
		{"P95 Ceiling", fmt.Sprintf("%.1fs", cfg.P95LatencyCeiling), "Err Rate Ceil  " + fmt.Sprintf("%.0f%%", cfg.ErrorRateCeiling*100)},
		{"RPS Trend", fmt.Sprintf("%.1f", cfg.RPSTrendThreshold), "Pre-warm       " + fmt.Sprintf("%d", cfg.PrewarmWorkers)},
	}
	for _, row := range rows {
		k1 := lipgloss.NewStyle().Foreground(clrMuted).Render(fmt.Sprintf("%-16s", row[0]))
		v1 := lipgloss.NewStyle().Foreground(clrText).Bold(true).Render(fmt.Sprintf("%-12s", row[1]))
		kv2 := lipgloss.NewStyle().Foreground(clrMuted).Render(row[2])
		b.WriteString(fmt.Sprintf("  %s %s  %s\n", k1, v1, kv2))
	}
	return b.String()
}

// ── Graph renderers ───────────────────────────────────────────────────────────

func (m model) renderUtilizationGraph(window []histPoint, width int, cfg lib.AutoscalerConfig) string {
	vals := make([]float64, len(window))
	upLine := make([]float64, len(window))
	downLine := make([]float64, len(window))
	targetLine := make([]float64, len(window))
	for i, p := range window {
		vals[i] = p.utilization * 100
		upLine[i] = cfg.ScaleUpThreshold * 100
		downLine[i] = cfg.ScaleDownThreshold * 100
		targetLine[i] = cfg.TargetUtilization * 100
	}
	graph := asciigraph.PlotMany(
		[][]float64{vals, upLine, downLine, targetLine},
		asciigraph.Height(6),
		asciigraph.Width(width),
		asciigraph.Caption("Worker Utilization %   [Util] [Up↑] [Down↓] [Target]"),
		asciigraph.SeriesColors(
			asciigraph.ColorNames["lime"],
			asciigraph.ColorNames["red"],
			asciigraph.ColorNames["teal"],
			asciigraph.ColorNames["yellow"],
		),
	)
	return indentGraph(graph) + "\n"
}

func (m model) renderQueueRPSGraph(window []histPoint, width int) string {
	queue := make([]float64, len(window))
	rps := make([]float64, len(window))
	for i, p := range window {
		v := p.queueDepth
		if v < 0 {
			v = 0
		}
		queue[i] = v
		rps[i] = p.rps
	}
	graph := asciigraph.PlotMany(
		[][]float64{queue, rps},
		asciigraph.Height(5),
		asciigraph.Width(width),
		asciigraph.Caption("Queue Depth  vs  Requests/sec"),
		asciigraph.SeriesColors(
			asciigraph.ColorNames["cyan"],
			asciigraph.ColorNames["magenta"],
		),
	)
	return indentGraph(graph) + "\n"
}

func (m model) renderLatencyGraph(window []histPoint, width int) string {
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
		asciigraph.Height(5),
		asciigraph.Width(width),
		asciigraph.Caption("P95 Latency (seconds)"),
	)
	return indentGraph(graph) + "\n"
}

func indentGraph(g string) string {
	lines := strings.Split(g, "\n")
	for i := range lines {
		lines[i] = "  " + lines[i]
	}
	return strings.Join(lines, "\n")
}

// ── Oscillation / overscaling ─────────────────────────────────────────────────

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
	return float64(altCount)/float64(len(last)-1) > 0.7 && len(last) >= 6
}

func (m model) detectOverscaling() bool {
	a := m.snap.autoscaling
	if len(a.RecentActions) < 4 {
		return false
	}
	last := a.RecentActions[len(a.RecentActions)-4:]
	for i := 1; i < len(last); i++ {
		if last[i].Action == "down" && last[i-1].Action == "up" {
			if last[i].Time.Sub(last[i-1].Time) < 2*time.Minute {
				return true
			}
		}
	}
	return false
}
