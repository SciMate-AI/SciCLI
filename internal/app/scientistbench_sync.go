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
				line := scientistBenchTaskEventLine(item, run, payload)
				if strings.TrimSpace(line) == "" {
					continue
				}
				if err := app.postScientistBenchText(watchCtx, item.RootSessionID, line); err != nil {
					logging.Warn("Failed to mirror scientist bench task progress", "case_id", item.ID, "session_id", payload.SessionID, "error", err)
				}
			}
		}
	}()
}

func (app *App) postScientistBenchLaunch(ctx context.Context, item scientistbench.Case, run scientistbench.RunRecord, prefix string) error {
	return app.postScientistBenchText(ctx, item.RootSessionID, scientistBenchLaunchLine(item, run, prefix))
}

func (app *App) postScientistBenchNodeResult(ctx context.Context, item scientistbench.Case, run scientistbench.RunRecord) error {
	return app.postScientistBenchText(ctx, item.RootSessionID, scientistBenchNodeResultLine(item, run))
}

func (app *App) postScientistBenchText(ctx context.Context, sessionID string, text string) error {
	if app.Messages == nil {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	text = strings.TrimSpace(text)
	if sessionID == "" || text == "" {
		return nil
	}
	_, err := app.Messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: text},
			message.Finish{
				Reason: message.FinishReasonEndTurn,
				Time:   time.Now().Unix(),
			},
		},
	})
	return err
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

func scientistBenchLaunchLine(item scientistbench.Case, run scientistbench.RunRecord, prefix string) string {
	parts := []string{
		firstNonEmpty(strings.TrimSpace(prefix), "Scientist Bench started"),
		"case " + item.ID,
		"mode " + string(item.Mode),
		"node " + firstNonEmpty(run.NodeID, item.GraphState.ActiveNode),
		"role " + firstNonEmpty(run.Role, item.GraphState.ActiveRole),
	}
	if strings.TrimSpace(run.SessionID) != "" {
		parts = append(parts, "task "+run.SessionID)
	}
	if strings.TrimSpace(item.Title) != "" {
		parts = append(parts, item.Title)
	}
	parts = append(parts, "Progress will stream in this chat.")
	return strings.Join(parts, " | ")
}

func scientistBenchTaskEventLine(item scientistbench.Case, run scientistbench.RunRecord, event taskrun.Event) string {
	stage := scientistBenchEventLabel(event)
	if stage == "" {
		return ""
	}

	parts := []string{
		"Scientist Bench progress",
		"case " + item.ID,
		"node " + firstNonEmpty(run.NodeID, item.GraphState.ActiveNode),
		"role " + firstNonEmpty(run.Role, item.GraphState.ActiveRole),
		stage,
	}
	if detail := strings.TrimSpace(event.Detail); detail != "" {
		parts = append(parts, detail)
	}
	if toolName := strings.TrimSpace(event.ToolName); toolName != "" {
		parts = append(parts, "tool "+toolName)
	}
	if reason := strings.TrimSpace(event.Metadata.PermissionReason); reason != "" {
		parts = append(parts, reason)
	}
	return strings.Join(parts, " | ")
}

func scientistBenchEventLabel(event taskrun.Event) string {
	switch event.Kind {
	case taskrun.EventQueued:
		return "queued"
	case taskrun.EventStarted:
		return "started"
	case taskrun.EventProgress:
		return "progress"
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
		return "cancel requested"
	default:
		return ""
	}
}

func scientistBenchNodeResultLine(item scientistbench.Case, run scientistbench.RunRecord) string {
	status := firstNonEmpty(run.Status, string(item.Status), "complete")
	detail := firstNonEmpty(run.OutputSummary, run.Error, "Completed")

	parts := []string{
		"Scientist Bench update",
		"case " + item.ID,
		"node " + firstNonEmpty(run.NodeID, item.GraphState.ActiveNode),
		"role " + firstNonEmpty(run.Role, item.GraphState.ActiveRole),
		status,
		detail,
	}
	if len(run.SignalsEmitted) > 0 {
		parts = append(parts, "signal "+strings.Join(run.SignalsEmitted, ","))
	}
	switch {
	case scientistBenchCaseIsTerminal(item):
		if item.Termination.Signal != "" {
			parts = append(parts, "termination "+string(item.Termination.Signal))
		}
	case item.GraphState.ActiveNode != "" && item.GraphState.ActiveNode != run.NodeID:
		parts = append(parts, "next node "+item.GraphState.ActiveNode)
		if item.GraphState.ActiveRole != "" {
			parts = append(parts, "next role "+item.GraphState.ActiveRole)
		}
	case item.GraphState.ActiveRole != "" && item.GraphState.ActiveRole != run.Role:
		parts = append(parts, "next role "+item.GraphState.ActiveRole)
	}
	return strings.Join(parts, " | ")
}
