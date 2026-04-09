package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/SciMate-AI/scicli/internal/app"
	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/SciMate-AI/scicli/internal/pubsub"
	"github.com/SciMate-AI/scicli/internal/research"
	"github.com/SciMate-AI/scicli/internal/session"
	"github.com/SciMate-AI/scicli/internal/tui/styles"
	"github.com/SciMate-AI/scicli/internal/tui/theme"
	"github.com/SciMate-AI/scicli/internal/tui/util"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
)

type workbenchNavLoadedMsg struct {
	sessionID         string
	researchSessionID string
	researchInherited bool
	researchTitle     string
	researchState     research.SessionState
}

type workbenchNavCmp struct {
	app               *app.App
	width             int
	height            int
	session           session.Session
	research          research.SessionState
	researchSessionID string
	researchInherited bool
	researchTitle     string
}

func (m *workbenchNavCmp) Init() tea.Cmd {
	return m.loadResearchCmd()
}

func (m *workbenchNavCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.MouseMsg:
		return m, m.handleMouse(msg)
	case SessionSelectedMsg:
		m.session = msg
		m.research = research.SessionState{}
		m.researchSessionID = ""
		m.researchInherited = false
		m.researchTitle = ""
		return m, m.loadResearchCmd()
	case SessionClearedMsg:
		m.session = session.Session{}
		m.research = research.SessionState{}
		m.researchSessionID = ""
		m.researchInherited = false
		m.researchTitle = ""
	case pubsub.Event[session.Session]:
		if msg.Type == pubsub.UpdatedEvent && msg.Payload.ID == m.session.ID {
			m.session = msg.Payload
			return m, m.loadResearchCmd()
		}
	case pubsub.Event[research.SessionState]:
		if msg.Payload.SessionID == m.researchSessionID {
			m.research = msg.Payload
		}
	case workbenchNavLoadedMsg:
		if msg.sessionID != m.session.ID {
			return m, nil
		}
		m.research = msg.researchState
		m.researchSessionID = msg.researchSessionID
		m.researchInherited = msg.researchInherited
		m.researchTitle = msg.researchTitle
	}
	return m, nil
}

func (m *workbenchNavCmp) View() string {
	baseStyle := styles.BaseStyle()
	t := theme.CurrentTheme()
	width := max(1, m.width)

	sessionTitle := "(new session)"
	if strings.TrimSpace(m.session.Title) != "" {
		sessionTitle = m.session.Title
	}
	sessionType := "chat"
	if m.session.ParentSessionID != "" {
		sessionType = "delegated-task"
	}
	activeSkills := len(m.app.Skills.Active(m.session.ID))
	nextAction := nextResearchAction(m.research)
	header := lipgloss.JoinHorizontal(
		lipgloss.Left,
		consoleBadge("command deck", t.Primary(), t.Background()),
		" ",
		baseStyle.Bold(true).Render("Scientific Workbench"),
	)

	sections := []string{header}
	sections = append(sections, consoleSection(width, "Workspace",
		logo(width),
		consoleMuted("Mode: "+string(config.Get().Automation.WorkMode)),
		consoleMuted("Repo: https://github.com/SciMate-AI/scicli"),
		consoleMuted("CWD: "+config.WorkingDirectory()),
	))
	sections = append(sections, consoleSection(width, "Session",
		baseStyle.Bold(true).Render(truncateString(sessionTitle, max(12, width-4))),
		lipgloss.JoinHorizontal(
			lipgloss.Left,
			consoleBadge(sessionType, t.Secondary(), t.Background()),
			" ",
			consoleBadge(fmt.Sprintf("%d skills", activeSkills), t.BackgroundDarker(), t.Text()),
		),
		consoleMuted("Ctrl+N new session  Ctrl+K palette  /help command guide"),
	))

	if !m.research.HasContent() {
		sections = append(sections, consoleSection(width, "Research Loop",
			consoleMuted("No research objective recorded yet."),
			baseStyle.Render("/research set <objective>"),
			consoleMuted("This becomes the anchor for experiments, evaluation and provenance."),
		))
	} else {
		meta := []string{
			"stage " + strings.ToUpper(string(m.research.Stage)),
			fmt.Sprintf("%d experiments", len(m.research.Experiments)),
			fmt.Sprintf("%d criteria", len(m.research.SuccessCriteria)),
		}
		if strings.TrimSpace(m.research.PromotedExperimentID) != "" {
			meta = append(meta, "best "+shortResearchID(m.research.PromotedExperimentID))
		}
		body := []string{
			baseStyle.Render(truncateString(m.research.Objective, max(12, width*3))),
			consoleMuted(strings.Join(meta, "  |  ")),
		}
		if m.researchInherited {
			source := "Inherited from parent chat"
			if strings.TrimSpace(m.researchTitle) != "" {
				source += ": " + m.researchTitle
			}
			body = append(body, consoleMuted(truncateString(source, max(12, width*2))))
		}
		body = append(body,
			consoleDivider(width-4, "next"),
			baseStyle.Bold(true).Render(truncateString(nextAction, max(12, width-4))),
		)
		sections = append(sections, consoleSection(width, "Research Loop", body...))
	}

	if plan, ok := activeExperiment(m.research); ok {
		sections = append(sections, zone.Mark(workbenchNavExperimentZoneID(plan.ID), m.renderExperimentCard(width, "Active Candidate", plan, true)))
	}
	if plan, ok := promotedExperiment(m.research); ok && plan.ID != m.research.ActiveExperimentID {
		sections = append(sections, zone.Mark(workbenchNavExperimentZoneID(plan.ID), m.renderExperimentCard(width, "Promoted Candidate", plan, false)))
	}

	queue := queuedExperiments(m.research)
	queueBody := []string{}
	if len(queue) == 0 {
		queueBody = append(queueBody, consoleMuted("No queued follow-up candidates"))
	} else {
		maxItems := min(len(queue), 4)
		for i := 0; i < maxItems; i++ {
			plan := queue[i]
			label := shortResearchID(plan.ID) + fmt.Sprintf("  G%d  ", plan.Generation) + truncateString(plan.Title, max(10, width-18))
			detail := strings.ToUpper(string(plan.Status))
			if plan.LatestEval != nil {
				detail += fmt.Sprintf("  |  %.2f %s", plan.LatestEval.Score, strings.ToUpper(string(plan.LatestEval.Decision)))
			}
			queueBody = append(queueBody, zone.Mark(workbenchNavExperimentZoneID(plan.ID), lipgloss.JoinVertical(
				lipgloss.Left,
				baseStyle.Bold(true).Render(label),
				consoleMuted(detail),
			)))
		}
		if len(queue) > maxItems {
			queueBody = append(queueBody, consoleMuted(fmt.Sprintf("+%d more queued candidates", len(queue)-maxItems)))
		}
	}
	sections = append(sections, consoleSection(width, "Queue", queueBody...))

	quickActions := []researchWorkbenchAction{
		{
			ID:      "command-palette",
			Label:   "Palette",
			Enabled: false,
			Reason:  "Use Ctrl+K to open the full command palette.",
		},
		{
			ID:      "next",
			Label:   "Next",
			Command: nextAction,
			Enabled: strings.HasPrefix(strings.TrimSpace(nextAction), "/"),
			Reason:  "Run the next action from chat when it is not a slash command.",
		},
		{
			ID:      "compare",
			Label:   "Compare",
			Command: "/experiment compare",
			Enabled: len(m.research.Experiments) >= 2,
			Reason:  "Need at least two experiments to compare.",
		},
		{
			ID:      "artifacts",
			Label:   "Artifacts",
			Command: "/artifact list",
			Enabled: len(research.ArtifactIndex(m.research)) > 0,
			Reason:  "No captured artifacts yet.",
		},
		{
			ID:      "tasks",
			Label:   "Tasks",
			Command: "/tasks",
			Enabled: true,
		},
		{
			ID:      "parent",
			Label:   "Parent",
			Command: "/parent",
			Enabled: m.session.ParentSessionID != "",
			Reason:  "Current session has no parent.",
		},
	}
	actionCards := make([]string, 0, len(quickActions))
	for _, action := range quickActions {
		actionCards = append(actionCards, renderWorkbenchNavActionChip(action, width))
	}
	sections = append(sections, consoleSection(width, "Quick Actions", actionCards...))
	sections = append(sections, consoleMuted("Click experiments to activate them. Click an action card to run it."))

	return baseStyle.
		Width(width).
		Height(max(1, m.height)).
		Render(lipgloss.JoinVertical(lipgloss.Left, sections...))
}

func (m *workbenchNavCmp) SetSize(width, height int) tea.Cmd {
	m.width = width
	m.height = height
	return nil
}

func (m *workbenchNavCmp) GetSize() (int, int) {
	return m.width, m.height
}

func (m *workbenchNavCmp) loadResearchCmd() tea.Cmd {
	sessionID := m.session.ID
	currentSession := m.session
	if sessionID == "" {
		return nil
	}
	return func() tea.Msg {
		researchSessionID, inherited, title, state := loadResearchSnapshot(context.Background(), m.app, currentSession)
		return workbenchNavLoadedMsg{
			sessionID:         sessionID,
			researchSessionID: researchSessionID,
			researchInherited: inherited,
			researchTitle:     title,
			researchState:     state,
		}
	}
}

func (m *workbenchNavCmp) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if msg.Action != tea.MouseActionPress {
		return nil
	}
	for _, plan := range m.research.Experiments {
		if zone.Get(workbenchNavExperimentZoneID(plan.ID)).InBounds(msg) {
			return util.CmdHandler(WorkbenchRunCommandMsg{Command: "/experiment activate " + plan.ID})
		}
	}

	actions := []researchWorkbenchAction{
		{ID: "command-palette", Command: "", Enabled: false, Reason: "Use Ctrl+K to open the full command palette."},
		{ID: "next", Command: nextResearchAction(m.research), Enabled: strings.HasPrefix(strings.TrimSpace(nextResearchAction(m.research)), "/"), Reason: "Run the next action from chat when it is not a slash command."},
		{ID: "compare", Command: "/experiment compare", Enabled: len(m.research.Experiments) >= 2, Reason: "Need at least two experiments to compare."},
		{ID: "artifacts", Command: "/artifact list", Enabled: len(research.ArtifactIndex(m.research)) > 0, Reason: "No captured artifacts yet."},
		{ID: "tasks", Command: "/tasks", Enabled: true},
		{ID: "parent", Command: "/parent", Enabled: m.session.ParentSessionID != "", Reason: "Current session has no parent."},
	}
	for _, action := range actions {
		if !zone.Get(workbenchNavActionZoneID(action.ID)).InBounds(msg) {
			continue
		}
		if !action.Enabled {
			reason := strings.TrimSpace(action.Reason)
			if reason == "" {
				reason = "Action is not available right now"
			}
			return util.ReportWarn(reason)
		}
		return util.CmdHandler(WorkbenchRunCommandMsg{Command: action.Command})
	}
	return nil
}

func (m *workbenchNavCmp) renderExperimentCard(width int, title string, plan research.ExperimentPlan, showNextAction bool) string {
	baseStyle := styles.BaseStyle()
	t := theme.CurrentTheme()

	header := lipgloss.JoinHorizontal(
		lipgloss.Left,
		consoleBadge(title, t.Primary(), t.Background()),
		" ",
		baseStyle.Bold(true).Render(truncateString(plan.Title, max(12, width-20))),
	)

	lines := []string{header}

	meta := []string{
		shortResearchID(plan.ID),
		fmt.Sprintf("G%d", plan.Generation),
		strings.ToUpper(string(plan.Status)),
	}
	flags := experimentFlags(m.research, plan)
	if len(flags) > 0 {
		meta = append(meta, strings.Join(flags, "/"))
	}
	lines = append(lines, consoleMuted(truncateString(strings.Join(meta, "  |  "), max(12, width*2))))

	if plan.LatestEval != nil {
		lines = append(lines, consoleMuted(
			truncateString(fmt.Sprintf("score %.2f  |  %s", plan.LatestEval.Score, strings.ToUpper(string(plan.LatestEval.Decision))), max(12, width*2)),
		))
	}
	if len(plan.Runs) > 0 {
		lines = append(lines, consoleMuted(
			truncateString(fmt.Sprintf("runs %d  |  latest %s", len(plan.Runs), plan.Runs[0].Title), max(12, width*2)),
		))
	}
	if showNextAction {
		lines = append(lines,
			consoleDivider(width-4, "next"),
			baseStyle.Bold(true).Render(truncateString(nextResearchAction(m.research), max(12, width-4))),
		)
	}
	lines = append(lines, consoleMuted("Click to activate"))
	return consoleSection(width, "", lines...)
}

func renderWorkbenchNavActionChip(action researchWorkbenchAction, width int) string {
	t := theme.CurrentTheme()
	labelStyle := lipgloss.NewStyle()
	if action.Enabled {
		labelStyle = labelStyle.Foreground(t.Secondary()).Bold(true)
	} else {
		labelStyle = labelStyle.Foreground(t.TextMuted())
	}
	label := labelStyle.Render("[" + action.Label + "]")
	detail := action.Reason
	if action.Enabled {
		detail = action.Command
	}
	card := consoleSection(width, "", label, consoleMuted(truncateString(detail, max(12, width*2))))
	return zone.Mark(workbenchNavActionZoneID(action.ID), card)
}

func workbenchNavExperimentZoneID(experimentID string) string {
	return "workbench-nav-experiment-" + strings.TrimSpace(experimentID)
}

func workbenchNavActionZoneID(actionID string) string {
	return "workbench-nav-action-" + strings.TrimSpace(actionID)
}

func NewWorkbenchNavCmp(app *app.App) tea.Model {
	return &workbenchNavCmp{app: app}
}
