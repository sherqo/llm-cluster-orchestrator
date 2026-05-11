package loadtui

import "github.com/charmbracelet/lipgloss"

var (
	colorBase    = lipgloss.Color("#141414")
	colorSurface = lipgloss.Color("#1f1f1f")
	colorBorder  = lipgloss.Color("#303030")
	colorText    = lipgloss.Color("#e8e3d6")
	colorMuted   = lipgloss.Color("#a19788")
	colorAccent  = lipgloss.Color("#d08c60")
	colorGreen   = lipgloss.Color("#8fbf7f")
	colorRed     = lipgloss.Color("#d46a6a")
	colorGold    = lipgloss.Color("#e5b567")

	styleHeader = lipgloss.NewStyle().
		Background(colorSurface).
		Foreground(colorText).
		Bold(true).
		Padding(0, 1)

	styleTab = lipgloss.NewStyle().
		Foreground(colorMuted).
		Padding(0, 1)

	styleTabActive = lipgloss.NewStyle().
		Foreground(colorText).
		Background(colorSurface).
		Bold(true).
		Padding(0, 1)

	stylePanel = lipgloss.NewStyle().
		Background(colorBase).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBorder).
		Padding(0, 1)

	styleKeyHint = lipgloss.NewStyle().
		Foreground(colorMuted)

	styleOK = lipgloss.NewStyle().
		Foreground(colorGreen)

	styleErr = lipgloss.NewStyle().
		Foreground(colorRed)

	styleWarn = lipgloss.NewStyle().
		Foreground(colorGold)
)
