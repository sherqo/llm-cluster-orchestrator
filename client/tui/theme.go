// Package tui implements the terminal chat interface for the LLM cluster client.
package tui

import "github.com/charmbracelet/lipgloss"

// Color palette – dark, inspired by OpenCode / Catppuccin Mocha.
var (
	// Base surfaces
	colorBase    = lipgloss.Color("#1e1e2e") // main background
	colorMantle  = lipgloss.Color("#181825") // sidebar / dimmer panels
	colorCrust   = lipgloss.Color("#11111b") // deepest borders
	colorSurface = lipgloss.Color("#313244") // elevated surfaces, input bg
	colorOverlay = lipgloss.Color("#45475a") // subtle dividers

	// Text
	colorText    = lipgloss.Color("#cdd6f4") // primary text
	colorSubtext = lipgloss.Color("#a6adc8") // secondary text
	colorMuted   = lipgloss.Color("#6c7086") // placeholder / muted

	// Accents
	colorAccent  = lipgloss.Color("#89b4fa") // blue – user messages, titles
	colorGreen   = lipgloss.Color("#a6e3a1") // assistant messages
	colorRed     = lipgloss.Color("#f38ba8") // errors
	colorYellow  = lipgloss.Color("#f9e2af") // warnings / timestamps
	colorMauve   = lipgloss.Color("#cba6f7") // session titles / highlights
	colorSky     = lipgloss.Color("#89dceb") // request-id / meta info
	colorPeach   = lipgloss.Color("#fab387") // tier badge
)

// ---------------------------------------------------------------------------
// Shared styles (defined once; cheap to reuse)
// ---------------------------------------------------------------------------

var (
	// Sidebar (left panel)
	styleSidebar = lipgloss.NewStyle().
			Background(colorMantle).
			BorderRight(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(colorOverlay)

	// Sidebar header title
	styleSidebarTitle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorMauve).
				Background(colorCrust).
				Padding(0, 1)

	// Session item – normal
	styleSessionItem = lipgloss.NewStyle().
				Foreground(colorSubtext).
				PaddingLeft(1)

	// Session item – selected
	styleSessionSelected = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorAccent).
				Background(colorSurface).
				PaddingLeft(1)

	// Main chat area
	styleMain = lipgloss.NewStyle().
			Background(colorBase)

	// Chat header bar
	styleHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorText).
			Background(colorCrust).
			Padding(0, 2)

	// User bubble
	styleUserBubble = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	// Assistant bubble
	styleAssistantBubble = lipgloss.NewStyle().
				Foreground(colorGreen)

	// Error bubble
	styleErrorBubble = lipgloss.NewStyle().
				Foreground(colorRed).
				Italic(true)

	// System / meta bubble (timestamps, request IDs)
	styleMetaBubble = lipgloss.NewStyle().
				Foreground(colorMuted).
				Italic(true)

	// Input container
	styleInputContainer = lipgloss.NewStyle().
				Background(colorSurface).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorAccent).
				Padding(0, 1)

	// Input container inactive (not focused)
	styleInputContainerBlur = lipgloss.NewStyle().
				Background(colorSurface).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorOverlay).
				Padding(0, 1)

	// Footer / status bar
	styleFooter = lipgloss.NewStyle().
			Foreground(colorMuted).
			Background(colorCrust).
			Padding(0, 2)

	// Loading spinner label
	styleSpinner = lipgloss.NewStyle().
			Foreground(colorYellow)

	// Tier badge
	styleTier = lipgloss.NewStyle().
			Foreground(colorPeach).
			Bold(true)

	// Request-ID inline
	styleReqID = lipgloss.NewStyle().
			Foreground(colorSky).
			Faint(true)
)
