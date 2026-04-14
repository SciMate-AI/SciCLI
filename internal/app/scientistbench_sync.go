package app

import (
	"context"
	"strings"
	"time"

	"github.com/SciMate-AI/scicli/internal/logging"
	"github.com/SciMate-AI/scicli/internal/message"
	"github.com/SciMate-AI/scicli/internal/pubsub"
	"github.com/SciMate-AI/scicli/internal/scientistbench"
	"github.com/SciMate-AI/scicli/internal/taskrun"
)

const maxScientistBenchAgentMessageLen = 500

const (
	scientistBenchMessageKindAgent  = "agent"
	scientistBenchMessageKindStatus = "status"
)

func (app *App) startScientistBenchRunSync(ctx context.Context) {
	if app.ScientistBench == nil || app.TaskRuns == nil || app.Messages == nil {
		return
	}

	watchCtx, cancel := context.WithCancel(ctx)
	app.cancelFuncsMutex.Lock()
	app.watcherCancelFuncs = append(app.watcherCancelFuncs, cancel)
	app.cancelFuncsMutex.Unlock()

	app.watcherWG.Add(1)
	go func() {
		defer app.watcherWG.Done()
		defer logging.RecoverPanic("scientistbench-run-sync", nil)

		sub := app.TaskRuns.SubscribeEvents(watchCtx)
		for {
			select {
			case <-watchCtx.Done():
				return
			case event, ok := <-sub:
				if !ok {
					return
				}
				if event.Type != pubsub.CreatedEvent {
					continue
				}
				payload := event.Payload
				if !looksLikeScientistBenchTask(payload.SessionID) {
					continue
				}
				item, run, ok := app.findScientistBenchRunByTaskSession(watchCtx, payload.SessionID)
				if !ok {
					continue
				}
				if toolName := strings.TrimSpace(payload.ToolName); toolName != "" {
					_ = app.recordScientistBenchToolCall(watchCtx, item.ID, run.ID, toolName, string(payload.Status))
				}
				meta := scientistBenchTaskEventContent(item, run, payload)
				if meta == nil {
					continue
				}
				if err := app.upsertScientistBenchStatusMessage(watchCtx, item.RootSessionID, run.ID, *meta); err != nil {
					logging.Warn("Failed to mirror scientist bench task progress", "case_id", item.ID, "session_id", payload.SessionID, "error", err)
				}
			}
		}
	}()
}

func (app *App) postScientistBenchLaunch(ctx context.Context, item scientistbench.Case, run scientistbench.RunRecord, prefix string) error {
	meta := scientistBenchLaunchContent(item, run, prefix)
	return app.upsertScientistBenchStatusMessage(ctx, item.RootSessionID, run.ID, meta)
}

func (app *App) postScientistBenchNodeResult(ctx context.Context, item scientistbench.Case, run scientistbench.RunRecord) error {
	meta := scientistBenchNodeResultContent(item, run)
	return app.upsertScientistBenchStatusMessage(ctx, item.RootSessionID, run.ID, meta)
}

func (app *App) upsertScientistBenchStatusMessage(
	ctx context.Context,
	sessionID string,
	runID string,
	meta message.ScientistBenchContent,
) error {
	return app.upsertScientistBenchMirror(ctx, sessionID, scientistBenchStatusMirrorKey(runID), meta, "", scientistBenchStateFinishReason(meta.State))
}

func (app *App) upsertScientistBenchAgentMessage(
	ctx context.Context,
	item scientistbench.Case,
	run scientistbench.RunRecord,
	source message.Message,
) error {
	text := strings.TrimSpace(source.Content().Text)
	if text == "" || isScientistBenchStructuredOutput(text) {
		return nil
	}

	meta := message.ScientistBenchContent{
		Kind:          scientistBenchMessageKindAgent,
		AgentID:       firstNonEmpty(run.Role, item.GraphState.ActiveRole, "agent"),
		AgentLabel:    scientistBenchAgentLabel(firstNonEmpty(run.Role, item.GraphState.ActiveRole, "agent")),
		State:         scientistBenchAgentState(source),
		Title:         scientistBenchAgentTitle(source),
		CaseID:        item.ID,
		NodeID:        firstNonEmpty(run.NodeID, item.GraphState.ActiveNode),
		RunID:         run.ID,
		TaskSessionID: firstNonEmpty(run.TaskRunSessionID, run.SessionID),
	}
	return app.upsertScientistBenchMirror(
		ctx,
		item.RootSessionID,
		scientistBenchAgentMirrorKey(source.ID),
		meta,
		truncateWithEllipsis(text, maxScientistBenchAgentMessageLen),
		scientistBenchAgentFinishReason(source),
	)
}

func (app *App) upsertScientistBenchMirror(
	ctx context.Context,
	sessionID string,
	mirrorKey string,
	meta message.ScientistBenchContent,
	body string,
	finishReason message.FinishReason,
) error {
	if app.Messages == nil {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	mirrorKey = strings.TrimSpace(mirrorKey)
	if sessionID == "" || mirrorKey == "" {
		return nil
	}

	if msgID, ok := app.lookupScientistBenchMirror(mirrorKey, meta.Kind); ok {
		msg, err := app.Messages.Get(ctx, msgID)
		if err == nil {
			msg.SetScientistBenchContent(meta)
			msg.SetContent(body)
			if finishReason != "" {
				msg.AddFinish(finishReason)
			}
			if updateErr := app.Messages.Update(ctx, msg); updateErr == nil {
				return nil
			}
		}
		app.deleteScientistBenchMirror(mirrorKey, meta.Kind)
	}

	parts := []message.ContentPart{
		meta,
		message.TextContent{Text: body},
	}
	if finishReason != "" {
		parts = append(parts, message.Finish{
			Reason: finishReason,
			Time:   time.Now().Unix(),
		})
	}
	msg, err := app.Messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role:  message.Assistant,
		Parts: parts,
	})
	if err != nil {
		return err
	}
	app.storeScientistBenchMirror(mirrorKey, meta.Kind, msg.ID)
	return nil
}

func (app *App) findScientistBenchRunByTaskSession(ctx context.Context, taskSessionID string) (scientistbench.Case, scientistbench.RunRecord, bool) {
	if app.ScientistBench == nil {
		return scientistbench.Case{}, scientistbench.RunRecord{}, false
	}
	taskSessionID = strings.TrimSpace(taskSessionID)
	if taskSessionID == "" {
		return scientistbench.Case{}, scientistbench.RunRecord{}, false
	}

	items, err := app.ScientistBench.List(ctx)
	if err != nil {
		return scientistbench.Case{}, scientistbench.RunRecord{}, false
	}
	for _, item := range items {
		for i := len(item.Runs) - 1; i >= 0; i-- {
			run := item.Runs[i]
			if run.TaskRunSessionID == taskSessionID || run.SessionID == taskSessionID {
				return item, run, true
			}
		}
	}
	return scientistbench.Case{}, scientistbench.RunRecord{}, false
}

func looksLikeScientistBenchTask(sessionID string) bool {
	return strings.HasPrefix(strings.TrimSpace(sessionID), "sbtask-")
}

func scientistBenchLaunchContent(item scientistbench.Case, run scientistbench.RunRecord, prefix string) message.ScientistBenchContent {
	detail := strings.TrimSpace(prefix)
	if detail == "" {
		detail = "Agent scheduled"
	}
	if title := strings.TrimSpace(item.Title); title != "" {
		detail = detail + " • " + title
	}
	return message.ScientistBenchContent{
		Kind:          scientistBenchMessageKindStatus,
		AgentID:       firstNonEmpty(run.Role, item.GraphState.ActiveRole, "agent"),
		AgentLabel:    scientistBenchAgentLabel(firstNonEmpty(run.Role, item.GraphState.ActiveRole, "agent")),
		State:         "started",
		Title:         "ScientistBench",
		Detail:        detail,
		CaseID:        item.ID,
		NodeID:        firstNonEmpty(run.NodeID, item.GraphState.ActiveNode),
		RunID:         run.ID,
		TaskSessionID: firstNonEmpty(run.TaskRunSessionID, run.SessionID),
	}
}

func scientistBenchTaskEventContent(item scientistbench.Case, run scientistbench.RunRecord, event taskrun.Event) *message.ScientistBenchContent {
	state := scientistBenchEventLabel(event)
	if state == "" {
		return nil
	}

	title := scientistBenchEventTitle(event)
	detail := scientistBenchEventDetail(event)
	return &message.ScientistBenchContent{
		Kind:          scientistBenchMessageKindStatus,
		AgentID:       firstNonEmpty(run.Role, item.GraphState.ActiveRole, "agent"),
		AgentLabel:    scientistBenchAgentLabel(firstNonEmpty(run.Role, item.GraphState.ActiveRole, "agent")),
		State:         state,
		Title:         title,
		Detail:        detail,
		ToolName:      strings.TrimSpace(event.ToolName),
		CaseID:        item.ID,
		NodeID:        firstNonEmpty(run.NodeID, item.GraphState.ActiveNode),
		RunID:         run.ID,
		TaskSessionID: firstNonEmpty(run.TaskRunSessionID, run.SessionID),
	}
}

func scientistBenchEventLabel(event taskrun.Event) string {
	switch event.Kind {
	case taskrun.EventQueued:
		return "queued"
	case taskrun.EventStarted:
		return "started"
	case taskrun.EventProgress:
		return "streaming"
	case taskrun.EventFinished:
		switch event.Status {
		case taskrun.StatusComplete:
			return "complete"
		case taskrun.StatusBlocked:
			return "blocked"
		case taskrun.StatusCanceled:
			return "canceled"
		case taskrun.StatusFailed:
			return "failed"
		default:
			return "finished"
		}
	case taskrun.EventCancelRequested:
		return "canceled"
	default:
		return ""
	}
}

func scientistBenchEventTitle(event taskrun.Event) string {
	switch event.Kind {
	case taskrun.EventQueued:
		return "Queued"
	case taskrun.EventStarted:
		return "Starting"
	case taskrun.EventProgress:
		if toolName := strings.TrimSpace(event.ToolName); toolName != "" {
			return "Running " + toolName
		}
		return "Working"
	case taskrun.EventFinished:
		switch event.Status {
		case taskrun.StatusComplete:
			return "Completed"
		case taskrun.StatusBlocked:
			return "Waiting for permission"
		case taskrun.StatusCanceled:
			return "Canceled"
		case taskrun.StatusFailed:
			return "Failed"
		default:
			return "Finished"
		}
	case taskrun.EventCancelRequested:
		return "Cancel requested"
	default:
		return "Working"
	}
}

func scientistBenchEventDetail(event taskrun.Event) string {
	parts := make([]string, 0, 3)
	if detail := strings.TrimSpace(event.Detail); detail != "" {
		parts = append(parts, detail)
	}
	if reason := strings.TrimSpace(event.Metadata.PermissionReason); reason != "" {
		parts = append(parts, reason)
	}
	if preview := strings.TrimSpace(event.Metadata.ToolInputPreview); preview != "" && strings.TrimSpace(event.ToolName) != "" {
		parts = append(parts, preview)
	}
	return strings.Join(parts, " • ")
}

func scientistBenchNodeResultContent(item scientistbench.Case, run scientistbench.RunRecord) message.ScientistBenchContent {
	state := strings.TrimSpace(run.Status)
	if state == "" {
		state = string(item.Status)
	}
	if state == "" {
		state = "complete"
	}

	detailParts := make([]string, 0, 4)
	if detail := strings.TrimSpace(firstNonEmpty(run.OutputSummary, run.Error)); detail != "" {
		detailParts = append(detailParts, detail)
	}
	if len(run.SignalsEmitted) > 0 {
		detailParts = append(detailParts, "signal "+strings.Join(run.SignalsEmitted, ", "))
	}
	if scientistBenchCaseIsTerminal(item) {
		if item.Termination.Signal != "" {
			detailParts = append(detailParts, "termination "+string(item.Termination.Signal))
		}
	}

	return message.ScientistBenchContent{
		Kind:          scientistBenchMessageKindStatus,
		AgentID:       firstNonEmpty(run.Role, item.GraphState.ActiveRole, "agent"),
		AgentLabel:    scientistBenchAgentLabel(firstNonEmpty(run.Role, item.GraphState.ActiveRole, "agent")),
		State:         state,
		Title:         scientistBenchResultTitle(state),
		Detail:        strings.Join(detailParts, " • "),
		CaseID:        item.ID,
		NodeID:        firstNonEmpty(run.NodeID, item.GraphState.ActiveNode),
		RunID:         run.ID,
		TaskSessionID: firstNonEmpty(run.TaskRunSessionID, run.SessionID),
	}
}

func scientistBenchResultTitle(state string) string {
	switch strings.TrimSpace(state) {
	case "blocked":
		return "Waiting for permission"
	case "canceled":
		return "Canceled"
	case "failed", string(scientistbench.StatusNotResolved):
		return "Failed"
	case string(scientistbench.StatusResolved):
		return "Resolved"
	default:
		return "Completed"
	}
}

// startScientistBenchMessageSync subscribes to child session assistant messages
// and mirrors them into the root ScientistBench session while the agent is still
// streaming, so the user sees an actual conversation instead of post-hoc logs.
func (app *App) startScientistBenchMessageSync(ctx context.Context) {
	if app.ScientistBench == nil || app.Messages == nil {
		return
	}

	watchCtx, cancel := context.WithCancel(ctx)
	app.cancelFuncsMutex.Lock()
	app.watcherCancelFuncs = append(app.watcherCancelFuncs, cancel)
	app.cancelFuncsMutex.Unlock()

	app.watcherWG.Add(1)
	go func() {
		defer app.watcherWG.Done()
		defer logging.RecoverPanic("scientistbench-message-sync", nil)

		sub := app.Messages.Subscribe(watchCtx)
		for {
			select {
			case <-watchCtx.Done():
				return
			case event, ok := <-sub:
				if !ok {
					return
				}
				msg := event.Payload
				if !looksLikeScientistBenchTask(msg.SessionID) {
					continue
				}
				if msg.Role != message.Assistant {
					continue
				}
				text := strings.TrimSpace(msg.Content().Text)
				if text == "" || isScientistBenchStructuredOutput(text) {
					continue
				}
				item, run, ok := app.findScientistBenchRunByTaskSession(watchCtx, msg.SessionID)
				if !ok {
					continue
				}
				if err := app.upsertScientistBenchAgentMessage(watchCtx, item, run, msg); err != nil {
					logging.Warn("Failed to mirror scientist bench agent message", "case_id", item.ID, "session_id", msg.SessionID, "error", err)
				}
			}
		}
	}()
}

// isScientistBenchStructuredOutput detects the final JSON WorkerOutput blob that
// agents emit at the end of their run. We skip mirroring this because it is
// already captured and summarized by watchScientistBenchNodeRun.
func isScientistBenchStructuredOutput(text string) bool {
	return strings.HasPrefix(text, "{") && strings.Contains(text, `"status":`)
}

func scientistBenchAgentState(msg message.Message) string {
	if !msg.IsFinished() {
		return "streaming"
	}
	switch msg.FinishReason() {
	case message.FinishReasonCanceled:
		return "canceled"
	case message.FinishReasonError:
		return "failed"
	case message.FinishReasonPermissionDenied:
		return "blocked"
	default:
		return "complete"
	}
}

func scientistBenchAgentTitle(msg message.Message) string {
	if !msg.IsFinished() {
		return "Streaming"
	}
	switch msg.FinishReason() {
	case message.FinishReasonCanceled:
		return "Canceled"
	case message.FinishReasonError:
		return "Failed"
	case message.FinishReasonPermissionDenied:
		return "Waiting for permission"
	default:
		return "Message"
	}
}

func scientistBenchAgentFinishReason(msg message.Message) message.FinishReason {
	if !msg.IsFinished() {
		return ""
	}
	if msg.FinishReason() == message.FinishReasonToolUse {
		return message.FinishReasonEndTurn
	}
	return msg.FinishReason()
}

func scientistBenchStateFinishReason(state string) message.FinishReason {
	switch strings.TrimSpace(state) {
	case "complete", "resolved", "failed", "blocked", "canceled":
		return message.FinishReasonEndTurn
	default:
		return ""
	}
}

func scientistBenchAgentMirrorKey(sourceMessageID string) string {
	return "agent:" + strings.TrimSpace(sourceMessageID)
}

func scientistBenchStatusMirrorKey(runID string) string {
	return "status:" + strings.TrimSpace(runID)
}

func (app *App) storeScientistBenchMirror(mirrorKey, kind, messageID string) {
	app.scientistBenchMirrorMu.Lock()
	defer app.scientistBenchMirrorMu.Unlock()
	switch kind {
	case scientistBenchMessageKindAgent:
		if app.scientistBenchAgentMirrors == nil {
			app.scientistBenchAgentMirrors = make(map[string]string)
		}
		app.scientistBenchAgentMirrors[mirrorKey] = messageID
	default:
		if app.scientistBenchStatusMirrors == nil {
			app.scientistBenchStatusMirrors = make(map[string]string)
		}
		app.scientistBenchStatusMirrors[mirrorKey] = messageID
	}
}

func (app *App) lookupScientistBenchMirror(mirrorKey, kind string) (string, bool) {
	app.scientistBenchMirrorMu.Lock()
	defer app.scientistBenchMirrorMu.Unlock()
	switch kind {
	case scientistBenchMessageKindAgent:
		msgID, ok := app.scientistBenchAgentMirrors[mirrorKey]
		return msgID, ok
	default:
		msgID, ok := app.scientistBenchStatusMirrors[mirrorKey]
		return msgID, ok
	}
}

func (app *App) deleteScientistBenchMirror(mirrorKey, kind string) {
	app.scientistBenchMirrorMu.Lock()
	defer app.scientistBenchMirrorMu.Unlock()
	switch kind {
	case scientistBenchMessageKindAgent:
		delete(app.scientistBenchAgentMirrors, mirrorKey)
	default:
		delete(app.scientistBenchStatusMirrors, mirrorKey)
	}
}

func scientistBenchAgentLabel(role string) string {
	role = strings.TrimSpace(role)
	if role == "" {
		return "Agent"
	}
	role = strings.ReplaceAll(role, "-", " ")
	role = strings.ReplaceAll(role, "_", " ")
	parts := strings.Fields(role)
	for i := range parts {
		part := strings.ToLower(parts[i])
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func (app *App) recordScientistBenchToolCall(ctx context.Context, caseID string, runID string, toolName string, taskStatus string) error {
	if app.ScientistBench == nil {
		return nil
	}
	toolName = strings.TrimSpace(toolName)
	taskStatus = strings.TrimSpace(taskStatus)
	if strings.TrimSpace(caseID) == "" || strings.TrimSpace(runID) == "" || toolName == "" {
		return nil
	}
	_, err := app.ScientistBench.MutateCase(ctx, caseID, func(item *scientistbench.Case) error {
		run, ok := findScientistBenchRun(*item, runID)
		if !ok {
			return nil
		}
		run.ToolCalls = appendUniqueString(run.ToolCalls, toolName)
		if taskStatus != "" {
			run.TaskRunStatus = taskStatus
		}
		item.Runs = upsertScientistBenchRunLocal(item.Runs, run)
		return nil
	})
	return err
}

func appendUniqueString(items []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return items
	}
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}

func truncateWithEllipsis(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
