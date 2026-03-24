package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/SciMate-AI/scicli/internal/diff"
	"github.com/SciMate-AI/scicli/internal/history"
	"github.com/SciMate-AI/scicli/internal/logging"
	"github.com/SciMate-AI/scicli/internal/message"
	"github.com/SciMate-AI/scicli/internal/pubsub"
	"github.com/SciMate-AI/scicli/internal/research"
	"github.com/SciMate-AI/scicli/internal/taskrun"
)

func (app *App) startResearchRunSync(ctx context.Context) {
	if app.Research == nil || app.TaskRuns == nil {
		return
	}

	watchCtx, cancel := context.WithCancel(ctx)
	app.cancelFuncsMutex.Lock()
	app.watcherCancelFuncs = append(app.watcherCancelFuncs, cancel)
	app.cancelFuncsMutex.Unlock()

	app.watcherWG.Add(1)
	go func() {
		defer app.watcherWG.Done()
		defer logging.RecoverPanic("research-run-sync", nil)

		sub := app.TaskRuns.Subscribe(watchCtx)
		for {
			select {
			case <-watchCtx.Done():
				return
			case event, ok := <-sub:
				if !ok {
					return
				}
				if event.Type != pubsub.CreatedEvent && event.Type != pubsub.UpdatedEvent {
					continue
				}
				run := event.Payload
				if run.ParentSessionID == "" {
					continue
				}
				if err := app.Research.SyncTaskRun(watchCtx, run); err != nil {
					logging.Warn("Failed to sync task run into research state", "session_id", run.SessionID, "error", err)
				}
				if isFinishedTaskRun(run.Status) {
					artifacts := app.collectTaskArtifacts(watchCtx, run)
					if len(artifacts) > 0 {
						if err := app.Research.SyncTaskArtifacts(watchCtx, run.SessionID, artifacts); err != nil {
							logging.Warn("Failed to sync task artifacts into research state", "session_id", run.SessionID, "error", err)
						}
					}
				}
			}
		}
	}()
}

func isFinishedTaskRun(status taskrun.Status) bool {
	switch status {
	case taskrun.StatusComplete, taskrun.StatusFailed, taskrun.StatusCanceled, taskrun.StatusBlocked:
		return true
	default:
		return false
	}
}

func (app *App) collectTaskArtifacts(ctx context.Context, run taskrun.Run) []research.ArtifactRef {
	artifacts := make([]research.ArtifactRef, 0, 8)

	if strings.TrimSpace(run.Prompt) != "" {
		artifacts = append(artifacts, research.ArtifactRef{
			ID:              "prompt:" + run.SessionID,
			Kind:            research.ArtifactPrompt,
			Label:           "Prompt",
			Summary:         truncateArtifactText(run.Prompt, 240),
			SourceSessionID: run.SessionID,
			CreatedAt:       run.UpdatedAt,
		})
	}

	if response := app.latestAssistantArtifact(ctx, run.SessionID); response != nil {
		artifacts = append(artifacts, *response)
	}

	if timeline := app.timelineArtifact(run); timeline != nil {
		artifacts = append(artifacts, *timeline)
	}

	artifacts = append(artifacts, app.modifiedFileArtifacts(ctx, run.SessionID)...)
	return artifacts
}

func (app *App) latestAssistantArtifact(ctx context.Context, sessionID string) *research.ArtifactRef {
	if app.Messages == nil {
		return nil
	}
	msgs, err := app.Messages.List(ctx, sessionID)
	if err != nil {
		return nil
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		msg := msgs[i]
		if msg.Role != message.Assistant {
			continue
		}
		text := strings.TrimSpace(msg.Content().Text)
		if text == "" {
			continue
		}
		return &research.ArtifactRef{
			ID:              "report:" + sessionID,
			Kind:            research.ArtifactReport,
			Label:           "Final Response",
			Summary:         truncateArtifactText(text, 240),
			SourceSessionID: sessionID,
			CreatedAt:       msg.UpdatedAt,
		}
	}
	return nil
}

func (app *App) timelineArtifact(run taskrun.Run) *research.ArtifactRef {
	if app.TaskRuns == nil {
		return nil
	}
	events := app.TaskRuns.Timeline(run.SessionID, 8)
	if len(events) == 0 {
		return nil
	}
	lines := make([]string, 0, min(len(events), 4))
	start := max(0, len(events)-4)
	for _, event := range events[start:] {
		line := strings.TrimSpace(event.Detail)
		if line == "" {
			line = string(event.Kind)
		}
		lines = append(lines, line)
	}
	return &research.ArtifactRef{
		ID:              "timeline:" + run.SessionID,
		Kind:            research.ArtifactRunLog,
		Label:           "Task Timeline",
		Summary:         truncateArtifactText(strings.Join(lines, " | "), 240),
		SourceSessionID: run.SessionID,
		CreatedAt:       run.UpdatedAt,
	}
}

func (app *App) modifiedFileArtifacts(ctx context.Context, sessionID string) []research.ArtifactRef {
	if app.History == nil {
		return nil
	}

	latestFiles, err := app.History.ListLatestSessionFiles(ctx, sessionID)
	if err != nil {
		return nil
	}
	allFiles, err := app.History.ListBySession(ctx, sessionID)
	if err != nil {
		return nil
	}

	artifacts := make([]research.ArtifactRef, 0)
	for _, file := range latestFiles {
		if file.Version == history.InitialVersion {
			continue
		}

		var initialVersion history.File
		for _, candidate := range allFiles {
			if candidate.Path == file.Path && candidate.Version == history.InitialVersion {
				initialVersion = candidate
				break
			}
		}
		if initialVersion.ID == "" || initialVersion.Content == file.Content {
			continue
		}

		_, additions, removals := diff.GenerateDiff(initialVersion.Content, file.Content, file.Path)
		if additions == 0 && removals == 0 {
			continue
		}

		displayPath := strings.TrimPrefix(file.Path, config.WorkingDirectory())
		displayPath = strings.TrimPrefix(displayPath, "/")
		artifacts = append(artifacts, research.ArtifactRef{
			ID:              "file:" + sessionID + ":" + displayPath,
			Kind:            research.ArtifactCodeSnapshot,
			Label:           "Modified File",
			Path:            displayPath,
			Summary:         fmt.Sprintf("%s (+%d -%d)", displayPath, additions, removals),
			SourceSessionID: sessionID,
			Metadata: map[string]string{
				"additions": fmt.Sprintf("%d", additions),
				"removals":  fmt.Sprintf("%d", removals),
				"version":   file.Version,
			},
			CreatedAt: file.UpdatedAt,
		})
	}
	return artifacts
}

func truncateArtifactText(text string, maxLen int) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	if maxLen <= 0 || len(text) <= maxLen {
		return text
	}
	if maxLen <= 3 {
		return text[:maxLen]
	}
	return text[:maxLen-3] + "..."
}
