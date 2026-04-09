package util

import (
	"strings"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
)

// PasteSingleLineTextInput pastes clipboard text into a single-line input.
// Any line breaks are stripped so pasted secrets/URLs/models stay on one line.
func PasteSingleLineTextInput(input *textinput.Model) error {
	if input == nil {
		return nil
	}

	raw, err := clipboard.ReadAll()
	if err != nil {
		return err
	}

	paste := strings.ReplaceAll(raw, "\r\n", "")
	paste = strings.ReplaceAll(paste, "\n", "")
	paste = strings.ReplaceAll(paste, "\r", "")
	if paste == "" {
		return nil
	}

	value := input.Value()
	pos := input.Position()
	runes := []rune(value)
	if pos < 0 {
		pos = 0
	}
	if pos > len(runes) {
		pos = len(runes)
	}

	updated := string(runes[:pos]) + paste + string(runes[pos:])
	input.SetValue(updated)
	input.SetCursor(pos + len([]rune(paste)))
	return nil
}

// PasteTextArea pastes clipboard text into the textarea at the current cursor.
func PasteTextArea(input *textarea.Model) error {
	if input == nil {
		return nil
	}

	raw, err := clipboard.ReadAll()
	if err != nil {
		return err
	}
	if raw == "" {
		return nil
	}

	input.InsertString(strings.ReplaceAll(raw, "\r\n", "\n"))
	return nil
}
