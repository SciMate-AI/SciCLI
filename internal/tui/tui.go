package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/SciMate-AI/scicli/internal/app"
	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/SciMate-AI/scicli/internal/llm/agent"
	"github.com/SciMate-AI/scicli/internal/logging"
	"github.com/SciMate-AI/scicli/internal/permission"
	"github.com/SciMate-AI/scicli/internal/pubsub"
	"github.com/SciMate-AI/scicli/internal/session"
	"github.com/SciMate-AI/scicli/internal/skills"
	"github.com/SciMate-AI/scicli/internal/taskrun"
	"github.com/SciMate-AI/scicli/internal/tui/components/chat"
	"github.com/SciMate-AI/scicli/internal/tui/components/core"
	"github.com/SciMate-AI/scicli/internal/tui/components/dialog"
	"github.com/SciMate-AI/scicli/internal/tui/layout"
	"github.com/SciMate-AI/scicli/internal/tui/page"
	"github.com/SciMate-AI/scicli/internal/tui/theme"
	"github.com/SciMate-AI/scicli/internal/tui/util"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
)

type keyMap struct {
	Logs          key.Binding
	Quit          key.Binding
	Help          key.Binding
	SwitchSession key.Binding
	Commands      key.Binding
	Filepicker    key.Binding
	Models        key.Binding
	SwitchTheme   key.Binding
	PrevTask      key.Binding
	NextTask      key.Binding
}

type showSessionDialogMsg struct{}
type showModelDialogMsg struct{}
type focusInspectorGlobalMsg struct{}
type openLatestTaskGlobalMsg struct{}
type stopLatestTaskGlobalMsg struct{}

const (
	quitKey = "q"
)

var keys = keyMap{
	Logs: key.NewBinding(
		key.WithKeys("ctrl+l"),
		key.WithHelp("ctrl+l", "logs"),
	),

	Quit: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "quit"),
	),
	Help: key.NewBinding(
		key.WithKeys("ctrl+_", "ctrl+h"),
		key.WithHelp("ctrl+?", "toggle help"),
	),

	SwitchSession: key.NewBinding(
		key.WithKeys("ctrl+s"),
		key.WithHelp("ctrl+s", "switch session"),
	),

	Commands: key.NewBinding(
		key.WithKeys("ctrl+k"),
		key.WithHelp("ctrl+k", "commands"),
	),
	Filepicker: key.NewBinding(
		key.WithKeys("ctrl+f"),
		key.WithHelp("ctrl+f", "select files to upload"),
	),
	Models: key.NewBinding(
		key.WithKeys("ctrl+o"),
		key.WithHelp("ctrl+o", "model selection"),
	),

	SwitchTheme: key.NewBinding(
		key.WithKeys("ctrl+t"),
		key.WithHelp("ctrl+t", "switch theme"),
	),
	PrevTask: key.NewBinding(
		key.WithKeys("alt+["),
		key.WithHelp("alt+[", "prev task"),
	),
	NextTask: key.NewBinding(
		key.WithKeys("alt+]"),
		key.WithHelp("alt+]", "next task"),
	),
}

var helpEsc = key.NewBinding(
	key.WithKeys("?"),
	key.WithHelp("?", "toggle help"),
)

var returnKey = key.NewBinding(
	key.WithKeys("esc"),
	key.WithHelp("esc", "close"),
)

var logsKeyReturnKey = key.NewBinding(
	key.WithKeys("esc", "backspace", quitKey),
	key.WithHelp("esc/q", "go back"),
)

type appModel struct {
	width, height   int
	currentPage     page.PageID
	previousPage    page.PageID
	pages           map[page.PageID]tea.Model
	loadedPages     map[page.PageID]bool
	status          core.StatusCmp
	app             *app.App
	selectedSession session.Session

	showPermissions bool
	permissions     dialog.PermissionDialogCmp

	showHelp bool
	help     dialog.HelpCmp

	showQuit bool
	quit     dialog.QuitDialog

	showSessionDialog bool
	sessionDialog     dialog.SessionDialog

	showCommandDialog bool
	commandDialog     dialog.CommandDialog
	commands          []dialog.Command

	showModelDialog bool
	modelDialog     dialog.ModelDialog

	showProviderSetupDialog bool
	providerSetupDialog     dialog.ProviderSetupDialog

	showFilepicker bool
	filepicker     dialog.FilepickerCmp

	showThemeDialog bool
	themeDialog     dialog.ThemeDialog

	showMultiArgumentsDialog bool
	multiArgumentsDialog     dialog.MultiArgumentsDialogCmp

	showSkillsDialog bool
	skillsDialog     dialog.SkillDialog

	showTaskDialog bool
	taskDialog     dialog.TaskDialog

	isCompacting      bool
	compactingMessage string
}

func (a appModel) permissionReservedHeight() int {
	if !a.showPermissions || a.height <= 0 {
		return 0
	}
	return min(max(10, a.height/3), a.height)
}

func (a appModel) pageHeight() int {
	return max(1, a.height-a.permissionReservedHeight())
}

func (a appModel) resizeCurrentPage() tea.Cmd {
	if a.width == 0 || a.height == 0 {
		return nil
	}
	if sizable, ok := a.pages[a.currentPage].(layout.Sizeable); ok {
		return sizable.SetSize(a.width, a.pageHeight())
	}
	return nil
}

func (a appModel) Init() tea.Cmd {
	var cmds []tea.Cmd
	cmd := a.pages[a.currentPage].Init()
	a.loadedPages[a.currentPage] = true
	cmds = append(cmds, cmd)
	cmd = a.status.Init()
	cmds = append(cmds, cmd)
	cmd = a.quit.Init()
	cmds = append(cmds, cmd)
	cmd = a.help.Init()
	cmds = append(cmds, cmd)
	cmd = a.sessionDialog.Init()
	cmds = append(cmds, cmd)
	cmd = a.commandDialog.Init()
	cmds = append(cmds, cmd)
	cmd = a.modelDialog.Init()
	cmds = append(cmds, cmd)
	cmd = a.providerSetupDialog.Init()
	cmds = append(cmds, cmd)
	cmd = a.filepicker.Init()
	cmds = append(cmds, cmd)
	cmd = a.themeDialog.Init()
	cmds = append(cmds, cmd)
	cmd = a.skillsDialog.Init()
	cmds = append(cmds, cmd)
	cmd = a.taskDialog.Init()
	cmds = append(cmds, cmd)

	return tea.Batch(cmds...)
}

func (a appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		msg.Height -= 1 // Make space for the status bar
		a.width, a.height = msg.Width, msg.Height

		s, _ := a.status.Update(msg)
		a.status = s.(core.StatusCmp)
		pageMsg := msg
		pageMsg.Height = a.pageHeight()
		a.pages[a.currentPage], cmd = a.pages[a.currentPage].Update(pageMsg)
		cmds = append(cmds, cmd)

		prm, permCmd := a.permissions.Update(msg)
		a.permissions = prm.(dialog.PermissionDialogCmp)
		cmds = append(cmds, permCmd)

		help, helpCmd := a.help.Update(msg)
		a.help = help.(dialog.HelpCmp)
		cmds = append(cmds, helpCmd)

		session, sessionCmd := a.sessionDialog.Update(msg)
		a.sessionDialog = session.(dialog.SessionDialog)
		cmds = append(cmds, sessionCmd)

		command, commandCmd := a.commandDialog.Update(msg)
		a.commandDialog = command.(dialog.CommandDialog)
		cmds = append(cmds, commandCmd)

		providerSetup, providerSetupCmd := a.providerSetupDialog.Update(msg)
		a.providerSetupDialog = providerSetup.(dialog.ProviderSetupDialog)
		cmds = append(cmds, providerSetupCmd)

		filepicker, filepickerCmd := a.filepicker.Update(msg)
		a.filepicker = filepicker.(dialog.FilepickerCmp)
		cmds = append(cmds, filepickerCmd)

		if a.showMultiArgumentsDialog {
			a.multiArgumentsDialog.SetSize(msg.Width, msg.Height)
			args, argsCmd := a.multiArgumentsDialog.Update(msg)
			a.multiArgumentsDialog = args.(dialog.MultiArgumentsDialogCmp)
			cmds = append(cmds, argsCmd, a.multiArgumentsDialog.Init())
		}

		return a, tea.Batch(cmds...)
	// Status
	case util.InfoMsg:
		s, cmd := a.status.Update(msg)
		a.status = s.(core.StatusCmp)
		cmds = append(cmds, cmd)
		return a, tea.Batch(cmds...)
	case pubsub.Event[logging.LogMessage]:
		if msg.Payload.Persist {
			switch msg.Payload.Level {
			case "error":
				s, cmd := a.status.Update(util.InfoMsg{
					Type: util.InfoTypeError,
					Msg:  msg.Payload.Message,
					TTL:  msg.Payload.PersistTime,
				})
				a.status = s.(core.StatusCmp)
				cmds = append(cmds, cmd)
			case "info":
				s, cmd := a.status.Update(util.InfoMsg{
					Type: util.InfoTypeInfo,
					Msg:  msg.Payload.Message,
					TTL:  msg.Payload.PersistTime,
				})
				a.status = s.(core.StatusCmp)
				cmds = append(cmds, cmd)

			case "warn":
				s, cmd := a.status.Update(util.InfoMsg{
					Type: util.InfoTypeWarn,
					Msg:  msg.Payload.Message,
					TTL:  msg.Payload.PersistTime,
				})

				a.status = s.(core.StatusCmp)
				cmds = append(cmds, cmd)
			default:
				s, cmd := a.status.Update(util.InfoMsg{
					Type: util.InfoTypeInfo,
					Msg:  msg.Payload.Message,
					TTL:  msg.Payload.PersistTime,
				})
				a.status = s.(core.StatusCmp)
				cmds = append(cmds, cmd)
			}
		}
	case util.ClearStatusMsg:
		s, _ := a.status.Update(msg)
		a.status = s.(core.StatusCmp)

	// Permission
	case pubsub.Event[permission.PermissionRequest]:
		a.showPermissions = true
		return a, tea.Batch(a.permissions.SetPermissions(msg.Payload), a.resizeCurrentPage())
	case dialog.PermissionResponseMsg:
		var cmd tea.Cmd
		switch msg.Action {
		case dialog.PermissionAllow:
			a.app.Permissions.Grant(msg.Permission)
		case dialog.PermissionAllowForSession:
			a.app.Permissions.GrantPersistant(msg.Permission)
		case dialog.PermissionDeny:
			a.app.Permissions.Deny(msg.Permission)
		}
		a.showPermissions = false
		return a, tea.Batch(cmd, a.resizeCurrentPage())

	case page.PageChangeMsg:
		return a, a.moveToPage(msg.ID)

	case dialog.CloseQuitMsg:
		a.showQuit = false
		return a, nil

	case dialog.CloseSessionDialogMsg:
		a.showSessionDialog = false
		return a, nil

	case dialog.CloseCommandDialogMsg:
		a.showCommandDialog = false
		return a, nil

	case showSessionDialogMsg:
		return a, a.openSessionDialog()

	case showModelDialogMsg:
		if a.currentPage == page.ChatPage &&
			!a.showQuit &&
			!a.showPermissions &&
			!a.showSessionDialog &&
			!a.showCommandDialog &&
			!a.showTaskDialog &&
			!a.showProviderSetupDialog &&
			!a.showThemeDialog &&
			!a.showFilepicker {
			a.showModelDialog = true
		}
		return a, nil

	case dialog.ShowProviderSetupDialogMsg:
		if a.currentPage != page.ChatPage ||
			a.showQuit ||
			a.showPermissions ||
			a.showSessionDialog ||
			a.showCommandDialog ||
			a.showTaskDialog ||
			a.showThemeDialog ||
			a.showFilepicker {
			return a, nil
		}
		a.providerSetupDialog.Open(msg.Provider, msg.ModelID)
		a.showProviderSetupDialog = true
		a.showModelDialog = false
		return a, nil

	case dialog.CloseProviderSetupDialogMsg:
		a.showProviderSetupDialog = false
		return a, nil

	case dialog.ProviderSetupSavedMsg:
		a.showProviderSetupDialog = false
		model, err := a.app.CoderAgent.Update(config.AgentCoder, msg.Selection.ModelID)
		if err != nil {
			return a, util.ReportWarn(fmt.Sprintf("Saved %s to config, but runtime reload failed: %v", msg.ProviderLabel, err))
		}
		return a, util.ReportInfo(fmt.Sprintf("Configured %s with %s", msg.ProviderLabel, model.Name))

	case dialog.StartCompactSessionMsg:
		// Start compacting the current session
		a.isCompacting = true
		a.compactingMessage = "Starting summarization..."

		if a.selectedSession.ID == "" {
			a.isCompacting = false
			return a, util.ReportWarn("No active session to summarize")
		}

		// Start the summarization process
		return a, func() tea.Msg {
			ctx := context.Background()
			a.app.CoderAgent.Summarize(ctx, a.selectedSession.ID)
			return nil
		}

	case pubsub.Event[agent.AgentEvent]:
		payload := msg.Payload
		if payload.Error != nil {
			a.isCompacting = false
			return a, util.ReportError(payload.Error)
		}

		a.compactingMessage = payload.Progress

		if payload.Done && payload.Type == agent.AgentEventTypeSummarize {
			a.isCompacting = false
			return a, util.ReportInfo("Session summarization complete")
		} else if payload.Done && payload.Type == agent.AgentEventTypeResponse && a.selectedSession.ID != "" {
			model := a.app.CoderAgent.Model()
			contextWindow := model.ContextWindow
			tokens := a.selectedSession.CompletionTokens + a.selectedSession.PromptTokens
			if (tokens >= int64(float64(contextWindow)*0.95)) && config.Get().AutoCompact {
				return a, util.CmdHandler(dialog.StartCompactSessionMsg{})
			}
		}
		// Continue listening for events
		return a, nil

	case dialog.CloseThemeDialogMsg:
		a.showThemeDialog = false
		return a, nil

	case dialog.ShowSkillDialogMsg:
		items, err := a.app.Skills.List(context.Background())
		if err != nil {
			return a, util.ReportError(err)
		}
		a.skillsDialog.SetSkills(items)
		a.showSkillsDialog = true
		return a, nil

	case dialog.CloseSkillDialogMsg:
		a.showSkillsDialog = false
		return a, nil

	case dialog.SkillSelectedMsg:
		a.showSkillsDialog = false
		if a.selectedSession.ID == "" {
			return a, util.ReportWarn("Start a session before activating a skill")
		}
		skill, err := a.app.Skills.Activate(context.Background(), a.selectedSession.ID, msg.Skill.ID)
		if err != nil {
			return a, util.ReportError(err)
		}
		return a, util.ReportInfo(fmt.Sprintf("Activated skill %s", skill.ID))

	case dialog.SkillInstallRequestedMsg:
		skill, err := a.app.Skills.Install(context.Background(), msg.Source)
		if err != nil {
			return a, util.ReportError(err)
		}
		if err := a.reloadSkillsDialog(); err != nil {
			return a, util.ReportError(err)
		}
		return a, util.ReportInfo(fmt.Sprintf("Installed skill %s", skill.ID))

	case dialog.SkillUninstallRequestedMsg:
		if msg.Skill.Source != skills.UserInstalledExtensionName {
			return a, util.ReportWarn("Only user-installed skills can be removed from the TUI")
		}
		if err := a.app.Skills.Uninstall(context.Background(), msg.Skill.ID); err != nil {
			return a, util.ReportError(err)
		}
		if err := a.reloadSkillsDialog(); err != nil {
			return a, util.ReportError(err)
		}
		return a, util.ReportInfo(fmt.Sprintf("Uninstalled skill %s", msg.Skill.ID))

	case dialog.ShowTaskDialogMsg:
		return a, a.openTaskDialog()

	case dialog.CloseTaskDialogMsg:
		a.showTaskDialog = false
		return a, nil

	case dialog.TaskSelectedMsg:
		a.showTaskDialog = false
		return a, util.CmdHandler(chat.SessionSelectedMsg(msg.Session))

	case dialog.ThemeChangedMsg:
		a.pages[a.currentPage], cmd = a.pages[a.currentPage].Update(msg)
		a.showThemeDialog = false
		return a, tea.Batch(cmd, util.ReportInfo("Theme changed to: "+msg.ThemeName))

	case dialog.CloseModelDialogMsg:
		a.showModelDialog = false
		return a, nil

	case dialog.ModelSelectedMsg:
		a.showModelDialog = false

		model, err := a.app.CoderAgent.Update(config.AgentCoder, msg.Model.ID)
		if err != nil {
			return a, util.ReportError(err)
		}

		return a, util.ReportInfo(fmt.Sprintf("Model changed to %s", model.Name))

	case chat.SessionSelectedMsg:
		a.selectedSession = msg
		a.sessionDialog.SetSelectedSession(msg.ID)

	case pubsub.Event[session.Session]:
		if msg.Type == pubsub.UpdatedEvent && msg.Payload.ID == a.selectedSession.ID {
			a.selectedSession = msg.Payload
		}
	case dialog.SessionSelectedMsg:
		a.showSessionDialog = false
		if a.currentPage == page.ChatPage {
			return a, util.CmdHandler(chat.SessionSelectedMsg(msg.Session))
		}
		return a, nil

	case dialog.CommandSelectedMsg:
		if msg.Command.Disabled {
			a.showCommandDialog = true
			reason := strings.TrimSpace(msg.Command.Reason)
			if reason == "" {
				reason = "This command is not available in the current context"
			}
			return a, util.ReportWarn(reason)
		}
		a.showCommandDialog = false
		// Execute the command handler if available
		if msg.Command.Handler != nil {
			return a, msg.Command.Handler(msg.Command)
		}
		return a, util.ReportInfo("Command selected: " + msg.Command.Title)

	case focusInspectorGlobalMsg:
		return a, util.CmdHandler(dialog.ShowTaskDialogMsg{})

	case core.StatusFocusInspectorMsg:
		return a, util.CmdHandler(dialog.ShowTaskDialogMsg{})

	case core.StatusOpenTaskMsg:
		selectedSession, err := a.app.Sessions.Get(context.Background(), msg.SessionID)
		if err != nil {
			return a, util.ReportError(err)
		}
		moveCmd := a.moveToPage(page.ChatPage)
		return a, tea.Batch(moveCmd, util.CmdHandler(chat.SessionSelectedMsg(selectedSession)))

	case openLatestTaskGlobalMsg:
		sessionID, title, err := a.latestTaskContext(false)
		if err != nil {
			return a, util.ReportError(err)
		}
		if sessionID == "" {
			return a, util.ReportWarn("No delegated task available to open")
		}
		moveCmd := a.moveToPage(page.ChatPage)
		return a, tea.Batch(
			moveCmd,
			util.CmdHandler(chat.InspectorOpenTaskMsg{SessionID: sessionID}),
			util.ReportInfo("Opening latest task: "+title),
		)

	case stopLatestTaskGlobalMsg:
		sessionID, title, err := a.latestTaskContext(true)
		if err != nil {
			return a, util.ReportError(err)
		}
		if sessionID == "" {
			return a, util.ReportWarn("No running delegated task available to stop")
		}
		moveCmd := a.moveToPage(page.ChatPage)
		return a, tea.Batch(
			moveCmd,
			util.CmdHandler(chat.InspectorStopTaskMsg{SessionID: sessionID}),
			util.ReportInfo("Stopping latest running task: "+title),
		)

	case dialog.ShowMultiArgumentsDialogMsg:
		// Show multi-arguments dialog
		a.multiArgumentsDialog = dialog.NewMultiArgumentsDialogCmp(msg.CommandID, msg.Content, msg.ArgNames)
		a.showMultiArgumentsDialog = true
		return a, a.multiArgumentsDialog.Init()

	case dialog.CloseMultiArgumentsDialogMsg:
		// Close multi-arguments dialog
		a.showMultiArgumentsDialog = false

		// If submitted, replace all named arguments and run the command
		if msg.Submit {
			content := msg.Content

			// Replace each named argument with its value
			for name, value := range msg.Args {
				placeholder := "$" + name
				content = strings.ReplaceAll(content, placeholder, value)
			}

			// Execute the command with arguments
			return a, util.CmdHandler(dialog.CommandRunCustomMsg{
				Content: content,
				Args:    msg.Args,
			})
		}
		return a, nil

	case tea.KeyMsg:
		// If multi-arguments dialog is open, let it handle the key press first
		if a.showMultiArgumentsDialog {
			args, cmd := a.multiArgumentsDialog.Update(msg)
			a.multiArgumentsDialog = args.(dialog.MultiArgumentsDialogCmp)
			return a, cmd
		}
		switch {

		case key.Matches(msg, keys.Quit):
			a.showQuit = !a.showQuit
			if a.showHelp {
				a.showHelp = false
			}
			if a.showSessionDialog {
				a.showSessionDialog = false
			}
			if a.showCommandDialog {
				a.showCommandDialog = false
			}
			if a.showProviderSetupDialog {
				a.showProviderSetupDialog = false
			}
			if a.showFilepicker {
				a.showFilepicker = false
				a.filepicker.ToggleFilepicker(a.showFilepicker)
			}
			if a.showModelDialog {
				a.showModelDialog = false
			}
			if a.showSkillsDialog {
				a.showSkillsDialog = false
			}
			if a.showTaskDialog {
				a.showTaskDialog = false
			}
			if a.showMultiArgumentsDialog {
				a.showMultiArgumentsDialog = false
			}
			return a, nil
		case key.Matches(msg, keys.SwitchSession):
			return a, a.openSessionDialog()
		case key.Matches(msg, keys.Commands):
			if a.currentPage == page.ChatPage && !a.showQuit && !a.showPermissions && !a.showSessionDialog && !a.showThemeDialog && !a.showFilepicker && !a.showTaskDialog {
				// Show commands dialog
				if len(a.commands) == 0 {
					return a, util.ReportWarn("No commands available")
				}
				a.commandDialog.SetCommands(a.contextualCommands())
				a.showCommandDialog = true
				return a, nil
			}
			return a, nil
		case key.Matches(msg, keys.Models):
			if a.showModelDialog {
				a.showModelDialog = false
				return a, nil
			}
			return a, util.CmdHandler(showModelDialogMsg{})
		case key.Matches(msg, keys.SwitchTheme):
			if !a.showQuit && !a.showPermissions && !a.showSessionDialog && !a.showCommandDialog && !a.showTaskDialog {
				// Show theme switcher dialog
				a.showThemeDialog = true
				// Theme list is dynamically loaded by the dialog component
				return a, a.themeDialog.Init()
			}
			return a, nil
		case key.Matches(msg, keys.PrevTask):
			s, cmd := a.status.Update(core.StatusCycleTaskMsg{Delta: -1})
			a.status = s.(core.StatusCmp)
			return a, cmd
		case key.Matches(msg, keys.NextTask):
			s, cmd := a.status.Update(core.StatusCycleTaskMsg{Delta: 1})
			a.status = s.(core.StatusCmp)
			return a, cmd
		case key.Matches(msg, returnKey) || key.Matches(msg):
			if msg.String() == quitKey {
				if a.currentPage == page.LogsPage {
					return a, a.moveToPage(page.ChatPage)
				}
			} else if !a.filepicker.IsCWDFocused() {
				if a.showQuit {
					a.showQuit = !a.showQuit
					return a, nil
				}
				if a.showHelp {
					a.showHelp = !a.showHelp
					return a, nil
				}
				if a.showFilepicker {
					a.showFilepicker = false
					a.filepicker.ToggleFilepicker(a.showFilepicker)
					return a, nil
				}
				if a.currentPage == page.LogsPage {
					return a, a.moveToPage(page.ChatPage)
				}
			}
		case key.Matches(msg, keys.Logs):
			return a, a.moveToPage(page.LogsPage)
		case key.Matches(msg, keys.Help):
			if a.showQuit {
				return a, nil
			}
			a.showHelp = !a.showHelp
			return a, nil
		case key.Matches(msg, helpEsc):
			if a.app.CoderAgent.IsBusy() {
				if a.showQuit {
					return a, nil
				}
				a.showHelp = !a.showHelp
				return a, nil
			}
		case key.Matches(msg, keys.Filepicker):
			a.showFilepicker = !a.showFilepicker
			a.filepicker.ToggleFilepicker(a.showFilepicker)
			return a, nil
		}
	default:
		f, filepickerCmd := a.filepicker.Update(msg)
		a.filepicker = f.(dialog.FilepickerCmp)
		cmds = append(cmds, filepickerCmd)

	}

	if a.showFilepicker {
		f, filepickerCmd := a.filepicker.Update(msg)
		a.filepicker = f.(dialog.FilepickerCmp)
		cmds = append(cmds, filepickerCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showQuit {
		q, quitCmd := a.quit.Update(msg)
		a.quit = q.(dialog.QuitDialog)
		cmds = append(cmds, quitCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}
	if a.showPermissions {
		d, permissionsCmd := a.permissions.Update(msg)
		a.permissions = d.(dialog.PermissionDialogCmp)
		cmds = append(cmds, permissionsCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showSessionDialog {
		d, sessionCmd := a.sessionDialog.Update(msg)
		a.sessionDialog = d.(dialog.SessionDialog)
		cmds = append(cmds, sessionCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showCommandDialog {
		d, commandCmd := a.commandDialog.Update(msg)
		a.commandDialog = d.(dialog.CommandDialog)
		cmds = append(cmds, commandCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showProviderSetupDialog {
		d, providerSetupCmd := a.providerSetupDialog.Update(msg)
		a.providerSetupDialog = d.(dialog.ProviderSetupDialog)
		cmds = append(cmds, providerSetupCmd)
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showModelDialog {
		d, modelCmd := a.modelDialog.Update(msg)
		a.modelDialog = d.(dialog.ModelDialog)
		cmds = append(cmds, modelCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showThemeDialog {
		d, themeCmd := a.themeDialog.Update(msg)
		a.themeDialog = d.(dialog.ThemeDialog)
		cmds = append(cmds, themeCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showSkillsDialog {
		d, skillsCmd := a.skillsDialog.Update(msg)
		a.skillsDialog = d.(dialog.SkillDialog)
		cmds = append(cmds, skillsCmd)
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showTaskDialog {
		d, taskCmd := a.taskDialog.Update(msg)
		a.taskDialog = d.(dialog.TaskDialog)
		cmds = append(cmds, taskCmd)
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	s, _ := a.status.Update(msg)
	a.status = s.(core.StatusCmp)
	a.pages[a.currentPage], cmd = a.pages[a.currentPage].Update(msg)
	cmds = append(cmds, cmd)
	return a, tea.Batch(cmds...)
}

// RegisterCommand adds a command to the command dialog
func (a *appModel) RegisterCommand(cmd dialog.Command) {
	a.commands = append(a.commands, cmd)
}

func (a *appModel) findCommand(id string) (dialog.Command, bool) {
	for _, cmd := range a.commands {
		if cmd.ID == id {
			return cmd, true
		}
	}
	return dialog.Command{}, false
}

func (a *appModel) openSessionDialog() tea.Cmd {
	if a.currentPage != page.ChatPage ||
		a.showQuit ||
		a.showPermissions ||
		a.showCommandDialog ||
		a.showTaskDialog ||
		a.showProviderSetupDialog ||
		a.showThemeDialog ||
		a.showFilepicker ||
		a.showModelDialog {
		return nil
	}

	sessions, err := a.app.Sessions.List(context.Background())
	if err != nil {
		return util.ReportError(err)
	}
	if len(sessions) == 0 {
		return util.ReportWarn("No sessions available")
	}
	a.sessionDialog.SetSessions(sessions)
	a.showSessionDialog = true
	return nil
}

func (a *appModel) openTaskDialog() tea.Cmd {
	if a.currentPage != page.ChatPage ||
		a.showQuit ||
		a.showPermissions ||
		a.showCommandDialog ||
		a.showSessionDialog ||
		a.showProviderSetupDialog ||
		a.showThemeDialog ||
		a.showFilepicker ||
		a.showModelDialog {
		return nil
	}
	if a.selectedSession.ID == "" {
		return util.ReportWarn("Start or select a session before inspecting delegated tasks")
	}
	tasks, err := a.app.Sessions.ListChildren(context.Background(), a.selectedSession.ID)
	if err != nil {
		return util.ReportError(err)
	}
	a.taskDialog.SetParentSession(a.selectedSession)
	a.taskDialog.SetTasks(tasks)
	a.showTaskDialog = true
	return nil
}

func (a *appModel) reloadSkillsDialog() error {
	items, err := a.app.Skills.List(context.Background())
	if err != nil {
		return err
	}
	a.skillsDialog.SetSkills(items)
	a.showSkillsDialog = true
	return nil
}

func (a *appModel) latestTaskContext(requireRunning bool) (string, string, error) {
	if a.selectedSession.ID == "" {
		return "", "", nil
	}

	contextID := a.selectedSession.ID
	if a.selectedSession.ParentSessionID != "" {
		contextID = a.selectedSession.ParentSessionID
	}

	runs := a.app.TaskRuns.Snapshot(contextID)
	if len(runs) == 0 {
		return "", "", nil
	}

	if requireRunning {
		for _, run := range runs {
			if run.Status == taskrun.StatusRunning {
				return run.SessionID, fallbackCommandTaskTitle(run.Title), nil
			}
		}
		return "", "", nil
	}

	return runs[0].SessionID, fallbackCommandTaskTitle(runs[0].Title), nil
}

func fallbackCommandTaskTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "latest task"
	}
	return title
}

func (a *appModel) moveToPage(pageID page.PageID) tea.Cmd {
	if a.app.CoderAgent.IsBusy() {
		// For now we don't move to any page if the agent is busy
		return util.ReportWarn("Agent is busy, please wait...")
	}

	var cmds []tea.Cmd
	if _, ok := a.loadedPages[pageID]; !ok {
		cmd := a.pages[pageID].Init()
		cmds = append(cmds, cmd)
		a.loadedPages[pageID] = true
	}
	a.previousPage = a.currentPage
	a.currentPage = pageID
	if sizable, ok := a.pages[a.currentPage].(layout.Sizeable); ok {
		cmd := sizable.SetSize(a.width, a.pageHeight())
		cmds = append(cmds, cmd)
	}

	return tea.Batch(cmds...)
}

func (a appModel) View() string {
	components := []string{a.pages[a.currentPage].View()}
	if a.showPermissions {
		components = append(components, a.permissions.View())
	}
	components = append(components, a.status.View())

	appView := lipgloss.JoinVertical(lipgloss.Top, components...)

	if a.showFilepicker {
		overlay := a.filepicker.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)

	}

	// Show compacting status overlay
	if a.isCompacting {
		t := theme.CurrentTheme()
		style := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(t.BorderFocused()).
			BorderBackground(t.Background()).
			Padding(1, 2).
			Background(t.Background()).
			Foreground(t.Text())

		overlay := style.Render("Summarizing\n" + a.compactingMessage)
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showHelp {
		bindings := layout.KeyMapToSlice(keys)
		if p, ok := a.pages[a.currentPage].(layout.Bindings); ok {
			bindings = append(bindings, p.BindingKeys()...)
		}
		if a.showPermissions {
			bindings = append(bindings, a.permissions.BindingKeys()...)
		}
		if a.showSessionDialog {
			bindings = append(bindings, a.sessionDialog.BindingKeys()...)
		}
		if a.showCommandDialog {
			bindings = append(bindings, a.commandDialog.BindingKeys()...)
		}
		if a.showProviderSetupDialog {
			bindings = append(bindings, a.providerSetupDialog.BindingKeys()...)
		}
		if a.showModelDialog {
			bindings = append(bindings, a.modelDialog.BindingKeys()...)
		}
		if a.showThemeDialog {
			bindings = append(bindings, a.themeDialog.BindingKeys()...)
		}
		if a.showSkillsDialog {
			bindings = append(bindings, a.skillsDialog.BindingKeys()...)
		}
		if a.showTaskDialog {
			bindings = append(bindings, a.taskDialog.BindingKeys()...)
		}
		if a.currentPage == page.LogsPage {
			bindings = append(bindings, logsKeyReturnKey)
		}
		if !a.app.CoderAgent.IsBusy() {
			bindings = append(bindings, helpEsc)
		}
		a.help.SetBindings(bindings)

		overlay := a.help.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showQuit {
		overlay := a.quit.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showSessionDialog {
		overlay := a.sessionDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showProviderSetupDialog {
		overlay := a.providerSetupDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showModelDialog {
		overlay := a.modelDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showCommandDialog {
		overlay := a.commandDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showThemeDialog {
		overlay := a.themeDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showSkillsDialog {
		overlay := a.skillsDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(col, row, overlay, appView, true)
	}

	if a.showTaskDialog {
		overlay := a.taskDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(col, row, overlay, appView, true)
	}

	if a.showMultiArgumentsDialog {
		overlay := a.multiArgumentsDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	return zone.Scan(appView)
}

func New(app *app.App) tea.Model {
	startPage := page.ChatPage
	model := &appModel{
		currentPage:         startPage,
		loadedPages:         make(map[page.PageID]bool),
		status:              core.NewStatusCmp(app.LSPClients, app.Skills, app.Permissions, app.TaskRuns),
		help:                dialog.NewHelpCmp(),
		quit:                dialog.NewQuitCmp(),
		sessionDialog:       dialog.NewSessionDialogCmp(),
		commandDialog:       dialog.NewCommandDialogCmp(),
		modelDialog:         dialog.NewModelDialogCmp(),
		providerSetupDialog: dialog.NewProviderSetupDialogCmp(),
		permissions:         dialog.NewPermissionDialogCmp(),
		themeDialog:         dialog.NewThemeDialogCmp(),
		skillsDialog:        dialog.NewSkillDialogCmp(),
		taskDialog:          dialog.NewTaskDialogCmp(),
		app:                 app,
		commands:            []dialog.Command{},
		pages: map[page.PageID]tea.Model{
			page.ChatPage: page.NewChatPage(app),
			page.LogsPage: page.NewLogsPage(),
		},
		filepicker: dialog.NewFilepickerCmp(app),
	}

	model.RegisterCommand(dialog.Command{
		ID:          "focus-inspector",
		Title:       "Open Tasks",
		Description: "Open the delegated task list",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(focusInspectorGlobalMsg{})
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          "open-latest-task",
		Title:       "Open Latest Task",
		Description: "Jump into the most recently updated delegated task",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(openLatestTaskGlobalMsg{})
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          "stop-latest-task",
		Title:       "Stop Latest Running Task",
		Description: "Send a stop signal to the most recent running delegated task",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(stopLatestTaskGlobalMsg{})
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          "project-memory",
		Title:       "Generate Project Memory",
		Description: "Create or refresh the SCICLI.md memory file",
		Handler: func(cmd dialog.Command) tea.Cmd {
			prompt := `Please analyze this codebase and create a SCICLI.md file containing:
1. Build/lint/test commands - especially for running a single test
2. Code style guidelines including imports, formatting, types, naming conventions, error handling, etc.

The file you create will be given to agentic coding agents (such as yourself) that operate in this repository. Make it about 20 lines long.
If there's already a scicli.md or SCICLI.md, improve it.
If there are Cursor rules (in .cursor/rules/ or .cursorrules) or Copilot rules (in .github/copilot-instructions.md), make sure to include them.`
			return tea.Batch(
				util.CmdHandler(chat.SendMsg{
					Text: prompt,
				}),
			)
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          "set-research-objective",
		Title:       "Set Research Objective",
		Description: "Create or update the structured research objective for this session",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(dialog.ShowMultiArgumentsDialogMsg{
				CommandID: cmd.ID,
				Content:   "/research set $objective",
				ArgNames:  []string{"objective"},
			})
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          "show-research-state",
		Title:       "Show Research State",
		Description: "Print the persisted research objective, stage, and experiment summary for this session",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(dialog.CommandRunCustomMsg{
				Content: "/research show",
			})
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          "add-experiment-plan",
		Title:       "Add Experiment Plan",
		Description: "Create a machine-readable experiment plan under the current research session",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(dialog.ShowMultiArgumentsDialogMsg{
				CommandID: cmd.ID,
				Content:   "/experiment add $title",
				ArgNames:  []string{"title"},
			})
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          "evaluate-active-experiment",
		Title:       "Evaluate Active Experiment",
		Description: "Store a score and decision for the active experiment plan",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(dialog.ShowMultiArgumentsDialogMsg{
				CommandID: cmd.ID,
				Content:   "/experiment evaluate $score $decision $summary",
				ArgNames:  []string{"score", "decision", "summary"},
			})
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          "promote-active-experiment",
		Title:       "Promote Active Experiment",
		Description: "Mark the active experiment as the current best candidate if it has a valid evaluation",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(dialog.CommandRunCustomMsg{
				Content: "/experiment promote",
			})
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          "propose-next-experiment",
		Title:       "Propose Next Experiment",
		Description: "Ask the agent to generate the next candidate experiment from current research state",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(dialog.CommandRunCustomMsg{
				Content: "/experiment propose",
			})
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          "evolve-active-experiment",
		Title:       "Evolve Active Experiment",
		Description: "Create the next generation from the active mutate/branch candidate using the default lineage title",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(dialog.CommandRunCustomMsg{
				Content: "/experiment evolve",
			})
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          "rerun-active-experiment",
		Title:       "Rerun Active Experiment",
		Description: "Clone the active experiment into a fresh rerun candidate with lineage",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(dialog.CommandRunCustomMsg{
				Content: "/experiment rerun",
			})
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          "compare-experiments",
		Title:       "Compare Experiments",
		Description: "Show a compact comparison of experiment outcomes and lineage",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(dialog.CommandRunCustomMsg{
				Content: "/experiment compare",
			})
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          "list-experiment-artifacts",
		Title:       "List Experiment Artifacts",
		Description: "Show the captured provenance artifacts for the active experiment run",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(dialog.CommandRunCustomMsg{
				Content: "/artifact list",
			})
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          "compact",
		Title:       "Compact Session",
		Description: "Summarize the current session and create a new one with the summary",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return func() tea.Msg {
				return dialog.StartCompactSessionMsg{}
			}
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          "skills",
		Title:       "Skills",
		Description: "Browse and activate available skills",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(dialog.ShowSkillDialogMsg{})
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          "tasks",
		Title:       "Delegated Tasks",
		Description: "Inspect delegated child-agent sessions for the current chat",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(dialog.ShowTaskDialogMsg{})
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          "ultrawork",
		Title:       "Enable Ultrawork",
		Description: "Switch the current runtime to ultrawork mode",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return func() tea.Msg {
				if err := config.SetWorkMode(config.WorkModeUltrawork, false); err != nil {
					return util.InfoMsg{Type: util.InfoTypeError, Msg: err.Error()}
				}
				return util.InfoMsg{Type: util.InfoTypeInfo, Msg: "Ultrawork mode enabled"}
			}
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          "parent-session",
		Title:       "Go To Parent Session",
		Description: "Jump from a delegated task session back to its parent chat",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return func() tea.Msg {
				if model.selectedSession.ParentSessionID == "" {
					return util.InfoMsg{Type: util.InfoTypeWarn, Msg: "Current session has no parent"}
				}
				parentSession, err := app.Sessions.Get(context.Background(), model.selectedSession.ParentSessionID)
				if err != nil {
					return util.InfoMsg{Type: util.InfoTypeError, Msg: err.Error()}
				}
				return chat.SessionSelectedMsg(parentSession)
			}
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          "switch-session",
		Title:       "Switch Session",
		Description: "Browse and switch saved sessions",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(showSessionDialogMsg{})
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          "switch-provider-model",
		Title:       "Switch Provider / Model",
		Description: "Choose a provider and model for the coder agent, including providers that still need setup",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(showModelDialogMsg{})
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          "configure-provider",
		Title:       "Configure Provider",
		Description: "Add or edit API key, base URL, and custom model for any provider",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(dialog.ShowProviderSetupDialogMsg{})
		},
	})
	// Load custom commands
	customCommands, err := dialog.LoadCustomCommands()
	if err != nil {
		logging.Warn("Failed to load custom commands", "error", err)
	} else {
		for _, cmd := range customCommands {
			model.RegisterCommand(cmd)
		}
	}

	return model
}
