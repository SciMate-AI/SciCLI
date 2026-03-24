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

	title := baseStyle.
		Foreground(t.Primary()).
		Bold(true).
		Render("Workbench")

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

	sections := []string{
		title,
		repo(width),
		cwd(width),
		"",
		baseStyle.Foreground(t.Primary()).Bold(true).Render("Context"),
		baseStyle.Width(width).Render(truncateString(sessionTitle, width)),
		baseStyle.Foreground(t.TextMuted()).Width(width).Render(fmt.Sprintf("type: %s", sessionType)),
		baseStyle.Foreground(t.TextMuted()).Width(width).Render(fmt.Sprintf("mode: %s", config.Get().Automation.WorkMode)),
		baseStyle.Foreground(t.TextMuted()).Width(width).Render(fmt.Sprintf("active skills: %d", activeSkills)),
		"",
		baseStyle.Foreground(t.Primary()).Bold(true).Render("Research Loop"),
	}

	if !m.research.HasContent() {
		sections = append(sections,
			baseStyle.Foreground(t.TextMuted()).Width(width).Render("No research objective yet"),
			baseStyle.Width(width).Render("Start with /research set <objective>"),
		)
	} else {
		meta := []string{
			"stage " + strings.ToUpper(string(m.research.Stage)),
			fmt.Sprintf("%d experiments", len(m.research.Experiments)),
			fmt.Sprintf("%d criteria", len(m.research.SuccessCriteria)),
		}
		if strings.TrimSpace(m.research.PromotedExperimentID) != "" {
			meta = append(meta, "promoted "+shortResearchID(m.research.PromotedExperimentID))
		}
		sections = append(sections,
			baseStyle.Width(width).Render(truncateString(m.research.Objective, max(12, width))),
			baseStyle.Foreground(t.TextMuted()).Width(width).Render(strings.Join(meta, "  ")),
		)
		if m.researchInherited {
			source := "Inherited from parent chat"
			if strings.TrimSpace(m.researchTitle) != "" {
				source += ": " + m.researchTitle
			}
			sections = append(sections, baseStyle.Foreground(t.TextMuted()).Width(width).Render(truncateString(source, max(12, width))))
		}
		sections = append(sections, baseStyle.Width(width).Render("Next: "+truncateString(nextAction, max(12, width-6))))
	}

	if plan, ok := activeExperiment(m.research); ok {
		sections = append(sections, "", baseStyle.Foreground(t.Primary()).Bold(true).Render("Active Candidate"))
		sections = append(sections, zone.Mark(workbenchNavExperimentZoneID(plan.ID), lipgloss.JoinVertical(lipgloss.Left, m.renderExperimentCard(width, plan, true)...)))
	}
	if plan, ok := promotedExperiment(m.research); ok && plan.ID != m.research.ActiveExperimentID {
		sections = append(sections, "", baseStyle.Foreground(t.Primary()).Bold(true).Render("Promoted Candidate"))
		sections = append(sections, zone.Mark(workbenchNavExperimentZoneID(plan.ID), lipgloss.JoinVertical(lipgloss.Left, m.renderExperimentCard(width, plan, false)...)))
	}

	queue := queuedExperiments(m.research)
	sections = append(sections, "", baseStyle.Foreground(t.Primary()).Bold(true).Render("Queue"))
	if len(queue) == 0 {
		sections = append(sections, baseStyle.Foreground(t.TextMuted()).Width(width).Render("No queued follow-up candidates"))
	} else {
		maxItems := min(len(queue), 4)
		for i := 0; i < maxItems; i++ {
			plan := queue[i]
			label := shortResearchID(plan.ID) + fmt.Sprintf(" G%d ", plan.Generation) + truncateString(plan.Title, max(10, width-18))
			flags := experimentFlags(m.research, plan)
			if len(flags) > 0 {
				label += " [" + strings.Join(flags, ", ") + "]"
			}
			block := []string{baseStyle.Width(width).Render(label)}
			detail := string(plan.Status)
			if plan.LatestEval != nil {
				detail += fmt.Sprintf("  %.2f %s", plan.LatestEval.Score, strings.ToUpper(string(plan.LatestEval.Decision)))
			}
			block = append(block,
				baseStyle.Foreground(t.TextMuted()).Width(width).Render(truncateString(detail, max(12, width))),
				baseStyle.Foreground(t.TextMuted()).Width(width).Render("Click to activate"),
			)
			sections = append(sections, zone.Mark(workbenchNavExperimentZoneID(plan.ID), lipgloss.JoinVertical(lipgloss.Left, block...)))
		}
		if len(queue) > maxItems {
			sections = append(sections, baseStyle.Foreground(t.TextMuted()).Width(width).Render(fmt.Sprintf("+%d more queued candidates", len(queue)-maxItems)))
		}
	}

	sections = append(sections,
		"",
		baseStyle.Foreground(t.Primary()).Bold(true).Render("Quick Actions"),
	)
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
	chips := make([]string, 0, len(quickActions))
	for _, action := range quickActions {
		chips = append(chips, renderWorkbenchNavActionChip(action))
	}
	sections = append(sections, lipgloss.JoinHorizontal(lipgloss.Left, chips...))
	sections = append(sections, baseStyle.Width(width).Foreground(t.TextMuted()).Render("Click experiments to activate them. Click actions to run them."))

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

func (m *workbenchNavCmp) renderExperimentCard(width int, plan research.ExperimentPlan, showNextAction bool) []string {
	baseStyle := styles.BaseStyle()
	t := theme.CurrentTheme()

	lines := []string{
		baseStyle.Width(width).Render(truncateString(plan.Title, max(12, width))),
	}

	meta := []string{
		shortResearchID(plan.ID),
		fmt.Sprintf("G%d", plan.Generation),
		strings.ToUpper(string(plan.Status)),
	}
	flags := experimentFlags(m.research, plan)
	if len(flags) > 0 {
		meta = append(meta, strings.Join(flags, "/"))
	}
	lines = append(lines, baseStyle.Foreground(t.TextMuted()).Width(width).Render(truncateString(strings.Join(meta, "  "), max(12, width))))

	if plan.LatestEval != nil {
		lines = append(lines, baseStyle.Foreground(t.TextMuted()).Width(width).Render(
			truncateString(fmt.Sprintf("score %.2f  %s", plan.LatestEval.Score, strings.ToUpper(string(plan.LatestEval.Decision))), max(12, width)),
		))
	}
	if len(plan.Runs) > 0 {
		lines = append(lines, baseStyle.Foreground(t.TextMuted()).Width(width).Render(
			truncateString(fmt.Sprintf("runs %d  latest %s", len(plan.Runs), plan.Runs[0].Title), max(12, width)),
		))
	}
	if showNextAction {
		lines = append(lines, baseStyle.Width(width).Render("Next: "+truncateString(nextResearchAction(m.research), max(12, width-6))))
	}
	lines = append(lines, baseStyle.Foreground(t.TextMuted()).Width(width).Render("Click to activate"))
	return lines
}

func renderWorkbenchNavActionChip(action researchWorkbenchAction) string {
	t := theme.CurrentTheme()
	style := lipgloss.NewStyle().
		Padding(0, 1).
		MarginRight(1).
		Background(t.BackgroundDarker()).
		Foreground(t.Text())

	if action.Enabled {
		style = style.Background(t.Secondary()).Foreground(t.Background()).Bold(true)
	} else {
		style = style.Foreground(t.TextMuted())
	}
	return zone.Mark(workbenchNavActionZoneID(action.ID), style.Render(action.Label))
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
