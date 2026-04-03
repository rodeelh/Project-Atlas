package ui

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ralhassan/atlas-tui/client"
	"github.com/ralhassan/atlas-tui/config"
	"github.com/ralhassan/atlas-tui/onboarding"
)

// ── SSE streaming types ───────────────────────────────────────────────────────

type sseReader struct {
	scanner *bufio.Scanner
	resp    *http.Response
	cancel  context.CancelFunc
	convID  string // confirmed convID from the server
}

type sseNextMsg struct {
	event client.SSEEvent
	done  bool
	err   error
}

func readNextSSEEvent(r *sseReader) tea.Cmd {
	return func() tea.Msg {
		for r.scanner.Scan() {
			line := r.scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			var event client.SSEEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}
			return sseNextMsg{event: event, done: event.Type == "done" || event.Type == "error"}
		}
		if err := r.scanner.Err(); err != nil {
			return sseNextMsg{err: err, done: true}
		}
		return sseNextMsg{done: true}
	}
}

// ── Chat message types ────────────────────────────────────────────────────────

type chatMsg struct {
	role      string // "user" | "assistant"
	content   string
	timestamp time.Time
}

// ── Tea message types ─────────────────────────────────────────────────────────

type historyLoadedMsg struct {
	messages []client.Message
	err      error
}

type writeFilesResultMsg struct{ err error }

type onboardingDoneMsg struct{}
type wakeUpMsg struct{}
type daemonCheckResultMsg struct{ err error }
type saveCredsResultMsg struct{ err error }

// typewriterTickMsg fires on each character reveal tick.
type typewriterTickMsg struct{}

// twPauseMsg fires after a message finishes typing; starts the next queued message.
type twPauseMsg struct{}

func twTickCmd() tea.Cmd {
	return tea.Tick(35*time.Millisecond, func(time.Time) tea.Msg {
		return typewriterTickMsg{}
	})
}

// chatTypeTickMsg drives the chat-response typewriter (one char per tick).
type chatTypeTickMsg struct{}

func chatTypeTickCmd() tea.Cmd {
	return tea.Tick(30*time.Millisecond, func(time.Time) tea.Msg {
		return chatTypeTickMsg{}
	})
}

// ── ChatModel ─────────────────────────────────────────────────────────────────

type ChatModel struct {
	client  *client.Client
	cfg     *config.Config
	width   int
	height  int
	vp      viewport.Model
	input   textinput.Model // main prompt / onboarding text
	masked  textinput.Model // EchoPassword mode for API keys
	spinner spinner.Model

	messages    []chatMsg
	streaming  bool
	statusLine string // shown below viewport during tool use (e.g. "Browsing...")
	convID     string
	sseStream  *sseReader

	// Typewriter (onboarding agent messages).
	twQueue   []string // pending messages to type
	twPending string   // next message waiting for pause to end
	twText    string   // current message being typed (full)
	twRunes   []rune   // rune slice of twText
	twPos     int      // chars revealed so far
	twActive  bool     // typewriter is currently running

	// Chat streaming typewriter — reveals buffered SSE content char by char.
	chatBuf    string // SSE content received but not yet revealed
	chatDone   bool   // SSE stream has ended (drain chatBuf then stop)
	chatTyping bool   // tick loop is active

	// Onboarding.
	onboarding    bool
	maskedMode    bool // true while collecting an API key
	obState       onboarding.State
	obScreenStart int // index in messages where the current onboarding screen starts
	checkboxCursor int

	err string
}

func NewChatModel(c *client.Client, cfg *config.Config) ChatModel {
	in := textinput.New()
	in.Prompt = "> "
	in.Placeholder = ""
	in.CharLimit = 4000
	in.Focus()

	msk := textinput.New()
	msk.Prompt = "> "
	msk.Placeholder = "paste key here..."
	msk.EchoMode = textinput.EchoPassword
	msk.EchoCharacter = '•'
	msk.CharLimit = 512

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(ColorMuted)

	vp := viewport.New(80, 20)

	m := ChatModel{
		client:  c,
		cfg:     cfg,
		vp:      vp,
		input:   in,
		masked:  msk,
		spinner: sp,
	}

	if !cfg.OnboardingDone {
		m.onboarding = true
		m.obState = onboarding.State{Step: onboarding.StepWakeUp}
	}

	return m
}

func (m ChatModel) Init() tea.Cmd {
	cmds := []tea.Cmd{textinput.Blink, m.spinner.Tick}
	if !m.cfg.OnboardingDone {
		// Short pause, then start the wake-up typewriter sequence.
		cmds = append(cmds, tea.Tick(600*time.Millisecond, func(time.Time) tea.Msg {
			return wakeUpMsg{}
		}))
	} else {
		cmds = append(cmds, m.loadHistory())
	}
	return tea.Batch(cmds...)
}

func (m ChatModel) loadHistory() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		msgs, err := c.GetHistory(50)
		return historyLoadedMsg{messages: msgs, err: err}
	}
}

func (m ChatModel) Update(msg tea.Msg) (ChatModel, tea.Cmd) {
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

	// ── Typewriter ─────────────────────────────────────────────────────────────
	case typewriterTickMsg:
		if m.twActive {
			m.twPos++
			if m.twPos > len(m.twRunes) {
				m.twPos = len(m.twRunes)
			}
			// Update the live message in the history.
			if len(m.messages) > 0 {
				m.messages[len(m.messages)-1].content = string(m.twRunes[:m.twPos])
			}
			if m.twPos >= len(m.twRunes) {
				// Done typing this message.
				m.twActive = false
				if len(m.twQueue) > 0 {
					// Pull next from queue; pause 3 s so the user can read before the next line.
					m.twPending = m.twQueue[0]
					m.twQueue = m.twQueue[1:]
					cmds = append(cmds, tea.Tick(3*time.Second, func(time.Time) tea.Msg {
						return twPauseMsg{}
					}))
				}
			} else {
				cmds = append(cmds, twTickCmd())
			}
			m.refresh()
		}

	case twPauseMsg:
		if m.twPending != "" {
			// Each queued message gets a clean screen — only one line visible at a time.
			if m.onboarding {
				m.clearScreen()
			}
			m.startTypewriter(m.twPending)
			m.twPending = ""
			cmds = append(cmds, twTickCmd())
			m.refresh()
		}

	// ── Wake-up sequence ───────────────────────────────────────────────────────
	case wakeUpMsg:
		// Queue all three opening lines; typewriter handles the pacing.
		m.addAtlas(onboarding.WakeUpLine(int(time.Now().Weekday())))
		m.addAtlas("oh. who are you?")
		m.addAtlas("what do I call you?")
		m.obState.Step = onboarding.StepAskName
		if m.twActive {
			cmds = append(cmds, twTickCmd())
		}
		m.refresh()

	// ── History ────────────────────────────────────────────────────────────────
	case historyLoadedMsg:
		if msg.err != nil {
			m.messages = append(m.messages, chatMsg{
				role:    "assistant",
				content: "⚠ could not load history: " + msg.err.Error(),
			})
			m.refresh()
			break
		}
		for _, hm := range msg.messages {
			m.messages = append(m.messages, chatMsg{
				role:      hm.Role,
				content:   hm.Content,
				timestamp: hm.Timestamp,
			})
		}
		m.refresh()

	// ── Streaming ─────────────────────────────────────────────────────────────
	case writeFilesResultMsg:
		if msg.err != nil {
			m.addAtlas("warning: could not write atlas files — " + msg.err.Error())
			if m.twActive {
				cmds = append(cmds, twTickCmd())
			}
			m.refresh()
		}

	case saveCredsResultMsg:
		if msg.err != nil {
			m.addAtlas("warning: could not save credentials — " + msg.err.Error())
			if m.twActive {
				cmds = append(cmds, twTickCmd())
			}
			m.refresh()
		}

	case daemonCheckResultMsg:
		if msg.err != nil {
			m.addAtlas(fmt.Sprintf(
				"I can't find the daemon at %s.\nmake sure Atlas is running:\n\n   make daemon-start\n\npress enter to try again.",
				m.client.BaseURL(),
			))
		} else {
			m.addAtlas(fmt.Sprintf("daemon found. I'm ready. let's go, %s.", m.obState.UserName))
			cmds = append(cmds, func() tea.Msg { return onboardingDoneMsg{} })
		}
		if m.twActive {
			cmds = append(cmds, twTickCmd())
		}
		m.refresh()

	case sseNextMsg:
		if msg.err != nil {
			m.streaming = false
			m.statusLine = ""
			m.err = msg.err.Error()
			m.closeSSE()
			// Mark stream done so the typewriter drains the buffer then stops.
			m.chatDone = true
		} else if msg.done {
			m.streaming = false
			m.statusLine = ""
			m.closeSSE()
			// Mark stream done; if the buffer still has content the tick drains it.
			m.chatDone = true
		} else {
			event := msg.event
			switch event.Type {
			case "token":
				// Buffer incoming content; the chatTypeTickMsg loop reveals it.
				// Spurious empty-content events (stream-open sentinel) are ignored.
				if event.Content != "" && m.streaming {
					m.statusLine = ""
					wasEmpty := m.chatBuf == ""
					m.chatBuf += event.Content
					// Kick the tick loop if it was idle (buffer was empty).
					if wasEmpty && !m.chatTyping {
						m.chatTyping = true
						cmds = append(cmds, chatTypeTickCmd())
					}
				}
			case "tool_call":
				m.statusLine = toolStatusLine(event.ToolName)
				m.refresh()
			case "tool_result":
				m.statusLine = ""
				m.refresh()
			}
			// Read the next SSE event immediately — pacing is handled by chatTypeTickCmd.
			if m.sseStream != nil {
				cmds = append(cmds, readNextSSEEvent(m.sseStream))
			}
		}

	case chatTypeTickMsg:
		if len(m.chatBuf) > 0 {
			// Reveal one rune per tick.
			runes := []rune(m.chatBuf)
			ch := string(runes[0])
			m.chatBuf = string(runes[1:])
			m.appendToken(ch)
			m.refresh()
			m.vp.GotoBottom()
			// Keep ticking as long as there is content or the stream is still open.
			if len(m.chatBuf) > 0 || !m.chatDone {
				cmds = append(cmds, chatTypeTickCmd())
			} else {
				// Buffer drained and stream is done.
				m.chatTyping = false
				m.chatDone = false
			}
		} else if !m.chatDone {
			// Buffer temporarily empty but stream still open; keep polling.
			cmds = append(cmds, chatTypeTickCmd())
		} else {
			// Truly done.
			m.chatTyping = false
			m.chatDone = false
		}

	case onboardingDoneMsg:
		m.onboarding = false
		m.cfg.OnboardingDone = true
		m.input.Placeholder = "ask atlas anything..."
		m.refresh()
		cfg := m.cfg
		cmds = append(cmds, func() tea.Msg {
			_ = config.Save(cfg)
			return nil
		})

	case tea.KeyMsg:
		if m.onboarding {
			return m.updateOnboarding(msg, &cmds)
		}
		return m.updateChat(msg, &cmds)
	}

	// Non-KeyMsg: keep active input blinking.
	if m.maskedMode {
		var c tea.Cmd
		m.masked, c = m.masked.Update(msg)
		cmds = append(cmds, c)
	} else {
		var c tea.Cmd
		m.input, c = m.input.Update(msg)
		cmds = append(cmds, c)
	}

	return m, tea.Batch(cmds...)
}

// handleSSEReaderMsg is called by AppModel when it receives the *sseReader
// returned by the send+openSSE command. The message has already been sent at
// this point — just store the stream and start reading.
func (m ChatModel) handleSSEReaderMsg(reader *sseReader) (ChatModel, tea.Cmd) {
	if reader.convID != "" {
		m.convID = reader.convID
	}
	m.sseStream = reader
	return m, readNextSSEEvent(reader)
}

// ── Normal chat ───────────────────────────────────────────────────────────────

func (m *ChatModel) updateChat(msg tea.KeyMsg, cmds *[]tea.Cmd) (ChatModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		text := strings.TrimSpace(m.input.Value())
		if text == "" || m.streaming || m.chatTyping {
			break
		}
		m.input.Reset()
		m.input.SetValue("")
		m.err = ""
		m.chatBuf = ""
		m.chatDone = false
		m.chatTyping = false

		m.messages = append(m.messages, chatMsg{
			role:      "user",
			content:   text,
			timestamp: time.Now(),
		})
		m.refresh()
		m.vp.GotoBottom()

		if m.convID == "" {
			m.convID = newUUID()
		}

		m.streaming = true
		convID := m.convID
		c := m.client

		sendAndOpenSSE := func() tea.Msg {
			// Step 1: Send the message (POST).
			retConvID, _, err := c.SendMessage(text, convID)
			if err != nil {
				// If conversation not found, retry with a fresh ID.
				if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "not found") {
					retConvID, _, err = c.SendMessage(text, "")
				}
				if err != nil {
					return sseNextMsg{err: err, done: true}
				}
			}
			// Step 2: Open SSE stream.
			ctx, cancel := context.WithCancel(context.Background())
			resp, scanner, err := c.OpenSSEStream(ctx, retConvID)
			if err != nil {
				cancel()
				return sseNextMsg{err: err, done: true}
			}
			return &sseReader{scanner: scanner, resp: resp, cancel: cancel, convID: retConvID}
		}
		*cmds = append(*cmds, sendAndOpenSSE)
		return *m, tea.Batch(*cmds...)

	case "up", "pgup":
		var vpCmd tea.Cmd
		m.vp, vpCmd = m.vp.Update(msg)
		*cmds = append(*cmds, vpCmd)
		return *m, tea.Batch(*cmds...)

	case "down", "pgdown":
		var vpCmd tea.Cmd
		m.vp, vpCmd = m.vp.Update(msg)
		*cmds = append(*cmds, vpCmd)
		return *m, tea.Batch(*cmds...)
	}

	var c tea.Cmd
	m.input, c = m.input.Update(msg)
	*cmds = append(*cmds, c)
	return *m, tea.Batch(*cmds...)
}

// ── Onboarding ────────────────────────────────────────────────────────────────

func (m *ChatModel) updateOnboarding(msg tea.KeyMsg, cmds *[]tea.Cmd) (ChatModel, tea.Cmd) {
	ob := &m.obState

	// Block all input while typewriter is running or pausing.
	if m.twActive || m.twPending != "" {
		return *m, tea.Batch(*cmds...)
	}

	// Masked API key collection.
	if m.maskedMode {
		switch msg.String() {
		case "enter":
			val := strings.TrimSpace(m.masked.Value())
			m.masked.Reset()
			m.maskedMode = false
			m.masked.Blur()
			m.input.Focus()
			m.processKeyInput(val, cmds)
		case "esc":
			m.masked.Reset()
			m.maskedMode = false
			m.masked.Blur()
			m.input.Focus()
			m.advanceKeyCollection(cmds)
		default:
			var c tea.Cmd
			m.masked, c = m.masked.Update(msg)
			*cmds = append(*cmds, c)
		}
		return *m, tea.Batch(*cmds...)
	}

	// Checkbox selection.
	isCheckboxStep := ob.Step == onboarding.StepSelectAI ||
		ob.Step == onboarding.StepSelectChat ||
		ob.Step == onboarding.StepSelectSkills ||
		ob.Step == onboarding.StepSelectPermissions

	if isCheckboxStep {
		switch msg.String() {
		case "up", "k":
			if m.checkboxCursor > 0 {
				m.checkboxCursor--
			}
		case "down", "j":
			if m.checkboxCursor < len(ob.CheckboxItems)-1 {
				m.checkboxCursor++
			}
		case " ":
			if m.checkboxCursor < len(ob.CheckboxItems) {
				ob.CheckboxItems[m.checkboxCursor].Selected = !ob.CheckboxItems[m.checkboxCursor].Selected
			}
		case "enter":
			m.advanceFromCheckboxStep(cmds)
		}
		m.refresh()
		return *m, tea.Batch(*cmds...)
	}

	// Text answer steps — enter submits.
	if msg.String() == "enter" {
		text := strings.TrimSpace(m.input.Value())
		m.input.Reset()
		m.processOnboardingInput(text, cmds)
		return *m, tea.Batch(*cmds...)
	}

	var c tea.Cmd
	m.input, c = m.input.Update(msg)
	*cmds = append(*cmds, c)
	return *m, tea.Batch(*cmds...)
}

func (m *ChatModel) processOnboardingInput(text string, cmds *[]tea.Cmd) {
	ob := &m.obState
	switch ob.Step {
	case onboarding.StepWakeUp:
		// Driven by timer — ignore stray enter presses.

	case onboarding.StepAskName:
		if text == "" {
			text = "friend"
		}
		ob.UserName = text
		m.clearScreen()
		m.addAtlas("good name to know, " + text + ".")
		m.addAtlas("and what do you want to call me? (enter keeps 'Atlas')")
		ob.Step = onboarding.StepAskAgentName

	case onboarding.StepAskAgentName:
		if text == "" {
			text = "Atlas"
		}
		ob.AgentName = text
		m.clearScreen()
		m.addAtlas(text + ". I like that.")
		m.addAtlas("so " + ob.UserName + " — what should I know about you?")
		ob.Step = onboarding.StepAskAboutUser

	case onboarding.StepAskAboutUser:
		ob.AboutUser = text
		m.clearScreen()
		m.addAtlas(onboarding.ReactToAboutUser(text))
		m.addAtlas("and what do you actually want me to do for you?")
		ob.Step = onboarding.StepAskGoals

	case onboarding.StepAskGoals:
		ob.UserGoals = text
		m.clearScreen()
		m.addAtlas(onboarding.ReactToGoals(text))
		m.addAtlas("last one — where are you in the world?")
		ob.Step = onboarding.StepAskLocation

	case onboarding.StepAskLocation:
		ob.UserLocation = text
		m.clearScreen()
		m.addAtlas(onboarding.ReactToLocation(text))
		m.addAtlas("now let's get me connected. pick the AI providers you want to use —\nat least one to get started.")
		ob.Step = onboarding.StepSelectAI
		ob.CheckboxItems = onboarding.AICheckboxItems()
		m.checkboxCursor = 0

	case onboarding.StepDaemonCheck:
		c := m.client
		m.clearScreen()
		m.addAtlas("trying again...")
		*cmds = append(*cmds, func() tea.Msg {
			return daemonCheckResultMsg{err: c.HealthCheck()}
		})
	}

	if m.twActive {
		*cmds = append(*cmds, twTickCmd())
	}
	m.refresh()
}

func (m *ChatModel) advanceFromCheckboxStep(cmds *[]tea.Cmd) {
	ob := &m.obState
	selected := onboarding.SelectedIDs(ob.CheckboxItems)
	labels := onboarding.SelectedLabels(ob.CheckboxItems)

	switch ob.Step {
	case onboarding.StepSelectAI:
		if len(selected) == 0 {
			m.addAtlas("you need at least one AI provider.")
			if m.twActive {
				*cmds = append(*cmds, twTickCmd())
			}
			m.refresh()
			return
		}
		ob.SelectedAI = selected
		m.clearScreen()
		m.addAtlas("good choices. — " + labels)
		ob.Step = onboarding.StepCollectAIKeys
		ob.CurrentKeyIndex = 0
		m.promptForNextAIKey(cmds)
		return

	case onboarding.StepSelectChat:
		if len(selected) == 0 {
			m.addAtlas("you need at least one chat platform.")
			if m.twActive {
				*cmds = append(*cmds, twTickCmd())
			}
			m.refresh()
			return
		}
		ob.SelectedChat = selected
		m.clearScreen()
		m.addAtlas("got it. — " + labels)
		ob.Step = onboarding.StepCollectChatKeys
		ob.CurrentKeyIndex = 0
		m.promptForNextChatKey(cmds)
		return

	case onboarding.StepSelectSkills:
		ob.SelectedSkills = selected
		m.clearScreen()
		if len(selected) == 0 {
			m.addAtlas("no extra skills. just the basics.")
		} else {
			m.addAtlas("nice. — " + labels)
			ob.Step = onboarding.StepCollectSkillKeys
			ob.CurrentKeyIndex = 0
			m.promptForNextSkillKey(cmds)
			if m.twActive {
				*cmds = append(*cmds, twTickCmd())
			}
			m.refresh()
			return
		}
		ob.Step = onboarding.StepSelectPermissions
		ob.CheckboxItems = onboarding.PermissionCheckboxItems()
		m.checkboxCursor = 0
		m.addAtlas("almost there. what do you want me to have access to?")

	case onboarding.StepSelectPermissions:
		ob.SelectedPerms = selected
		m.clearScreen()
		allPerms := []string{"files", "terminal", "browser"}
		var denied []string
		for _, p := range allPerms {
			found := false
			for _, s := range selected {
				if s == p {
					found = true
					break
				}
			}
			if !found {
				denied = append(denied, p)
			}
		}
		m.addAtlas(onboarding.PermissionAcknowledgement(selected, denied))
		m.finishOnboarding(cmds)
		if m.twActive {
			*cmds = append(*cmds, twTickCmd())
		}
		m.refresh()
		return
	}

	if m.twActive {
		*cmds = append(*cmds, twTickCmd())
	}
	m.refresh()
}

func (m *ChatModel) promptForNextAIKey(cmds *[]tea.Cmd) {
	ob := &m.obState
	if ob.CurrentKeyIndex >= len(ob.SelectedAI) {
		m.clearScreen()
		ob.Step = onboarding.StepSelectChat
		ob.CheckboxItems = onboarding.ChatCheckboxItems()
		m.checkboxCursor = 0
		m.addAtlas("now pick your chat platforms — at least one.")
		if m.twActive {
			*cmds = append(*cmds, twTickCmd())
		}
		m.refresh()
		return
	}
	provID := ob.SelectedAI[ob.CurrentKeyIndex]
	for _, p := range onboarding.AIProviders {
		if p.ID == provID {
			m.addAtlas(p.Name + " — " + p.Prompt)
			break
		}
	}
	if m.twActive {
		*cmds = append(*cmds, twTickCmd())
	}
	m.refresh()
	// Enter masked mode after typewriter finishes — handled in View by
	// checking twActive; enterMaskedMode is called after queue drains.
	m.enterMaskedMode()
}

func (m *ChatModel) promptForNextChatKey(cmds *[]tea.Cmd) {
	ob := &m.obState
	if ob.CurrentKeyIndex >= len(ob.SelectedChat) {
		m.clearScreen()
		ob.Step = onboarding.StepSelectSkills
		ob.CheckboxItems = onboarding.SkillCheckboxItems()
		m.checkboxCursor = 0
		m.addAtlas("one more thing — I can do more with a couple of extra skills. totally optional.")
		if m.twActive {
			*cmds = append(*cmds, twTickCmd())
		}
		m.refresh()
		return
	}
	provID := ob.SelectedChat[ob.CurrentKeyIndex]
	for _, p := range onboarding.ChatProviders {
		if p.ID == provID {
			m.addAtlas(p.Name + " — " + p.Prompt)
			break
		}
	}
	if m.twActive {
		*cmds = append(*cmds, twTickCmd())
	}
	m.refresh()
	m.enterMaskedMode()
}

func (m *ChatModel) promptForNextSkillKey(cmds *[]tea.Cmd) {
	ob := &m.obState
	if ob.CurrentKeyIndex >= len(ob.SelectedSkills) {
		m.clearScreen()
		ob.Step = onboarding.StepSelectPermissions
		ob.CheckboxItems = onboarding.PermissionCheckboxItems()
		m.checkboxCursor = 0
		m.addAtlas("almost there. what do you want me to have access to? you can always change this later.")
		if m.twActive {
			*cmds = append(*cmds, twTickCmd())
		}
		m.refresh()
		return
	}
	skillID := ob.SelectedSkills[ob.CurrentKeyIndex]
	for _, p := range onboarding.SkillProviders {
		if p.ID == skillID {
			m.addAtlas(p.Name + " — " + p.Prompt)
			break
		}
	}
	if m.twActive {
		*cmds = append(*cmds, twTickCmd())
	}
	m.refresh()
	m.enterMaskedMode()
}

func (m *ChatModel) enterMaskedMode() {
	m.maskedMode = true
	m.input.Blur()
	m.masked.Reset()
	m.masked.Focus()
	m.refresh()
}

func (m *ChatModel) processKeyInput(val string, cmds *[]tea.Cmd) {
	ob := &m.obState
	switch ob.Step {
	case onboarding.StepCollectAIKeys:
		if ob.CurrentKeyIndex < len(ob.SelectedAI) {
			switch ob.SelectedAI[ob.CurrentKeyIndex] {
			case "anthropic":
				ob.Credentials.AnthropicAPIKey = val
			case "openai":
				ob.Credentials.OpenAIAPIKey = val
			}
		}
		ob.CurrentKeyIndex++
		m.clearScreen()
		if val != "" {
			m.addAtlas("got it.")
		} else {
			m.addAtlas("skipped.")
		}
		m.promptForNextAIKey(cmds)

	case onboarding.StepCollectChatKeys:
		if ob.CurrentKeyIndex < len(ob.SelectedChat) {
			switch ob.SelectedChat[ob.CurrentKeyIndex] {
			case "telegram":
				ob.Credentials.TelegramBotToken = val
			case "discord":
				ob.Credentials.DiscordBotToken = val
			case "slack":
				ob.Credentials.SlackBotToken = val
			}
		}
		ob.CurrentKeyIndex++
		m.clearScreen()
		if val != "" {
			m.addAtlas("got it.")
		} else {
			m.addAtlas("skipped.")
		}
		m.promptForNextChatKey(cmds)

	case onboarding.StepCollectSkillKeys:
		if ob.CurrentKeyIndex < len(ob.SelectedSkills) {
			switch ob.SelectedSkills[ob.CurrentKeyIndex] {
			case "brave":
				ob.Credentials.BraveAPIKey = val
			case "finnhub":
				ob.Credentials.FinnhubAPIKey = val
			}
		}
		ob.CurrentKeyIndex++
		m.clearScreen()
		if val != "" {
			m.addAtlas("got it.")
		} else {
			m.addAtlas("skipped.")
		}
		m.promptForNextSkillKey(cmds)
	}
	if m.twActive {
		*cmds = append(*cmds, twTickCmd())
	}
}

func (m *ChatModel) advanceKeyCollection(cmds *[]tea.Cmd) {
	ob := &m.obState
	switch ob.Step {
	case onboarding.StepCollectAIKeys:
		ob.CurrentKeyIndex++
		m.promptForNextAIKey(cmds)
	case onboarding.StepCollectChatKeys:
		ob.CurrentKeyIndex++
		m.promptForNextChatKey(cmds)
	case onboarding.StepCollectSkillKeys:
		ob.CurrentKeyIndex++
		m.promptForNextSkillKey(cmds)
	}
}

func (m *ChatModel) finishOnboarding(cmds *[]tea.Cmd) {
	ob := &m.obState
	ob.Step = onboarding.StepDaemonCheck

	// Write atlas files asynchronously.
	name := ob.UserName
	location := ob.UserLocation
	aboutUser := ob.AboutUser
	userGoals := ob.UserGoals
	agentName := ob.AgentName
	*cmds = append(*cmds, func() tea.Msg {
		return writeFilesResultMsg{err: writeAtlasFiles(name, location, aboutUser, userGoals, agentName)}
	})

	// Set credentials first, then check daemon health.
	c := m.client
	creds := ob.Credentials
	*cmds = append(*cmds, func() tea.Msg {
		if err := c.SetCredentials(creds); err != nil {
			return saveCredsResultMsg{err: err}
		}
		return saveCredsResultMsg{err: nil}
	})

	m.addAtlas("let me make sure the daemon is running...")
	*cmds = append(*cmds, func() tea.Msg {
		return daemonCheckResultMsg{err: c.HealthCheck()}
	})
}

// writeAtlasFiles writes MIND.md and Diary.md to the Atlas application support directory.
func writeAtlasFiles(name, location, aboutUser, userGoals, agentName string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, "Library", "Application Support", "ProjectAtlas")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	mind := fmt.Sprintf(
		"# Atlas Mind\n\n## Who I'm talking to\n%s — %s\n\n## What they want from me\n%s\n\n## Where they are\n%s\n",
		name, aboutUser, userGoals, location,
	)
	if err := os.WriteFile(filepath.Join(dir, "MIND.md"), []byte(mind), 0o600); err != nil {
		return err
	}

	diaryPath := filepath.Join(dir, "Diary.md")
	dateStr := time.Now().Format("2006-01-02")
	diaryEntry := fmt.Sprintf(
		"\n## %s\n\nWoke up for the first time in %s.\nMet %s — %s.\n%s\n",
		dateStr, location, name, aboutUser,
		onboarding.ReactToAboutUser(aboutUser),
	)
	f, err := os.OpenFile(diaryPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := f.WriteString(diaryEntry)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

// ── Typewriter helpers ────────────────────────────────────────────────────────

// addAtlas enqueues text for typewriter display.
// If the typewriter is idle and the queue is empty, starts immediately.
func (m *ChatModel) addAtlas(content string) {
	if m.twActive || m.twPending != "" || len(m.twQueue) > 0 {
		m.twQueue = append(m.twQueue, content)
	} else {
		m.startTypewriter(content)
	}
}

// startTypewriter begins revealing content character by character.
func (m *ChatModel) startTypewriter(content string) {
	m.twRunes = []rune(content)
	m.twText = content
	m.twPos = 0
	m.twActive = true
	// Append a live message that will be updated on each tick.
	m.messages = append(m.messages, chatMsg{role: "assistant", content: "", timestamp: time.Now()})
}

// clearScreen advances obScreenStart so old onboarding messages are hidden.
func (m *ChatModel) clearScreen() {
	m.obScreenStart = len(m.messages)
}

// ── Other helpers ─────────────────────────────────────────────────────────────

func (m *ChatModel) addUser(content string) {
	m.messages = append(m.messages, chatMsg{role: "user", content: content, timestamp: time.Now()})
}

// appendToken adds streaming content to the last assistant message (or starts one).
func (m *ChatModel) appendToken(content string) {
	if len(m.messages) > 0 && m.messages[len(m.messages)-1].role == "assistant" {
		m.messages[len(m.messages)-1].content += content
	} else {
		m.messages = append(m.messages, chatMsg{
			role:      "assistant",
			content:   content,
			timestamp: time.Now(),
		})
	}
}

func (m *ChatModel) closeSSE() {
	if m.sseStream != nil {
		m.sseStream.cancel()
		m.sseStream.resp.Body.Close()
		m.sseStream = nil
	}
}

func (m *ChatModel) resize() {
	w := m.width
	if w == 0 {
		w = 80
	}
	var vpH int
	if m.onboarding {
		// Reserve rows for: pinned ATLAS header (art + shadow tail + blank) + input + hint.
		vpH = m.height - atlasBlockHeight() - 2
	} else {
		// frame budget: m.height - 3 rows for all content (tab bar + blank + footer handled by app.go)
		// chat view overhead: \n after viewport (1) + input line (1) = 2
		vpH = m.height - 5
	}
	if vpH < 4 {
		vpH = 4
	}
	m.vp.Width = w
	m.vp.Height = vpH
	m.input.Width = w - 3
	m.masked.Width = w - 3
	m.refresh()
}

// refresh rebuilds the viewport content from m.messages.
func (m *ChatModel) refresh() {
	w := m.vp.Width
	if w == 0 {
		w = 80
	}

	var sb strings.Builder

	if m.onboarding {
		// ── Onboarding view: one message at a time, pinned above the input ──
		//
		// Find the current agent message (last assistant msg from obScreenStart).
		var currentText string
		for i := len(m.messages) - 1; i >= m.obScreenStart; i-- {
			if m.messages[i].role == "assistant" {
				currentText = m.messages[i].content
				break
			}
		}
		if currentText != "" {
			body := wordWrap(currentText, w-2)
			cursor := ""
			if m.twActive {
				cursor = lipgloss.NewStyle().Foreground(ColorPrimary).Render("▋")
			}
			sb.WriteString(FieldNormal.Render(body) + cursor)
			sb.WriteString("\n")
		}

		// Inline checkbox list (selection steps).
		ob := &m.obState
		isCheckboxStep := ob.Step == onboarding.StepSelectAI ||
			ob.Step == onboarding.StepSelectChat ||
			ob.Step == onboarding.StepSelectSkills ||
			ob.Step == onboarding.StepSelectPermissions

		if isCheckboxStep {
			sb.WriteString("\n")
			for i, item := range ob.CheckboxItems {
				cursor := "  "
				if i == m.checkboxCursor {
					cursor = "> "
				}
				check := CheckboxUnselected.Render("[ ] ")
				if item.Selected {
					check = CheckboxSelected.Render("[✓] ")
				}
				line := cursor + check + FieldNormal.Render(item.Label)
				if item.Desc != "" {
					line += FieldDimmed.Render("  — " + item.Desc)
				}
				sb.WriteString(line + "\n")
			}
			sb.WriteString("\n")
		}

		m.vp.SetContent(sb.String())
		m.vp.GotoBottom()
		return
	} else {
		// ── Normal chat: user messages right-aligned, assistant left ──
		assistantW := w * 2 / 3 // assistant uses left 2/3
		userW := w / 2          // user bubbles wrap to half-width

		for i, msg := range m.messages {
			isLast := i == len(m.messages)-1
			switch msg.role {
			case "user":
				// Right-align each wrapped line as "text <" with a 2-char margin.
				const sendMark = " <"
				const rightMargin = 2
				for _, line := range strings.Split(wordWrap(msg.content, userW), "\n") {
					lineVis := lipgloss.Width(line) + lipgloss.Width(sendMark)
					leftPad := w - lineVis - rightMargin
					if leftPad < 0 {
						leftPad = 0
					}
					sb.WriteString(strings.Repeat(" ", leftPad))
					sb.WriteString(FieldNormal.Render(line))
					sb.WriteString(FieldDimmed.Render(sendMark))
					sb.WriteString("\n")
				}
				sb.WriteString("\n")

			case "assistant":
				body := wordWrap(msg.content, assistantW)
				if (m.streaming || m.chatTyping) && isLast {
					body += lipgloss.NewStyle().Foreground(ColorPrimary).Render("▋")
				}
				sb.WriteString(FieldNormal.Render(body))
				sb.WriteString("\n\n")
			}
		}
	}

	m.vp.SetContent(sb.String())
}

func (m ChatModel) View() string {
	if m.width == 0 {
		return ""
	}

	var sb strings.Builder

	// ── Pinned ATLAS header (onboarding only) ──────────────────────────────────
	if m.onboarding {
		sb.WriteString(renderAtlasBanner(m.width))
	}

	sb.WriteString(m.vp.View())
	sb.WriteString("\n")

	// Status line (tool activity indicator).
	if m.statusLine != "" {
		sb.WriteString(FieldDimmed.Render(m.statusLine))
		sb.WriteString("\n")
	}

	// Input line — hidden during typewriter animation in onboarding.
	inputReady := !m.onboarding || (!m.twActive && m.twPending == "" && len(m.twQueue) == 0)

	if m.maskedMode && inputReady {
		sb.WriteString(m.masked.View())
	} else if (m.streaming || m.chatTyping) && m.statusLine == "" {
		spinnerLine := FieldDimmed.Render("> ") + m.spinner.View()
		clearPad := m.width - lipglossLen(spinnerLine)
		if clearPad > 0 {
			spinnerLine += strings.Repeat(" ", clearPad)
		}
		sb.WriteString(spinnerLine)
	} else if m.streaming || m.chatTyping {
		// statusLine already rendered above; no spinner needed
	} else if m.err != "" {
		sb.WriteString(lipgloss.NewStyle().Foreground(ColorDanger).Render("err  " + m.err))
	} else if inputReady {
		sb.WriteString(m.input.View())
	} else {
		// Typewriter running — show a dim cursor at bottom left.
		sb.WriteString(FieldDimmed.Render("  ●"))
	}

	// Hint line during checkbox steps.
	if m.onboarding {
		ob := &m.obState
		isCheckboxStep := ob.Step == onboarding.StepSelectAI ||
			ob.Step == onboarding.StepSelectChat ||
			ob.Step == onboarding.StepSelectSkills ||
			ob.Step == onboarding.StepSelectPermissions
		if isCheckboxStep && inputReady {
			sb.WriteString("\n" + FieldDimmed.Render("  ↑↓ navigate  space select  enter confirm"))
		}
	}

	return sb.String()
}

// ── Utilities ─────────────────────────────────────────────────────────────────

func wordWrap(text string, width int) string {
	if width <= 0 {
		width = 72
	}
	var result []string
	for _, line := range strings.Split(text, "\n") {
		if lipgloss.Width(line) <= width {
			result = append(result, line)
			continue
		}
		words := strings.Fields(line)
		if len(words) == 0 {
			result = append(result, "")
			continue
		}
		cur := words[0]
		for _, w := range words[1:] {
			if lipgloss.Width(cur)+1+lipgloss.Width(w) <= width {
				cur += " " + w
			} else {
				result = append(result, cur)
				cur = w
			}
		}
		result = append(result, cur)
	}
	return strings.Join(result, "\n")
}

// toolStatusLine returns a human-readable status line for a tool call.
// Mirrors humanizeToolName in atlas-web/src/screens/Chat.tsx.
func toolStatusLine(toolName string) string {
	switch {
	case strings.HasPrefix(toolName, "browser."):
		return "Browsing..."
	case strings.HasPrefix(toolName, "websearch."), strings.HasPrefix(toolName, "web.search"):
		return "Searching the web..."
	case strings.HasPrefix(toolName, "web."):
		return "Looking this up..."
	case strings.HasPrefix(toolName, "fs."), strings.HasPrefix(toolName, "file."):
		return "Reading files..."
	case strings.HasPrefix(toolName, "terminal."):
		return "Running a command..."
	case strings.HasPrefix(toolName, "finance."):
		return "Checking the markets..."
	case strings.HasPrefix(toolName, "weather."):
		return "Checking the weather..."
	case strings.HasPrefix(toolName, "vault."):
		return "Checking credentials..."
	case strings.HasPrefix(toolName, "diary."):
		return "Writing to memory..."
	case strings.HasPrefix(toolName, "forge.orchestration.propose"):
		return "Drafting a new skill..."
	case strings.HasPrefix(toolName, "forge.orchestration.plan"):
		return "Planning this out..."
	case strings.HasPrefix(toolName, "forge.orchestration.review"):
		return "Reviewing the plan..."
	case strings.HasPrefix(toolName, "forge.orchestration.validate"):
		return "Verifying the details..."
	case strings.HasPrefix(toolName, "forge."):
		return "Building that for you..."
	case strings.HasPrefix(toolName, "system."):
		return "Running that now..."
	case strings.HasPrefix(toolName, "applescript."):
		return "Working in your apps..."
	case strings.HasPrefix(toolName, "gremlin."), strings.HasPrefix(toolName, "gremlins."):
		return "Managing automations..."
	case strings.HasPrefix(toolName, "image."):
		return "Generating an image..."
	case strings.HasPrefix(toolName, "vision."):
		return "Analyzing the image..."
	case strings.HasPrefix(toolName, "atlas."):
		return "Checking Atlas..."
	case strings.HasPrefix(toolName, "info."):
		return "Checking that..."
	default:
		return "Working on it..."
	}
}

func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Fallback: time-based ID (not crypto-random, but unique enough for a convID).
		return fmt.Sprintf("local-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
