package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Art definitions ───────────────────────────────────────────────────────────

// splashWelcomeLines — "WELCOME" wordmark shown in phase 1.
var splashWelcomeLines = []string{
	`        ██╗    ██╗███████╗██╗      ██████╗ ██████╗ ███╗   ███╗███████╗`,
	`        ██║    ██║██╔════╝██║     ██╔════╝██╔═══██╗████╗ ████║██╔════╝`,
	`        ██║ █╗ ██║█████╗  ██║     ██║     ██║   ██║██╔████╔██║█████╗  `,
	`        ██║███╗██║██╔══╝  ██║     ██║     ██║   ██║██║╚██╔╝██║██╔══╝  `,
	`        ╚███╔███╔╝███████╗███████╗╚██████╗╚██████╔╝██║ ╚═╝ ██║███████╗`,
	`         ╚══╝╚══╝ ╚══════╝╚══════╝ ╚═════╝ ╚═════╝ ╚═╝     ╚═╝╚══════╝`,
}

// splashAtlasLines — "TO ATLAS" wordmark shown in phase 2 (appended below WELCOME).
var splashAtlasLines = []string{
	``,
	`                    ████████╗ ██████╗     █████╗ ████████╗██╗      █████╗ ███████╗`,
	`                    ╚══██╔══╝██╔═══██╗   ██╔══██╗╚══██╔══╝██║     ██╔══██╗██╔════╝`,
	`                       ██║   ██║   ██║   ███████║   ██║   ██║     ███████║███████╗`,
	`                       ██║   ██║   ██║   ██╔══██║   ██║   ██║     ██╔══██║╚════██║`,
	`                       ██║   ╚██████╔╝   ██║  ██║   ██║   ███████╗██║  ██║███████║`,
	`                       ╚═╝    ╚═════╝    ╚═╝  ╚═╝   ╚═╝   ╚══════╝╚═╝  ╚═╝╚══════╝`,
}

// atlasArtLines — compact ATLAS wordmark used as the pinned onboarding header.
var atlasArtLines = []string{
	`████████╗ ██████╗     █████╗ ████████╗██╗      █████╗ ███████╗`,
	`╚══██╔══╝██╔═══██╗   ██╔══██╗╚══██╔══╝██║     ██╔══██╗██╔════╝`,
	`   ██║   ██║   ██║   ███████║   ██║   ██║     ███████║███████╗`,
	`   ██║   ██║   ██║   ██╔══██║   ██║   ██║     ██╔══██║╚════██║`,
	`   ██║   ╚██████╔╝   ██║  ██║   ██║   ███████╗██║  ██║███████║`,
	`   ╚═╝    ╚═════╝    ╚═╝  ╚═╝   ╚═╝   ╚══════╝╚═╝  ╚═╝╚══════╝`,
}

const atlasSubtitle = "Welcome to"

// atlasBlockHeight is the height of the onboarding banner block:
// art lines + shadow tail + blank separator + subtitle.
func atlasBlockHeight() int { return len(atlasArtLines) + 3 }

// maxRuneWidth returns the longest line width (in runes) across a set of lines.
func maxRuneWidth(lines []string) int {
	w := 0
	for _, l := range lines {
		if n := len([]rune(l)); n > w {
			w = n
		}
	}
	return w
}

// ── renderAtlasBanner — pinned onboarding header ──────────────────────────────

func renderAtlasBanner(width int) string {
	artStyle    := lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true).Italic(true)
	shadowStyle := lipgloss.NewStyle().Foreground(ColorShadow)
	subStyle    := lipgloss.NewStyle().Foreground(ColorMuted)

	artLen := len(atlasArtLines)
	artW   := maxRuneWidth(atlasArtLines)

	center := func(lineWidth int) string {
		pad := (width - lineWidth) / 2
		if pad < 0 {
			pad = 0
		}
		return strings.Repeat(" ", pad)
	}

	var sb strings.Builder
	for row := 0; row < artLen; row++ {
		sb.WriteString(center(artW + 2))
		if row == 0 {
			sb.WriteString(artStyle.Render(atlasArtLines[row]))
		} else {
			sb.WriteString(compositeRow(atlasArtLines[row], atlasArtLines[row-1]))
		}
		sb.WriteString("\n")
	}

	// Shadow tail.
	sb.WriteString(center(artW + 2))
	sb.WriteString("  ")
	sb.WriteString(shadowStyle.Render(atlasArtLines[artLen-1]))
	sb.WriteString("\n")

	// Blank separator.
	sb.WriteString("\n")

	// Subtitle.
	sb.WriteString(center(len(atlasSubtitle)))
	sb.WriteString(subStyle.Render(atlasSubtitle))
	sb.WriteString("\n")

	return sb.String()
}

// ── compositeRow (shadow compositor) ─────────────────────────────────────────

func compositeRow(artLine, shadowLine string) string {
	if artLine == "" {
		return ""
	}
	artStyle    := lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true).Italic(true)
	shadowStyle := lipgloss.NewStyle().Foreground(ColorShadow)

	artRunes    := []rune(artLine)
	shadowRunes := []rune(shadowLine)
	shadowShift := 2

	maxLen := len(artRunes)
	if len(shadowRunes)+shadowShift > maxLen {
		maxLen = len(shadowRunes) + shadowShift
	}

	type run struct {
		text     string
		isArt    bool
		isShadow bool
	}

	var runs []run
	var cur []rune
	curIsArt, curIsShadow := false, false

	flush := func() {
		if len(cur) == 0 {
			return
		}
		runs = append(runs, run{string(cur), curIsArt, curIsShadow})
		cur = nil
		curIsArt, curIsShadow = false, false
	}

	for col := 0; col < maxLen; col++ {
		ac := rune(' ')
		if col < len(artRunes) {
			ac = artRunes[col]
		}
		sc := rune(' ')
		if si := col - shadowShift; si >= 0 && si < len(shadowRunes) {
			sc = shadowRunes[si]
		}

		var ch rune
		var ia, is bool
		if ac != ' ' {
			ch, ia = ac, true
		} else if sc != ' ' {
			ch, is = sc, true
		} else {
			ch = ' '
		}

		if ia == curIsArt && is == curIsShadow {
			cur = append(cur, ch)
		} else {
			flush()
			cur = append(cur, ch)
			curIsArt, curIsShadow = ia, is
		}
	}
	flush()

	var sb strings.Builder
	for _, r := range runs {
		switch {
		case r.isArt:
			sb.WriteString(artStyle.Render(r.text))
		case r.isShadow:
			sb.WriteString(shadowStyle.Render(r.text))
		default:
			sb.WriteString(r.text)
		}
	}
	return sb.String()
}

// ── Splash model ──────────────────────────────────────────────────────────────

type splashPhase int

const (
	splashPhaseBlank   splashPhase = iota // initial blank hold
	splashPhaseWelcome                    // WELCOME visible
	splashPhaseFull                       // WELCOME + TO ATLAS visible
	splashPhaseSlide                      // sliding up to top
)

type (
	splashShowWelcomeMsg struct{}
	splashShowAtlasMsg   struct{}
	splashTickMsg        struct{}
	splashDoneMsg        struct{}
)

// SplashModel animates the WELCOME → TO ATLAS reveal, then slides the art to
// the top of the screen before handing control to the main UI.
type SplashModel struct {
	width   int
	height  int
	phase   splashPhase
	offsetY int // top-padding rows for slide animation
}

func NewSplashModel() SplashModel {
	return SplashModel{}
}

func (m SplashModel) Init() tea.Cmd {
	// Short blank hold, then reveal WELCOME.
	return tea.Tick(250*time.Millisecond, func(time.Time) tea.Msg {
		return splashShowWelcomeMsg{}
	})
}

func (m SplashModel) Update(msg tea.Msg) (SplashModel, tea.Cmd) {
	switch msg.(type) {
	case tea.WindowSizeMsg:
		wm := msg.(tea.WindowSizeMsg)
		if wm.Width == 0 || wm.Height == 0 {
			return m, nil
		}
		m.width = wm.Width
		m.height = wm.Height

	case splashShowWelcomeMsg:
		m.phase = splashPhaseWelcome
		m.offsetY = (m.height - len(splashWelcomeLines)) / 2
		if m.offsetY < 0 {
			m.offsetY = 0
		}
		return m, tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
			return splashShowAtlasMsg{}
		})

	case splashShowAtlasMsg:
		m.phase = splashPhaseFull
		// WELCOME (6) + blank separator (1) + ATLAS (6) = 13 rows total.
		combined := len(splashWelcomeLines) + 1 + len(atlasArtLines)
		m.offsetY = (m.height - combined) / 2
		if m.offsetY < 0 {
			m.offsetY = 0
		}
		return m, tea.Tick(700*time.Millisecond, func(time.Time) tea.Msg {
			return splashTickMsg{}
		})

	case splashTickMsg:
		m.phase = splashPhaseSlide
		m.offsetY -= 3
		if m.offsetY <= 0 {
			m.offsetY = 0
			return m, func() tea.Msg { return splashDoneMsg{} }
		}
		return m, tea.Tick(35*time.Millisecond, func(time.Time) tea.Msg {
			return splashTickMsg{}
		})

	case tea.KeyMsg:
		return m, func() tea.Msg { return splashDoneMsg{} }
	}

	return m, nil
}

func (m SplashModel) View() string {
	if m.width == 0 || m.phase == splashPhaseBlank {
		return strings.Repeat("\n", m.height)
	}

	artStyle := lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true).Italic(true)

	// Each block is centered independently so their own widths determine padding,
	// eliminating misalignment caused by the shared-padStr approach.
	welcomeW := maxRuneWidth(splashWelcomeLines)
	welcomePad := (m.width - welcomeW) / 2
	if welcomePad < 0 {
		welcomePad = 0
	}

	atlasW := maxRuneWidth(atlasArtLines)
	atlasPad := (m.width - atlasW) / 2
	if atlasPad < 0 {
		atlasPad = 0
	}

	nW := len(splashWelcomeLines) // 6
	nA := len(atlasArtLines)      // 6

	renderLine := func(sb *strings.Builder, line string, pad int) {
		if line != "" {
			sb.WriteString(strings.Repeat(" ", pad))
			sb.WriteString(artStyle.Render(line))
		}
	}

	var sb strings.Builder
	for row := 0; row < m.height; row++ {
		idx := row - m.offsetY

		switch m.phase {
		case splashPhaseWelcome:
			if idx >= 0 && idx < nW {
				renderLine(&sb, splashWelcomeLines[idx], welcomePad)
			}
		default: // splashPhaseFull, splashPhaseSlide
			switch {
			case idx >= 0 && idx < nW:
				// WELCOME block
				renderLine(&sb, splashWelcomeLines[idx], welcomePad)
			case idx == nW:
				// blank separator row between WELCOME and ATLAS
			default:
				// ATLAS block — use atlasArtLines (no baked-in indentation)
				aIdx := idx - nW - 1
				if aIdx >= 0 && aIdx < nA {
					renderLine(&sb, atlasArtLines[aIdx], atlasPad)
				}
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
