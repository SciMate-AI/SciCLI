package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
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
	"github.com/SciMate-AI/scicli/internal/skills"
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

type stubSkillService struct {
	items       []skills.Skill
	recommended []skills.Skill
}

func (s stubSkillService) List(context.Context) ([]skills.Skill, error) {
	return s.items, nil
}

func (s stubSkillService) Recommend(context.Context, string, int) ([]skills.Skill, error) {
	return s.recommended, nil
}

func (s stubSkillService) Activate(context.Context, string, string) (skills.Skill, error) {
	return skills.Skill{}, nil
}

func (s stubSkillService) Install(context.Context, string) (skills.Skill, error) {
	return skills.Skill{}, nil
}

func (s stubSkillService) Uninstall(context.Context, string) error {
	return nil
}

func (s stubSkillService) Active(string) []skills.Skill {
	return nil
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

	prompt := buildScientistBenchWorkerPrompt(item, node, profile, nil, nil, nil)
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

	prompt := buildScientistBenchWorkerPrompt(item, node, profile, nil, nil, nil)
	assert.True(t, strings.Contains(prompt, "Train a hybrid encoder"))
	assert.True(t, strings.Contains(prompt, "encode data | optimize ranking loss"))
	assert.True(t, strings.Contains(prompt, "use small batch smoke test first"))
}

func TestBuildScientistBenchWorkerPromptIncludesNormalizedReferenceBundle(t *testing.T) {
	refs := []orchestrator.ReferencePayload{
		{
			Key:                "smith2024",
			Title:              "Structured Citation Grounding",
			Authors:            []string{"Alice Smith", "Bob Jones"},
			Year:               2024,
			Venue:              "ICML",
			URL:                "https://example.com/paper",
			ZoteroKey:          "ABCD1234",
			FormattedReference: "Smith, A., & Jones, B. (2024). Structured Citation Grounding. ICML.",
			BibTeX:             "@inproceedings{smith2024,title={Structured Citation Grounding}}",
			KeyClaim:           "Normalizing references reduces citation errors.",
		},
	}
	rawRefs, err := json.Marshal(refs)
	require.NoError(t, err)

	item := scientistbench.Case{
		Artifacts: []scientistbench.Artifact{
			{
				Kind: scientistbench.ArtifactCitation,
				Metadata: map[string]string{
					"summary":              "Collected normalized references",
					"references_json":      string(rawRefs),
					"citation_keys":        "smith2024",
					"bibtex_bundle":        refs[0].BibTeX,
					"formatted_references": refs[0].FormattedReference,
				},
			},
		},
	}
	node := orchestrator.NodeSpec{ID: "node-paper-draft", Stage: "paper_generation"}
	profile := orchestrator.WorkerProfile{RoleID: "paper_writer", PromptPreamble: "Paper writer preamble"}

	prompt := buildScientistBenchWorkerPrompt(item, node, profile, nil, nil, nil)
	assert.Contains(t, prompt, "Structured Citation Grounding")
	assert.Contains(t, prompt, "smith2024")
	assert.Contains(t, prompt, "@inproceedings{smith2024")
	assert.Contains(t, prompt, "Create references.bib")
}

func TestBuildScientistBenchWorkerPromptIncludesSkillRecommendations(t *testing.T) {
	item := scientistbench.Case{
		Title: "CRISPR off-target analysis",
		Inputs: scientistbench.Inputs{
			CoreIdea: "Compare retrieval grounded literature review pipelines",
		},
	}
	node := orchestrator.NodeSpec{ID: "node-corpus-retrieval", Stage: "literature_review"}
	profile := orchestrator.WorkerProfile{RoleID: "research_agent", PromptPreamble: "Research agent preamble"}
	skillSvc := stubSkillService{
		items: []skills.Skill{
			{ID: "citation-management", Description: "Validate and format citations."},
			{ID: "research-lookup", Description: "Find authoritative research references."},
		},
		recommended: []skills.Skill{
			{ID: "citation-management", Description: "Validate and format citations."},
			{ID: "research-lookup", Description: "Find authoritative research references."},
		},
	}

	prompt := buildScientistBenchWorkerPrompt(item, node, profile, nil, nil, skillSvc)
	assert.Contains(t, prompt, "Recommended skills for this worker")
	assert.Contains(t, prompt, "activate_skill")
	assert.Contains(t, prompt, "citation-management")
	assert.Contains(t, prompt, "research-lookup")
}

func TestBuildScientistBenchWorkerPromptIncludesRuntimeProfiles(t *testing.T) {
	registry := runtimex.NewService()
	executor := runtimex.NewExecutor()
	item := scientistbench.Case{}
	node := orchestrator.NodeSpec{ID: "node-execution", Stage: "execution"}
	profile := orchestrator.WorkerProfile{RoleID: "execution_agent", PromptPreamble: "Execution agent preamble"}

	prompt := buildScientistBenchWorkerPrompt(item, node, profile, registry, executor, nil)
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
		scientistBenchContinuation: map[string]int{},
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

func TestPostScientistBenchLaunchWritesRootSessionMessage(t *testing.T) {
	msgs := newStubMessageService()
	app := &App{Messages: msgs}

	err := app.postScientistBenchLaunch(context.Background(), scientistbench.Case{
		ID:            "case-launch",
		Mode:          scientistbench.ModePaperGeneration,
		RootSessionID: "root-1",
		Title:         "Write the scicli paper",
	}, scientistbench.RunRecord{
		NodeID:    "node-case-intake",
		Role:      "chief_scientist",
		SessionID: "sbtask-launch",
	}, "Scientist Bench node started")
	require.NoError(t, err)

	text := msgs.latestText(t, "root-1")
	assert.Contains(t, text, "Scientist Bench node started")
	assert.Contains(t, text, "case case-launch")
	assert.Contains(t, text, "mode paper_generation")
	assert.Contains(t, text, "node node-case-intake")
	assert.Contains(t, text, "role chief_scientist")
	assert.Contains(t, text, "task sbtask-launch")
}

func TestStartScientistBenchRunSyncMirrorsTaskProgressIntoRootSession(t *testing.T) {
	msgs := newStubMessageService()
	bench := newStubScientistBenchService(scientistbench.Case{
		ID:            "case-sync",
		Mode:          scientistbench.ModeJoint,
		RootSessionID: "root-1",
		GraphState: scientistbench.GraphState{
			ActiveNode: "node-method-plan",
			ActiveRole: "method_planner",
		},
		Runs: []scientistbench.RunRecord{
			{
				ID:               "run-sync",
				NodeID:           "node-method-plan",
				Role:             "method_planner",
				SessionID:        "sbtask-sync",
				TaskRunSessionID: "sbtask-sync",
			},
		},
	})
	taskRuns := taskrun.NewService(nil)
	app := &App{
		Messages:       msgs,
		ScientistBench: bench,
		TaskRuns:       taskRuns,
	}

	ctx, cancel := context.WithCancel(context.Background())
	app.startScientistBenchRunSync(ctx)
	t.Cleanup(func() {
		cancel()
		app.watcherWG.Wait()
	})
	time.Sleep(50 * time.Millisecond)

	sess := session.Session{ID: "sbtask-sync", ParentSessionID: "root-1", Title: "Method Planner: node-method-plan"}
	taskRuns.Queue(sess, "plan the method")
	taskRuns.Start(sess)
	taskRuns.UpdateDetail(sess.ID, "Running tool rg", "rg", nil)
	taskRuns.Finish(sess.ID, taskrun.StatusComplete, "Drafted method plan", nil)

	require.Eventually(t, func() bool {
		lines := strings.Join(msgs.listTexts("root-1"), "\n")
		return strings.Contains(lines, "progress | Running tool rg | tool rg") &&
			strings.Contains(lines, "complete | Drafted method plan")
	}, 2*time.Second, 20*time.Millisecond)

	lines := msgs.listTexts("root-1")
	assert.Contains(t, strings.Join(lines, "\n"), "Scientist Bench progress | case case-sync | node node-method-plan | role method_planner | progress | Running tool rg | tool rg")
	assert.Contains(t, strings.Join(lines, "\n"), "Scientist Bench progress | case case-sync | node node-method-plan | role method_planner | complete | Drafted method plan")
}

func TestPostScientistBenchNodeResultWritesNextStepSummary(t *testing.T) {
	msgs := newStubMessageService()
	app := &App{Messages: msgs}

	err := app.postScientistBenchNodeResult(context.Background(), scientistbench.Case{
		ID:            "case-next",
		RootSessionID: "root-1",
		Status:        scientistbench.StatusRunning,
		GraphState: scientistbench.GraphState{
			ActiveNode: "node-execution",
			ActiveRole: "execution_agent",
		},
	}, scientistbench.RunRecord{
		NodeID:         "node-method-plan",
		Role:           "method_planner",
		Status:         "complete",
		OutputSummary:  "Method plan drafted",
		SignalsEmitted: []string{"method_ready"},
	})
	require.NoError(t, err)

	text := msgs.latestText(t, "root-1")
	assert.Contains(t, text, "Scientist Bench update")
	assert.Contains(t, text, "case case-next")
	assert.Contains(t, text, "node node-method-plan")
	assert.Contains(t, text, "role method_planner")
	assert.Contains(t, text, "Method plan drafted")
	assert.Contains(t, text, "signal method_ready")
	assert.Contains(t, text, "next node node-execution")
	assert.Contains(t, text, "next role execution_agent")
}

func TestReadyRolesForNodeHonorsDependencies(t *testing.T) {
	app := &App{Orchestrator: orchestrator.NewService()}
	node, ok := app.Orchestrator.GetNode("node-idea-gate")
	require.True(t, ok)

	item := scientistbench.Case{}
	assert.ElementsMatch(t, []string{"idea_maker", "idea_hater"}, readyRolesForNode(item, node))

	item.Runs = []scientistbench.RunRecord{
		{ID: "run-maker", NodeID: "node-idea-gate", Role: "idea_maker", Status: "complete"},
	}
	assert.Equal(t, []string{"idea_hater"}, readyRolesForNode(item, node))

	item.Runs = append(item.Runs, scientistbench.RunRecord{
		ID: "run-hater", NodeID: "node-idea-gate", Role: "idea_hater", Status: "complete",
	})
	assert.Equal(t, []string{"chief_scientist"}, readyRolesForNode(item, node))
}

func TestScientistBenchBudgetExceededByAgentSteps(t *testing.T) {
	app := &App{}
	item := scientistbench.Case{
		Budget: scientistbench.Budget{MaxAgentSteps: 2},
		Runs: []scientistbench.RunRecord{
			{ID: "run-1"},
			{ID: "run-2"},
		},
	}
	exhausted, reason, err := app.scientistBenchBudgetExceeded(context.Background(), item)
	require.NoError(t, err)
	assert.True(t, exhausted)
	assert.Contains(t, reason, "agent step budget exceeded")
}

func TestScientistBenchArtifactsForOutputStoresStructuredReferences(t *testing.T) {
	artifacts := scientistBenchArtifactsForOutput(
		scientistbench.RunRecord{ID: "run-ref", Role: "research_agent", SessionID: "sess-ref"},
		orchestrator.NodeSpec{ID: "node-corpus-retrieval"},
		orchestrator.WorkerOutput{
			Status:  "succeeded",
			Summary: "done",
			References: []orchestrator.ReferencePayload{
				{
					Key:                "smith2024",
					Title:              "Structured Citation Grounding",
					Authors:            []string{"Alice Smith"},
					Year:               2024,
					URL:                "https://example.com/paper",
					ZoteroKey:          "ABCD1234",
					FormattedReference: "Smith, A. (2024). Structured Citation Grounding.",
					BibTeX:             "@article{smith2024,title={Structured Citation Grounding}}",
				},
			},
		},
		123,
	)
	require.NotEmpty(t, artifacts)
	assert.Contains(t, artifacts[0].Metadata["references_json"], "\"smith2024\"")
	assert.Contains(t, artifacts[0].Metadata["bibtex_bundle"], "@article{smith2024")
	assert.Equal(t, "smith2024", artifacts[0].Metadata["citation_keys"])
}

func TestScientistBenchSessionTreeCostSkipsVisitedSessions(t *testing.T) {
	app := &App{
		Sessions: &stubSessionService{
			items: map[string]session.Session{
				"root-cost":  {ID: "root-cost", Cost: 1.25},
				"child-a":    {ID: "child-a", Cost: 2.5},
				"child-b":    {ID: "child-b", Cost: 0.75},
				"grandchild": {ID: "grandchild", Cost: 4.0},
			},
			children: map[string][]string{
				"root-cost":  {"child-a", "child-b"},
				"child-a":    {"grandchild"},
				"child-b":    {"grandchild"},
				"grandchild": {"root-cost"},
			},
		},
	}

	cost, err := app.scientistBenchSessionTreeCost(context.Background(), "root-cost", map[string]struct{}{})
	require.NoError(t, err)
	assert.InDelta(t, 8.5, cost, 0.001)
}

func TestWatchScientistBenchNodeRunRejectsMalformedWorkerOutput(t *testing.T) {
	run := scientistbench.RunRecord{
		ID:            "run-malformed",
		NodeID:        "node-method-plan",
		Role:          "method_planner",
		Status:        "queued",
		SessionID:     "sess-malformed",
		TaskRunStatus: "running",
	}
	item := scientistbench.Case{
		ID:            "case-malformed",
		RootSessionID: "root-malformed",
		Status:        scientistbench.StatusRunning,
		GraphState: scientistbench.GraphState{
			CurrentStage: "method_planning",
			ActiveNode:   "node-method-plan",
			ActiveRole:   "method_planner",
		},
		Runs: []scientistbench.RunRecord{run},
	}

	msgs := newStubMessageService()
	svc := newStubScientistBenchService(item)
	app := &App{
		Messages:       msgs,
		ScientistBench: svc,
		Orchestrator:   orchestrator.NewService(),
	}
	node, ok := app.Orchestrator.GetNode("node-method-plan")
	require.True(t, ok)

	done := make(chan agent.AgentEvent, 1)
	done <- agent.AgentEvent{
		Message: message.Message{
			Parts: []message.ContentPart{message.TextContent{Text: "plain text summary"}},
		},
	}
	close(done)

	app.watchScientistBenchNodeRun(item.ID, run, node, done)

	saved, err := svc.Get(context.Background(), item.ID)
	require.NoError(t, err)
	savedRun, ok := findScientistBenchRun(saved, run.ID)
	require.True(t, ok)
	assert.Equal(t, "failed", savedRun.Status)
	assert.Contains(t, savedRun.Error, "worker output must be valid JSON")
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

func (trueScientistBenchService) MutateCase(context.Context, string, func(*scientistbench.Case) error) (scientistbench.Case, error) {
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

type stubMessageService struct {
	broker   *pubsub.Broker[message.Message]
	mu       sync.Mutex
	messages []message.Message
}

func newStubMessageService() *stubMessageService {
	return &stubMessageService{
		broker: pubsub.NewBroker[message.Message](),
	}
}

func (s *stubMessageService) Subscribe(ctx context.Context) <-chan pubsub.Event[message.Message] {
	return s.broker.Subscribe(ctx)
}

func (s *stubMessageService) Create(_ context.Context, sessionID string, params message.CreateMessageParams) (message.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	msg := message.Message{
		ID:        fmt.Sprintf("msg-%d", len(s.messages)+1),
		SessionID: sessionID,
		Role:      params.Role,
		Parts:     append([]message.ContentPart(nil), params.Parts...),
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
	}
	s.messages = append(s.messages, msg)
	s.broker.Publish(pubsub.CreatedEvent, msg)
	return msg, nil
}

func (s *stubMessageService) Update(_ context.Context, msg message.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.messages {
		if s.messages[i].ID == msg.ID {
			s.messages[i] = msg
			s.broker.Publish(pubsub.UpdatedEvent, msg)
			return nil
		}
	}
	return nil
}

func (s *stubMessageService) Get(_ context.Context, id string) (message.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, msg := range s.messages {
		if msg.ID == id {
			return msg, nil
		}
	}
	return message.Message{}, fmt.Errorf("message %s not found", id)
}

func (s *stubMessageService) List(_ context.Context, sessionID string) ([]message.Message, error) {
	return s.listSession(sessionID), nil
}

func (s *stubMessageService) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	filtered := s.messages[:0]
	for _, msg := range s.messages {
		if msg.ID != id {
			filtered = append(filtered, msg)
		}
	}
	s.messages = filtered
	return nil
}

func (s *stubMessageService) DeleteSessionMessages(_ context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	filtered := s.messages[:0]
	for _, msg := range s.messages {
		if msg.SessionID != sessionID {
			filtered = append(filtered, msg)
		}
	}
	s.messages = filtered
	return nil
}

func (s *stubMessageService) listSession(sessionID string) []message.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]message.Message, 0)
	for _, msg := range s.messages {
		if msg.SessionID == sessionID {
			out = append(out, msg)
		}
	}
	return out
}

func (s *stubMessageService) listTexts(sessionID string) []string {
	msgs := s.listSession(sessionID)
	out := make([]string, 0, len(msgs))
	for _, msg := range msgs {
		out = append(out, strings.TrimSpace(msg.Content().Text))
	}
	return out
}

func (s *stubMessageService) latestText(t *testing.T, sessionID string) string {
	t.Helper()
	msgs := s.listSession(sessionID)
	require.NotEmpty(t, msgs)
	return strings.TrimSpace(msgs[len(msgs)-1].Content().Text)
}

type stubScientistBenchService struct {
	items map[string]scientistbench.Case
}

type stubSessionService struct {
	items    map[string]session.Session
	children map[string][]string
}

func (s *stubSessionService) Subscribe(context.Context) <-chan pubsub.Event[session.Session] {
	return nil
}

func (s *stubSessionService) Create(context.Context, string) (session.Session, error) {
	return session.Session{}, nil
}

func (s *stubSessionService) CreateTitleSession(context.Context, string) (session.Session, error) {
	return session.Session{}, nil
}

func (s *stubSessionService) CreateTaskSession(context.Context, string, string, string) (session.Session, error) {
	return session.Session{}, nil
}

func (s *stubSessionService) Get(_ context.Context, id string) (session.Session, error) {
	item, ok := s.items[id]
	if !ok {
		return session.Session{}, fmt.Errorf("session %s not found", id)
	}
	return item, nil
}

func (s *stubSessionService) List(context.Context) ([]session.Session, error) {
	out := make([]session.Session, 0, len(s.items))
	for _, item := range s.items {
		out = append(out, item)
	}
	return out, nil
}

func (s *stubSessionService) ListChildren(_ context.Context, parentSessionID string) ([]session.Session, error) {
	ids := s.children[parentSessionID]
	out := make([]session.Session, 0, len(ids))
	for _, id := range ids {
		item, ok := s.items[id]
		if !ok {
			return nil, fmt.Errorf("session %s not found", id)
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *stubSessionService) Save(_ context.Context, sess session.Session) (session.Session, error) {
	if s.items == nil {
		s.items = make(map[string]session.Session)
	}
	s.items[sess.ID] = sess
	return sess, nil
}

func (s *stubSessionService) Delete(_ context.Context, id string) error {
	delete(s.items, id)
	return nil
}

func newStubScientistBenchService(items ...scientistbench.Case) *stubScientistBenchService {
	svc := &stubScientistBenchService{items: make(map[string]scientistbench.Case, len(items))}
	for _, item := range items {
		svc.items[item.ID] = item
	}
	return svc
}

func (s *stubScientistBenchService) Subscribe(context.Context) <-chan pubsub.Event[scientistbench.Case] {
	return nil
}

func (s *stubScientistBenchService) CreateCase(context.Context, scientistbench.CreateCaseInput) (scientistbench.Case, error) {
	return scientistbench.Case{}, nil
}

func (s *stubScientistBenchService) Get(_ context.Context, caseID string) (scientistbench.Case, error) {
	item, ok := s.items[caseID]
	if !ok {
		return scientistbench.Case{}, fmt.Errorf("case %s not found", caseID)
	}
	return item, nil
}

func (s *stubScientistBenchService) List(context.Context) ([]scientistbench.Case, error) {
	out := make([]scientistbench.Case, 0, len(s.items))
	for _, item := range s.items {
		out = append(out, item)
	}
	return out, nil
}

func (s *stubScientistBenchService) Save(_ context.Context, item scientistbench.Case) (scientistbench.Case, error) {
	s.items[item.ID] = item
	return item, nil
}

func (s *stubScientistBenchService) MutateCase(_ context.Context, caseID string, mutate func(*scientistbench.Case) error) (scientistbench.Case, error) {
	item, ok := s.items[caseID]
	if !ok {
		return scientistbench.Case{}, fmt.Errorf("case %s not found", caseID)
	}
	if mutate != nil {
		if err := mutate(&item); err != nil {
			return scientistbench.Case{}, err
		}
	}
	s.items[caseID] = item
	return item, nil
}

func (s *stubScientistBenchService) UpdateGraphState(context.Context, string, scientistbench.GraphState) (scientistbench.Case, error) {
	return scientistbench.Case{}, nil
}

func (s *stubScientistBenchService) SetTermination(context.Context, string, scientistbench.TerminationSignal, string) (scientistbench.Case, error) {
	return scientistbench.Case{}, nil
}

func (s *stubScientistBenchService) UpsertRun(_ context.Context, caseID string, run scientistbench.RunRecord) (scientistbench.Case, error) {
	item, ok := s.items[caseID]
	if !ok {
		return scientistbench.Case{}, fmt.Errorf("case %s not found", caseID)
	}
	updated := false
	for i := range item.Runs {
		if item.Runs[i].ID == run.ID {
			item.Runs[i] = run
			updated = true
			break
		}
	}
	if !updated {
		item.Runs = append(item.Runs, run)
	}
	s.items[caseID] = item
	return item, nil
}

func (s *stubScientistBenchService) UpsertArtifact(context.Context, string, scientistbench.Artifact) (scientistbench.Case, error) {
	return scientistbench.Case{}, nil
}

func (s *stubScientistBenchService) UpsertReview(context.Context, string, scientistbench.Review) (scientistbench.Case, error) {
	return scientistbench.Case{}, nil
}

func (s *stubScientistBenchService) UpdateScores(context.Context, string, scientistbench.AggregateScores) (scientistbench.Case, error) {
	return scientistbench.Case{}, nil
}

func (s *stubScientistBenchService) Delete(context.Context, string) error {
	return nil
}
