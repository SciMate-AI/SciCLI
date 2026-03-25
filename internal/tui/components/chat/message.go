package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/SciMate-AI/scicli/internal/diff"
	"github.com/SciMate-AI/scicli/internal/llm/agent"
	"github.com/SciMate-AI/scicli/internal/llm/models"
	"github.com/SciMate-AI/scicli/internal/llm/tools"
	"github.com/SciMate-AI/scicli/internal/message"
	"github.com/SciMate-AI/scicli/internal/tui/styles"
	"github.com/SciMate-AI/scicli/internal/tui/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type uiMessageType int

const (
	userMessageType uiMessageType = iota
	assistantMessageType
	toolMessageType

	maxResultHeight = 10
)

type uiMessage struct {
	ID          string
	messageType uiMessageType
	position    int
	height      int
	content     string
}

func toMarkdown(content string, focused bool, width int) string {
	r := styles.GetMarkdownRenderer(width)
	rendered, _ := r.Render(content)
	return rendered
}

func renderMessage(msg string, isUser bool, isFocused bool, width int, info ...string) string {
	t := theme.CurrentTheme()
	contentWidth := max(12, width-4)
	roleBadge := consoleBadge("scicli", t.BackgroundDarker(), t.Text())
	headerMeta := consoleMuted("assistant")
	if isUser {
		roleBadge = consoleBadge("operator", t.Secondary(), t.Background())
		headerMeta = consoleMuted("prompt")
	} else if isFocused {
		roleBadge = consoleBadge("live", t.Primary(), t.Background())
		headerMeta = consoleMuted("stream")
	}

	header := lipgloss.JoinHorizontal(
		lipgloss.Left,
		roleBadge,
		" ",
		headerMeta,
	)
	body := styles.ForceReplaceBackgroundWithLipgloss(toMarkdown(msg, isFocused, contentWidth), t.Background())
	body = strings.TrimSuffix(body, "\n")
	return consoleTranscriptBlock(width, header, body, info...)
}

func renderUserMessage(msg message.Message, isFocused bool, width int, position int) uiMessage {
	var styledAttachments []string
	t := theme.CurrentTheme()
	attachmentStyles := styles.BaseStyle().
		MarginLeft(1).
		Background(t.BackgroundDarker()).
		Foreground(t.Text())
	for _, attachment := range msg.BinaryContent() {
		file := filepath.Base(attachment.Path)
		var filename string
		if len(file) > 10 {
			filename = fmt.Sprintf(" %s %s...", styles.DocumentIcon, file[0:7])
		} else {
			filename = fmt.Sprintf(" %s %s", styles.DocumentIcon, file)
		}
		styledAttachments = append(styledAttachments, attachmentStyles.Render(filename))
	}
	content := ""
	if len(styledAttachments) > 0 {
		attachmentContent := styles.BaseStyle().Width(width).Render(lipgloss.JoinHorizontal(lipgloss.Left, styledAttachments...))
		content = renderMessage(msg.Content().String(), true, isFocused, width, attachmentContent)
	} else {
		content = renderMessage(msg.Content().String(), true, isFocused, width)
	}
	return uiMessage{
		ID:          msg.ID,
		messageType: userMessageType,
		position:    position,
		height:      lipgloss.Height(content),
		content:     content,
	}
}

func renderAssistantMessage(
	msg message.Message,
	msgIndex int,
	allMessages []message.Message,
	messagesService message.Service,
	focusedUIMessageId string,
	isSummary bool,
	expandTools bool,
	width int,
	position int,
) []uiMessage {
	messages := []uiMessage{}
	content := msg.Content().String()
	thinking := msg.IsThinking()
	thinkingContent := msg.ReasoningContent().Thinking
	finished := msg.IsFinished()
	finishData := msg.FinishPart()
	info := []string{}
	fallbackContent := ""

	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	if finished {
		modelName := string(msg.Model)
		if model, ok := models.SupportedModels[msg.Model]; ok {
			modelName = model.Name
		}
		switch finishData.Reason {
		case message.FinishReasonEndTurn:
			took := formatTimestampDiff(msg.CreatedAt, finishData.Time)
			info = append(info, baseStyle.Foreground(t.TextMuted()).Render(modelName+" ("+took+")"))
		case message.FinishReasonCanceled:
			info = append(info, baseStyle.Foreground(t.TextMuted()).Render(modelName+" (canceled)"))
		case message.FinishReasonError:
			info = append(info, baseStyle.Foreground(t.Error()).Render(modelName+" (error)"))
			if strings.TrimSpace(content) == "" {
				fallbackContent = "Request failed before the model returned content."
			}
		case message.FinishReasonPermissionDenied:
			info = append(info, baseStyle.Foreground(t.Warning()).Render(modelName+" (permission denied)"))
			if strings.TrimSpace(content) == "" {
				fallbackContent = "Permission denied while executing this step."
			}
		case message.FinishReasonMaxTokens:
			info = append(info, baseStyle.Foreground(t.Warning()).Render(modelName+" (max tokens)"))
			if strings.TrimSpace(content) == "" {
				fallbackContent = "Stopped because the response hit the token limit."
			}
		}
	}

	if strings.TrimSpace(content) == "" && fallbackContent != "" {
		content = fallbackContent
	}
	if content != "" || (finished && finishData.Reason == message.FinishReasonEndTurn) {
		if content == "" {
			content = "*Finished without output*"
		}
		if isSummary {
			info = append(info, consoleMuted("summary"))
		}

		rendered := renderMessage(content, false, msg.ID == focusedUIMessageId, width, info...)
		messages = append(messages, uiMessage{
			ID:          msg.ID,
			messageType: assistantMessageType,
			position:    position,
			height:      lipgloss.Height(rendered),
			content:     rendered,
		})
		position += messages[0].height + 1
	} else if thinking && thinkingContent != "" {
		rendered := renderMessage(thinkingContent, false, msg.ID == focusedUIMessageId, width, consoleMuted("reasoning"))
		messages = append(messages, uiMessage{
			ID:          msg.ID,
			messageType: assistantMessageType,
			position:    position,
			height:      lipgloss.Height(rendered),
			content:     rendered,
		})
		position += messages[len(messages)-1].height + 1
	}

	for i, toolCall := range msg.ToolCalls() {
		toolCallContent := renderToolMessage(
			toolCall,
			allMessages,
			messagesService,
			focusedUIMessageId,
			expandTools,
			false,
			width,
			i+1,
		)
		messages = append(messages, toolCallContent)
		position += toolCallContent.height + 1
	}
	return messages
}

func findToolResponse(toolCallID string, futureMessages []message.Message) *message.ToolResult {
	for _, msg := range futureMessages {
		for _, result := range msg.ToolResults() {
			if result.ToolCallID == toolCallID {
				return &result
			}
		}
	}
	return nil
}

func toolName(name string) string {
	switch name {
	case agent.AgentToolName:
		return "Task"
	case tools.BashToolName:
		return "Bash"
	case tools.EditToolName:
		return "Edit"
	case tools.FetchToolName:
		return "Fetch"
	case tools.GlobToolName:
		return "Glob"
	case tools.GrepToolName:
		return "Grep"
	case tools.LSToolName:
		return "List"
	case tools.SourcegraphToolName:
		return "Sourcegraph"
	case tools.ViewToolName:
		return "View"
	case tools.WriteToolName:
		return "Write"
	case tools.PatchToolName:
		return "Patch"
	}
	return name
}

func getToolAction(name string) string {
	switch name {
	case agent.AgentToolName:
		return "Preparing prompt..."
	case tools.BashToolName:
		return "Building command..."
	case tools.EditToolName:
		return "Preparing edit..."
	case tools.FetchToolName:
		return "Writing fetch..."
	case tools.GlobToolName:
		return "Finding files..."
	case tools.GrepToolName:
		return "Searching content..."
	case tools.LSToolName:
		return "Listing directory..."
	case tools.SourcegraphToolName:
		return "Searching code..."
	case tools.ViewToolName:
		return "Reading file..."
	case tools.WriteToolName:
		return "Preparing write..."
	case tools.PatchToolName:
		return "Preparing patch..."
	}
	return "Working..."
}

func renderParams(paramsWidth int, params ...string) string {
	if len(params) == 0 {
		return ""
	}
	mainParam := params[0]
	if len(mainParam) > paramsWidth {
		mainParam = mainParam[:paramsWidth-3] + "..."
	}

	if len(params) == 1 {
		return mainParam
	}
	otherParams := params[1:]
	if len(otherParams)%2 != 0 {
		otherParams = append(otherParams, "")
	}
	parts := make([]string, 0, len(otherParams)/2)
	for i := 0; i < len(otherParams); i += 2 {
		key := otherParams[i]
		value := otherParams[i+1]
		if value == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%s", key, value))
	}

	partsRendered := strings.Join(parts, ", ")
	remainingWidth := paramsWidth - lipgloss.Width(partsRendered) - 5
	if remainingWidth < 30 {
		return mainParam
	}

	if len(parts) > 0 {
		mainParam = fmt.Sprintf("%s (%s)", mainParam, strings.Join(parts, ", "))
	}

	return ansi.Truncate(mainParam, paramsWidth, "...")
}

func removeWorkingDirPrefix(path string) string {
	wd := config.WorkingDirectory()
	if strings.HasPrefix(path, wd) {
		path = strings.TrimPrefix(path, wd)
	}
	if strings.HasPrefix(path, "/") {
		path = strings.TrimPrefix(path, "/")
	}
	if strings.HasPrefix(path, "./") {
		path = strings.TrimPrefix(path, "./")
	}
	if strings.HasPrefix(path, "../") {
		path = strings.TrimPrefix(path, "../")
	}
	return path
}

func renderToolParams(paramWidth int, toolCall message.ToolCall) string {
	params := ""
	switch toolCall.Name {
	case agent.AgentToolName:
		var params agent.AgentParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		return renderParams(paramWidth, strings.ReplaceAll(params.Prompt, "\n", " "))
	case tools.BashToolName:
		var params tools.BashParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		return renderParams(paramWidth, strings.ReplaceAll(params.Command, "\n", " "))
	case tools.EditToolName:
		var params tools.EditParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		return renderParams(paramWidth, removeWorkingDirPrefix(params.FilePath))
	case tools.FetchToolName:
		var params tools.FetchParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		toolParams := []string{params.URL}
		if params.Format != "" {
			toolParams = append(toolParams, "format", params.Format)
		}
		if params.Timeout != 0 {
			toolParams = append(toolParams, "timeout", (time.Duration(params.Timeout) * time.Second).String())
		}
		return renderParams(paramWidth, toolParams...)
	case tools.GlobToolName:
		var params tools.GlobParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		toolParams := []string{params.Pattern}
		if params.Path != "" {
			toolParams = append(toolParams, "path", params.Path)
		}
		return renderParams(paramWidth, toolParams...)
	case tools.GrepToolName:
		var params tools.GrepParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		toolParams := []string{params.Pattern}
		if params.Path != "" {
			toolParams = append(toolParams, "path", params.Path)
		}
		if params.Include != "" {
			toolParams = append(toolParams, "include", params.Include)
		}
		if params.LiteralText {
			toolParams = append(toolParams, "literal", "true")
		}
		return renderParams(paramWidth, toolParams...)
	case tools.LSToolName:
		var params tools.LSParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		path := params.Path
		if path == "" {
			path = "."
		}
		return renderParams(paramWidth, path)
	case tools.SourcegraphToolName:
		var params tools.SourcegraphParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		return renderParams(paramWidth, params.Query)
	case tools.ViewToolName:
		var params tools.ViewParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		toolParams := []string{removeWorkingDirPrefix(params.FilePath)}
		if params.Limit != 0 {
			toolParams = append(toolParams, "limit", fmt.Sprintf("%d", params.Limit))
		}
		if params.Offset != 0 {
			toolParams = append(toolParams, "offset", fmt.Sprintf("%d", params.Offset))
		}
		return renderParams(paramWidth, toolParams...)
	case tools.WriteToolName:
		var params tools.WriteParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		return renderParams(paramWidth, removeWorkingDirPrefix(params.FilePath))
	default:
		params = renderParams(paramWidth, strings.ReplaceAll(toolCall.Input, "\n", " "))
	}
	return params
}

func truncateHeight(content string, height int) string {
	lines := strings.Split(content, "\n")
	if len(lines) > height {
		return strings.Join(lines[:height], "\n")
	}
	return content
}

func truncateToolContent(content string, expanded bool) (string, int) {
	lines := strings.Split(content, "\n")
	if expanded || len(lines) <= maxResultHeight {
		return content, 0
	}
	return strings.Join(lines[:maxResultHeight], "\n"), len(lines) - maxResultHeight
}

func renderDefaultToolContent(response message.ToolResult, expanded bool) (string, int) {
	var metadata agent.MCPToolResponseMetadata
	if err := json.Unmarshal([]byte(response.Metadata), &metadata); err == nil && strings.TrimSpace(metadata.RawContent) != "" {
		if expanded {
			return metadata.RawContent, 0
		}
		content, _ := truncateToolContent(response.Content, false)
		rawLines := strings.Count(metadata.RawContent, "\n") + 1
		contentLines := strings.Count(content, "\n") + 1
		hiddenLines := 0
		if rawLines > contentLines {
			hiddenLines = rawLines - contentLines
		}
		return content, hiddenLines
	}
	return truncateToolContent(response.Content, expanded)
}

func renderToolCollapseHint(width int, hiddenLines int) string {
	if hiddenLines <= 0 {
		return ""
	}
	return styles.BaseStyle().
		Width(width).
		Foreground(theme.CurrentTheme().TextMuted()).
		Render(fmt.Sprintf("... [%d more lines hidden, press %s to expand]", hiddenLines, toggleToolResultsKey.Help().Key))
}

func renderToolResponse(toolCall message.ToolCall, response message.ToolResult, width int, expanded bool) (string, int) {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	if response.IsError {
		errContent := fmt.Sprintf("Error: %s", strings.ReplaceAll(response.Content, "\n", " "))
		errContent = ansi.Truncate(errContent, width-1, "...")
		return baseStyle.Width(width).Foreground(t.Error()).Render(errContent), 0
	}

	resultContent, hiddenLines := renderDefaultToolContent(response, expanded)
	switch toolCall.Name {
	case agent.AgentToolName:
		return styles.ForceReplaceBackgroundWithLipgloss(toMarkdown(resultContent, false, width), t.Background()), hiddenLines
	case tools.BashToolName:
		resultContent = fmt.Sprintf("```bash\n%s\n```", resultContent)
		return styles.ForceReplaceBackgroundWithLipgloss(toMarkdown(resultContent, true, width), t.Background()), hiddenLines
	case tools.EditToolName:
		metadata := tools.EditResponseMetadata{}
		json.Unmarshal([]byte(response.Metadata), &metadata)
		truncDiff, diffHiddenLines := truncateToolContent(metadata.Diff, expanded)
		formattedDiff, _ := diff.FormatDiff(truncDiff, diff.WithTotalWidth(width))
		return formattedDiff, diffHiddenLines
	case tools.FetchToolName:
		var params tools.FetchParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		mdFormat := "markdown"
		switch params.Format {
		case "text":
			mdFormat = "text"
		case "html":
			mdFormat = "html"
		}
		resultContent = fmt.Sprintf("```%s\n%s\n```", mdFormat, resultContent)
		return styles.ForceReplaceBackgroundWithLipgloss(toMarkdown(resultContent, true, width), t.Background()), hiddenLines
	case tools.GlobToolName, tools.GrepToolName, tools.LSToolName, tools.SourcegraphToolName:
		return baseStyle.Width(width).Foreground(t.TextMuted()).Render(resultContent), hiddenLines
	case tools.ViewToolName:
		metadata := tools.ViewResponseMetadata{}
		json.Unmarshal([]byte(response.Metadata), &metadata)
		ext := filepath.Ext(metadata.FilePath)
		if ext != "" {
			ext = strings.ToLower(ext[1:])
		}
		viewContent, viewHiddenLines := truncateToolContent(metadata.Content, expanded)
		resultContent = fmt.Sprintf("```%s\n%s\n```", ext, viewContent)
		return styles.ForceReplaceBackgroundWithLipgloss(toMarkdown(resultContent, true, width), t.Background()), viewHiddenLines
	case tools.WriteToolName:
		params := tools.WriteParams{}
		json.Unmarshal([]byte(toolCall.Input), &params)
		ext := filepath.Ext(params.FilePath)
		if ext != "" {
			ext = strings.ToLower(ext[1:])
		}
		writeContent, writeHiddenLines := truncateToolContent(params.Content, expanded)
		resultContent = fmt.Sprintf("```%s\n%s\n```", ext, writeContent)
		return styles.ForceReplaceBackgroundWithLipgloss(toMarkdown(resultContent, true, width), t.Background()), writeHiddenLines
	default:
		resultContent = fmt.Sprintf("```text\n%s\n```", resultContent)
		return styles.ForceReplaceBackgroundWithLipgloss(toMarkdown(resultContent, true, width), t.Background()), hiddenLines
	}
}

func renderToolMessage(
	toolCall message.ToolCall,
	allMessages []message.Message,
	messagesService message.Service,
	focusedUIMessageId string,
	expandTools bool,
	nested bool,
	width int,
	position int,
) uiMessage {
	if nested {
		width = width - 3
	}

	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()
	response := findToolResponse(toolCall.ID, allMessages)

	header := lipgloss.JoinHorizontal(
		lipgloss.Left,
		consoleBadge(toolName(toolCall.Name), t.BackgroundDarker(), t.Text()),
		" ",
		consoleMuted(renderToolParams(max(12, width/2), toolCall)),
	)
	if nested {
		header = lipgloss.JoinHorizontal(lipgloss.Left, consoleMuted("->"), " ", header)
	}

	if !toolCall.Finished {
		content := consoleTranscriptBlock(
			width,
			header,
			baseStyle.Foreground(t.TextMuted()).Render(getToolAction(toolCall.Name)),
		)
		return uiMessage{
			messageType: toolMessageType,
			position:    position,
			height:      lipgloss.Height(content),
			content:     content,
		}
	}

	footers := []string{}
	if toolCall.Name == agent.AgentToolName {
		taskMessages, _ := messagesService.List(context.Background(), toolCall.ID)
		toolCalls := []message.ToolCall{}
		for _, v := range taskMessages {
			toolCalls = append(toolCalls, v.ToolCalls()...)
		}
		for _, call := range toolCalls {
			rendered := renderToolMessage(call, []message.Message{}, messagesService, focusedUIMessageId, expandTools, true, width, 0)
			footers = append(footers, rendered.content)
		}
	}

	if !nested {
		if response != nil {
			responseContent, hiddenLines := renderToolResponse(toolCall, *response, width-2, expandTools)
			footers = append(footers, strings.TrimSuffix(responseContent, "\n"))
			if hint := renderToolCollapseHint(width-2, hiddenLines); hint != "" {
				footers = append(footers, hint)
			}
		} else {
			footers = append(footers, baseStyle.
				Italic(true).
				Width(width-2).
				Foreground(t.TextMuted()).
				Render("Waiting for response..."))
		}
	}

	content := consoleTranscriptBlock(width, header, "", footers...)
	return uiMessage{
		messageType: toolMessageType,
		position:    position,
		height:      lipgloss.Height(content),
		content:     content,
	}
}

func formatTimestampDiff(start, end int64) string {
	diffSeconds := float64(end-start) / 1000.0
	if diffSeconds < 1 {
		return fmt.Sprintf("%dms", int(diffSeconds*1000))
	}
	if diffSeconds < 60 {
		return fmt.Sprintf("%.1fs", diffSeconds)
	}
	return fmt.Sprintf("%.1fm", diffSeconds/60)
}
