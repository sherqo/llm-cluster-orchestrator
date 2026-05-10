package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"master/lib"
)

// ── Layout helpers ────────────────────────────────────────────────────────────

// usableWidth returns the inner content width (terminal width with a small margin).
func (m model) usableWidth() int {
	w := m.width
	if w <= 0 {
		w = 80
	}
	return w
}

// graphWidth returns width for ASCII graphs (usable width minus 4 for indent/labels).
func (m model) graphWidth() int {
	w := m.usableWidth() - 6
	if w < 30 {
		w = 30
	}
	return w
}

// ── Progress bar ──────────────────────────────────────────────────────────────

func progressBar(ratio float64, width int) string {
	if width < 10 {
		width = 10
	}
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}

	inner := width - 2
	filled := int(ratio * float64(inner))
	if filled > inner {
		filled = inner
	}
	empty := inner - filled

	var barClr lipgloss.Color
	switch {
	case ratio > 0.8:
		barClr = clrRed
	case ratio > 0.5:
		barClr = clrYellow
	default:
		barClr = clrGreen
	}

	fill := lipgloss.NewStyle().Foreground(barClr).Render(strings.Repeat("█", filled))
	emp := lipgloss.NewStyle().Foreground(clrBorder).Render(strings.Repeat("░", empty))
	return lipgloss.NewStyle().Foreground(clrBorder).Render("[") + fill + emp +
		lipgloss.NewStyle().Foreground(clrBorder).Render("]")
}

// ── Hysteresis band ───────────────────────────────────────────────────────────

func hysteresisBand(cfg lib.AutoscalerConfig, currentUtil float64, width int) string {
	if width < 20 {
		width = 20
	}

	downAt := int(cfg.ScaleDownThreshold * float64(width))
	upAt := int(cfg.ScaleUpThreshold * float64(width))
	currAt := int(currentUtil * float64(width))
	if downAt > width {
		downAt = width
	}
	if upAt > width {
		upAt = width
	}
	if currAt > width {
		currAt = width
	}

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(clrBorder).Render("["))
	for i := 0; i < width; i++ {
		switch {
		case i == currAt:
			b.WriteString(lipgloss.NewStyle().Foreground(clrText).Bold(true).Render("◆"))
		case i < downAt:
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#58a6ff")).Render("·"))
		case i < upAt:
			b.WriteString(lipgloss.NewStyle().Foreground(clrYellow).Render("▒"))
		default:
			b.WriteString(lipgloss.NewStyle().Foreground(clrRed).Render("▓"))
		}
	}
	b.WriteString(lipgloss.NewStyle().Foreground(clrBorder).Render("]"))
	b.WriteString(lipgloss.NewStyle().Foreground(clrMuted).
		Render(fmt.Sprintf("  ↓%.0f%%  tgt%.0f%%  ↑%.0f%%",
			cfg.ScaleDownThreshold*100,
			cfg.TargetUtilization*100,
			cfg.ScaleUpThreshold*100,
		)))
	return b.String()
}

// ── String helpers ────────────────────────────────────────────────────────────

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-1] + "…"
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
