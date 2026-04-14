package app

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/SciMate-AI/scicli/internal/llm/agent"
	"github.com/SciMate-AI/scicli/internal/logging"
	"github.com/SciMate-AI/scicli/internal/orchestrator"
	runtimex "github.com/SciMate-AI/scicli/internal/runtime"
	"github.com/SciMate-AI/scicli/internal/scientistbench"
	"github.com/SciMate-AI/scicli/internal/skills"
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
	if exhausted, exhaustedItem, err := app.enforceScientistBenchBudget(ctx, item); err != nil {
		return ScientistBenchNodeRun{}, err
	} else if exhausted {
		return ScientistBenchNodeRun{}, fmt.Errorf("case %s is terminal with status %s", exhaustedItem.ID, exhaustedItem.Status)
	}

	node, ok := app.Orchestrator.GetNode(item.GraphState.ActiveNode)
	if !ok {
		return ScientistBenchNodeRun{}, fmt.Errorf("active node %s is not registered", item.GraphState.ActiveNode)
	}
	readyRoles := readyRolesForNode(item, node)
	if len(readyRoles) == 0 {
		if liveRun, ok := latestRecoverableNodeRunForNode(item, node.ID); ok && shouldKeepScientistBenchRunAlive(app.TaskRuns, liveRun, time.Now()) {
			_ = app.postScientistBenchLaunch(context.Background(), item, liveRun, "Scientist Bench node already running")
			return ScientistBenchNodeRun{Case: item, Run: liveRun}, nil
		}
		return ScientistBenchNodeRun{}, fmt.Errorf("node %s has no ready roles to schedule", node.ID)
	}
	roleID := readyRoles[0]
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
		_ = app.postScientistBenchLaunch(context.Background(), item, *liveRun, "Scientist Bench node already running")
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

	prompt := buildScientistBenchWorkerPrompt(item, node, profile, app.RuntimeRegistry, app.RuntimeExecutor, app.Skills)
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
	_ = app.postScientistBenchLaunch(context.Background(), item, updatedRun, "Scientist Bench node started")
	app.launchScientistBenchReadyRoles(item.ID, item, node, updatedRun.Role)
	app.launchScientistBenchPendingConcurrentNodes(item.ID, item)
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
		app.scientistBenchContinuation = make(map[string]int)
	}
	if app.scientistBenchContinuation[caseID] > 0 {
		return false
	}
	app.scientistBenchContinuation[caseID] = 1
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
	if app.ScientistBench != nil {
		if item, err := app.ScientistBench.Get(ctx, strings.TrimSpace(caseID)); err == nil {
			app.launchScientistBenchPendingConcurrentNodes(caseID, item)
		}
	}
	return app.StartScientistBenchActiveNodeRun(ctx, caseID)
}

// launchScientistBenchConcurrentNode schedules a specific node to run in parallel
// with the current ActiveNode. It does not go through the deduplication gate used
// by scheduleScientistBenchContinuation so multiple parallel review nodes can
// execute simultaneously.
func (app *App) launchScientistBenchConcurrentNode(caseID, nodeID string) {
	caseID = strings.TrimSpace(caseID)
	nodeID = strings.TrimSpace(nodeID)
	if caseID == "" || nodeID == "" || app.ScientistBench == nil || app.Orchestrator == nil {
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
		defer logging.RecoverPanic("scientistbench-concurrent-node", nil)

		_, err := app.startScientistBenchNamedNodeRun(ctx, caseID, nodeID)
		if err != nil {
			logging.Warn("Concurrent scientist bench node failed to start", "case_id", caseID, "node_id", nodeID, "error", err)
		}
	}()
}

func (app *App) launchScientistBenchConcurrentRole(caseID, nodeID, roleID string) {
	caseID = strings.TrimSpace(caseID)
	nodeID = strings.TrimSpace(nodeID)
	roleID = strings.TrimSpace(roleID)
	if caseID == "" || nodeID == "" || roleID == "" || app.ScientistBench == nil || app.Orchestrator == nil {
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
		defer logging.RecoverPanic("scientistbench-concurrent-role", nil)

		_, err := app.startScientistBenchSpecificRoleRun(ctx, caseID, nodeID, roleID, true)
		if err != nil {
			logging.Warn("Concurrent scientist bench role failed to start", "case_id", caseID, "node_id", nodeID, "role_id", roleID, "error", err)
		}
	}()
}

// startScientistBenchNamedNodeRun is like StartScientistBenchActiveNodeRun but
// targets a specific node by ID rather than reading GraphState.ActiveNode.
// This is used to launch concurrent (parallel) review nodes.
func (app *App) startScientistBenchNamedNodeRun(ctx context.Context, caseID, nodeID string) (ScientistBenchNodeRun, error) {
	if app.Orchestrator == nil {
		return ScientistBenchNodeRun{}, fmt.Errorf("orchestrator service is not configured")
	}
	node, ok := app.Orchestrator.GetNode(nodeID)
	if !ok {
		return ScientistBenchNodeRun{}, fmt.Errorf("node %s is not registered", nodeID)
	}
	roleID := orchestrator.FirstAssignedRole(node)
	if roleID == "" {
		return ScientistBenchNodeRun{}, fmt.Errorf("node %s has no assigned roles", nodeID)
	}
	return app.startScientistBenchSpecificRoleRun(ctx, caseID, nodeID, roleID, true)
}

func (app *App) startScientistBenchSpecificRoleRun(
	ctx context.Context,
	caseID string,
	nodeID string,
	roleID string,
	parallel bool,
) (ScientistBenchNodeRun, error) {
	if app.ScientistBench == nil || app.Orchestrator == nil || app.Sessions == nil || app.Messages == nil {
		return ScientistBenchNodeRun{}, fmt.Errorf("required services are not configured")
	}

	item, err := app.ScientistBench.Get(ctx, strings.TrimSpace(caseID))
	if err != nil {
		return ScientistBenchNodeRun{}, err
	}
	if scientistBenchCaseIsTerminal(item) {
		return ScientistBenchNodeRun{}, fmt.Errorf("case %s is terminal", item.ID)
	}
	item, err = app.ensureScientistBenchRootSession(ctx, item)
	if err != nil {
		return ScientistBenchNodeRun{}, err
	}
	if exhausted, exhaustedItem, err := app.enforceScientistBenchBudget(ctx, item); err != nil {
		return ScientistBenchNodeRun{}, err
	} else if exhausted {
		return ScientistBenchNodeRun{}, fmt.Errorf("case %s is terminal with status %s", exhaustedItem.ID, exhaustedItem.Status)
	}

	node, ok := app.Orchestrator.GetNode(nodeID)
	if !ok {
		return ScientistBenchNodeRun{}, fmt.Errorf("node %s is not registered", nodeID)
	}
	profile, ok := app.Orchestrator.WorkerProfileForRole(roleID)
	if !ok {
		return ScientistBenchNodeRun{}, fmt.Errorf("worker profile for role %s is not registered", roleID)
	}
	if hasSuccessfulNodeRoleRun(item, node.ID, roleID) {
		return ScientistBenchNodeRun{}, fmt.Errorf("role %s already completed for node %s", roleID, node.ID)
	}
	if liveRun, ok := latestRecoverableNodeRoleRun(item, node.ID, roleID); ok && shouldKeepScientistBenchRunAlive(app.TaskRuns, liveRun, time.Now()) {
		_ = app.postScientistBenchLaunch(context.Background(), item, liveRun, "Scientist Bench node already running")
		return ScientistBenchNodeRun{Case: item, Run: liveRun}, nil
	}

	taskSessionID := "sbtask-" + uuid.NewString()
	taskTitle := strings.TrimSpace(profile.SessionLabel)
	if taskTitle == "" {
		taskTitle = "Scientist Bench Worker"
	}
	if parallel {
		taskTitle = fmt.Sprintf("%s: %s (parallel)", taskTitle, node.ID)
	} else {
		taskTitle = fmt.Sprintf("%s: %s", taskTitle, node.ID)
	}

	taskSession, err := app.Sessions.CreateTaskSession(ctx, taskSessionID, item.RootSessionID, taskTitle)
	if err != nil {
		return ScientistBenchNodeRun{}, fmt.Errorf("create task session: %w", err)
	}
	if app.shouldAutoApproveScientistBenchRoot(item.RootSessionID) {
		app.Permissions.AutoApproveSession(taskSession.ID)
	}

	prompt := buildScientistBenchWorkerPrompt(item, node, profile, app.RuntimeRegistry, app.RuntimeExecutor, app.Skills)
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

	if parallel {
		_ = app.postScientistBenchLaunch(context.Background(), item, run, "Scientist Bench parallel node started")
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
	return ScientistBenchNodeRun{Case: item, Run: updatedRun}, nil
}

func sliceContains(slice []string, value string) bool {
	for _, v := range slice {
		if v == value {
			return true
		}
	}
	return false
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
		_, _ = app.ScientistBench.MutateCase(context.Background(), caseID, func(item *scientistbench.Case) error {
			item.Runs = upsertScientistBenchRunLocal(item.Runs, run)
			return nil
		})
	}()

	result, ok := <-done
	if !ok {
		result = agent.AgentEvent{
			Error: fmt.Errorf("role worker event channel closed without result"),
		}
	}
	content := scientistBenchEventContent(result)
	parsed := orchestrator.WorkerOutput{}
	outcomeErr := result.Error
	if outcomeErr == nil {
		var parseErr error
		parsed, parseErr = orchestrator.StrictParseWorkerOutput(content)
		if parseErr == nil {
			parsed = app.reconcileScientistBenchRuntimeExecution(context.Background(), run, node, parsed)
			parseErr = orchestrator.ValidateWorkerOutputForRole(node, run.Role, parsed)
		}
		if parseErr != nil {
			outcomeErr = parseErr
		}
	}

	item, savedRun, shouldContinue, err := app.persistScientistBenchNodeRunOutcome(
		context.Background(),
		caseID,
		run,
		node,
		parsed,
		content,
		outcomeErr,
	)
	if err != nil {
		return
	}
	_ = app.postScientistBenchNodeResult(context.Background(), item, savedRun)
	if scientistBenchCaseIsTerminal(item) {
		return
	}
	if shouldContinue {
		app.scheduleScientistBenchContinuation(caseID)
	}
	if activeNode, ok := app.Orchestrator.GetNode(item.GraphState.ActiveNode); ok {
		app.launchScientistBenchReadyRoles(caseID, item, activeNode, savedRun.Role)
	}
	app.launchScientistBenchPendingConcurrentNodes(caseID, item)
}

func (app *App) persistScientistBenchNodeRunOutcome(
	ctx context.Context,
	caseID string,
	run scientistbench.RunRecord,
	node orchestrator.NodeSpec,
	parsed orchestrator.WorkerOutput,
	content string,
	outcomeErr error,
) (scientistbench.Case, scientistbench.RunRecord, bool, error) {
	var (
		savedRun       scientistbench.RunRecord
		shouldContinue bool
	)

	item, err := app.ScientistBench.MutateCase(ctx, caseID, func(item *scientistbench.Case) error {
		now := time.Now().Unix()
		if currentRun, ok := findScientistBenchRun(*item, run.ID); ok {
			if len(run.ToolCalls) == 0 {
				run.ToolCalls = append([]string(nil), currentRun.ToolCalls...)
			}
			if strings.TrimSpace(run.TaskRunStatus) == "" {
				run.TaskRunStatus = currentRun.TaskRunStatus
			}
		}

		run.Status = "complete"
		run.TaskRunStatus = "complete"
		run.FinishedAt = now
		run.Error = ""
		run.OutputSummary = ""
		run.SignalsEmitted = nil

		if outcomeErr != nil {
			run.Status = "failed"
			run.TaskRunStatus = firstNonEmpty(run.TaskRunStatus, "failed")
			run.Error = outcomeErr.Error()
		} else {
			run.OutputSummary = truncateScientistBenchText(parsed.Summary, maxScientistBenchSummaryLen)
			if strings.TrimSpace(run.OutputSummary) == "" {
				run.OutputSummary = "Completed"
			}
			run.SignalsEmitted = normalizeRunSignals(parsed, node)
		}
		item.Runs = upsertScientistBenchRunLocal(item.Runs, run)

		if outcomeErr == nil && strings.TrimSpace(content) != "" {
			item.Artifacts = append(item.Artifacts, scientistBenchArtifactsForOutput(run, node, parsed, now)...)
			*item = applyScientistBenchWorkerOutput(*item, run, parsed)
			*item = applyScientistBenchReviewOutput(*item, run, parsed)
			*item = refreshScientistBenchMetrics(*item)
		}

		if outcomeErr != nil {
			updated, applyErr := app.Orchestrator.ApplySignal(*item, node.FailureSignal)
			if applyErr != nil {
				return applyErr
			}
			*item = updated
			shouldContinue = !scientistBenchCaseIsTerminal(*item)
			savedRun = run
			return nil
		}

		readyRoles := readyRolesForNode(*item, node)
		if len(readyRoles) > 0 {
			item.GraphState.ActiveRole = readyRoles[0]
			shouldContinue = true
			savedRun = run
			return nil
		}
		if !allAssignedRolesCompleted(*item, node) {
			if liveRole := firstLiveNodeRoleForNode(*item, node.ID); liveRole != "" {
				item.GraphState.ActiveRole = liveRole
			}
			savedRun = run
			return nil
		}

		prevActiveNode := item.GraphState.ActiveNode
		signal := determineScientistBenchNodeSignal(*item, node, parsed)
		updated, applyErr := app.Orchestrator.ApplySignal(*item, signal)
		if applyErr != nil {
			return applyErr
		}
		*item = updated
		if item.GraphState.ActiveNode != prevActiveNode {
			shouldContinue = true
		}
		savedRun = run
		return nil
	})
	if err != nil {
		return scientistbench.Case{}, scientistbench.RunRecord{}, false, err
	}
	if currentRun, ok := findScientistBenchRun(item, run.ID); ok {
		savedRun = currentRun
	}
	if !shouldContinue && !scientistBenchCaseIsTerminal(item) {
		if activeNode, ok := app.Orchestrator.GetNode(item.GraphState.ActiveNode); ok && len(readyRolesForNode(item, activeNode)) > 0 {
			shouldContinue = true
		}
	}
	return item, savedRun, shouldContinue, nil
}

func determineScientistBenchNodeSignal(
	item scientistbench.Case,
	node orchestrator.NodeSpec,
	parsed orchestrator.WorkerOutput,
) string {
	signal := node.SuccessSignal
	if parsed.FailureSignal == node.FailureSignal {
		signal = node.FailureSignal
	} else if parsed.SuccessSignal == node.SuccessSignal {
		signal = node.SuccessSignal
	} else if strings.EqualFold(parsed.Status, "failed") {
		signal = node.FailureSignal
	} else if node.ID == "node-aggregate" {
		signal = scientistBenchAggregateSignal(item)
	} else if parsed.SuccessSignal != "" {
		if _, isDynamic := node.DynamicRoutes[parsed.SuccessSignal]; isDynamic {
			signal = parsed.SuccessSignal
		}
	}
	return signal
}

func scientistBenchArtifactsForOutput(
	run scientistbench.RunRecord,
	node orchestrator.NodeSpec,
	parsed orchestrator.WorkerOutput,
	createdAt int64,
) []scientistbench.Artifact {
	artifacts := make([]scientistbench.Artifact, 0, 3)
	artifact := scientistbench.Artifact{
		ID:            "artifact-" + uuid.NewString(),
		Kind:          artifactKindForNode(node),
		Label:         artifactLabelForNode(node),
		ProducerRole:  run.Role,
		ProducerRunID: run.ID,
		CreatedAt:     createdAt,
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
	applyScientistBenchReferenceMetadata(artifact.Metadata, parsed.References)
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
	artifacts = append(artifacts, artifact)

	if parsed.Execution == nil {
		return artifacts
	}
	if len(parsed.Execution.LogHighlights) > 0 || strings.TrimSpace(parsed.Execution.VerificationSummary) != "" {
		artifacts = append(artifacts, scientistbench.Artifact{
			ID:            "artifact-" + uuid.NewString(),
			Kind:          scientistbench.ArtifactDockerLog,
			Label:         "Runtime Log",
			ProducerRole:  run.Role,
			ProducerRunID: run.ID,
			CreatedAt:     createdAt,
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
		})
	}
	if len(parsed.Execution.OutputFiles) > 0 {
		artifacts = append(artifacts, scientistbench.Artifact{
			ID:            "artifact-" + uuid.NewString(),
			Kind:          scientistbench.ArtifactResultBundle,
			Label:         "Execution Outputs",
			ProducerRole:  run.Role,
			ProducerRunID: run.ID,
			CreatedAt:     createdAt,
			Metadata: map[string]string{
				"runtime_id":   strings.TrimSpace(parsed.Execution.RuntimeID),
				"output_files": strings.Join(parsed.Execution.OutputFiles, " | "),
				"commands":     strings.Join(parsed.Execution.Commands, " | "),
			},
		})
	}
	return artifacts
}

func applyScientistBenchReferenceMetadata(metadata map[string]string, refs []orchestrator.ReferencePayload) {
	if metadata == nil || len(refs) == 0 {
		return
	}
	payload, err := json.Marshal(refs)
	if err == nil {
		metadata["references_json"] = string(payload)
	}

	formatted := make([]string, 0, len(refs))
	keys := make([]string, 0, len(refs))
	bibtex := make([]string, 0, len(refs))
	citations := make([]string, 0, len(refs))
	for _, ref := range refs {
		if line := strings.TrimSpace(ref.FormattedReference); line != "" {
			formatted = append(formatted, line)
			citations = append(citations, line)
		} else if line := strings.TrimSpace(scientistBenchReferenceDisplay(ref)); line != "" {
			citations = append(citations, line)
		}
		if key := strings.TrimSpace(ref.Key); key != "" {
			keys = append(keys, key)
		}
		if entry := strings.TrimSpace(ref.BibTeX); entry != "" {
			bibtex = append(bibtex, entry)
		}
	}
	if len(formatted) > 0 {
		metadata["formatted_references"] = strings.Join(formatted, "\n")
	}
	if len(keys) > 0 {
		metadata["citation_keys"] = strings.Join(keys, " | ")
	}
	if len(bibtex) > 0 {
		metadata["bibtex_bundle"] = strings.Join(bibtex, "\n\n")
	}
	if strings.TrimSpace(metadata["citations"]) == "" && len(citations) > 0 {
		metadata["citations"] = strings.Join(citations, " | ")
	}
	metadata["reference_count"] = fmt.Sprintf("%d", len(refs))
}

func scientistBenchArtifactReferences(artifact scientistbench.Artifact) []orchestrator.ReferencePayload {
	raw := strings.TrimSpace(artifact.Metadata["references_json"])
	if raw == "" {
		return nil
	}
	var refs []orchestrator.ReferencePayload
	if err := json.Unmarshal([]byte(raw), &refs); err != nil {
		return nil
	}
	return refs
}

func scientistBenchReferenceDisplay(ref orchestrator.ReferencePayload) string {
	if line := strings.TrimSpace(ref.FormattedReference); line != "" {
		return line
	}
	parts := make([]string, 0, 5)
	if len(ref.Authors) > 0 {
		parts = append(parts, strings.Join(ref.Authors, ", "))
	}
	if ref.Year > 0 {
		parts = append(parts, fmt.Sprintf("%d", ref.Year))
	}
	if title := strings.TrimSpace(ref.Title); title != "" {
		parts = append(parts, title)
	}
	if venue := strings.TrimSpace(ref.Venue); venue != "" {
		parts = append(parts, venue)
	}
	if url := strings.TrimSpace(ref.URL); url != "" {
		parts = append(parts, url)
	}
	return strings.Join(parts, ". ")
}

func appendScientistBenchRoleContractOverride(b *strings.Builder, node orchestrator.NodeSpec, profile orchestrator.WorkerProfile) {
	if b == nil {
		return
	}
	switch {
	case profile.RoleID == "research_agent" && node.ID == "node-corpus-retrieval":
		b.WriteString("- OVERRIDE: references[] is now the canonical bibliography payload. Do not rely on free-form citations[] alone.\n")
		b.WriteString("- Provide at least 3 normalized references[]. Each reference must include key, title, authors[], year, formatted_reference, bibtex, and at least one of doi/url/zotero_key.\n")
		b.WriteString("- Preferred workflow: discover candidate papers, then use Zotero MCP tools if available to search the library, add missing papers by DOI/URL, and export normalized metadata plus BibTeX.\n")
		b.WriteString("- citations[] is optional human-readable support text. references[] is mandatory for successful research output.\n")
	case profile.RoleID == "paper_writer":
		b.WriteString("- OVERRIDE: use the normalized reference bundle from context as the only citation source of truth.\n")
		b.WriteString("- Create references.bib from the provided BibTeX bundle before drafting the paper.\n")
		b.WriteString("- Every \\cite{key} must use a key from the normalized reference bundle. Do not invent keys or cite uncatalogued papers.\n")
	}
}

func appendScientistBenchConvergenceSection(b *strings.Builder, node orchestrator.NodeSpec, profile orchestrator.WorkerProfile) {
	if b == nil {
		return
	}

	policy := profile.Convergence
	if policy.CompletionMode == "" {
		return
	}

	fmt.Fprintf(
		b,
		"<scicli_execution_policy step_budget=\"%d\" completion_mode=\"%s\" terminal_json_key=\"%s\" strategy=\"%s\" node=\"%s\" role=\"%s\" />\n\n",
		policy.StepBudget,
		policy.CompletionMode,
		policy.TerminalJSONKey,
		policy.Strategy,
		node.ID,
		profile.RoleID,
	)

	b.WriteString("Convergence strategy.\n")
	if policy.StepBudget > 0 {
		fmt.Fprintf(b, "- Autonomous turn budget: %d.\n", policy.StepBudget)
	}
	b.WriteString("- Do not emit <agent_loop_status> tags for this workflow.\n")
	b.WriteString("- Intermediate turns may be brief progress notes or tool actions.\n")
	b.WriteString("- As soon as your role contract is satisfied, return the terminal JSON object only.\n")
	b.WriteString("- If you reach the final allowed turn with unresolved gaps, return best-effort JSON with status=\"failed\" and summarize the blocker.\n")
	for _, bullet := range scientistBenchConvergenceBullets(node, profile) {
		fmt.Fprintf(b, "- %s\n", bullet)
	}
	b.WriteString("\n")
}

func scientistBenchConvergenceBullets(node orchestrator.NodeSpec, profile orchestrator.WorkerProfile) []string {
	switch profile.RoleID {
	case "research_agent":
		return []string{
			"Stop once references[] and evidence_summary[] meet the minimum contract and the key gap, evaluation setup, and risks are covered.",
			"Prefer breadth first and depth on the top sources; do not keep searching just to make the bibliography longer.",
		}
	case "idea_maker":
		return []string{
			"Stop when you have 2-4 distinct, testable ideas with concrete hypotheses, novelty claims, experiments, and falsification conditions.",
			"Do not self-debate indefinitely once the idea set is diverse and specific.",
		}
	case "idea_hater":
		return []string{
			"Stop after every candidate idea has a concrete objection or conditional acceptance grounded in evidence.",
			"Focus on decisive objections, not stylistic rewriting.",
		}
	case "method_planner":
		return []string{
			"Stop when the method_plan is implementable without clarifying questions and the acceptance checks are machine-verifiable.",
			"Do not keep polishing prose once the plan is executable.",
		}
	case "code_agent":
		return []string{
			"Stop after you have a minimal patch, reproducible run instructions, and at least one smoke verification or a precise blocker.",
			"Do not continue into unrelated refactors after the planned method is implemented.",
		}
	case "execution_agent":
		return []string{
			"Run a smoke test first, then the main command if warranted, and attempt at most one targeted fix before finalizing.",
			"Stop once execution evidence, verification status, and blockers are captured clearly.",
		}
	case "figure_agent":
		return []string{
			"Stop once the required figures and manifest are produced or a concrete generation blocker is documented.",
			"Do not keep regenerating variants after you have a readable figure set.",
		}
	case "paper_writer":
		return []string{
			"Stop once the draft is structurally complete, cites only the provided reference bundle, and addresses current revision feedback.",
			"Do not keep rewriting for tone once all required sections, figures, and tables are in place.",
		}
	case "advisor_agent":
		return []string{
			"Stop once PASS/FAIL coverage, major deviations, and severity-ranked findings are complete.",
		}
	case "judge_agent":
		return []string{
			"Stop once overall_score, justification, and top issues are set from the advisor report.",
		}
	case "domain_expert_reviewer":
		return []string{
			"Stop once the review payload contains calibrated scores, strengths, weaknesses, questions, and a clear decision.",
		}
	case "paper_comparison_reviewer":
		return []string{
			"Stop once all alignment dimensions and the overall quality gap are scored with evidence.",
		}
	case "chief_scientist":
		switch node.ID {
		case "node-case-intake":
			return []string{"Stop once the case is validated and the node routing decision is explicit."}
		case "node-idea-gate":
			return []string{
				"Stop once you choose one route: idea_gate_passed, idea_gate_rejected, or a valid dynamic retry signal.",
				"Do not reopen ideation yourself once the available ideas and objections are sufficient to decide.",
			}
		case "node-revision-gate":
			return []string{
				"Stop once you compute the review aggregate and emit revision_decision plus revision_feedback when revising.",
				"Do not reopen evidence gathering or redraft content at this gate.",
			}
		case "node-aggregate":
			return []string{"Stop once the final case decision is synthesized from the accumulated artifacts and reviews."}
		}
	}
	return []string{
		"Stop once the assigned node contract is satisfied and the final JSON is machine-readable.",
	}
}

func appendScientistBenchSkillRecommendations(
	b *strings.Builder,
	item scientistbench.Case,
	node orchestrator.NodeSpec,
	profile orchestrator.WorkerProfile,
	skillsSvc skills.Service,
) {
	if b == nil || skillsSvc == nil {
		return
	}

	recommended := scientistBenchRecommendedSkills(context.Background(), item, node, profile, skillsSvc, 6)
	if len(recommended) == 0 {
		return
	}

	b.WriteString("\nRecommended skills for this worker.\n")
	b.WriteString("- If one matches the task, call activate_skill before using the skill's instructions or files.\n")
	for _, skill := range recommended {
		fmt.Fprintf(b, "- %s: %s\n", skill.ID, skill.Description)
	}
}

func scientistBenchRecommendedSkills(
	ctx context.Context,
	item scientistbench.Case,
	node orchestrator.NodeSpec,
	profile orchestrator.WorkerProfile,
	skillsSvc skills.Service,
	limit int,
) []skills.Skill {
	if skillsSvc == nil {
		return nil
	}
	if limit <= 0 {
		limit = 6
	}

	available, err := skillsSvc.List(ctx)
	if err != nil || len(available) == 0 {
		return nil
	}

	curated := scientistBenchCuratedSkillIDs(node, profile)
	query := scientistBenchSkillRecommendationQuery(item, node, profile)
	dynamic, err := skillsSvc.Recommend(ctx, query, limit)
	if err != nil {
		dynamic = nil
	}

	out := make([]skills.Skill, 0, limit)
	seen := make(map[string]struct{}, limit)
	appendSkill := func(skill skills.Skill) {
		if len(out) >= limit {
			return
		}
		key := strings.ToLower(strings.TrimSpace(skill.ID))
		if key == "" {
			return
		}
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		out = append(out, skill)
	}

	for _, id := range curated {
		for _, skill := range available {
			if strings.EqualFold(skill.ID, id) {
				appendSkill(skill)
				break
			}
		}
	}
	for _, skill := range dynamic {
		appendSkill(skill)
	}
	return out
}

func scientistBenchCuratedSkillIDs(node orchestrator.NodeSpec, profile orchestrator.WorkerProfile) []string {
	ids := []string{}
	switch profile.RoleID {
	case "research_agent":
		ids = append(ids, "citation-management", "research-lookup", "arxiv-database", "biorxiv-database", "bgpt-paper-search")
	case "paper_writer":
		ids = append(ids, "scientific-writing", "venue-templates", "citation-management")
	case "judge_agent", "advisor_agent", "domain_expert_reviewer", "paper_comparison_reviewer", "chief_scientist":
		ids = append(ids, "scholar-evaluation", "scientific-critical-thinking", "research-lookup", "citation-management")
	case "execution_agent":
		ids = append(ids, "scientific-visualization")
	}

	switch node.ID {
	case "node-corpus-retrieval":
		ids = append(ids, "citation-management", "research-lookup")
	case "node-paper-draft":
		ids = append(ids, "scientific-writing", "venue-templates")
	case "node-paper-compare":
		ids = append(ids, "scholar-evaluation", "scientific-critical-thinking")
	}
	return ids
}

func scientistBenchSkillRecommendationQuery(
	item scientistbench.Case,
	node orchestrator.NodeSpec,
	profile orchestrator.WorkerProfile,
) string {
	parts := []string{
		profile.RoleID,
		node.ID,
		node.Stage,
		item.Title,
		item.Inputs.CoreIdea,
		item.Inputs.Dataset.Name,
		item.Inputs.TargetPaper.Title,
	}

	switch profile.RoleID {
	case "research_agent":
		parts = append(parts, "literature review papers citations bibliography doi bibtex zotero academic search")
	case "paper_writer":
		parts = append(parts, "scientific writing manuscript paper draft references citations bibtex venue formatting")
	case "judge_agent", "advisor_agent", "domain_expert_reviewer", "paper_comparison_reviewer", "chief_scientist":
		parts = append(parts, "peer review scholarly evaluation critique paper comparison novelty methodology evidence")
	case "execution_agent":
		parts = append(parts, "reproducibility experiment execution runtime benchmark figures")
	}

	switch node.ID {
	case "node-corpus-retrieval":
		parts = append(parts, "reference discovery citation normalization")
	case "node-paper-draft":
		parts = append(parts, "paper writing introduction methods results discussion bibliography")
	case "node-paper-compare":
		parts = append(parts, "paper comparison reviewer alignment")
	}

	return strings.Join(sanitizeStrings(parts), " ")
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
	skillsSvc skills.Service,
) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(profile.PromptPreamble))
	b.WriteString("\n\n")
	appendScientistBenchConvergenceSection(&b, node, profile)
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
	b.WriteString("- Be concrete and evidence-driven. Never fabricate citations, results, or data.\n")
	b.WriteString("- Intermediate turns may be brief prose or tool work. Only the terminal response is parsed by the orchestrator.\n")
	b.WriteString("- The terminal response must be valid JSON without markdown fences.\n")

	// status rules differ by node type to prevent research/analysis agents from
	// emitting needs_revision (a revision-gate-only concept) which causes
	// confusing repeated incomplete outputs instead of looping to finish the work.
	isRevisionGate := node.ID == "node-revision-gate"
	isReviewNode := node.Type == "review" || node.ID == "node-advisor-review" ||
		node.ID == "node-judge-review" || node.ID == "node-domain-review" ||
		node.ID == "node-paper-compare"

	if isRevisionGate {
		b.WriteString("- status must be \"succeeded\" (paper_quality_acceptable) or \"failed\" (paper_needs_revision).\n")
		b.WriteString("- Set revision_decision to \"accept\" or \"revise\" and populate revision_feedback with specific items when revising.\n")
	} else if isReviewNode {
		b.WriteString("- status must be \"succeeded\" or \"failed\" only. Do NOT use \"needs_revision\".\n")
	} else {
		b.WriteString("- status must be \"succeeded\" or \"failed\" only.\n")
		b.WriteString("- Return the final JSON as soon as your node contract is satisfied. Do not keep looping for cosmetic improvements.\n")
	}

	b.WriteString("- Use this shape: {\"status\":\"succeeded|failed\",\"summary\":\"...\",\"success_signal\":\"...\",\"failure_signal\":\"...\",\"overall_score\":0.0,\"revision_decision\":\"accept|revise\",\"revision_feedback\":[...],\"evidence_summary\":[...],\"citations\":[...],\"references\":[{\"key\":\"smith2024\",\"title\":\"...\",\"authors\":[\"...\"],\"year\":2024,\"venue\":\"...\",\"doi\":\"...\",\"url\":\"...\",\"zotero_key\":\"...\",\"formatted_reference\":\"...\",\"bibtex\":\"@article{...}\",\"key_claim\":\"...\"}],\"risks\":[...],\"ideas\":[...],\"objections\":[...],\"method_plan\":{\"summary\":\"...\",\"pipeline_steps\":[...],\"acceptance_checks\":[...],\"implementation_notes\":[...],\"runtime_hints\":[...]},\"execution\":{\"runtime_id\":\"...\",\"commands\":[...],\"verification_summary\":\"...\",\"verification_passed\":true,\"output_files\":[...],\"log_highlights\":[...]},\"review\":{\"decision\":\"accept|revise|reject\",\"summary\":\"...\",\"strengths\":[...],\"weaknesses\":[...],\"questions\":[...],\"confidence\":0.0,\"readable_paper\":true,\"novel_insight_present\":true,\"code_runs\":true,\"scores\":{\"overall\":0.0,\"idea_quality\":0.0,\"method_soundness\":0.0,\"result_interpretation\":0.0,\"writing_quality\":0.0}},\"comparison\":{\"summary\":\"...\",\"strengths\":[...],\"weaknesses\":[...],\"motivation_alignment\":0.0,\"methodology_alignment\":0.0,\"novelty_alignment\":0.0,\"experimental_alignment\":0.0,\"confidence\":0.0}}\n")
	b.WriteString("- overall_score: set to the mean review score (0-5) when acting as reviewer or revision gate.\n")
	b.WriteString("- Only set success_signal or failure_signal if you are confident it matches the assigned node contract.\n")
	appendScientistBenchRoleContractOverride(&b, node, profile)
	appendScientistBenchSkillRecommendations(&b, item, node, profile, skillsSvc)
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

func latestRecoverableNodeRunForNode(item scientistbench.Case, nodeID string) (scientistbench.RunRecord, bool) {
	for _, run := range item.Runs {
		if run.NodeID != nodeID {
			continue
		}
		if isScientistBenchRunTerminal(run) {
			continue
		}
		return run, true
	}
	return scientistbench.RunRecord{}, false
}

func firstLiveNodeRoleForNode(item scientistbench.Case, nodeID string) string {
	for _, run := range item.Runs {
		if run.NodeID != nodeID {
			continue
		}
		if isScientistBenchRunTerminal(run) {
			continue
		}
		return strings.TrimSpace(run.Role)
	}
	return ""
}

func (app *App) launchScientistBenchReadyRoles(caseID string, item scientistbench.Case, node orchestrator.NodeSpec, excludeRole string) {
	excludeRole = strings.TrimSpace(excludeRole)
	slots := app.scientistBenchAvailableWorkerSlots(item)
	if slots == 0 {
		return
	}
	for _, roleID := range readyRolesForNode(item, node) {
		if roleID == excludeRole {
			continue
		}
		if slots > 0 {
			app.launchScientistBenchConcurrentRole(caseID, node.ID, roleID)
			slots--
		}
	}
}

func (app *App) launchScientistBenchPendingConcurrentNodes(caseID string, item scientistbench.Case) {
	slots := app.scientistBenchAvailableWorkerSlots(item)
	if slots == 0 || app.Orchestrator == nil {
		return
	}
	for _, nodeID := range item.GraphState.ConcurrentNodes {
		if slots == 0 {
			return
		}
		node, ok := app.Orchestrator.GetNode(nodeID)
		if !ok {
			continue
		}
		if allAssignedRolesCompleted(item, node) || firstLiveNodeRoleForNode(item, nodeID) != "" {
			continue
		}
		app.launchScientistBenchConcurrentNode(caseID, nodeID)
		slots--
	}
}

func (app *App) scientistBenchAvailableWorkerSlots(item scientistbench.Case) int {
	maxParallel := item.Budget.MaxParallelWorkers
	if maxParallel <= 0 {
		return 1 << 20
	}
	used := 0
	for _, run := range item.Runs {
		if isScientistBenchRunTerminal(run) {
			continue
		}
		used++
	}
	if used >= maxParallel {
		return 0
	}
	return maxParallel - used
}

func (app *App) enforceScientistBenchBudget(ctx context.Context, item scientistbench.Case) (bool, scientistbench.Case, error) {
	exhausted, reason, err := app.scientistBenchBudgetExceeded(ctx, item)
	if err != nil || !exhausted || app.ScientistBench == nil || strings.TrimSpace(item.ID) == "" {
		return exhausted, item, err
	}
	saved, err := app.ScientistBench.MutateCase(ctx, item.ID, func(caseItem *scientistbench.Case) error {
		if scientistBenchCaseIsTerminal(*caseItem) {
			return nil
		}
		caseItem.Termination = scientistbench.Termination{
			Signal:       scientistbench.SignalBudgetExhausted,
			Reason:       strings.TrimSpace(reason),
			Resolved:     false,
			TerminatedAt: time.Now().Unix(),
		}
		caseItem.Status = scientistbench.StatusNotResolved
		caseItem.GraphState.PendingNodes = nil
		return nil
	})
	return true, saved, err
}

func (app *App) scientistBenchBudgetExceeded(ctx context.Context, item scientistbench.Case) (bool, string, error) {
	if maxMinutes := item.Budget.MaxWallClockMinutes; maxMinutes > 0 && item.CreatedAt > 0 {
		if time.Since(time.Unix(item.CreatedAt, 0)) > time.Duration(maxMinutes)*time.Minute {
			return true, fmt.Sprintf("wall clock budget exceeded (%d minutes)", maxMinutes), nil
		}
	}
	if maxSteps := item.Budget.MaxAgentSteps; maxSteps > 0 && len(item.Runs) >= maxSteps {
		return true, fmt.Sprintf("agent step budget exceeded (%d)", maxSteps), nil
	}
	if maxToolCalls := item.Budget.MaxToolCalls; maxToolCalls > 0 && scientistBenchToolCallCount(item) >= maxToolCalls {
		return true, fmt.Sprintf("tool call budget exceeded (%d)", maxToolCalls), nil
	}
	if maxJudgeRepeats := item.Budget.MaxJudgeRepeats; maxJudgeRepeats > 0 && scientistBenchRoleRunCount(item, "judge_agent") >= maxJudgeRepeats {
		return true, fmt.Sprintf("judge repeat budget exceeded (%d)", maxJudgeRepeats), nil
	}
	if maxCost := item.Budget.MaxCostUSD; maxCost > 0 {
		cost, err := app.scientistBenchSessionTreeCost(ctx, item.RootSessionID, map[string]struct{}{})
		if err != nil {
			return false, "", err
		}
		if cost >= maxCost {
			return true, fmt.Sprintf("cost budget exceeded (%.2f USD)", maxCost), nil
		}
	}
	return false, "", nil
}

func scientistBenchRoleRunCount(item scientistbench.Case, roleID string) int {
	count := 0
	for _, run := range item.Runs {
		if run.Role == roleID {
			count++
		}
	}
	return count
}

func scientistBenchToolCallCount(item scientistbench.Case) int {
	total := 0
	for _, run := range item.Runs {
		total += len(run.ToolCalls)
	}
	return total
}

func (app *App) scientistBenchSessionTreeCost(ctx context.Context, sessionID string, visited map[string]struct{}) (float64, error) {
	if app.Sessions == nil {
		return 0, nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return 0, nil
	}
	if _, ok := visited[sessionID]; ok {
		return 0, nil
	}
	visited[sessionID] = struct{}{}

	sess, err := app.Sessions.Get(ctx, sessionID)
	if err != nil {
		return 0, err
	}
	total := sess.Cost
	children, err := app.Sessions.ListChildren(ctx, sessionID)
	if err != nil {
		return 0, err
	}
	for _, child := range children {
		childCost, childErr := app.scientistBenchSessionTreeCost(ctx, child.ID, visited)
		if childErr != nil {
			return 0, childErr
		}
		total += childCost
	}
	return total, nil
}

func readyRolesForNode(item scientistbench.Case, node orchestrator.NodeSpec) []string {
	ready := make([]string, 0, len(node.AssignedRoles))
	for _, roleID := range node.AssignedRoles {
		roleID = strings.TrimSpace(roleID)
		if roleID == "" {
			continue
		}
		if hasSuccessfulNodeRoleRun(item, node.ID, roleID) || hasLiveNodeRoleRun(item, node.ID, roleID) {
			continue
		}
		if !roleDependenciesSatisfied(item, node, roleID) {
			continue
		}
		ready = append(ready, roleID)
	}
	return ready
}

func roleDependenciesSatisfied(item scientistbench.Case, node orchestrator.NodeSpec, roleID string) bool {
	if len(node.RoleDependencies) == 0 {
		return true
	}
	for _, dependency := range node.RoleDependencies[strings.TrimSpace(roleID)] {
		if !hasSuccessfulNodeRoleRun(item, node.ID, dependency) {
			return false
		}
	}
	return true
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

		item, concurrentChanged, err := app.reconcileScientistBenchConcurrentSignals(item)
		if err != nil {
			return scientistbench.Case{}, changed, err
		}
		if concurrentChanged {
			changed = true
			continue
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
		before := item
		updated, applyErr := app.Orchestrator.ApplySignal(item, signal)
		if applyErr != nil {
			return scientistbench.Case{}, changed, applyErr
		}
		if scientistBenchProgressEqual(before, updated) {
			return item, changed, nil
		}
		item = updated
		changed = true
		return item, changed, nil
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
	ready := readyRolesForNode(item, node)
	if len(ready) > 0 {
		return ready[0], nil
	}
	if !allAssignedRolesCompleted(item, node) {
		return "", fmt.Errorf("node %s is waiting on running or blocked roles", node.ID)
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

func hasLiveNodeRoleRun(item scientistbench.Case, nodeID string, roleID string) bool {
	for _, run := range item.Runs {
		if run.NodeID != nodeID || run.Role != roleID {
			continue
		}
		if isScientistBenchRunTerminal(run) {
			continue
		}
		return true
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
			default:
				if _, ok := node.DynamicRoutes[strings.TrimSpace(signal)]; ok {
					return strings.TrimSpace(signal)
				}
			}
		}
	}
	if node.SuccessSignal != "" && allAssignedRolesCompleted(item, node) {
		return node.SuccessSignal
	}
	return ""
}

func (app *App) reconcileScientistBenchConcurrentSignals(item scientistbench.Case) (scientistbench.Case, bool, error) {
	if app.Orchestrator == nil || len(item.GraphState.ConcurrentNodes) == 0 {
		return item, false, nil
	}

	changed := false
	for i := 0; i < 32; i++ {
		for _, nodeID := range item.GraphState.ConcurrentNodes {
			node, ok := app.Orchestrator.GetNode(nodeID)
			if !ok {
				continue
			}
			signal := scientistBenchRecoveredNodeSignal(item, node)
			if signal == "" {
				continue
			}
			before := item
			updated, err := app.Orchestrator.ApplySignal(item, signal)
			if err != nil {
				return scientistbench.Case{}, changed, err
			}
			if scientistBenchProgressEqual(before, updated) {
				return item, changed, nil
			}
			item = updated
			changed = true
			return item, changed, nil
		}
		return item, changed, nil
	}
	return scientistbench.Case{}, changed, fmt.Errorf("scientist bench concurrent reconciliation exceeded safety limit")
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

func scientistBenchProgressEqual(a scientistbench.Case, b scientistbench.Case) bool {
	return a.Status == b.Status &&
		reflect.DeepEqual(a.GraphState, b.GraphState) &&
		reflect.DeepEqual(a.Termination, b.Termination)
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
		if run.NodeID == "node-revision-gate" && len(output.RevisionFeedback) > 0 {
			// Store the gate's distilled feedback as a Review so paper_writer
			// context injection picks it up during the next revision round.
			item.Reviews = append(item.Reviews, scientistbench.Review{
				ID:           "review-gate-" + uuid.NewString(),
				Type:         scientistbench.ReviewAdvisor,
				ReviewerRole: "revision_gate",
				Decision:     output.RevisionDecision,
				Summary:      output.Summary,
				Weaknesses:   output.RevisionFeedback,
				Scores: scientistbench.ReviewScores{
					Overall: output.OverallScore,
				},
				CreatedAt: time.Now().Unix(),
			})
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
	if output.SuccessSignal != "" {
		if _, ok := node.DynamicRoutes[output.SuccessSignal]; ok {
			signals = append(signals, output.SuccessSignal)
		}
	}
	if output.FailureSignal == node.FailureSignal && output.FailureSignal != "" {
		signals = append(signals, output.FailureSignal)
	}
	return sanitizeStrings(signals)
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
	case "node-domain-review", "node-paper-compare", "node-revision-gate":
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
	case "node-revision-gate":
		return "Revision Gate Decision"
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

	// Chief scientist always gets a full pipeline status summary so it knows
	// exactly what has been completed and what is missing.
	isCS := profile.RoleID == "chief_scientist"
	if isCS {
		b.WriteString("\nPipeline status (completed stages).\n")
		if len(item.GraphState.CompletedNodes) == 0 {
			b.WriteString("  (no stages completed yet)\n")
		} else {
			for _, n := range item.GraphState.CompletedNodes {
				fmt.Fprintf(b, "  - %s\n", n)
			}
		}
		if len(item.GraphState.BlockedNodes) > 0 {
			b.WriteString("Blocked stages: " + strings.Join(item.GraphState.BlockedNodes, ", ") + "\n")
		}
		b.WriteString("\nDynamic routing signals you can emit (besides the node success/failure signals):\n")
		for sig, target := range node.DynamicRoutes {
			retries := 0
			if item.GraphState.StageRetries != nil {
				retries = item.GraphState.StageRetries[node.ID]
			}
			max := node.MaxDynamicRetries
			if max <= 0 {
				max = 2
			}
			fmt.Fprintf(b, "  - \"%s\" -> routes back to %s (used %d/%d times)\n", sig, target, retries, max)
		}
		b.WriteString("Emit a dynamic signal by setting success_signal to one of the above keys.\n")
		b.WriteString("If evidence or context is thin but workable, proceed rather than requesting a retry.\n")
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
		if refs := scientistBenchArtifactReferences(*evidence); len(refs) > 0 {
			fmt.Fprintf(b, "Normalized references: %d\n", len(refs))
			limit := len(refs)
			if limit > 12 {
				limit = 12
			}
			for i := 0; i < limit; i++ {
				ref := refs[i]
				fmt.Fprintf(b, "- [%s] %s\n", firstNonEmpty(ref.Key, ref.ZoteroKey, fmt.Sprintf("ref-%d", i+1)), scientistBenchReferenceDisplay(ref))
				if claim := strings.TrimSpace(ref.KeyClaim); claim != "" {
					fmt.Fprintf(b, "  Key claim: %s\n", claim)
				}
			}
		}
		if keys := strings.TrimSpace(evidence.Metadata["citation_keys"]); keys != "" {
			fmt.Fprintf(b, "Citation keys: %s\n", keys)
		}
		if (profile.RoleID == "paper_writer" || node.ID == "node-paper-draft") && strings.TrimSpace(evidence.Metadata["bibtex_bundle"]) != "" {
			b.WriteString("BibTeX bundle:\n")
			b.WriteString(strings.TrimSpace(evidence.Metadata["bibtex_bundle"]))
			b.WriteString("\n")
		}
	} else if isCS || node.ID == "node-idea-gate" {
		// No evidence artifact found - tell CS so it can make a decision.
		b.WriteString("\nEvidence pack status: NO evidence artifacts found from research stages.\n")
		b.WriteString("You may emit \"research_insufficient\" to request a research retry,\n")
		b.WriteString("OR proceed using the core_idea and constraints as the basis for ideation.\n")
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

	// Revision gate context: show all review details + decision logic
	if node.ID == "node-revision-gate" {
		maxRevisions := item.GraphState.MaxRevisions
		if maxRevisions <= 0 {
			maxRevisions = 2
		}
		fmt.Fprintf(b, "\nRevision gate context.\n")
		fmt.Fprintf(b, "Current revision round: %d / max %d\n", item.GraphState.RevisionRound, maxRevisions)
		b.WriteString("Decision rule: compute the mean of all review overall_scores.\n")
		b.WriteString("  If mean >= 3.5: emit paper_quality_acceptable (advance to aggregate).\n")
		b.WriteString("  If mean < 3.5 AND rounds remain: emit paper_needs_revision.\n")
		b.WriteString("    Include revision_feedback: a list of specific, actionable items\n")
		b.WriteString("    that address the most critical weaknesses across all reviewers.\n")
		b.WriteString("All reviewer feedback for this decision:\n")
		for _, review := range item.Reviews {
			fmt.Fprintf(b, "  Reviewer: %s | Score: %.1f | Decision: %s\n",
				review.ReviewerRole, review.Scores.Overall, review.Decision)
			for _, w := range review.Weaknesses {
				fmt.Fprintf(b, "    Weakness: %s\n", w)
			}
			for _, q := range review.Questions {
				fmt.Fprintf(b, "    Question: %s\n", q)
			}
		}
	}

	// Revision feedback injection: paper_writer on rounds > 0 sees all prior weaknesses
	if profile.RoleID == "paper_writer" && item.GraphState.RevisionRound > 0 {
		fmt.Fprintf(b, "\nRevision round %d - you MUST address all feedback below.\n", item.GraphState.RevisionRound)
		b.WriteString("Begin the paper with a comment block: %% Changes in revision N: ...\n")
		b.WriteString("Do not reduce quality in sections not mentioned in the feedback.\n")
		anyFeedback := false
		for _, review := range item.Reviews {
			if len(review.Weaknesses) == 0 && len(review.Questions) == 0 {
				continue
			}
			anyFeedback = true
			fmt.Fprintf(b, "\nFeedback from %s reviewer (score %.1f/5):\n",
				review.ReviewerRole, review.Scores.Overall)
			for _, w := range review.Weaknesses {
				fmt.Fprintf(b, "  [weakness] %s\n", w)
			}
			for _, q := range review.Questions {
				fmt.Fprintf(b, "  [question] %s\n", q)
			}
		}
		if !anyFeedback {
			b.WriteString("  (No specific feedback recorded - improve overall depth and rigor.)\n")
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
	// Iterate newest-first (artifacts are appended in creation order, so reverse).
	for i := len(item.Artifacts) - 1; i >= 0; i-- {
		artifact := item.Artifacts[i]
		if artifact.Kind != scientistbench.ArtifactCitation {
			continue
		}
		if strings.TrimSpace(artifact.Metadata["citations"]) == "" &&
			strings.TrimSpace(artifact.Metadata["evidence"]) == "" &&
			strings.TrimSpace(artifact.Metadata["references_json"]) == "" &&
			strings.TrimSpace(artifact.Metadata["bibtex_bundle"]) == "" {
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
