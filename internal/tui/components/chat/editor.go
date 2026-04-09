package chat

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"unicode"

	"github.com/SciMate-AI/scicli/internal/app"
	"github.com/SciMate-AI/scicli/internal/logging"
	"github.com/SciMate-AI/scicli/internal/message"
	"github.com/SciMate-AI/scicli/internal/session"
	"github.com/SciMate-AI/scicli/internal/tui/components/dialog"
	"github.com/SciMate-AI/scicli/internal/tui/layout"
	"github.com/SciMate-AI/scicli/internal/tui/styles"
	"github.com/SciMate-AI/scicli/internal/tui/theme"
	"github.com/SciMate-AI/scicli/internal/tui/util"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type editorCmp struct {
	width       int
	height      int
	app         *app.App
	session     session.Session
	textarea    textarea.Model
	attachments []message.Attachment
	deleteMode  bool
	slashItems  []slashCommand
	slashIndex  int
	slashQuery  string
	slashHidden bool
}

type EditorKeyMaps struct {
	Send       key.Binding
	OpenEditor key.Binding
}

type bluredEditorKeyMaps struct {
	Send       key.Binding
	Focus      key.Binding
	OpenEditor key.Binding
}
type DeleteAttachmentKeyMaps struct {
	AttachmentDeleteMode key.Binding
	Escape               key.Binding
	DeleteAllAttachments key.Binding
}

var editorMaps = EditorKeyMaps{
	Send: key.NewBinding(
		key.WithKeys("enter", "ctrl+s"),
		key.WithHelp("enter", "send message"),
	),
	OpenEditor: key.NewBinding(
		key.WithKeys("ctrl+e"),
		key.WithHelp("ctrl+e", "open editor"),
	),
}

var DeleteKeyMaps = DeleteAttachmentKeyMaps{
	AttachmentDeleteMode: key.NewBinding(
		key.WithKeys("ctrl+r"),
		key.WithHelp("ctrl+r+{i}", "delete attachment at index i"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel delete mode"),
	),
	DeleteAllAttachments: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("ctrl+r+r", "delete all attchments"),
	),
}

const (
	maxAttachments = 5
)

func (m *editorCmp) openEditor() tea.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "nvim"
	}

	tmpfile, err := os.CreateTemp("", "msg_*.md")
	if err != nil {
		return util.ReportError(err)
	}
	tmpfile.Close()
	c := exec.Command(editor, tmpfile.Name()) //nolint:gosec
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return tea.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			return util.ReportError(err)
		}
		content, err := os.ReadFile(tmpfile.Name())
		if err != nil {
			return util.ReportError(err)
		}
		if len(content) == 0 {
			return util.ReportWarn("Message is empty")
		}
		os.Remove(tmpfile.Name())
		attachments := m.attachments
		m.attachments = nil
		return SendMsg{
			Text:        string(content),
			Attachments: attachments,
		}
	})
}

func (m *editorCmp) Init() tea.Cmd {
	return textarea.Blink
}

func (m *editorCmp) send() tea.Cmd {
	if m.app.CoderAgent.IsSessionBusy(m.session.ID) {
		return util.ReportWarn("Agent is working, please wait...")
	}

	value := m.textarea.Value()
	m.textarea.Reset()
	attachments := m.attachments

	m.attachments = nil
	m.refreshSlashSuggestions()
	if value == "" {
		return nil
	}
	return tea.Batch(
		util.CmdHandler(SendMsg{
			Text:        value,
			Attachments: attachments,
		}),
	)
}

func (m *editorCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case dialog.ThemeChangedMsg:
		m.textarea = CreateTextArea(&m.textarea)
		m.refreshSlashSuggestions()
	case dialog.CompletionSelectedMsg:
		existingValue := m.textarea.Value()
		modifiedValue := strings.Replace(existingValue, msg.SearchString, msg.CompletionValue, 1)

		m.textarea.SetValue(modifiedValue)
		m.refreshSlashSuggestions()
		return m, nil
	case SessionSelectedMsg:
		if msg.ID != m.session.ID {
			m.session = msg
		}
		return m, nil
	case dialog.AttachmentAddedMsg:
		if len(m.attachments) >= maxAttachments {
			logging.ErrorPersist(fmt.Sprintf("cannot add more than %d images", maxAttachments))
			return m, cmd
		}
		m.attachments = append(m.attachments, msg.Attachment)
	case tea.KeyMsg:
		if key.Matches(msg, DeleteKeyMaps.AttachmentDeleteMode) {
			m.deleteMode = true
			return m, nil
		}
		if key.Matches(msg, DeleteKeyMaps.DeleteAllAttachments) && m.deleteMode {
			m.deleteMode = false
			m.attachments = nil
			return m, nil
		}
		if m.deleteMode && len(msg.Runes) > 0 && unicode.IsDigit(msg.Runes[0]) {
			num := int(msg.Runes[0] - '0')
			m.deleteMode = false
			if num < 10 && len(m.attachments) > num {
				if num == 0 {
					m.attachments = m.attachments[num+1:]
				} else {
					m.attachments = slices.Delete(m.attachments, num, num+1)
				}
				return m, nil
			}
		}
		if key.Matches(msg, messageKeys.PageUp) || key.Matches(msg, messageKeys.PageDown) ||
			key.Matches(msg, messageKeys.HalfPageUp) || key.Matches(msg, messageKeys.HalfPageDown) {
			return m, nil
		}
		if key.Matches(msg, editorMaps.OpenEditor) {
			if m.app.CoderAgent.IsSessionBusy(m.session.ID) {
				return m, util.ReportWarn("Agent is working, please wait...")
			}
			return m, m.openEditor()
		}
		if key.Matches(msg, DeleteKeyMaps.Escape) {
			m.deleteMode = false
			if len(m.slashItems) > 0 {
				m.slashHidden = true
				m.slashItems = nil
				m.slashIndex = 0
				m.slashQuery = slashQuery(m.textarea.Value())
			}
			return m, nil
		}
		if msg.String() == "ctrl+v" {
			if err := util.PasteTextArea(&m.textarea); err != nil {
				return m, util.ReportError(err)
			}
			m.refreshSlashSuggestions()
			return m, nil
		}
		if len(m.slashItems) > 0 {
			switch msg.String() {
			case "up":
				m.moveSlashSelection(-1)
				return m, nil
			case "down":
				m.moveSlashSelection(1)
				return m, nil
			case "tab":
				if item, ok := m.selectedSlashCommand(); ok {
					m.applySlashCommand(item)
					return m, nil
				}
			}
		}
		// Hanlde Enter key
		if m.textarea.Focused() && key.Matches(msg, editorMaps.Send) {
			if item, ok := m.selectedSlashCommand(); ok && m.shouldCompleteSlashOnEnter(item) {
				m.applySlashCommand(item)
				return m, nil
			}
			value := m.textarea.Value()
			if len(value) > 0 && value[len(value)-1] == '\\' {
				// If the last character is a backslash, remove it and add a newline
				m.textarea.SetValue(value[:len(value)-1] + "\n")
				m.refreshSlashSuggestions()
				return m, nil
			} else {
				// Otherwise, send the message
				return m, m.send()
			}
		}

	}
	m.textarea, cmd = m.textarea.Update(msg)
	m.refreshSlashSuggestions()
	return m, cmd
}

func (m *editorCmp) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()
	contextLine := m.renderContextLine()
	statusParts := []string{
		"Enter send",
		"\\ + Enter newline",
		"Ctrl+E editor",
	}
	if len(m.slashItems) > 0 {
		statusParts = append([]string{"Tab accept", "Up/Down choose", "Esc close"}, statusParts...)
	}
	status := strings.Join(statusParts, "  ")
	prompt := baseStyle.Bold(true).Foreground(t.Primary()).Render(">")
	if m.app.CoderAgent.IsSessionBusy(m.session.ID) {
		prompt = baseStyle.Bold(true).Foreground(t.Warning()).Render("!")
	}
	inputRow := lipgloss.JoinHorizontal(lipgloss.Top, prompt, " ", m.textarea.View())
	lines := []string{contextLine}

	if len(m.attachments) > 0 {
		lines = append(lines, consoleMuted("attachments")+"  "+m.attachmentsContent())
	}
	lines = append(lines, inputRow)
	if suggestions := m.renderSlashSuggestions(); suggestions != "" {
		lines = append(lines, suggestions)
	}
	lines = append(lines, consoleMuted(status))

	columnWidth := codexColumnWidth(m.width)
	box := lipgloss.NewStyle().
		Width(max(24, columnWidth)).
		Padding(1, 0, 0, 0).
		BorderTop(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(t.BorderDim()).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	return baseStyle.Width(m.width).Render(codexCenter(m.width, box))
}

func (m *editorCmp) SetSize(width, height int) tea.Cmd {
	m.width = width
	m.height = height
	columnWidth := codexColumnWidth(width)
	m.textarea.SetHeight(max(1, height-5))
	m.textarea.SetWidth(max(12, columnWidth-6))
	return nil
}

func (m *editorCmp) GetSize() (int, int) {
	return m.textarea.Width(), m.textarea.Height()
}

func (m *editorCmp) attachmentsContent() string {
	var styledAttachments []string
	attachmentStyles := styles.BaseStyle().Foreground(theme.CurrentTheme().TextMuted())
	for i, attachment := range m.attachments {
		var filename string
		if len(attachment.FileName) > 10 {
			filename = fmt.Sprintf("%s %s...", styles.DocumentIcon, attachment.FileName[0:7])
		} else {
			filename = fmt.Sprintf("%s %s", styles.DocumentIcon, attachment.FileName)
		}
		if m.deleteMode {
			filename = fmt.Sprintf("%d:%s", i, filename)
		}
		styledAttachments = append(styledAttachments, attachmentStyles.Render(filename))
	}
	content := strings.Join(styledAttachments, "  ")
	return content
}

func (m *editorCmp) BindingKeys() []key.Binding {
	bindings := []key.Binding{}
	bindings = append(bindings, layout.KeyMapToSlice(editorMaps)...)
	bindings = append(bindings, layout.KeyMapToSlice(DeleteKeyMaps)...)
	return bindings
}

func CreateTextArea(existing *textarea.Model) textarea.Model {
	t := theme.CurrentTheme()
	bgColor := t.Background()
	textColor := t.Text()
	textMutedColor := t.TextMuted()

	ta := textarea.New()
	ta.BlurredStyle.Base = styles.BaseStyle().Background(bgColor).Foreground(textColor)
	ta.BlurredStyle.CursorLine = styles.BaseStyle().Background(bgColor)
	ta.BlurredStyle.Placeholder = styles.BaseStyle().Background(bgColor).Foreground(textMutedColor)
	ta.BlurredStyle.Text = styles.BaseStyle().Background(bgColor).Foreground(textColor)
	ta.FocusedStyle.Base = styles.BaseStyle().Background(bgColor).Foreground(textColor)
	ta.FocusedStyle.CursorLine = styles.BaseStyle().Background(bgColor)
	ta.FocusedStyle.Placeholder = styles.BaseStyle().Background(bgColor).Foreground(textMutedColor)
	ta.FocusedStyle.Text = styles.BaseStyle().Background(bgColor).Foreground(textColor)

	ta.Prompt = ""
	ta.Placeholder = "Type a request or / for commands"
	ta.ShowLineNumbers = false
	ta.CharLimit = -1

	if existing != nil {
		ta.SetValue(existing.Value())
		ta.SetWidth(existing.Width())
		ta.SetHeight(existing.Height())
	}

	ta.Focus()
	return ta
}

func NewEditorCmp(app *app.App) tea.Model {
	ta := CreateTextArea(nil)
	editor := &editorCmp{
		app:      app,
		textarea: ta,
	}
	editor.refreshSlashSuggestions()
	return editor
}

func (m *editorCmp) refreshSlashSuggestions() {
	query := slashQuery(m.textarea.Value())
	if query != m.slashQuery {
		m.slashHidden = false
	}
	m.slashQuery = query
	if m.slashHidden || query == "" {
		m.slashItems = nil
		m.slashIndex = 0
		return
	}
	m.slashItems = filterSlashCommands(m.textarea.Value())
	if len(m.slashItems) == 0 {
		m.slashIndex = 0
		return
	}
	if m.slashIndex >= len(m.slashItems) {
		m.slashIndex = len(m.slashItems) - 1
	}
	if m.slashIndex < 0 {
		m.slashIndex = 0
	}
}

func (m *editorCmp) moveSlashSelection(delta int) {
	if len(m.slashItems) == 0 {
		return
	}
	m.slashIndex = (m.slashIndex + delta + len(m.slashItems)) % len(m.slashItems)
}

func (m *editorCmp) selectedSlashCommand() (slashCommand, bool) {
	if len(m.slashItems) == 0 || m.slashIndex < 0 || m.slashIndex >= len(m.slashItems) {
		return slashCommand{}, false
	}
	return m.slashItems[m.slashIndex], true
}

func (m *editorCmp) shouldCompleteSlashOnEnter(item slashCommand) bool {
	query := slashQuery(m.textarea.Value())
	if query == "" || strings.Contains(query, "\n") {
		return false
	}
	normalizedInsert := strings.ToLower(strings.TrimSpace(item.InsertText))
	if item.RequiresArgs {
		return !slashCommandHasArguments(m.textarea.Value(), item)
	}
	return query != normalizedInsert
}

func (m *editorCmp) applySlashCommand(item slashCommand) {
	m.slashHidden = false
	m.textarea.SetValue(item.InsertText)
	m.textarea.SetCursor(len(item.InsertText))
	m.refreshSlashSuggestions()
}

func (m *editorCmp) renderSlashSuggestions() string {
	if len(m.slashItems) == 0 || m.width <= 0 {
		return ""
	}

	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()
	visible := min(len(m.slashItems), 7)
	selected, _ := m.selectedSlashCommand()
	header := baseStyle.Foreground(t.TextMuted()).Render("COMMANDS")
	if selected.Category != "" {
		header += baseStyle.Foreground(t.TextMuted()).Render("  " + strings.ToUpper(selected.Category))
	}
	lines := []string{header}
	commandWidth := max(18, min(m.width/2, 42))

	for i := 0; i < visible; i++ {
		item := m.slashItems[i]
		prefix := "  "
		commandStyle := baseStyle.Foreground(t.TextMuted())
		descStyle := baseStyle.Foreground(t.TextMuted())
		if i == m.slashIndex {
			prefix = "> "
			commandStyle = commandStyle.Foreground(t.Primary()).Bold(true)
			descStyle = descStyle.Foreground(t.Text())
		}

		commandLabel := truncateString(item.DisplayCommand(), commandWidth)
		description := truncateString(item.Description, max(16, m.width-commandWidth-8))
		row := prefix + commandStyle.Render(commandLabel)
		if description != "" && m.width-commandWidth > 12 {
			padding := max(2, commandWidth-lipgloss.Width(commandLabel)+2)
			row += strings.Repeat(" ", padding) + descStyle.Render(description)
		}
		lines = append(lines, row)
	}
	if len(m.slashItems) > visible {
		lines = append(lines, baseStyle.Foreground(t.TextMuted()).Render(fmt.Sprintf("  +%d more commands", len(m.slashItems)-visible)))
	}
	if usage := selected.DisplayCommand(); usage != "" {
		lines = append(lines, "")
		lines = append(lines, baseStyle.Foreground(t.TextMuted()).Render("USAGE"))
		lines = append(lines, baseStyle.Foreground(t.Text()).Render(truncateString(usage, max(16, m.width-10))))
	}

	return lipgloss.NewStyle().
		MaxWidth(max(24, codexColumnWidth(m.width)-4)).
		BorderLeft(true).
		BorderForeground(t.BorderDim()).
		PaddingLeft(1).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m *editorCmp) renderContextLine() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle().Foreground(t.TextMuted())
	sessionLabel := "new session"
	if strings.TrimSpace(m.session.Title) != "" {
		sessionLabel = truncateString(m.session.Title, max(12, m.width/3))
	}
	modeLabel := "ready"
	if m.app.CoderAgent.IsSessionBusy(m.session.ID) {
		modeLabel = "running"
	}
	modelLabel := activeModelShellLabel(max(18, m.width/3))
	return baseStyle.Render(strings.Join([]string{
		"session " + sessionLabel,
		modelLabel,
		modeLabel,
		"/ commands",
		"@ paths",
	}, "  "))
}
