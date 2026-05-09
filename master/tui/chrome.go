package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// ── Design tokens ────────────────────────────────────────────────────────────

var (
	clrBg      = lipgloss.Color("#0d1117")
	clrSurface = lipgloss.Color("#161b22")
	clrBorder  = lipgloss.Color("#30363d")
	clrAccent  = lipgloss.Color("#58a6ff")
	clrMuted   = lipgloss.Color("#8b949e")
	clrText    = lipgloss.Color("#e6edf3")
	clrGreen   = lipgloss.Color("#3fb950")
	clrYellow  = lipgloss.Color("#d29922")
	clrRed     = lipgloss.Color("#f85149")
	clrOrange  = lipgloss.Color("#db6d28")
	clrCyan    = lipgloss.Color("#79c0ff")
	clrPurple  = lipgloss.Color("#bc8cff")

	styleHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(clrText).
			Background(lipgloss.Color("#1c2431")).
			Padding(0, 2)

	stylePill = lipgloss.NewStyle().
			Padding(0, 1).
			Bold(true)

	styleTabActive = lipgloss.NewStyle().
			Bold(true).
			Foreground(clrBg).
			Background(clrAccent).
			Padding(0, 1)

	styleTabInactive = lipgloss.NewStyle().
				Foreground(clrMuted).
				Padding(0, 1)

	styleFooter = lipgloss.NewStyle().
			Foreground(clrMuted).
			Background(clrSurface).
			Padding(0, 1)

	styleSectionTitle = lipgloss.NewStyle().
				Bold(true).
				Foreground(clrAccent)

	styleKey = lipgloss.NewStyle().
			Foreground(clrAccent).
			Bold(true)

	_ = styleKey     // suppress unused
	_ = styleSectionTitle
	_ = clrBg
	_ = clrSurface
	_ = clrOrange
)

// ── Header ───────────────────────────────────────────────────────────────────

func (m model) renderHeader() string {
	workerCount := len(m.snap.workers)
	healthyCount := 0
	for _, w := range m.snap.workers {
		if w.Lifecycle.String() == "healthy" {
			healthyCount++
		}
	}

	healthy := stylePill.Foreground(clrGreen).Render(fmt.Sprintf("✓ %d/%d workers", healthyCount, workerCount))
	agents := stylePill.Foreground(clrCyan).Render(fmt.Sprintf("⬡ %d agents", len(m.snap.agents)))
	inflight := stylePill.Foreground(clrYellow).Render(fmt.Sprintf("⟳ %d in-flight", len(m.snap.inflight)))
	strategy := stylePill.Foreground(clrPurple).Render(fmt.Sprintf("⚖ %s", m.snap.strategy))
	uptime := stylePill.Foreground(clrMuted).Render(fmt.Sprintf("⏱ %s", m.snap.uptime.Round(time.Second)))

	title := lipgloss.NewStyle().Bold(true).Foreground(clrAccent).Render("⬡ LLM Cluster")
	pills := strings.Join([]string{healthy, agents, inflight, strategy, uptime}, "  ")

	w := m.width
	if w <= 0 {
		w = 80
	}
	gap := w - lipgloss.Width(title) - lipgloss.Width(pills) - 4
	if gap < 1 {
		gap = 1
	}
	line := title + strings.Repeat(" ", gap) + pills
	return styleHeader.Width(w).Render(line)
}

// ── Tabs ─────────────────────────────────────────────────────────────────────

func (m model) renderTabs() string {
	keys := []string{"1", "2", "3", "4", "5", "6"}
	parts := make([]string, 0, len(tabLabels))
	for i, label := range tabLabels {
		num := lipgloss.NewStyle().Foreground(clrMuted).Render(keys[i])
		if i == int(m.activeTab) {
			parts = append(parts, styleTabActive.Render(num+label))
		} else {
			parts = append(parts, styleTabInactive.Render(num+label))
		}
	}
	tabBar := strings.Join(parts, "")
	w := m.width
	if w <= 0 {
		w = 80
	}
	fill := w - lipgloss.Width(tabBar)
	if fill > 0 {
		tabBar += strings.Repeat(" ", fill)
	}
	divider := lipgloss.NewStyle().Foreground(clrBorder).Render(strings.Repeat("─", w))
	return tabBar + "\n" + divider
}

// ── Footer ───────────────────────────────────────────────────────────────────

func (m model) renderFooter() string {
	var keysStr string
	switch {
	case m.inputMode == "add":
		keysStr = fmt.Sprintf("  ✎ add worker: %s%s",
			lipgloss.NewStyle().Foreground(clrText).Render(m.inputValue),
			lipgloss.NewStyle().Foreground(clrAccent).Render("▌"))
	case m.activeTab == tabAutoscaling:
		keysStr = keyHint("z", "zoom") + "  " + keyHint("←→", "scroll") + "  " + keyHint("r", "refresh") + "  " + keyHint("q", "quit")
	default:
		keysStr = keyHint("j/k", "sel") + "  " + keyHint("a", "add") + "  " +
			keyHint("d", "drain") + "  " + keyHint("x", "rm") + "  " +
			keyHint("s", "strategy") + "  " + keyHint("r", "refresh") + "  " + keyHint("q", "quit")
	}

	statusPart := lipgloss.NewStyle().Foreground(clrText).Render("  " + m.status)
	errPart := ""
	if m.lastError != "" {
		errPart = "  " + lipgloss.NewStyle().Foreground(clrRed).Render("✗ "+m.lastError)
	}
	pct := fmt.Sprintf("%3.0f%%", m.vp.ScrollPercent()*100)
	pctPart := lipgloss.NewStyle().Foreground(clrMuted).Render("  " + pct)

	w := m.width
	if w <= 0 {
		w = 80
	}
	left := statusPart + errPart + "  " + keysStr
	right := pctPart
	gap := w - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	line := left + strings.Repeat(" ", gap) + right
	return styleFooter.Width(w).Render(line)
}

func keyHint(key, label string) string {
	k := lipgloss.NewStyle().Foreground(clrAccent).Bold(true).Render("[" + key + "]")
	l := lipgloss.NewStyle().Foreground(clrMuted).Render(label)
	return k + " " + l
}
