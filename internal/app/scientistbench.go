package app

import (
	"context"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/SciMate-AI/scicli/internal/llm/agent"
	"github.com/SciMate-AI/scicli/internal/orchestrator"
	runtimex "github.com/SciMate-AI/scicli/internal/runtime"
	"github.com/SciMate-AI/scicli/internal/scientistbench"
	"github.com/SciMate-AI/scicli/internal/taskrun"
	"github.com/google/uuid"
)

type ScientistBenchNodeRun struct {
	Case scientistbench.Case      `json:"case"`
	Run  scientistbench.RunRecord `json:"run"`
}

const maxScientistBenchSummaryLen = 280

const scientistBenchCheckpointGracePeriod = 90 * time.Second

func (app *App) CreateScientistBenchCase(ctx context.Context, input scientistbench.CreateCaseInput) (scientistbench.Case, error) {
	if app.ScientistBench == nil {
		return scientistbench.Case{}, fmt.Errorf("scientist bench service is not configured")
	}

	item, err := app.ScientistBench.CreateCase(ctx, input)
	if err != nil {
		return scientistbench.Case{}, err
	}

	item, err = app.ensureScientistBenchRootSession(ctx, item)
	if err != nil {
		return scientistbench.Case{}, err
	}
	if app.Orchestrator == nil {
		return app.ScientistBench.Save(ctx, item)
	}

	item, err = app.Orchestrator.BootstrapCase(item)
	if err != nil {
		return scientistbench.Case{}, err
	}
	return app.ScientistBench.Save(ctx, item)
}

func (app *App) AdvanceScientistBenchCase(ctx context.Context, caseID string, signal string) (scientistbench.Case, error) {
	if app.ScientistBench == nil {
		return scientistbench.Case{}, fmt.Errorf("scientist bench service is not configured")
	}
	if app.Orchestrator == nil {
		return scientistbench.Case{}, fmt.Errorf("orchestrator service is not configured")
	}
	caseID = strings.TrimSpace(caseID)
	if caseID == "" {
		return scientistbench.Case{}, fmt.Errorf("case ID is required")
	}

	item, err := app.ScientistBench.Get(ctx, caseID)
	if err != nil {
		return scientistbench.Case{}, err
	}
	item, err = app.Orchestrator.ApplySignal(item, signal)
	if err != nil {
		return scientistbench.Case{}, err
	}
	return app.ScientistBench.Save(ctx, item)
}

func (app *App) StartScientistBenchActiveNodeRun(ctx context.Context, caseID string) (ScientistBenchNodeRun, error) {
	if app.ScientistBench == nil {
		return ScientistBenchNodeRun{}, fmt.Errorf("scientist bench service is not configured")
	}
	if app.Orchestrator == nil {
		return ScientistBenchNodeRun{}, fmt.Errorf("orchestrator service is not configured")
	}
	if app.Sessions == nil {
		return ScientistBenchNodeRun{}, fmt.Errorf("session service is not configured")
	}
	if app.Messages == nil {
		return ScientistBenchNodeRun{}, fmt.Errorf("message service is not configured")
	}

	item, err := app.ScientistBench.Get(ctx, strings.TrimSpace(caseID))
	if err != nil {
		return ScientistBenchNodeRun{}, err
	}
	item, err = app.ensureScientistBenchRootSession(ctx, item)
	if err != nil {
		return ScientistBenchNodeRun{}, err
	}
	item, err = app.Orchestrator.BootstrapCase(item)
	if err != nil {
		return ScientistBenchNodeRun{}, err
	}
	item, _, err = app.reconcileScientistBenchGraphFromRuns(item)
	if err != nil {
		return ScientistBenchNodeRun{}, err
	}
	item, err = app.ScientistBench.Save(ctx, item)
	if err != nil {
		return ScientistBenchNodeRun{}, err
	}
	if scientistBenchCaseIsTerminal(item) {
		return ScientistBenchNodeRun{}, fmt.Errorf("case %s is terminal with status %s", item.ID, item.Status)
	}

	node, ok := app.Orchestrator.GetNode(item.GraphState.ActiveNode)
	if !ok {
		return ScientistBenchNodeRun{}, fmt.Errorf("active node %s is not registered", item.GraphState.ActiveNode)
	}
	roleID, err := nextPendingRoleForNode(item, node)
	if err != nil {
		return ScientistBenchNodeRun{}, err
	}
	profile, ok := app.Orchestrator.WorkerProfileForRole(roleID)
	if !ok {
		return ScientistBenchNodeRun{}, fmt.Errorf("worker profile for role %s is not registered", roleID)
	}
	item.GraphState.ActiveRole = roleID
	item, err = app.ScientistBench.Save(ctx, item)
	if err != nil {
		return ScientistBenchNodeRun{}, err
	}
	item, liveRun := app.reconcileScientistBenchCheckpoint(item, node.ID, roleID, time.Now())
	item, err = app.ScientistBench.Save(ctx, item)
	if err != nil {
		return ScientistBenchNodeRun{}, err
	}
	if liveRun != nil {
		return ScientistBenchNodeRun{
			Case: item,
			Run:  *liveRun,
		}, nil
	}

	taskSessionID := "sbtask-" + uuid.NewString()
	taskTitle := strings.TrimSpace(profile.SessionLabel)
	if taskTitle == "" {
		taskTitle = "Scientist Bench Worker"
	}
	taskTitle = fmt.Sprintf("%s: %s", taskTitle, node.ID)

	taskSession, err := app.Sessions.CreateTaskSession(ctx, taskSessionID, item.RootSessionID, taskTitle)
	if err != nil {
		return ScientistBenchNodeRun{}, fmt.Errorf("create task session: %w", err)
	}
	if app.shouldAutoApproveScientistBenchRoot(item.RootSessionID) {
		app.Permissions.AutoApproveSession(taskSession.ID)
	}

	prompt := buildScientistBenchWorkerPrompt(item, node, profile, app.RuntimeRegistry, app.RuntimeExecutor)
	if app.TaskRuns != nil {
		app.TaskRuns.Queue(taskSession, prompt)
	}

	run := scientistbench.RunRecord{
		ID:               "run-" + uuid.NewString(),
		SessionID:        taskSession.ID,
		Role:             roleID,
		RoleInstance:     fmt.Sprintf("%s#%d", roleID, nextRoleOrdinal(item, roleID)),
		NodeID:           node.ID,
		Status:           "queued",
		StartedAt:        time.Now().Unix(),
		InputSummary:     truncateScientistBenchText(prompt, maxScientistBenchSummaryLen),
		TaskRunSessionID: taskSession.ID,
		TaskRunStatus:    "queued",
	}

	item = app.captureRuntimePlansForCase(item, node.ID, roleID, run.ID)

	item, err = app.ScientistBench.UpsertRun(ctx, item.ID, run)
	if err != nil {
		return ScientistBenchNodeRun{}, err
	}

	worker, err := agent.NewAgent(
		config.AgentTask,
		app.Sessions,
		app.Messages,
		agent.RoleWorkerTools(profile.ToolProfile, app.Permissions, app.History, app.LSPClients, app.Skills),
		app.Skills,
		app.TaskRuns,
	)
	if err != nil {
		return ScientistBenchNodeRun{}, fmt.Errorf("create role worker: %w", err)
	}

	done, err := worker.Run(context.Background(), taskSession.ID, prompt)
	if err != nil {
		run.Status = "failed"
		run.TaskRunStatus = "failed"
		run.Error = err.Error()
		run.FinishedAt = time.Now().Unix()
		_, _ = app.ScientistBench.UpsertRun(context.Background(), item.ID, run)
		return ScientistBenchNodeRun{}, fmt.Errorf("start role worker: %w", err)
	}
	if done == nil {
		run.Status = "failed"
		run.TaskRunStatus = "failed"
		run.Error = "role worker returned nil event channel"
		run.FinishedAt = time.Now().Unix()
		_, _ = app.ScientistBench.UpsertRun(context.Background(), item.ID, run)
		return ScientistBenchNodeRun{}, fmt.Errorf("start role worker: nil event channel")
	}

	go app.watchScientistBenchNodeRun(item.ID, run, node, done)

	item, err = app.ScientistBench.Get(ctx, item.ID)
	if err != nil {
		return ScientistBenchNodeRun{}, err
	}
	updatedRun, ok := findScientistBenchRun(item, run.ID)
	if !ok {
		return ScientistBenchNodeRun{}, fmt.Errorf("run %s not found after scheduling", run.ID)
	}
	return ScientistBenchNodeRun{
		Case: item,
		Run:  updatedRun,
	}, nil
}

func (app *App) ensureScientistBenchRootSession(ctx context.Context, item scientistbench.Case) (scientistbench.Case, error) {
	if strings.TrimSpace(item.RootSessionID) != "" {
		return item, nil
	}
	if app.Sessions == nil {
		return scientistbench.Case{}, fmt.Errorf("session service is not configured")
	}

	title := strings.TrimSpace(item.Title)
	if title == "" {
		title = item.ID
	}
	rootSession, err := app.Sessions.Create(ctx, "Scientist Bench: "+title)
	if err != nil {
		return scientistbench.Case{}, fmt.Errorf("create root session: %w", err)
	}
	item.RootSessionID = rootSession.ID
	return item, nil
}

func (app *App) scheduleScientistBenchContinuation(caseID string) {
	caseID = strings.TrimSpace(caseID)
	if caseID == "" || app.ScientistBench == nil || app.Orchestrator == nil {
		return
	}
	if !app.tryBeginScientistBenchContinuation(caseID) {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	app.cancelFuncsMutex.Lock()
	app.watcherCancelFuncs = append(app.watcherCancelFuncs, cancel)
	app.cancelFuncsMutex.Unlock()

	app.watcherWG.Add(1)
	go func() {
		defer app.watcherWG.Done()
		defer cancel()
		defer app.finishScientistBenchContinuation(caseID)
		_, _ = app.startScientistBenchContinuation(ctx, caseID)
	}()
}

func (app *App) tryBeginScientistBenchContinuation(caseID string) bool {
	app.scientistBenchContinuationMu.Lock()
	defer app.scientistBenchContinuationMu.Unlock()
	if app.scientistBenchContinuation == nil {
		app.scientistBenchContinuation = make(map[string]struct{})
	}
	if _, exists := app.scientistBenchContinuation[caseID]; exists {
		return false
	}
	app.scientistBenchContinuation[caseID] = struct{}{}
	return true
}

func (app *App) finishScientistBenchContinuation(caseID string) {
	app.scientistBenchContinuationMu.Lock()
	defer app.scientistBenchContinuationMu.Unlock()
	delete(app.scientistBenchContinuation, strings.TrimSpace(caseID))
}

func (app *App) startScientistBenchContinuation(ctx context.Context, caseID string) (ScientistBenchNodeRun, error) {
	if app.scientistBenchStarter != nil {
		return app.scientistBenchStarter(ctx, caseID)
	}
	return app.StartScientistBenchActiveNodeRun(ctx, caseID)
}

func (app *App) shouldAutoApproveScientistBenchRoot(rootSessionID string) bool {
	if app.Permissions == nil {
		return false
	}
	if cfg := config.Get(); cfg != nil && cfg.Automation.WorkMode == config.WorkModeUltrawork {
		return true
	}
	return app.Permissions.IsAutoApproved(rootSessionID)
}

func (app *App) watchScientistBenchNodeRun(
	caseID string,
	run scientistbench.RunRecord,
	node orchestrator.NodeSpec,
	done <-chan agent.AgentEvent,
) {
	defer func() {
		recovered := recover()
		if recovered == nil || app.ScientistBench == nil {
			return
		}
		run.Status = "failed"
		run.TaskRunStatus = "failed"
		run.Error = fmt.Sprintf("scientist bench watcher panic: %v", recovered)
		run.FinishedAt = time.Now().Unix()
		_, _ = app.ScientistBench.UpsertRun(context.Background(), caseID, run)
	}()

	result, ok := <-done
	if !ok {
		result = agent.AgentEvent{
			Error: fmt.Errorf("role worker event channel closed without result"),
		}
	}
	content := scientistBenchEventContent(result)
	parsed := orchestrator.ParseWorkerOutput(content)
	if result.Error == nil {
		parsed = app.reconcileScientistBenchRuntimeExecution(context.Background(), run, node, parsed)
	}

	run.Status = "complete"
	run.TaskRunStatus = "complete"
	run.FinishedAt = time.Now().Unix()
	if result.Error != nil {
		run.Status = "failed"
		run.TaskRunStatus = "failed"
		run.Error = result.Error.Error()
	} else {
		run.OutputSummary = truncateScientistBenchText(parsed.Summary, maxScientistBenchSummaryLen)
		if strings.TrimSpace(run.OutputSummary) == "" {
			run.OutputSummary = "Completed"
		}
		run.SignalsEmitted = normalizeRunSignals(parsed, node)
	}

	ctx := context.Background()
	item, err := app.ScientistBench.UpsertRun(ctx, caseID, run)
	if err != nil {
		return
	}

	if result.Error == nil && strings.TrimSpace(content) != "" {
		artifact := scientistbench.Artifact{
			ID:            "artifact-" + uuid.NewString(),
			Kind:          artifactKindForNode(node),
			Label:         artifactLabelForNode(node),
			ProducerRole:  run.Role,
			ProducerRunID: run.ID,
			CreatedAt:     time.Now().Unix(),
			Metadata: map[string]string{
				"node_id":    node.ID,
				"session_id": run.SessionID,
				"summary":    run.OutputSummary,
				"status":     parsed.Status,
			},
		}
		if len(parsed.Citations) > 0 {
			artifact.Metadata["citations"] = strings.Join(parsed.Citations, " | ")
		}
		if len(parsed.EvidenceSummary) > 0 {
			artifact.Metadata["evidence"] = strings.Join(parsed.EvidenceSummary, " | ")
		}
		if len(parsed.Risks) > 0 {
			artifact.Metadata["risks"] = strings.Join(parsed.Risks, " | ")
		}
		if parsed.Review != nil {
			if decision := strings.TrimSpace(parsed.Review.Decision); decision != "" {
				artifact.Metadata["review_decision"] = decision
			}
			if summary := strings.TrimSpace(parsed.Review.Summary); summary != "" {
				artifact.Metadata["review_summary"] = summary
			}
			if len(parsed.Review.Strengths) > 0 {
				artifact.Metadata["review_strengths"] = strings.Join(parsed.Review.Strengths, " | ")
			}
			if len(parsed.Review.Weaknesses) > 0 {
				artifact.Metadata["review_weaknesses"] = strings.Join(parsed.Review.Weaknesses, " | ")
			}
			if len(parsed.Review.Questions) > 0 {
				artifact.Metadata["review_questions"] = strings.Join(parsed.Review.Questions, " | ")
			}
			if parsed.Review.Confidence > 0 {
				artifact.Metadata["review_confidence"] = fmt.Sprintf("%.2f", parsed.Review.Confidence)
			}
			if parsed.Review.Scores.Overall > 0 {
				artifact.Metadata["review_overall"] = fmt.Sprintf("%.2f", parsed.Review.Scores.Overall)
			}
		}
		if parsed.Comparison != nil {
			if summary := strings.TrimSpace(parsed.Comparison.Summary); summary != "" {
				artifact.Metadata["comparison_summary"] = summary
			}
			if parsed.Comparison.MotivationAlignment > 0 {
				artifact.Metadata["motivation_alignment"] = fmt.Sprintf("%.2f", parsed.Comparison.MotivationAlignment)
			}
			if parsed.Comparison.MethodologyAlignment > 0 {
				artifact.Metadata["methodology_alignment"] = fmt.Sprintf("%.2f", parsed.Comparison.MethodologyAlignment)
			}
			if parsed.Comparison.NoveltyAlignment > 0 {
				artifact.Metadata["novelty_alignment"] = fmt.Sprintf("%.2f", parsed.Comparison.NoveltyAlignment)
			}
			if parsed.Comparison.ExperimentalAlignment > 0 {
				artifact.Metadata["experimental_alignment"] = fmt.Sprintf("%.2f", parsed.Comparison.ExperimentalAlignment)
			}
		}
		if parsed.MethodPlan != nil {
			if parsed.MethodPlan.Summary != "" {
				artifact.Metadata["method_summary"] = parsed.MethodPlan.Summary
			}
			if len(parsed.MethodPlan.PipelineSteps) > 0 {
				artifact.Metadata["method_pipeline"] = strings.Join(parsed.MethodPlan.PipelineSteps, " | ")
			}
			if len(parsed.MethodPlan.AcceptanceChecks) > 0 {
				artifact.Metadata["acceptance_checks"] = strings.Join(parsed.MethodPlan.AcceptanceChecks, " | ")
			}
			if len(parsed.MethodPlan.ImplementationNotes) > 0 {
				artifact.Metadata["implementation_notes"] = strings.Join(parsed.MethodPlan.ImplementationNotes, " | ")
			}
			if len(parsed.MethodPlan.RuntimeHints) > 0 {
				artifact.Metadata["runtime_hints"] = strings.Join(parsed.MethodPlan.RuntimeHints, " | ")
			}
		}
		item, _ = app.ScientistBench.UpsertArtifact(ctx, caseID, artifact)
		if parsed.Execution != nil {
			if len(parsed.Execution.LogHighlights) > 0 || strings.TrimSpace(parsed.Execution.VerificationSummary) != "" {
				runtimeArtifact := scientistbench.Artifact{
					ID:            "artifact-" + uuid.NewString(),
					Kind:          scientistbench.ArtifactDockerLog,
					Label:         "Runtime Log",
					ProducerRole:  run.Role,
					ProducerRunID: run.ID,
					CreatedAt:     time.Now().Unix(),
					Metadata: map[string]string{
						"runtime_id":           strings.TrimSpace(parsed.Execution.RuntimeID),
						"verification_summary": strings.TrimSpace(parsed.Execution.VerificationSummary),
						"verification_passed":  fmt.Sprintf("%t", parsed.Execution.VerificationPassed),
						"executed":             fmt.Sprintf("%t", parsed.Execution.Executed),
						"blocked":              fmt.Sprintf("%t", parsed.Execution.Blocked),
						"exit_code":            fmt.Sprintf("%d", parsed.Execution.ExitCode),
						"stdout_excerpt":       strings.TrimSpace(parsed.Execution.StdoutExcerpt),
						"stderr_excerpt":       strings.TrimSpace(parsed.Execution.StderrExcerpt),
						"log_highlights":       strings.Join(parsed.Execution.LogHighlights, " | "),
						"commands":             strings.Join(parsed.Execution.Commands, " | "),
					},
				}
				item, _ = app.ScientistBench.UpsertArtifact(ctx, caseID, runtimeArtifact)
			}
			if len(parsed.Execution.OutputFiles) > 0 {
				resultArtifact := scientistbench.Artifact{
					ID:            "artifact-" + uuid.NewString(),
					Kind:          scientistbench.ArtifactResultBundle,
					Label:         "Execution Outputs",
					ProducerRole:  run.Role,
					ProducerRunID: run.ID,
					CreatedAt:     time.Now().Unix(),
					Metadata: map[string]string{
						"runtime_id":   strings.TrimSpace(parsed.Execution.RuntimeID),
						"output_files": strings.Join(parsed.Execution.OutputFiles, " | "),
						"commands":     strings.Join(parsed.Execution.Commands, " | "),
					},
				}
				item, _ = app.ScientistBench.UpsertArtifact(ctx, caseID, resultArtifact)
			}
		}
	}

	item = applyScientistBenchWorkerOutput(item, run, parsed)
	item = applyScientistBenchReviewOutput(item, run, parsed)
	item = refreshScientistBenchMetrics(item)
	if result.Error != nil {
		item, _ = app.Orchestrator.ApplySignal(item, node.FailureSignal)
		savedItem, _ := app.ScientistBench.Save(ctx, item)
		if !scientistBenchCaseIsTerminal(savedItem) {
			app.scheduleScientistBenchContinuation(caseID)
		}
		return
	}

	nextRole, nextErr := nextPendingRoleForNode(item, node)
	if nextErr == nil && nextRole != "" {
		item.GraphState.ActiveRole = nextRole
		savedItem, _ := app.ScientistBench.Save(ctx, item)
		if !scientistBenchCaseIsTerminal(savedItem) {
			app.scheduleScientistBenchContinuation(caseID)
		}
		return
	}

	signal := node.SuccessSignal
	if parsed.FailureSignal == node.FailureSignal {
		signal = node.FailureSignal
	} else if parsed.SuccessSignal == node.SuccessSignal {
		signal = node.SuccessSignal
	} else if strings.EqualFold(parsed.Status, "failed") {
		signal = node.FailureSignal
	} else if node.ID == "node-aggregate" {
		signal = scientistBenchAggregateSignal(item)
	}
	item, err = app.Orchestrator.ApplySignal(item, signal)
	if err != nil {
		return
	}
	savedItem, _ := app.ScientistBench.Save(ctx, item)
	if !scientistBenchCaseIsTerminal(savedItem) {
		app.scheduleScientistBenchContinuation(caseID)
	}
}

func buildScientistBenchWorkerPrompt(
	item scientistbench.Case,
	node orchestrator.NodeSpec,
	profile orchestrator.WorkerProfile,
	registry interface {
		ForNodeRole(string, string) []runtimex.Spec
	},
	executor interface {
		BuildPlans([]runtimex.Spec, runtimex.ExecutionRequest) []runtimex.Plan
	},
) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(profile.PromptPreamble))
	b.WriteString("\n\n")
	b.WriteString("Scientist Bench case context.\n")
	fmt.Fprintf(&b, "Case ID: %s\n", item.ID)
	fmt.Fprintf(&b, "Mode: %s\n", item.Mode)
	fmt.Fprintf(&b, "Level: %s\n", item.Level)
	if strings.TrimSpace(item.Title) != "" {
		fmt.Fprintf(&b, "Title: %s\n", item.Title)
	}
	if strings.TrimSpace(item.Inputs.CoreIdea) != "" {
		fmt.Fprintf(&b, "Core idea: %s\n", item.Inputs.CoreIdea)
	}
	if item.Inputs.Dataset.Name != "" {
		fmt.Fprintf(&b, "Dataset: %s\n", item.Inputs.Dataset.Name)
	}
	if item.Inputs.TargetPaper.Title != "" {
		fmt.Fprintf(&b, "Target paper: %s\n", item.Inputs.TargetPaper.Title)
	}
	if len(item.Inputs.ReferenceSet) > 0 {
		fmt.Fprintf(&b, "Reference count: %d\n", len(item.Inputs.ReferenceSet))
	}
	if len(item.Inputs.MaskedMethodSpec.PipelineSummary) > 0 {
		fmt.Fprintf(&b, "Pipeline summary: %s\n", strings.Join(item.Inputs.MaskedMethodSpec.PipelineSummary, " | "))
	}
	if len(item.Inputs.Constraints) > 0 {
		fmt.Fprintf(&b, "Constraints: %s\n", strings.Join(item.Inputs.Constraints, " | "))
	}
	appendScientistBenchContextSections(&b, item, node, profile)
	appendScientistBenchRuntimeSection(registry, executor, &b, item, node, profile)

	b.WriteString("\nAssigned node.\n")
	fmt.Fprintf(&b, "Node ID: %s\n", node.ID)
	if node.Stage != "" {
		fmt.Fprintf(&b, "Stage: %s\n", node.Stage)
	}
	if len(node.Inputs) > 0 {
		fmt.Fprintf(&b, "Required inputs: %s\n", strings.Join(node.Inputs, ", "))
	}
	if len(node.Outputs) > 0 {
		fmt.Fprintf(&b, "Expected outputs: %s\n", strings.Join(node.Outputs, ", "))
	}
	if node.SuccessSignal != "" {
		fmt.Fprintf(&b, "Success signal for this node: %s\n", node.SuccessSignal)
	}
	if node.FailureSignal != "" {
		fmt.Fprintf(&b, "Failure signal for this node: %s\n", node.FailureSignal)
	}

	b.WriteString("\nExecution contract.\n")
	b.WriteString("- Work only within your role boundary.\n")
	b.WriteString("- Use available tools when they materially improve the output.\n")
	b.WriteString("- Be concrete and evidence-driven.\n")
	b.WriteString("- Your final response must be valid JSON without markdown fences.\n")
	b.WriteString("- Use this shape: {\"status\":\"succeeded|failed|needs_revision\",\"summary\":\"...\",\"success_signal\":\"...\",\"failure_signal\":\"...\",\"evidence_summary\":[...],\"citations\":[...],\"risks\":[...],\"ideas\":[...],\"objections\":[...],\"method_plan\":{\"summary\":\"...\",\"pipeline_steps\":[...],\"acceptance_checks\":[...],\"implementation_notes\":[...],\"runtime_hints\":[...]},\"execution\":{\"runtime_id\":\"...\",\"commands\":[...],\"verification_summary\":\"...\",\"verification_passed\":true,\"output_files\":[...],\"log_highlights\":[...]},\"review\":{\"decision\":\"...\",\"summary\":\"...\",\"strengths\":[...],\"weaknesses\":[...],\"questions\":[...],\"confidence\":0.0,\"readable_paper\":true,\"novel_insight_present\":true,\"code_runs\":true,\"scores\":{\"overall\":0.0,\"idea_quality\":0.0,\"method_soundness\":0.0,\"result_interpretation\":0.0,\"writing_quality\":0.0}},\"comparison\":{\"summary\":\"...\",\"strengths\":[...],\"weaknesses\":[...],\"motivation_alignment\":0.0,\"methodology_alignment\":0.0,\"novelty_alignment\":0.0,\"experimental_alignment\":0.0,\"confidence\":0.0}}\n")
	b.WriteString("- Only set success_signal or failure_signal if you are confident it matches the assigned node contract.\n")
	return strings.TrimSpace(b.String())
}

func findScientistBenchRun(item scientistbench.Case, runID string) (scientistbench.RunRecord, bool) {
	for _, run := range item.Runs {
		if run.ID == runID {
			return run, true
		}
	}
	return scientistbench.RunRecord{}, false
}

func nextRoleOrdinal(item scientistbench.Case, roleID string) int {
	count := 0
	for _, run := range item.Runs {
		if run.Role == roleID {
			count++
		}
	}
	return count + 1
}

func (app *App) reconcileScientistBenchGraphFromRuns(item scientistbench.Case) (scientistbench.Case, bool, error) {
	if app.Orchestrator == nil {
		return item, false, nil
	}

	changed := false
	for i := 0; i < 32; i++ {
		if scientistBenchCaseIsTerminal(item) {
			return item, changed, nil
		}
		node, ok := app.Orchestrator.GetNode(item.GraphState.ActiveNode)
		if !ok {
			return scientistbench.Case{}, changed, fmt.Errorf("active node %s is not registered", item.GraphState.ActiveNode)
		}

		nextRole, err := nextPendingRoleForNode(item, node)
		if err == nil && nextRole != "" {
			if item.GraphState.ActiveRole != nextRole {
				item.GraphState.ActiveRole = nextRole
				changed = true
			}
			return item, changed, nil
		}

		signal := scientistBenchRecoveredNodeSignal(item, node)
		if signal == "" {
			return item, changed, nil
		}
		updated, applyErr := app.Orchestrator.ApplySignal(item, signal)
		if applyErr != nil {
			return scientistbench.Case{}, changed, applyErr
		}
		item = updated
		changed = true
	}
	return scientistbench.Case{}, changed, fmt.Errorf("scientist bench graph reconciliation exceeded safety limit")
}

func (app *App) reconcileScientistBenchCheckpoint(
	item scientistbench.Case,
	nodeID string,
	roleID string,
	now time.Time,
) (scientistbench.Case, *scientistbench.RunRecord) {
	run, ok := latestRecoverableNodeRoleRun(item, nodeID, roleID)
	if !ok {
		return item, nil
	}
	if shouldKeepScientistBenchRunAlive(app.TaskRuns, run, now) {
		copyRun := run
		return item, &copyRun
	}

	taskStatus, detail := scientistBenchRecoveryDetail(app.TaskRuns, run)
	run.Status = "failed"
	run.TaskRunStatus = firstNonEmpty(taskStatus, run.TaskRunStatus, "failed")
	run.Error = detail
	run.FinishedAt = now.Unix()
	item.Runs = upsertScientistBenchRunLocal(item.Runs, run)
	item.Artifacts = append(item.Artifacts, scientistbench.Artifact{
		ID:            "artifact-" + uuid.NewString(),
		Kind:          scientistbench.ArtifactLog,
		Label:         "Recovery Checkpoint",
		ProducerRole:  roleID,
		ProducerRunID: run.ID,
		CreatedAt:     now.Unix(),
		Metadata: map[string]string{
			"node_id":         strings.TrimSpace(nodeID),
			"role_id":         strings.TrimSpace(roleID),
			"recovery_reason": detail,
			"taskrun_status":  firstNonEmpty(taskStatus, run.TaskRunStatus),
		},
	})
	return item, nil
}

func firstRole(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return strings.TrimSpace(items[0])
}

func nextPendingRoleForNode(item scientistbench.Case, node orchestrator.NodeSpec) (string, error) {
	for _, roleID := range node.AssignedRoles {
		if !hasSuccessfulNodeRoleRun(item, node.ID, roleID) {
			return strings.TrimSpace(roleID), nil
		}
	}
	return "", fmt.Errorf("no pending roles remain for node %s", node.ID)
}

func hasSuccessfulNodeRoleRun(item scientistbench.Case, nodeID string, roleID string) bool {
	for _, run := range item.Runs {
		if run.NodeID == nodeID && run.Role == roleID && run.Status == "complete" && strings.TrimSpace(run.Error) == "" {
			return true
		}
	}
	return false
}

func scientistBenchRecoveredNodeSignal(item scientistbench.Case, node orchestrator.NodeSpec) string {
	if node.ID == "node-aggregate" {
		return scientistBenchAggregateSignal(item)
	}
	for _, run := range item.Runs {
		if run.NodeID != node.ID || run.Status != "complete" || strings.TrimSpace(run.Error) != "" {
			continue
		}
		for _, signal := range run.SignalsEmitted {
			switch strings.TrimSpace(signal) {
			case node.SuccessSignal:
				return node.SuccessSignal
			case node.FailureSignal:
				return node.FailureSignal
			}
		}
	}
	if node.SuccessSignal != "" && allAssignedRolesCompleted(item, node) {
		return node.SuccessSignal
	}
	return ""
}

func allAssignedRolesCompleted(item scientistbench.Case, node orchestrator.NodeSpec) bool {
	if len(node.AssignedRoles) == 0 {
		return false
	}
	for _, roleID := range node.AssignedRoles {
		if !hasSuccessfulNodeRoleRun(item, node.ID, roleID) {
			return false
		}
	}
	return true
}

func scientistBenchCaseIsTerminal(item scientistbench.Case) bool {
	switch item.Status {
	case scientistbench.StatusResolved, scientistbench.StatusNotResolved:
		return true
	default:
		return false
	}
}

func latestRecoverableNodeRoleRun(item scientistbench.Case, nodeID string, roleID string) (scientistbench.RunRecord, bool) {
	for _, run := range item.Runs {
		if run.NodeID != nodeID || run.Role != roleID {
			continue
		}
		if isScientistBenchRunTerminal(run) {
			continue
		}
		return run, true
	}
	return scientistbench.RunRecord{}, false
}

func isScientistBenchRunTerminal(run scientistbench.RunRecord) bool {
	if run.FinishedAt > 0 {
		return true
	}
	switch strings.TrimSpace(run.Status) {
	case "complete", "failed":
		return true
	default:
		return false
	}
}

func shouldKeepScientistBenchRunAlive(taskRuns taskrun.Service, run scientistbench.RunRecord, now time.Time) bool {
	if isScientistBenchRunTerminal(run) {
		return false
	}
	age := now.Sub(time.Unix(run.StartedAt, 0))
	if age < 0 {
		age = 0
	}
	status := strings.TrimSpace(run.TaskRunStatus)
	if taskRuns != nil && strings.TrimSpace(run.TaskRunSessionID) != "" {
		if task, ok := taskRuns.Get(run.TaskRunSessionID); ok {
			status = string(task.Status)
		}
	}
	switch status {
	case string(taskrun.StatusQueued), string(taskrun.StatusRunning), string(taskrun.StatusIdle), string(taskrun.StatusEmpty), "":
		return age < scientistBenchCheckpointGracePeriod
	default:
		return false
	}
}

func scientistBenchRecoveryDetail(taskRuns taskrun.Service, run scientistbench.RunRecord) (string, string) {
	if taskRuns != nil && strings.TrimSpace(run.TaskRunSessionID) != "" {
		if task, ok := taskRuns.Get(run.TaskRunSessionID); ok {
			switch task.Status {
			case taskrun.StatusComplete:
				return string(task.Status), "Recovered completed task run without durable node checkpoint; rerunning active checkpoint"
			case taskrun.StatusBlocked:
				return string(task.Status), firstNonEmpty(strings.TrimSpace(task.Detail), "Recovered blocked task run; rerunning active checkpoint")
			case taskrun.StatusCanceled, taskrun.StatusFailed:
				return string(task.Status), firstNonEmpty(strings.TrimSpace(task.Detail), "Recovered failed task run; rerunning active checkpoint")
			case taskrun.StatusQueued, taskrun.StatusRunning, taskrun.StatusIdle, taskrun.StatusEmpty:
				return string(task.Status), "Recovered stale in-progress task run; rerunning active checkpoint"
			}
			return string(task.Status), "Recovered unfinished task run; rerunning active checkpoint"
		}
	}
	return strings.TrimSpace(run.TaskRunStatus), "Recovered orphaned task run; rerunning active checkpoint"
}

func upsertScientistBenchRunLocal(items []scientistbench.RunRecord, run scientistbench.RunRecord) []scientistbench.RunRecord {
	for i, item := range items {
		if item.ID == run.ID {
			items[i] = run
			return items
		}
	}
	return append(items, run)
}

func applyScientistBenchWorkerOutput(
	item scientistbench.Case,
	run scientistbench.RunRecord,
	output orchestrator.WorkerOutput,
) scientistbench.Case {
	switch run.Role {
	case "idea_maker":
		item.IdeaModule.Status = "drafted"
		for _, idea := range output.Ideas {
			item.IdeaModule.AcceptedIdeas = append(item.IdeaModule.AcceptedIdeas, scientistbench.IdeaCandidate{
				ID:             "idea-" + uuid.NewString(),
				Title:          strings.TrimSpace(idea.Title),
				Summary:        strings.TrimSpace(idea.Summary),
				NoveltyClaim:   strings.TrimSpace(idea.NoveltyClaim),
				Hypotheses:     sanitizeStrings(idea.Hypotheses),
				SupportingRefs: sanitizeStrings(idea.SupportingRefs),
				CreatedAt:      time.Now().Unix(),
			})
		}
	case "idea_hater":
		item.IdeaModule.Status = "criticized"
		for _, objection := range output.Objections {
			item.IdeaModule.Objections = append(item.IdeaModule.Objections, scientistbench.IdeaObjection{
				ID:           "obj-" + uuid.NewString(),
				IdeaID:       strings.TrimSpace(objection.IdeaTitle),
				Summary:      strings.TrimSpace(objection.Summary),
				Severity:     strings.TrimSpace(objection.Severity),
				EvidenceRefs: sanitizeStrings(objection.EvidenceRefs),
				CreatedAt:    time.Now().Unix(),
			})
		}
	case "chief_scientist":
		if run.NodeID == "node-idea-gate" {
			item.IdeaModule.Status = "accepted"
			if output.FailureSignal == "idea_gate_rejected" || strings.EqualFold(output.Status, "failed") {
				item.IdeaModule.Status = "rejected"
				item.IdeaModule.RejectedIdeas = append(item.IdeaModule.RejectedIdeas, item.IdeaModule.AcceptedIdeas...)
				item.IdeaModule.AcceptedIdeas = nil
			}
		}
	case "method_planner":
		if output.MethodPlan != nil {
			item.IdeaModule.Status = "planned"
		}
	case "execution_agent":
		if output.Execution != nil && output.Execution.VerificationPassed {
			item.IdeaModule.Status = item.IdeaModule.Status
		}
	}
	return item
}

func normalizeRunSignals(output orchestrator.WorkerOutput, node orchestrator.NodeSpec) []string {
	signals := make([]string, 0, 2)
	if output.SuccessSignal == node.SuccessSignal && output.SuccessSignal != "" {
		signals = append(signals, output.SuccessSignal)
	}
	if output.FailureSignal == node.FailureSignal && output.FailureSignal != "" {
		signals = append(signals, output.FailureSignal)
	}
	return signals
}

func sanitizeStrings(items []string) []string {
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func artifactKindForNode(node orchestrator.NodeSpec) scientistbench.ArtifactKind {
	switch node.ID {
	case "node-research-plan", "node-corpus-retrieval":
		return scientistbench.ArtifactCitation
	case "node-idea-gate":
		return scientistbench.ArtifactLog
	case "node-analysis-figures":
		return scientistbench.ArtifactFigure
	case "node-paper-draft":
		return scientistbench.ArtifactPaperDraft
	case "node-advisor-review":
		return scientistbench.ArtifactAdvisorReport
	case "node-judge-review":
		return scientistbench.ArtifactJudgeReport
	case "node-domain-review", "node-paper-compare":
		return scientistbench.ArtifactReview
	case "node-execution":
		return scientistbench.ArtifactDockerLog
	default:
		return scientistbench.ArtifactLog
	}
}

func artifactLabelForNode(node orchestrator.NodeSpec) string {
	switch node.ID {
	case "node-research-plan":
		return "Research Plan"
	case "node-corpus-retrieval":
		return "Evidence Summary"
	case "node-idea-gate":
		return "Idea Debate Output"
	case "node-method-plan":
		return "Method Specification"
	case "node-analysis-figures":
		return "Figures and Tables"
	case "node-paper-draft":
		return "Paper Draft"
	case "node-advisor-review":
		return "Advisor Report"
	case "node-judge-review":
		return "Judge Score"
	case "node-domain-review":
		return "Domain Expert Review"
	case "node-paper-compare":
		return "Paper Comparison Review"
	case "node-execution":
		return "Execution Summary"
	default:
		return "Node Output"
	}
}

func appendScientistBenchContextSections(
	b *strings.Builder,
	item scientistbench.Case,
	node orchestrator.NodeSpec,
	profile orchestrator.WorkerProfile,
) {
	if b == nil {
		return
	}

	if evidence := latestEvidenceArtifact(item); evidence != nil {
		b.WriteString("\nPrior evidence pack.\n")
		if summary := strings.TrimSpace(evidence.Metadata["summary"]); summary != "" {
			fmt.Fprintf(b, "Evidence summary: %s\n", summary)
		}
		if citations := strings.TrimSpace(evidence.Metadata["citations"]); citations != "" {
			fmt.Fprintf(b, "Citations: %s\n", citations)
		}
		if risks := strings.TrimSpace(evidence.Metadata["risks"]); risks != "" {
			fmt.Fprintf(b, "Risks: %s\n", risks)
		}
		if evidenceText := strings.TrimSpace(evidence.Metadata["evidence"]); evidenceText != "" {
			fmt.Fprintf(b, "Evidence bullets: %s\n", evidenceText)
		}
	}

	if len(item.IdeaModule.AcceptedIdeas) > 0 {
		b.WriteString("\nAccepted ideas so far.\n")
		for _, idea := range item.IdeaModule.AcceptedIdeas {
			fmt.Fprintf(b, "- %s: %s\n", idea.Title, idea.Summary)
			if idea.NoveltyClaim != "" {
				fmt.Fprintf(b, "  Novelty claim: %s\n", idea.NoveltyClaim)
			}
			if len(idea.Hypotheses) > 0 {
				fmt.Fprintf(b, "  Hypotheses: %s\n", strings.Join(idea.Hypotheses, " | "))
			}
		}
	}

	if len(item.IdeaModule.Objections) > 0 && (profile.RoleID == "idea_hater" || profile.RoleID == "chief_scientist" || node.ID == "node-method-plan") {
		b.WriteString("\nExisting objections.\n")
		for _, objection := range item.IdeaModule.Objections {
			fmt.Fprintf(b, "- [%s] %s\n", objection.Severity, objection.Summary)
		}
	}

	if method := latestMethodArtifact(item); method != nil {
		b.WriteString("\nCurrent method plan.\n")
		if summary := strings.TrimSpace(method.Metadata["method_summary"]); summary != "" {
			fmt.Fprintf(b, "Method summary: %s\n", summary)
		}
		if pipeline := strings.TrimSpace(method.Metadata["method_pipeline"]); pipeline != "" {
			fmt.Fprintf(b, "Pipeline steps: %s\n", pipeline)
		}
		if checks := strings.TrimSpace(method.Metadata["acceptance_checks"]); checks != "" {
			fmt.Fprintf(b, "Acceptance checks: %s\n", checks)
		}
		if notes := strings.TrimSpace(method.Metadata["implementation_notes"]); notes != "" {
			fmt.Fprintf(b, "Implementation notes: %s\n", notes)
		}
		if hints := strings.TrimSpace(method.Metadata["runtime_hints"]); hints != "" {
			fmt.Fprintf(b, "Runtime hints: %s\n", hints)
		}
	}

	if paperDraft := latestArtifactOfKind(item, scientistbench.ArtifactPaperDraft); paperDraft != nil {
		b.WriteString("\nCurrent paper draft.\n")
		if summary := strings.TrimSpace(paperDraft.Metadata["summary"]); summary != "" {
			fmt.Fprintf(b, "Draft summary: %s\n", summary)
		}
		if path := strings.TrimSpace(paperDraft.Path); path != "" {
			fmt.Fprintf(b, "Draft path: %s\n", path)
		}
	}

	if advisor := latestArtifactOfKind(item, scientistbench.ArtifactAdvisorReport); advisor != nil &&
		(profile.RoleID == "judge_agent" || profile.RoleID == "chief_scientist") {
		b.WriteString("\nLatest advisor report.\n")
		if summary := strings.TrimSpace(advisor.Metadata["summary"]); summary != "" {
			fmt.Fprintf(b, "Advisor summary: %s\n", summary)
		}
		if risks := strings.TrimSpace(advisor.Metadata["risks"]); risks != "" {
			fmt.Fprintf(b, "Advisor risks: %s\n", risks)
		}
	}

	if profile.RoleID == "paper_comparison_reviewer" || node.ID == "node-paper-compare" {
		b.WriteString("\nTarget paper context.\n")
		if title := strings.TrimSpace(item.Inputs.TargetPaper.Title); title != "" {
			fmt.Fprintf(b, "Target title: %s\n", title)
		}
		if abstract := strings.TrimSpace(item.Inputs.TargetPaper.Abstract); abstract != "" {
			fmt.Fprintf(b, "Target abstract: %s\n", abstract)
		}
		if notes := strings.TrimSpace(item.Inputs.TargetPaper.EvaluationNotes); notes != "" {
			fmt.Fprintf(b, "Evaluation notes: %s\n", notes)
		}
	}

	if len(item.Reviews) > 0 && (profile.RoleID == "chief_scientist" || node.ID == "node-aggregate" || profile.RoleID == "domain_expert_reviewer") {
		b.WriteString("\nEvaluation snapshot.\n")
		for _, review := range item.Reviews {
			fmt.Fprintf(b, "- [%s/%s] %s\n", review.Type, review.Decision, review.Summary)
			if review.Scores.Overall > 0 {
				fmt.Fprintf(b, "  Overall score: %.2f\n", review.Scores.Overall)
			}
		}
	}

	if node.ID == "node-aggregate" {
		b.WriteString("\nCurrent aggregate scores.\n")
		fmt.Fprintf(b, "Paper generation overall: %.2f\n", item.Scores.PaperGeneration.OverallSuccess)
		fmt.Fprintf(b, "Reproduction correctness mean: %.2f\n", item.Scores.Reproduction.CorrectnessMean)
		fmt.Fprintf(b, "Paper comparison methodology alignment: %.2f\n", item.Scores.PaperComparison.MethodologyAlignment)
	}
}

func appendScientistBenchRuntimeSection(
	registry interface {
		ForNodeRole(string, string) []runtimex.Spec
	},
	executor interface {
		BuildPlans([]runtimex.Spec, runtimex.ExecutionRequest) []runtimex.Plan
	},
	b *strings.Builder,
	item scientistbench.Case,
	node orchestrator.NodeSpec,
	profile orchestrator.WorkerProfile,
) {
	if b == nil || registry == nil {
		return
	}
	items := registry.ForNodeRole(node.ID, profile.RoleID)
	if len(items) == 0 {
		return
	}

	b.WriteString("\nAvailable runtime profiles.\n")
	for _, item := range items {
		fmt.Fprintf(b, "- %s [%s] image=%s timeout=%dmin cpu=%d mem=%dGB artifacts=%s safety=%s\n",
			item.ID,
			item.RuntimeClass,
			item.Image,
			item.ResourceLimits.TimeoutMinutes,
			item.ResourceLimits.CPU,
			item.ResourceLimits.MemoryGB,
			strings.Join(item.ArtifactsEmitted, "|"),
			strings.Join(item.SafetyFlags, "|"),
		)
	}

	if executor == nil {
		return
	}

	hints := []string{}
	if method := latestMethodArtifact(item); method != nil {
		if rawHints := strings.TrimSpace(method.Metadata["runtime_hints"]); rawHints != "" {
			hints = append(hints, strings.Split(rawHints, " | ")...)
		}
	}
	plans := executor.BuildPlans(items, runtimex.ExecutionRequest{
		NodeID:       node.ID,
		RoleID:       profile.RoleID,
		Workdir:      scientistBenchWorkingDirectory(),
		RuntimeHints: sanitizeStrings(hints),
	})
	if len(plans) == 0 {
		return
	}

	b.WriteString("\nSuggested runtime plans.\n")
	for _, plan := range plans {
		fmt.Fprintf(b, "- %s: %s\n", plan.RuntimeID, plan.Summary)
		fmt.Fprintf(b, "  Output dir: %s\n", plan.OutputDir)
		for _, command := range plan.Commands {
			fmt.Fprintf(b, "  Command: %s\n", command)
		}
	}
}

func (app *App) reconcileScientistBenchRuntimeExecution(
	ctx context.Context,
	run scientistbench.RunRecord,
	node orchestrator.NodeSpec,
	parsed orchestrator.WorkerOutput,
) orchestrator.WorkerOutput {
	if run.Role != "execution_agent" || parsed.Execution == nil {
		return parsed
	}
	if app.RuntimeRegistry == nil || app.RuntimeExecutor == nil || app.RuntimeRunner == nil {
		return parsed
	}

	specs := app.RuntimeRegistry.ForNodeRole(node.ID, run.Role)
	if len(specs) == 0 {
		return parsed
	}

	selectedRuntimeID := strings.TrimSpace(parsed.Execution.RuntimeID)
	selectedSpec := specs[0]
	if selectedRuntimeID != "" {
		for _, spec := range specs {
			if spec.ID == selectedRuntimeID {
				selectedSpec = spec
				break
			}
		}
	}

	plan := app.RuntimeExecutor.BuildPlan(selectedSpec, runtimex.ExecutionRequest{
		NodeID:  node.ID,
		RoleID:  run.Role,
		Workdir: scientistBenchWorkingDirectory(),
	})
	if app.TaskRuns != nil {
		app.TaskRuns.UpdateDetail(run.SessionID, "Running runtime plan "+plan.RuntimeID, "runtime_executor", nil)
	}

	execResult := app.RuntimeRunner.RunPlan(ctx, plan, runtimex.RunRequest{
		SessionID: run.SessionID,
		Execute:   true,
	})
	parsed.Execution.RuntimeID = plan.RuntimeID
	parsed.Execution.Commands = append([]string{}, plan.Commands...)
	parsed.Execution.VerificationSummary = firstNonEmpty(
		strings.TrimSpace(execResult.Summary),
		strings.TrimSpace(parsed.Execution.VerificationSummary),
	)
	parsed.Execution.VerificationPassed = execResult.Succeeded
	parsed.Execution.Executed = execResult.Executed
	parsed.Execution.Blocked = execResult.Blocked
	parsed.Execution.ExitCode = execResult.ExitCode
	parsed.Execution.StdoutExcerpt = summarizeRuntimeOutput(execResult.Stdout, 240)
	parsed.Execution.StderrExcerpt = summarizeRuntimeOutput(execResult.Stderr, 240)
	parsed.Execution.LogHighlights = sanitizeStrings(append(parsed.Execution.LogHighlights,
		summarizeRuntimeOutput(execResult.Stdout, 240),
		summarizeRuntimeOutput(execResult.Stderr, 240),
	))
	if execResult.OutputDir != "" {
		parsed.Execution.OutputFiles = sanitizeStrings(append(parsed.Execution.OutputFiles, execResult.OutputDir))
	}
	if execResult.Succeeded {
		if strings.TrimSpace(parsed.Status) == "" || strings.EqualFold(parsed.Status, "failed") {
			parsed.Status = "succeeded"
		}
		if parsed.SuccessSignal == "" {
			parsed.SuccessSignal = node.SuccessSignal
		}
		parsed.FailureSignal = ""
		if strings.TrimSpace(parsed.Summary) == "" {
			parsed.Summary = execResult.Summary
		}
		return parsed
	}

	parsed.Status = "failed"
	parsed.SuccessSignal = ""
	parsed.FailureSignal = node.FailureSignal
	parsed.Risks = sanitizeStrings(append(parsed.Risks, execResult.Summary))
	parsed.Summary = firstNonEmpty(parsed.Summary, execResult.Summary)
	return parsed
}

func latestEvidenceArtifact(item scientistbench.Case) *scientistbench.Artifact {
	for _, artifact := range item.Artifacts {
		if artifact.Kind != scientistbench.ArtifactCitation {
			continue
		}
		if strings.TrimSpace(artifact.Metadata["citations"]) == "" && strings.TrimSpace(artifact.Metadata["evidence"]) == "" {
			continue
		}
		copyArtifact := artifact
		return &copyArtifact
	}
	return nil
}

func latestMethodArtifact(item scientistbench.Case) *scientistbench.Artifact {
	for _, artifact := range item.Artifacts {
		if strings.TrimSpace(artifact.Metadata["method_summary"]) == "" &&
			strings.TrimSpace(artifact.Metadata["method_pipeline"]) == "" {
			continue
		}
		copyArtifact := artifact
		return &copyArtifact
	}
	return nil
}

func latestArtifactOfKind(item scientistbench.Case, kind scientistbench.ArtifactKind) *scientistbench.Artifact {
	for _, artifact := range item.Artifacts {
		if artifact.Kind != kind {
			continue
		}
		copyArtifact := artifact
		return &copyArtifact
	}
	return nil
}

func (app *App) captureRuntimePlansForCase(
	item scientistbench.Case,
	nodeID string,
	roleID string,
	runID string,
) scientistbench.Case {
	if app.RuntimeRegistry == nil || app.RuntimeExecutor == nil {
		return item
	}

	specs := app.RuntimeRegistry.ForNodeRole(strings.TrimSpace(nodeID), strings.TrimSpace(roleID))
	if len(specs) == 0 {
		return item
	}

	hints := []string{}
	if method := latestMethodArtifact(item); method != nil {
		if rawHints := strings.TrimSpace(method.Metadata["runtime_hints"]); rawHints != "" {
			hints = append(hints, strings.Split(rawHints, " | ")...)
		}
	}
	plans := app.RuntimeExecutor.BuildPlans(specs, runtimex.ExecutionRequest{
		NodeID:       nodeID,
		RoleID:       roleID,
		Workdir:      scientistBenchWorkingDirectory(),
		RuntimeHints: sanitizeStrings(hints),
	})
	for _, plan := range plans {
		item.Artifacts = append(item.Artifacts, scientistbench.Artifact{
			ID:            "artifact-" + uuid.NewString(),
			Kind:          scientistbench.ArtifactLog,
			Label:         "Runtime Plan",
			ProducerRole:  roleID,
			ProducerRunID: runID,
			CreatedAt:     time.Now().Unix(),
			Metadata: map[string]string{
				"runtime_id": plan.RuntimeID,
				"summary":    plan.Summary,
				"commands":   strings.Join(plan.Commands, " | "),
				"output_dir": plan.OutputDir,
				"dry_run":    fmt.Sprintf("%t", plan.DryRun),
				"safety":     strings.Join(plan.SafetyFlags, " | "),
			},
		})
	}
	return item
}

func truncateScientistBenchText(text string, maxLen int) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	if maxLen <= 0 || len(text) <= maxLen {
		return text
	}
	if maxLen <= 3 {
		return text[:maxLen]
	}
	return text[:maxLen-3] + "..."
}

func scientistBenchEventContent(event agent.AgentEvent) string {
	return strings.TrimSpace(event.Message.Content().String())
}

func applyScientistBenchReviewOutput(
	item scientistbench.Case,
	run scientistbench.RunRecord,
	output orchestrator.WorkerOutput,
) scientistbench.Case {
	now := time.Now().Unix()
	switch run.Role {
	case "advisor_agent", "judge_agent", "domain_expert_reviewer":
		if output.Review == nil {
			return item
		}
		item.Reviews = append(item.Reviews, scientistbench.Review{
			ID:               "review-" + uuid.NewString(),
			Type:             reviewTypeForRole(run.Role),
			ReviewerRole:     run.Role,
			TargetArtifactID: reviewTargetArtifactID(item, run.Role),
			TargetPaperID:    strings.TrimSpace(item.Inputs.TargetPaper.PaperID),
			CreatedAt:        now,
			Decision:         strings.TrimSpace(output.Review.Decision),
			Summary:          firstNonEmpty(strings.TrimSpace(output.Review.Summary), strings.TrimSpace(output.Summary)),
			Strengths:        sanitizeStrings(output.Review.Strengths),
			Weaknesses:       sanitizeStrings(output.Review.Weaknesses),
			Questions:        sanitizeStrings(output.Review.Questions),
			Scores: scientistbench.ReviewScores{
				Overall:              output.Review.Scores.Overall,
				IdeaQuality:          output.Review.Scores.IdeaQuality,
				MethodSoundness:      output.Review.Scores.MethodSoundness,
				ResultInterpretation: output.Review.Scores.ResultInterpretation,
				WritingQuality:       output.Review.Scores.WritingQuality,
			},
			Confidence: output.Review.Confidence,
		})
		switch run.Role {
		case "advisor_agent":
			item.Scores.Reproduction.AdvisorReports = countReviewsByType(item.Reviews, scientistbench.ReviewAdvisor)
		case "judge_agent":
			judgeScores := judgeReviewScores(item.Reviews)
			item.Scores.Reproduction.JudgeScores = judgeScores
			item.Scores.Reproduction.CorrectnessMean = meanFloat64(judgeScores)
			item.Scores.Reproduction.CorrectnessStd = stddevFloat64(judgeScores)
		case "domain_expert_reviewer":
			item.Scores.PaperGeneration.ReadablePaper = output.Review.ReadablePaper
			item.Scores.PaperGeneration.NovelInsightPresent = output.Review.NovelInsightPresent
			item.Scores.PaperGeneration.CodeRuns = output.Review.CodeRuns || hasExecutionEvidence(item)
			item.Scores.PaperGeneration.IdeaQuality = output.Review.Scores.IdeaQuality
			item.Scores.PaperGeneration.MethodSoundness = output.Review.Scores.MethodSoundness
			item.Scores.PaperGeneration.ResultInterpretation = output.Review.Scores.ResultInterpretation
			item.Scores.PaperGeneration.WritingQuality = output.Review.Scores.WritingQuality
			item.Scores.PaperGeneration.OverallSuccess = computePaperGenerationOverall(item.Scores.PaperGeneration, output.Review.Scores.Overall)
		}
	case "paper_comparison_reviewer":
		if output.Comparison == nil {
			return item
		}
		item.Reviews = append(item.Reviews, scientistbench.Review{
			ID:               "review-" + uuid.NewString(),
			Type:             scientistbench.ReviewPaperComparison,
			ReviewerRole:     run.Role,
			TargetArtifactID: reviewTargetArtifactID(item, run.Role),
			TargetPaperID:    strings.TrimSpace(item.Inputs.TargetPaper.PaperID),
			CreatedAt:        now,
			Decision:         "comparison_ready",
			Summary:          firstNonEmpty(strings.TrimSpace(output.Comparison.Summary), strings.TrimSpace(output.Summary)),
			Strengths:        sanitizeStrings(output.Comparison.Strengths),
			Weaknesses:       sanitizeStrings(output.Comparison.Weaknesses),
			Confidence:       output.Comparison.Confidence,
			Scores: scientistbench.ReviewScores{
				Overall: averageFloat64(
					output.Comparison.MotivationAlignment,
					output.Comparison.MethodologyAlignment,
					output.Comparison.NoveltyAlignment,
					output.Comparison.ExperimentalAlignment,
				),
			},
		})
		item.Scores.PaperComparison = scientistbench.PaperComparisonScores{
			MotivationAlignment:   output.Comparison.MotivationAlignment,
			MethodologyAlignment:  output.Comparison.MethodologyAlignment,
			NoveltyAlignment:      output.Comparison.NoveltyAlignment,
			ExperimentalAlignment: output.Comparison.ExperimentalAlignment,
		}
	}
	return item
}

func reviewTypeForRole(roleID string) scientistbench.ReviewType {
	switch strings.TrimSpace(roleID) {
	case "advisor_agent":
		return scientistbench.ReviewAdvisor
	case "judge_agent":
		return scientistbench.ReviewJudge
	case "paper_comparison_reviewer":
		return scientistbench.ReviewPaperComparison
	default:
		return scientistbench.ReviewDomainExpert
	}
}

func reviewTargetArtifactID(item scientistbench.Case, roleID string) string {
	switch strings.TrimSpace(roleID) {
	case "judge_agent":
		if artifact := latestArtifactOfKind(item, scientistbench.ArtifactAdvisorReport); artifact != nil {
			return artifact.ID
		}
	case "advisor_agent":
		if artifact := latestArtifactOfKind(item, scientistbench.ArtifactResultBundle); artifact != nil {
			return artifact.ID
		}
	default:
		if artifact := latestArtifactOfKind(item, scientistbench.ArtifactPaperDraft); artifact != nil {
			return artifact.ID
		}
	}
	return ""
}

func countReviewsByType(items []scientistbench.Review, reviewType scientistbench.ReviewType) int {
	total := 0
	for _, item := range items {
		if item.Type == reviewType {
			total++
		}
	}
	return total
}

func judgeReviewScores(items []scientistbench.Review) []float64 {
	out := make([]float64, 0, len(items))
	for _, item := range items {
		if item.Type != scientistbench.ReviewJudge {
			continue
		}
		if item.Scores.Overall <= 0 {
			continue
		}
		out = append(out, item.Scores.Overall)
	}
	return out
}

func hasExecutionEvidence(item scientistbench.Case) bool {
	for _, artifact := range item.Artifacts {
		if artifact.Kind == scientistbench.ArtifactResultBundle || artifact.Kind == scientistbench.ArtifactDockerLog {
			return true
		}
	}
	return false
}

func refreshScientistBenchMetrics(item scientistbench.Case) scientistbench.Case {
	item.Scores.Reproduction.AdvisorReports = countReviewsByType(item.Reviews, scientistbench.ReviewAdvisor)
	judgeScores := judgeReviewScores(item.Reviews)
	item.Scores.Reproduction.JudgeScores = judgeScores
	item.Scores.Reproduction.CorrectnessMean = meanFloat64(judgeScores)
	item.Scores.Reproduction.CorrectnessStd = stddevFloat64(judgeScores)
	item.Scores.Reproduction.Completeness = scientistBenchCompleteness(item)
	if hasPassingExecutionEvidence(item) {
		item.Scores.PaperGeneration.CodeRuns = true
	}
	if item.Scores.PaperGeneration.OverallSuccess == 0 &&
		(item.Scores.PaperGeneration.IdeaQuality > 0 ||
			item.Scores.PaperGeneration.MethodSoundness > 0 ||
			item.Scores.PaperGeneration.ResultInterpretation > 0 ||
			item.Scores.PaperGeneration.WritingQuality > 0) {
		item.Scores.PaperGeneration.OverallSuccess = computePaperGenerationOverall(item.Scores.PaperGeneration, 0)
	}
	return item
}

func scientistBenchCompleteness(item scientistbench.Case) float64 {
	if hasPassingExecutionEvidence(item) {
		return 1.0
	}
	if hasExecutionEvidence(item) {
		return 0.5
	}
	for _, artifact := range item.Artifacts {
		if artifact.Label == "Runtime Plan" {
			return 0.25
		}
	}
	return 0
}

func hasPassingExecutionEvidence(item scientistbench.Case) bool {
	for _, artifact := range item.Artifacts {
		if artifact.Kind != scientistbench.ArtifactDockerLog {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(artifact.Metadata["verification_passed"]), "true") {
			return true
		}
	}
	return false
}

func scientistBenchAggregateSignal(item scientistbench.Case) string {
	paper := item.Scores.PaperGeneration
	repro := item.Scores.Reproduction
	comparison := item.Scores.PaperComparison

	passesPaperGate := paper.ReadablePaper && paper.CodeRuns && paper.OverallSuccess >= 3.0
	passesReproGate := repro.Completeness >= 1.0 && (repro.CorrectnessMean == 0 || repro.CorrectnessMean >= 3.0)
	passesComparisonGate := true
	if strings.TrimSpace(item.Inputs.TargetPaper.Title) != "" {
		passesComparisonGate = comparison.MethodologyAlignment == 0 || comparison.MethodologyAlignment >= 2.5
	}
	if passesPaperGate && passesReproGate && passesComparisonGate {
		return string(scientistbench.SignalCaseResolved)
	}
	return string(scientistbench.SignalCaseNotResolved)
}

func computePaperGenerationOverall(scores scientistbench.PaperGenerationScores, reviewerOverall float64) float64 {
	base := averageFloat64(
		scores.IdeaQuality,
		scores.MethodSoundness,
		scores.ResultInterpretation,
		scores.WritingQuality,
	)
	if reviewerOverall > 0 {
		base = averageFloat64(base, reviewerOverall)
	}
	if scores.ReadablePaper {
		base += 0.25
	}
	if scores.NovelInsightPresent {
		base += 0.25
	}
	if scores.CodeRuns {
		base += 0.25
	}
	if base > 5 {
		base = 5
	}
	return base
}

func averageFloat64(values ...float64) float64 {
	sum := 0.0
	count := 0.0
	for _, value := range values {
		if value <= 0 {
			continue
		}
		sum += value
		count++
	}
	if count == 0 {
		return 0
	}
	return sum / count
}

func meanFloat64(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}

func stddevFloat64(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}
	mean := meanFloat64(values)
	variance := 0.0
	for _, value := range values {
		delta := value - mean
		variance += delta * delta
	}
	return math.Sqrt(variance / float64(len(values)))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func summarizeRuntimeOutput(text string, maxLen int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if maxLen > 0 && len(text) > maxLen {
		text = text[:maxLen-3] + "..."
	}
	return text
}

func scientistBenchWorkingDirectory() string {
	if cfg := config.Get(); cfg != nil {
		if dir := strings.TrimSpace(cfg.WorkingDir); dir != "" {
			return dir
		}
	}
	if dir, err := os.Getwd(); err == nil {
		if dir = strings.TrimSpace(dir); dir != "" {
			return dir
		}
	}
	return "."
}
