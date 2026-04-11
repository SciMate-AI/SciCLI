package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/SciMate-AI/scicli/internal/llm/models"
	"github.com/SciMate-AI/scicli/internal/llm/prompt"
	"github.com/SciMate-AI/scicli/internal/llm/provider"
	"github.com/SciMate-AI/scicli/internal/llm/tools"
	"github.com/SciMate-AI/scicli/internal/logging"
	"github.com/SciMate-AI/scicli/internal/message"
	"github.com/SciMate-AI/scicli/internal/permission"
	"github.com/SciMate-AI/scicli/internal/pubsub"
	"github.com/SciMate-AI/scicli/internal/session"
	"github.com/SciMate-AI/scicli/internal/skills"
	"github.com/SciMate-AI/scicli/internal/taskrun"
)

// Common errors
var (
	ErrRequestCancelled = errors.New("request cancelled by user")
	ErrSessionBusy      = errors.New("session is currently processing another request")
)

type AgentEventType string

const (
	AgentEventTypeError     AgentEventType = "error"
	AgentEventTypeResponse  AgentEventType = "response"
	AgentEventTypeSummarize AgentEventType = "summarize"
)

type AgentEvent struct {
	Type            AgentEventType
	Message         message.Message
	Error           error
	TaskRunMetadata *taskrun.EventMetadata

	// When summarizing
	SessionID string
	Progress  string
	Done      bool
}

type Service interface {
	pubsub.Suscriber[AgentEvent]
	Model() models.Model
	Run(ctx context.Context, sessionID string, content string, attachments ...message.Attachment) (<-chan AgentEvent, error)
	Cancel(sessionID string)
	IsSessionBusy(sessionID string) bool
	IsBusy() bool
	Update(agentName config.AgentName, modelID models.ModelID) (models.Model, error)
	Summarize(ctx context.Context, sessionID string) error
}

type agent struct {
	*pubsub.Broker[AgentEvent]
	agentName config.AgentName
	maxTokens int64

	sessions session.Service
	messages message.Service

	tools     []tools.BaseTool
	provider  provider.Provider
	skillsSvc skills.Service
	taskRuns  taskrun.Service

	titleProvider     provider.Provider
	summarizeProvider provider.Provider

	activeRequests sync.Map
}

func NewAgent(
	agentName config.AgentName,
	sessions session.Service,
	messages message.Service,
	agentTools []tools.BaseTool,
	skillsSvc skills.Service,
	taskRuns taskrun.Service,
) (Service, error) {
	agentProvider, err := createAgentProvider(agentName)
	if err != nil {
		return nil, err
	}
	var titleProvider provider.Provider
	// Only generate titles for the coder agent
	if agentName == config.AgentCoder {
		titleProvider, err = createAgentProvider(config.AgentTitle)
		if err != nil {
			return nil, err
		}
	}
	var summarizeProvider provider.Provider
	if agentName == config.AgentCoder {
		summarizeProvider, err = createAgentProvider(config.AgentSummarizer)
		if err != nil {
			return nil, err
		}
	}

	agent := &agent{
		Broker:            pubsub.NewBroker[AgentEvent](),
		agentName:         agentName,
		provider:          agentProvider,
		maxTokens:         configuredAgentMaxTokens(agentName, agentProvider.Model()),
		messages:          messages,
		sessions:          sessions,
		tools:             agentTools,
		skillsSvc:         skillsSvc,
		taskRuns:          taskRuns,
		titleProvider:     titleProvider,
		summarizeProvider: summarizeProvider,
		activeRequests:    sync.Map{},
	}

	return agent, nil
}

func (a *agent) Model() models.Model {
	return a.provider.Model()
}

func (a *agent) Cancel(sessionID string) {
	// Cancel regular requests
	if cancelFunc, exists := a.activeRequests.LoadAndDelete(sessionID); exists {
		if cancel, ok := cancelFunc.(context.CancelFunc); ok {
			logging.InfoPersist(fmt.Sprintf("Request cancellation initiated for session: %s", sessionID))
			cancel()
		}
	}

	// Also check for summarize requests
	if cancelFunc, exists := a.activeRequests.LoadAndDelete(sessionID + "-summarize"); exists {
		if cancel, ok := cancelFunc.(context.CancelFunc); ok {
			logging.InfoPersist(fmt.Sprintf("Summarize cancellation initiated for session: %s", sessionID))
			cancel()
		}
	}
}

func (a *agent) IsBusy() bool {
	busy := false
	a.activeRequests.Range(func(key, value interface{}) bool {
		if cancelFunc, ok := value.(context.CancelFunc); ok {
			if cancelFunc != nil {
				busy = true
				return false // Stop iterating
			}
		}
		return true // Continue iterating
	})
	return busy
}

func (a *agent) IsSessionBusy(sessionID string) bool {
	_, busy := a.activeRequests.Load(sessionID)
	return busy
}

func (a *agent) generateTitle(ctx context.Context, sessionID string, content string) error {
	if content == "" {
		return nil
	}
	if a.titleProvider == nil {
		return nil
	}
	session, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	ctx = context.WithValue(ctx, tools.SessionIDContextKey, sessionID)
	parts := []message.ContentPart{message.TextContent{Text: content}}
	response, err := a.titleProvider.SendMessages(
		ctx,
		[]message.Message{
			{
				Role:  message.User,
				Parts: parts,
			},
		},
		make([]tools.BaseTool, 0),
	)
	if err != nil {
		return err
	}

	title := strings.TrimSpace(strings.ReplaceAll(response.Content, "\n", " "))
	if title == "" {
		return nil
	}

	session.Title = title
	_, err = a.sessions.Save(ctx, session)
	return err
}

func (a *agent) err(err error) AgentEvent {
	return AgentEvent{
		Type:  AgentEventTypeError,
		Error: err,
	}
}

func (a *agent) Run(ctx context.Context, sessionID string, content string, attachments ...message.Attachment) (<-chan AgentEvent, error) {
	if !a.provider.Model().SupportsAttachments && attachments != nil {
		attachments = nil
	}
	events := make(chan AgentEvent)
	if a.IsSessionBusy(sessionID) {
		return nil, ErrSessionBusy
	}
	sess, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	genCtx, cancel := context.WithCancel(ctx)

	a.activeRequests.Store(sessionID, cancel)
	if a.taskRuns != nil && sess.ParentSessionID != "" {
		a.taskRuns.Start(sess)
		a.taskRuns.RegisterCancel(sessionID, cancel)
	}
	go func() {
		logging.Debug("Request started", "sessionID", sessionID)
		defer logging.RecoverPanic("agent.Run", func() {
			events <- a.err(fmt.Errorf("panic while running the agent"))
		})
		defer func() {
			if a.taskRuns != nil {
				a.taskRuns.ClearCancel(sessionID)
			}
		}()
		var attachmentParts []message.ContentPart
		for _, attachment := range attachments {
			attachmentParts = append(attachmentParts, message.BinaryContent{Path: attachment.FilePath, MIMEType: attachment.MimeType, Data: attachment.Content})
		}
		result := a.processGeneration(genCtx, sessionID, content, attachmentParts)
		if result.Error != nil && !errors.Is(result.Error, ErrRequestCancelled) && !errors.Is(result.Error, context.Canceled) {
			logging.ErrorPersist(result.Error.Error())
		}
		a.publishTaskRunResult(sess, result)
		logging.Debug("Request completed", "sessionID", sessionID)
		a.activeRequests.Delete(sessionID)
		cancel()
		a.Publish(pubsub.CreatedEvent, result)
		events <- result
		close(events)
	}()
	return events, nil
}

func (a *agent) publishTaskRunResult(sess session.Session, result AgentEvent) {
	if a.taskRuns == nil || sess.ParentSessionID == "" {
		return
	}
	if result.Error != nil {
		metadata := result.TaskRunMetadata
		switch {
		case errors.Is(result.Error, ErrRequestCancelled), errors.Is(result.Error, context.Canceled):
			if metadata == nil {
				metadata = &taskrun.EventMetadata{FinishReason: string(message.FinishReasonCanceled)}
			}
			a.taskRuns.Finish(sess.ID, taskrun.StatusCanceled, "Canceled", metadata)
		default:
			if metadata == nil {
				metadata = &taskrun.EventMetadata{FinishReason: string(message.FinishReasonError)}
			}
			a.taskRuns.Finish(sess.ID, taskrun.StatusFailed, truncateTaskDetail(result.Error.Error()), metadata)
		}
		return
	}

	metadata := result.TaskRunMetadata
	switch result.Message.FinishReason() {
	case message.FinishReasonPermissionDenied:
		a.taskRuns.Finish(sess.ID, taskrun.StatusBlocked, "Waiting on permission", metadata)
	case message.FinishReasonCanceled:
		a.taskRuns.Finish(sess.ID, taskrun.StatusCanceled, "Canceled", metadata)
	case message.FinishReasonError:
		a.taskRuns.Finish(sess.ID, taskrun.StatusFailed, "Failed", metadata)
	default:
		detail := result.Message.Content().String()
		if strings.TrimSpace(detail) == "" {
			detail = "Finished successfully"
		}
		a.taskRuns.Finish(sess.ID, taskrun.StatusComplete, truncateTaskDetail(detail), metadata)
	}
}

func (a *agent) processGeneration(ctx context.Context, sessionID, content string, attachmentParts []message.ContentPart) AgentEvent {
	cfg := config.Get()
	// List existing messages; if none, start title generation asynchronously.
	msgs, err := a.messages.List(ctx, sessionID)
	if err != nil {
		return a.err(fmt.Errorf("failed to list messages: %w", err))
	}
	if len(msgs) == 0 {
		go func() {
			defer logging.RecoverPanic("agent.Run", func() {
				logging.ErrorPersist("panic while generating title")
			})
			titleErr := a.generateTitle(context.Background(), sessionID, content)
			if titleErr != nil {
				logging.ErrorPersist(fmt.Sprintf("failed to generate title: %v", titleErr))
			}
		}()
	}
	session, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return a.err(fmt.Errorf("failed to get session: %w", err))
	}
	if session.SummaryMessageID != "" {
		summaryMsgInex := -1
		for i, msg := range msgs {
			if msg.ID == session.SummaryMessageID {
				summaryMsgInex = i
				break
			}
		}
		if summaryMsgInex != -1 {
			msgs = msgs[summaryMsgInex:]
			msgs[0].Role = message.User
		}
	}

	userMsg, err := a.createUserMessage(ctx, sessionID, content, attachmentParts)
	if err != nil {
		return a.err(fmt.Errorf("failed to create user message: %w", err))
	}
	// Append the new user message to the conversation history.
	msgHistory := append(msgs, userMsg)
	loopState := newExecutionLoopState()

	for {
		// Check for cancellation before each iteration
		select {
		case <-ctx.Done():
			return a.err(ctx.Err())
		default:
			// Continue processing
		}
		persistentHistory, requestHistory, err := a.prepareHistoryForTurn(ctx, sessionID, msgHistory, &loopState)
		if err != nil {
			return a.err(fmt.Errorf("failed to prepare history: %w", err))
		}
		msgHistory = persistentHistory

		availableTools := a.tools
		if !loopState.toolsAllowed() {
			availableTools = nil
		}

		agentMessage, toolResults, err := a.streamWithRetry(ctx, sessionID, requestHistory, availableTools)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				agentMessage.AddFinish(message.FinishReasonCanceled)
				a.messages.Update(context.Background(), agentMessage)
				return a.err(ErrRequestCancelled)
			}
			return a.err(fmt.Errorf("failed to process events: %w", err))
		}
		if cfg.Debug {
			seqId := (len(requestHistory) + 1) / 2
			toolResultFilepath := logging.WriteToolResultsJson(sessionID, seqId, toolResults)
			logging.Info("Result", "message", agentMessage.FinishReason(), "toolResults", "{}", "filepath", toolResultFilepath)
		} else {
			logging.Info("Result", "message", agentMessage.FinishReason(), "toolResults", toolResults)
		}
		if (agentMessage.FinishReason() == message.FinishReasonToolUse) && toolResults != nil {
			loopState.toolRounds++
			msgHistory = append(msgHistory, agentMessage, *toolResults)
			continue
		}

		decision := sanitizeLoopDecision(&agentMessage)
		if err := a.messages.Update(context.Background(), agentMessage); err != nil {
			return a.err(fmt.Errorf("failed to update loop decision message: %w", err))
		}

		if loopState.phase == loopPhasePlan {
			msgHistory = append(msgHistory, agentMessage)
			loopState.phase = loopPhaseReview
			loopState.injectControl = true
			continue
		}

		if decision != loopDecisionComplete && !loopState.isLastStep() {
			msgHistory = append(msgHistory, agentMessage)
			loopState.currentStep++
			loopState.phase = loopPhasePlan
			loopState.injectControl = true
			continue
		}

		return AgentEvent{
			Type:            AgentEventTypeResponse,
			Message:         agentMessage,
			TaskRunMetadata: buildTaskRunMetadata(agentMessage),
			Done:            true,
		}
	}
}

func (a *agent) createUserMessage(ctx context.Context, sessionID, content string, attachmentParts []message.ContentPart) (message.Message, error) {
	parts := []message.ContentPart{message.TextContent{Text: content}}
	parts = append(parts, attachmentParts...)
	return a.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role:  message.User,
		Parts: parts,
	})
}

const (
	maxStreamRetries    = 3
	streamRetryBaseWait = 2 * time.Second
)

// streamWithRetry wraps streamAndHandleEvents with exponential-backoff retries
// for transient network errors (connection resets, EOF, timeouts from the LLM
// API).  Context cancellation and permission errors are never retried.
func (a *agent) streamWithRetry(ctx context.Context, sessionID string, msgHistory []message.Message, availableTools []tools.BaseTool) (message.Message, *message.Message, error) {
	var (
		msg         message.Message
		toolResults *message.Message
		err         error
	)
	for attempt := 0; attempt <= maxStreamRetries; attempt++ {
		if attempt > 0 {
			wait := streamRetryBaseWait * time.Duration(1<<(attempt-1))
			logging.Warn("Retrying LLM stream after transient error",
				"session_id", sessionID, "attempt", attempt, "wait", wait, "error", err)
			select {
			case <-ctx.Done():
				return msg, toolResults, ctx.Err()
			case <-time.After(wait):
			}
		}
		msg, toolResults, err = a.streamAndHandleEvents(ctx, sessionID, msgHistory, availableTools)
		if err == nil {
			return msg, toolResults, nil
		}
		// Never retry on explicit cancellation or permission denial.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrRequestCancelled) {
			return msg, toolResults, err
		}
		// Give up after the last attempt.
		if attempt == maxStreamRetries {
			break
		}
	}
	return msg, toolResults, err
}

func (a *agent) streamAndHandleEvents(ctx context.Context, sessionID string, msgHistory []message.Message, availableTools []tools.BaseTool) (message.Message, *message.Message, error) {
	ctx = context.WithValue(ctx, tools.SessionIDContextKey, sessionID)
	eventChan := a.provider.StreamResponse(ctx, msgHistory, availableTools)

	assistantMsg, err := a.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role:  message.Assistant,
		Parts: []message.ContentPart{},
		Model: a.provider.Model().ID,
	})
	if err != nil {
		return assistantMsg, nil, fmt.Errorf("failed to create assistant message: %w", err)
	}

	// Add the session and message ID into the context if needed by tools.
	ctx = context.WithValue(ctx, tools.MessageIDContextKey, assistantMsg.ID)

	// Process each event in the stream.
	for event := range eventChan {
		if processErr := a.processEvent(ctx, sessionID, &assistantMsg, event); processErr != nil {
			a.finishMessage(ctx, &assistantMsg, message.FinishReasonCanceled)
			return assistantMsg, nil, processErr
		}
		if ctx.Err() != nil {
			a.finishMessage(context.Background(), &assistantMsg, message.FinishReasonCanceled)
			return assistantMsg, nil, ctx.Err()
		}
	}

	toolResults := make([]message.ToolResult, len(assistantMsg.ToolCalls()))
	toolCalls := assistantMsg.ToolCalls()
	for i, toolCall := range toolCalls {
		select {
		case <-ctx.Done():
			a.finishMessage(context.Background(), &assistantMsg, message.FinishReasonCanceled)
			// Make all future tool calls cancelled
			for j := i; j < len(toolCalls); j++ {
				toolResults[j] = message.ToolResult{
					ToolCallID: toolCalls[j].ID,
					Content:    "Tool execution canceled by user",
					IsError:    true,
				}
			}
			goto out
		default:
			// Continue processing
			var tool tools.BaseTool
			for _, availableTool := range availableTools {
				if availableTool.Info().Name == toolCall.Name {
					tool = availableTool
					break
				}
				// Monkey patch for Copilot Sonnet-4 tool repetition obfuscation
				// if strings.HasPrefix(toolCall.Name, availableTool.Info().Name) &&
				// 	strings.HasPrefix(toolCall.Name, availableTool.Info().Name+availableTool.Info().Name) {
				// 	tool = availableTool
				// 	break
				// }
			}

			// Tool not found
			if tool == nil {
				toolResults[i] = message.ToolResult{
					ToolCallID: toolCall.ID,
					Content:    fmt.Sprintf("Tool not found: %s", toolCall.Name),
					IsError:    true,
				}
				continue
			}
			toolResult, toolErr := tool.Run(ctx, tools.ToolCall{
				ID:    toolCall.ID,
				Name:  toolCall.Name,
				Input: toolCall.Input,
			})
			if toolErr != nil {
				if errors.Is(toolErr, permission.ErrorPermissionDenied) {
					toolResults[i] = message.ToolResult{
						ToolCallID: toolCall.ID,
						Content:    "Permission denied",
						IsError:    true,
					}
					for j := i + 1; j < len(toolCalls); j++ {
						toolResults[j] = message.ToolResult{
							ToolCallID: toolCalls[j].ID,
							Content:    "Tool execution canceled by user",
							IsError:    true,
						}
					}
					a.finishMessage(ctx, &assistantMsg, message.FinishReasonPermissionDenied)
					break
				}
			}
			toolResult = compactToolResponseForModelContext(toolCall.Name, toolResult)
			toolResults[i] = message.ToolResult{
				ToolCallID: toolCall.ID,
				Content:    toolResult.Content,
				Metadata:   toolResult.Metadata,
				IsError:    toolResult.IsError,
			}
		}
	}
out:
	if len(toolResults) == 0 {
		return assistantMsg, nil, nil
	}
	parts := make([]message.ContentPart, 0)
	for _, tr := range toolResults {
		parts = append(parts, tr)
	}
	msg, err := a.messages.Create(context.Background(), assistantMsg.SessionID, message.CreateMessageParams{
		Role:  message.Tool,
		Parts: parts,
	})
	if err != nil {
		return assistantMsg, nil, fmt.Errorf("failed to create cancelled tool message: %w", err)
	}

	return assistantMsg, &msg, err
}

func (a *agent) finishMessage(ctx context.Context, msg *message.Message, finishReson message.FinishReason) {
	msg.AddFinish(finishReson)
	_ = a.messages.Update(ctx, *msg)
}

func (a *agent) processEvent(ctx context.Context, sessionID string, assistantMsg *message.Message, event provider.ProviderEvent) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Continue processing.
	}

	switch event.Type {
	case provider.EventThinkingDelta:
		a.publishTaskProgress(sessionID, "Reasoning", "", nil)
		assistantMsg.AppendReasoningContent(event.Content)
		return a.messages.Update(ctx, *assistantMsg)
	case provider.EventContentDelta:
		a.publishTaskProgress(sessionID, "Generating response", "", nil)
		assistantMsg.AppendContent(event.Content)
		return a.messages.Update(ctx, *assistantMsg)
	case provider.EventToolUseStart:
		toolName := toolNameForTask(event.ToolCall.Name)
		a.publishTaskProgress(
			sessionID,
			"Running tool "+toolName,
			toolName,
			&taskrun.EventMetadata{ToolInputPreview: previewTaskToolInput(event.ToolCall.Input)},
		)
		assistantMsg.AddToolCall(*event.ToolCall)
		return a.messages.Update(ctx, *assistantMsg)
	// TODO: see how to handle this
	// case provider.EventToolUseDelta:
	// 	tm := time.Unix(assistantMsg.UpdatedAt, 0)
	// 	assistantMsg.AppendToolCallInput(event.ToolCall.ID, event.ToolCall.Input)
	// 	if time.Since(tm) > 1000*time.Millisecond {
	// 		err := a.messages.Update(ctx, *assistantMsg)
	// 		assistantMsg.UpdatedAt = time.Now().Unix()
	// 		return err
	// 	}
	case provider.EventToolUseStop:
		assistantMsg.FinishToolCall(event.ToolCall.ID)
		return a.messages.Update(ctx, *assistantMsg)
	case provider.EventError:
		if errors.Is(event.Error, context.Canceled) {
			logging.InfoPersist(fmt.Sprintf("Event processing canceled for session: %s", sessionID))
			return context.Canceled
		}
		logging.ErrorPersist(event.Error.Error())
		errText := strings.TrimSpace(event.Error.Error())
		if errText != "" {
			if existing := strings.TrimSpace(assistantMsg.Content().String()); existing != "" {
				assistantMsg.SetContent(existing + "\n\nRequest failed:\n" + errText)
			} else {
				assistantMsg.SetContent("Request failed:\n" + errText)
			}
		}
		assistantMsg.AddFinish(message.FinishReasonError)
		_ = a.messages.Update(ctx, *assistantMsg)
		return event.Error
	case provider.EventComplete:
		if event.Response.GeminiRawContent != nil {
			assistantMsg.SetGeminiRawContent(*event.Response.GeminiRawContent)
		}
		assistantMsg.SetToolCalls(event.Response.ToolCalls)
		assistantMsg.AddFinish(event.Response.FinishReason)
		if err := a.messages.Update(ctx, *assistantMsg); err != nil {
			return fmt.Errorf("failed to update message: %w", err)
		}
		return a.TrackUsage(ctx, sessionID, a.provider.Model(), event.Response.Usage)
	}

	return nil
}

func (a *agent) publishTaskProgress(sessionID, detail, toolName string, metadata *taskrun.EventMetadata) {
	if a.taskRuns == nil {
		return
	}
	if run, ok := a.taskRuns.Get(sessionID); ok && run.ParentSessionID != "" {
		a.taskRuns.UpdateDetail(sessionID, truncateTaskDetail(detail), toolName, metadata)
	}
}

func truncateTaskDetail(detail string) string {
	detail = strings.TrimSpace(strings.ReplaceAll(detail, "\n", " "))
	if len(detail) <= 120 {
		return detail
	}
	return detail[:117] + "..."
}

func toolNameForTask(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "tool"
	}
	return name
}

func previewTaskToolInput(input string) string {
	input = strings.TrimSpace(strings.ReplaceAll(input, "\n", " "))
	if input == "" {
		return ""
	}
	return truncateTaskDetail(input)
}

func buildTaskRunMetadata(msg message.Message) *taskrun.EventMetadata {
	reason := strings.TrimSpace(string(msg.FinishReason()))
	if reason == "" {
		return nil
	}

	metadata := taskrun.EventMetadata{
		FinishReason: reason,
	}
	if msg.FinishReason() == message.FinishReasonPermissionDenied {
		if toolCall, ok := latestToolCall(msg); ok {
			metadata.ToolInputPreview = previewTaskToolInput(toolCall.Input)
			metadata.PermissionReason = permissionReasonForTask(toolCall.Name)
		} else {
			metadata.PermissionReason = "Tool execution requires permission approval"
		}
	}
	if metadata.IsZero() {
		return nil
	}
	return &metadata
}

func latestToolCall(msg message.Message) (message.ToolCall, bool) {
	toolCalls := msg.ToolCalls()
	if len(toolCalls) == 0 {
		return message.ToolCall{}, false
	}
	return toolCalls[len(toolCalls)-1], true
}

func permissionReasonForTask(toolName string) string {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return "Tool execution requires permission approval"
	}
	return "Permission approval required before running " + toolName
}

func (a *agent) TrackUsage(ctx context.Context, sessionID string, model models.Model, usage provider.TokenUsage) error {
	sess, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}

	cost := model.CostPer1MInCached/1e6*float64(usage.CacheCreationTokens) +
		model.CostPer1MOutCached/1e6*float64(usage.CacheReadTokens) +
		model.CostPer1MIn/1e6*float64(usage.InputTokens) +
		model.CostPer1MOut/1e6*float64(usage.OutputTokens)

	sess.Cost += cost
	sess.CompletionTokens = usage.OutputTokens + usage.CacheReadTokens
	sess.PromptTokens = usage.InputTokens + usage.CacheCreationTokens

	_, err = a.sessions.Save(ctx, sess)
	if err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}
	return nil
}

func (a *agent) Update(agentName config.AgentName, modelID models.ModelID) (models.Model, error) {
	if a.IsBusy() {
		return models.Model{}, fmt.Errorf("cannot change model while processing requests")
	}

	if err := config.UpdateAgentModel(agentName, modelID); err != nil {
		return models.Model{}, fmt.Errorf("failed to update config: %w", err)
	}

	provider, err := createAgentProvider(agentName)
	if err != nil {
		return models.Model{}, fmt.Errorf("failed to create provider for model %s: %w", modelID, err)
	}

	a.provider = provider
	a.maxTokens = configuredAgentMaxTokens(agentName, provider.Model())
	if a.agentName == config.AgentCoder {
		if err := a.reloadAuxProviders(); err != nil {
			return models.Model{}, err
		}
	}

	return a.provider.Model(), nil
}

func (a *agent) reloadAuxProviders() error {
	if a.titleProvider != nil {
		titleProvider, err := createAgentProvider(config.AgentTitle)
		if err != nil {
			return fmt.Errorf("failed to reload title model: %w", err)
		}
		a.titleProvider = titleProvider
	}
	if a.summarizeProvider != nil {
		summarizeProvider, err := createAgentProvider(config.AgentSummarizer)
		if err != nil {
			return fmt.Errorf("failed to reload summarizer model: %w", err)
		}
		a.summarizeProvider = summarizeProvider
	}
	return nil
}

func (a *agent) Summarize(ctx context.Context, sessionID string) error {
	if a.summarizeProvider == nil {
		return fmt.Errorf("summarize provider not available")
	}

	// Check if session is busy
	if a.IsSessionBusy(sessionID) {
		return ErrSessionBusy
	}

	// Create a new context with cancellation
	summarizeCtx, cancel := context.WithCancel(ctx)

	// Store the cancel function in activeRequests to allow cancellation
	a.activeRequests.Store(sessionID+"-summarize", cancel)

	go func() {
		defer a.activeRequests.Delete(sessionID + "-summarize")
		defer cancel()
		event := AgentEvent{
			Type:     AgentEventTypeSummarize,
			Progress: "Starting summarization...",
		}

		a.Publish(pubsub.CreatedEvent, event)
		// Get all messages from the session
		msgs, err := a.messages.List(summarizeCtx, sessionID)
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to list messages: %w", err),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
			return
		}
		summarizeCtx = context.WithValue(summarizeCtx, tools.SessionIDContextKey, sessionID)

		if len(msgs) == 0 {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("no messages to summarize"),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
			return
		}

		event = AgentEvent{
			Type:     AgentEventTypeSummarize,
			Progress: "Analyzing conversation...",
		}
		a.Publish(pubsub.CreatedEvent, event)

		// Add a system message to guide the summarization
		summarizePrompt := "Provide a detailed but concise summary of our conversation above. Focus on information that would be helpful for continuing the conversation, including what we did, what we're doing, which files we're working on, and what we're going to do next."

		// Create a new message with the summarize prompt
		promptMsg := message.Message{
			Role:  message.User,
			Parts: []message.ContentPart{message.TextContent{Text: summarizePrompt}},
		}

		// Append the prompt to the messages
		msgsWithPrompt := append(msgs, promptMsg)

		event = AgentEvent{
			Type:     AgentEventTypeSummarize,
			Progress: "Generating summary...",
		}

		a.Publish(pubsub.CreatedEvent, event)

		// Send the messages to the summarize provider
		response, err := a.summarizeProvider.SendMessages(
			summarizeCtx,
			msgsWithPrompt,
			make([]tools.BaseTool, 0),
		)
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to summarize: %w", err),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
			return
		}

		summary := strings.TrimSpace(response.Content)
		if summary == "" {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("empty summary returned"),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
			return
		}
		event = AgentEvent{
			Type:     AgentEventTypeSummarize,
			Progress: "Creating new session...",
		}

		a.Publish(pubsub.CreatedEvent, event)
		oldSession, err := a.sessions.Get(summarizeCtx, sessionID)
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to get session: %w", err),
				Done:  true,
			}

			a.Publish(pubsub.CreatedEvent, event)
			return
		}
		// Create a message in the new session with the summary
		msg, err := a.messages.Create(summarizeCtx, oldSession.ID, message.CreateMessageParams{
			Role: message.Assistant,
			Parts: []message.ContentPart{
				message.TextContent{Text: summary},
				message.Finish{
					Reason: message.FinishReasonEndTurn,
					Time:   time.Now().Unix(),
				},
			},
			Model: a.summarizeProvider.Model().ID,
		})
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to create summary message: %w", err),
				Done:  true,
			}

			a.Publish(pubsub.CreatedEvent, event)
			return
		}
		oldSession.SummaryMessageID = msg.ID
		oldSession.CompletionTokens = response.Usage.OutputTokens
		oldSession.PromptTokens = 0
		model := a.summarizeProvider.Model()
		usage := response.Usage
		cost := model.CostPer1MInCached/1e6*float64(usage.CacheCreationTokens) +
			model.CostPer1MOutCached/1e6*float64(usage.CacheReadTokens) +
			model.CostPer1MIn/1e6*float64(usage.InputTokens) +
			model.CostPer1MOut/1e6*float64(usage.OutputTokens)
		oldSession.Cost += cost
		_, err = a.sessions.Save(summarizeCtx, oldSession)
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to save session: %w", err),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
		}

		event = AgentEvent{
			Type:      AgentEventTypeSummarize,
			SessionID: oldSession.ID,
			Progress:  "Summary complete",
			Done:      true,
		}
		a.Publish(pubsub.CreatedEvent, event)
		// Send final success event with the new session ID
	}()

	return nil
}

func createAgentProvider(agentName config.AgentName) (provider.Provider, error) {
	cfg := config.Get()
	agentConfig, ok := cfg.Agents[agentName]
	if !ok {
		return nil, fmt.Errorf("agent %s not found", agentName)
	}
	model, ok := models.SupportedModels[agentConfig.Model]
	if !ok {
		return nil, fmt.Errorf("model %s not supported", agentConfig.Model)
	}

	providerCfg, ok := cfg.Providers[model.Provider]
	if !ok {
		return nil, fmt.Errorf("provider %s not supported", model.Provider)
	}
	if providerCfg.Disabled {
		return nil, fmt.Errorf("provider %s is not enabled", model.Provider)
	}
	maxTokens := model.DefaultMaxTokens
	if agentConfig.MaxTokens > 0 {
		maxTokens = agentConfig.MaxTokens
	}

	if model.Provider == models.ProviderOpenAICompatible {
		model.APIModel = providerCfg.Model
		if strings.TrimSpace(providerCfg.Model) != "" {
			model.Name = "OpenAI-compatible: " + providerCfg.Model
		}
	}

	opts := []provider.ProviderClientOption{
		provider.WithAPIKey(providerCfg.APIKey),
		provider.WithModel(model),
		provider.WithSystemMessage(prompt.GetAgentPrompt(agentName, model.Provider)),
		provider.WithMaxTokens(maxTokens),
	}
	if model.Provider == models.ProviderOpenAI || model.Provider == models.ProviderLocal || model.Provider == models.ProviderOpenAICompatible {
		openAIOptions := []provider.OpenAIOption{}
		if strings.TrimSpace(providerCfg.BaseURL) != "" {
			openAIOptions = append(openAIOptions, provider.WithOpenAIBaseURL(providerCfg.BaseURL))
		}
		if model.CanReason {
			openAIOptions = append(openAIOptions, provider.WithReasoningEffort(agentConfig.ReasoningEffort))
		}
		opts = append(
			opts,
			provider.WithOpenAIOptions(openAIOptions...),
		)
	} else if model.Provider == models.ProviderAnthropic && model.CanReason && agentName == config.AgentCoder {
		opts = append(
			opts,
			provider.WithAnthropicOptions(
				provider.WithAnthropicShouldThinkFn(provider.DefaultShouldThinkFn),
			),
		)
	}
	agentProvider, err := provider.NewProvider(
		model.Provider,
		opts...,
	)
	if err != nil {
		return nil, fmt.Errorf("could not create provider: %v", err)
	}

	return agentProvider, nil
}

func configuredAgentMaxTokens(agentName config.AgentName, model models.Model) int64 {
	cfg := config.Get()
	if cfg != nil {
		if agentCfg, ok := cfg.Agents[agentName]; ok && agentCfg.MaxTokens > 0 {
			return agentCfg.MaxTokens
		}
	}
	if model.DefaultMaxTokens > 0 {
		return model.DefaultMaxTokens
	}
	return config.MaxTokensFallbackDefault
}
