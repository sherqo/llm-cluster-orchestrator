package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// ── Design tokens ────────────────────────────────────────────────────────────

var (
	// Background / surface
	clrBg      = lipgloss.Color("#0d1117")
	clrSurface = lipgloss.Color("#161b22")
	clrBorder  = lipgloss.Color("#30363d")

	// Semantic
	clrAccent = lipgloss.Color("#58a6ff")
	clrMuted  = lipgloss.Color("#8b949e")
	clrText   = lipgloss.Color("#e6edf3")
	clrGreen  = lipgloss.Color("#3fb950")
	clrYellow = lipgloss.Color("#e3b341")
	clrRed    = lipgloss.Color("#f85149")
	clrOrange = lipgloss.Color("#db6d28")
	clrCyan   = lipgloss.Color("#79c0ff")
	clrPurple = lipgloss.Color("#bc8cff")

	// suppress unused
	_ = clrBg
	_ = clrSurface
	_ = clrOrange
)

// ── Header ───────────────────────────────────────────────────────────────────
// Single-line: title on left, live metrics on right. Height = 1 row.

func (m model) renderHeader() string {
	w := m.width
	if w <= 0 {
		w = 80
	}

	workerCount := len(m.snap.workers)
	healthyCount := 0
	for _, wk := range m.snap.workers {
		if wk.Lifecycle.String() == "healthy" {
			healthyCount++
		}
	}

	title := lipgloss.NewStyle().Bold(true).Foreground(clrAccent).
		Background(clrSurface).Render("  ⬡ LLM Cluster  ")

	pills := strings.Join([]string{
		pill(fmt.Sprintf("%d/%d", healthyCount, workerCount), "workers", clrGreen),
		pill(fmt.Sprintf("%d", len(m.snap.agents)), "agents", clrCyan),
		pill(fmt.Sprintf("%d", len(m.snap.inflight)), "in-flight", clrYellow),
		pill(string(m.snap.strategy), "strategy", clrPurple),
		pill(m.snap.uptime.Round(time.Second).String(), "uptime", clrMuted),
	}, "  ")

	gap := w - lipgloss.Width(title) - lipgloss.Width(pills) - 2
	if gap < 1 {
		gap = 1
	}

	line := title + strings.Repeat(" ", gap) + pills + " "
	
	// Truncate to exact width to prevent terminal auto-wrap
	if lipgloss.Width(line) > w {
		line = line[:w]
	}

	return lipgloss.NewStyle().
		Background(clrSurface).
		Render(line)
}

func pill(val, label string, clr lipgloss.Color) string {
	v := lipgloss.NewStyle().Foreground(clr).Bold(true).Render(val)
	l := lipgloss.NewStyle().Foreground(clrMuted).Render(" " + label)
	return v + l
}

// ── Tabs ─────────────────────────────────────────────────────────────────────
// Height = 1 row (tab bar) + 1 row (divider) = 2 rows total.

func (m model) renderTabs() string {
	w := m.width
	if w <= 0 {
		w = 80
	}

	var parts []string
	for i, label := range tabLabels {
		keyStr := fmt.Sprintf("%d", i+1)
		
		if i == int(m.activeTab) {
			tab := lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#ffffff")).
				Background(lipgloss.Color("#1c2431")).
				Underline(true).
				Render(" " + keyStr + " " + label + " ")
			parts = append(parts, tab)
		} else {
			key := lipgloss.NewStyle().Foreground(lipgloss.Color("#555e6e")).Render(" " + keyStr)
			lbl := lipgloss.NewStyle().Foreground(clrMuted).Render(" " + label + " ")
			parts = append(parts, key+lbl)
		}
	}

	tabBar := strings.Join(parts, "")
	fill := w - lipgloss.Width(tabBar)
	if fill > 0 {
		tabBar += lipgloss.NewStyle().Background(clrBg).Render(strings.Repeat(" ", fill))
	}
	divider := lipgloss.NewStyle().Foreground(clrBorder).Render(strings.Repeat("─", w))
	return tabBar + "\n" + divider
}

// ── Footer ───────────────────────────────────────────────────────────────────
// Height = 1 row.

func (m model) renderFooter() string {
	w := m.width
	if w <= 0 {
		w = 80
	}

	// Left: status + error
	left := lipgloss.NewStyle().Foreground(clrText).Render(m.status)
	if m.lastError != "" {
		left += "  " + lipgloss.NewStyle().Foreground(clrRed).Render("✗ " + m.lastError)
	}

	// Middle: input prompt OR keybinds
	var middle string
	if m.inputMode == "add" {
		middle = lipgloss.NewStyle().Foreground(clrYellow).Render("Add worker: ") +
			lipgloss.NewStyle().Foreground(clrText).Render(m.inputValue) +
			lipgloss.NewStyle().Foreground(clrAccent).Render("█") +
			lipgloss.NewStyle().Foreground(clrMuted).Render("  [Enter] confirm  [Esc] cancel")
	} else if m.activeTab == tabAutoscaling {
		middle = keyHints([][2]string{{"z", "zoom"}, {"←→", "shift zoom"}, {"r", "refresh"}, {"q", "quit"}})
	} else {
		middle = keyHints([][2]string{{"↑↓", "scroll"}, {"a", "add"}, {"d", "drain"}, {"x", "remove"}, {"s", "strategy"}, {"r", "refresh"}, {"q", "quit"}})
	}

	// Right: scroll %
	pct := fmt.Sprintf("%.0f%%", m.vp.ScrollPercent()*100)
	right := lipgloss.NewStyle().Foreground(clrMuted).Render(pct)

	// Compose safely without exceeding terminal width
	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	midW := lipgloss.Width(middle)
	
	// If it's too small, just return a truncated left string
	if w < leftW+rightW+4 {
		return lipgloss.NewStyle().Background(clrSurface).Foreground(clrMuted).Width(w).Render(left)
	}

	totalUsed := leftW + midW + rightW + 4 // padding
	gap1 := (w - totalUsed) / 2
	gap2 := w - totalUsed - gap1
	if gap1 < 1 { gap1 = 1 }
	if gap2 < 1 { gap2 = 1 }

	line := " " + left + strings.Repeat(" ", gap1) + middle + strings.Repeat(" ", gap2) + right + " "
	
	// Truncate to exact width to prevent terminal auto-wrap
	if lipgloss.Width(line) > w {
		line = line[:w]
	}

	return lipgloss.NewStyle().
		Background(clrSurface).
		Foreground(clrMuted).
		Render(line)
}

func keyHints(pairs [][2]string) string {
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		k := lipgloss.NewStyle().Foreground(clrAccent).Bold(true).Render("[" + p[0] + "]")
		l := lipgloss.NewStyle().Foreground(clrMuted).Render(p[1])
		parts = append(parts, k+" "+l)
	}
	return strings.Join(parts, "  ")
}
