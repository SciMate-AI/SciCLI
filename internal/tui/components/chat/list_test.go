package chat

import (
	"strings"
	"testing"

	"github.com/SciMate-AI/scicli/internal/message"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/stretchr/testify/assert"
)

func TestMessagesCmpViewIncludesScrollbarAndFooter(t *testing.T) {
	m := &messagesCmp{
		width:    32,
		height:   8,
		messages: []message.Message{{ID: "msg-1"}},
		viewport: viewport.New(31, 6),
	}
	m.viewport.SetContent(strings.Join([]string{
		"line 1",
		"line 2",
		"line 3",
		"line 4",
		"line 5",
		"line 6",
		"line 7",
		"line 8",
	}, "\n"))
	m.contentLines = 8

	view := m.View()

	assert.Contains(t, view, "transcript")
	assert.True(t, strings.Contains(view, "#") || strings.Contains(view, "|"))
}
