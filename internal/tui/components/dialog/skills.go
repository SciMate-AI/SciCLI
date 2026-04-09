package dialog

import (
	"fmt"
	"slices"
	"strings"

	"github.com/SciMate-AI/scicli/internal/skills"
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

type ShowSkillDialogMsg struct{}
type CloseSkillDialogMsg struct{}

type SkillSelectedMsg struct {
	Skill skills.Skill
}

type SkillInstallRequestedMsg struct {
	Source string
}

type SkillUninstallRequestedMsg struct {
	Skill skills.Skill
}

type SkillDialog interface {
	tea.Model
	layout.Bindings
	SetSkills([]skills.Skill)
}

type skillDialogCmp struct {
	listView     utilComponents.SimpleList[skillListItem]
	items        []skills.Skill
	filterInput  textinput.Model
	installInput textinput.Model
	filterValue  string
	installMode  bool
	width        int
	height       int
}

type skillListItem struct {
	skill skills.Skill
}

func (s skillListItem) Render(selected bool, width int) string {
	t := theme.CurrentTheme()
	base := styles.BaseStyle().Width(width)
	titleStyle := base.Foreground(t.TextMuted())
	descStyle := base.Foreground(t.TextMuted())
	prefix := "  "
	if selected {
		prefix = "> "
		titleStyle = titleStyle.Foreground(t.Text()).Bold(true)
		descStyle = descStyle.Foreground(t.Text())
	}
	return lipgloss.JoinVertical(
		lipgloss.Left,
		titleStyle.Render(prefix+skillHeadline(s.skill)),
		descStyle.Render("  "+skillSubline(s.skill)),
	)
}

type skillKeyMap struct {
	Enter     key.Binding
	Escape    key.Binding
	Install   key.Binding
	Uninstall key.Binding
	Clear     key.Binding
}

var skillKeys = skillKeyMap{
	Enter:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "activate skill")),
	Escape:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close/back")),
	Install:   key.NewBinding(key.WithKeys("ctrl+i"), key.WithHelp("ctrl+i", "install skill")),
	Uninstall: key.NewBinding(key.WithKeys("ctrl+x"), key.WithHelp("ctrl+x", "uninstall skill")),
	Clear:     key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "clear filter")),
}

func (s *skillDialogCmp) Init() tea.Cmd {
	s.filterInput.Focus()
	return s.filterInput.Cursor.BlinkCmd()
}

func (s *skillDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, skillKeys.Enter):
			if s.installMode {
				source := strings.TrimSpace(s.installInput.Value())
				if source == "" {
					return s, util.ReportWarn("Enter a local skill path or GitHub tree URL")
				}
				s.installInput.SetValue("")
				s.installInput.Blur()
				s.installMode = false
				s.filterInput.Focus()
				return s, util.CmdHandler(SkillInstallRequestedMsg{Source: source})
			}
			item, idx := s.listView.GetSelectedItem()
			if idx != -1 {
				return s, util.CmdHandler(SkillSelectedMsg{Skill: item.skill})
			}
		case key.Matches(msg, skillKeys.Escape):
			if s.installMode {
				s.installMode = false
				s.installInput.Blur()
				s.filterInput.Focus()
				return s, nil
			}
			return s, util.CmdHandler(CloseSkillDialogMsg{})
		case key.Matches(msg, skillKeys.Install):
			s.installMode = true
			s.installInput.SetValue("")
			s.installInput.Focus()
			s.filterInput.Blur()
			return s, nil
		case key.Matches(msg, skillKeys.Uninstall):
			if s.installMode {
				return s, nil
			}
			item, idx := s.listView.GetSelectedItem()
			if idx != -1 {
				return s, util.CmdHandler(SkillUninstallRequestedMsg{Skill: item.skill})
			}
		case key.Matches(msg, skillKeys.Clear):
			if s.installMode {
				s.installInput.SetValue("")
				return s, nil
			}
			s.filterInput.SetValue("")
			s.applyFilter("")
		default:
			if s.installMode {
				before := s.installInput.Value()
				var cmd tea.Cmd
				s.installInput, cmd = s.installInput.Update(msg)
				if s.installInput.Value() != before {
					return s, cmd
				}
				return s, nil
			}
			if shouldUpdateDialogFilter(msg) {
				before := s.filterInput.Value()
				var cmd tea.Cmd
				s.filterInput, cmd = s.filterInput.Update(msg)
				if next := strings.TrimSpace(s.filterInput.Value()); next != strings.TrimSpace(before) {
					s.applyFilter(next)
				}
				return s, cmd
			}
		}
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		s.listView.SetMaxVisibleItems(max(6, min(18, s.height/2)))
	}

	u, cmd := s.listView.Update(msg)
	s.listView = u.(utilComponents.SimpleList[skillListItem])
	return s, cmd
}

func (s *skillDialogCmp) View() string {
	t := theme.CurrentTheme()
	base := styles.BaseStyle()
	maxWidth := 80
	if s.width > 0 {
		maxWidth = max(48, min(maxWidth, s.width-12))
	}
	s.listView.SetMaxWidth(maxWidth)
	s.filterInput.Width = maxWidth - 2
	s.installInput.Width = maxWidth - 2
	title := base.Foreground(t.Primary()).Bold(true).Width(maxWidth).Render(fmt.Sprintf("Skills (%d)", len(s.items)))

	searchBlock := lipgloss.JoinVertical(
		lipgloss.Left,
		base.Width(maxWidth).Foreground(t.TextMuted()).Render("SEARCH"),
		base.Width(maxWidth).Render(s.filterInput.View()),
	)

	footerText := "Type to filter. Enter activates the selected skill."
	if s.installMode {
		footerText = "Enter installs. Esc returns to browsing."
	}

	contentParts := []string{
		title,
		searchBlock,
		base.Width(maxWidth).Render(""),
		base.Width(maxWidth).Render(s.listView.View()),
	}
	if s.installMode {
		contentParts = append(contentParts,
			"",
			base.Width(maxWidth).Foreground(t.TextMuted()).Render("Install skill from local path or GitHub tree URL"),
			base.Width(maxWidth).Render(s.installInput.View()),
		)
	}
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		append(contentParts,
			base.Width(maxWidth).Foreground(t.TextMuted()).Render(footerText),
		)...,
	)
	return inlineSheet(base.Width(maxWidth).Render(content))
}

func (s *skillDialogCmp) BindingKeys() []key.Binding {
	bindings := layout.KeyMapToSlice(skillKeys)
	bindings = append(bindings, s.listView.BindingKeys()...)
	return bindings
}

func (s *skillDialogCmp) SetSkills(items []skills.Skill) {
	s.items = append([]skills.Skill{}, items...)
	s.applyFilter(strings.TrimSpace(s.filterInput.Value()))
}

func (s *skillDialogCmp) applyFilter(query string) {
	s.filterValue = strings.TrimSpace(query)
	filtered := filterSkills(s.items, s.filterValue)
	listItems := make([]skillListItem, 0, len(filtered))
	for _, item := range filtered {
		listItems = append(listItems, skillListItem{skill: item})
	}
	s.listView.SetItems(listItems)
}

func NewSkillDialogCmp() SkillDialog {
	filter := textinput.New()
	filter.Placeholder = "Search skills"
	filter.Prompt = "> "
	filter.CharLimit = 120
	filter.Cursor.Blink = true
	filter.Focus()

	install := textinput.New()
	install.Placeholder = "C:\\path\\to\\skill or https://github.com/<owner>/<repo>/tree/<ref>/<path>"
	install.Prompt = "install> "
	install.CharLimit = 512
	install.Cursor.Blink = true

	return &skillDialogCmp{
		listView:     utilComponents.NewSimpleList([]skillListItem{}, 12, "No matching skills", false),
		filterInput:  filter,
		installInput: install,
	}
}

func filterSkills(items []skills.Skill, query string) []skills.Skill {
	if strings.TrimSpace(query) == "" {
		return append([]skills.Skill{}, items...)
	}

	type scoredSkill struct {
		skill skills.Skill
		score int
	}

	scored := make([]scoredSkill, 0, len(items))
	for _, item := range items {
		score := scoreSkillMatch(item, query)
		if score <= 0 {
			continue
		}
		scored = append(scored, scoredSkill{skill: item, score: score})
	}

	slices.SortFunc(scored, func(a, b scoredSkill) int {
		if a.score != b.score {
			return b.score - a.score
		}
		return strings.Compare(strings.ToLower(a.skill.ID), strings.ToLower(b.skill.ID))
	})

	out := make([]skills.Skill, 0, len(scored))
	for _, item := range scored {
		out = append(out, item.skill)
	}
	return out
}

func scoreSkillMatch(item skills.Skill, query string) int {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return 1
	}

	id := strings.ToLower(item.ID)
	name := strings.ToLower(item.Name)
	description := strings.ToLower(item.Description)
	scope := strings.ToLower(string(item.Scope))
	source := strings.ToLower(item.Source)
	combined := strings.Join([]string{id, name, description, scope, source}, "\n")

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
	if strings.HasPrefix(id, query) {
		score += 140
	}
	if strings.HasPrefix(name, query) {
		score += 120
	}
	if strings.Contains(id, query) {
		score += 90
	}
	if strings.Contains(name, query) {
		score += 80
	}
	if strings.Contains(description, query) {
		score += 40
	}
	for _, token := range strings.Fields(query) {
		if strings.Contains(id, token) {
			score += 24
		}
		if strings.Contains(name, token) {
			score += 20
		}
		if strings.Contains(description, token) {
			score += 8
		}
		if strings.Contains(scope, token) || strings.Contains(source, token) {
			score += 6
		}
	}
	return score
}

func skillHeadline(item skills.Skill) string {
	headline := item.ID
	if item.Source == skills.UserInstalledExtensionName {
		headline += "  [installed]"
	}
	return headline
}

func skillSubline(item skills.Skill) string {
	parts := []string{fmt.Sprintf("scope: %s", item.Scope)}
	if item.Source != "" {
		parts = append(parts, fmt.Sprintf("source: %s", item.Source))
	}
	if item.Description != "" {
		parts = append(parts, item.Description)
	}
	return strings.Join(parts, "  ")
}
