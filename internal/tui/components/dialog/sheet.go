package dialog

import (
	"strings"

	"github.com/SciMate-AI/scicli/internal/tui/styles"
	"github.com/SciMate-AI/scicli/internal/tui/theme"
	"github.com/charmbracelet/lipgloss"
)

func inlineSheet(content string) string {
	t := theme.CurrentTheme()
	return lipgloss.NewStyle().
		Width(lipgloss.Width(content)).
		BorderTop(true).
		BorderForeground(t.BorderNormal()).
		PaddingTop(1).
		Render(content)
}

func sheetHeader(width int, title string, subtitle string) string {
	t := theme.CurrentTheme()
	base := styles.BaseStyle()
	lines := []string{
		base.Width(width).Foreground(t.Primary()).Bold(true).Render(strings.ToUpper(strings.TrimSpace(title))),
	}
	if subtitle = strings.TrimSpace(subtitle); subtitle != "" {
		lines = append(lines, base.Width(width).Foreground(t.TextMuted()).Render(subtitle))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func sheetBlock(width int, label string, content ...string) string {
	t := theme.CurrentTheme()
	lines := make([]string, 0, len(content)+1)
	if label = strings.TrimSpace(label); label != "" {
		lines = append(lines, lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(t.Primary()).
			Border(lipgloss.NormalBorder()).
			BorderForeground(t.Primary()).
			Render(strings.ToUpper(label)))
	}
	for _, item := range content {
		if strings.TrimSpace(item) == "" {
			continue
		}
		lines = append(lines, item)
	}
	return lipgloss.NewStyle().
		Width(width).
		BorderLeft(true).
		BorderForeground(t.BorderDim()).
		PaddingLeft(1).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}
