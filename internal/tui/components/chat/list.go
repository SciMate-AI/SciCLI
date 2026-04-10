package chat

import (
	"context"
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/SciMate-AI/scicli/internal/app"
	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/SciMate-AI/scicli/internal/message"
	"github.com/SciMate-AI/scicli/internal/pubsub"
	"github.com/SciMate-AI/scicli/internal/research"
	"github.com/SciMate-AI/scicli/internal/session"
	"github.com/SciMate-AI/scicli/internal/tui/components/dialog"
	"github.com/SciMate-AI/scicli/internal/tui/styles"
	"github.com/SciMate-AI/scicli/internal/tui/theme"
	"github.com/SciMate-AI/scicli/internal/tui/util"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
)

type cacheItem struct {
	width   int
	content []uiMessage
}

type messagesOverviewLoadedMsg struct {
	sessionID         string
	researchSessionID string
	researchInherited bool
	researchTitle     string
	researchState     research.SessionState
}

type messagesCmp struct {
	app           *app.App
	width, height int
	viewport      viewport.Model
	session       session.Session
	messages      []message.Message
	uiMessages    []uiMessage
	currentMsgID  string
	cachedContent map[string]cacheItem
	spinner       spinner.Model
	rendering     bool
	expandTools   bool
	attachments   viewport.Model
	contentLines  int

	research          research.SessionState
	researchSessionID string
	researchInherited bool
	researchTitle     string
	draggingScrollbar bool
	mouseMode         bool
}
type renderFinishedMsg struct{}

const transcriptScrollbarZoneID = "chat-transcript-scrollbar"

type MouseModeChangedMsg struct {
	Enabled bool
}

type MessageKeys struct {
	PageDown     key.Binding
	PageUp       key.Binding
	HalfPageUp   key.Binding
	HalfPageDown key.Binding
}

var messageKeys = MessageKeys{
	PageDown: key.NewBinding(
		key.WithKeys("pgdown"),
		key.WithHelp("f/pgdn", "page down"),
	),
	PageUp: key.NewBinding(
		key.WithKeys("pgup"),
		key.WithHelp("b/pgup", "page up"),
	),
	HalfPageUp: key.NewBinding(
		key.WithKeys("ctrl+u"),
		key.WithHelp("ctrl+u", "½ page up"),
	),
	HalfPageDown: key.NewBinding(
		key.WithKeys("ctrl+d", "ctrl+d"),
		key.WithHelp("ctrl+d", "½ page down"),
	),
}

func (m *messagesCmp) Init() tea.Cmd {
	return tea.Batch(m.viewport.Init(), m.spinner.Tick, m.loadOverviewCmd())
}

func (m *messagesCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case dialog.ThemeChangedMsg:
		m.rerender()
		return m, nil
	case MouseModeChangedMsg:
		m.mouseMode = msg.Enabled
		return m, nil
	case SessionSelectedMsg:
		if msg.ID != m.session.ID {
			cmd := m.SetSession(msg)
			return m, tea.Batch(cmd, m.loadOverviewCmd())
		}
		return m, nil
	case SessionClearedMsg:
		m.session = session.Session{}
		m.messages = make([]message.Message, 0)
		m.currentMsgID = ""
		m.rendering = false
		m.research = research.SessionState{}
		m.researchSessionID = ""
		m.researchInherited = false
		m.researchTitle = ""
		return m, nil

	case tea.KeyMsg:
		if key.Matches(msg, messageKeys.PageUp) || key.Matches(msg, messageKeys.PageDown) ||
			key.Matches(msg, messageKeys.HalfPageUp) || key.Matches(msg, messageKeys.HalfPageDown) {
			u, cmd := m.viewport.Update(msg)
			m.viewport = u
			cmds = append(cmds, cmd)
		}
		if key.Matches(msg, toggleToolResultsKey) {
			m.expandTools = !m.expandTools
			m.rerender()
		}
	case tea.MouseMsg:
		if m.handleScrollbarMouse(msg) {
			return m, nil
		}
		u, cmd := m.viewport.Update(msg)
		m.viewport = u
		cmds = append(cmds, cmd)

	case renderFinishedMsg:
		m.rendering = false
		m.viewport.GotoBottom()
	case pubsub.Event[session.Session]:
		if msg.Type == pubsub.UpdatedEvent && msg.Payload.ID == m.session.ID {
			m.session = msg.Payload
			if m.session.SummaryMessageID == m.currentMsgID {
				delete(m.cachedContent, m.currentMsgID)
				m.renderView()
			}
			return m, m.loadOverviewCmd()
		}
	case pubsub.Event[research.SessionState]:
		if msg.Payload.SessionID == m.researchSessionID {
			m.research = msg.Payload
			return m, nil
		}
	case messagesOverviewLoadedMsg:
		if msg.sessionID != m.session.ID {
			return m, nil
		}
		m.research = msg.researchState
		m.researchSessionID = msg.researchSessionID
		m.researchInherited = msg.researchInherited
		m.researchTitle = msg.researchTitle
		return m, nil
	case pubsub.Event[message.Message]:
		needsRerender := false
		if msg.Type == pubsub.CreatedEvent {
			if msg.Payload.SessionID == m.session.ID {

				messageExists := false
				for _, v := range m.messages {
					if v.ID == msg.Payload.ID {
						messageExists = true
						break
					}
				}

				if !messageExists {
					if len(m.messages) > 0 {
						lastMsgID := m.messages[len(m.messages)-1].ID
						delete(m.cachedContent, lastMsgID)
					}

					m.messages = append(m.messages, msg.Payload)
					delete(m.cachedContent, m.currentMsgID)
					m.currentMsgID = msg.Payload.ID
					needsRerender = true
				}
			}
			// There are tool calls from the child task
			for _, v := range m.messages {
				for _, c := range v.ToolCalls() {
					if c.ID == msg.Payload.SessionID {
						delete(m.cachedContent, v.ID)
						needsRerender = true
					}
				}
			}
		} else if msg.Type == pubsub.UpdatedEvent && msg.Payload.SessionID == m.session.ID {
			for i, v := range m.messages {
				if v.ID == msg.Payload.ID {
					m.messages[i] = msg.Payload
					delete(m.cachedContent, msg.Payload.ID)
					needsRerender = true
					break
				}
			}
		}
		if needsRerender {
			m.renderView()
			if len(m.messages) > 0 {
				if (msg.Type == pubsub.CreatedEvent) ||
					(msg.Type == pubsub.UpdatedEvent && msg.Payload.ID == m.messages[len(m.messages)-1].ID) {
					m.viewport.GotoBottom()
				}
			}
		}
	}

	spinner, cmd := m.spinner.Update(msg)
	m.spinner = spinner
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

func (m *messagesCmp) IsAgentWorking() bool {
	if m.app == nil || m.app.CoderAgent == nil {
		return false
	}
	return m.app.CoderAgent.IsSessionBusy(m.session.ID)
}

func (m *messagesCmp) loadOverviewCmd() tea.Cmd {
	sessionID := m.session.ID
	currentSession := m.session
	if sessionID == "" {
		return nil
	}
	return func() tea.Msg {
		researchSessionID, inherited, title, state := loadResearchSnapshot(context.Background(), m.app, currentSession)
		return messagesOverviewLoadedMsg{
			sessionID:         sessionID,
			researchSessionID: researchSessionID,
			researchInherited: inherited,
			researchTitle:     title,
			researchState:     state,
		}
	}
}

func formatTimeDifference(unixTime1, unixTime2 int64) string {
	diffSeconds := float64(math.Abs(float64(unixTime2 - unixTime1)))

	if diffSeconds < 60 {
		return fmt.Sprintf("%.1fs", diffSeconds)
	}

	minutes := int(diffSeconds / 60)
	seconds := int(diffSeconds) % 60
	return fmt.Sprintf("%dm%ds", minutes, seconds)
}

func (m *messagesCmp) renderView() {
	m.uiMessages = make([]uiMessage, 0)
	pos := 0
	baseStyle := styles.BaseStyle()

	if m.width == 0 {
		return
	}
	columnWidth := codexColumnWidth(m.width)
	for inx, msg := range m.messages {
		switch msg.Role {
		case message.User:
			if cache, ok := m.cachedContent[msg.ID]; ok && cache.width == columnWidth {
				m.uiMessages = append(m.uiMessages, cache.content...)
				continue
			}
			userMsg := renderUserMessage(
				msg,
				msg.ID == m.currentMsgID,
				columnWidth,
				pos,
			)
			m.uiMessages = append(m.uiMessages, userMsg)
			m.cachedContent[msg.ID] = cacheItem{
				width:   columnWidth,
				content: []uiMessage{userMsg},
			}
			pos += userMsg.height + 1 // + 1 for spacing
		case message.Assistant:
			if cache, ok := m.cachedContent[msg.ID]; ok && cache.width == columnWidth {
				m.uiMessages = append(m.uiMessages, cache.content...)
				continue
			}
			isSummary := m.session.SummaryMessageID == msg.ID

			assistantMessages := renderAssistantMessage(
				msg,
				inx,
				m.messages,
				m.app.Messages,
				m.currentMsgID,
				isSummary,
				m.expandTools,
				columnWidth,
				pos,
			)
			for _, msg := range assistantMessages {
				m.uiMessages = append(m.uiMessages, msg)
				pos += msg.height + 1 // + 1 for spacing
			}
			m.cachedContent[msg.ID] = cacheItem{
				width:   columnWidth,
				content: assistantMessages,
			}
		}
	}

	messages := make([]string, 0)
	for i, v := range m.uiMessages {
		if i > 0 {
			messages = append(messages, "")
		}
		messages = append(messages, v.content)
	}

	content := baseStyle.
		Width(m.viewport.Width).
		Render(codexCenter(
			m.viewport.Width,
			lipgloss.JoinVertical(
				lipgloss.Top,
				messages...,
			),
		))
	m.contentLines = strings.Count(content, "\n") + 1
	m.viewport.SetContent(content)
}

func (m *messagesCmp) View() string {
	baseStyle := styles.BaseStyle()

	if m.rendering {
		return baseStyle.
			Width(m.width).
			Render(
				lipgloss.JoinVertical(
					lipgloss.Top,
					m.working(),
				),
			)
	}
	if len(m.messages) == 0 {
		content := baseStyle.
			Width(m.width).
			Height(max(1, m.height)).
			Render(m.initialScreen())

		return baseStyle.
			Width(m.width).
			Render(content)
	}

	return baseStyle.
		Width(m.width).
		Render(
			lipgloss.JoinVertical(
				lipgloss.Top,
				m.renderViewport(),
				m.footer(),
				m.working(),
			),
		)
}

func hasToolsWithoutResponse(messages []message.Message) bool {
	toolCalls := make([]message.ToolCall, 0)
	toolResults := make([]message.ToolResult, 0)
	for _, m := range messages {
		toolCalls = append(toolCalls, m.ToolCalls()...)
		toolResults = append(toolResults, m.ToolResults()...)
	}

	for _, v := range toolCalls {
		found := false
		for _, r := range toolResults {
			if v.ID == r.ToolCallID {
				found = true
				break
			}
		}
		if !found && v.Finished {
			return true
		}
	}
	return false
}

func hasUnfinishedToolCalls(messages []message.Message) bool {
	toolCalls := make([]message.ToolCall, 0)
	for _, m := range messages {
		toolCalls = append(toolCalls, m.ToolCalls()...)
	}
	for _, v := range toolCalls {
		if !v.Finished {
			return true
		}
	}
	return false
}

func hasToolResults(messages []message.Message) bool {
	for _, msg := range messages {
		if len(msg.ToolResults()) > 0 {
			return true
		}
	}
	return false
}

func (m *messagesCmp) working() string {
	if !m.IsAgentWorking() || len(m.messages) == 0 {
		return ""
	}

	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()
	task := "Thinking..."
	lastMessage := m.messages[len(m.messages)-1]
	if hasToolsWithoutResponse(m.messages) {
		task = "Waiting for tool response..."
	} else if hasUnfinishedToolCalls(m.messages) {
		task = "Building tool call..."
	} else if !lastMessage.IsFinished() {
		task = "Generating..."
	}

	return baseStyle.
		Width(m.width).
		Foreground(t.TextMuted()).
		Render(codexCenter(m.width, fmt.Sprintf("%s %s", m.spinner.View(), task)))
}

func (m *messagesCmp) helpText() string {
	if m.app != nil && m.app.CoderAgent != nil && m.app.CoderAgent.IsBusy() {
		return "Esc cancel"
	}
	text := "/ commands  @ paths  Ctrl+K palette"
	if hasToolResults(m.messages) {
		text += "  " + toggleToolResultsKey.Help().Key + " tool output"
	}
	if m.mouseMode {
		text += "  Alt+M 返回复制"
	} else {
		text += "  Alt+M 进入滚动"
	}
	return text
}

func (m *messagesCmp) initialScreen() string {
	baseStyle := styles.BaseStyle()
	providerLine, modelLine := activeProviderAndModel()
	lines := []string{
		m.renderConsoleHeader(),
		consoleMuted("workspace  " + config.WorkingDirectory()),
		consoleDivider(m.width, "ready"),
		codexSolidWordmark(m.width),
		"",
		consoleSection(m.width, "Start here",
			"Describe the task, paste code, or use a slash command.",
			"Use normal terminal scroll/selection outside the transcript; use PgUp/PgDn inside the transcript.",
		),
		"",
		consoleSection(m.width, "Try",
			"explain this repository architecture",
			"refactor the provider setup flow",
			"/sb new paper write a top-tier conference paper for this project",
		),
		"",
		consoleSection(m.width, "Shortcuts",
			"/ commands",
			"@ paths",
			"Ctrl+K palette",
			"/tasks",
			providerLine+"  "+modelLine,
		),
	}
	if strings.TrimSpace(m.research.Objective) != "" {
		lines = append(lines,
			"",
			consoleSection(m.width, "Current objective",
				truncateString(m.research.Objective, max(24, m.width*3)),
				consoleMuted("next  "+nextResearchAction(m.research)),
			),
		)
	}
	return baseStyle.
		Width(m.width).
		Height(max(1, m.height)).
		Render(lipgloss.JoinVertical(lipgloss.Top, lines...))
}

func scicliWordmark(width int) string {
	t := theme.CurrentTheme()
	if width < 48 {
		return styles.BaseStyle().
			Foreground(t.Primary()).
			Bold(true).
			Render("SCICLI")
	}
	return styles.BaseStyle().
		Foreground(t.Primary()).
		Bold(true).
		Render(strings.Join([]string{
			"███████╗ ██████╗██╗ ██████╗██╗     ██╗",
			"██╔════╝██╔════╝██║██╔════╝██║     ██║",
			"███████╗██║     ██║██║     ██║     ██║",
			"╚════██║██║     ██║██║     ██║     ██║",
			"███████║╚██████╗██║╚██████╗███████╗██║",
			"╚══════╝ ╚═════╝╚═╝ ╚═════╝╚══════╝╚═╝",
		}, "\n"))
}

func codexWordmark(width int) string {
	t := theme.CurrentTheme()
	if width < 72 {
		return styles.BaseStyle().
			Foreground(t.Primary()).
			Bold(true).
			Render("SCICLI")
	}

	wordmark := strings.Join([]string{
		"███████╗  ██████╗ ██╗  ██████╗ ██╗      ██╗",
		"██╔════╝ ██╔════╝ ██║ ██╔════╝ ██║      ██║",
		"███████╗ ██║      ██║ ██║      ██║      ██║",
		"╚════██║ ██║      ██║ ██║      ██║      ██║",
		"███████║ ╚██████╗ ██║ ╚██████╗ ███████╗ ██║",
	}, "\n")

	return styles.BaseStyle().
		Foreground(t.Primary()).
		Bold(true).
		Render(wordmark)
}

func codexSolidWordmark(width int) string {
	t := theme.CurrentTheme()
	if width < 72 {
		return styles.BaseStyle().
			Foreground(t.Primary()).
			Bold(true).
			Render("SCICLI")
	}

	wordmark := strings.Join([]string{
		"██████   █████   ██   █████   ██      ██",
		"██      ██   ██  ██  ██   ██  ██      ██",
		"██████  ██       ██  ██       ██      ██",
		"     ██ ██   ██  ██  ██   ██  ██      ██",
		"██████   █████   ██   █████   ██████  ██",
	}, "\n")

	return styles.BaseStyle().
		Foreground(t.Primary()).
		Bold(true).
		Render(wordmark)
}

func (m *messagesCmp) rerender() {
	for _, msg := range m.messages {
		delete(m.cachedContent, msg.ID)
	}
	m.renderView()
}

func (m *messagesCmp) SetSize(width, height int) tea.Cmd {
	if m.width == width && m.height == height {
		return nil
	}
	m.width = width
	m.height = height
	m.viewport.Width = max(1, width-m.scrollbarWidth())
	m.viewport.Height = max(3, height-2)
	m.attachments.Width = width + 40
	m.attachments.Height = 3
	m.rerender()
	return nil
}

func (m *messagesCmp) GetSize() (int, int) {
	return m.width, m.height
}

func (m *messagesCmp) SetSession(session session.Session) tea.Cmd {
	if m.session.ID == session.ID {
		return nil
	}
	m.session = session
	messages, err := m.app.Messages.List(context.Background(), session.ID)
	if err != nil {
		return util.ReportError(err)
	}
	m.messages = messages
	if len(m.messages) > 0 {
		m.currentMsgID = m.messages[len(m.messages)-1].ID
	}
	delete(m.cachedContent, m.currentMsgID)
	m.rendering = true
	return func() tea.Msg {
		m.renderView()
		return renderFinishedMsg{}
	}
}

func (m *messagesCmp) BindingKeys() []key.Binding {
	return []key.Binding{
		m.viewport.KeyMap.PageDown,
		m.viewport.KeyMap.PageUp,
		m.viewport.KeyMap.HalfPageUp,
		m.viewport.KeyMap.HalfPageDown,
		toggleToolResultsKey,
	}
}

func NewMessagesCmp(app *app.App) tea.Model {
	s := spinner.New()
	s.Spinner = spinner.Pulse
	vp := viewport.New(0, 0)
	attachmets := viewport.New(0, 0)
	vp.KeyMap.PageUp = messageKeys.PageUp
	vp.KeyMap.PageDown = messageKeys.PageDown
	vp.KeyMap.HalfPageUp = messageKeys.HalfPageUp
	vp.KeyMap.HalfPageDown = messageKeys.HalfPageDown
	return &messagesCmp{
		app:           app,
		cachedContent: make(map[string]cacheItem),
		viewport:      vp,
		spinner:       s,
		attachments:   attachmets,
		mouseMode:     strings.TrimSpace(os.Getenv("SCICLI_NO_MOUSE")) == "",
	}
}

func (m *messagesCmp) renderViewport() string {
	if m.width <= 0 {
		return ""
	}
	if m.scrollbarWidth() == 0 {
		return m.viewport.View()
	}
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.viewport.View(),
		m.renderScrollbar(),
	)
}

func (m *messagesCmp) renderScrollbar() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	height := max(1, m.viewport.Height)
	if m.contentLines <= height {
		return zone.Mark(transcriptScrollbarZoneID, baseStyle.Foreground(t.TextMuted()).Render(strings.Repeat(" ", 1)))
	}

	thumbSize := max(1, int(math.Round(float64(height*height)/float64(max(1, m.contentLines)))))
	maxTop := max(0, height-thumbSize)
	thumbTop := 0
	if maxTop > 0 {
		thumbTop = int(math.Round(m.viewport.ScrollPercent() * float64(maxTop)))
	}

	lines := make([]string, 0, height)
	for i := 0; i < height; i++ {
		ch := "|"
		color := t.TextMuted()
		if i >= thumbTop && i < thumbTop+thumbSize {
			ch = "#"
			color = t.Primary()
		}
		lines = append(lines, baseStyle.Foreground(color).Render(ch))
	}
	return zone.Mark(transcriptScrollbarZoneID, lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m *messagesCmp) scrollbarWidth() int {
	if m.width < 3 {
		return 0
	}
	return 1
}

func (m *messagesCmp) handleScrollbarMouse(msg tea.MouseMsg) bool {
	if m.contentLines <= m.viewport.Height || m.scrollbarWidth() == 0 {
		m.draggingScrollbar = false
		return false
	}

	scrollbarZone := zone.Get(transcriptScrollbarZoneID)
	switch msg.Action {
	case tea.MouseActionPress:
		if msg.Button != tea.MouseButtonLeft || !scrollbarZone.InBounds(msg) {
			m.draggingScrollbar = false
			return false
		}
		m.draggingScrollbar = true
		_, localY := scrollbarZone.Pos(msg)
		m.viewport.SetYOffset(m.scrollbarTargetYOffset(localY))
		return true
	case tea.MouseActionMotion:
		if !m.draggingScrollbar || !scrollbarZone.InBounds(msg) {
			return false
		}
		_, localY := scrollbarZone.Pos(msg)
		m.viewport.SetYOffset(m.scrollbarTargetYOffset(localY))
		return true
	case tea.MouseActionRelease:
		if m.draggingScrollbar {
			m.draggingScrollbar = false
			if scrollbarZone.InBounds(msg) {
				_, localY := scrollbarZone.Pos(msg)
				m.viewport.SetYOffset(m.scrollbarTargetYOffset(localY))
				return true
			}
		}
	}

	return false
}

func (m *messagesCmp) scrollbarTargetYOffset(localY int) int {
	height := max(1, m.viewport.Height)
	maxOffset := max(0, m.contentLines-m.viewport.Height)
	if maxOffset == 0 {
		return 0
	}
	if localY <= 0 {
		return 0
	}
	if localY >= height-1 {
		return maxOffset
	}
	ratio := float64(localY) / float64(max(1, height-1))
	return int(math.Round(ratio * float64(maxOffset)))
}

func (m *messagesCmp) footer() string {
	baseStyle := styles.BaseStyle()

	left := m.scrollStatusText()
	right := m.helpText()
	if left == "" {
		return baseStyle.Width(m.width).Foreground(theme.CurrentTheme().TextMuted()).Render(right)
	}

	space := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if space < 1 {
		return baseStyle.Width(m.width).Foreground(theme.CurrentTheme().TextMuted()).Render(left + "  " + right)
	}
	return baseStyle.Width(m.width).Foreground(theme.CurrentTheme().TextMuted()).Render(left + strings.Repeat(" ", space) + right)
}

func (m *messagesCmp) scrollStatusText() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	if len(m.messages) == 0 {
		return ""
	}

	position := "top"
	switch {
	case m.contentLines <= m.viewport.Height:
		position = "all"
	case m.viewport.AtBottom():
		position = "bottom"
	case !m.viewport.AtTop():
		position = fmt.Sprintf("%.0f%%", m.viewport.ScrollPercent()*100)
	}

	up := " "
	if !m.viewport.AtTop() {
		up = "^"
	}
	down := " "
	if !m.viewport.AtBottom() {
		down = "v"
	}

	return baseStyle.
		Foreground(t.TextMuted()).
		Render(fmt.Sprintf("%s%s transcript %s", up, down, position))
}

func (m *messagesCmp) renderConsoleHeader() string {
	baseStyle := styles.BaseStyle()
	title := "scicli"
	if strings.TrimSpace(m.session.Title) != "" {
		title = truncateString(m.session.Title, max(18, m.width/2))
	}
	providerLine, modelLine := activeProviderAndModel()
	state := "idle"
	switch {
	case m.IsAgentWorking():
		state = "running"
	case len(m.messages) > 0:
		last := m.messages[len(m.messages)-1]
		switch last.FinishReason() {
		case message.FinishReasonError:
			state = "failed"
		case message.FinishReasonPermissionDenied:
			state = "blocked"
		case message.FinishReasonCanceled:
			state = "canceled"
		case message.FinishReasonEndTurn:
			state = "ready"
		}
	}

	left := baseStyle.Bold(true).Render(styles.OpenCodeIcon + " " + title)
	right := consoleMuted(strings.Join([]string{
		providerLine,
		modelLine,
		fmt.Sprintf("%d msgs", len(m.messages)),
		"state " + state,
	}, "  "))
	space := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if space < 2 {
		return lipgloss.JoinVertical(lipgloss.Left, left, right)
	}
	return left + strings.Repeat(" ", space) + right
}
