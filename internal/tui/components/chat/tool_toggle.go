package chat

import "github.com/charmbracelet/bubbles/key"

var toggleToolResultsKey = key.NewBinding(
	key.WithKeys("ctrl+g"),
	key.WithHelp("ctrl+g", "toggle tool output"),
)
