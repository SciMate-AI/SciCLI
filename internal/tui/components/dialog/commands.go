package dialog

import (
	"slices"
	"strings"

	utilComponents "github.com/SciMate-AI/scicli/internal/tui/components/util"
	"github.com/SciMate-AI/scicli/internal/tui/layout"
	"github.com/SciMate-AI/scicli/internal/tui/styles"
	"github.com/SciMate-AI/scicli/internal/tui/theme"
	"github.com/SciMate-AI/scicli/internal/tui/util"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Command represents a command that can be executed
type Command struct {
	ID          string
	Title       string
	Description string
	Handler     func(cmd Command) tea.Cmd
}

func (ci Command) Render(selected bool, width int) string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	descStyle := baseStyle.Width(width).Foreground(t.TextMuted())
	itemStyle := baseStyle.Width(width).
		Foreground(t.Text()).
		Background(t.Background())

	if selected {
		itemStyle = itemStyle.
			Background(t.Primary()).
			Foreground(t.Background()).
			Bold(true)
		descStyle = descStyle.
			Background(t.Primary()).
			Foreground(t.Background())
	}

	title := itemStyle.Padding(0, 1).Render(ci.Title)
	if ci.Description != "" {
		description := descStyle.Padding(0, 1).Render(ci.Description)
		return lipgloss.JoinVertical(lipgloss.Left, title, description)
	}
	return title
}

// CommandSelectedMsg is sent when a command is selected
type CommandSelectedMsg struct {
	Command Command
}

// CloseCommandDialogMsg is sent when the command dialog is closed
type CloseCommandDialogMsg struct{}

// CommandDialog interface for the command selection dialog
type CommandDialog interface {
	tea.Model
	layout.Bindings
	SetCommands(commands []Command)
}

type commandDialogCmp struct {
	listView    utilComponents.SimpleList[Command]
	allCommands []Command
	filterInput textinput.Model
	filterValue string
	width       int
	height      int
}

type commandKeyMap struct {
	Enter  key.Binding
	Escape key.Binding
	Clear  key.Binding
}

var commandKeys = commandKeyMap{
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select command"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "close"),
	),
	Clear: key.NewBinding(
		key.WithKeys("ctrl+u"),
		key.WithHelp("ctrl+u", "clear filter"),
	),
}

func (c *commandDialogCmp) Init() tea.Cmd {
	c.filterInput.Focus()
	return c.filterInput.Cursor.BlinkCmd()
}

func (c *commandDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, commandKeys.Enter):
			selectedItem, idx := c.listView.GetSelectedItem()
			if idx != -1 {
				return c, util.CmdHandler(CommandSelectedMsg{
					Command: selectedItem,
				})
			}
		case key.Matches(msg, commandKeys.Escape):
			return c, util.CmdHandler(CloseCommandDialogMsg{})
		case key.Matches(msg, commandKeys.Clear):
			c.filterInput.SetValue("")
			c.applyFilter("")
		default:
			if shouldUpdateDialogFilter(msg) {
				before := c.filterInput.Value()
				var cmd tea.Cmd
				c.filterInput, cmd = c.filterInput.Update(msg)
				cmds = append(cmds, cmd)
				if next := strings.TrimSpace(c.filterInput.Value()); next != strings.TrimSpace(before) {
					c.applyFilter(next)
				}
			}
		}
	case tea.WindowSizeMsg:
		c.width = msg.Width
		c.height = msg.Height
		if c.height > 0 {
			c.listView.SetMaxVisibleItems(max(6, min(14, c.height/2)))
		}
	}

	u, cmd := c.listView.Update(msg)
	c.listView = u.(utilComponents.SimpleList[Command])
	cmds = append(cmds, cmd)

	return c, tea.Batch(cmds...)
}

func (c *commandDialogCmp) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	maxWidth := 56

	commands := c.listView.GetItems()

	for _, cmd := range commands {
		if len(cmd.Title) > maxWidth-4 {
			maxWidth = len(cmd.Title) + 4
		}
		if cmd.Description != "" {
			if len(cmd.Description) > maxWidth-4 {
				maxWidth = len(cmd.Description) + 4
			}
		}
	}
	if c.width > 0 {
		maxWidth = max(40, min(maxWidth, c.width-12))
	}

	c.listView.SetMaxWidth(maxWidth)
	c.filterInput.Width = maxWidth - 2

	title := baseStyle.
		Foreground(t.Primary()).
		Bold(true).
		Width(maxWidth).
		Padding(0, 1).
		Render("Command Palette")

	filter := baseStyle.
		Width(maxWidth).
		Foreground(t.Text()).
		Render(c.filterInput.View())

	footerText := "Type to filter. Enter to run."
	if strings.TrimSpace(c.filterValue) != "" {
		footerText = "Type to refine. Ctrl+U clears."
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		baseStyle.Width(maxWidth).Foreground(t.TextMuted()).Render("Search"),
		filter,
		baseStyle.Width(maxWidth).Render(""),
		baseStyle.Width(maxWidth).Render(c.listView.View()),
		baseStyle.Width(maxWidth).Render(""),
		baseStyle.Width(maxWidth).Foreground(t.TextMuted()).Render(footerText),
	)

	return baseStyle.Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderBackground(t.Background()).
		BorderForeground(t.TextMuted()).
		Width(lipgloss.Width(content) + 4).
		Render(content)
}

func (c *commandDialogCmp) BindingKeys() []key.Binding {
	bindings := layout.KeyMapToSlice(commandKeys)
	bindings = append(bindings, c.listView.BindingKeys()...)
	return bindings
}

func (c *commandDialogCmp) SetCommands(commands []Command) {
	c.allCommands = append([]Command{}, commands...)
	c.filterInput.SetValue("")
	c.applyFilter("")
}

func (c *commandDialogCmp) applyFilter(query string) {
	c.filterValue = strings.TrimSpace(query)
	c.listView.SetItems(filterCommands(c.allCommands, c.filterValue))
}

// NewCommandDialogCmp creates a new command selection dialog
func NewCommandDialogCmp() CommandDialog {
	filter := textinput.New()
	filter.Placeholder = "Search commands"
	filter.Prompt = "> "
	filter.CharLimit = 120
	filter.Cursor.Blink = true
	filter.Focus()

	listView := utilComponents.NewSimpleList[Command](
		[]Command{},
		10,
		"No matching commands",
		false,
	)
	return &commandDialogCmp{
		listView:    listView,
		filterInput: filter,
	}
}

func filterCommands(commands []Command, query string) []Command {
	if strings.TrimSpace(query) == "" {
		return append([]Command{}, commands...)
	}

	type scoredCommand struct {
		command Command
		score   int
	}

	scored := make([]scoredCommand, 0, len(commands))
	for _, cmd := range commands {
		score := scoreCommandMatch(cmd, query)
		if score <= 0 {
			continue
		}
		scored = append(scored, scoredCommand{command: cmd, score: score})
	}

	slices.SortFunc(scored, func(a, b scoredCommand) int {
		if a.score != b.score {
			return b.score - a.score
		}
		return strings.Compare(strings.ToLower(a.command.Title), strings.ToLower(b.command.Title))
	})

	out := make([]Command, 0, len(scored))
	for _, item := range scored {
		out = append(out, item.command)
	}
	return out
}

func scoreCommandMatch(cmd Command, query string) int {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return 1
	}

	title := strings.ToLower(cmd.Title)
	id := strings.ToLower(cmd.ID)
	description := strings.ToLower(cmd.Description)
	combined := strings.Join([]string{title, id, description}, "\n")

	if !strings.Contains(combined, query) {
		allTokensMatch := true
		for _, token := range strings.Fields(query) {
			if !strings.Contains(combined, token) {
				allTokensMatch = false
				break
			}
		}
		if !allTokensMatch {
			return 0
		}
	}

	score := 0
	if strings.HasPrefix(title, query) {
		score += 140
	}
	if strings.HasPrefix(id, query) {
		score += 120
	}
	if strings.Contains(title, query) {
		score += 90
	}
	if strings.Contains(id, query) {
		score += 80
	}
	if strings.Contains(description, query) {
		score += 40
	}
	for _, token := range strings.Fields(query) {
		if strings.Contains(title, token) {
			score += 24
		}
		if strings.Contains(id, token) {
			score += 20
		}
		if strings.Contains(description, token) {
			score += 8
		}
	}
	return score
}

func shouldUpdateDialogFilter(msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeyRunes, tea.KeySpace, tea.KeyBackspace, tea.KeyDelete:
		return true
	default:
		return false
	}
}
