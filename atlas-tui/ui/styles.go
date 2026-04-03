package ui

import "github.com/charmbracelet/lipgloss"

// Semantic colors — same on both light and dark backgrounds.
var (
	ColorPrimary   = lipgloss.Color("#7C3AED")
	ColorSuccess   = lipgloss.Color("#10B981")
	ColorWarning   = lipgloss.Color("#F59E0B")
	ColorDanger    = lipgloss.Color("#EF4444")
	ColorHighlight = lipgloss.Color("#8B5CF6")
	ColorShadow    = lipgloss.Color("#2D1062") // deep purple drop-shadow
)

// Adaptive colors — automatically pick light-bg or dark-bg variant.
var (
	// ColorMuted: mid-gray readable on both backgrounds.
	ColorMuted = lipgloss.AdaptiveColor{Light: "#374151", Dark: "#6B7280"}

	// ColorBorder: subtle border, darker on light, lighter on dark.
	ColorBorder = lipgloss.AdaptiveColor{Light: "#9CA3AF", Dark: "#374151"}

	// textNormal: main readable text.
	textNormal = lipgloss.AdaptiveColor{Light: "#111827", Dark: "#D1D5DB"}
)

var (
	TabActive   = lipgloss.NewStyle().Background(lipgloss.Color("#111827")).Foreground(ColorPrimary).Bold(true).PaddingLeft(2).PaddingRight(2)
	TabInactive = lipgloss.NewStyle().Background(lipgloss.Color("#111827")).Foreground(lipgloss.Color("#6B7280")).PaddingLeft(2).PaddingRight(2)
	StatusBar   = lipgloss.NewStyle().Background(lipgloss.Color("#111827")).Foreground(lipgloss.Color("#6B7280")).PaddingLeft(1)

	UserLabel       = lipgloss.NewStyle().Foreground(ColorHighlight).Bold(true)
	AssistantLabel  = lipgloss.NewStyle().Foreground(ColorMuted).Bold(true)
	AssistantBorder = lipgloss.NewStyle().BorderLeft(true).BorderStyle(lipgloss.NormalBorder()).BorderForeground(ColorBorder).PaddingLeft(1)

	FieldSelected = lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
	FieldNormal   = lipgloss.NewStyle().Foreground(textNormal)
	FieldDimmed   = lipgloss.NewStyle().Foreground(ColorMuted)

	CheckboxSelected   = lipgloss.NewStyle().Foreground(ColorPrimary)
	CheckboxUnselected = lipgloss.NewStyle().Foreground(ColorMuted)

	PanelBorder = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(ColorBorder)
)
