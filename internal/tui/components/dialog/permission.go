package dialog

import (
	"fmt"
	"strings"

	"github.com/SciMate-AI/scicli/internal/diff"
	"github.com/SciMate-AI/scicli/internal/llm/tools"
	"github.com/SciMate-AI/scicli/internal/permission"
	"github.com/SciMate-AI/scicli/internal/tui/layout"
	"github.com/SciMate-AI/scicli/internal/tui/styles"
	"github.com/SciMate-AI/scicli/internal/tui/theme"
	"github.com/SciMate-AI/scicli/internal/tui/util"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type PermissionAction string

// Permission responses
const (
	PermissionAllow           PermissionAction = "allow"
	PermissionAllowForSession PermissionAction = "allow_session"
	PermissionDeny            PermissionAction = "deny"
)

// PermissionResponseMsg represents the user's response to a permission request
type PermissionResponseMsg struct {
	Permission permission.PermissionRequest
	Action     PermissionAction
}

// PermissionDialogCmp interface for permission dialog component
type PermissionDialogCmp interface {
	tea.Model
	layout.Bindings
	SetPermissions(permission permission.PermissionRequest) tea.Cmd
}

type permissionsMapping struct {
	Left         key.Binding
	Right        key.Binding
	EnterSpace   key.Binding
	Allow        key.Binding
	AllowSession key.Binding
	Deny         key.Binding
	Tab          key.Binding
}

var permissionsKeys = permissionsMapping{
	Left: key.NewBinding(
		key.WithKeys("left"),
		key.WithHelp("left", "switch options"),
	),
	Right: key.NewBinding(
		key.WithKeys("right"),
		key.WithHelp("right", "switch options"),
	),
	EnterSpace: key.NewBinding(
		key.WithKeys("enter", " "),
		key.WithHelp("enter/space", "confirm"),
	),
	Allow: key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "allow"),
	),
	AllowSession: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "allow for session"),
	),
	Deny: key.NewBinding(
		key.WithKeys("d"),
		key.WithHelp("d", "deny"),
	),
	Tab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "switch options"),
	),
}

// permissionDialogCmp is the implementation of PermissionDialog
type permissionDialogCmp struct {
	width           int
	height          int
	permission      permission.PermissionRequest
	windowSize      tea.WindowSizeMsg
	contentViewPort viewport.Model
	selectedOption  int // 0: Allow, 1: Allow for session, 2: Deny

	diffCache     map[string]string
	markdownCache map[string]string
}

func (p *permissionDialogCmp) Init() tea.Cmd {
	return p.contentViewPort.Init()
}

func (p *permissionDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.windowSize = msg
		cmd := p.SetSize()
		cmds = append(cmds, cmd)
		p.markdownCache = make(map[string]string)
		p.diffCache = make(map[string]string)
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, permissionsKeys.Right) || key.Matches(msg, permissionsKeys.Tab):
			p.selectedOption = (p.selectedOption + 1) % 3
			return p, nil
		case key.Matches(msg, permissionsKeys.Left):
			p.selectedOption = (p.selectedOption + 2) % 3
		case key.Matches(msg, permissionsKeys.EnterSpace):
			return p, p.selectCurrentOption()
		case key.Matches(msg, permissionsKeys.Allow):
			return p, util.CmdHandler(PermissionResponseMsg{Action: PermissionAllow, Permission: p.permission})
		case key.Matches(msg, permissionsKeys.AllowSession):
			return p, util.CmdHandler(PermissionResponseMsg{Action: PermissionAllowForSession, Permission: p.permission})
		case key.Matches(msg, permissionsKeys.Deny):
			return p, util.CmdHandler(PermissionResponseMsg{Action: PermissionDeny, Permission: p.permission})
		default:
			// Pass other keys to viewport
			viewPort, cmd := p.contentViewPort.Update(msg)
			p.contentViewPort = viewPort
			cmds = append(cmds, cmd)
		}
	}

	return p, tea.Batch(cmds...)
}

func (p *permissionDialogCmp) selectCurrentOption() tea.Cmd {
	var action PermissionAction

	switch p.selectedOption {
	case 0:
		action = PermissionAllow
	case 1:
		action = PermissionAllowForSession
	case 2:
		action = PermissionDeny
	}

	return util.CmdHandler(PermissionResponseMsg{Action: action, Permission: p.permission})
}

func (p *permissionDialogCmp) renderButtons() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()
	options := []string{"  allow", "  allow for session", "  deny"}
	stylesForOption := []lipgloss.Style{
		baseStyle.Foreground(t.TextMuted()),
		baseStyle.Foreground(t.TextMuted()),
		baseStyle.Foreground(t.TextMuted()),
	}
	switch p.selectedOption {
	case 0:
		options[0] = "> allow"
		stylesForOption[0] = baseStyle.Foreground(t.Text()).Bold(true)
	case 1:
		options[1] = "> allow for session"
		stylesForOption[1] = baseStyle.Foreground(t.Text()).Bold(true)
	case 2:
		options[2] = "> deny"
		stylesForOption[2] = baseStyle.Foreground(t.Error()).Bold(true)
	}
	lines := []string{
		baseStyle.Foreground(t.TextMuted()).Render("OPTIONS"),
		stylesForOption[0].Render(options[0]),
		stylesForOption[1].Render(options[1]),
		stylesForOption[2].Render(options[2]),
		baseStyle.Foreground(t.TextMuted()).Render("enter confirm  left/right switch"),
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (p *permissionDialogCmp) renderHeader() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()
	labelStyle := baseStyle.Foreground(t.TextMuted())
	valueStyle := baseStyle.Foreground(t.Text())
	headerParts := []string{
		lipgloss.JoinHorizontal(lipgloss.Left, labelStyle.Render("Tool"), "  ", valueStyle.Render(p.permission.ToolName)),
		lipgloss.JoinHorizontal(lipgloss.Left, labelStyle.Render("Path"), "  ", valueStyle.Render(p.permission.Path)),
	}

	// Add tool-specific header information
	switch p.permission.ToolName {
	case tools.BashToolName:
		headerParts = append(headerParts, labelStyle.Bold(true).Render("Command preview"))
	case tools.EditToolName:
		params := p.permission.Params.(tools.EditPermissionsParams)
		headerParts = append(headerParts, lipgloss.JoinHorizontal(lipgloss.Left, labelStyle.Render("File"), "  ", valueStyle.Render(params.FilePath)))

	case tools.WriteToolName:
		params := p.permission.Params.(tools.WritePermissionsParams)
		headerParts = append(headerParts, lipgloss.JoinHorizontal(lipgloss.Left, labelStyle.Render("File"), "  ", valueStyle.Render(params.FilePath)))
	case tools.FetchToolName:
		headerParts = append(headerParts, labelStyle.Bold(true).Render("URL"))
	}

	return lipgloss.JoinVertical(lipgloss.Left, headerParts...)
}

func (p *permissionDialogCmp) renderBashContent() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	if pr, ok := p.permission.Params.(tools.BashPermissionsParams); ok {
		content := fmt.Sprintf("```bash\n%s\n```", pr.Command)

		// Use the cache for markdown rendering
		renderedContent := p.GetOrSetMarkdown(p.permission.ID, func() (string, error) {
			r := styles.GetMarkdownRenderer(p.width - 10)
			s, err := r.Render(content)
			return styles.ForceReplaceBackgroundWithLipgloss(s, t.Background()), err
		})

		finalContent := baseStyle.
			Width(p.contentViewPort.Width).
			Render(renderedContent)
		p.contentViewPort.SetContent(finalContent)
		return p.styleViewport()
	}
	return ""
}

func (p *permissionDialogCmp) renderEditContent() string {
	if pr, ok := p.permission.Params.(tools.EditPermissionsParams); ok {
		diff := p.GetOrSetDiff(p.permission.ID, func() (string, error) {
			return diff.FormatDiff(pr.Diff, diff.WithTotalWidth(p.contentViewPort.Width))
		})

		p.contentViewPort.SetContent(diff)
		return p.styleViewport()
	}
	return ""
}

func (p *permissionDialogCmp) renderPatchContent() string {
	if pr, ok := p.permission.Params.(tools.EditPermissionsParams); ok {
		diff := p.GetOrSetDiff(p.permission.ID, func() (string, error) {
			return diff.FormatDiff(pr.Diff, diff.WithTotalWidth(p.contentViewPort.Width))
		})

		p.contentViewPort.SetContent(diff)
		return p.styleViewport()
	}
	return ""
}

func (p *permissionDialogCmp) renderWriteContent() string {
	if pr, ok := p.permission.Params.(tools.WritePermissionsParams); ok {
		// Use the cache for diff rendering
		diff := p.GetOrSetDiff(p.permission.ID, func() (string, error) {
			return diff.FormatDiff(pr.Diff, diff.WithTotalWidth(p.contentViewPort.Width))
		})

		p.contentViewPort.SetContent(diff)
		return p.styleViewport()
	}
	return ""
}

func (p *permissionDialogCmp) renderFetchContent() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	if pr, ok := p.permission.Params.(tools.FetchPermissionsParams); ok {
		content := fmt.Sprintf("```bash\n%s\n```", pr.URL)

		// Use the cache for markdown rendering
		renderedContent := p.GetOrSetMarkdown(p.permission.ID, func() (string, error) {
			r := styles.GetMarkdownRenderer(p.width - 10)
			s, err := r.Render(content)
			return styles.ForceReplaceBackgroundWithLipgloss(s, t.Background()), err
		})

		finalContent := baseStyle.
			Width(p.contentViewPort.Width).
			Render(renderedContent)
		p.contentViewPort.SetContent(finalContent)
		return p.styleViewport()
	}
	return ""
}

func (p *permissionDialogCmp) renderDefaultContent() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	content := p.permission.Description

	// Use the cache for markdown rendering
	renderedContent := p.GetOrSetMarkdown(p.permission.ID, func() (string, error) {
		r := styles.GetMarkdownRenderer(p.width - 10)
		s, err := r.Render(content)
		return styles.ForceReplaceBackgroundWithLipgloss(s, t.Background()), err
	})

	finalContent := baseStyle.
		Width(p.contentViewPort.Width).
		Render(renderedContent)
	p.contentViewPort.SetContent(finalContent)

	if renderedContent == "" {
		return ""
	}

	return p.styleViewport()
}

func (p *permissionDialogCmp) styleViewport() string {
	t := theme.CurrentTheme()
	contentStyle := lipgloss.NewStyle().
		BorderLeft(true).
		BorderForeground(t.BorderDim()).
		PaddingLeft(1)

	return contentStyle.Render(p.contentViewPort.View())
}

func (p *permissionDialogCmp) render() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	title := baseStyle.Bold(true).Foreground(t.Text()).Render("Permission required")
	subtitle := baseStyle.Foreground(t.TextMuted()).Render(firstNonEmptyPermissionText(
		strings.TrimSpace(p.permission.Description),
		"Review this action before continuing.",
	))
	// Render header
	headerContent := p.renderHeader()
	// Render buttons
	buttons := p.renderButtons()

	// Calculate content height dynamically based on window size
	p.contentViewPort.Height = max(5, p.height-lipgloss.Height(headerContent)-lipgloss.Height(buttons)-lipgloss.Height(title)-lipgloss.Height(subtitle)-6)
	p.contentViewPort.Width = max(24, p.width-8)

	// Render content based on tool type
	var contentFinal string
	switch p.permission.ToolName {
	case tools.BashToolName:
		contentFinal = p.renderBashContent()
	case tools.EditToolName:
		contentFinal = p.renderEditContent()
	case tools.PatchToolName:
		contentFinal = p.renderPatchContent()
	case tools.WriteToolName:
		contentFinal = p.renderWriteContent()
	case tools.FetchToolName:
		contentFinal = p.renderFetchContent()
	default:
		contentFinal = p.renderDefaultContent()
	}

	content := lipgloss.JoinVertical(
		lipgloss.Top,
		permissionOutlineBadge("permission", t.Warning()),
		"",
		title,
		subtitle,
		"",
		headerContent,
		"",
		contentFinal,
		"",
		buttons,
	)

	sheet := baseStyle.
		Width(p.width).
		Height(p.height).
		Render(content)
	return lipgloss.Place(p.windowSize.Width, p.windowSize.Height, lipgloss.Center, lipgloss.Center, sheet)
}

func (p *permissionDialogCmp) View() string {
	return p.render()
}

func (p *permissionDialogCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(permissionsKeys)
}

func (p *permissionDialogCmp) SetSize() tea.Cmd {
	if p.permission.ID == "" {
		return nil
	}
	switch p.permission.ToolName {
	case tools.BashToolName:
		p.width = max(56, int(float64(p.windowSize.Width)*0.4))
		p.height = max(16, int(float64(p.windowSize.Height)*0.3))
	case tools.EditToolName:
		p.width = max(72, int(float64(p.windowSize.Width)*0.8))
		p.height = max(20, int(float64(p.windowSize.Height)*0.8))
	case tools.WriteToolName:
		p.width = max(72, int(float64(p.windowSize.Width)*0.8))
		p.height = max(20, int(float64(p.windowSize.Height)*0.8))
	case tools.FetchToolName:
		p.width = max(56, int(float64(p.windowSize.Width)*0.4))
		p.height = max(16, int(float64(p.windowSize.Height)*0.3))
	default:
		p.width = max(64, int(float64(p.windowSize.Width)*0.7))
		p.height = max(18, int(float64(p.windowSize.Height)*0.5))
	}
	p.width = min(max(40, p.width), max(40, p.windowSize.Width-4))
	p.height = min(max(12, p.height), max(12, p.windowSize.Height-4))
	return nil
}

func (p *permissionDialogCmp) SetPermissions(permission permission.PermissionRequest) tea.Cmd {
	p.permission = permission
	return p.SetSize()
}

// Helper to get or set cached diff content
func (c *permissionDialogCmp) GetOrSetDiff(key string, generator func() (string, error)) string {
	if cached, ok := c.diffCache[key]; ok {
		return cached
	}

	content, err := generator()
	if err != nil {
		return fmt.Sprintf("Error formatting diff: %v", err)
	}

	c.diffCache[key] = content

	return content
}

// Helper to get or set cached markdown content
func (c *permissionDialogCmp) GetOrSetMarkdown(key string, generator func() (string, error)) string {
	if cached, ok := c.markdownCache[key]; ok {
		return cached
	}

	content, err := generator()
	if err != nil {
		return fmt.Sprintf("Error rendering markdown: %v", err)
	}

	c.markdownCache[key] = content

	return content
}

func NewPermissionDialogCmp() PermissionDialogCmp {
	// Create viewport for content
	contentViewport := viewport.New(0, 0)

	return &permissionDialogCmp{
		contentViewPort: contentViewport,
		selectedOption:  0, // Default to "Allow"
		diffCache:       make(map[string]string),
		markdownCache:   make(map[string]string),
	}
}

func firstNonEmptyPermissionText(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func permissionOutlineBadge(label string, fg lipgloss.AdaptiveColor) string {
	return lipgloss.NewStyle().
		Padding(0, 1).
		Foreground(fg).
		Border(lipgloss.NormalBorder()).
		BorderForeground(fg).
		Render(strings.ToUpper(strings.TrimSpace(label)))
}
