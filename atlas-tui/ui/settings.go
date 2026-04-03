package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ralhassan/atlas-tui/client"
)

type settingsLoadedMsg struct {
	cfg *client.RuntimeConfig
	err error
}

type settingsSavedMsg struct {
	err error
}

type settingsClearStatusMsg struct{}

// settingsField describes a single editable config field.
type settingsField struct {
	key     string // JSON key for PUT /config
	label   string
	value   string
	kind    string // "string" | "int" | "bool" | "readonly"
	editing bool
}

type SettingsModel struct {
	client    *client.Client
	width     int
	height    int
	fields    []settingsField
	cursor    int
	textinput textinput.Model
	loading   bool
	saving    bool
	saveMsg   string
	saveErr   bool
	err       string
	cfg       *client.RuntimeConfig
}

func NewSettingsModel(c *client.Client) SettingsModel {
	ti := textinput.New()
	ti.CharLimit = 200

	m := SettingsModel{
		client:    c,
		textinput: ti,
		loading:   true,
	}
	return m
}

func (m SettingsModel) Init() tea.Cmd {
	return m.loadConfig()
}

func (m SettingsModel) loadConfig() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		cfg, err := c.GetConfig()
		return settingsLoadedMsg{cfg: cfg, err: err}
	}
}

func (m *SettingsModel) buildFields(cfg *client.RuntimeConfig) {
	m.cfg = cfg
	m.fields = []settingsField{
		{
			key:   "defaultOpenAIModel",
			label: "model",
			value: cfg.Model,
			kind:  "string",
		},
		{
			key:   "maxAgentIterations",
			label: "max agent iterations",
			value: strconv.Itoa(cfg.MaxAgentIterations),
			kind:  "int",
		},
		{
			key:   "enableMultiAgentOrchestration",
			label: "multi-agent orchestration",
			value: boolStr(cfg.EnableMultiAgentOrchestration),
			kind:  "bool",
		},
		{
			key:   "maxParallelAgents",
			label: "max parallel agents",
			value: strconv.Itoa(cfg.MaxParallelAgents),
			kind:  "int",
		},
		{
			key:   "workerMaxIterations",
			label: "worker max iterations",
			value: strconv.Itoa(cfg.WorkerMaxIterations),
			kind:  "int",
		},
		{
			key:   "runtimePort",
			label: "port",
			value: strconv.Itoa(cfg.Port),
			kind:  "readonly",
		},
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func (m SettingsModel) Update(msg tea.Msg) (SettingsModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case settingsLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err.Error()
		} else {
			m.buildFields(msg.cfg)
		}

	case settingsSavedMsg:
		m.saving = false
		if msg.err != nil {
			m.saveMsg = "error: " + msg.err.Error()
			m.saveErr = true
		} else {
			m.saveMsg = "saved"
			m.saveErr = false
		}
		cmds = append(cmds, tea.Tick(2*time.Second, func(_ time.Time) tea.Msg {
			return settingsClearStatusMsg{}
		}))

	case settingsClearStatusMsg:
		m.saveMsg = ""
		m.saveErr = false

	case tea.KeyMsg:
		// Editing mode
		if m.cursor < len(m.fields) && m.fields[m.cursor].editing {
			switch msg.String() {
			case "enter":
				m.fields[m.cursor].value = m.textinput.Value()
				m.fields[m.cursor].editing = false
				m.textinput.Blur()
			case "esc":
				m.fields[m.cursor].editing = false
				m.textinput.Blur()
			default:
				var tiCmd tea.Cmd
				m.textinput, tiCmd = m.textinput.Update(msg)
				cmds = append(cmds, tiCmd)
			}
			return m, tea.Batch(cmds...)
		}

		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.fields)-1 {
				m.cursor++
			}
		case "enter":
			if m.cursor < len(m.fields) {
				f := &m.fields[m.cursor]
				switch f.kind {
				case "bool":
					if f.value == "true" {
						f.value = "false"
					} else {
						f.value = "true"
					}
				case "readonly":
					// no-op
				default:
					f.editing = true
					m.textinput.SetValue(f.value)
					m.textinput.Focus()
				}
			}
		case " ":
			// Toggle bool with space too
			if m.cursor < len(m.fields) && m.fields[m.cursor].kind == "bool" {
				f := &m.fields[m.cursor]
				if f.value == "true" {
					f.value = "false"
				} else {
					f.value = "true"
				}
			}
		case "s":
			if !m.saving && len(m.fields) > 0 {
				m.saving = true
				cmds = append(cmds, m.saveConfig())
			}
		case "r":
			m.loading = true
			m.err = ""
			cmds = append(cmds, m.loadConfig())
		}
	}

	return m, tea.Batch(cmds...)
}

func (m SettingsModel) saveConfig() tea.Cmd {
	patch := make(map[string]any)
	for _, f := range m.fields {
		if f.kind == "readonly" {
			continue
		}
		switch f.kind {
		case "bool":
			patch[f.key] = f.value == "true"
		case "int":
			n, err := strconv.Atoi(f.value)
			if err == nil {
				patch[f.key] = n
			}
		default:
			patch[f.key] = f.value
		}
	}
	c := m.client
	return func() tea.Msg {
		err := c.UpdateConfig(patch)
		return settingsSavedMsg{err: err}
	}
}

func (m SettingsModel) View() string {
	if m.width == 0 {
		return ""
	}

	var sb strings.Builder

	if m.loading {
		sb.WriteString("  loading settings...\n")
		return sb.String()
	}

	if m.err != "" {
		sb.WriteString(lipgloss.NewStyle().Foreground(ColorDanger).Render("  error: "+m.err) + "\n")
		sb.WriteString(FieldDimmed.Render("  r reload"))
		return sb.String()
	}

	sb.WriteString("  " + FieldSelected.Render("Settings") + "\n\n")

	for i, f := range m.fields {
		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}

		var valueStr string
		if f.editing {
			valueStr = m.textinput.View()
		} else {
			switch f.kind {
			case "readonly":
				valueStr = FieldDimmed.Render(f.value)
			case "bool":
				if f.value == "true" {
					valueStr = CheckboxSelected.Render("[on]")
				} else {
					valueStr = CheckboxUnselected.Render("[off]")
				}
			default:
				if i == m.cursor {
					valueStr = FieldSelected.Render(f.value)
				} else {
					valueStr = FieldNormal.Render(f.value)
				}
			}
		}

		labelStyle := FieldDimmed
		if i == m.cursor {
			labelStyle = FieldSelected
		}

		line := fmt.Sprintf("%s%-28s %s",
			cursor,
			labelStyle.Render(f.label),
			valueStr)
		sb.WriteString(line + "\n")
	}

	sb.WriteString("\n")

	// Save status
	if m.saveMsg != "" {
		style := lipgloss.NewStyle().Foreground(ColorSuccess)
		if m.saveErr {
			style = lipgloss.NewStyle().Foreground(ColorDanger)
		}
		sb.WriteString("  " + style.Render(m.saveMsg) + "\n")
	}

	if m.saving {
		sb.WriteString("  saving...\n")
	}

	// Pad to the full content budget so switching from a taller tab (e.g. status
	// with its log viewport) doesn't leave old rows visible at the bottom.
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
