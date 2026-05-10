package tui

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// Config holds the runtime configuration passed from main.
type Config struct {
	MasterURL string
	UserID    string
	Tier      string
}

// ---------------------------------------------------------------------------
// Messages (tea.Msg)
// ---------------------------------------------------------------------------

type chatResponseMsg struct {
	sessionID string
	resp      chatResponse
}

type chatErrorMsg struct {
	sessionID string
	err       error
}

type tickMsg time.Time

// ---------------------------------------------------------------------------
// Focus zones
// ---------------------------------------------------------------------------

type focus int

const (
	focusInput focus = iota
	focusSidebar
)

// ---------------------------------------------------------------------------
// Application model
// ---------------------------------------------------------------------------

const sidebarWidth = 28

// Model is the root Bubble Tea model.
type Model struct {
	cfg Config

	// Layout
	width  int
	height int

	// UI components
	input    textarea.Model
	viewport viewport.Model
	spinner  spinner.Model

	// State
	sessions       []Session
	activeIdx      int  // index into sessions
	loading        bool // waiting for API response
	focus          focus
	sessionCount   int  // monotonically increasing ID counter
	sidebarVisible bool
}

// New creates a fully initialized Model.
func New(cfg Config) Model {
	// Textarea input
	ta := textarea.New()
	ta.Placeholder = "Type a message…  (Enter to send, Shift+Enter for newline)"
	ta.ShowLineNumbers = false
	ta.CharLimit = 4096
	ta.SetHeight(3)
	ta.Focus()

	// Viewport for chat messages
	vp := viewport.New(80, 20)
	vp.SetContent("")

	// Spinner (dots style)
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = styleSpinner

	m := Model{
		cfg:            cfg,
		input:          ta,
		viewport:       vp,
		spinner:        sp,
		focus:          focusInput,
		sidebarVisible: true,
	}
	m.newSession() // create the first session
	return m
}

// ---------------------------------------------------------------------------
// Bubble Tea interface
// ---------------------------------------------------------------------------

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		textarea.Blink,
		m.spinner.Tick,
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	// ── Window resize ────────────────────────────────────────────────────────
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.relayout()
		m.refreshViewport()
		return m, nil

	// ── Spinner tick ─────────────────────────────────────────────────────────
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	// ── API response ─────────────────────────────────────────────────────────
	case chatResponseMsg:
		m.loading = false
		for i := range m.sessions {
			if m.sessions[i].ID == msg.sessionID {
				m.sessions[i].Messages = append(m.sessions[i].Messages, Message{
					Role:      RoleAssistant,
					Content:   msg.resp.Reply,
					RequestID: msg.resp.RequestID,
					At:        time.Now(),
				})
				// Update session title from first reply (first 30 chars)
				if m.sessions[i].Title == "New Session" && msg.resp.Reply != "" {
					title := msg.resp.Reply
					if utf8.RuneCountInString(title) > 28 {
						runes := []rune(title)
						title = string(runes[:28]) + "…"
					}
					m.sessions[i].Title = title
				}
				break
			}
		}
		m.refreshViewport()
		return m, nil

	case chatErrorMsg:
		m.loading = false
		for i := range m.sessions {
			if m.sessions[i].ID == msg.sessionID {
				m.sessions[i].Messages = append(m.sessions[i].Messages, Message{
					Role:    RoleError,
					Content: msg.err.Error(),
					At:      time.Now(),
				})
				break
			}
		}
		m.refreshViewport()
		return m, nil

	// ── Keyboard ─────────────────────────────────────────────────────────────
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit

		// Toggle sidebar visibility
		case "ctrl+b":
			m.sidebarVisible = !m.sidebarVisible
			m.relayout()
			m.refreshViewport()
			return m, nil

		// Switch focus: Tab cycles between sidebar / input
		case "tab":
			if m.focus == focusInput {
				m.focus = focusSidebar
				m.input.Blur()
			} else {
				m.focus = focusInput
				m.input.Focus()
			}
			return m, nil

		// Sidebar navigation
		case "up", "k":
			if m.focus == focusSidebar && m.activeIdx > 0 {
				m.activeIdx--
				m.refreshViewport()
			}
		case "down", "j":
			if m.focus == focusSidebar && m.activeIdx < len(m.sessions)-1 {
				m.activeIdx++
				m.refreshViewport()
			}

		// New session
		case "ctrl+n":
			m.newSession()
			m.activeIdx = len(m.sessions) - 1
			m.focus = focusInput
			m.input.Focus()
			m.input.Reset()
			m.refreshViewport()
			return m, nil

		// Delete current session
		case "ctrl+d":
			if len(m.sessions) > 1 {
				m.sessions = append(m.sessions[:m.activeIdx], m.sessions[m.activeIdx+1:]...)
				if m.activeIdx >= len(m.sessions) {
					m.activeIdx = len(m.sessions) - 1
				}
				m.refreshViewport()
			}
			return m, nil

		// Send message
		case "enter":
			if m.focus == focusInput && !m.loading {
				prompt := strings.TrimSpace(m.input.Value())
				if prompt != "" {
					cmd := m.sendPrompt(prompt)
					cmds = append(cmds, cmd)
				}
			}

		// Shift+Enter → newline in input (bubbles textarea handles this natively)
		}
	}

	// ── Delegate to sub-components ───────────────────────────────────────────
	if m.focus == focusInput {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)
	} else {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

func (m Model) View() string {
	if m.width == 0 {
		return "Loading…"
	}

	if !m.sidebarVisible {
		return m.renderChatArea()
	}

	sidebar := m.renderSidebar()
	chatArea := m.renderChatArea()

	return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, chatArea)
}

// ---------------------------------------------------------------------------
// Layout helpers
// ---------------------------------------------------------------------------

func (m *Model) relayout() {
	chatWidth := m.width
	if m.sidebarVisible {
		chatWidth = m.width - sidebarWidth - 1 // -1 for border
	}
	if chatWidth < 20 {
		chatWidth = 20
	}

	inputHeight := 5 // 3 content + 2 border
	headerHeight := 1
	footerHeight := 1
	vpHeight := m.height - headerHeight - inputHeight - footerHeight - 2
	if vpHeight < 4 {
		vpHeight = 4
	}

	m.viewport.Width = chatWidth - 2 // inner padding
	m.viewport.Height = vpHeight

	m.input.SetWidth(chatWidth - 4) // account for border + padding
}

func (m *Model) refreshViewport() {
	if m.activeIdx >= len(m.sessions) {
		return
	}
	content := m.renderMessages(m.sessions[m.activeIdx])
	m.viewport.SetContent(content)
	m.viewport.GotoBottom()
}

// ---------------------------------------------------------------------------
// Sidebar render
// ---------------------------------------------------------------------------

func (m Model) renderSidebar() string {
	var b strings.Builder

	// Title bar
	title := styleSidebarTitle.Width(sidebarWidth).Render("  LLM Chat")
	b.WriteString(title + "\n")

	// Session list
	for i, s := range m.sessions {
		label := truncate(s.Title, sidebarWidth-4)
		var row string
		if i == m.activeIdx {
			icon := "▶ "
			row = styleSessionSelected.Width(sidebarWidth - 2).Render(icon + label)
		} else {
			icon := "  "
			row = styleSessionItem.Width(sidebarWidth - 2).Render(icon + label)
		}
		b.WriteString(row + "\n")
	}

	// Fill remaining height
	used := 1 + len(m.sessions) + 3
	remaining := m.height - used
	for i := 0; i < remaining; i++ {
		b.WriteString(strings.Repeat(" ", sidebarWidth) + "\n")
	}

	// Bottom hint
	hint := styleMetaBubble.Width(sidebarWidth - 2).Render("  Tab ⇄  ^N new  ^D del  ^B hide")
	b.WriteString("\n" + hint)

	return styleSidebar.
		Width(sidebarWidth).
		Height(m.height).
		Render(b.String())
}

// ---------------------------------------------------------------------------
// Chat area render
// ---------------------------------------------------------------------------

func (m Model) renderChatArea() string {
	chatWidth := m.width
	if m.sidebarVisible {
		chatWidth = m.width - sidebarWidth - 1
	}
	if chatWidth < 20 {
		chatWidth = 20
	}

	// Header
	sessionTitle := "New Session"
	tier := m.cfg.Tier
	if m.activeIdx < len(m.sessions) {
		sessionTitle = m.sessions[m.activeIdx].Title
	}
	left := truncate(sessionTitle, chatWidth-20)
	right := styleTier.Render("[" + tier + "]") + "  " +
		styleMetaBubble.Render(m.cfg.UserID)
	gap := chatWidth - lipgloss.Width(left) - lipgloss.Width(right) - 4
	if gap < 1 {
		gap = 1
	}
	header := styleHeader.Width(chatWidth).Render(
		left + strings.Repeat(" ", gap) + right,
	)

	// Viewport (chat messages)
	vp := styleMain.Width(chatWidth).Render(m.viewport.View())

	// Input area
	var inputBox string
	if m.loading {
		spinnerStr := m.spinner.View()
		inputBox = styleInputContainerBlur.Width(chatWidth - 2).Render(
			styleSpinner.Render(spinnerStr + "  Waiting for response…"),
		)
	} else {
		style := styleInputContainer
		if m.focus != focusInput {
			style = styleInputContainerBlur
		}
		inputBox = style.Width(chatWidth - 2).Render(m.input.View())
	}

	// Footer / key hints
	scrollInfo := fmt.Sprintf("↑↓ scroll  %d%%", m.scrollPercent())
	sendHint := "Enter → send"
	if m.loading {
		sendHint = styleSpinner.Render("sending…")
	}
	footer := styleFooter.Width(chatWidth).Render(
		scrollInfo + strings.Repeat(" ", max(1, chatWidth-len(scrollInfo)-len(sendHint)-4)) + sendHint,
	)

	return lipgloss.JoinVertical(lipgloss.Left, header, vp, inputBox, footer)
}

// ---------------------------------------------------------------------------
// Message render
// ---------------------------------------------------------------------------

func (m Model) renderMessages(s Session) string {
	if len(s.Messages) == 0 {
		empty := lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true).
			Padding(4, 2).
			Width(m.viewport.Width).
			Align(lipgloss.Center).
			Render("Start a conversation by typing below…")
		return empty
	}

	var b strings.Builder
	for _, msg := range s.Messages {
		b.WriteString(m.renderMessage(msg))
		b.WriteString("\n")
	}
	return b.String()
}

func (m Model) renderMessage(msg Message) string {
	ts := styleMetaBubble.Render(msg.At.Format("15:04:05"))
	maxW := m.viewport.Width
	if maxW < 20 {
		maxW = 80
	}

	switch msg.Role {
	case RoleUser:
		label := styleUserBubble.Render("  You")
		meta := ts
		header := lipgloss.JoinHorizontal(lipgloss.Bottom, label, "  ", meta)
		body := lipgloss.NewStyle().
			Foreground(colorText).
			Width(maxW).
			Render(wordWrap(msg.Content, maxW))
		return header + "\n" + body + "\n"

	case RoleAssistant:
		label := styleAssistantBubble.Render("  Assistant")
		reqID := ""
		if msg.RequestID != "" {
			reqID = "  " + styleReqID.Render("req:"+truncate(msg.RequestID, 13))
		}
		header := lipgloss.JoinHorizontal(lipgloss.Bottom, label, reqID, "  ", ts)
		body := lipgloss.NewStyle().
			Foreground(colorText).
			Width(maxW).
			Render(wordWrap(msg.Content, maxW))
		return header + "\n" + body + "\n"

	case RoleError:
		label := styleErrorBubble.Render("  Error")
		header := lipgloss.JoinHorizontal(lipgloss.Bottom, label, "  ", ts)
		body := styleErrorBubble.Width(maxW).Render(wordWrap(msg.Content, maxW))
		return header + "\n" + body + "\n"

	default:
		return styleMetaBubble.Width(maxW).Render("  "+msg.Content) + "\n"
	}
}

// ---------------------------------------------------------------------------
// Actions
// ---------------------------------------------------------------------------

func (m *Model) newSession() {
	m.sessionCount++
	id := fmt.Sprintf("session-%d", m.sessionCount)
	s := newSession(id, "New Session")
	// System welcome message
	s.Messages = append(s.Messages, Message{
		Role:    RoleSystem,
		Content: fmt.Sprintf("Connected to %s  |  user: %s  |  tier: %s", m.cfg.MasterURL, m.cfg.UserID, m.cfg.Tier),
		At:      time.Now(),
	})
	m.sessions = append(m.sessions, s)
}

func (m *Model) sendPrompt(prompt string) tea.Cmd {
	if m.activeIdx >= len(m.sessions) {
		return nil
	}
	session := &m.sessions[m.activeIdx]
	session.Messages = append(session.Messages, Message{
		Role:    RoleUser,
		Content: prompt,
		At:      time.Now(),
	})
	m.loading = true
	m.input.Reset()
	m.refreshViewport()

	sessionID := session.ID
	cfg := m.cfg

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		resp, err := sendChat(ctx, cfg.MasterURL, cfg.UserID, cfg.Tier, prompt)
		if err != nil {
			return chatErrorMsg{sessionID: sessionID, err: err}
		}
		return chatResponseMsg{sessionID: sessionID, resp: resp}
	}
}

// ---------------------------------------------------------------------------
// Utility
// ---------------------------------------------------------------------------

func (m Model) scrollPercent() int {
	if m.viewport.TotalLineCount() == 0 {
		return 100
	}
	pct := float64(m.viewport.ScrollPercent()) * 100
	return int(math.Round(pct))
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n <= 3 {
		return string(runes[:n])
	}
	return string(runes[:n-1]) + "…"
}

// wordWrap naively wraps s at maxWidth, breaking on spaces where possible.
func wordWrap(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return s
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return s
	}

	var b strings.Builder
	lineLen := 0
	for i, w := range words {
		wLen := utf8.RuneCountInString(w)
		if i == 0 {
			b.WriteString(w)
			lineLen = wLen
			continue
		}
		if lineLen+1+wLen > maxWidth {
			b.WriteString("\n")
			b.WriteString(w)
			lineLen = wLen
		} else {
			b.WriteString(" ")
			b.WriteString(w)
			lineLen += 1 + wLen
		}
	}
	return b.String()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
