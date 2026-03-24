package research

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/SciMate-AI/scicli/internal/taskrun"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	researchTestSetup sync.Once
	researchTestDir   string
)

func TestServicePersistsResearchState(t *testing.T) {
	resetResearchTestState(t)

	svc, err := NewService()
	require.NoError(t, err)

	state, err := svc.SetObjective(context.Background(), "session-1", "Find a lower-drag geometry")
	require.NoError(t, err)
	assert.Equal(t, "session-1", state.SessionID)
	assert.Equal(t, "Find a lower-drag geometry", state.Objective)
	assert.Equal(t, StageObjective, state.Stage)

	loaded, err := svc.Get(context.Background(), "session-1")
	require.NoError(t, err)
	assert.Equal(t, state.Objective, loaded.Objective)
	assert.True(t, loaded.HasContent())
}

func TestServiceDeleteRemovesPersistedState(t *testing.T) {
	resetResearchTestState(t)

	svc, err := NewService()
	require.NoError(t, err)

	_, err = svc.Save(context.Background(), SessionState{
		SessionID: "session-2",
		Objective: "Compare catalyst candidates",
		Stage:     StageEvaluation,
	})
	require.NoError(t, err)

	require.NoError(t, svc.Delete(context.Background(), "session-2"))

	loaded, err := svc.Get(context.Background(), "session-2")
	require.NoError(t, err)
	assert.Equal(t, "session-2", loaded.SessionID)
	assert.Empty(t, loaded.Objective)
	assert.Equal(t, StageObjective, loaded.Stage)
}

func TestServiceTracksExperimentsRunsAndEvaluations(t *testing.T) {
	resetResearchTestState(t)

	svc, err := NewService()
	require.NoError(t, err)

	state, plan, err := svc.AddExperiment(context.Background(), "session-3", "Baseline CFD sweep", "Run a coarse parameter sweep")
	require.NoError(t, err)
	assert.Equal(t, StageExperiment, state.Stage)
	assert.Equal(t, plan.ID, state.ActiveExperimentID)
	assert.Equal(t, ExperimentPlanned, plan.Status)

	state, plan, err = svc.AttachTaskRun(context.Background(), "session-3", plan.ID, taskrun.Run{
		SessionID:       "task-1",
		ParentSessionID: "session-3",
		Title:           "Sweep alpha variants",
		Status:          taskrun.StatusQueued,
		Detail:          "Queued",
		UpdatedAt:       100,
	})
	require.NoError(t, err)
	require.Len(t, plan.Runs, 1)
	assert.Equal(t, ExperimentRunning, plan.Status)
	assert.Equal(t, "task-1", plan.Runs[0].SessionID)

	require.NoError(t, svc.SyncTaskRun(context.Background(), taskrun.Run{
		SessionID:       "task-1",
		ParentSessionID: "session-3",
		Title:           "Sweep alpha variants",
		Status:          taskrun.StatusComplete,
		Detail:          "Completed successfully",
		UpdatedAt:       200,
	}))

	state, err = svc.SetActiveExperiment(context.Background(), "session-3", plan.ID)
	require.NoError(t, err)
	assert.Equal(t, plan.ID, state.ActiveExperimentID)

	state, plan, err = svc.EvaluateExperiment(context.Background(), "session-3", plan.ID, 0.82, "keep", "Best lift-to-drag so far")
	require.NoError(t, err)
	require.NotNil(t, plan.LatestEval)
	assert.Equal(t, ExperimentEvaluated, plan.Status)
	assert.Equal(t, StageDecision, state.Stage)
	assert.Equal(t, 0.82, plan.LatestEval.Score)
	assert.Equal(t, DecisionKeep, plan.LatestEval.Decision)
}

func TestServiceCloneExperimentCreatesLineageCandidate(t *testing.T) {
	resetResearchTestState(t)

	svc, err := NewService()
	require.NoError(t, err)

	_, source, err := svc.AddExperimentCandidate(context.Background(), "session-4", ExperimentPlan{
		Title:     "Refined sweep",
		Prompt:    "Run the refined sweep",
		Rationale: "Improve around the previous optimum",
	})
	require.NoError(t, err)

	state, clone, err := svc.CloneExperiment(context.Background(), "session-4", source.ID)
	require.NoError(t, err)
	assert.Equal(t, clone.ID, state.ActiveExperimentID)
	assert.Equal(t, source.ID, clone.ParentExperimentID)
	assert.Equal(t, source.LineageRootID, clone.LineageRootID)
	assert.Equal(t, source.Generation+1, clone.Generation)
	assert.Equal(t, SelectionDecision(""), clone.EvolutionDecision)
	assert.Equal(t, source.Prompt, clone.Prompt)
	assert.Equal(t, source.Rationale, clone.Rationale)
	assert.Empty(t, clone.Runs)
	assert.Nil(t, clone.LatestEval)
	assert.Contains(t, clone.Title, "Rerun:")
}

func TestServiceEvolveExperimentCreatesNextGenerationFromSelection(t *testing.T) {
	resetResearchTestState(t)

	svc, err := NewService()
	require.NoError(t, err)

	_, source, err := svc.AddExperimentCandidate(context.Background(), "session-7", ExperimentPlan{
		Title:  "Seed candidate",
		Prompt: "Run the seed candidate",
	})
	require.NoError(t, err)

	_, _, err = svc.EvaluateExperiment(context.Background(), "session-7", source.ID, 0.76, "mutate", "Promising but needs parameter refinement")
	require.NoError(t, err)

	state, evolved, err := svc.EvolveExperiment(context.Background(), "session-7", source.ID, "")
	require.NoError(t, err)
	assert.Equal(t, evolved.ID, state.ActiveExperimentID)
	assert.Equal(t, source.ID, evolved.ParentExperimentID)
	assert.Equal(t, source.LineageRootID, evolved.LineageRootID)
	assert.Equal(t, source.Generation+1, evolved.Generation)
	assert.Equal(t, DecisionMutate, evolved.EvolutionDecision)
	assert.Contains(t, evolved.Title, "Mutate:")
}

func TestServicePromoteExperimentMarksBestCandidate(t *testing.T) {
	resetResearchTestState(t)

	svc, err := NewService()
	require.NoError(t, err)

	_, source, err := svc.AddExperiment(context.Background(), "session-9", "Candidate A", "Run candidate A")
	require.NoError(t, err)
	_, _, err = svc.EvaluateExperiment(context.Background(), "session-9", source.ID, 0.88, "keep", "Best candidate so far")
	require.NoError(t, err)

	state, plan, err := svc.PromoteExperiment(context.Background(), "session-9", source.ID)
	require.NoError(t, err)
	assert.Equal(t, source.ID, state.PromotedExperimentID)
	assert.Equal(t, source.ID, state.ActiveExperimentID)
	assert.Equal(t, StageDecision, state.Stage)
	assert.Equal(t, source.ID, plan.ID)
}

func TestServicePromoteExperimentRejectsDiscardedCandidate(t *testing.T) {
	resetResearchTestState(t)

	svc, err := NewService()
	require.NoError(t, err)

	_, source, err := svc.AddExperiment(context.Background(), "session-10", "Candidate B", "Run candidate B")
	require.NoError(t, err)
	_, _, err = svc.EvaluateExperiment(context.Background(), "session-10", source.ID, 0.12, "discard", "Not worth continuing")
	require.NoError(t, err)

	_, _, err = svc.PromoteExperiment(context.Background(), "session-10", source.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be promoted")
}

func TestEvaluateExperimentRejectsInvalidSelectionDecision(t *testing.T) {
	resetResearchTestState(t)

	svc, err := NewService()
	require.NoError(t, err)

	_, source, err := svc.AddExperiment(context.Background(), "session-8", "Bad eval", "Run something")
	require.NoError(t, err)

	_, _, err = svc.EvaluateExperiment(context.Background(), "session-8", source.ID, 0.5, "maybe", "Unclear")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "use keep, discard, mutate, or branch")
}

func TestServiceSyncTaskArtifactsMergesByArtifactID(t *testing.T) {
	resetResearchTestState(t)

	svc, err := NewService()
	require.NoError(t, err)

	_, plan, err := svc.AddExperiment(context.Background(), "session-5", "Artifact capture", "Run artifact capture")
	require.NoError(t, err)
	_, _, err = svc.AttachTaskRun(context.Background(), "session-5", plan.ID, taskrun.Run{
		SessionID:       "task-artifacts",
		ParentSessionID: "session-5",
		Title:           "Artifact task",
		Status:          taskrun.StatusComplete,
		Detail:          "Finished",
		UpdatedAt:       100,
	})
	require.NoError(t, err)

	err = svc.SyncTaskArtifacts(context.Background(), "task-artifacts", []ArtifactRef{
		{ID: "prompt:task-artifacts", Kind: ArtifactPrompt, Label: "Prompt", Summary: "First prompt"},
		{ID: "report:task-artifacts", Kind: ArtifactReport, Label: "Result", Summary: "Initial report"},
	})
	require.NoError(t, err)
	err = svc.SyncTaskArtifacts(context.Background(), "task-artifacts", []ArtifactRef{
		{ID: "report:task-artifacts", Kind: ArtifactReport, Label: "Result", Summary: "Updated report"},
	})
	require.NoError(t, err)

	state, err := svc.Get(context.Background(), "session-5")
	require.NoError(t, err)
	require.Len(t, state.Experiments, 1)
	require.Len(t, state.Experiments[0].Runs, 1)
	require.Len(t, state.Experiments[0].Runs[0].Artifacts, 2)
	var report ArtifactRef
	for _, artifact := range state.Experiments[0].Runs[0].Artifacts {
		if artifact.ID == "report:task-artifacts" {
			report = artifact
			break
		}
	}
	assert.Equal(t, "Updated report", report.Summary)
}

func TestArtifactIndexSupportsSearchAndLookup(t *testing.T) {
	state := SessionState{
		SessionID:            "session-6",
		ActiveExperimentID:   "exp-1",
		PromotedExperimentID: "exp-1",
		Experiments: []ExperimentPlan{
			{
				ID:        "exp-1",
				Title:     "Baseline sweep",
				Status:    ExperimentEvaluated,
				UpdatedAt: 200,
				LatestEval: &EvaluationResult{
					Score:    0.91,
					Decision: "keep",
					Summary:  "Best run",
				},
				Runs: []ExperimentRun{
					{
						SessionID: "task-1",
						Title:     "Run A",
						Status:    taskrun.StatusComplete,
						Artifacts: []ArtifactRef{
							{ID: "report:task-1", Kind: ArtifactReport, Label: "Final Response", Summary: "Improved baseline", CreatedAt: 200},
							{ID: "file:task-1:solver.py", Kind: ArtifactCodeSnapshot, Label: "Modified File", Path: "solver.py", Summary: "solver.py (+10 -2)", CreatedAt: 150},
						},
					},
				},
			},
			{
				ID:        "exp-2",
				Title:     "Discarded branch",
				Status:    ExperimentFailed,
				UpdatedAt: 100,
				Runs: []ExperimentRun{
					{
						SessionID: "task-2",
						Title:     "Run B",
						Status:    taskrun.StatusFailed,
						Artifacts: []ArtifactRef{
							{ID: "timeline:task-2", Kind: ArtifactRunLog, Label: "Task Timeline", Summary: "tool error", CreatedAt: 100},
						},
					},
				},
			},
		},
	}

	index := ArtifactIndex(state)
	require.Len(t, index, 3)
	assert.Equal(t, "report:task-1", index[0].Artifact.ID)
	assert.Equal(t, "Baseline sweep", index[0].ExperimentTitle)
	require.NotNil(t, index[0].Evaluation)
	assert.Equal(t, DecisionKeep, index[0].Evaluation.Decision)

	filtered := FilterArtifactIndex(index, "baseline keep")
	require.Len(t, filtered, 2)

	record, ok := FindArtifact(index, "report:task-1")
	require.True(t, ok)
	assert.Equal(t, "Run A", record.RunTitle)

	record, ok = FindArtifact(index, "file:task-1")
	require.True(t, ok)
	assert.Equal(t, "solver.py", record.Artifact.Path)

	_, ok = FindArtifact(index, "task")
	assert.False(t, ok)
}

func resetResearchTestState(t *testing.T) {
	t.Helper()
	researchTestSetup.Do(func() {
		dir, err := os.MkdirTemp("", "scicli-research-test-*")
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
		researchTestDir = dir
	})

	err := os.Remove(filepath.Join(researchTestDir, "data", "research.json"))
	if err != nil && !os.IsNotExist(err) {
		require.NoError(t, err)
	}
}
