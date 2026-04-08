package dialog

import (
	"fmt"
	"strings"
	"time"

	"github.com/SciMate-AI/scicli/internal/session"
	utilComponents "github.com/SciMate-AI/scicli/internal/tui/components/util"
	"github.com/SciMate-AI/scicli/internal/tui/layout"
	"github.com/SciMate-AI/scicli/internal/tui/styles"
	"github.com/SciMate-AI/scicli/internal/tui/theme"
	"github.com/SciMate-AI/scicli/internal/tui/util"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ShowTaskDialogMsg struct{}
type CloseTaskDialogMsg struct{}

type TaskSelectedMsg struct {
	Session session.Session
}

type TaskDialog interface {
	tea.Model
	layout.Bindings
	SetParentSession(session.Session)
	SetTasks([]session.Session)
}

type taskDialogCmp struct {
	listView      utilComponents.SimpleList[taskListItem]
	parentSession session.Session
	tasks         []session.Session
	width         int
	height        int
}

type taskListItem struct {
	session session.Session
}

func (t taskListItem) Render(selected bool, width int) string {
	themeColors := theme.CurrentTheme()
	baseStyle := styles.BaseStyle().Width(width)
	titleStyle := baseStyle.Foreground(themeColors.TextMuted())
	descStyle := baseStyle.Foreground(themeColors.TextMuted())
	prefix := "  "
	if selected {
		prefix = "> "
		titleStyle = titleStyle.Foreground(themeColors.Text()).Bold(true)
		descStyle = descStyle.Foreground(themeColors.Text())
	}

	title := t.session.Title
	if strings.TrimSpace(title) == "" {
		title = "(untitled delegated task)"
	}
	return lipgloss.JoinVertical(
		lipgloss.Left,
		titleStyle.Render(prefix+title),
		descStyle.Render("  "+renderTaskMeta(t.session)),
	)
}

type taskKeyMap struct {
	Enter  key.Binding
	Escape key.Binding
}

var taskKeys = taskKeyMap{
	Enter:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open task session")),
	Escape: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")),
}

func (t *taskDialogCmp) Init() tea.Cmd {
	return t.listView.Init()
}

func (t *taskDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, taskKeys.Enter):
			item, idx := t.listView.GetSelectedItem()
			if idx != -1 {
				return t, util.CmdHandler(TaskSelectedMsg{Session: item.session})
			}
		case key.Matches(msg, taskKeys.Escape):
			return t, util.CmdHandler(CloseTaskDialogMsg{})
		}
	case tea.WindowSizeMsg:
		t.width = msg.Width
		t.height = msg.Height
		t.listView.SetMaxVisibleItems(max(6, min(16, t.height/2)))
	}

	u, cmd := t.listView.Update(msg)
	t.listView = u.(utilComponents.SimpleList[taskListItem])
	return t, cmd
}

func (t *taskDialogCmp) View() string {
	themeColors := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()
	maxWidth := 76
	if t.width > 0 {
		maxWidth = max(44, min(maxWidth, t.width-12))
	}
	t.listView.SetMaxWidth(maxWidth)

	title := baseStyle.
		Foreground(themeColors.Primary()).
		Bold(true).
		Width(maxWidth).
		Render(fmt.Sprintf("Delegated Tasks (%d)", len(t.tasks)))

	parentLine := "Current session"
	if strings.TrimSpace(t.parentSession.Title) != "" {
		parentLine = "Parent: " + t.parentSession.Title
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		baseStyle.Width(maxWidth).Foreground(themeColors.TextMuted()).Render(parentLine),
		"",
		baseStyle.Width(maxWidth).Render(t.listView.View()),
		baseStyle.Width(maxWidth).Foreground(themeColors.TextMuted()).Render("Enter opens the delegated task session."),
	)

	return lipgloss.PlaceHorizontal(max(maxWidth, t.width), lipgloss.Center, baseStyle.Width(maxWidth).Render(content))
}

func (t *taskDialogCmp) BindingKeys() []key.Binding {
	bindings := layout.KeyMapToSlice(taskKeys)
	bindings = append(bindings, t.listView.BindingKeys()...)
	return bindings
}

func (t *taskDialogCmp) SetParentSession(parent session.Session) {
	t.parentSession = parent
}

func (t *taskDialogCmp) SetTasks(tasks []session.Session) {
	t.tasks = append([]session.Session{}, tasks...)
	items := make([]taskListItem, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, taskListItem{session: task})
	}
	t.listView.SetItems(items)
}

func NewTaskDialogCmp() TaskDialog {
	return &taskDialogCmp{
		listView: utilComponents.NewSimpleList([]taskListItem{}, 10, "No delegated tasks for this session", true),
	}
}

func renderTaskMeta(task session.Session) string {
	updated := time.Unix(task.UpdatedAt, 0)
	if task.UpdatedAt == 0 {
		updated = time.Now()
	}

	parts := []string{
		fmt.Sprintf("messages: %d", task.MessageCount),
		fmt.Sprintf("cost: $%.2f", task.Cost),
		fmt.Sprintf("updated: %s", updated.Format("2006-01-02 15:04")),
	}
	totalTokens := task.PromptTokens + task.CompletionTokens
	if totalTokens > 0 {
		parts = append(parts, fmt.Sprintf("tokens: %d", totalTokens))
	}
	return strings.Join(parts, "  ")
}
