package app

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SciMate-AI/scicli/internal/llm/agent"
	"github.com/SciMate-AI/scicli/internal/message"
	"github.com/SciMate-AI/scicli/internal/orchestrator"
	"github.com/SciMate-AI/scicli/internal/pubsub"
	runtimex "github.com/SciMate-AI/scicli/internal/runtime"
	"github.com/SciMate-AI/scicli/internal/scientistbench"
	"github.com/SciMate-AI/scicli/internal/session"
	"github.com/SciMate-AI/scicli/internal/taskrun"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubRuntimeRunner struct {
	result runtimex.RunResult
}

func (s stubRuntimeRunner) RunPlan(context.Context, runtimex.Plan, runtimex.RunRequest) runtimex.RunResult {
	return s.result
}

func TestNextPendingRoleForNode(t *testing.T) {
	node := orchestrator.NodeSpec{
		ID:            "node-idea-gate",
		AssignedRoles: []string{"idea_maker", "idea_hater", "chief_scientist"},
	}
	item := scientistbench.Case{
		Runs: []scientistbench.RunRecord{
			{NodeID: "node-idea-gate", Role: "idea_maker", Status: "complete"},
		},
	}

	role, err := nextPendingRoleForNode(item, node)
	require.NoError(t, err)
	assert.Equal(t, "idea_hater", role)
}

func TestApplyScientistBenchWorkerOutputUpdatesIdeaModule(t *testing.T) {
	item := scientistbench.Case{}
	item = applyScientistBenchWorkerOutput(item, scientistbench.RunRecord{Role: "idea_maker"}, orchestrator.WorkerOutput{
		Ideas: []orchestrator.IdeaPayload{
			{
				Title:        "Idea A",
				Summary:      "Promising path",
				NoveltyClaim: "Combines two priors",
				Hypotheses:   []string{"h1"},
			},
		},
	})
	assert.Equal(t, "drafted", item.IdeaModule.Status)
	require.Len(t, item.IdeaModule.AcceptedIdeas, 1)
	assert.Equal(t, "Idea A", item.IdeaModule.AcceptedIdeas[0].Title)

	item = applyScientistBenchWorkerOutput(item, scientistbench.RunRecord{Role: "idea_hater"}, orchestrator.WorkerOutput{
		Objections: []orchestrator.ObjectionPayload{
			{
				IdeaTitle: "Idea A",
				Summary:   "Novelty is weak",
				Severity:  "high",
			},
		},
	})
	assert.Equal(t, "criticized", item.IdeaModule.Status)
	require.Len(t, item.IdeaModule.Objections, 1)
	assert.Equal(t, "Novelty is weak", item.IdeaModule.Objections[0].Summary)
}

func TestApplyScientistBenchWorkerOutputChiefScientistRejectsIdeas(t *testing.T) {
	item := scientistbench.Case{
		IdeaModule: scientistbench.IdeaModule{
			AcceptedIdeas: []scientistbench.IdeaCandidate{
				{Title: "Idea A", Summary: "Promising"},
			},
		},
	}

	item = applyScientistBenchWorkerOutput(item, scientistbench.RunRecord{Role: "chief_scientist", NodeID: "node-idea-gate"}, orchestrator.WorkerOutput{
		Status:        "failed",
		FailureSignal: "idea_gate_rejected",
	})
	assert.Equal(t, "rejected", item.IdeaModule.Status)
	assert.Empty(t, item.IdeaModule.AcceptedIdeas)
	require.Len(t, item.IdeaModule.RejectedIdeas, 1)
}

func TestBuildScientistBenchWorkerPromptIncludesEvidenceAndIdeas(t *testing.T) {
	item := scientistbench.Case{
		ID:    "case-1",
		Mode:  scientistbench.ModeJoint,
		Level: scientistbench.Level1,
		Artifacts: []scientistbench.Artifact{
			{
				Kind: scientistbench.ArtifactCitation,
				Metadata: map[string]string{
					"summary":   "Found 18 relevant papers",
					"citations": "ref-1 | ref-2",
					"evidence":  "paper A introduces the base pipeline",
				},
			},
		},
		IdeaModule: scientistbench.IdeaModule{
			AcceptedIdeas: []scientistbench.IdeaCandidate{
				{Title: "Idea A", Summary: "Use a hybrid encoder", NoveltyClaim: "Combines two priors"},
			},
			Objections: []scientistbench.IdeaObjection{
				{Summary: "Novelty may be weak", Severity: "high"},
			},
		},
	}
	node := orchestrator.NodeSpec{ID: "node-method-plan", Stage: "method_planning"}
	profile := orchestrator.WorkerProfile{RoleID: "method_planner", PromptPreamble: "Method planner preamble"}

	prompt := buildScientistBenchWorkerPrompt(item, node, profile, nil, nil)
	assert.True(t, strings.Contains(prompt, "Found 18 relevant papers"))
	assert.True(t, strings.Contains(prompt, "Idea A"))
	assert.True(t, strings.Contains(prompt, "Novelty may be weak"))
}

func TestBuildScientistBenchWorkerPromptIncludesMethodPlan(t *testing.T) {
	item := scientistbench.Case{
		Artifacts: []scientistbench.Artifact{
			{
				Metadata: map[string]string{
					"method_summary":       "Train a hybrid encoder then refine with a ranking loss",
					"method_pipeline":      "encode data | optimize ranking loss | evaluate retrieval",
					"acceptance_checks":    "script runs | metrics produced",
					"implementation_notes": "start from baseline trainer",
					"runtime_hints":        "use small batch smoke test first",
				},
			},
		},
	}
	node := orchestrator.NodeSpec{ID: "node-implementation", Stage: "implementation"}
	profile := orchestrator.WorkerProfile{RoleID: "code_agent", PromptPreamble: "Code agent preamble"}

	prompt := buildScientistBenchWorkerPrompt(item, node, profile, nil, nil)
	assert.True(t, strings.Contains(prompt, "Train a hybrid encoder"))
	assert.True(t, strings.Contains(prompt, "encode data | optimize ranking loss"))
	assert.True(t, strings.Contains(prompt, "use small batch smoke test first"))
}

func TestBuildScientistBenchWorkerPromptIncludesRuntimeProfiles(t *testing.T) {
	registry := runtimex.NewService()
	executor := runtimex.NewExecutor()
	item := scientistbench.Case{}
	node := orchestrator.NodeSpec{ID: "node-execution", Stage: "execution"}
	profile := orchestrator.WorkerProfile{RoleID: "execution_agent", PromptPreamble: "Execution agent preamble"}

	prompt := buildScientistBenchWorkerPrompt(item, node, profile, registry, executor)
	assert.True(t, strings.Contains(prompt, "docker.openfoam.v1"))
	assert.True(t, strings.Contains(prompt, "docker.benchmark-runner.v1"))
	assert.True(t, strings.Contains(prompt, "docker run --rm"))
}

func TestApplyScientistBenchReviewOutputTracksJudgeScores(t *testing.T) {
	item := scientistbench.Case{
		Reviews: []scientistbench.Review{
			{
				ID:   "review-existing",
				Type: scientistbench.ReviewJudge,
				Scores: scientistbench.ReviewScores{
					Overall: 4,
				},
			},
		},
	}

	item = applyScientistBenchReviewOutput(item, scientistbench.RunRecord{Role: "judge_agent"}, orchestrator.WorkerOutput{
		Summary: "Faithful overall",
		Review: &orchestrator.ReviewPayload{
			Decision:   "accept",
			Summary:    "Implementation is mostly faithful",
			Confidence: 0.8,
			Scores: orchestrator.ReviewScorePayload{
				Overall: 5,
			},
		},
	})

	require.Len(t, item.Reviews, 2)
	assert.Equal(t, []float64{4, 5}, item.Scores.Reproduction.JudgeScores)
	assert.Equal(t, 4.5, item.Scores.Reproduction.CorrectnessMean)
	assert.Greater(t, item.Scores.Reproduction.CorrectnessStd, 0.0)
}

func TestApplyScientistBenchReviewOutputTracksPaperGenerationScores(t *testing.T) {
	item := scientistbench.Case{
		Artifacts: []scientistbench.Artifact{
			{ID: "bundle-1", Kind: scientistbench.ArtifactResultBundle},
		},
	}

	item = applyScientistBenchReviewOutput(item, scientistbench.RunRecord{Role: "domain_expert_reviewer"}, orchestrator.WorkerOutput{
		Summary: "Readable and coherent draft",
		Review: &orchestrator.ReviewPayload{
			Decision:            "weak_accept",
			ReadablePaper:       true,
			NovelInsightPresent: true,
			CodeRuns:            true,
			Scores: orchestrator.ReviewScorePayload{
				Overall:              4.2,
				IdeaQuality:          4.0,
				MethodSoundness:      4.1,
				ResultInterpretation: 3.8,
				WritingQuality:       4.3,
			},
		},
	})

	require.Len(t, item.Reviews, 1)
	assert.True(t, item.Scores.PaperGeneration.ReadablePaper)
	assert.True(t, item.Scores.PaperGeneration.NovelInsightPresent)
	assert.True(t, item.Scores.PaperGeneration.CodeRuns)
	assert.Equal(t, 4.0, item.Scores.PaperGeneration.IdeaQuality)
	assert.Greater(t, item.Scores.PaperGeneration.OverallSuccess, 0.0)
}

func TestApplyScientistBenchReviewOutputTracksPaperComparisonScores(t *testing.T) {
	item := scientistbench.Case{}

	item = applyScientistBenchReviewOutput(item, scientistbench.RunRecord{Role: "paper_comparison_reviewer"}, orchestrator.WorkerOutput{
		Comparison: &orchestrator.ComparisonPayload{
			Summary:               "Method matches closely",
			MotivationAlignment:   4.5,
			MethodologyAlignment:  4.0,
			NoveltyAlignment:      3.5,
			ExperimentalAlignment: 4.2,
		},
	})

	require.Len(t, item.Reviews, 1)
	assert.Equal(t, 4.5, item.Scores.PaperComparison.MotivationAlignment)
	assert.Equal(t, 4.0, item.Scores.PaperComparison.MethodologyAlignment)
	assert.Equal(t, 3.5, item.Scores.PaperComparison.NoveltyAlignment)
	assert.Equal(t, 4.2, item.Scores.PaperComparison.ExperimentalAlignment)
}

func TestReconcileScientistBenchRuntimeExecutionMarksSuccess(t *testing.T) {
	app := &App{
		RuntimeRegistry: runtimex.NewService(),
		RuntimeExecutor: runtimex.NewExecutor(),
		RuntimeRunner: stubRuntimeRunner{result: runtimex.RunResult{
			RuntimeID: "docker.python-sci.v1",
			OutputDir: "D:/tmp/runtime",
			Executed:  true,
			Succeeded: true,
			Summary:   "Runtime execution completed",
			Stdout:    "smoke passed",
		}},
	}

	node := orchestrator.NodeSpec{ID: "node-execution", SuccessSignal: "execution_complete", FailureSignal: "code_not_executable"}
	run := scientistbench.RunRecord{Role: "execution_agent", SessionID: "sess-1"}
	parsed := orchestrator.WorkerOutput{
		Execution: &orchestrator.ExecutionPayload{RuntimeID: "docker.python-sci.v1"},
	}

	parsed = app.reconcileScientistBenchRuntimeExecution(context.Background(), run, node, parsed)
	require.NotNil(t, parsed.Execution)
	assert.Equal(t, "docker.python-sci.v1", parsed.Execution.RuntimeID)
	assert.True(t, parsed.Execution.VerificationPassed)
	assert.True(t, parsed.Execution.Executed)
	assert.Equal(t, "smoke passed", parsed.Execution.StdoutExcerpt)
	assert.Equal(t, "execution_complete", parsed.SuccessSignal)
	assert.Contains(t, parsed.Execution.OutputFiles, "D:/tmp/runtime")
}

func TestReconcileScientistBenchRuntimeExecutionMarksFailure(t *testing.T) {
	app := &App{
		RuntimeRegistry: runtimex.NewService(),
		RuntimeExecutor: runtimex.NewExecutor(),
		RuntimeRunner: stubRuntimeRunner{result: runtimex.RunResult{
			RuntimeID: "docker.python-sci.v1",
			Executed:  true,
			Succeeded: false,
			Summary:   "Runtime execution failed with exit code 1",
			Stderr:    "traceback",
		}},
	}

	node := orchestrator.NodeSpec{ID: "node-execution", SuccessSignal: "execution_complete", FailureSignal: "code_not_executable"}
	run := scientistbench.RunRecord{Role: "execution_agent", SessionID: "sess-1"}
	parsed := orchestrator.WorkerOutput{
		Status:    "succeeded",
		Execution: &orchestrator.ExecutionPayload{RuntimeID: "docker.python-sci.v1"},
	}

	parsed = app.reconcileScientistBenchRuntimeExecution(context.Background(), run, node, parsed)
	require.NotNil(t, parsed.Execution)
	assert.False(t, parsed.Execution.VerificationPassed)
	assert.True(t, parsed.Execution.Executed)
	assert.Equal(t, "failed", parsed.Status)
	assert.Equal(t, "code_not_executable", parsed.FailureSignal)
	assert.Contains(t, parsed.Risks, "Runtime execution failed with exit code 1")
}

func TestRefreshScientistBenchMetricsComputesCompletenessAndCorrectness(t *testing.T) {
	item := scientistbench.Case{
		Artifacts: []scientistbench.Artifact{
			{
				Kind: scientistbench.ArtifactDockerLog,
				Metadata: map[string]string{
					"verification_passed": "true",
				},
			},
		},
		Reviews: []scientistbench.Review{
			{Type: scientistbench.ReviewAdvisor},
			{Type: scientistbench.ReviewJudge, Scores: scientistbench.ReviewScores{Overall: 4}},
			{Type: scientistbench.ReviewJudge, Scores: scientistbench.ReviewScores{Overall: 5}},
		},
	}

	item = refreshScientistBenchMetrics(item)
	assert.Equal(t, 1.0, item.Scores.Reproduction.Completeness)
	assert.Equal(t, 1, item.Scores.Reproduction.AdvisorReports)
	assert.Equal(t, 4.5, item.Scores.Reproduction.CorrectnessMean)
	assert.True(t, item.Scores.PaperGeneration.CodeRuns)
}

func TestScientistBenchAggregateSignalResolvedWhenThresholdsPass(t *testing.T) {
	item := scientistbench.Case{
		Scores: scientistbench.AggregateScores{
			PaperGeneration: scientistbench.PaperGenerationScores{
				ReadablePaper:  true,
				CodeRuns:       true,
				OverallSuccess: 3.8,
			},
			Reproduction: scientistbench.ReproductionScores{
				Completeness:    1.0,
				CorrectnessMean: 3.6,
			},
			PaperComparison: scientistbench.PaperComparisonScores{
				MethodologyAlignment: 3.2,
			},
		},
		Inputs: scientistbench.Inputs{
			TargetPaper: scientistbench.TargetPaper{Title: "Target"},
		},
	}

	assert.Equal(t, string(scientistbench.SignalCaseResolved), scientistBenchAggregateSignal(item))
}

func TestScientistBenchAggregateSignalFailsWhenCompletenessMissing(t *testing.T) {
	item := scientistbench.Case{
		Scores: scientistbench.AggregateScores{
			PaperGeneration: scientistbench.PaperGenerationScores{
				ReadablePaper:  true,
				CodeRuns:       true,
				OverallSuccess: 3.8,
			},
			Reproduction: scientistbench.ReproductionScores{
				Completeness:    0.5,
				CorrectnessMean: 4.0,
			},
		},
	}

	assert.Equal(t, string(scientistbench.SignalCaseNotResolved), scientistBenchAggregateSignal(item))
}

func TestReconcileScientistBenchCheckpointKeepsFreshRunningRun(t *testing.T) {
	taskRuns := taskrun.NewService(nil)
	sess := session.Session{ID: "child-1", ParentSessionID: "parent-1", Title: "Execution"}
	taskRuns.Queue(sess, "run")
	taskRuns.Start(sess)

	now := time.Now()
	item := scientistbench.Case{
		Runs: []scientistbench.RunRecord{
			{
				ID:               "run-1",
				NodeID:           "node-execution",
				Role:             "execution_agent",
				Status:           "queued",
				StartedAt:        now.Add(-30 * time.Second).Unix(),
				TaskRunSessionID: "child-1",
				TaskRunStatus:    "running",
			},
		},
	}
	app := &App{TaskRuns: taskRuns}

	item, liveRun := app.reconcileScientistBenchCheckpoint(item, "node-execution", "execution_agent", now)
	require.NotNil(t, liveRun)
	assert.Equal(t, "run-1", liveRun.ID)
	require.Len(t, item.Runs, 1)
	assert.Empty(t, item.Artifacts)
}

func TestReconcileScientistBenchCheckpointRecoversStaleRun(t *testing.T) {
	taskRuns := taskrun.NewService(nil)
	sess := session.Session{ID: "child-2", ParentSessionID: "parent-1", Title: "Execution"}
	taskRuns.Queue(sess, "run")
	taskRuns.Start(sess)

	now := time.Now()
	item := scientistbench.Case{
		Runs: []scientistbench.RunRecord{
			{
				ID:               "run-2",
				NodeID:           "node-execution",
				Role:             "execution_agent",
				Status:           "queued",
				StartedAt:        now.Add(-5 * time.Minute).Unix(),
				TaskRunSessionID: "child-2",
				TaskRunStatus:    "running",
			},
		},
	}
	app := &App{TaskRuns: taskRuns}

	item, liveRun := app.reconcileScientistBenchCheckpoint(item, "node-execution", "execution_agent", now)
	assert.Nil(t, liveRun)
	require.Len(t, item.Runs, 1)
	assert.Equal(t, "failed", item.Runs[0].Status)
	assert.Contains(t, item.Runs[0].Error, "Recovered stale in-progress task run")
	require.Len(t, item.Artifacts, 1)
	assert.Equal(t, "Recovery Checkpoint", item.Artifacts[0].Label)
}

func TestReconcileScientistBenchGraphFromRunsAdvancesSingleRoleNode(t *testing.T) {
	app := &App{Orchestrator: orchestrator.NewService()}
	item := scientistbench.Case{
		ID:     "case-reconcile-1",
		Status: scientistbench.StatusRunning,
		GraphState: scientistbench.GraphState{
			CurrentStage: "corpus_retrieval",
			ActiveNode:   "node-corpus-retrieval",
			ActiveRole:   "research_agent",
		},
		Runs: []scientistbench.RunRecord{
			{
				ID:             "run-evidence",
				NodeID:         "node-corpus-retrieval",
				Role:           "research_agent",
				Status:         "complete",
				SignalsEmitted: []string{"evidence_ready"},
			},
		},
	}

	item, changed, err := app.reconcileScientistBenchGraphFromRuns(item)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, "node-idea-gate", item.GraphState.ActiveNode)
	assert.Equal(t, "idea_maker", item.GraphState.ActiveRole)
}

func TestReconcileScientistBenchGraphFromRunsAdvancesToNextPendingRole(t *testing.T) {
	app := &App{Orchestrator: orchestrator.NewService()}
	item := scientistbench.Case{
		ID:     "case-reconcile-2",
		Status: scientistbench.StatusRunning,
		GraphState: scientistbench.GraphState{
			CurrentStage: "ideation",
			ActiveNode:   "node-idea-gate",
			ActiveRole:   "idea_maker",
		},
		Runs: []scientistbench.RunRecord{
			{
				ID:     "run-idea-maker",
				NodeID: "node-idea-gate",
				Role:   "idea_maker",
				Status: "complete",
			},
		},
	}

	item, changed, err := app.reconcileScientistBenchGraphFromRuns(item)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, "node-idea-gate", item.GraphState.ActiveNode)
	assert.Equal(t, "idea_hater", item.GraphState.ActiveRole)
}

func TestReconcileScientistBenchGraphFromRunsAdvancesCompletedMultiRoleNode(t *testing.T) {
	app := &App{Orchestrator: orchestrator.NewService()}
	item := scientistbench.Case{
		ID:     "case-reconcile-3",
		Status: scientistbench.StatusRunning,
		GraphState: scientistbench.GraphState{
			CurrentStage: "ideation",
			ActiveNode:   "node-idea-gate",
			ActiveRole:   "chief_scientist",
		},
		Runs: []scientistbench.RunRecord{
			{ID: "run-chief", NodeID: "node-idea-gate", Role: "chief_scientist", Status: "complete", SignalsEmitted: []string{"idea_gate_passed"}},
			{ID: "run-hater", NodeID: "node-idea-gate", Role: "idea_hater", Status: "complete"},
			{ID: "run-maker", NodeID: "node-idea-gate", Role: "idea_maker", Status: "complete"},
		},
	}

	item, changed, err := app.reconcileScientistBenchGraphFromRuns(item)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, "node-method-plan", item.GraphState.ActiveNode)
	assert.Equal(t, "method_planner", item.GraphState.ActiveRole)
}

func TestScientistBenchEventContentHandlesZeroValueEvent(t *testing.T) {
	assert.Equal(t, "", scientistBenchEventContent(agent.AgentEvent{}))
}

func TestScientistBenchEventContentReturnsAssistantText(t *testing.T) {
	event := agent.AgentEvent{
		Message: message.Message{
			Parts: []message.ContentPart{
				message.TextContent{Text: "  runtime ok  "},
			},
		},
	}

	assert.Equal(t, "runtime ok", scientistBenchEventContent(event))
}

func TestScheduleScientistBenchContinuationDeduplicatesCase(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var calls atomic.Int32

	app := &App{
		scientistBenchStarter: func(context.Context, string) (ScientistBenchNodeRun, error) {
			calls.Add(1)
			started <- struct{}{}
			<-release
			return ScientistBenchNodeRun{}, nil
		},
		scientistBenchContinuation: map[string]struct{}{},
		ScientistBench:             trueScientistBenchService{},
		Orchestrator:               orchestrator.NewService(),
	}

	app.scheduleScientistBenchContinuation("case-auto")
	app.scheduleScientistBenchContinuation("case-auto")

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("expected continuation starter to run")
	}

	select {
	case <-started:
		t.Fatal("expected duplicate continuation scheduling to be suppressed")
	case <-time.After(150 * time.Millisecond):
	}

	close(release)
	app.watcherWG.Wait()
	assert.Equal(t, int32(1), calls.Load())
}

func TestScientistBenchRegressionFixtures(t *testing.T) {
	now := time.Now()
	taskRuns := taskrun.NewService(nil)
	staleSession := session.Session{ID: "fixture-child", ParentSessionID: "fixture-parent", Title: "Execution"}
	taskRuns.Queue(staleSession, "run")
	taskRuns.Start(staleSession)

	registry := runtimex.NewService()
	executor := runtimex.NewExecutor()

	t.Run("resolved", func(t *testing.T) {
		item := scientistbench.Case{
			Scores: scientistbench.AggregateScores{
				PaperGeneration: scientistbench.PaperGenerationScores{
					ReadablePaper:  true,
					CodeRuns:       true,
					OverallSuccess: 3.7,
				},
				Reproduction: scientistbench.ReproductionScores{
					Completeness:    1.0,
					CorrectnessMean: 3.5,
				},
			},
		}
		assert.Equal(t, string(scientistbench.SignalCaseResolved), scientistBenchAggregateSignal(item))
	})

	t.Run("permission_blocked", func(t *testing.T) {
		app := &App{
			RuntimeRegistry: registry,
			RuntimeExecutor: executor,
			RuntimeRunner: stubRuntimeRunner{result: runtimex.RunResult{
				RuntimeID: "docker.python-sci.v1",
				Blocked:   true,
				Summary:   "Runtime execution blocked by permission policy",
			}},
		}
		parsed := app.reconcileScientistBenchRuntimeExecution(context.Background(),
			scientistbench.RunRecord{Role: "execution_agent", SessionID: "sess-blocked"},
			orchestrator.NodeSpec{ID: "node-execution", SuccessSignal: "execution_complete", FailureSignal: "code_not_executable"},
			orchestrator.WorkerOutput{Execution: &orchestrator.ExecutionPayload{RuntimeID: "docker.python-sci.v1"}},
		)
		require.NotNil(t, parsed.Execution)
		assert.True(t, parsed.Execution.Blocked)
		assert.Equal(t, "failed", parsed.Status)
	})

	t.Run("runtime_dry_run_plan", func(t *testing.T) {
		item := scientistbench.Case{}
		app := &App{RuntimeRegistry: registry, RuntimeExecutor: executor}
		item = app.captureRuntimePlansForCase(item, "node-execution", "execution_agent", "run-dry")
		require.NotEmpty(t, item.Artifacts)
		assert.Equal(t, "Runtime Plan", item.Artifacts[0].Label)
		assert.Equal(t, "true", item.Artifacts[0].Metadata["dry_run"])
	})

	t.Run("checkpoint_recovery", func(t *testing.T) {
		item := scientistbench.Case{
			Runs: []scientistbench.RunRecord{
				{
					ID:               "fixture-run",
					NodeID:           "node-execution",
					Role:             "execution_agent",
					Status:           "queued",
					StartedAt:        now.Add(-10 * time.Minute).Unix(),
					TaskRunSessionID: staleSession.ID,
					TaskRunStatus:    "running",
				},
			},
		}
		app := &App{TaskRuns: taskRuns}
		item, liveRun := app.reconcileScientistBenchCheckpoint(item, "node-execution", "execution_agent", now)
		assert.Nil(t, liveRun)
		assert.Equal(t, "failed", item.Runs[0].Status)
		require.NotEmpty(t, item.Artifacts)
	})

	t.Run("graph_recovery_continue", func(t *testing.T) {
		app := &App{Orchestrator: orchestrator.NewService()}
		item := scientistbench.Case{
			ID:     "fixture-graph-recovery",
			Status: scientistbench.StatusRunning,
			GraphState: scientistbench.GraphState{
				CurrentStage: "corpus_retrieval",
				ActiveNode:   "node-corpus-retrieval",
				ActiveRole:   "research_agent",
			},
			Runs: []scientistbench.RunRecord{
				{
					ID:             "fixture-complete",
					NodeID:         "node-corpus-retrieval",
					Role:           "research_agent",
					Status:         "complete",
					SignalsEmitted: []string{"evidence_ready"},
				},
			},
		}
		item, changed, err := app.reconcileScientistBenchGraphFromRuns(item)
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, "node-idea-gate", item.GraphState.ActiveNode)
	})

	t.Run("aggregate_unresolved", func(t *testing.T) {
		item := scientistbench.Case{
			Scores: scientistbench.AggregateScores{
				PaperGeneration: scientistbench.PaperGenerationScores{
					ReadablePaper:  true,
					CodeRuns:       true,
					OverallSuccess: 3.7,
				},
				Reproduction: scientistbench.ReproductionScores{
					Completeness:    0.5,
					CorrectnessMean: 4.0,
				},
			},
		}
		assert.Equal(t, string(scientistbench.SignalCaseNotResolved), scientistBenchAggregateSignal(item))
	})
}

type trueScientistBenchService struct{}

func (trueScientistBenchService) Subscribe(context.Context) <-chan pubsub.Event[scientistbench.Case] {
	return nil
}

func (trueScientistBenchService) CreateCase(context.Context, scientistbench.CreateCaseInput) (scientistbench.Case, error) {
	return scientistbench.Case{}, nil
}

func (trueScientistBenchService) Get(context.Context, string) (scientistbench.Case, error) {
	return scientistbench.Case{}, nil
}

func (trueScientistBenchService) List(context.Context) ([]scientistbench.Case, error) {
	return nil, nil
}

func (trueScientistBenchService) Save(context.Context, scientistbench.Case) (scientistbench.Case, error) {
	return scientistbench.Case{}, nil
}

func (trueScientistBenchService) UpdateGraphState(context.Context, string, scientistbench.GraphState) (scientistbench.Case, error) {
	return scientistbench.Case{}, nil
}

func (trueScientistBenchService) SetTermination(context.Context, string, scientistbench.TerminationSignal, string) (scientistbench.Case, error) {
	return scientistbench.Case{}, nil
}

func (trueScientistBenchService) UpsertRun(context.Context, string, scientistbench.RunRecord) (scientistbench.Case, error) {
	return scientistbench.Case{}, nil
}

func (trueScientistBenchService) UpsertArtifact(context.Context, string, scientistbench.Artifact) (scientistbench.Case, error) {
	return scientistbench.Case{}, nil
}

func (trueScientistBenchService) UpsertReview(context.Context, string, scientistbench.Review) (scientistbench.Case, error) {
	return scientistbench.Case{}, nil
}

func (trueScientistBenchService) UpdateScores(context.Context, string, scientistbench.AggregateScores) (scientistbench.Case, error) {
	return scientistbench.Case{}, nil
}

func (trueScientistBenchService) Delete(context.Context, string) error {
	return nil
}
