package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ralhassan/atlas-tui/client"
	"github.com/ralhassan/atlas-tui/config"
)

type tab int

const (
	tabChat     tab = 0
	tabStatus   tab = 1
	tabSettings tab = 2
)

var tabNames = []string{"chat", "status", "settings"}

type appPhase int

const (
	phaseSplash appPhase = iota
	phaseMain
)

// AppModel is the root BubbleTea model.
type AppModel struct {
	client   *client.Client
	cfg      *config.Config
	width    int
	height   int
	activeTab tab
	phase    appPhase
	splash   SplashModel

	chat     ChatModel
	status   StatusModel
	settings SettingsModel
}

func NewAppModel(c *client.Client, cfg *config.Config) AppModel {
	return AppModel{
		client:   c,
		cfg:      cfg,
		phase:    phaseSplash,
		splash:   NewSplashModel(),
		chat:     NewChatModel(c, cfg),
		status:   NewStatusModel(c),
		settings: NewSettingsModel(c),
	}
}

func (m AppModel) Init() tea.Cmd {
	return tea.Batch(
		m.splash.Init(),
		m.chat.Init(),
		m.status.Init(),
		m.settings.Init(),
	)
}

func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		var splashCmd tea.Cmd
		m.splash, splashCmd = m.splash.Update(msg)
		cmds = append(cmds, splashCmd)

		var chatCmd, statusCmd, settingsCmd tea.Cmd
		m.chat, chatCmd = m.chat.Update(msg)
		m.status, statusCmd = m.status.Update(msg)
		m.settings, settingsCmd = m.settings.Update(msg)
		cmds = append(cmds, chatCmd, statusCmd, settingsCmd)
		return m, tea.Batch(cmds...)

	case splashDoneMsg:
		m.phase = phaseMain
		return m, nil

	case tea.KeyMsg:
		// Global quit always works.
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		// During splash, any key skips it.
		if m.phase == phaseSplash {
			var cmd tea.Cmd
			m.splash, cmd = m.splash.Update(msg)
			return m, cmd
		}

		// During onboarding, only route to chat.
		if m.chat.onboarding {
			var cmd tea.Cmd
			m.chat, cmd = m.chat.Update(msg)
			cmds = append(cmds, cmd)
			return m, tea.Batch(cmds...)
		}

		// Tab switching.
		switch msg.String() {
		case "1":
			m.activeTab = tabChat
			return m, tea.ClearScreen
		case "2":
			if m.activeTab != tabStatus {
				m.activeTab = tabStatus
				var cmd tea.Cmd
				m.status, cmd = m.status.Activate()
				return m, tea.Batch(tea.ClearScreen, cmd)
			}
			return m, nil
		case "3":
			m.activeTab = tabSettings
			return m, tea.ClearScreen
		case "tab":
			next := (m.activeTab + 1) % tab(len(tabNames))
			m.activeTab = next
			if next == tabStatus {
				var cmd tea.Cmd
				m.status, cmd = m.status.Activate()
				return m, tea.Batch(tea.ClearScreen, cmd)
			}
			return m, tea.ClearScreen
		}

		// Delegate to active tab.
		switch m.activeTab {
		case tabChat:
			var cmd tea.Cmd
			m.chat, cmd = m.chat.Update(msg)
			cmds = append(cmds, cmd)
		case tabStatus:
			var cmd tea.Cmd
			m.status, cmd = m.status.Update(msg)
			cmds = append(cmds, cmd)
		case tabSettings:
			var cmd tea.Cmd
			m.settings, cmd = m.settings.Update(msg)
			cmds = append(cmds, cmd)
		}

	// Handle the sseReader msg from the chat open-SSE command.
	case *sseReader:
		var cmd tea.Cmd
		m.chat, cmd = m.chat.handleSSEReaderMsg(msg)
		cmds = append(cmds, cmd)

	default:
		// Route splash ticks to splash during splash phase.
		if m.phase == phaseSplash {
			var cmd tea.Cmd
			m.splash, cmd = m.splash.Update(msg)
			cmds = append(cmds, cmd)
		}

		// Route all other messages to all tab models so timers/spinners work.
		var chatCmd, statusCmd, settingsCmd tea.Cmd
		m.chat, chatCmd = m.chat.Update(msg)
		m.status, statusCmd = m.status.Update(msg)
		m.settings, settingsCmd = m.settings.Update(msg)
		cmds = append(cmds, chatCmd, statusCmd, settingsCmd)
	}

	return m, tea.Batch(cmds...)
}

func (m AppModel) View() string {
	if m.width == 0 {
		return ""
	}

	// Splash phase: full-screen logo animation.
	if m.phase == phaseSplash {
		return m.splash.View()
	}

	// During onboarding, full screen is the chat.
	if m.chat.onboarding {
		return m.chat.View()
	}

	var sb strings.Builder

	// Tab bar (row 0).
	sb.WriteString(m.renderTabBar())
	sb.WriteString("\n")

	// Active tab content rendered into a fixed-size box.
	// Height+MaxHeight enforce exactly (m.height-3) rows regardless of tab content;
	// Width pads every line to m.width so no old characters bleed through.
	contentH := m.height - 3
	if contentH < 1 {
		contentH = 1
	}
	var rawContent string
	switch m.activeTab {
	case tabChat:
		rawContent = m.chat.View()
	case tabStatus:
		rawContent = m.status.View()
	case tabSettings:
		rawContent = m.settings.View()
	}
	contentBox := lipgloss.NewStyle().
		Width(m.width).
		Height(contentH).
		MaxHeight(contentH)
	sb.WriteString(contentBox.Render(rawContent))

	// Status bar (immediately after the fixed content box — no extra blank needed
	// because the box already fills the rows).
	sb.WriteString("\n")
	sb.WriteString(m.renderStatusBar())

	return sb.String()
}

func (m AppModel) renderTabBar() string {
	var parts []string
	for i, name := range tabNames {
		if tab(i) == m.activeTab {
			parts = append(parts, TabActive.Render(name))
		} else {
			parts = append(parts, TabInactive.Render(name))
		}
	}
	// Join tabs then fill remaining width with the same dark background as the footer.
	bar := strings.Join(parts, "")
	return lipgloss.NewStyle().
		Background(lipgloss.Color("#111827")).
		Width(m.width).
		Render(bar)
}

func (m AppModel) renderStatusBar() string {
	var hint string
	switch m.activeTab {
	case tabStatus:
		hint = "  r refresh  |  c clear logs  |  ↑↓ scroll  |  1-3 switch tab  |  ctrl+c quit"
	case tabSettings:
		hint = "  s save  |  r reload  |  ↑↓ navigate  |  enter edit  |  space toggle  |  esc cancel"
	default:
		hint = "  1 chat  |  2 status  |  3 settings  |  tab switch  |  ctrl+c quit"
	}
	return StatusBar.Width(m.width).Render(hint)
}

// lipglossLen approximates the visible width of a lipgloss-rendered string
// by stripping ANSI escape codes.
func lipglossLen(s string) int {
	inEscape := false
	count := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if c == 'm' {
				inEscape = false
			}
			continue
		}
		count++
	}
	return count
}
