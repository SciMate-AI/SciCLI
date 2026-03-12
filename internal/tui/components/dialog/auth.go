package dialog

import (
	"fmt"
	"strings"
	"time"

	"github.com/SciMate-AI/scicli/internal/auth"
	"github.com/SciMate-AI/scicli/internal/tui/layout"
	"github.com/SciMate-AI/scicli/internal/tui/styles"
	"github.com/SciMate-AI/scicli/internal/tui/theme"
	"github.com/SciMate-AI/scicli/internal/tui/util"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type AuthDialogMode string

const (
	AuthDialogModeStatus   AuthDialogMode = "status"
	AuthDialogModeLogin    AuthDialogMode = "login"
	AuthDialogModeRegister AuthDialogMode = "register"
)

type ShowAuthDialogMsg struct {
	Mode AuthDialogMode
}

type CloseAuthDialogMsg struct{}

type AuthSubmitMsg struct {
	Mode     AuthDialogMode
	Email    string
	Password string
}

type AuthDialogResultMsg struct {
	Mode              AuthDialogMode
	Session           *auth.Session
	Email             string
	Error             string
	NeedsConfirmation bool
}

type AuthQuickActionMsg struct {
	Action AuthQuickAction
}

type AuthQuickAction string

const (
	AuthQuickActionLogout  AuthQuickAction = "logout"
	AuthQuickActionRefresh AuthQuickAction = "refresh"
)

type AuthDialog interface {
	tea.Model
	layout.Bindings
	Open(mode AuthDialogMode, session *auth.Session)
	SetResult(session *auth.Session, errMsg string)
}

type authDialogCmp struct {
	width, height int
	mode          AuthDialogMode
	current       *auth.Session
	errMsg        string
	inputs        []textinput.Model
	focusIndex    int
}

type authKeyMap struct {
	Enter    key.Binding
	Tab      key.Binding
	BackTab  key.Binding
	Escape   key.Binding
	Login    key.Binding
	Register key.Binding
	Logout   key.Binding
	Refresh  key.Binding
}

var authKeys = authKeyMap{
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "submit"),
	),
	Tab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "next field"),
	),
	BackTab: key.NewBinding(
		key.WithKeys("shift+tab"),
		key.WithHelp("shift+tab", "prev field"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "close"),
	),
	Login: key.NewBinding(
		key.WithKeys("l"),
		key.WithHelp("l", "login form"),
	),
	Register: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "register form"),
	),
	Logout: key.NewBinding(
		key.WithKeys("o"),
		key.WithHelp("o", "logout"),
	),
	Refresh: key.NewBinding(
		key.WithKeys("f"),
		key.WithHelp("f", "refresh token"),
	),
}

func (a *authDialogCmp) Init() tea.Cmd {
	return textinput.Blink
}

func (a *authDialogCmp) Open(mode AuthDialogMode, session *auth.Session) {
	a.mode = mode
	a.current = session
	a.errMsg = ""
	a.focusIndex = 0
	a.resetInputs()
}

func (a *authDialogCmp) SetResult(session *auth.Session, errMsg string) {
	a.current = session
	a.errMsg = strings.TrimSpace(errMsg)
}

func (a *authDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if key.Matches(msg, authKeys.Escape) {
			return a, util.CmdHandler(CloseAuthDialogMsg{})
		}
		if a.mode == AuthDialogModeStatus {
			switch {
			case key.Matches(msg, authKeys.Login):
				a.mode = AuthDialogModeLogin
				a.errMsg = ""
				a.resetInputs()
				return a, nil
			case key.Matches(msg, authKeys.Register):
				a.mode = AuthDialogModeRegister
				a.errMsg = ""
				a.resetInputs()
				return a, nil
			case key.Matches(msg, authKeys.Logout):
				return a, util.CmdHandler(AuthQuickActionMsg{Action: AuthQuickActionLogout})
			case key.Matches(msg, authKeys.Refresh):
				return a, util.CmdHandler(AuthQuickActionMsg{Action: AuthQuickActionRefresh})
			}
			return a, nil
		}

		switch {
		case key.Matches(msg, authKeys.Enter):
			if len(a.inputs) < 2 {
				return a, nil
			}
			return a, util.CmdHandler(AuthSubmitMsg{
				Mode:     a.mode,
				Email:    strings.TrimSpace(a.inputs[0].Value()),
				Password: a.inputs[1].Value(),
			})
		case key.Matches(msg, authKeys.Tab):
			a.moveFocus(1)
			return a, nil
		case key.Matches(msg, authKeys.BackTab):
			a.moveFocus(-1)
			return a, nil
		}
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
	}

	if a.mode != AuthDialogModeStatus && len(a.inputs) > 0 {
		var cmd tea.Cmd
		a.inputs[a.focusIndex], cmd = a.inputs[a.focusIndex].Update(msg)
		return a, cmd
	}

	return a, nil
}

func (a *authDialogCmp) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	width := 60
	if a.width > 0 {
		width = max(50, min(76, a.width-10))
	}

	title := "Account"
	switch a.mode {
	case AuthDialogModeLogin:
		title = "Login"
	case AuthDialogModeRegister:
		title = "Register"
	}

	header := baseStyle.
		Foreground(t.Primary()).
		Bold(true).
		Width(width).
		Padding(0, 1).
		Render(title)

	contentParts := []string{
		header,
		baseStyle.Width(width).Render(""),
		a.renderStatus(width),
	}
	if a.errMsg != "" {
		contentParts = append(contentParts, baseStyle.
			Foreground(t.Error()).
			Width(width).
			Padding(0, 1).
			Render(a.errMsg))
	}
	if a.mode != AuthDialogModeStatus {
		contentParts = append(contentParts, a.renderForm(width)...)
	} else {
		contentParts = append(contentParts, a.renderStatusHelp(width))
	}

	content := lipgloss.JoinVertical(lipgloss.Left, contentParts...)
	return baseStyle.Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderBackground(t.Background()).
		BorderForeground(t.TextMuted()).
		Width(lipgloss.Width(content) + 4).
		Render(content)
}

func (a *authDialogCmp) BindingKeys() []key.Binding {
	if a.mode == AuthDialogModeStatus {
		return []key.Binding{
			authKeys.Escape,
			authKeys.Login,
			authKeys.Register,
			authKeys.Logout,
			authKeys.Refresh,
		}
	}
	return []key.Binding{
		authKeys.Escape,
		authKeys.Enter,
		authKeys.Tab,
		authKeys.BackTab,
	}
}

func (a *authDialogCmp) renderStatus(width int) string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	lines := []string{
		baseStyle.Foreground(t.TextMuted()).Width(width).Padding(0, 1).
			Render("Current authentication status"),
	}
	if a.current == nil || strings.TrimSpace(a.current.AccessToken) == "" {
		lines = append(lines, baseStyle.Width(width).Padding(0, 1).Render("Not logged in"))
		return lipgloss.JoinVertical(lipgloss.Left, lines...)
	}

	expiry := "unknown"
	if !a.current.ExpiresAt.IsZero() {
		expiry = a.current.ExpiresAt.Local().Format(time.RFC3339)
	}

	lines = append(lines,
		baseStyle.Width(width).Padding(0, 1).Render(fmt.Sprintf("Email: %s", a.current.Email)),
		baseStyle.Width(width).Padding(0, 1).Render(fmt.Sprintf("Expires: %s", expiry)),
	)
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (a *authDialogCmp) renderForm(width int) []string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	for i := range a.inputs {
		a.inputs[i].Width = max(20, width-4)
	}

	help := "Enter email and password, then press Enter."
	if a.mode == AuthDialogModeRegister {
		help = "Create a new SciMate account. Press Enter to submit."
	}

	return []string{
		baseStyle.Width(width).Render(""),
		baseStyle.Foreground(t.TextMuted()).Width(width).Padding(0, 1).Render(help),
		baseStyle.Width(width).Padding(0, 1).Render("Email"),
		baseStyle.Width(width).Padding(0, 1).Render(a.inputs[0].View()),
		baseStyle.Width(width).Padding(1, 1, 0, 1).Render("Password"),
		baseStyle.Width(width).Padding(0, 1).Render(a.inputs[1].View()),
		baseStyle.Width(width).Render(""),
		baseStyle.Foreground(t.TextMuted()).Width(width).Padding(0, 1).
			Render("Shortcuts: tab switch field, enter submit, esc close"),
	}
}

func (a *authDialogCmp) renderStatusHelp(width int) string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()
	return baseStyle.
		Foreground(t.TextMuted()).
		Width(width).
		Padding(0, 1).
		Render("Shortcuts: l login, r register, o logout, f refresh, esc close")
}

func (a *authDialogCmp) resetInputs() {
	emailValue := ""
	if a.current != nil {
		emailValue = strings.TrimSpace(a.current.Email)
	}

	email := textinput.New()
	email.Placeholder = "name@example.com"
	email.SetValue(emailValue)
	email.Focus()
	email.Prompt = ""

	password := textinput.New()
	password.Placeholder = "Password"
	password.Prompt = ""
	password.EchoMode = textinput.EchoPassword
	password.EchoCharacter = '*'

	a.inputs = []textinput.Model{email, password}
	a.focusIndex = 0
	a.applyFocusStyles()
}

func (a *authDialogCmp) moveFocus(delta int) {
	if len(a.inputs) == 0 {
		return
	}
	a.focusIndex = (a.focusIndex + delta + len(a.inputs)) % len(a.inputs)
	a.applyFocusStyles()
}

func (a *authDialogCmp) applyFocusStyles() {
	t := theme.CurrentTheme()
	for i := range a.inputs {
		if i == a.focusIndex {
			a.inputs[i].Focus()
			a.inputs[i].TextStyle = a.inputs[i].TextStyle.Foreground(t.Primary())
			a.inputs[i].PromptStyle = a.inputs[i].PromptStyle.Foreground(t.Primary())
			continue
		}
		a.inputs[i].Blur()
		a.inputs[i].TextStyle = a.inputs[i].TextStyle.Foreground(t.Text())
		a.inputs[i].PromptStyle = a.inputs[i].PromptStyle.Foreground(t.TextMuted())
	}
}

func NewAuthDialogCmp() AuthDialog {
	return &authDialogCmp{
		mode: AuthDialogModeStatus,
	}
}
