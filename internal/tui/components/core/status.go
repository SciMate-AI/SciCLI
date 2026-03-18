package core

import (
	"fmt"
	"strings"
	"time"

	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/SciMate-AI/scicli/internal/llm/models"
	"github.com/SciMate-AI/scicli/internal/lsp"
	"github.com/SciMate-AI/scicli/internal/lsp/protocol"
	"github.com/SciMate-AI/scicli/internal/permission"
	"github.com/SciMate-AI/scicli/internal/pubsub"
	"github.com/SciMate-AI/scicli/internal/session"
	"github.com/SciMate-AI/scicli/internal/skills"
	"github.com/SciMate-AI/scicli/internal/taskrun"
	"github.com/SciMate-AI/scicli/internal/tui/components/chat"
	"github.com/SciMate-AI/scicli/internal/tui/styles"
	"github.com/SciMate-AI/scicli/internal/tui/theme"
	"github.com/SciMate-AI/scicli/internal/tui/util"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
)

type StatusCmp interface {
	tea.Model
}

type StatusCycleTaskMsg struct {
	Delta int
}

type StatusOpenTaskMsg struct {
	SessionID string
}

type StatusFocusInspectorMsg struct{}

const (
	statusTaskZoneID      = "status-task-chip"
	statusInspectorZoneID = "status-inspector-chip"
)

type statusCmp struct {
	info             util.InfoMsg
	width            int
	messageTTL       time.Duration
	lspClients       map[string]*lsp.Client
	session          session.Session
	skillsSvc        skills.Service
	permission       permission.Service
	taskRuns         taskrun.Service
	inspectorFocused bool
	taskCursor       int
}

// clearMessageCmd is a command that clears status messages after a timeout
func (m statusCmp) clearMessageCmd(ttl time.Duration) tea.Cmd {
	return tea.Tick(ttl, func(time.Time) tea.Msg {
		return util.ClearStatusMsg{}
	})
}

func (m statusCmp) Init() tea.Cmd {
	return nil
}

func (m statusCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case chat.SessionSelectedMsg:
		m.session = msg
		m.taskCursor = 0
	case chat.SessionClearedMsg:
		m.session = session.Session{}
		m.taskCursor = 0
	case chat.InspectorFocusMsg:
		m.inspectorFocused = msg.Focused
	case StatusCycleTaskMsg:
		m.shiftTaskCursor(msg.Delta)
		return m, nil
	case pubsub.Event[session.Session]:
		if msg.Type == pubsub.UpdatedEvent {
			if m.session.ID == msg.Payload.ID {
				m.session = msg.Payload
			}
		}
	case pubsub.Event[taskrun.Run]:
		return m, nil
	case tea.MouseMsg:
		if !zone.Get(statusTaskZoneID).InBounds(msg) && !zone.Get(statusInspectorZoneID).InBounds(msg) {
			return m, nil
		}
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		if zone.Get(statusInspectorZoneID).InBounds(msg) {
			return m, util.CmdHandler(StatusFocusInspectorMsg{})
		}
		if run, ok := m.selectedTaskRun(); ok {
			return m, util.CmdHandler(StatusOpenTaskMsg{SessionID: run.SessionID})
		}
	case util.InfoMsg:
		m.info = msg
		ttl := msg.TTL
		if ttl == 0 {
			ttl = m.messageTTL
		}
		return m, m.clearMessageCmd(ttl)
	case util.ClearStatusMsg:
		m.info = util.InfoMsg{}
	}
	return m, nil
}

var helpWidget = ""

// getHelpWidget returns the help widget with current theme colors
func getHelpWidget() string {
	t := theme.CurrentTheme()
	helpText := "ctrl+? help"

	return styles.Padded().
		Background(t.TextMuted()).
		Foreground(t.BackgroundDarker()).
		Bold(true).
		Render(helpText)
}

func formatTokensAndCost(tokens, contextWindow int64, cost float64) string {
	// Format tokens in human-readable format (e.g., 110K, 1.2M)
	var formattedTokens string
	switch {
	case tokens >= 1_000_000:
		formattedTokens = fmt.Sprintf("%.1fM", float64(tokens)/1_000_000)
	case tokens >= 1_000:
		formattedTokens = fmt.Sprintf("%.1fK", float64(tokens)/1_000)
	default:
		formattedTokens = fmt.Sprintf("%d", tokens)
	}

	// Remove .0 suffix if present
	if strings.HasSuffix(formattedTokens, ".0K") {
		formattedTokens = strings.Replace(formattedTokens, ".0K", "K", 1)
	}
	if strings.HasSuffix(formattedTokens, ".0M") {
		formattedTokens = strings.Replace(formattedTokens, ".0M", "M", 1)
	}

	// Format cost with $ symbol and 2 decimal places
	formattedCost := fmt.Sprintf("$%.2f", cost)

	percentage := (float64(tokens) / float64(contextWindow)) * 100
	if percentage > 80 {
		// add the warning icon and percentage
		formattedTokens = fmt.Sprintf("%s(%d%%)", styles.WarningIcon, int(percentage))
	}

	return fmt.Sprintf("Context: %s, Cost: %s", formattedTokens, formattedCost)
}

func (m statusCmp) View() string {
	t := theme.CurrentTheme()
	modelID := config.Get().Agents[config.AgentCoder].Model
	model := models.SupportedModels[modelID]

	// Initialize the help widget
	status := getHelpWidget()
	modeWidget := m.workMode()
	skillWidget := ""
	if m.session.ID != "" && m.skillsSvc != nil {
		skillWidget = m.skillSummary()
	}
	inspectorWidget := m.inspectorSummary()
	taskWidget := m.taskSummary()
	approvalWidget := ""
	if m.permission != nil && m.permission.PendingCount() > 0 {
		approvalWidget = m.pendingApprovals()
	}

	tokenInfoWidth := 0
	if m.session.ID != "" {
		totalTokens := m.session.PromptTokens + m.session.CompletionTokens
		tokens := formatTokensAndCost(totalTokens, model.ContextWindow, m.session.Cost)
		tokensStyle := styles.Padded().
			Background(t.Text()).
			Foreground(t.BackgroundSecondary())
		percentage := (float64(totalTokens) / float64(model.ContextWindow)) * 100
		if percentage > 80 {
			tokensStyle = tokensStyle.Background(t.Warning())
		}
		tokenInfoWidth = lipgloss.Width(tokens) + 2
		status += tokensStyle.Render(tokens)
	}

	diagnostics := styles.Padded().
		Background(t.BackgroundDarker()).
		Render(m.projectDiagnostics())

	availableWidht := max(0, m.width-lipgloss.Width(helpWidget)-lipgloss.Width(m.model())-lipgloss.Width(modeWidget)-lipgloss.Width(skillWidget)-lipgloss.Width(inspectorWidget)-lipgloss.Width(taskWidget)-lipgloss.Width(approvalWidget)-lipgloss.Width(diagnostics)-tokenInfoWidth)

	if m.info.Msg != "" {
		infoStyle := styles.Padded().
			Foreground(t.Background()).
			Width(availableWidht)

		switch m.info.Type {
		case util.InfoTypeInfo:
			infoStyle = infoStyle.Background(t.Info())
		case util.InfoTypeWarn:
			infoStyle = infoStyle.Background(t.Warning())
		case util.InfoTypeError:
			infoStyle = infoStyle.Background(t.Error())
		}

		infoWidth := availableWidht - 10
		// Truncate message if it's longer than available width
		msg := m.info.Msg
		if len(msg) > infoWidth && infoWidth > 0 {
			msg = msg[:infoWidth] + "..."
		}
		status += infoStyle.Render(msg)
	} else {
		status += styles.Padded().
			Foreground(t.Text()).
			Background(t.BackgroundSecondary()).
			Width(availableWidht).
			Render(truncateString("cwd: "+config.WorkingDirectory(), availableWidht-2))
	}

	status += diagnostics
	status += taskWidget
	status += inspectorWidget
	status += skillWidget
	status += approvalWidget
	status += modeWidget
	status += m.model()
	return status
}

func (m *statusCmp) projectDiagnostics() string {
	t := theme.CurrentTheme()

	// Check if any LSP server is still initializing
	initializing := false
	for _, client := range m.lspClients {
		if client.GetServerState() == lsp.StateStarting {
			initializing = true
			break
		}
	}

	// If any server is initializing, show that status
	if initializing {
		return lipgloss.NewStyle().
			Background(t.BackgroundDarker()).
			Foreground(t.Warning()).
			Render(fmt.Sprintf("%s Initializing LSP...", styles.SpinnerIcon))
	}

	errorDiagnostics := []protocol.Diagnostic{}
	warnDiagnostics := []protocol.Diagnostic{}
	hintDiagnostics := []protocol.Diagnostic{}
	infoDiagnostics := []protocol.Diagnostic{}
	for _, client := range m.lspClients {
		for _, d := range client.GetDiagnostics() {
			for _, diag := range d {
				switch diag.Severity {
				case protocol.SeverityError:
					errorDiagnostics = append(errorDiagnostics, diag)
				case protocol.SeverityWarning:
					warnDiagnostics = append(warnDiagnostics, diag)
				case protocol.SeverityHint:
					hintDiagnostics = append(hintDiagnostics, diag)
				case protocol.SeverityInformation:
					infoDiagnostics = append(infoDiagnostics, diag)
				}
			}
		}
	}

	if len(errorDiagnostics) == 0 && len(warnDiagnostics) == 0 && len(hintDiagnostics) == 0 && len(infoDiagnostics) == 0 {
		return "No diagnostics"
	}

	diagnostics := []string{}

	if len(errorDiagnostics) > 0 {
		errStr := lipgloss.NewStyle().
			Background(t.BackgroundDarker()).
			Foreground(t.Error()).
			Render(fmt.Sprintf("%s %d", styles.ErrorIcon, len(errorDiagnostics)))
		diagnostics = append(diagnostics, errStr)
	}
	if len(warnDiagnostics) > 0 {
		warnStr := lipgloss.NewStyle().
			Background(t.BackgroundDarker()).
			Foreground(t.Warning()).
			Render(fmt.Sprintf("%s %d", styles.WarningIcon, len(warnDiagnostics)))
		diagnostics = append(diagnostics, warnStr)
	}
	if len(hintDiagnostics) > 0 {
		hintStr := lipgloss.NewStyle().
			Background(t.BackgroundDarker()).
			Foreground(t.Text()).
			Render(fmt.Sprintf("%s %d", styles.HintIcon, len(hintDiagnostics)))
		diagnostics = append(diagnostics, hintStr)
	}
	if len(infoDiagnostics) > 0 {
		infoStr := lipgloss.NewStyle().
			Background(t.BackgroundDarker()).
			Foreground(t.Info()).
			Render(fmt.Sprintf("%s %d", styles.InfoIcon, len(infoDiagnostics)))
		diagnostics = append(diagnostics, infoStr)
	}

	return strings.Join(diagnostics, " ")
}

func (m statusCmp) availableFooterMsgWidth(diagnostics, tokenInfo string) int {
	tokensWidth := 0
	if m.session.ID != "" {
		tokensWidth = lipgloss.Width(tokenInfo) + 2
	}
	return max(0, m.width-lipgloss.Width(helpWidget)-lipgloss.Width(m.model())-lipgloss.Width(diagnostics)-tokensWidth)
}

func (m statusCmp) model() string {
	t := theme.CurrentTheme()

	cfg := config.Get()

	coder, ok := cfg.Agents[config.AgentCoder]
	if !ok {
		return "Unknown"
	}
	model := models.SupportedModels[coder.Model]

	return styles.Padded().
		Background(t.Secondary()).
		Foreground(t.Background()).
		Render(model.Name)
}

func (m statusCmp) workMode() string {
	t := theme.CurrentTheme()
	return styles.Padded().
		Background(t.Primary()).
		Foreground(t.Background()).
		Render("Mode: " + string(config.Get().Automation.WorkMode))
}

func (m statusCmp) skillSummary() string {
	t := theme.CurrentTheme()
	return styles.Padded().
		Background(t.BackgroundDarker()).
		Foreground(t.Text()).
		Render(fmt.Sprintf("Skills: %d", len(m.skillsSvc.Active(m.session.ID))))
}

func (m statusCmp) inspectorSummary() string {
	t := theme.CurrentTheme()
	label := "Inspector: Ready"
	bg := t.BackgroundDarker()
	fg := t.Text()
	if m.inspectorFocused {
		label = "Inspector: Focused"
		bg = t.Primary()
		fg = t.Background()
	}
	return zone.Mark(statusInspectorZoneID, styles.Padded().
		Background(bg).
		Foreground(fg).
		Render(label))
}

func (m statusCmp) taskSummary() string {
	if m.taskRuns == nil {
		return ""
	}
	contextID := m.taskContextSessionID()
	if contextID == "" {
		return ""
	}
	runs := m.taskRuns.Snapshot(contextID)
	if len(runs) == 0 {
		return ""
	}

	running := 0
	for _, run := range runs {
		if run.Status == taskrun.StatusRunning {
			running++
		}
	}

	selected := runs[m.clampedTaskCursor(len(runs))]
	title := fallbackTaskTitle(selected.Title)
	label := fmt.Sprintf("Task %d/%d", m.clampedTaskCursor(len(runs))+1, len(runs))
	if running > 0 {
		label += fmt.Sprintf(" Run:%d", running)
	}
	label += " " + strings.ToUpper(string(selected.Status))
	label += " " + truncateString(title, 18)

	t := theme.CurrentTheme()
	bg := t.BackgroundDarker()
	fg := t.Text()
	if selected.Status == taskrun.StatusRunning || running > 0 {
		bg = t.Secondary()
		fg = t.Background()
	}
	return zone.Mark(statusTaskZoneID, styles.Padded().
		Background(bg).
		Foreground(fg).
		Render(label))
}

func (m statusCmp) taskContextSessionID() string {
	if m.session.ID == "" {
		return ""
	}
	if m.session.ParentSessionID != "" {
		return m.session.ParentSessionID
	}
	return m.session.ID
}

func fallbackTaskTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "latest"
	}
	return title
}

func (m *statusCmp) shiftTaskCursor(delta int) {
	runs := m.currentTaskRuns()
	if len(runs) == 0 || delta == 0 {
		return
	}
	m.taskCursor += delta
	if m.taskCursor < 0 {
		m.taskCursor = len(runs) - 1
	}
	if m.taskCursor >= len(runs) {
		m.taskCursor = 0
	}
}

func (m statusCmp) clampedTaskCursor(length int) int {
	if length <= 0 {
		return 0
	}
	if m.taskCursor < 0 {
		return 0
	}
	if m.taskCursor >= length {
		return length - 1
	}
	return m.taskCursor
}

func (m statusCmp) currentTaskRuns() []taskrun.Run {
	if m.taskRuns == nil {
		return nil
	}
	contextID := m.taskContextSessionID()
	if contextID == "" {
		return nil
	}
	return m.taskRuns.Snapshot(contextID)
}

func (m statusCmp) selectedTaskRun() (taskrun.Run, bool) {
	runs := m.currentTaskRuns()
	if len(runs) == 0 {
		return taskrun.Run{}, false
	}
	return runs[m.clampedTaskCursor(len(runs))], true
}

func (m statusCmp) pendingApprovals() string {
	t := theme.CurrentTheme()
	return styles.Padded().
		Background(t.Warning()).
		Foreground(t.Background()).
		Render(fmt.Sprintf("Approvals: %d", m.permission.PendingCount()))
}

func truncateString(value string, width int) string {
	if width <= 0 || len(value) <= width {
		return value
	}
	if width <= 3 {
		return value[:width]
	}
	return value[:width-3] + "..."
}

func NewStatusCmp(lspClients map[string]*lsp.Client, skillsSvc skills.Service, permissionSvc permission.Service, taskRuns taskrun.Service) StatusCmp {
	helpWidget = getHelpWidget()

	return &statusCmp{
		messageTTL: 10 * time.Second,
		lspClients: lspClients,
		skillsSvc:  skillsSvc,
		permission: permissionSvc,
		taskRuns:   taskRuns,
	}
}
