package page

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/SciMate-AI/scicli/internal/app"
	"github.com/SciMate-AI/scicli/internal/completions"
	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/SciMate-AI/scicli/internal/message"
	"github.com/SciMate-AI/scicli/internal/research"
	"github.com/SciMate-AI/scicli/internal/session"
	"github.com/SciMate-AI/scicli/internal/tui/components/chat"
	"github.com/SciMate-AI/scicli/internal/tui/components/dialog"
	"github.com/SciMate-AI/scicli/internal/tui/layout"
	"github.com/SciMate-AI/scicli/internal/tui/util"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var ChatPage PageID = "chat"

type chatPage struct {
	app                  *app.App
	editor               layout.Container
	messages             layout.Container
	inspector            layout.Container
	layout               layout.SplitPaneLayout
	session              session.Session
	completionDialog     dialog.CompletionDialog
	showCompletionDialog bool
	inspectorFocused     bool
	inspectorVisible     bool
}

type experimentProposalGeneratedMsg struct {
	plan  research.ExperimentPlan
	err   error
	parse bool
}

type experimentProposalPayload struct {
	Title     string                     `json:"title"`
	Prompt    string                     `json:"prompt"`
	Rationale string                     `json:"rationale,omitempty"`
	Strategy  research.SelectionDecision `json:"strategy,omitempty"`
}

type ChatKeyMap struct {
	ShowCompletionDialog key.Binding
	NewSession           key.Binding
	Cancel               key.Binding
	FocusInspector       key.Binding
}

var keyMap = ChatKeyMap{
	ShowCompletionDialog: key.NewBinding(
		key.WithKeys("@"),
		key.WithHelp("@", "Complete"),
	),
	NewSession: key.NewBinding(
		key.WithKeys("ctrl+n"),
		key.WithHelp("ctrl+n", "new session"),
	),
	Cancel: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel"),
	),
	FocusInspector: key.NewBinding(
		key.WithKeys("ctrl+i"),
		key.WithHelp("ctrl+i", "focus inspector"),
	),
}

func (p *chatPage) Init() tea.Cmd {
	cmds := []tea.Cmd{
		p.layout.Init(),
		p.completionDialog.Init(),
	}
	return tea.Batch(cmds...)
}

func (p *chatPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		cmd := p.layout.SetSize(msg.Width, msg.Height)
		cmds = append(cmds, cmd)
	case dialog.CompletionDialogCloseMsg:
		p.showCompletionDialog = false
	case chat.SendMsg:
		cmd := p.sendMessage(msg.Text, msg.Attachments)
		if cmd != nil {
			return p, cmd
		}
	case dialog.CommandRunCustomMsg:
		// Check if the agent is busy before executing custom commands
		if p.app.CoderAgent.IsBusy() {
			return p, util.ReportWarn("Agent is busy, please wait before executing a command...")
		}

		// Process the command content with arguments if any
		content := msg.Content
		if msg.Args != nil {
			// Replace all named arguments with their values
			for name, value := range msg.Args {
				placeholder := "$" + name
				content = strings.ReplaceAll(content, placeholder, value)
			}
		}

		// Handle custom command execution
		cmd := p.sendMessage(content, nil)
		if cmd != nil {
			return p, cmd
		}
	case chat.SessionSelectedMsg:
		p.session = msg
	case chat.InspectorFocusMsg:
		p.inspectorFocused = msg.Focused
		u, cmd := p.layout.Update(msg)
		p.layout = u.(layout.SplitPaneLayout)
		return p, cmd
	case tea.KeyMsg:
		if p.inspectorFocused {
			switch {
			case key.Matches(msg, keyMap.FocusInspector):
				p.inspectorFocused = false
				p.inspectorVisible = false
				cmds := []tea.Cmd{
					p.layout.ClearRightPanel(),
				}
				u, cmd := p.layout.Update(chat.InspectorFocusMsg{Focused: false})
				p.layout = u.(layout.SplitPaneLayout)
				cmds = append(cmds, cmd)
				return p, tea.Batch(cmds...)
			default:
				u, cmd := p.layout.Update(chat.InspectorKeyMsg{Key: msg})
				p.layout = u.(layout.SplitPaneLayout)
				return p, cmd
			}
		}
		switch {
		case key.Matches(msg, keyMap.ShowCompletionDialog):
			p.showCompletionDialog = true
			// Continue sending keys to layout->chat
		case key.Matches(msg, keyMap.NewSession):
			p.session = session.Session{}
			return p, tea.Batch(
				p.clearSidebar(),
				util.CmdHandler(chat.SessionClearedMsg{}),
			)
		case key.Matches(msg, keyMap.Cancel):
			if p.session.ID != "" {
				// Cancel the current session's generation process
				// This allows users to interrupt long-running operations
				p.app.CoderAgent.Cancel(p.session.ID)
				return p, nil
			}
		case key.Matches(msg, keyMap.FocusInspector):
			if !p.inspectorVisible {
				p.inspectorVisible = true
				p.inspectorFocused = true
				return p, tea.Batch(
					p.layout.SetRightPanel(p.inspector),
					util.CmdHandler(chat.InspectorFocusMsg{Focused: true}),
				)
			}
			p.inspectorFocused = !p.inspectorFocused
			if !p.inspectorFocused {
				p.inspectorVisible = false
				return p, tea.Batch(
					p.layout.ClearRightPanel(),
					util.CmdHandler(chat.InspectorFocusMsg{Focused: false}),
				)
			}
			u, cmd := p.layout.Update(chat.InspectorFocusMsg{Focused: true})
			p.layout = u.(layout.SplitPaneLayout)
			return p, cmd
		}
	case chat.InspectorOpenTaskMsg:
		selectedSession, err := p.app.Sessions.Get(context.Background(), msg.SessionID)
		if err != nil {
			return p, util.ReportError(err)
		}
		p.session = selectedSession
		p.inspectorFocused = false
		u, cmd := p.layout.Update(chat.InspectorFocusMsg{Focused: false})
		p.layout = u.(layout.SplitPaneLayout)
		return p, tea.Batch(cmd, util.CmdHandler(chat.SessionSelectedMsg(selectedSession)))
	case chat.InspectorStopTaskMsg:
		if err := p.app.TaskRuns.Cancel(msg.SessionID); err != nil {
			return p, util.ReportError(err)
		}
		return p, util.ReportInfo("Stop signal sent to delegated task")
	case chat.WorkbenchRunCommandMsg:
		return p, util.CmdHandler(dialog.CommandRunCustomMsg{Content: msg.Command})
	case chat.WorkbenchOpenArgumentsMsg:
		return p, util.CmdHandler(dialog.ShowMultiArgumentsDialogMsg{
			CommandID: msg.CommandID,
			Content:   msg.Command,
			ArgNames:  msg.ArgNames,
		})
	case experimentProposalGeneratedMsg:
		if msg.err != nil {
			if msg.parse {
				return p, util.ReportWarn("Experiment proposal was generated in chat, but could not be parsed into a plan")
			}
			return p, util.ReportError(msg.err)
		}
		return p, util.ReportInfo(fmt.Sprintf("Proposed next experiment %s: %s", shortExperimentID(msg.plan.ID), msg.plan.Title))
	}
	if p.showCompletionDialog {
		context, contextCmd := p.completionDialog.Update(msg)
		p.completionDialog = context.(dialog.CompletionDialog)
		cmds = append(cmds, contextCmd)

		// Doesn't forward event if enter key is pressed
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			if keyMsg.String() == "enter" {
				return p, tea.Batch(cmds...)
			}
		}
	}

	u, cmd := p.layout.Update(msg)
	cmds = append(cmds, cmd)
	p.layout = u.(layout.SplitPaneLayout)

	return p, tea.Batch(cmds...)
}

func (p *chatPage) setSidebar() tea.Cmd {
	return nil
}

func (p *chatPage) clearSidebar() tea.Cmd {
	return nil
}

func (p *chatPage) sendMessage(text string, attachments []message.Attachment) tea.Cmd {
	if strings.HasPrefix(strings.TrimSpace(text), "/") && len(attachments) == 0 {
		return p.handleSlashCommand(text)
	}
	activeSession, sessionCmd, err := p.ensureSession(context.Background())
	if err != nil {
		return util.ReportError(err)
	}
	p.session = activeSession
	if config.Get().Automation.WorkMode == config.WorkModeUltrawork {
		p.app.Permissions.AutoApproveSession(p.session.ID)
	}

	_, err = p.app.CoderAgent.Run(context.Background(), p.session.ID, text, attachments...)
	if err != nil {
		return util.ReportError(err)
	}
	return sessionCmd
}

func (p *chatPage) handleSlashCommand(text string) tea.Cmd {
	trimmed := strings.TrimSpace(text)
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return nil
	}

	switch fields[0] {
	case "/new":
		p.session = session.Session{}
		return tea.Batch(p.clearSidebar(), util.CmdHandler(chat.SessionClearedMsg{}))
	case "/compact":
		return util.CmdHandler(dialog.StartCompactSessionMsg{})
	case "/skills":
		return util.CmdHandler(dialog.ShowSkillDialogMsg{})
	case "/tasks":
		return util.CmdHandler(dialog.ShowTaskDialogMsg{})
	case "/install-skill":
		if len(fields) < 2 {
			return util.ReportWarn("Usage: /install-skill <local-path-or-github-tree-url>")
		}
		skill, err := p.app.Skills.Install(context.Background(), fields[1])
		if err != nil {
			return util.ReportError(err)
		}
		return util.ReportInfo(fmt.Sprintf("Installed skill %s", skill.ID))
	case "/ultrawork":
		mode := config.WorkModeUltrawork
		if len(fields) > 1 {
			switch strings.ToLower(fields[1]) {
			case "off", "interactive":
				mode = config.WorkModeInteractive
			case "auto":
				mode = config.WorkModeAuto
			case "on", "ultrawork":
				mode = config.WorkModeUltrawork
			default:
				return util.ReportWarn("Usage: /ultrawork [on|off|auto]")
			}
		}
		if err := config.SetWorkMode(mode, false); err != nil {
			return util.ReportError(err)
		}
		return util.ReportInfo("Work mode set to " + string(mode))
	case "/parent":
		if p.session.ParentSessionID == "" {
			return util.ReportWarn("Current session has no parent")
		}
		parentSession, err := p.app.Sessions.Get(context.Background(), p.session.ParentSessionID)
		if err != nil {
			return util.ReportError(err)
		}
		p.session = parentSession
		return util.CmdHandler(chat.SessionSelectedMsg(parentSession))
	case "/research":
		if len(fields) < 2 {
			return util.ReportWarn("Usage: /research [set <objective>|show]")
		}
		switch strings.ToLower(fields[1]) {
		case "set":
			const prefix = "/research set"
			if len(trimmed) <= len(prefix) {
				return util.ReportWarn("Usage: /research set <objective>")
			}
			objective := strings.TrimSpace(trimmed[len(prefix):])
			if objective == "" {
				return util.ReportWarn("Usage: /research set <objective>")
			}
			researchSession, sessionCmd, inherited, err := p.ensureResearchSession(context.Background())
			if err != nil {
				return util.ReportError(err)
			}
			state, err := p.app.Research.SetObjective(context.Background(), researchSession.ID, objective)
			if err != nil {
				return util.ReportError(err)
			}
			label := "Research objective saved"
			if inherited {
				label = "Research objective saved on parent research session"
			}
			return tea.Batch(sessionCmd, util.ReportInfo(formatResearchSummary(label, state)))
		case "show":
			if p.session.ID == "" {
				return util.ReportWarn("No active session")
			}
			researchSession, err := p.resolveResearchSession(context.Background(), p.session)
			if err != nil {
				return util.ReportError(err)
			}
			state, err := p.app.Research.Get(context.Background(), researchSession.ID)
			if err != nil {
				return util.ReportError(err)
			}
			if !state.HasContent() {
				return util.ReportInfo("No research objective set for this session")
			}
			return util.ReportInfo(formatResearchSummary("Research state", state))
		default:
			return util.ReportWarn("Usage: /research [set <objective>|show]")
		}
	case "/experiment":
		if len(fields) < 2 {
			return util.ReportWarn("Usage: /experiment [add <title>|list|activate <id>|promote [id]|evaluate <score> <keep|discard|mutate|branch> <summary>|rerun [id]|evolve [title]|propose]")
		}
		switch strings.ToLower(fields[1]) {
		case "add":
			const prefix = "/experiment add"
			title := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
			if title == "" {
				return util.ReportWarn("Usage: /experiment add <title>")
			}
			researchSession, sessionCmd, inherited, err := p.ensureResearchSession(context.Background())
			if err != nil {
				return util.ReportError(err)
			}
			_, plan, err := p.app.Research.AddExperiment(context.Background(), researchSession.ID, title, "")
			if err != nil {
				return util.ReportError(err)
			}
			label := fmt.Sprintf("Experiment %s created: %s", shortExperimentID(plan.ID), plan.Title)
			if inherited {
				label += " [parent research session]"
			}
			return tea.Batch(sessionCmd, util.ReportInfo(label))
		case "list", "show":
			if p.session.ID == "" {
				return util.ReportWarn("No active session")
			}
			researchSession, err := p.resolveResearchSession(context.Background(), p.session)
			if err != nil {
				return util.ReportError(err)
			}
			state, err := p.app.Research.Get(context.Background(), researchSession.ID)
			if err != nil {
				return util.ReportError(err)
			}
			if len(state.Experiments) == 0 {
				return util.ReportInfo("No experiment plans yet")
			}
			return util.ReportInfo(formatExperimentSummary(state))
		case "compare":
			if p.session.ID == "" {
				return util.ReportWarn("No active session")
			}
			researchSession, err := p.resolveResearchSession(context.Background(), p.session)
			if err != nil {
				return util.ReportError(err)
			}
			state, err := p.app.Research.Get(context.Background(), researchSession.ID)
			if err != nil {
				return util.ReportError(err)
			}
			if len(state.Experiments) < 2 {
				return util.ReportWarn("Need at least two experiments to compare")
			}
			return util.ReportInfo(formatExperimentComparison(state))
		case "activate":
			if len(fields) < 3 {
				return util.ReportWarn("Usage: /experiment activate <experiment-id>")
			}
			researchSession, sessionCmd, _, err := p.ensureResearchSession(context.Background())
			if err != nil {
				return util.ReportError(err)
			}
			state, err := p.app.Research.SetActiveExperiment(context.Background(), researchSession.ID, fields[2])
			if err != nil {
				return util.ReportError(err)
			}
			return tea.Batch(sessionCmd, util.ReportInfo("Active experiment set to "+shortExperimentID(state.ActiveExperimentID)))
		case "promote":
			researchSession, sessionCmd, _, err := p.ensureResearchSession(context.Background())
			if err != nil {
				return util.ReportError(err)
			}
			state, err := p.app.Research.Get(context.Background(), researchSession.ID)
			if err != nil {
				return util.ReportError(err)
			}
			experimentID := strings.TrimSpace(state.ActiveExperimentID)
			if len(fields) > 2 {
				experimentID = strings.TrimSpace(fields[2])
			}
			if experimentID == "" {
				return util.ReportWarn("No active experiment. Use /experiment activate <id> first")
			}
			state, plan, err := p.app.Research.PromoteExperiment(context.Background(), researchSession.ID, experimentID)
			if err != nil {
				return util.ReportError(err)
			}
			return tea.Batch(sessionCmd, util.ReportInfo(fmt.Sprintf("Promoted %s as the current best candidate", shortExperimentID(state.PromotedExperimentID)+" "+plan.Title)))
		case "evaluate":
			if len(fields) < 5 {
				return util.ReportWarn("Usage: /experiment evaluate <score> <keep|discard|mutate|branch> <summary>")
			}
			score, err := strconv.ParseFloat(fields[2], 64)
			if err != nil {
				return util.ReportWarn("Experiment score must be a number")
			}
			decision := strings.TrimSpace(fields[3])
			prefix := fmt.Sprintf("/experiment evaluate %s %s", fields[2], fields[3])
			summary := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
			if summary == "" {
				return util.ReportWarn("Usage: /experiment evaluate <score> <keep|discard|mutate|branch> <summary>")
			}
			researchSession, sessionCmd, _, err := p.ensureResearchSession(context.Background())
			if err != nil {
				return util.ReportError(err)
			}
			state, err := p.app.Research.Get(context.Background(), researchSession.ID)
			if err != nil {
				return util.ReportError(err)
			}
			if strings.TrimSpace(state.ActiveExperimentID) == "" {
				return util.ReportWarn("No active experiment. Use /experiment add or /experiment activate first")
			}
			_, plan, err := p.app.Research.EvaluateExperiment(context.Background(), researchSession.ID, state.ActiveExperimentID, score, decision, summary)
			if err != nil {
				return util.ReportError(err)
			}
			return tea.Batch(sessionCmd, util.ReportInfo(fmt.Sprintf("Evaluated %s: %.2f %s", shortExperimentID(plan.ID), score, strings.ToUpper(string(plan.LatestEval.Decision)))))
		case "rerun":
			researchSession, sessionCmd, _, err := p.ensureResearchSession(context.Background())
			if err != nil {
				return util.ReportError(err)
			}
			state, err := p.app.Research.Get(context.Background(), researchSession.ID)
			if err != nil {
				return util.ReportError(err)
			}
			experimentID := strings.TrimSpace(state.ActiveExperimentID)
			if len(fields) > 2 {
				experimentID = strings.TrimSpace(fields[2])
			}
			if experimentID == "" {
				return util.ReportWarn("No active experiment. Use /experiment activate <id> first")
			}
			_, plan, err := p.app.Research.CloneExperiment(context.Background(), researchSession.ID, experimentID)
			if err != nil {
				return util.ReportError(err)
			}
			return tea.Batch(sessionCmd, util.ReportInfo(fmt.Sprintf("Created rerun candidate %s from %s", shortExperimentID(plan.ID), shortExperimentID(plan.ParentExperimentID))))
		case "evolve":
			researchSession, sessionCmd, _, err := p.ensureResearchSession(context.Background())
			if err != nil {
				return util.ReportError(err)
			}
			state, err := p.app.Research.Get(context.Background(), researchSession.ID)
			if err != nil {
				return util.ReportError(err)
			}
			experimentID := strings.TrimSpace(state.ActiveExperimentID)
			if experimentID == "" {
				return util.ReportWarn("No active experiment. Use /experiment activate <id> first")
			}
			title := strings.TrimSpace(strings.TrimPrefix(trimmed, "/experiment evolve"))
			_, plan, err := p.app.Research.EvolveExperiment(context.Background(), researchSession.ID, experimentID, title)
			if err != nil {
				return util.ReportError(err)
			}
			return tea.Batch(sessionCmd, util.ReportInfo(fmt.Sprintf("Created generation %d candidate %s from %s", plan.Generation, shortExperimentID(plan.ID), shortExperimentID(plan.ParentExperimentID))))
		case "propose":
			if p.app.CoderAgent.IsBusy() {
				return util.ReportWarn("Agent is busy, please wait before proposing the next experiment")
			}
			activeSession, sessionCmd, err := p.ensureSession(context.Background())
			if err != nil {
				return util.ReportError(err)
			}
			researchSession, err := p.resolveResearchSession(context.Background(), activeSession)
			if err != nil {
				return util.ReportError(err)
			}
			state, err := p.app.Research.Get(context.Background(), researchSession.ID)
			if err != nil {
				return util.ReportError(err)
			}
			if strings.TrimSpace(state.Objective) == "" {
				return util.ReportWarn("Set a research objective before proposing the next experiment")
			}
			if len(state.Experiments) == 0 {
				return util.ReportWarn("Create or run at least one experiment before proposing the next one")
			}
			return tea.Batch(sessionCmd, p.proposeExperimentCmd(activeSession.ID, researchSession.ID, state))
		default:
			return util.ReportWarn("Usage: /experiment [add <title>|list|compare|activate <id>|promote [id]|evaluate <score> <keep|discard|mutate|branch> <summary>|rerun [id]|evolve [title]|propose]")
		}
	case "/artifact":
		if len(fields) < 2 {
			return util.ReportWarn("Usage: /artifact [list [query]|show <artifact-id>]")
		}
		if p.session.ID == "" {
			return util.ReportWarn("No active session")
		}
		researchSession, err := p.resolveResearchSession(context.Background(), p.session)
		if err != nil {
			return util.ReportError(err)
		}
		state, err := p.app.Research.Get(context.Background(), researchSession.ID)
		if err != nil {
			return util.ReportError(err)
		}
		index := research.ArtifactIndex(state)
		if len(index) == 0 {
			return util.ReportWarn("No experiment artifacts available")
		}
		switch strings.ToLower(fields[1]) {
		case "list":
			query := ""
			if len(fields) > 2 {
				query = strings.TrimSpace(strings.TrimPrefix(trimmed, "/artifact list"))
			}
			filtered := research.FilterArtifactIndex(index, query)
			if len(filtered) == 0 {
				return util.ReportWarn("No artifacts match that query")
			}
			return util.ReportInfo(formatArtifactIndexSummary(filtered, len(index), query))
		case "show":
			if len(fields) < 3 {
				return util.ReportWarn("Usage: /artifact show <artifact-id>")
			}
			record, ok := research.FindArtifact(index, fields[2])
			if !ok {
				return util.ReportWarn("Artifact not found or ambiguous prefix")
			}
			return util.ReportInfo(formatArtifactDetail(record))
		default:
			return util.ReportWarn("Usage: /artifact [list [query]|show <artifact-id>]")
		}
	case "/help":
		return util.ReportInfo("Ctrl+K opens the searchable command palette. /skills opens the skill browser. /tasks shows delegated sessions. /research set stores a session objective. /experiment add creates a structured plan. /experiment evaluate uses keep/discard/mutate/branch. /experiment promote marks the current best candidate. /experiment evolve creates the next generation from mutate/branch decisions. /experiment propose asks the agent for the next candidate. /artifact list [query] searches captured provenance. /artifact show <id> shows artifact provenance. /parent jumps back to the parent chat.")
	default:
		return util.ReportWarn("Unknown slash command")
	}
}

func (p *chatPage) ensureSession(ctx context.Context) (session.Session, tea.Cmd, error) {
	if p.session.ID != "" {
		return p.session, nil, nil
	}

	activeSession, err := p.app.Sessions.Create(ctx, "New Session")
	if err != nil {
		return session.Session{}, nil, err
	}

	p.session = activeSession
	cmds := []tea.Cmd{}
	if cmd := p.setSidebar(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	cmds = append(cmds, util.CmdHandler(chat.SessionSelectedMsg(activeSession)))
	return activeSession, tea.Batch(cmds...), nil
}

func (p *chatPage) ensureResearchSession(ctx context.Context) (session.Session, tea.Cmd, bool, error) {
	activeSession, sessionCmd, err := p.ensureSession(ctx)
	if err != nil {
		return session.Session{}, nil, false, err
	}
	researchSession, err := p.resolveResearchSession(ctx, activeSession)
	if err != nil {
		return session.Session{}, nil, false, err
	}
	return researchSession, sessionCmd, researchSession.ID != activeSession.ID, nil
}

func (p *chatPage) resolveResearchSession(ctx context.Context, current session.Session) (session.Session, error) {
	for current.ParentSessionID != "" {
		parent, err := p.app.Sessions.Get(ctx, current.ParentSessionID)
		if err != nil {
			return session.Session{}, err
		}
		current = parent
	}
	return current, nil
}

func formatResearchSummary(prefix string, state research.SessionState) string {
	summary := prefix + ": " + state.Objective
	meta := []string{string(state.Stage)}
	if len(state.SuccessCriteria) > 0 {
		meta = append(meta, fmt.Sprintf("%d criteria", len(state.SuccessCriteria)))
	}
	if len(state.Hypotheses) > 0 {
		meta = append(meta, fmt.Sprintf("%d hypotheses", len(state.Hypotheses)))
	}
	if len(state.Experiments) > 0 {
		meta = append(meta, fmt.Sprintf("%d experiments", len(state.Experiments)))
	}
	if strings.TrimSpace(state.PromotedExperimentID) != "" {
		meta = append(meta, "promoted "+shortExperimentID(state.PromotedExperimentID))
	}
	return summary + " [" + strings.Join(meta, ", ") + "]"
}

func formatExperimentSummary(state research.SessionState) string {
	items := make([]string, 0, len(state.Experiments))
	for _, plan := range state.Experiments {
		prefix := shortExperimentID(plan.ID) + fmt.Sprintf(" G%d ", plan.Generation) + strings.ToUpper(string(plan.Status))
		if plan.ID == state.ActiveExperimentID {
			prefix += " [active]"
		}
		if plan.ID == state.PromotedExperimentID {
			prefix += " [promoted]"
		}
		line := prefix + ": " + plan.Title
		if plan.EvolutionDecision != "" {
			line += " {" + strings.ToUpper(string(plan.EvolutionDecision)) + "}"
		}
		if plan.LatestEval != nil {
			line += fmt.Sprintf(" (score %.2f %s)", plan.LatestEval.Score, strings.ToUpper(string(plan.LatestEval.Decision)))
		} else if len(plan.Runs) > 0 {
			line += fmt.Sprintf(" (%d runs)", len(plan.Runs))
		}
		if plan.ParentExperimentID != "" {
			line += " from " + shortExperimentID(plan.ParentExperimentID)
		}
		items = append(items, line)
	}
	return strings.Join(items, " | ")
}

func formatExperimentComparison(state research.SessionState) string {
	groups := research.LineageGroups(state)
	lines := make([]string, 0, len(state.Experiments)*2+len(groups)+2)
	lines = append(lines, "Lineage board:")
	for _, group := range groups {
		header := "Root " + shortExperimentID(group.RootID)
		if strings.TrimSpace(group.RootTitle) != "" {
			header += " " + group.RootTitle
		}
		if group.RootID == state.PromotedExperimentID {
			header += " [PROMOTED]"
		}
		if group.BestScore >= 0 {
			header += fmt.Sprintf(" | best %.2f", group.BestScore)
		}
		lines = append(lines, header)
		for _, plan := range group.Plans {
			line := "  " + shortExperimentID(plan.ID) + fmt.Sprintf(" G%d ", plan.Generation) + plan.Title
			flags := make([]string, 0, 3)
			if plan.ID == state.ActiveExperimentID {
				flags = append(flags, "ACTIVE")
			}
			if plan.ID == state.PromotedExperimentID {
				flags = append(flags, "PROMOTED")
			}
			if plan.EvolutionDecision != "" {
				flags = append(flags, strings.ToUpper(string(plan.EvolutionDecision)))
			}
			if len(flags) > 0 {
				line += " [" + strings.Join(flags, ", ") + "]"
			}
			lines = append(lines, line)

			detail := []string{strings.ToUpper(string(plan.Status))}
			if plan.LatestEval != nil {
				detail = append(detail, fmt.Sprintf("score %.2f", plan.LatestEval.Score), strings.ToUpper(string(plan.LatestEval.Decision)))
			} else {
				detail = append(detail, "no evaluation")
			}
			detail = append(detail, fmt.Sprintf("runs %d", len(plan.Runs)))
			if plan.ParentExperimentID != "" {
				detail = append(detail, "from "+shortExperimentID(plan.ParentExperimentID))
			}
			lines = append(lines, "    "+strings.Join(detail, " | "))
		}
	}
	lines = append(lines, "Actions: /experiment promote <id> to mark the best candidate, /experiment evolve [title] to create the next generation from mutate/branch.")
	return strings.Join(lines, "\n")
}

func formatArtifactIndexSummary(items []research.ArtifactRecord, total int, query string) string {
	limit := min(len(items), 6)
	lines := make([]string, 0, limit+1)
	header := fmt.Sprintf("Artifacts %d/%d", len(items), total)
	if strings.TrimSpace(query) != "" {
		header += fmt.Sprintf(" for %q", query)
	}
	lines = append(lines, header)
	for i := 0; i < limit; i++ {
		item := items[i]
		line := shortArtifactID(item.Artifact.ID) + " " + strings.ToUpper(string(item.Artifact.Kind))
		if strings.TrimSpace(item.Artifact.Label) != "" {
			line += " " + item.Artifact.Label
		}
		line += " | " + shortExperimentID(item.ExperimentID)
		if strings.TrimSpace(item.RunTitle) != "" {
			line += " | " + item.RunTitle
		}
		if strings.TrimSpace(item.Artifact.Path) != "" {
			line += " | " + item.Artifact.Path
		}
		lines = append(lines, line)
	}
	if len(items) > limit {
		lines = append(lines, fmt.Sprintf("+%d more artifacts", len(items)-limit))
	}
	return strings.Join(lines, " | ")
}

func formatArtifactDetail(item research.ArtifactRecord) string {
	parts := []string{
		"Artifact " + item.Artifact.ID,
		"kind " + strings.ToUpper(string(item.Artifact.Kind)),
		"experiment " + shortExperimentID(item.ExperimentID) + " " + item.ExperimentTitle,
	}
	if strings.TrimSpace(item.RunTitle) != "" {
		parts = append(parts, "run "+item.RunTitle)
	}
	if strings.TrimSpace(item.Artifact.Path) != "" {
		parts = append(parts, "path "+item.Artifact.Path)
	}
	if strings.TrimSpace(item.Artifact.URI) != "" {
		parts = append(parts, "uri "+item.Artifact.URI)
	}
	if item.Evaluation != nil {
		parts = append(parts, fmt.Sprintf("evaluation %.2f %s", item.Evaluation.Score, string(item.Evaluation.Decision)))
	}
	if strings.TrimSpace(item.Artifact.Summary) != "" {
		parts = append(parts, "summary "+item.Artifact.Summary)
	}
	return strings.Join(parts, " | ")
}

func shortArtifactID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) <= 18 {
		return id
	}
	return id[:18]
}

func shortExperimentID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

func (p *chatPage) proposeExperimentCmd(chatSessionID string, researchSessionID string, state research.SessionState) tea.Cmd {
	prompt := buildExperimentProposalPrompt(state)
	return func() tea.Msg {
		done, err := p.app.CoderAgent.Run(context.Background(), chatSessionID, prompt)
		if err != nil {
			return experimentProposalGeneratedMsg{err: err}
		}
		result := <-done
		if result.Error != nil {
			return experimentProposalGeneratedMsg{err: result.Error}
		}

		payload, err := parseExperimentProposal(result.Message.Content().String())
		if err != nil {
			return experimentProposalGeneratedMsg{err: err, parse: true}
		}

		parentID := strings.TrimSpace(state.ActiveExperimentID)
		_, plan, err := p.app.Research.AddExperimentCandidate(context.Background(), researchSessionID, research.ExperimentPlan{
			Title:              payload.Title,
			Prompt:             payload.Prompt,
			Rationale:          payload.Rationale,
			ParentExperimentID: parentID,
			EvolutionDecision:  payload.Strategy,
		})
		if err != nil {
			return experimentProposalGeneratedMsg{err: err}
		}
		if _, err := p.app.Research.SetActiveExperiment(context.Background(), researchSessionID, plan.ID); err != nil {
			return experimentProposalGeneratedMsg{err: err}
		}
		return experimentProposalGeneratedMsg{plan: plan}
	}
}

func buildExperimentProposalPrompt(state research.SessionState) string {
	var lines []string
	lines = append(lines,
		"You are proposing the next experiment in a structured research loop.",
		"Return JSON only. No markdown fence. No explanation outside JSON.",
		`JSON schema: {"title":"...","prompt":"...","rationale":"...","strategy":"mutate|branch"}`,
		"",
		"Research objective:",
		state.Objective,
		"",
		"Current stage: "+string(state.Stage),
	)
	if strings.TrimSpace(state.PromotedExperimentID) != "" {
		lines = append(lines, "Promoted candidate: "+shortExperimentID(state.PromotedExperimentID))
	}
	if len(state.SuccessCriteria) > 0 {
		lines = append(lines, "", "Success criteria:")
		for _, item := range state.SuccessCriteria {
			lines = append(lines, "- "+item)
		}
	}
	lines = append(lines, "", "Recent experiments:")
	for i, plan := range state.Experiments {
		if i >= 4 {
			break
		}
		header := fmt.Sprintf("- G%d | %s | %s", plan.Generation, plan.Title, string(plan.Status))
		if plan.ID == state.ActiveExperimentID {
			header += " | active"
		}
		if plan.ID == state.PromotedExperimentID {
			header += " | promoted"
		}
		if strings.TrimSpace(plan.LineageRootID) != "" {
			header += " | root " + shortExperimentID(plan.LineageRootID)
		}
		if strings.TrimSpace(plan.ParentExperimentID) != "" {
			header += " | from " + shortExperimentID(plan.ParentExperimentID)
		}
		lines = append(lines, header)
		if len(plan.Runs) > 0 {
			run := plan.Runs[0]
			lines = append(lines, fmt.Sprintf("  latest run: %s | %s", run.Title, run.Status))
			if strings.TrimSpace(run.Detail) != "" {
				lines = append(lines, "  run detail: "+run.Detail)
			}
		}
		if plan.LatestEval != nil {
			lines = append(lines, fmt.Sprintf("  evaluation: score %.2f | %s", plan.LatestEval.Score, string(plan.LatestEval.Decision)))
			if strings.TrimSpace(plan.LatestEval.Summary) != "" {
				lines = append(lines, "  evaluation summary: "+plan.LatestEval.Summary)
			}
		}
	}
	lines = append(lines,
		"",
		"Constraints:",
		"- Propose exactly one next experiment candidate.",
		"- strategy must be either mutate or branch.",
		"- Respect prior selection decisions: keep/discard are terminal outcomes; mutate/branch should produce the next generation.",
		"- The title must be short and specific.",
		"- The prompt must be ready to send to the coding agent as the next delegated task or operator instruction.",
		"- Use prior results to avoid repeating the same experiment.",
	)
	return strings.Join(lines, "\n")
}

func parseExperimentProposal(raw string) (experimentProposalPayload, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return experimentProposalPayload{}, fmt.Errorf("empty experiment proposal")
	}

	if strings.Contains(raw, "```") {
		parts := strings.Split(raw, "```")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(strings.ToLower(part), "json") {
				part = strings.TrimSpace(part[4:])
			}
			if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
				raw = part
				break
			}
		}
	}

	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end > start {
		raw = raw[start : end+1]
	}

	var payload experimentProposalPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return experimentProposalPayload{}, err
	}
	payload.Title = strings.TrimSpace(payload.Title)
	payload.Prompt = strings.TrimSpace(payload.Prompt)
	payload.Rationale = strings.TrimSpace(payload.Rationale)
	strategy, err := research.NormalizeSelectionDecision(string(payload.Strategy))
	if err != nil {
		return experimentProposalPayload{}, fmt.Errorf("proposal strategy must be mutate or branch: %w", err)
	}
	if strategy != research.DecisionMutate && strategy != research.DecisionBranch {
		return experimentProposalPayload{}, fmt.Errorf("proposal strategy must be mutate or branch")
	}
	payload.Strategy = strategy
	if payload.Title == "" || payload.Prompt == "" {
		return experimentProposalPayload{}, fmt.Errorf("proposal must include title and prompt")
	}
	return payload, nil
}

func (p *chatPage) SetSize(width, height int) tea.Cmd {
	return p.layout.SetSize(width, height)
}

func (p *chatPage) GetSize() (int, int) {
	return p.layout.GetSize()
}

func (p *chatPage) View() string {
	layoutView := p.layout.View()

	if p.showCompletionDialog {
		_, layoutHeight := p.layout.GetSize()
		editorWidth, editorHeight := p.editor.GetSize()

		p.completionDialog.SetWidth(editorWidth)
		overlay := p.completionDialog.View()

		layoutView = layout.PlaceOverlay(
			0,
			layoutHeight-editorHeight-lipgloss.Height(overlay),
			overlay,
			layoutView,
			false,
		)
	}

	return layoutView
}

func (p *chatPage) BindingKeys() []key.Binding {
	bindings := layout.KeyMapToSlice(keyMap)
	bindings = append(bindings, p.messages.BindingKeys()...)
	bindings = append(bindings, p.editor.BindingKeys()...)
	return bindings
}

func NewChatPage(app *app.App) tea.Model {
	cg := completions.NewFileAndFolderContextGroup()
	completionDialog := dialog.NewCompletionDialogCmp(cg)

	messagesContainer := layout.NewContainer(
		chat.NewMessagesCmp(app),
		layout.WithPadding(1, 2, 0, 2),
	)
	inspectorContainer := layout.NewContainer(
		chat.NewInspectorCmp(app),
		layout.WithPadding(1, 1, 1, 1),
		layout.WithBorder(false, false, false, true),
	)
	editorContainer := layout.NewContainer(
		chat.NewEditorCmp(app),
		layout.WithPadding(0, 2, 1, 2),
	)
	return &chatPage{
		app:              app,
		editor:           editorContainer,
		messages:         messagesContainer,
		inspector:        inspectorContainer,
		completionDialog: completionDialog,
		layout: layout.NewSplitPane(
			layout.WithLeftPanel(messagesContainer),
			layout.WithRatio(0.72),
			layout.WithBottomPanel(editorContainer),
			layout.WithVerticalRatio(0.86),
		),
	}
}
