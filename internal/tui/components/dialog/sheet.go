package dialog

import (
	"github.com/SciMate-AI/scicli/internal/tui/theme"
	"github.com/charmbracelet/lipgloss"
)

func inlineSheet(content string) string {
	return lipgloss.NewStyle().
		BorderTop(true).
		BorderForeground(theme.CurrentTheme().BorderDim()).
		PaddingTop(1).
		Render(content)
}
