package chat

import (
	"strings"

	"github.com/SciMate-AI/scicli/internal/tui/styles"
	"github.com/SciMate-AI/scicli/internal/tui/theme"
	"github.com/charmbracelet/lipgloss"
)

func consoleSection(width int, title string, body ...string) string {
	t := theme.CurrentTheme()
	base := styles.BaseStyle()
	lines := make([]string, 0, len(body)+1)
	if strings.TrimSpace(title) != "" {
		lines = append(lines, base.
			Foreground(t.Primary()).
			Bold(true).
			Render(strings.ToUpper(title)))
	}
	for _, line := range body {
		if line == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, line)
	}
	return lipgloss.NewStyle().
		Width(max(1, width)).
		Padding(0, 1).
		Border(lipgloss.NormalBorder()).
		BorderForeground(t.BorderDim()).
		Background(t.Background()).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func consoleBadge(label string, bg, fg lipgloss.AdaptiveColor) string {
	return lipgloss.NewStyle().
		Padding(0, 1).
		Background(bg).
		Foreground(fg).
		Bold(true).
		Render(strings.ToUpper(strings.TrimSpace(label)))
}

func consoleMuted(text string) string {
	return styles.BaseStyle().
		Foreground(theme.CurrentTheme().TextMuted()).
		Render(text)
}

func consoleKey(keyText, label string) string {
	t := theme.CurrentTheme()
	return lipgloss.JoinHorizontal(
		lipgloss.Left,
		lipgloss.NewStyle().
			Padding(0, 1).
			MarginRight(1).
			Background(t.BackgroundDarker()).
			Foreground(t.Text()).
			Render(keyText),
		consoleMuted(label),
	)
}

func consoleDivider(width int, label string) string {
	t := theme.CurrentTheme()
	base := styles.BaseStyle().Foreground(t.BorderDim())
	if strings.TrimSpace(label) == "" || width < 12 {
		return base.Render(strings.Repeat("-", max(1, width)))
	}
	tag := " " + strings.ToUpper(strings.TrimSpace(label)) + " "
	lineWidth := max(1, width-lipgloss.Width(tag))
	return base.Render(tag + strings.Repeat("-", lineWidth))
}

func consoleTranscriptBlock(width int, header string, body string, footer ...string) string {
	base := styles.BaseStyle().Width(max(1, width))
	lines := []string{header}
	if strings.TrimSpace(body) != "" {
		lines = append(lines, lipgloss.NewStyle().PaddingLeft(2).Render(strings.TrimSuffix(body, "\n")))
	}
	for _, item := range footer {
		if strings.TrimSpace(item) == "" {
			continue
		}
		lines = append(lines, lipgloss.NewStyle().PaddingLeft(2).Render(item))
	}
	return base.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}
