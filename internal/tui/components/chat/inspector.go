package chat

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/SciMate-AI/scicli/internal/app"
	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/SciMate-AI/scicli/internal/diff"
	"github.com/SciMate-AI/scicli/internal/history"
	"github.com/SciMate-AI/scicli/internal/message"
	"github.com/SciMate-AI/scicli/internal/pubsub"
	"github.com/SciMate-AI/scicli/internal/research"
	"github.com/SciMate-AI/scicli/internal/session"
	"github.com/SciMate-AI/scicli/internal/skills"
	"github.com/SciMate-AI/scicli/internal/taskrun"
	"github.com/SciMate-AI/scicli/internal/tui/layout"
	"github.com/SciMate-AI/scicli/internal/tui/styles"
	"github.com/SciMate-AI/scicli/internal/tui/theme"
	"github.com/SciMate-AI/scicli/internal/tui/util"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
)

type InspectorKeyMsg struct {
	Key tea.KeyMsg
}

type InspectorFocusMsg struct {
	Focused bool
}

type InspectorOpenTaskMsg struct {
	SessionID string
}

type InspectorStopTaskMsg struct {
	SessionID string
}

type WorkbenchRunCommandMsg struct {
	Command string
}

type WorkbenchOpenArgumentsMsg struct {
	CommandID string
	Command   string
	ArgNames  []string
}

type inspectorDataLoadedMsg struct {
	sessionID string
	data      inspectorData
	err       error
}

type inspectorTimelineLoadedMsg struct {
	sessionID string
	timeline  []timelineSnapshot
}

type inspectorData struct {
	parentTitle       string
	activeSkills      []skills.Skill
	current           runSnapshot
	research          research.SessionState
	researchSessionID string
	researchInherited bool
	researchTitle     string
	tasks             []runSnapshot
	timelineSessionID string
	timeline          []timelineSnapshot
	modified          []modifiedFileStat
}

type modifiedFileStat struct {
	Path      string
	Additions int
	Removals  int
}

type runSnapshot struct {
	Session      session.Session
	Status       taskrun.Status
	Detail       string
	Preview      string
	StartedAt    int64
	FinishedAt   int64
	UpdatedAt    int64
	UpdatedLabel string
}

type timelineSnapshot struct {
	ID           int64
	SessionID    string
	Status       taskrun.Status
	Kind         taskrun.EventKind
	ToolName     string
	Metadata     taskrun.EventMetadata
	Detail       string
	CreatedAt    int64
	CreatedLabel string
}

type consoleFilter string

const (
	consoleFilterAll       consoleFilter = "all"
	consoleFilterLifecycle consoleFilter = "lifecycle"
	consoleFilterTools     consoleFilter = "tools"
	consoleFilterProgress  consoleFilter = "progress"

	consoleTimelineLimit = 256
)

type inspectorCmp struct {
	app               *app.App
	width             int
	height            int
	session           session.Session
	parentTitle       string
	activeSkills      []skills.Skill
	current           runSnapshot
	research          research.SessionState
	researchSessionID string
	researchInherited bool
	researchTitle     string
	tasks             []runSnapshot
	timelineSessionID string
	timeline          []timelineSnapshot
	modified          []modifiedFileStat

	focused             bool
	filterMode          bool
	filter              textinput.Model
	artifactFilterMode  bool
	artifactFilter      textinput.Model
	selected            int
	consoleFilter       consoleFilter
	consoleSearchMode   bool
	consoleSearch       textinput.Model
	consoleScrollOffset int
}

type inspectorKeyMap struct {
	Focus         key.Binding
	Up            key.Binding
	Down          key.Binding
	Open          key.Binding
	Stop          key.Binding
	Filter        key.Binding
	Artifact      key.Binding
	Clear         key.Binding
	ConsoleSearch key.Binding
	NextConsole   key.Binding
	PrevConsole   key.Binding
	ConsoleUp     key.Binding
	ConsoleDown   key.Binding
	ConsoleTop    key.Binding
	ConsoleTail   key.Binding
	Exit          key.Binding
}

var inspectorKeys = inspectorKeyMap{
	Focus:         key.NewBinding(key.WithKeys("ctrl+i"), key.WithHelp("ctrl+i", "focus inspector")),
	Up:            key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("up/k", "previous task")),
	Down:          key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("down/j", "next task")),
	Open:          key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open task")),
	Stop:          key.NewBinding(key.WithKeys("ctrl+x"), key.WithHelp("ctrl+x", "stop task")),
	Filter:        key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter tasks")),
	Artifact:      key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "filter artifacts")),
	Clear:         key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "clear filter")),
	ConsoleSearch: key.NewBinding(key.WithKeys("."), key.WithHelp(".", "filter console")),
	NextConsole:   key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next console filter")),
	PrevConsole:   key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "prev console filter")),
	ConsoleUp:     key.NewBinding(key.WithKeys("pgup", "pageup"), key.WithHelp("pgup", "scroll older")),
	ConsoleDown:   key.NewBinding(key.WithKeys("pgdown", "pagedown"), key.WithHelp("pgdn", "scroll newer")),
	ConsoleTop:    key.NewBinding(key.WithKeys("home"), key.WithHelp("home", "oldest console")),
	ConsoleTail:   key.NewBinding(key.WithKeys("end"), key.WithHelp("end", "follow live")),
	Exit:          key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "unfocus inspector")),
}

func (m *inspectorCmp) Init() tea.Cmd {
	return m.loadDataCmd()
}

func (m *inspectorCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case SessionSelectedMsg:
		m.session = msg
		m.parentTitle = ""
		m.current = runSnapshot{}
		m.research = research.SessionState{}
		m.researchSessionID = ""
		m.researchInherited = false
		m.researchTitle = ""
		m.tasks = nil
		m.timelineSessionID = ""
		m.timeline = nil
		m.modified = nil
		m.artifactFilterMode = false
		m.artifactFilter.SetValue("")
		m.selected = 0
		m.consoleScrollOffset = 0
		return m, m.loadDataCmd()
	case SessionClearedMsg:
		m.session = session.Session{}
		m.parentTitle = ""
		m.activeSkills = nil
		m.current = runSnapshot{}
		m.research = research.SessionState{}
		m.researchSessionID = ""
		m.researchInherited = false
		m.researchTitle = ""
		m.tasks = nil
		m.timelineSessionID = ""
		m.timeline = nil
		m.modified = nil
		m.artifactFilterMode = false
		m.artifactFilter.SetValue("")
		m.selected = 0
		m.consoleScrollOffset = 0
		return m, nil
	case InspectorFocusMsg:
		m.focused = msg.Focused
		if !m.focused {
			m.filterMode = false
			m.filter.Blur()
			m.artifactFilterMode = false
			m.artifactFilter.Blur()
			m.consoleSearchMode = false
			m.consoleSearch.Blur()
		}
		return m, nil
	case InspectorKeyMsg:
		return m, m.handleInspectorKey(msg.Key)
	case tea.MouseMsg:
		return m, m.handleMouse(msg)
	case inspectorDataLoadedMsg:
		if msg.sessionID != m.session.ID || msg.err != nil {
			return m, nil
		}
		m.parentTitle = msg.data.parentTitle
		m.activeSkills = msg.data.activeSkills
		m.current = msg.data.current
		m.research = msg.data.research
		m.researchSessionID = msg.data.researchSessionID
		m.researchInherited = msg.data.researchInherited
		m.researchTitle = msg.data.researchTitle
		m.tasks = msg.data.tasks
		if m.timelineSessionID != msg.data.timelineSessionID {
			m.consoleScrollOffset = 0
		}
		m.timelineSessionID = msg.data.timelineSessionID
		m.timeline = msg.data.timeline
		m.modified = msg.data.modified
		m.clampSelection()
		m.clampConsoleScroll()
		return m, nil
	case inspectorTimelineLoadedMsg:
		if msg.sessionID != m.timelineTargetSessionID() {
			return m, nil
		}
		if m.timelineSessionID != msg.sessionID {
			m.consoleScrollOffset = 0
		}
		m.timelineSessionID = msg.sessionID
		m.timeline = msg.timeline
		m.clampConsoleScroll()
		return m, nil
	case pubsub.Event[session.Session]:
		if msg.Type == pubsub.UpdatedEvent && msg.Payload.ID == m.session.ID {
			m.session = msg.Payload
			return m, m.loadCurrentRunCmd()
		}
	case pubsub.Event[message.Message]:
		if msg.Payload.SessionID == m.session.ID {
			return m, tea.Batch(m.loadCurrentRunCmd(), m.loadModifiedCmd())
		}
	case pubsub.Event[history.File]:
		if msg.Payload.SessionID == m.session.ID {
			return m, m.loadModifiedCmd()
		}
	case pubsub.Event[research.SessionState]:
		if msg.Payload.SessionID == m.researchSessionID {
			return m, m.loadDataCmd()
		}
	case pubsub.Event[taskrun.Run]:
		if msg.Payload.SessionID == m.session.ID && m.session.ParentSessionID != "" {
			m.current = mergeCurrentRunSnapshot(m.current, m.session, msg.Payload)
		}
		if msg.Payload.ParentSessionID == m.session.ID {
			previous := m.timelineTargetSessionID()
			m.upsertTaskSnapshot(runSnapshotFromTaskRun(msg.Payload))
			return m, m.timelineSyncCmd(previous)
		}
	case pubsub.Event[taskrun.Event]:
		if msg.Payload.SessionID == m.timelineSessionID {
			m.timeline = appendTimelineSnapshot(m.timeline, timelineSnapshotFromEvent(msg.Payload))
			m.clampConsoleScroll()
			return m, nil
		}
	}
	return m, nil
}

func (m *inspectorCmp) View() string {
	baseStyle := styles.BaseStyle()
	t := theme.CurrentTheme()
	width := max(1, m.width)

	title := "Run Inspector"
	if m.focused {
		title = "Run Inspector [Focused]"
	}
	header := baseStyle.Foreground(t.Primary()).Bold(true).Width(width).Render(title)

	if m.session.ID == "" {
		return baseStyle.Width(width).Height(max(1, m.height)).Render(lipgloss.JoinVertical(
			lipgloss.Left,
			header,
			baseStyle.Foreground(t.TextMuted()).Render("No active session"),
		))
	}

	filterLabel := "Task filter"
	if m.filterMode {
		filterLabel = "Task filter [typing]"
	}

	sections := []string{
		header,
		baseStyle.Foreground(t.TextMuted()).Width(width).Render("Ctrl+I focuses inspector. Enter opens task. Ctrl+X stops task. A filters artifacts. PgUp/PgDn scroll the run console."),
		sectionTitle(filterLabel, width),
		baseStyle.Width(width).Render(m.filter.View()),
		m.renderCurrentSession(width),
		m.renderResearchSection(width),
		m.renderExperimentSection(width),
		m.renderLineageSection(width),
		m.renderActionSection(width),
		m.renderArtifactSection(width),
		m.renderSkillSection(width),
		m.renderTaskSection(width),
		m.renderTaskConsole(width),
		m.renderModifiedFiles(width),
	}

	return baseStyle.
		Width(width).
		Height(max(1, m.height)).
		Render(lipgloss.JoinVertical(lipgloss.Left, sections...))
}

func (m *inspectorCmp) SetSize(width, height int) tea.Cmd {
	m.width = width
	m.height = height
	m.filter.Width = max(12, width-2)
	m.artifactFilter.Width = max(12, width-2)
	return nil
}

func (m *inspectorCmp) GetSize() (int, int) {
	return m.width, m.height
}

func (m *inspectorCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(inspectorKeys)
}

func NewInspectorCmp(app *app.App) tea.Model {
	filter := textinput.New()
	filter.Placeholder = "Filter delegated tasks"
	filter.Prompt = "> "
	filter.CharLimit = 120
	filter.Cursor.Blink = true
	consoleSearch := textinput.New()
	consoleSearch.Placeholder = "Filter console by tool/detail"
	consoleSearch.Prompt = "> "
	consoleSearch.CharLimit = 120
	consoleSearch.Cursor.Blink = true
	artifactFilter := textinput.New()
	artifactFilter.Placeholder = "Filter artifacts by kind, path, summary, experiment"
	artifactFilter.Prompt = "> "
	artifactFilter.CharLimit = 120
	artifactFilter.Cursor.Blink = true
	return &inspectorCmp{
		app:            app,
		filter:         filter,
		artifactFilter: artifactFilter,
		consoleFilter:  consoleFilterAll,
		consoleSearch:  consoleSearch,
	}
}

func (m *inspectorCmp) handleInspectorKey(msg tea.KeyMsg) tea.Cmd {
	if !m.focused {
		return nil
	}

	if m.consoleSearchMode {
		switch {
		case key.Matches(msg, inspectorKeys.Exit):
			m.consoleSearchMode = false
			m.consoleSearch.Blur()
			return nil
		case key.Matches(msg, inspectorKeys.Clear):
			m.consoleSearch.SetValue("")
			m.consoleScrollOffset = 0
			return nil
		default:
			previousQuery := m.consoleSearch.Value()
			var cmd tea.Cmd
			m.consoleSearch, cmd = m.consoleSearch.Update(msg)
			if m.consoleSearch.Value() != previousQuery {
				m.consoleScrollOffset = 0
				m.clampConsoleScroll()
			}
			return cmd
		}
	}

	if m.filterMode {
		previous := m.timelineTargetSessionID()
		switch {
		case key.Matches(msg, inspectorKeys.Exit):
			m.filterMode = false
			m.filter.Blur()
			return nil
		default:
			var cmd tea.Cmd
			m.filter, cmd = m.filter.Update(msg)
			m.clampSelection()
			return tea.Batch(cmd, m.timelineSyncCmd(previous))
		}
	}

	if m.artifactFilterMode {
		switch {
		case key.Matches(msg, inspectorKeys.Exit):
			m.artifactFilterMode = false
			m.artifactFilter.Blur()
			return nil
		case key.Matches(msg, inspectorKeys.Clear):
			m.artifactFilter.SetValue("")
			return nil
		default:
			var cmd tea.Cmd
			m.artifactFilter, cmd = m.artifactFilter.Update(msg)
			return cmd
		}
	}

	previous := m.timelineTargetSessionID()
	switch {
	case key.Matches(msg, inspectorKeys.Up):
		m.selected--
		m.clampSelection()
	case key.Matches(msg, inspectorKeys.Down):
		m.selected++
		m.clampSelection()
	case key.Matches(msg, inspectorKeys.Filter):
		m.filterMode = true
		m.filter.Focus()
		return m.filter.Cursor.BlinkCmd()
	case key.Matches(msg, inspectorKeys.Artifact):
		m.artifactFilterMode = true
		m.artifactFilter.Focus()
		return m.artifactFilter.Cursor.BlinkCmd()
	case key.Matches(msg, inspectorKeys.Clear):
		m.filter.SetValue("")
		m.artifactFilter.SetValue("")
		m.clampSelection()
		return m.timelineSyncCmd(previous)
	case key.Matches(msg, inspectorKeys.ConsoleSearch):
		m.consoleSearchMode = true
		m.consoleSearch.Focus()
		return m.consoleSearch.Cursor.BlinkCmd()
	case key.Matches(msg, inspectorKeys.NextConsole):
		m.consoleFilter = nextConsoleFilter(m.consoleFilter, 1)
		m.consoleScrollOffset = 0
	case key.Matches(msg, inspectorKeys.PrevConsole):
		m.consoleFilter = nextConsoleFilter(m.consoleFilter, -1)
		m.consoleScrollOffset = 0
	case key.Matches(msg, inspectorKeys.ConsoleUp):
		m.consoleScrollOffset += m.consoleStepSize()
		m.clampConsoleScroll()
		return nil
	case key.Matches(msg, inspectorKeys.ConsoleDown):
		m.consoleScrollOffset -= m.consoleStepSize()
		m.clampConsoleScroll()
		return nil
	case key.Matches(msg, inspectorKeys.ConsoleTop):
		m.consoleScrollOffset = m.maxConsoleScrollOffset()
		return nil
	case key.Matches(msg, inspectorKeys.ConsoleTail):
		m.consoleScrollOffset = 0
		return nil
	case key.Matches(msg, inspectorKeys.Open):
		selected, ok := m.selectedTask()
		if ok {
			return util.CmdHandler(InspectorOpenTaskMsg{SessionID: selected.Session.ID})
		}
	case key.Matches(msg, inspectorKeys.Stop):
		selected, ok := m.selectedTask()
		if ok {
			return util.CmdHandler(InspectorStopTaskMsg{SessionID: selected.Session.ID})
		}
	case key.Matches(msg, inspectorKeys.Exit):
		m.focused = false
		m.filterMode = false
		m.filter.Blur()
		m.artifactFilterMode = false
		m.artifactFilter.Blur()
	}
	m.clampConsoleScroll()
	return m.timelineSyncCmd(previous)
}

func (m *inspectorCmp) loadDataCmd() tea.Cmd {
	sessionID := m.session.ID
	if sessionID == "" {
		return nil
	}
	currentSession := m.session

	return func() tea.Msg {
		data := inspectorData{}
		data.activeSkills = m.app.Skills.Active(sessionID)

		if currentSession.ParentSessionID != "" {
			if parent, err := m.app.Sessions.Get(context.Background(), currentSession.ParentSessionID); err == nil {
				data.parentTitle = parent.Title
			}
		}

		data.current = loadCurrentRunSnapshot(context.Background(), m.app, currentSession)
		data.researchSessionID, data.researchInherited, data.researchTitle, data.research = loadResearchSnapshot(context.Background(), m.app, currentSession)

		data.tasks = loadTaskSnapshots(context.Background(), m.app, sessionID)
		data.timelineSessionID = initialTimelineTargetSessionID(currentSession, data.tasks)
		data.timeline = loadTimelineSnapshots(context.Background(), m.app.TaskRuns, data.timelineSessionID, consoleTimelineLimit)
		if modified, err := loadModifiedFileStats(context.Background(), m.app.History, sessionID); err == nil {
			data.modified = modified
		}

		return inspectorDataLoadedMsg{sessionID: sessionID, data: data}
	}
}

func (m *inspectorCmp) loadCurrentRunCmd() tea.Cmd {
	sessionID := m.session.ID
	currentSession := m.session
	if sessionID == "" {
		return nil
	}
	return func() tea.Msg {
		return inspectorDataLoadedMsg{
			sessionID: sessionID,
			data: inspectorData{
				parentTitle:       m.parentTitle,
				activeSkills:      m.app.Skills.Active(sessionID),
				current:           loadCurrentRunSnapshot(context.Background(), m.app, currentSession),
				research:          m.research,
				researchSessionID: m.researchSessionID,
				researchInherited: m.researchInherited,
				researchTitle:     m.researchTitle,
				tasks:             m.tasks,
				timelineSessionID: m.timelineSessionID,
				timeline:          m.timeline,
				modified:          m.modified,
			},
		}
	}
}

func (m *inspectorCmp) loadModifiedCmd() tea.Cmd {
	sessionID := m.session.ID
	if sessionID == "" {
		return nil
	}
	return func() tea.Msg {
		modified, err := loadModifiedFileStats(context.Background(), m.app.History, sessionID)
		if err != nil {
			return inspectorDataLoadedMsg{sessionID: sessionID, err: err}
		}
		return inspectorDataLoadedMsg{
			sessionID: sessionID,
			data: inspectorData{
				parentTitle:       m.parentTitle,
				activeSkills:      m.activeSkills,
				current:           m.current,
				research:          m.research,
				researchSessionID: m.researchSessionID,
				researchInherited: m.researchInherited,
				researchTitle:     m.researchTitle,
				tasks:             m.tasks,
				timelineSessionID: m.timelineSessionID,
				timeline:          m.timeline,
				modified:          modified,
			},
		}
	}
}

func (m *inspectorCmp) loadTimelineCmd(sessionID string) tea.Cmd {
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	return func() tea.Msg {
		return inspectorTimelineLoadedMsg{
			sessionID: sessionID,
			timeline:  loadTimelineSnapshots(context.Background(), m.app.TaskRuns, sessionID, consoleTimelineLimit),
		}
	}
}

func (m *inspectorCmp) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if msg.Action != tea.MouseActionPress {
		return nil
	}
	for _, action := range researchWorkbenchActions(m.research) {
		zoneID := inspectorActionZoneID(action.ID)
		if !zone.Get(zoneID).InBounds(msg) {
			continue
		}
		if !action.Enabled {
			reason := strings.TrimSpace(action.Reason)
			if reason == "" {
				reason = action.Label + " is not available right now"
			}
			return util.ReportWarn(reason)
		}
		if len(action.ArgNames) > 0 {
			return util.CmdHandler(WorkbenchOpenArgumentsMsg{
				CommandID: action.CommandID,
				Command:   action.Command,
				ArgNames:  action.ArgNames,
			})
		}
		return util.CmdHandler(WorkbenchRunCommandMsg{Command: action.Command})
	}
	for _, plan := range m.research.Experiments {
		if zone.Get(inspectorExperimentZoneID(plan.ID)).InBounds(msg) || zone.Get(inspectorLineageZoneID(plan.ID)).InBounds(msg) {
			return util.CmdHandler(WorkbenchRunCommandMsg{Command: "/experiment activate " + plan.ID})
		}
	}
	return nil
}

func (m *inspectorCmp) timelineTargetSessionID() string {
	if m.session.ID == "" {
		return ""
	}
	if m.session.ParentSessionID != "" {
		return m.session.ID
	}
	selected, ok := m.selectedTask()
	if !ok {
		return ""
	}
	return selected.Session.ID
}

func (m *inspectorCmp) timelineSyncCmd(previous string) tea.Cmd {
	current := m.timelineTargetSessionID()
	if current == "" || current == previous {
		return nil
	}
	m.consoleScrollOffset = 0
	return m.loadTimelineCmd(current)
}

func (m *inspectorCmp) renderCurrentSession(width int) string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	sessionType := "Chat Session"
	if m.session.ParentSessionID != "" {
		sessionType = "Delegated Task Session"
	}
	title := m.session.Title
	if strings.TrimSpace(title) == "" {
		title = "(untitled session)"
	}

	lines := []string{
		sectionTitle("Current Run", width),
		baseStyle.Width(width).Render(lipgloss.JoinHorizontal(lipgloss.Left, statusBadge(m.current.Status), " ", baseStyle.Bold(true).Render(title))),
		baseStyle.Width(width).Foreground(t.TextMuted()).Render(sessionType),
		baseStyle.Width(width).Foreground(t.TextMuted()).Render(fmt.Sprintf("Updated %s", m.current.UpdatedLabel)),
		baseStyle.Width(width).Foreground(t.TextMuted()).Render(fmt.Sprintf("Tokens %d  Cost $%.2f", m.session.PromptTokens+m.session.CompletionTokens, m.session.Cost)),
	}
	if m.parentTitle != "" {
		lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render("Parent: "+m.parentTitle))
	}
	if strings.TrimSpace(m.current.Detail) != "" {
		lines = append(lines, baseStyle.Width(width).Foreground(t.Text()).Render(m.current.Detail))
	}
	return baseStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m *inspectorCmp) renderResearchSection(width int) string {
	baseStyle := styles.BaseStyle()
	t := theme.CurrentTheme()

	lines := []string{sectionTitle("Research", width)}
	if !m.research.HasContent() {
		lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render("No research objective yet. Use /research set <objective>."))
		return baseStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	}

	lines = append(lines, baseStyle.Width(width).Render(truncateString(m.research.Objective, max(12, width))))

	meta := []string{fmt.Sprintf("Stage %s", strings.ToUpper(string(m.research.Stage)))}
	if strings.TrimSpace(m.research.Domain) != "" {
		meta = append(meta, "Domain "+m.research.Domain)
	}
	meta = append(meta, fmt.Sprintf("%d criteria", len(m.research.SuccessCriteria)))
	meta = append(meta, fmt.Sprintf("%d hypotheses", len(m.research.Hypotheses)))
	meta = append(meta, fmt.Sprintf("%d experiments", len(m.research.Experiments)))
	if strings.TrimSpace(m.research.PromotedExperimentID) != "" {
		meta = append(meta, "Promoted "+shortResearchID(m.research.PromotedExperimentID))
	}
	lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render(strings.Join(meta, "  ")))

	if m.researchInherited {
		source := "Inherited from parent chat"
		if strings.TrimSpace(m.researchTitle) != "" {
			source += ": " + m.researchTitle
		}
		lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render(source))
	}
	lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render("Updated "+formatInspectorTime(m.research.UpdatedAt)))

	maxCriteria := min(len(m.research.SuccessCriteria), 2)
	for i := 0; i < maxCriteria; i++ {
		lines = append(lines, baseStyle.Width(width).Render("- "+truncateString(m.research.SuccessCriteria[i], max(12, width-2))))
	}
	if len(m.research.SuccessCriteria) > maxCriteria {
		lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render(fmt.Sprintf("+%d more criteria", len(m.research.SuccessCriteria)-maxCriteria)))
	}

	return baseStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m *inspectorCmp) renderExperimentSection(width int) string {
	baseStyle := styles.BaseStyle()
	t := theme.CurrentTheme()

	lines := []string{sectionTitle("Experiments", width)}
	if len(m.research.Experiments) == 0 {
		lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render("No experiment plans yet. Use /experiment add <title>."))
		return baseStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	}

	maxItems := min(len(m.research.Experiments), 3)
	for i := 0; i < maxItems; i++ {
		plan := m.research.Experiments[i]
		statusLine := fmt.Sprintf("G%d  %s  %s", plan.Generation, strings.ToUpper(string(plan.Status)), shortResearchID(plan.ID))
		if plan.ID == m.research.ActiveExperimentID {
			statusLine += "  ACTIVE"
		}
		if plan.ID == m.research.PromotedExperimentID {
			statusLine += "  PROMOTED"
		}
		block := []string{
			baseStyle.Width(width).Render(statusLine),
			baseStyle.Width(width).Render(truncateString(plan.Title, max(12, width))),
		}
		if len(plan.Runs) > 0 {
			latestRun := plan.Runs[0]
			block = append(block, baseStyle.Width(width).Foreground(t.TextMuted()).Render(fmt.Sprintf("Runs %d  Latest %s  %s", len(plan.Runs), strings.ToUpper(string(latestRun.Status)), truncateString(latestRun.Title, max(10, width-24)))))
		} else {
			block = append(block, baseStyle.Width(width).Foreground(t.TextMuted()).Render("Runs 0"))
		}
		if strings.TrimSpace(plan.ParentExperimentID) != "" {
			root := shortResearchID(plan.LineageRootID)
			if root == "" {
				root = shortResearchID(plan.ParentExperimentID)
			}
			lineage := "From " + shortResearchID(plan.ParentExperimentID) + "  Root " + root
			if plan.EvolutionDecision != "" {
				lineage += "  " + strings.ToUpper(string(plan.EvolutionDecision))
			}
			block = append(block, baseStyle.Width(width).Foreground(t.TextMuted()).Render(lineage))
		}
		if strings.TrimSpace(plan.Rationale) != "" {
			block = append(block, baseStyle.Width(width).Foreground(t.TextMuted()).Render(truncateString(plan.Rationale, max(12, width))))
		}
		if plan.LatestEval != nil {
			block = append(block,
				baseStyle.Width(width).Foreground(t.TextMuted()).Render(fmt.Sprintf("Eval %.2f %s", plan.LatestEval.Score, strings.ToUpper(string(plan.LatestEval.Decision)))),
				baseStyle.Width(width).Foreground(t.TextMuted()).Render(truncateString(plan.LatestEval.Summary, max(12, width))),
			)
		}
		block = append(block, baseStyle.Width(width).Foreground(t.TextMuted()).Render("Click to activate"))
		lines = append(lines, zone.Mark(inspectorExperimentZoneID(plan.ID), lipgloss.JoinVertical(lipgloss.Left, block...)))
	}
	if len(m.research.Experiments) > maxItems {
		lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render(fmt.Sprintf("+%d more experiments", len(m.research.Experiments)-maxItems)))
	}
	return baseStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m *inspectorCmp) renderLineageSection(width int) string {
	baseStyle := styles.BaseStyle()
	t := theme.CurrentTheme()

	lines := []string{sectionTitle("Lineage", width)}
	groups := research.LineageGroups(m.research)
	if len(groups) == 0 {
		lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render("No lineage yet. Evaluate and evolve an experiment to start one."))
		return baseStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	}

	maxGroups := min(len(groups), 3)
	for i := 0; i < maxGroups; i++ {
		group := groups[i]
		header := "Root " + shortResearchID(group.RootID)
		if strings.TrimSpace(group.RootTitle) != "" {
			header += " " + truncateString(group.RootTitle, max(10, width-18))
		}
		headerFlags := make([]string, 0, 2)
		if group.RootID == m.research.PromotedExperimentID {
			headerFlags = append(headerFlags, "PROMOTED")
		}
		if lineageContainsExperiment(group, m.research.ActiveExperimentID) {
			headerFlags = append(headerFlags, "ACTIVE-LINEAGE")
		}
		if len(headerFlags) > 0 {
			header += " [" + strings.Join(headerFlags, ", ") + "]"
		}
		if group.BestScore >= 0 {
			header += fmt.Sprintf(" | best %.2f", group.BestScore)
		}
		header += fmt.Sprintf(" | %d generations", len(group.Plans))
		lines = append(lines, baseStyle.Width(width).Render(header))

		if best, ok := bestExperimentInLineage(group); ok && best.LatestEval != nil {
			bestLine := fmt.Sprintf("  Best %s  %.2f %s", shortResearchID(best.ID), best.LatestEval.Score, strings.ToUpper(string(best.LatestEval.Decision)))
			lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render(truncateString(bestLine, max(12, width))))
		}
		if current, ok := currentExperimentInLineage(m.research, group); ok {
			currentLine := fmt.Sprintf("  Current %s G%d %s", shortResearchID(current.ID), current.Generation, truncateString(current.Title, max(10, width-28)))
			flags := experimentFlags(m.research, current)
			if len(flags) > 0 {
				currentLine += " [" + strings.Join(flags, ", ") + "]"
			}
			lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render(truncateString(currentLine, max(12, width))))
		}
		statsLine := fmt.Sprintf("  Queue %d", queuedExperimentCount(group))
		lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render(statsLine))

		maxPlans := min(len(group.Plans), 3)
		for j := 0; j < maxPlans; j++ {
			plan := group.Plans[j]
			label := fmt.Sprintf("  %s G%d %s", shortResearchID(plan.ID), plan.Generation, truncateString(plan.Title, max(10, width-24)))
			flags := experimentFlags(m.research, plan)
			if len(flags) > 0 {
				label += " [" + strings.Join(flags, ", ") + "]"
			}
			details := []string{strings.ToUpper(string(plan.Status))}
			if plan.LatestEval != nil {
				details = append(details, fmt.Sprintf("score %.2f", plan.LatestEval.Score), strings.ToUpper(string(plan.LatestEval.Decision)))
			} else {
				details = append(details, "no evaluation")
			}
			details = append(details, fmt.Sprintf("runs %d", len(plan.Runs)))
			block := lipgloss.JoinVertical(
				lipgloss.Left,
				baseStyle.Width(width).Render(truncateString(label, max(12, width))),
				baseStyle.Width(width).Foreground(t.TextMuted()).Render(truncateString("    "+strings.Join(details, " | "), max(12, width))),
			)
			lines = append(lines, zone.Mark(inspectorLineageZoneID(plan.ID), block))
		}
		if len(group.Plans) > maxPlans {
			lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render(fmt.Sprintf("  +%d more generations", len(group.Plans)-maxPlans)))
		}
		lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render("  "+truncateString(lineageActionHint(m.research, group), max(12, width-2))))
	}
	if len(groups) > maxGroups {
		lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render(fmt.Sprintf("+%d more lineages", len(groups)-maxGroups)))
	}
	lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render("Use /experiment compare for the full lineage board."))
	return baseStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m *inspectorCmp) renderActionSection(width int) string {
	baseStyle := styles.BaseStyle()
	t := theme.CurrentTheme()

	lines := []string{
		sectionTitle("Actions", width),
		baseStyle.Width(width).Foreground(t.TextMuted()).Render("Click an action chip to drive the active experiment loop"),
	}

	actions := researchWorkbenchActions(m.research)
	chips := make([]string, 0, len(actions))
	for _, action := range actions {
		chips = append(chips, renderInspectorActionChip(action))
	}
	lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Left, chips...))

	for _, action := range actions {
		if action.Enabled || strings.TrimSpace(action.Reason) == "" {
			continue
		}
		lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render(
			truncateString(action.Label+": "+action.Reason, max(12, width)),
		))
	}
	return baseStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m *inspectorCmp) renderArtifactSection(width int) string {
	baseStyle := styles.BaseStyle()
	t := theme.CurrentTheme()

	lines := []string{sectionTitle("Artifacts", width)}
	if m.artifactFilterMode {
		lines = append(lines,
			baseStyle.Width(width).Foreground(t.TextMuted()).Render("Artifact filter [typing]"),
			baseStyle.Width(width).Render(m.artifactFilter.View()),
		)
	} else if strings.TrimSpace(m.artifactFilter.Value()) != "" {
		lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render("Artifact filter: "+m.artifactFilter.Value()))
	} else {
		lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render("Press A to search artifacts across the research session"))
	}

	items := research.ArtifactIndex(m.research)
	if len(items) == 0 {
		lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render("No captured artifacts yet"))
		return baseStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	}

	filtered := research.FilterArtifactIndex(items, m.artifactFilter.Value())
	if len(filtered) == 0 {
		lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render("No artifacts match the current filter"))
		return baseStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	}

	lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render(fmt.Sprintf("Showing %d of %d indexed artifacts", len(filtered), len(items))))
	maxItems := min(len(filtered), max(4, min(8, m.height/9)))
	for i := 0; i < maxItems; i++ {
		item := filtered[i]
		artifact := item.Artifact
		label := artifact.Label
		if strings.TrimSpace(label) == "" {
			label = string(artifact.Kind)
		}
		lines = append(lines,
			baseStyle.Width(width).Render(strings.ToUpper(string(artifact.Kind))+"  "+truncateString(label, max(10, width-14))),
		)
		contextLine := []string{shortResearchID(item.ExperimentID)}
		if strings.TrimSpace(item.RunTitle) != "" {
			contextLine = append(contextLine, truncateString(item.RunTitle, max(10, width/2)))
		}
		if item.Evaluation != nil && strings.TrimSpace(string(item.Evaluation.Decision)) != "" {
			contextLine = append(contextLine, strings.ToUpper(string(item.Evaluation.Decision)))
		}
		lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render(strings.Join(contextLine, "  ")))
		if strings.TrimSpace(artifact.Path) != "" {
			lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render(truncateString(artifact.Path, max(10, width))))
		}
		if strings.TrimSpace(artifact.Summary) != "" {
			lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render(truncateString(artifact.Summary, max(10, width))))
		}
	}
	if len(filtered) > maxItems {
		lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render(fmt.Sprintf("+%d more artifacts", len(filtered)-maxItems)))
	}
	return baseStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m *inspectorCmp) renderSkillSection(width int) string {
	baseStyle := styles.BaseStyle()
	t := theme.CurrentTheme()

	lines := []string{sectionTitle("Active Skills", width)}
	if len(m.activeSkills) == 0 {
		lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render("No active skills"))
		return baseStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	}

	maxItems := min(len(m.activeSkills), max(3, (m.height/12)))
	for i := 0; i < maxItems; i++ {
		lines = append(lines, baseStyle.Width(width).Render("- "+m.activeSkills[i].ID))
	}
	if len(m.activeSkills) > maxItems {
		lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render(fmt.Sprintf("+%d more", len(m.activeSkills)-maxItems)))
	}
	return baseStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m *inspectorCmp) renderTaskSection(width int) string {
	baseStyle := styles.BaseStyle()
	t := theme.CurrentTheme()
	summary := m.taskStatusSummary()
	title := "Delegated Tasks"
	if summary != "" {
		title += "  " + summary
	}
	lines := []string{sectionTitle(title, width)}

	filtered := m.filteredTasks()
	if len(filtered) == 0 {
		lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render("No matching child tasks"))
		return baseStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	}

	maxItems := max(2, min(len(filtered), max(2, (m.height-20)/4)))
	selectedID := ""
	if selected, ok := m.selectedTask(); ok {
		selectedID = selected.Session.ID
	}
	for i := 0; i < maxItems; i++ {
		task := filtered[i]
		title := task.Session.Title
		if strings.TrimSpace(title) == "" {
			title = "(untitled delegated task)"
		}

		lineStyle := baseStyle.Width(width)
		detailStyle := baseStyle.Width(width).Foreground(t.TextMuted())
		if task.Session.ID == selectedID {
			lineStyle = lineStyle.Foreground(t.Primary()).Bold(true)
			detailStyle = detailStyle.Foreground(t.Text())
		}

		lines = append(lines,
			lineStyle.Render(lipgloss.JoinHorizontal(lipgloss.Left, statusBadge(task.Status), " ", truncateString(title, max(10, width-12)))),
			detailStyle.Render(truncateString(task.Preview, max(10, width))),
			detailStyle.Render(fmt.Sprintf("%s  %s", truncateString(task.Detail, max(10, width-18)), task.UpdatedLabel)),
		)
	}
	if len(filtered) > maxItems {
		lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render(fmt.Sprintf("+%d more filtered tasks", len(filtered)-maxItems)))
	}
	return baseStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m *inspectorCmp) renderTaskConsole(width int) string {
	baseStyle := styles.BaseStyle()
	t := theme.CurrentTheme()
	lines := []string{sectionTitle("Run Console", width)}

	selected, ok := m.consoleRun()
	if !ok {
		lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render("Select a delegated task to inspect its live event timeline"))
		return baseStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	}

	title := selected.Session.Title
	if strings.TrimSpace(title) == "" {
		title = "(untitled delegated task)"
	}
	lines = append(lines,
		baseStyle.Width(width).Render(lipgloss.JoinHorizontal(lipgloss.Left, statusBadge(selected.Status), " ", truncateString(title, max(12, width-12)))),
		baseStyle.Width(width).Foreground(t.TextMuted()).Render(truncateString(selected.Preview, max(12, width))),
		baseStyle.Width(width).Foreground(t.TextMuted()).Render(fmt.Sprintf("Updated %s", selected.UpdatedLabel)),
	)
	if strings.TrimSpace(selected.Detail) != "" {
		lines = append(lines, baseStyle.Width(width).Render(truncateString(selected.Detail, max(12, width))))
	}

	lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render("Console filter: "+m.consoleFilterView()))
	searchLabel := "Console match"
	if m.consoleSearchMode {
		searchLabel = "Console match [typing]"
	}
	lines = append(lines,
		baseStyle.Width(width).Foreground(t.TextMuted()).Render(searchLabel),
		baseStyle.Width(width).Render(m.consoleSearch.View()),
	)

	consoleLines := m.consoleViewportLines(width, selected)
	if len(consoleLines) == 0 {
		lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render("No matching task events"))
		return baseStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	}

	for _, line := range consoleLines {
		lines = append(lines, line)
	}

	lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render(m.consoleViewportStatus()))

	return baseStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m *inspectorCmp) renderModifiedFiles(width int) string {
	baseStyle := styles.BaseStyle()
	t := theme.CurrentTheme()
	lines := []string{sectionTitle("Modified Files", width)}
	if len(m.modified) == 0 {
		lines = append(lines, baseStyle.Width(width).Foreground(t.TextMuted()).Render("No tracked file changes"))
		return baseStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	}

	maxItems := max(2, min(len(m.modified), max(2, (m.height-24)/4)))
	for i := 0; i < maxItems; i++ {
		file := m.modified[i]
		stats := []string{}
		if file.Additions > 0 {
			stats = append(stats, lipgloss.NewStyle().Foreground(t.Success()).Render(fmt.Sprintf("+%d", file.Additions)))
		}
		if file.Removals > 0 {
			stats = append(stats, lipgloss.NewStyle().Foreground(t.Error()).Render(fmt.Sprintf("-%d", file.Removals)))
		}
		lines = append(lines, baseStyle.Width(width).Render(lipgloss.JoinHorizontal(lipgloss.Left, truncateString(file.Path, max(10, width-10)), " ", strings.Join(stats, " "))))
	}
	return baseStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m *inspectorCmp) taskStatusSummary() string {
	if len(m.tasks) == 0 {
		return ""
	}
	counts := map[taskrun.Status]int{}
	for _, task := range m.tasks {
		counts[task.Status]++
	}
	parts := make([]string, 0, 4)
	if counts[taskrun.StatusRunning] > 0 {
		parts = append(parts, fmt.Sprintf("run %d", counts[taskrun.StatusRunning]))
	}
	if counts[taskrun.StatusQueued] > 0 {
		parts = append(parts, fmt.Sprintf("queued %d", counts[taskrun.StatusQueued]))
	}
	if counts[taskrun.StatusBlocked] > 0 {
		parts = append(parts, fmt.Sprintf("blocked %d", counts[taskrun.StatusBlocked]))
	}
	done := counts[taskrun.StatusComplete] + counts[taskrun.StatusCanceled] + counts[taskrun.StatusFailed]
	if done > 0 {
		parts = append(parts, fmt.Sprintf("done %d", done))
	}
	return strings.Join(parts, "  ")
}

func (m *inspectorCmp) filteredTimeline() []timelineSnapshot {
	query := strings.ToLower(strings.TrimSpace(m.consoleSearch.Value()))
	if m.consoleFilter == consoleFilterAll && query == "" {
		return append([]timelineSnapshot{}, m.timeline...)
	}
	out := make([]timelineSnapshot, 0, len(m.timeline))
	for _, item := range m.timeline {
		if !item.matchesConsoleFilter(m.consoleFilter) {
			continue
		}
		if query != "" && !item.matchesConsoleQuery(query) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func (m *inspectorCmp) consoleViewportLines(width int, selected runSnapshot) []string {
	baseStyle := styles.BaseStyle().Width(width)
	t := theme.CurrentTheme()
	filteredTimeline := m.filteredTimeline()
	if len(filteredTimeline) == 0 {
		return nil
	}

	allLines := make([]string, 0, len(filteredTimeline)*2)
	for _, item := range filteredTimeline {
		segments := []string{
			baseStyle.Foreground(t.TextMuted()).Render(item.CreatedLabel),
		}
		if elapsed := formatElapsedSince(selected.StartedAt, item.CreatedAt); elapsed != "" {
			segments = append(segments, baseStyle.Foreground(t.TextMuted()).Render(elapsed))
		}
		segments = append(segments, eventKindBadge(item.Kind))
		if strings.TrimSpace(item.ToolName) != "" {
			segments = append(segments, toolNameBadge(item.ToolName))
		}
		segments = append(segments, truncateString(item.DetailOrFallback(), max(10, width-28)))
		allLines = append(allLines, baseStyle.Render(lipgloss.JoinHorizontal(lipgloss.Left, segments...)))

		if metadataLine := item.MetadataLine(width); metadataLine != "" {
			allLines = append(allLines, baseStyle.Foreground(t.TextMuted()).Render(metadataLine))
		}
	}

	viewportHeight := m.consoleViewportHeight()
	if viewportHeight >= len(allLines) {
		return allLines
	}

	offset := m.clampedConsoleScrollOffset(len(allLines), viewportHeight)
	start := max(0, len(allLines)-viewportHeight-offset)
	end := min(len(allLines), start+viewportHeight)
	return allLines[start:end]
}

func (m *inspectorCmp) consoleViewportStatus() string {
	totalLines := m.consoleTotalLineCount()
	if totalLines == 0 {
		return "PgUp/PgDn scroll console  Home oldest  End live"
	}

	viewportHeight := m.consoleViewportHeight()
	maxOffset := max(0, totalLines-viewportHeight)
	offset := m.clampedConsoleScrollOffset(totalLines, viewportHeight)
	mode := "live"
	if offset > 0 {
		mode = fmt.Sprintf("history %d/%d", offset, maxOffset)
	}
	return fmt.Sprintf(
		"PgUp/PgDn scroll console  Home oldest  End live  Showing %d/%d lines  [%s]",
		min(viewportHeight, totalLines),
		totalLines,
		mode,
	)
}

func (m *inspectorCmp) consoleViewportHeight() int {
	return max(6, m.height-34)
}

func (m *inspectorCmp) consoleStepSize() int {
	return max(3, m.consoleViewportHeight()-2)
}

func (m *inspectorCmp) consoleTotalLineCount() int {
	filteredTimeline := m.filteredTimeline()
	total := 0
	for _, item := range filteredTimeline {
		total++
		if !item.Metadata.IsZero() {
			total++
		}
	}
	return total
}

func (m *inspectorCmp) maxConsoleScrollOffset() int {
	return max(0, m.consoleTotalLineCount()-m.consoleViewportHeight())
}

func (m *inspectorCmp) clampedConsoleScrollOffset(totalLines, viewportHeight int) int {
	maxOffset := max(0, totalLines-viewportHeight)
	return min(max(0, m.consoleScrollOffset), maxOffset)
}

func (m *inspectorCmp) clampConsoleScroll() {
	maxOffset := max(0, m.consoleTotalLineCount()-m.consoleViewportHeight())
	m.consoleScrollOffset = min(max(0, m.consoleScrollOffset), maxOffset)
}

func (m *inspectorCmp) consoleFilterView() string {
	filters := []consoleFilter{
		consoleFilterAll,
		consoleFilterLifecycle,
		consoleFilterTools,
		consoleFilterProgress,
	}
	t := theme.CurrentTheme()
	parts := make([]string, 0, len(filters))
	for _, filter := range filters {
		style := lipgloss.NewStyle().
			MarginRight(1).
			Foreground(t.TextMuted())
		if filter == m.consoleFilter {
			style = style.Foreground(t.Primary()).Bold(true)
		}
		parts = append(parts, style.Render("["+strings.ToUpper(string(filter))+"]"))
	}
	return strings.Join(parts, " ")
}

func (m *inspectorCmp) upsertTaskSnapshot(snapshot runSnapshot) {
	if snapshot.Session.ID == "" {
		return
	}
	for i, task := range m.tasks {
		if task.Session.ID == snapshot.Session.ID {
			m.tasks[i] = snapshot
			m.sortTasks()
			m.clampSelection()
			return
		}
	}
	m.tasks = append(m.tasks, snapshot)
	m.sortTasks()
	m.clampSelection()
}

func (m *inspectorCmp) sortTasks() {
	sort.Slice(m.tasks, func(i, j int) bool {
		if m.tasks[i].UpdatedAt != m.tasks[j].UpdatedAt {
			return m.tasks[i].UpdatedAt > m.tasks[j].UpdatedAt
		}
		return m.tasks[i].Session.ID < m.tasks[j].Session.ID
	})
}

func (m *inspectorCmp) filteredTasks() []runSnapshot {
	filter := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	if filter == "" {
		return append([]runSnapshot{}, m.tasks...)
	}

	out := make([]runSnapshot, 0, len(m.tasks))
	for _, task := range m.tasks {
		content := strings.ToLower(strings.Join([]string{
			task.Session.Title,
			task.Preview,
			task.Detail,
			string(task.Status),
		}, "\n"))
		match := true
		for _, token := range strings.Fields(filter) {
			if !strings.Contains(content, token) {
				match = false
				break
			}
		}
		if match {
			out = append(out, task)
		}
	}
	return out
}

func (m *inspectorCmp) selectedTask() (runSnapshot, bool) {
	filtered := m.filteredTasks()
	if len(filtered) == 0 {
		return runSnapshot{}, false
	}
	m.clampSelection()
	return filtered[m.selected], true
}

func (m *inspectorCmp) clampSelection() {
	filtered := m.filteredTasks()
	if len(filtered) == 0 {
		m.selected = 0
		return
	}
	if m.selected < 0 {
		m.selected = 0
	}
	if m.selected >= len(filtered) {
		m.selected = len(filtered) - 1
	}
}

func (m *inspectorCmp) consoleRun() (runSnapshot, bool) {
	target := m.timelineTargetSessionID()
	if target == "" {
		return runSnapshot{}, false
	}
	if target == m.session.ID && m.session.ParentSessionID != "" {
		return m.current, true
	}
	for _, task := range m.tasks {
		if task.Session.ID == target {
			return task, true
		}
	}
	return runSnapshot{}, false
}

func nextConsoleFilter(current consoleFilter, delta int) consoleFilter {
	filters := []consoleFilter{
		consoleFilterAll,
		consoleFilterLifecycle,
		consoleFilterTools,
		consoleFilterProgress,
	}
	index := 0
	for i, filter := range filters {
		if filter == current {
			index = i
			break
		}
	}
	index += delta
	if index < 0 {
		index = len(filters) - 1
	}
	if index >= len(filters) {
		index = 0
	}
	return filters[index]
}

func sectionTitle(title string, width int) string {
	t := theme.CurrentTheme()
	return styles.BaseStyle().
		Width(width).
		Foreground(t.Primary()).
		Bold(true).
		Render(title)
}

func statusBadge(status taskrun.Status) string {
	t := theme.CurrentTheme()
	fg := t.TextMuted()

	switch status {
	case taskrun.StatusRunning:
		fg = t.Primary()
	case taskrun.StatusComplete:
		fg = t.Success()
	case taskrun.StatusBlocked:
		fg = t.Warning()
	case taskrun.StatusFailed, taskrun.StatusCanceled:
		fg = t.Error()
	case taskrun.StatusQueued:
		fg = t.Secondary()
	}

	return lipgloss.NewStyle().
		Foreground(fg).
		Bold(true).
		Render("[" + strings.ToUpper(string(status)) + "]")
}

func eventKindBadge(kind taskrun.EventKind) string {
	t := theme.CurrentTheme()
	fg := t.TextMuted()

	switch kind {
	case taskrun.EventStarted:
		fg = t.Primary()
	case taskrun.EventProgress:
		fg = t.Secondary()
	case taskrun.EventFinished:
		fg = t.Success()
	case taskrun.EventCancelRequested:
		fg = t.Error()
	}

	label := strings.ToUpper(strings.ReplaceAll(string(kind), "_", " "))
	return lipgloss.NewStyle().
		Foreground(fg).
		Bold(true).
		Render("[" + label + "]")
}

func toolNameBadge(name string) string {
	t := theme.CurrentTheme()
	return lipgloss.NewStyle().
		Foreground(t.Secondary()).
		Render("[" + strings.ToUpper(truncateString(strings.TrimSpace(name), 18)) + "]")
}

func renderInspectorActionChip(action researchWorkbenchAction) string {
	t := theme.CurrentTheme()
	style := lipgloss.NewStyle().
		MarginRight(1).
		Foreground(t.TextMuted())

	if action.Enabled {
		style = style.Foreground(t.Primary()).Bold(true)
	}

	return zone.Mark(inspectorActionZoneID(action.ID), style.Render("["+action.Label+"]"))
}

func inspectorActionZoneID(actionID string) string {
	return "inspector-action-" + strings.TrimSpace(actionID)
}

func inspectorExperimentZoneID(experimentID string) string {
	return "inspector-experiment-" + strings.TrimSpace(experimentID)
}

func inspectorLineageZoneID(experimentID string) string {
	return "inspector-lineage-" + strings.TrimSpace(experimentID)
}

func buildRunSnapshot(sess session.Session, messages []message.Message) runSnapshot {
	status, detail := deriveRunStatus(messages)
	updatedAt := sess.UpdatedAt
	for _, msg := range messages {
		if msg.UpdatedAt > updatedAt {
			updatedAt = msg.UpdatedAt
		}
		if msg.CreatedAt > updatedAt {
			updatedAt = msg.CreatedAt
		}
	}
	return runSnapshot{
		Session:      sess,
		Status:       status,
		Detail:       detail,
		Preview:      summarizeRunPreview(messages),
		UpdatedAt:    updatedAt,
		UpdatedLabel: formatInspectorTime(updatedAt),
	}
}

func deriveRunStatus(messages []message.Message) (taskrun.Status, string) {
	if len(messages) == 0 {
		return taskrun.StatusEmpty, "No messages yet"
	}

	var latestAssistant *message.Message
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == message.Assistant {
			latestAssistant = &messages[i]
			break
		}
	}
	if latestAssistant == nil {
		return taskrun.StatusQueued, "Waiting for first model response"
	}
	if !latestAssistant.IsFinished() {
		if latestAssistant.Content().Text != "" {
			return taskrun.StatusRunning, "Generating response"
		}
		if latestAssistant.ReasoningContent().Thinking != "" {
			return taskrun.StatusRunning, "Reasoning in progress"
		}
		return taskrun.StatusRunning, "Running"
	}

	switch latestAssistant.FinishReason() {
	case message.FinishReasonEndTurn:
		return taskrun.StatusComplete, "Finished successfully"
	case message.FinishReasonPermissionDenied:
		return taskrun.StatusBlocked, "Waiting on permission"
	case message.FinishReasonCanceled:
		return taskrun.StatusCanceled, "Canceled"
	case message.FinishReasonError:
		return taskrun.StatusFailed, "Failed"
	case message.FinishReasonToolUse:
		return taskrun.StatusRunning, "Executing tools"
	case message.FinishReasonMaxTokens:
		return taskrun.StatusFailed, "Stopped at max tokens"
	default:
		return taskrun.StatusIdle, "Idle"
	}
}

func summarizeRunPreview(messages []message.Message) string {
	for _, msg := range messages {
		if msg.Role != message.User {
			continue
		}
		text := strings.TrimSpace(strings.ReplaceAll(msg.Content().Text, "\n", " "))
		if text != "" {
			return truncateString(text, 120)
		}
	}
	return "No prompt captured"
}

func formatInspectorTime(unixSeconds int64) string {
	if unixSeconds == 0 {
		return "just now"
	}
	return time.Unix(unixSeconds, 0).Format("2006-01-02 15:04")
}

func loadModifiedFileStats(ctx context.Context, historySvc history.Service, sessionID string) ([]modifiedFileStat, error) {
	if historySvc == nil || sessionID == "" {
		return nil, nil
	}

	latestFiles, err := historySvc.ListLatestSessionFiles(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	allFiles, err := historySvc.ListBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	stats := make([]modifiedFileStat, 0)
	for _, file := range latestFiles {
		if file.Version == history.InitialVersion {
			continue
		}

		var initialVersion history.File
		for _, v := range allFiles {
			if v.Path == file.Path && v.Version == history.InitialVersion {
				initialVersion = v
				break
			}
		}
		if initialVersion.ID == "" || initialVersion.Content == file.Content {
			continue
		}

		_, additions, removals := diff.GenerateDiff(initialVersion.Content, file.Content, file.Path)
		if additions == 0 && removals == 0 {
			continue
		}
		displayPath := strings.TrimPrefix(file.Path, config.WorkingDirectory())
		displayPath = strings.TrimPrefix(displayPath, "/")
		stats = append(stats, modifiedFileStat{
			Path:      displayPath,
			Additions: additions,
			Removals:  removals,
		})
	}

	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Path < stats[j].Path
	})
	return stats, nil
}

func runSnapshotFromTaskRun(run taskrun.Run) runSnapshot {
	return runSnapshot{
		Session: session.Session{
			ID:              run.SessionID,
			ParentSessionID: run.ParentSessionID,
			Title:           run.Title,
		},
		Status:       run.Status,
		Detail:       run.Detail,
		Preview:      truncateString(run.Prompt, 120),
		StartedAt:    run.StartedAt,
		FinishedAt:   run.FinishedAt,
		UpdatedAt:    run.UpdatedAt,
		UpdatedLabel: formatInspectorTime(run.UpdatedAt),
	}
}

func mergeCurrentRunSnapshot(existing runSnapshot, sess session.Session, run taskrun.Run) runSnapshot {
	snapshot := runSnapshotFromTaskRun(run)
	snapshot.Session = sess
	if snapshot.Preview == "" {
		snapshot.Preview = existing.Preview
	}
	return snapshot
}

func loadCurrentRunSnapshot(ctx context.Context, app *app.App, sess session.Session) runSnapshot {
	if run, ok := app.TaskRuns.Get(sess.ID); ok {
		return mergeCurrentRunSnapshot(runSnapshot{}, sess, run)
	}
	currentMessages, err := app.Messages.List(ctx, sess.ID)
	if err != nil {
		return runSnapshot{Session: sess}
	}
	return buildRunSnapshot(sess, currentMessages)
}

func loadTaskSnapshots(ctx context.Context, app *app.App, parentSessionID string) []runSnapshot {
	taskMap := map[string]runSnapshot{}

	realTime := app.TaskRuns.Snapshot(parentSessionID)
	for _, run := range realTime {
		taskMap[run.SessionID] = runSnapshotFromTaskRun(run)
	}

	tasks, err := app.Sessions.ListChildren(ctx, parentSessionID)
	if err != nil {
		return valuesSorted(taskMap)
	}
	for _, task := range tasks {
		if existing, ok := taskMap[task.ID]; ok {
			existing.Session = task
			taskMap[task.ID] = existing
			continue
		}

		taskMessages, taskErr := app.Messages.List(ctx, task.ID)
		if taskErr != nil {
			taskMap[task.ID] = runSnapshot{
				Session:      task,
				Status:       taskrun.StatusQueued,
				Detail:       "Loading task messages failed",
				UpdatedAt:    task.UpdatedAt,
				UpdatedLabel: formatInspectorTime(task.UpdatedAt),
			}
			continue
		}
		taskMap[task.ID] = buildRunSnapshot(task, taskMessages)
	}

	return valuesSorted(taskMap)
}

func initialTimelineTargetSessionID(current session.Session, tasks []runSnapshot) string {
	if current.ParentSessionID != "" {
		return current.ID
	}
	if len(tasks) == 0 {
		return ""
	}
	return tasks[0].Session.ID
}

func loadResearchSnapshot(ctx context.Context, app *app.App, current session.Session) (string, bool, string, research.SessionState) {
	if app == nil || app.Research == nil || current.ID == "" {
		return "", false, "", research.SessionState{}
	}

	researchSession, err := resolveResearchSession(ctx, app.Sessions, current)
	if err != nil {
		return "", false, "", research.SessionState{}
	}

	state, err := app.Research.Get(ctx, researchSession.ID)
	if err != nil {
		return researchSession.ID, researchSession.ID != current.ID, researchSession.Title, research.SessionState{SessionID: researchSession.ID, Stage: research.StageObjective}
	}
	return researchSession.ID, researchSession.ID != current.ID, researchSession.Title, state
}

func shortResearchID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

func activeExperiment(state research.SessionState) (research.ExperimentPlan, bool) {
	if strings.TrimSpace(state.ActiveExperimentID) != "" {
		for _, plan := range state.Experiments {
			if plan.ID == state.ActiveExperimentID {
				return plan, true
			}
		}
	}
	if len(state.Experiments) == 0 {
		return research.ExperimentPlan{}, false
	}
	return state.Experiments[0], true
}

func resolveResearchSession(ctx context.Context, sessions session.Service, current session.Session) (session.Session, error) {
	for current.ParentSessionID != "" {
		parent, err := sessions.Get(ctx, current.ParentSessionID)
		if err != nil {
			return session.Session{}, err
		}
		current = parent
	}
	return current, nil
}

func loadTimelineSnapshots(_ context.Context, svc taskrun.Service, sessionID string, limit int) []timelineSnapshot {
	if svc == nil || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	events := svc.Timeline(sessionID, limit)
	out := make([]timelineSnapshot, 0, len(events))
	for _, event := range events {
		out = append(out, timelineSnapshotFromEvent(event))
	}
	return out
}

func timelineSnapshotFromEvent(event taskrun.Event) timelineSnapshot {
	return timelineSnapshot{
		ID:           event.ID,
		SessionID:    event.SessionID,
		Status:       event.Status,
		Kind:         event.Kind,
		ToolName:     event.ToolName,
		Metadata:     event.Metadata,
		Detail:       event.Detail,
		CreatedAt:    event.CreatedAt,
		CreatedLabel: time.Unix(event.CreatedAt, 0).Format("15:04:05"),
	}
}

func appendTimelineSnapshot(items []timelineSnapshot, event timelineSnapshot) []timelineSnapshot {
	items = append(items, event)
	if len(items) <= consoleTimelineLimit {
		return items
	}
	return append([]timelineSnapshot{}, items[len(items)-consoleTimelineLimit:]...)
}

func (t timelineSnapshot) DetailOrFallback() string {
	if strings.TrimSpace(t.Detail) != "" {
		return t.Detail
	}
	return string(t.Status)
}

func (t timelineSnapshot) matchesConsoleFilter(filter consoleFilter) bool {
	switch filter {
	case consoleFilterLifecycle:
		return t.Kind == taskrun.EventQueued || t.Kind == taskrun.EventStarted || t.Kind == taskrun.EventFinished || t.Kind == taskrun.EventCancelRequested
	case consoleFilterTools:
		return strings.HasPrefix(strings.ToLower(strings.TrimSpace(t.Detail)), "running tool ")
	case consoleFilterProgress:
		return t.Kind == taskrun.EventProgress && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(t.Detail)), "running tool ")
	default:
		return true
	}
}

func (t timelineSnapshot) matchesConsoleQuery(query string) bool {
	if query == "" {
		return true
	}
	content := strings.ToLower(strings.Join([]string{
		t.ToolName,
		t.Detail,
		t.Metadata.ToolInputPreview,
		t.Metadata.FinishReason,
		t.Metadata.PermissionReason,
		string(t.Kind),
		string(t.Status),
	}, "\n"))
	for _, token := range strings.Fields(query) {
		if !strings.Contains(content, token) {
			return false
		}
	}
	return true
}

func (t timelineSnapshot) MetadataLine(width int) string {
	parts := make([]string, 0, 3)
	if strings.TrimSpace(t.Metadata.ToolInputPreview) != "" {
		parts = append(parts, "input "+truncateString(t.Metadata.ToolInputPreview, max(12, width-10)))
	}
	if strings.TrimSpace(t.Metadata.FinishReason) != "" {
		parts = append(parts, "finish "+t.Metadata.FinishReason)
	}
	if strings.TrimSpace(t.Metadata.PermissionReason) != "" {
		parts = append(parts, truncateString(t.Metadata.PermissionReason, max(12, width-10)))
	}
	if len(parts) == 0 {
		return ""
	}
	return "  " + strings.Join(parts, "  |  ")
}

func formatElapsedSince(startedAt, createdAt int64) string {
	if startedAt <= 0 || createdAt <= 0 || createdAt < startedAt {
		return ""
	}
	delta := createdAt - startedAt
	minutes := delta / 60
	seconds := delta % 60
	return fmt.Sprintf("+%02d:%02d", minutes, seconds)
}

func valuesSorted(taskMap map[string]runSnapshot) []runSnapshot {
	out := make([]runSnapshot, 0, len(taskMap))
	for _, task := range taskMap {
		out = append(out, task)
	}
	slices.SortFunc(out, func(a, b runSnapshot) int {
		if a.UpdatedAt == b.UpdatedAt {
			return strings.Compare(a.Session.ID, b.Session.ID)
		}
		if a.UpdatedAt > b.UpdatedAt {
			return -1
		}
		return 1
	})
	return out
}
