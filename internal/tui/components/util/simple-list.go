package utilComponents

import (
	"fmt"
	"strings"

	"github.com/SciMate-AI/scicli/internal/tui/layout"
	"github.com/SciMate-AI/scicli/internal/tui/styles"
	"github.com/SciMate-AI/scicli/internal/tui/theme"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type SimpleListItem interface {
	Render(selected bool, width int) string
}

type SimpleList[T SimpleListItem] interface {
	tea.Model
	layout.Bindings
	SetMaxWidth(maxWidth int)
	SetMaxVisibleItems(maxVisibleItems int)
	SetSelectedIndex(idx int)
	GetSelectedItem() (item T, idx int)
	SetItems(items []T)
	GetItems() []T
}

type simpleListCmp[T SimpleListItem] struct {
	fallbackMsg         string
	items               []T
	selectedIdx         int
	scrollOffset        int
	maxWidth            int
	maxVisibleItems     int
	useAlphaNumericKeys bool
}

type simpleListKeyMap struct {
	Up        key.Binding
	Down      key.Binding
	PageUp    key.Binding
	PageDown  key.Binding
	UpAlpha   key.Binding
	DownAlpha key.Binding
}

var simpleListKeys = simpleListKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up"),
		key.WithHelp("up", "previous item"),
	),
	Down: key.NewBinding(
		key.WithKeys("down"),
		key.WithHelp("down", "next item"),
	),
	PageUp: key.NewBinding(
		key.WithKeys("pgup"),
		key.WithHelp("pgup", "page up"),
	),
	PageDown: key.NewBinding(
		key.WithKeys("pgdn"),
		key.WithHelp("pgdn", "page down"),
	),
	UpAlpha: key.NewBinding(
		key.WithKeys("k"),
		key.WithHelp("k", "previous item"),
	),
	DownAlpha: key.NewBinding(
		key.WithKeys("j"),
		key.WithHelp("j", "next item"),
	),
}

func (c *simpleListCmp[T]) Init() tea.Cmd {
	return nil
}

func (c *simpleListCmp[T]) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, simpleListKeys.Up) || (c.useAlphaNumericKeys && key.Matches(msg, simpleListKeys.UpAlpha)):
			c.moveSelection(-1)
			return c, nil
		case key.Matches(msg, simpleListKeys.Down) || (c.useAlphaNumericKeys && key.Matches(msg, simpleListKeys.DownAlpha)):
			c.moveSelection(1)
			return c, nil
		case key.Matches(msg, simpleListKeys.PageUp):
			c.moveSelection(-c.pageSize())
			return c, nil
		case key.Matches(msg, simpleListKeys.PageDown):
			c.moveSelection(c.pageSize())
			return c, nil
		}
	case tea.MouseMsg:
		if msg.Action != tea.MouseActionPress {
			return c, nil
		}
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			c.moveSelection(-1)
			return c, nil
		case tea.MouseButtonWheelDown:
			c.moveSelection(1)
			return c, nil
		}
	}

	return c, nil
}

func (c *simpleListCmp[T]) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(simpleListKeys)
}

func (c *simpleListCmp[T]) GetSelectedItem() (T, int) {
	if len(c.items) > 0 {
		return c.items[c.selectedIdx], c.selectedIdx
	}

	var zero T
	return zero, -1
}

func (c *simpleListCmp[T]) SetItems(items []T) {
	c.items = items
	c.selectedIdx = 0
	c.scrollOffset = 0
}

func (c *simpleListCmp[T]) GetItems() []T {
	return c.items
}

func (c *simpleListCmp[T]) SetMaxWidth(width int) {
	c.maxWidth = width
}

func (c *simpleListCmp[T]) SetMaxVisibleItems(maxVisibleItems int) {
	if maxVisibleItems <= 0 {
		maxVisibleItems = 1
	}
	c.maxVisibleItems = maxVisibleItems
	c.ensureVisible()
}

func (c *simpleListCmp[T]) SetSelectedIndex(idx int) {
	if len(c.items) == 0 {
		c.selectedIdx = 0
		c.scrollOffset = 0
		return
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(c.items) {
		idx = len(c.items) - 1
	}
	c.selectedIdx = idx
	c.ensureVisible()
}

func (c *simpleListCmp[T]) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	items := c.items
	maxWidth := c.maxWidth
	if maxWidth <= 0 {
		maxWidth = 1
	}
	maxVisibleItems := min(max(1, c.maxVisibleItems), len(items))
	startIdx := 0

	if len(items) == 0 {
		return baseStyle.
			Background(t.Background()).
			Padding(0, 1).
			Width(maxWidth).
			Render(c.fallbackMsg)
	}

	c.ensureVisible()
	startIdx = c.scrollOffset
	endIdx := min(startIdx+maxVisibleItems, len(items))

	listItems := make([]string, 0, maxVisibleItems)
	for i := startIdx; i < endIdx; i++ {
		listItems = append(listItems, items[i].Render(i == c.selectedIdx, maxWidth))
	}

	content := lipgloss.JoinVertical(lipgloss.Left, listItems...)
	if len(items) <= maxVisibleItems {
		return content
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		content,
		c.renderIndicator(startIdx, endIdx, len(items)),
	)
}

func (c *simpleListCmp[T]) pageSize() int {
	if c.maxVisibleItems <= 1 {
		return 1
	}
	return max(1, c.maxVisibleItems-1)
}

func (c *simpleListCmp[T]) moveSelection(delta int) {
	if len(c.items) == 0 || delta == 0 {
		return
	}
	next := c.selectedIdx + delta
	if next < 0 {
		next = 0
	}
	if next >= len(c.items) {
		next = len(c.items) - 1
	}
	c.selectedIdx = next
	c.ensureVisible()
}

func (c *simpleListCmp[T]) ensureVisible() {
	if len(c.items) == 0 {
		c.selectedIdx = 0
		c.scrollOffset = 0
		return
	}

	maxVisible := max(1, c.maxVisibleItems)
	maxOffset := max(0, len(c.items)-maxVisible)
	if c.selectedIdx < c.scrollOffset {
		c.scrollOffset = c.selectedIdx
	}
	if c.selectedIdx >= c.scrollOffset+maxVisible {
		c.scrollOffset = c.selectedIdx - maxVisible + 1
	}
	if c.scrollOffset > maxOffset {
		c.scrollOffset = maxOffset
	}
	if c.scrollOffset < 0 {
		c.scrollOffset = 0
	}
}

func (c *simpleListCmp[T]) renderIndicator(startIdx, endIdx, total int) string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	up := " "
	if startIdx > 0 {
		up = "^"
	}
	down := " "
	if endIdx < total {
		down = "v"
	}
	label := fmt.Sprintf("%s%s %d/%d  showing %d-%d", up, down, c.selectedIdx+1, total, startIdx+1, endIdx)

	return baseStyle.
		Foreground(t.TextMuted()).
		Width(max(1, c.maxWidth)).
		Render(strings.TrimSpace(label))
}

func NewSimpleList[T SimpleListItem](items []T, maxVisibleItems int, fallbackMsg string, useAlphaNumericKeys bool) SimpleList[T] {
	return &simpleListCmp[T]{
		fallbackMsg:         fallbackMsg,
		items:               items,
		maxVisibleItems:     maxVisibleItems,
		useAlphaNumericKeys: useAlphaNumericKeys,
	}
}
