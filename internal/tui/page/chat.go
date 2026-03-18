package page

import (
	"context"
	"fmt"
	"strings"

	"github.com/SciMate-AI/scicli/internal/app"
	"github.com/SciMate-AI/scicli/internal/completions"
	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/SciMate-AI/scicli/internal/message"
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
	layout               layout.SplitPaneLayout
	session              session.Session
	completionDialog     dialog.CompletionDialog
	showCompletionDialog bool
	inspectorFocused     bool
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
				u, cmd := p.layout.Update(chat.InspectorFocusMsg{Focused: false})
				p.layout = u.(layout.SplitPaneLayout)
				return p, cmd
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
			p.inspectorFocused = !p.inspectorFocused
			u, cmd := p.layout.Update(chat.InspectorFocusMsg{Focused: p.inspectorFocused})
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
	var cmds []tea.Cmd
	if p.session.ID == "" {
		session, err := p.app.Sessions.Create(context.Background(), "New Session")
		if err != nil {
			return util.ReportError(err)
		}

		p.session = session
		cmd := p.setSidebar()
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		cmds = append(cmds, util.CmdHandler(chat.SessionSelectedMsg(session)))
	}
	if config.Get().Automation.WorkMode == config.WorkModeUltrawork {
		p.app.Permissions.AutoApproveSession(p.session.ID)
	}

	_, err := p.app.CoderAgent.Run(context.Background(), p.session.ID, text, attachments...)
	if err != nil {
		return util.ReportError(err)
	}
	return tea.Batch(cmds...)
}

func (p *chatPage) handleSlashCommand(text string) tea.Cmd {
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
	case "/help":
		return util.ReportInfo("Ctrl+K opens the searchable command palette. /skills opens the skill browser. /tasks shows delegated sessions. /parent jumps back to the parent chat.")
	default:
		return util.ReportWarn("Unknown slash command")
	}
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
		layout.WithPadding(1, 1, 0, 1),
	)
	navContainer := layout.NewContainer(
		chat.NewWorkbenchNavCmp(app),
		layout.WithPadding(1, 1, 1, 1),
		layout.WithBorder(false, true, false, false),
	)
	centerWorkbench := layout.NewContainer(
		layout.NewSplitPane(
			layout.WithLeftPanel(navContainer),
			layout.WithRightPanel(messagesContainer),
			layout.WithRatio(0.28),
		),
	)
	inspectorContainer := layout.NewContainer(
		chat.NewInspectorCmp(app),
		layout.WithPadding(1, 1, 1, 1),
		layout.WithBorder(false, false, false, true),
	)
	editorContainer := layout.NewContainer(
		chat.NewEditorCmp(app),
		layout.WithBorder(true, false, false, false),
	)
	return &chatPage{
		app:              app,
		editor:           editorContainer,
		messages:         messagesContainer,
		completionDialog: completionDialog,
		layout: layout.NewSplitPane(
			layout.WithLeftPanel(centerWorkbench),
			layout.WithRightPanel(inspectorContainer),
			layout.WithRatio(0.74),
			layout.WithBottomPanel(editorContainer),
			layout.WithVerticalRatio(0.84),
		),
	}
}
