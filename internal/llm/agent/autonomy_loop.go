package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/SciMate-AI/scicli/internal/llm/tools"
	"github.com/SciMate-AI/scicli/internal/logging"
	"github.com/SciMate-AI/scicli/internal/message"
)

const (
	defaultStepBudget               = 0
	contextCompactionThreshold      = 0.82
	contextCompactionReserveRatio   = 0.12
	minMessagesBeforeCompaction     = 10
	recentMessagesToKeepUncompacted = 8
	maxInRunCompactions             = 3
	approxCharsPerToken             = 4
)

type loopPhase string

const (
	loopPhasePlan   loopPhase = "plan"
	loopPhaseReview loopPhase = "review"
)

type loopDecision string

const (
	loopDecisionUnknown  loopDecision = "unknown"
	loopDecisionContinue loopDecision = "continue"
	loopDecisionComplete loopDecision = "complete"
)

type executionLoopState struct {
	stepBudget      int
	currentStep     int
	phase           loopPhase
	toolRounds      int
	compactionCount int
	injectControl   bool
	completionMode  loopCompletionMode
	terminalJSONKey string
}

type loopCompletionMode string

const (
	loopCompletionModeTagged         loopCompletionMode = "loop_tagged"
	loopCompletionModeStructuredJSON loopCompletionMode = "structured_json"
)

var loopDecisionPattern = regexp.MustCompile(`(?is)<agent_loop_status>\s*(continue|complete)\s*</agent_loop_status>`)
var executionPolicyPattern = regexp.MustCompile(`(?is)<scicli_execution_policy\b([^>]*)\/>`)
var executionPolicyAttrPattern = regexp.MustCompile(`([a-z_]+)="([^"]*)"`)

type promptExecutionPolicy struct {
	stepBudget      int
	completionMode  loopCompletionMode
	terminalJSONKey string
}

func newExecutionLoopState(policy promptExecutionPolicy) executionLoopState {
	stepBudget := defaultStepBudget
	if policy.stepBudget > 0 {
		stepBudget = policy.stepBudget
	}
	completionMode := loopCompletionModeTagged
	if policy.completionMode != "" {
		completionMode = policy.completionMode
	}
	return executionLoopState{
		stepBudget:      stepBudget,
		currentStep:     1,
		phase:           loopPhasePlan,
		injectControl:   true,
		completionMode:  completionMode,
		terminalJSONKey: strings.TrimSpace(policy.terminalJSONKey),
	}
}

func (s executionLoopState) hasStepBudget() bool {
	return s.stepBudget > 0
}

func (s executionLoopState) remainingSteps() int {
	if !s.hasStepBudget() {
		return -1
	}
	remaining := s.stepBudget - s.currentStep + 1
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (s executionLoopState) toolsAllowed() bool {
	if !s.hasStepBudget() {
		return true
	}
	return s.currentStep <= s.stepBudget
}

func (s executionLoopState) isLastStep() bool {
	if !s.hasStepBudget() {
		return false
	}
	return s.currentStep >= s.stepBudget
}

func (s executionLoopState) promptStepLabel() string {
	if !s.hasStepBudget() {
		return fmt.Sprintf("%d (no fixed step limit)", s.currentStep)
	}
	return fmt.Sprintf("%d/%d", s.currentStep, s.stepBudget)
}

func buildLoopControlMessage(state executionLoopState) message.Message {
	if state.completionMode == loopCompletionModeStructuredJSON {
		return buildStructuredLoopControlMessage(state)
	}
	switch state.phase {
	case loopPhaseReview:
		lastStepRule := "If more autonomous work remains, start your response with <agent_loop_status>continue</agent_loop_status> and keep going yourself."
		if state.isLastStep() {
			lastStepRule = "This is the last allowed step. Start your response with <agent_loop_status>complete</agent_loop_status> and provide the best final answer you can now, including any remaining limitations."
		}
		return message.Message{
			Role: message.User,
			Parts: []message.ContentPart{
				message.TextContent{Text: strings.TrimSpace(fmt.Sprintf(`
Autonomous loop controller.
Current step: %s.
Phase: review.

Review the work completed so far, including tool results and verification status.
- If obvious verification or follow-up work remains, do it now instead of stopping.
- %s
- If the task is fully complete, start your response with <agent_loop_status>complete</agent_loop_status> and then provide the final user-facing answer.
- Remove any internal planning language from the final answer.
`, state.promptStepLabel(), lastStepRule))},
			},
		}
	default:
		completionRule := "Do not give the final user-facing answer in this phase unless the task is already fully complete without more work."
		if config.Get().Automation.WorkMode == config.WorkModeUltrawork {
			completionRule = "In ultrawork mode, do not stop after partial progress. Keep executing until the task is actually complete or a hard blocker remains."
		}
		return message.Message{
			Role: message.User,
			Parts: []message.ContentPart{
				message.TextContent{Text: strings.TrimSpace(fmt.Sprintf(`
Autonomous loop controller.
Current step: %s.
Phase: plan and execute.

Update your internal plan, choose the single highest-value next action, and execute it now.
- Use tools immediately when they help.
- Do not stop just because one command or tool call finished.
- %s
- Keep any plain-text in this phase minimal.
`, state.promptStepLabel(), completionRule))},
			},
		}
	}
}

func buildStructuredLoopControlMessage(state executionLoopState) message.Message {
	switch state.phase {
	case loopPhaseReview:
		lastStepRule := "If work remains, keep going yourself. When the role contract is satisfied, return only the final JSON payload with no control tags."
		if state.isLastStep() {
			lastStepRule = "This is the last allowed step. Return the best valid JSON payload now. If required information is still missing, set status to failed and summarize the blocker."
		}
		return message.Message{
			Role: message.User,
			Parts: []message.ContentPart{
				message.TextContent{Text: strings.TrimSpace(fmt.Sprintf(`
Autonomous loop controller.
Current step: %s.
Phase: review.

Review the work completed so far, including tool results and verification status.
- Do not emit <agent_loop_status> tags for this workflow.
- If the role contract is satisfied, return only the final JSON payload now.
- %s
- Remove any internal planning language from the final payload.
`, state.promptStepLabel(), lastStepRule))},
			},
		}
	default:
		return message.Message{
			Role: message.User,
			Parts: []message.ContentPart{
				message.TextContent{Text: strings.TrimSpace(fmt.Sprintf(`
Autonomous loop controller.
Current step: %s.
Phase: plan and execute.

Update your internal plan, choose the single highest-value next action, and execute it now.
- Use tools immediately when they help.
- Keep plain-text in this phase minimal.
- Do not emit <agent_loop_status> tags for this workflow.
- Return the final JSON payload as soon as the role contract is satisfied.
`, state.promptStepLabel()))},
			},
		}
	}
}

func sanitizeLoopDecision(msg *message.Message) loopDecision {
	content := msg.Content().Text
	if strings.TrimSpace(content) == "" {
		return loopDecisionUnknown
	}

	match := loopDecisionPattern.FindStringSubmatch(content)
	if len(match) < 2 {
		return loopDecisionUnknown
	}

	cleaned := strings.TrimSpace(loopDecisionPattern.ReplaceAllString(content, ""))
	msg.SetContent(cleaned)
	_ = msg // kept for clarity with in-place mutation
	switch strings.ToLower(match[1]) {
	case string(loopDecisionContinue):
		return loopDecisionContinue
	case string(loopDecisionComplete):
		return loopDecisionComplete
	default:
		return loopDecisionUnknown
	}
}

func parsePromptExecutionPolicy(content string) promptExecutionPolicy {
	policy := promptExecutionPolicy{}
	match := executionPolicyPattern.FindStringSubmatch(content)
	if len(match) < 2 {
		return policy
	}
	for _, attr := range executionPolicyAttrPattern.FindAllStringSubmatch(match[1], -1) {
		if len(attr) < 3 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(attr[1]))
		value := strings.TrimSpace(attr[2])
		switch key {
		case "step_budget":
			if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
				policy.stepBudget = parsed
			}
		case "completion_mode":
			switch loopCompletionMode(value) {
			case loopCompletionModeStructuredJSON:
				policy.completionMode = loopCompletionModeStructuredJSON
			case loopCompletionModeTagged:
				policy.completionMode = loopCompletionModeTagged
			}
		case "terminal_json_key":
			policy.terminalJSONKey = value
		}
	}
	return policy
}

func inferStructuredLoopDecision(msg message.Message, state executionLoopState) loopDecision {
	if state.completionMode != loopCompletionModeStructuredJSON {
		return loopDecisionUnknown
	}
	cleaned := strings.TrimSpace(loopDecisionPattern.ReplaceAllString(msg.Content().Text, ""))
	if cleaned == "" || !strings.HasPrefix(cleaned, "{") {
		return loopDecisionUnknown
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(cleaned), &payload); err != nil {
		return loopDecisionUnknown
	}
	if state.terminalJSONKey == "" {
		return loopDecisionComplete
	}
	if _, ok := payload[state.terminalJSONKey]; ok {
		return loopDecisionComplete
	}
	return loopDecisionUnknown
}

func (a *agent) prepareHistoryForTurn(ctx context.Context, sessionID string, msgHistory []message.Message, state *executionLoopState) ([]message.Message, []message.Message, error) {
	compactedHistory, err := a.compactHistoryForContextWindow(ctx, sessionID, msgHistory, state)
	if err != nil {
		return nil, nil, err
	}

	requestHistory := append([]message.Message{}, compactedHistory...)
	if skillContext := a.buildSkillContextMessage(ctx, sessionID, msgHistory); skillContext != nil {
		requestHistory = append(requestHistory, *skillContext)
	}
	if state.injectControl {
		requestHistory = append(requestHistory, buildLoopControlMessage(*state))
		state.injectControl = false
	}
	return compactedHistory, requestHistory, nil
}

func (a *agent) buildSkillContextMessage(ctx context.Context, sessionID string, msgHistory []message.Message) *message.Message {
	if a.skillsSvc == nil {
		return nil
	}

	recommendedSkills, err := a.skillsSvc.Recommend(ctx, latestUserQuery(msgHistory), 12)
	if err != nil {
		logging.Warn("failed to recommend skills", "error", err)
		return nil
	}
	activeSkills := a.skillsSvc.Active(sessionID)
	if len(recommendedSkills) == 0 && len(activeSkills) == 0 {
		return nil
	}

	var body strings.Builder
	body.WriteString("Internal skill context for this session.\n")
	if len(recommendedSkills) > 0 {
		body.WriteString("If a task matches one of these skills, call activate_skill before following the skill instructions.\n")
		body.WriteString("<recommended_skills>\n")
		for _, skill := range recommendedSkills {
			fmt.Fprintf(&body, "- %s: %s\n", skill.ID, skill.Description)
		}
		body.WriteString("</recommended_skills>\n")
	}
	if len(activeSkills) > 0 {
		body.WriteString("<active_skills>\n")
		for _, skill := range activeSkills {
			fmt.Fprintf(&body, "<skill id=%q dir=%q scope=%q>\n", skill.ID, skill.Dir, skill.Scope)
			body.WriteString("Resolve relative paths in this skill from the dir above.\n")
			body.WriteString(strings.TrimSpace(skill.Content))
			body.WriteString("\n</skill>\n")
		}
		body.WriteString("</active_skills>")
	}

	return &message.Message{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: body.String()}},
	}
}

func latestUserQuery(history []message.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role != message.User {
			continue
		}
		text := strings.TrimSpace(history[i].Content().Text)
		if text != "" {
			return text
		}
	}
	return ""
}

func (a *agent) compactHistoryForContextWindow(ctx context.Context, sessionID string, msgHistory []message.Message, state *executionLoopState) ([]message.Message, error) {
	if !config.Get().AutoCompact || len(msgHistory) < minMessagesBeforeCompaction || state.compactionCount >= maxInRunCompactions {
		return msgHistory, nil
	}

	model := a.provider.Model()
	if model.ContextWindow <= 0 {
		return msgHistory, nil
	}

	for historyNeedsCompaction(msgHistory, model.ContextWindow, a.maxTokens) && state.compactionCount < maxInRunCompactions {
		boundary := findHistoryCompactionBoundary(msgHistory, recentMessagesToKeepUncompacted)
		if boundary <= 0 {
			return msgHistory, nil
		}

		summary, err := a.summarizeHistorySlice(ctx, sessionID, msgHistory[:boundary])
		if err != nil {
			return msgHistory, err
		}

		msgHistory = append([]message.Message{buildCompactedHistoryMessage(summary)}, msgHistory[boundary:]...)
		state.compactionCount++
		logging.Info("Compacted message history before provider call", "sessionID", sessionID, "step", state.currentStep, "compactions", state.compactionCount)
	}

	return msgHistory, nil
}

func historyNeedsCompaction(msgHistory []message.Message, contextWindow, maxTokens int64) bool {
	if contextWindow <= 0 {
		return false
	}

	estimatedInput := estimateHistoryTokens(msgHistory)
	reserve := maxTokens
	if reserve <= 0 {
		reserve = int64(float64(contextWindow) * contextCompactionReserveRatio)
	}

	estimatedTotal := estimatedInput + reserve
	return float64(estimatedTotal) >= float64(contextWindow)*contextCompactionThreshold
}

func estimateHistoryTokens(msgHistory []message.Message) int64 {
	var chars int
	for _, msg := range msgHistory {
		chars += 24
		switch msg.Role {
		case message.User:
			chars += len(msg.Content().Text)
			for _, binary := range msg.BinaryContent() {
				chars += len(binary.Data) / 3
			}
		case message.Assistant:
			chars += len(msg.Content().Text)
			for _, toolCall := range msg.ToolCalls() {
				chars += len(toolCall.Name) + len(toolCall.Input) + 24
			}
		case message.Tool:
			for _, result := range msg.ToolResults() {
				chars += len(result.Content) + 16
			}
		}
	}

	if chars <= 0 {
		return 0
	}
	return int64(chars/approxCharsPerToken + 1)
}

func findHistoryCompactionBoundary(msgHistory []message.Message, tailKeep int) int {
	if len(msgHistory) < tailKeep+2 {
		return -1
	}

	start := len(msgHistory) - tailKeep
	if start <= 0 {
		return -1
	}

	for i := start; i > 0; i-- {
		if msgHistory[i].Role == message.User {
			return i
		}
	}
	return -1
}

func (a *agent) summarizeHistorySlice(ctx context.Context, sessionID string, history []message.Message) (string, error) {
	if len(history) == 0 {
		return "", nil
	}

	if a.summarizeProvider != nil {
		summarizeCtx := context.WithValue(ctx, tools.SessionIDContextKey, sessionID)
		promptMsg := message.Message{
			Role: message.User,
			Parts: []message.ContentPart{
				message.TextContent{Text: strings.TrimSpace(`
Compress the earlier conversation for continued autonomous execution.
Preserve:
- the user's real goal and constraints
- what was already tried and what happened
- relevant files, commands, edits, and tool results
- open questions, blockers, and the best next actions

Be concise but complete enough that the task can continue without the dropped messages.
`)},
			},
		}

		response, err := a.summarizeProvider.SendMessages(summarizeCtx, append(append([]message.Message{}, history...), promptMsg), make([]tools.BaseTool, 0))
		if err == nil {
			if summary := strings.TrimSpace(response.Content); summary != "" {
				return summary, nil
			}
		}
		logging.Warn("Falling back to local history compaction", "error", err)
	}

	return fallbackHistorySummary(history), nil
}

func buildCompactedHistoryMessage(summary string) message.Message {
	return message.Message{
		Role: message.User,
		Parts: []message.ContentPart{
			message.TextContent{Text: strings.TrimSpace(fmt.Sprintf(`
Earlier conversation was compacted automatically because the session was approaching the model context window.

Compressed history:
%s

Treat that compressed history as authoritative for everything before the recent messages that follow.
`, summary))},
		},
	}
}

func fallbackHistorySummary(history []message.Message) string {
	lines := make([]string, 0, len(history))
	for _, msg := range history {
		summary := summarizeMessageForFallback(msg)
		if summary == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", msg.Role, summary))
	}

	if len(lines) == 0 {
		return "Earlier conversation included prior attempts, tool calls, and intermediate results."
	}

	if len(lines) > 24 {
		lines = append(lines[:24], fmt.Sprintf("- [omitted %d earlier summary lines]", len(lines)-24))
	}

	return strings.Join(lines, "\n")
}

func summarizeMessageForFallback(msg message.Message) string {
	parts := make([]string, 0, 4)
	if text := strings.TrimSpace(msg.Content().Text); text != "" {
		parts = append(parts, trimLineLength(strings.ReplaceAll(text, "\n", " "), 280))
	}
	for _, toolCall := range msg.ToolCalls() {
		parts = append(parts, trimLineLength(fmt.Sprintf("tool call %s %s", toolCall.Name, strings.ReplaceAll(toolCall.Input, "\n", " ")), 220))
	}
	for _, toolResult := range msg.ToolResults() {
		parts = append(parts, trimLineLength(strings.ReplaceAll(toolResult.Content, "\n", " "), 220))
	}
	return strings.Join(parts, " | ")
}
