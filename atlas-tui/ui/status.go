package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ralhassan/atlas-tui/client"
)

const statusPollInterval = 3 * time.Second

type statusTickMsg time.Time
type statusDataMsg struct {
	status *client.StatusResponse
	logs   []string
	err    error
}

type StatusModel struct {
	client   *client.Client
	width    int
	height   int
	viewport viewport.Model
	spinner  spinner.Model

	status   *client.StatusResponse
	logs     []string
	loading  bool
	fetching bool // true while a fetchData Cmd is in-flight
	active   bool // true when the status tab is visible
	err      string
}

func NewStatusModel(c *client.Client) StatusModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(ColorPrimary)

	vp := viewport.New(80, 20)

	return StatusModel{
		client:  c,
		spinner: sp,
		viewport: vp,
		loading: true,
	}
}

func (m StatusModel) Init() tea.Cmd {
	// Don't fetch or start polling until the tab is activated.
	return m.spinner.Tick
}

// Activate marks the tab as visible and kicks off the initial fetch + poll.
// Call this from app.go whenever the user switches to the status tab.
func (m StatusModel) Activate() (StatusModel, tea.Cmd) {
	m.active = true
	m.loading = true
	m.fetching = true
	m.err = ""
	return m, tea.Batch(m.fetchData(), m.schedulePoll())
}

func (m StatusModel) schedulePoll() tea.Cmd {
	return tea.Tick(statusPollInterval, func(t time.Time) tea.Msg {
		return statusTickMsg(t)
	})
}

func (m StatusModel) fetchData() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		status, err := c.GetStatus()
		if err != nil {
			return statusDataMsg{err: err}
		}
		logs, _ := c.GetLogs(100)
		return statusDataMsg{status: status, logs: logs}
	}
}

// setFetching returns a Cmd that sets m.fetching=true before dispatching fetchData.
// (Used by manual refresh where the caller doesn't go through statusTickMsg.)
func (m *StatusModel) startFetch() tea.Cmd {
	m.fetching = true
	return m.fetchData()
}

func (m StatusModel) Update(msg tea.Msg) (StatusModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case statusTickMsg:
		if m.active && !m.fetching {
			m.fetching = true
			cmds = append(cmds, m.fetchData(), m.schedulePoll())
		} else if m.active {
			cmds = append(cmds, m.schedulePoll())
		}

	case statusDataMsg:
		m.fetching = false
		m.loading = false
		if msg.err != nil {
			m.err = msg.err.Error()
		} else {
			m.err = ""
			m.status = msg.status
			m.logs = msg.logs
			m.refreshViewport()
		}

	case tea.KeyMsg:
		switch msg.String() {
		case "r":
			m.loading = true
			m.err = ""
			cmds = append(cmds, m.startFetch())
		case "c":
			m.logs = nil
			m.refreshViewport()
		default:
			var vpCmd tea.Cmd
			m.viewport, vpCmd = m.viewport.Update(msg)
			cmds = append(cmds, vpCmd)
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *StatusModel) resize() {
	// frame budget: m.height - 3 rows for content (app.go handles tab bar + blank + footer)
	// status view overhead: status panel (~8 rows) + "logs" label (1) + key hint in footer (0, moved to app statusbar)
	statusPanelH := 8
	logsLabelH := 1
	vpH := (m.height - 3) - statusPanelH - logsLabelH
	if vpH < 4 {
		vpH = 4
	}
	m.viewport.Width = m.width
	m.viewport.Height = vpH
	m.refreshViewport()
}

func (m *StatusModel) refreshViewport() {
	if len(m.logs) == 0 {
		m.viewport.SetContent(FieldDimmed.Render("no logs"))
		return
	}
	var sb strings.Builder
	for _, l := range m.logs {
		sb.WriteString(l)
		sb.WriteString("\n")
	}
	m.viewport.SetContent(sb.String())
	m.viewport.GotoBottom()
}

func stateColor(state string) lipgloss.Style {
	switch strings.ToLower(state) {
	case "idle", "ready":
		return lipgloss.NewStyle().Foreground(ColorSuccess).Bold(true)
	case "running", "thinking", "busy":
		return lipgloss.NewStyle().Foreground(ColorWarning).Bold(true)
	case "error":
		return lipgloss.NewStyle().Foreground(ColorDanger).Bold(true)
	default:
		return lipgloss.NewStyle().Foreground(ColorMuted).Bold(true)
	}
}

func (m StatusModel) View() string {
	if m.width == 0 {
		return ""
	}

	var sb strings.Builder

	// Status panel
	if m.loading {
		sb.WriteString(m.spinner.View() + " loading...\n\n")
	} else if m.err != "" {
		sb.WriteString(lipgloss.NewStyle().Foreground(ColorDanger).Render("error: "+m.err) + "\n\n")
	} else if m.status != nil {
		s := m.status
		stateStr := stateColor(s.State).Render(s.State)
		if s.State == "" {
			stateStr = FieldDimmed.Render("unknown")
		}

		row := func(label, value string) string {
			return fmt.Sprintf("  %-18s %s\n",
				FieldDimmed.Render(label+":"),
				FieldNormal.Render(value))
		}

		panel := "  " + FieldSelected.Render("Atlas Runtime") + "\n\n"
		panel += row("state", stateStr)
		if s.CurrentTask != "" {
			panel += row("task", s.CurrentTask)
		}
		panel += row("model", s.Model)
		panel += row("uptime", s.Uptime)
		if s.Version != "" {
			panel += row("version", s.Version)
		}

		sb.WriteString(PanelBorder.Width(m.width - 4).Render(panel))
		sb.WriteString("\n\n")
	}

	// Logs section
	sb.WriteString(FieldDimmed.Render("  logs") + "\n")
	sb.WriteString(m.viewport.View())

	// Pad to the full content budget so switching tabs never leaves stale rows.
	result := sb.String()
	budget := m.height - 3
	if budget > 0 {
		written := strings.Count(result, "\n")
		if written < budget {
			result += strings.Repeat("\n", budget-written)
		}
	}
	return result
}
