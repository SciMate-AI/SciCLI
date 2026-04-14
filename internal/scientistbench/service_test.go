package scientistbench

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	scientistBenchTestSetup sync.Once
	scientistBenchTestDir   string
)

func TestServiceCreateAndPersistCase(t *testing.T) {
	resetScientistBenchTestState(t)

	svc, err := NewService()
	require.NoError(t, err)

	item, err := svc.CreateCase(context.Background(), CreateCaseInput{
		Mode:  ModeJoint,
		Level: Level2,
		Title: "Recover hidden method details",
		Inputs: Inputs{
			CoreIdea: "Recover a hidden method from references and dataset",
			ReferenceSet: []Reference{
				{ID: "ref-1", Title: "Paper A"},
			},
		},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, item.ID)
	assert.Equal(t, StatusPlanning, item.Status)
	assert.Equal(t, Level2, item.Level)
	assert.Equal(t, ModeJoint, item.Mode)
	assert.Equal(t, "scientist-bench", item.Suite)

	loaded, err := svc.Get(context.Background(), item.ID)
	require.NoError(t, err)
	assert.Equal(t, item.ID, loaded.ID)
	assert.Equal(t, "Recover hidden method details", loaded.Title)
	assert.Equal(t, "Recover a hidden method from references and dataset", loaded.Inputs.CoreIdea)
}

func TestServiceTracksRunsArtifactsReviewsAndTermination(t *testing.T) {
	resetScientistBenchTestState(t)

	svc, err := NewService()
	require.NoError(t, err)

	item, err := svc.CreateCase(context.Background(), CreateCaseInput{
		ID:    "case-1",
		Title: "Evaluate generated paper",
	})
	require.NoError(t, err)

	item, err = svc.UpsertRun(context.Background(), item.ID, RunRecord{
		ID:            "run-1",
		Role:          "research_agent",
		NodeID:        "node-corpus-retrieval",
		Status:        "complete",
		OutputSummary: "Collected 18 papers",
	})
	require.NoError(t, err)
	require.Len(t, item.Runs, 1)

	item, err = svc.UpsertArtifact(context.Background(), item.ID, Artifact{
		ID:           "artifact-1",
		Kind:         ArtifactPaperDraft,
		Label:        "Draft v1",
		ProducerRole: "paper_writer",
	})
	require.NoError(t, err)
	require.Len(t, item.Artifacts, 1)

	item, err = svc.UpsertReview(context.Background(), item.ID, Review{
		ID:           "review-1",
		Type:         ReviewDomainExpert,
		ReviewerRole: "domain_expert_reviewer",
		Decision:     "weak_accept",
		Summary:      "Readable paper with decent structure",
		Scores: ReviewScores{
			Overall:        3.8,
			WritingQuality: 4.2,
		},
	})
	require.NoError(t, err)
	require.Len(t, item.Reviews, 1)

	item, err = svc.UpdateScores(context.Background(), item.ID, AggregateScores{
		PaperGeneration: PaperGenerationScores{
			ReadablePaper:  true,
			CodeRuns:       true,
			OverallSuccess: 0.75,
		},
	})
	require.NoError(t, err)
	assert.True(t, item.Scores.PaperGeneration.ReadablePaper)
	assert.Equal(t, 0.75, item.Scores.PaperGeneration.OverallSuccess)

	item, err = svc.SetTermination(context.Background(), item.ID, SignalCaseResolved, "Paper and code passed baseline checks")
	require.NoError(t, err)
	assert.Equal(t, SignalCaseResolved, item.Termination.Signal)
	assert.True(t, item.Termination.Resolved)
	assert.Equal(t, StatusResolved, item.Status)
}

func TestUpdateGraphStateMarksCaseRunning(t *testing.T) {
	resetScientistBenchTestState(t)

	svc, err := NewService()
	require.NoError(t, err)

	item, err := svc.CreateCase(context.Background(), CreateCaseInput{ID: "case-graph", Title: "Graph state"})
	require.NoError(t, err)

	item, err = svc.UpdateGraphState(context.Background(), item.ID, GraphState{
		CurrentStage: "research_planning",
		ActiveNode:   "node-research-plan",
		ActiveRole:   "research_agent",
		PendingNodes: []string{"node-corpus-retrieval"},
	})
	require.NoError(t, err)
	assert.Equal(t, StatusRunning, item.Status)
	assert.Equal(t, "node-research-plan", item.GraphState.ActiveNode)
}

func TestSyncWorkflowStateBuildsPhaseMachine(t *testing.T) {
	item := SyncWorkflowState(Case{
		ID:     "case-workflow",
		Status: StatusRunning,
		GraphState: GraphState{
			ActiveNode:      "node-paper-draft",
			CompletedNodes:  []string{"node-case-intake", "node-research-plan", "node-corpus-retrieval", "node-idea-gate", "node-method-plan", "node-implementation", "node-execution", "node-analysis-figures"},
			BlockedNodes:    []string{},
			CurrentStage:    "paper_writing",
			ActiveRole:      "paper_writer",
			ReceivedSignals: []string{"execution_complete"},
		},
		Runs: []RunRecord{
			{ID: "run-intake", NodeID: "node-case-intake", StartedAt: 10, FinishedAt: 20, StateUpdates: []string{"scientistbench_submit_route_decision"}},
			{ID: "run-research", NodeID: "node-corpus-retrieval", StartedAt: 30, FinishedAt: 40, StateUpdates: []string{"scientistbench_submit_research_pack"}},
			{ID: "run-draft", NodeID: "node-paper-draft", StartedAt: 50, StateUpdates: []string{"scientistbench_submit_artifact"}},
		},
	})

	assert.Equal(t, "paper_writing", item.Workflow.CurrentPhase)
	require.NotEmpty(t, item.Workflow.Phases)
	assert.Equal(t, WorkflowPhaseCompleted, item.Workflow.Phases[0].Status)
	assert.Equal(t, WorkflowPhaseCompleted, item.Workflow.Phases[1].Status)
	assert.Equal(t, WorkflowPhaseActive, item.Workflow.Phases[5].Status)
	assert.Equal(t, "node-paper-draft", item.Workflow.Phases[5].ActiveNode)
	assert.Contains(t, item.Workflow.Phases[5].AppliedStateUpdates, "scientistbench_submit_artifact")
}

func resetScientistBenchTestState(t *testing.T) {
	t.Helper()

	scientistBenchTestSetup.Do(func() {
		dir, err := os.MkdirTemp("", "scicli-scientist-bench-test-*")
		require.NoError(t, err)

		dataDir := filepath.Join(dir, "data")
		require.NoError(t, os.MkdirAll(dataDir, 0o755))

		payload, err := json.Marshal(map[string]any{
			"data": map[string]any{
				"directory": dataDir,
			},
		})
		require.NoError(t, err)

		configFile := filepath.Join(dir, ".scicli.json")
		require.NoError(t, os.WriteFile(configFile, payload, 0o644))

		_, err = config.Load(dir, false)
		require.NoError(t, err)
		scientistBenchTestDir = dir
	})

	err := os.Remove(filepath.Join(scientistBenchTestDir, "data", "scientist-bench.json"))
	if err != nil && !os.IsNotExist(err) {
		require.NoError(t, err)
	}
}
