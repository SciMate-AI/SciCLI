package chat

import (
	"testing"

	"github.com/SciMate-AI/scicli/internal/research"
)

func TestQueuedExperimentsSkipsActiveAndPromoted(t *testing.T) {
	state := research.SessionState{
		ActiveExperimentID:   "exp-active",
		PromotedExperimentID: "exp-promoted",
		Experiments: []research.ExperimentPlan{
			{ID: "exp-active", Title: "Active", Status: research.ExperimentRunning},
			{ID: "exp-promoted", Title: "Promoted", Status: research.ExperimentEvaluated},
			{ID: "exp-queued", Title: "Queued", Status: research.ExperimentPlanned},
			{
				ID:     "exp-mut",
				Title:  "Mutate",
				Status: research.ExperimentEvaluated,
				LatestEval: &research.EvaluationResult{
					Decision: research.DecisionMutate,
				},
			},
		},
	}

	queue := queuedExperiments(state)
	if len(queue) != 2 {
		t.Fatalf("expected 2 queued experiments, got %d", len(queue))
	}
	if queue[0].ID != "exp-queued" || queue[1].ID != "exp-mut" {
		t.Fatalf("unexpected queue order: %#v", queue)
	}
}

func TestNextResearchActionSuggestsEvaluateThenEvolve(t *testing.T) {
	evaluate := nextResearchAction(research.SessionState{
		Objective:          "Improve the candidate",
		ActiveExperimentID: "exp-a",
		Experiments: []research.ExperimentPlan{
			{
				ID:     "exp-a",
				Title:  "Candidate A",
				Status: research.ExperimentCompleted,
				Runs:   []research.ExperimentRun{{ID: "run-1", SessionID: "run-1"}},
			},
		},
	})
	if evaluate != "/experiment evaluate <score> <keep|discard|mutate|branch> <summary>" {
		t.Fatalf("unexpected evaluate hint: %q", evaluate)
	}

	evolve := nextResearchAction(research.SessionState{
		Objective:          "Improve the candidate",
		ActiveExperimentID: "exp-a",
		Experiments: []research.ExperimentPlan{
			{
				ID:     "exp-a",
				Title:  "Candidate A",
				Status: research.ExperimentEvaluated,
				LatestEval: &research.EvaluationResult{
					Decision: research.DecisionMutate,
				},
			},
		},
	})
	if evolve != "/experiment evolve [title]" {
		t.Fatalf("unexpected evolve hint: %q", evolve)
	}
}

func TestLineageActionHintSuggestsPromotionWhenBestCandidateExists(t *testing.T) {
	group := research.LineageGroup{
		RootID: "exp-a",
		Plans: []research.ExperimentPlan{
			{
				ID:         "exp-a",
				Title:      "Seed",
				Generation: 0,
				LatestEval: &research.EvaluationResult{
					Score:    0.91,
					Decision: research.DecisionKeep,
				},
			},
		},
	}

	hint := lineageActionHint(research.SessionState{}, group)
	if hint != "Next /experiment promote exp-a" {
		t.Fatalf("unexpected lineage hint: %q", hint)
	}
}

func TestResearchWorkbenchActionsEnableEvaluatePromoteEvolveForMutableCandidate(t *testing.T) {
	state := research.SessionState{
		ActiveExperimentID: "exp-a1",
		Experiments: []research.ExperimentPlan{
			{
				ID:     "exp-a1",
				Title:  "Candidate A",
				Status: research.ExperimentCompleted,
				Runs:   []research.ExperimentRun{{ID: "run-1", SessionID: "run-1"}},
				LatestEval: &research.EvaluationResult{
					Score:    0.83,
					Decision: research.DecisionMutate,
				},
			},
			{
				ID:    "exp-b1",
				Title: "Candidate B",
			},
		},
	}

	actions := researchWorkbenchActions(state)
	if !actionEnabled(actions, "promote") {
		t.Fatalf("expected promote action enabled")
	}
	if !actionEnabled(actions, "evolve") {
		t.Fatalf("expected evolve action enabled")
	}
	if actionEnabled(actions, "evaluate") {
		t.Fatalf("did not expect evaluate action enabled after evaluation")
	}
	if !actionEnabled(actions, "compare") {
		t.Fatalf("expected compare action enabled")
	}
}

func TestResearchWorkbenchActionsDisableEvaluateBeforeRun(t *testing.T) {
	actions := researchWorkbenchActions(research.SessionState{
		ActiveExperimentID: "exp-a1",
		Experiments: []research.ExperimentPlan{
			{
				ID:     "exp-a1",
				Title:  "Candidate A",
				Status: research.ExperimentPlanned,
			},
		},
	})

	evaluate, ok := findAction(actions, "evaluate")
	if !ok {
		t.Fatalf("expected evaluate action")
	}
	if evaluate.Enabled {
		t.Fatalf("did not expect evaluate to be enabled")
	}
	if evaluate.Reason == "" {
		t.Fatalf("expected disabled reason")
	}
}

func findAction(actions []researchWorkbenchAction, id string) (researchWorkbenchAction, bool) {
	for _, action := range actions {
		if action.ID == id {
			return action, true
		}
	}
	return researchWorkbenchAction{}, false
}

func actionEnabled(actions []researchWorkbenchAction, id string) bool {
	action, ok := findAction(actions, id)
	return ok && action.Enabled
}
