package dialog

import (
	"fmt"

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

type SessionSelectedMsg struct {
	Session session.Session
}

type CloseSessionDialogMsg struct{}

type SessionDialog interface {
	tea.Model
	layout.Bindings
	SetSessions(sessions []session.Session)
	SetSelectedSession(sessionID string)
}

type sessionDialogCmp struct {
	listView          utilComponents.SimpleList[sessionListItem]
	sessions          []session.Session
	width             int
	height            int
	selectedSessionID string
}

type sessionListItem struct {
	session session.Session
}

func (s sessionListItem) Render(selected bool, width int) string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	itemStyle := baseStyle.
		Width(width).
		Foreground(t.TextMuted())
	prefix := "  "
	if selected {
		prefix = "> "
		itemStyle = itemStyle.Foreground(t.Text()).Bold(true)
	}

	title := s.session.Title
	if title == "" {
		title = "(untitled session)"
	}
	return itemStyle.Render(prefix + title)
}

type sessionKeyMap struct {
	Enter  key.Binding
	Escape key.Binding
}

var sessionKeys = sessionKeyMap{
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select session"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "close"),
	),
}

func (s *sessionDialogCmp) Init() tea.Cmd {
	return s.listView.Init()
}

func (s *sessionDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, sessionKeys.Enter):
			selectedItem, idx := s.listView.GetSelectedItem()
			if idx != -1 {
				return s, util.CmdHandler(SessionSelectedMsg{Session: selectedItem.session})
			}
		case key.Matches(msg, sessionKeys.Escape):
			return s, util.CmdHandler(CloseSessionDialogMsg{})
		}
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		s.setVisibleRows()
	}

	u, cmd := s.listView.Update(msg)
	s.listView = u.(utilComponents.SimpleList[sessionListItem])
	cmds = append(cmds, cmd)
	return s, tea.Batch(cmds...)
}

func (s *sessionDialogCmp) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	if len(s.sessions) == 0 {
		return inlineSheet(baseStyle.Width(40).Render("No sessions available"))
	}

	maxWidth := 40
	for _, sess := range s.sessions {
		if len(sess.Title) > maxWidth-4 {
			maxWidth = len(sess.Title) + 4
		}
	}
	if s.width > 0 {
		maxWidth = max(30, min(maxWidth, s.width-15))
	}

	s.listView.SetMaxWidth(maxWidth)
	s.setVisibleRows()

	title := baseStyle.
		Foreground(t.Primary()).
		Bold(true).
		Width(maxWidth).
		Render(fmt.Sprintf("Switch Session (%d)", len(s.sessions)))

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		baseStyle.Width(maxWidth).Render(s.listView.View()),
	)

	return inlineSheet(baseStyle.Width(maxWidth).Render(content))
}

func (s *sessionDialogCmp) BindingKeys() []key.Binding {
	bindings := layout.KeyMapToSlice(sessionKeys)
	bindings = append(bindings, s.listView.BindingKeys()...)
	return bindings
}

func (s *sessionDialogCmp) SetSessions(sessions []session.Session) {
	s.sessions = sessions

	items := make([]sessionListItem, 0, len(sessions))
	for _, sess := range sessions {
		items = append(items, sessionListItem{session: sess})
	}
	s.listView.SetItems(items)

	if s.selectedSessionID == "" {
		s.listView.SetSelectedIndex(0)
		return
	}
	for i, sess := range sessions {
		if sess.ID == s.selectedSessionID {
			s.listView.SetSelectedIndex(i)
			return
		}
	}
	s.listView.SetSelectedIndex(0)
}

func (s *sessionDialogCmp) SetSelectedSession(sessionID string) {
	s.selectedSessionID = sessionID
	for i, sess := range s.sessions {
		if sess.ID == sessionID {
			s.listView.SetSelectedIndex(i)
			return
		}
	}
}

func (s *sessionDialogCmp) setVisibleRows() {
	rows := 10
	if s.height > 0 {
		rows = max(6, min(18, s.height/2))
	}
	s.listView.SetMaxVisibleItems(rows)
}

func NewSessionDialogCmp() SessionDialog {
	return &sessionDialogCmp{
		listView: utilComponents.NewSimpleList(
			[]sessionListItem{},
			10,
			"No sessions available",
			true,
		),
		sessions: []session.Session{},
	}
}
