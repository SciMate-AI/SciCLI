package dialog

import (
	"github.com/SciMate-AI/scicli/internal/tui/layout"
	"github.com/SciMate-AI/scicli/internal/tui/styles"
	"github.com/SciMate-AI/scicli/internal/tui/theme"
	"github.com/SciMate-AI/scicli/internal/tui/util"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const question = "Are you sure you want to quit?"

type CloseQuitMsg struct{}

type QuitDialog interface {
	tea.Model
	layout.Bindings
}

type quitDialogCmp struct {
	selectedNo bool
}

type helpMapping struct {
	LeftRight  key.Binding
	EnterSpace key.Binding
	Yes        key.Binding
	No         key.Binding
	Tab        key.Binding
}

var helpKeys = helpMapping{
	LeftRight: key.NewBinding(
		key.WithKeys("left", "right"),
		key.WithHelp("←/→", "switch options"),
	),
	EnterSpace: key.NewBinding(
		key.WithKeys("enter", " "),
		key.WithHelp("enter/space", "confirm"),
	),
	Yes: key.NewBinding(
		key.WithKeys("y", "Y"),
		key.WithHelp("y/Y", "yes"),
	),
	No: key.NewBinding(
		key.WithKeys("n", "N"),
		key.WithHelp("n/N", "no"),
	),
	Tab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "switch options"),
	),
}

func (q *quitDialogCmp) Init() tea.Cmd {
	return nil
}

func (q *quitDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, helpKeys.LeftRight) || key.Matches(msg, helpKeys.Tab):
			q.selectedNo = !q.selectedNo
			return q, nil
		case key.Matches(msg, helpKeys.EnterSpace):
			if !q.selectedNo {
				return q, tea.Quit
			}
			return q, util.CmdHandler(CloseQuitMsg{})
		case key.Matches(msg, helpKeys.Yes):
			return q, tea.Quit
		case key.Matches(msg, helpKeys.No):
			return q, util.CmdHandler(CloseQuitMsg{})
		}
	}
	return q, nil
}

func (q *quitDialogCmp) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	yesLabel := "  yes"
	noLabel := "  no"
	if q.selectedNo {
		noLabel = "> no"
	} else {
		yesLabel = "> yes"
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		baseStyle.Foreground(t.Primary()).Bold(true).Render("Quit"),
		baseStyle.Render(question),
		"",
		baseStyle.Foreground(t.TextMuted()).Render(yesLabel),
		baseStyle.Foreground(t.TextMuted()).Render(noLabel),
		"",
		baseStyle.Foreground(t.TextMuted()).Render("Enter confirms. y quits. n closes."),
	)

	return inlineSheet(content)
}

func (q *quitDialogCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(helpKeys)
}

func NewQuitCmp() QuitDialog {
	return &quitDialogCmp{
		selectedNo: true,
	}
}
