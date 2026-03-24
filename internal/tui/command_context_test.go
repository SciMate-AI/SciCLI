package tui

import (
	"testing"

	"github.com/SciMate-AI/scicli/internal/research"
	"github.com/SciMate-AI/scicli/internal/tui/components/dialog"
)

func TestContextualizeCommandRecommendsEvolveForMutableActiveExperiment(t *testing.T) {
	cmd := contextualizeCommand(dialog.Command{
		ID:          "evolve-active-experiment",
		Title:       "Evolve Active Experiment",
		Description: "base",
	}, researchCommandContext{
		state: research.SessionState{
			ActiveExperimentID: "exp-a1",
		},
		active: research.ExperimentPlan{
			ID: "exp-a1",
			LatestEval: &research.EvaluationResult{
				Score:    0.87,
				Decision: research.DecisionMutate,
			},
		},
		hasActive: true,
	})

	if !cmd.Recommended {
		t.Fatalf("expected evolve command to be recommended")
	}
	if cmd.Disabled {
		t.Fatalf("did not expect evolve command to be disabled")
	}
	if cmd.Boost <= 0 {
		t.Fatalf("expected positive boost")
	}
}

func TestContextualizeCommandDisablesPromoteForDiscardedActiveExperiment(t *testing.T) {
	cmd := contextualizeCommand(dialog.Command{
		ID:          "promote-active-experiment",
		Title:       "Promote Active Experiment",
		Description: "base",
	}, researchCommandContext{
		state: research.SessionState{
			ActiveExperimentID: "exp-a1",
		},
		active: research.ExperimentPlan{
			ID: "exp-a1",
			LatestEval: &research.EvaluationResult{
				Score:    0.11,
				Decision: research.DecisionDiscard,
			},
		},
		hasActive: true,
	})

	if !cmd.Disabled {
		t.Fatalf("expected promote command to be disabled")
	}
	if cmd.Reason == "" {
		t.Fatalf("expected disabled reason")
	}
}

func TestCompareContextualCommandsOrdersRecommendedEnabledFirst(t *testing.T) {
	commands := []dialog.Command{
		{ID: "disabled", Title: "Disabled", Disabled: true, Boost: 999},
		{ID: "plain", Title: "Plain", Boost: 10},
		{ID: "recommended", Title: "Recommended", Recommended: true, Boost: 20},
	}

	sorted := append([]dialog.Command{}, commands...)
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if compareContextualCommands(sorted[j], sorted[i]) < 0 {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	if sorted[0].ID != "recommended" {
		t.Fatalf("expected recommended command first, got %q", sorted[0].ID)
	}
	if sorted[len(sorted)-1].ID != "disabled" {
		t.Fatalf("expected disabled command last, got %q", sorted[len(sorted)-1].ID)
	}
}
