package chat

import (
	"fmt"
	"strings"

	"github.com/SciMate-AI/scicli/internal/app"
	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/SciMate-AI/scicli/internal/pubsub"
	"github.com/SciMate-AI/scicli/internal/session"
	"github.com/SciMate-AI/scicli/internal/tui/styles"
	"github.com/SciMate-AI/scicli/internal/tui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type workbenchNavCmp struct {
	app     *app.App
	width   int
	height  int
	session session.Session
}

func (m *workbenchNavCmp) Init() tea.Cmd {
	return nil
}

func (m *workbenchNavCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case SessionSelectedMsg:
		m.session = msg
	case SessionClearedMsg:
		m.session = session.Session{}
	case pubsub.Event[session.Session]:
		if msg.Type == pubsub.UpdatedEvent && msg.Payload.ID == m.session.ID {
			m.session = msg.Payload
		}
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
		baseStyle.Foreground(t.Primary()).Bold(true).Render("Quick Actions"),
		baseStyle.Width(width).Render("Ctrl+K command palette"),
		baseStyle.Width(width).Render("/skills skill browser"),
		baseStyle.Width(width).Render("/tasks delegated tasks"),
		baseStyle.Width(width).Render("/parent go to parent"),
		baseStyle.Width(width).Render("/ultrawork on|off|auto"),
		"",
		baseStyle.Foreground(t.Primary()).Bold(true).Render("Notes"),
		baseStyle.Foreground(t.TextMuted()).Width(width).Render("The center pane is the live conversation. The right pane tracks run state, delegated tasks, and file changes."),
	}

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

func NewWorkbenchNavCmp(app *app.App) tea.Model {
	return &workbenchNavCmp{app: app}
}
