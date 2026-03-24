package page

import (
	"testing"

	"github.com/SciMate-AI/scicli/internal/research"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseExperimentProposalParsesBareJSON(t *testing.T) {
	payload, err := parseExperimentProposal(`{"title":"Next sweep","prompt":"Run the refined sweep","rationale":"Use prior best point","strategy":"mutate"}`)
	require.NoError(t, err)
	assert.Equal(t, "Next sweep", payload.Title)
	assert.Equal(t, "Run the refined sweep", payload.Prompt)
	assert.Equal(t, "Use prior best point", payload.Rationale)
	assert.Equal(t, research.DecisionMutate, payload.Strategy)
}

func TestParseExperimentProposalParsesFencedJSON(t *testing.T) {
	payload, err := parseExperimentProposal("```json\n{\"title\":\"Next sweep\",\"prompt\":\"Run the refined sweep\",\"strategy\":\"branch\"}\n```")
	require.NoError(t, err)
	assert.Equal(t, "Next sweep", payload.Title)
	assert.Equal(t, "Run the refined sweep", payload.Prompt)
	assert.Equal(t, research.DecisionBranch, payload.Strategy)
}

func TestParseExperimentProposalRejectsMissingFields(t *testing.T) {
	_, err := parseExperimentProposal(`{"title":"Only title","strategy":"mutate"}`)
	require.Error(t, err)
}

func TestParseExperimentProposalRejectsUnsupportedStrategy(t *testing.T) {
	_, err := parseExperimentProposal(`{"title":"Next sweep","prompt":"Run it","strategy":"keep"}`)
	require.Error(t, err)
}

func TestFormatArtifactDetailIncludesProvenance(t *testing.T) {
	text := formatArtifactDetail(research.ArtifactRecord{
		Artifact: research.ArtifactRef{
			ID:      "report:task-1",
			Kind:    research.ArtifactReport,
			Label:   "Final Response",
			Summary: "Improved baseline",
		},
		ExperimentID:    "exp-1234567890",
		ExperimentTitle: "Baseline sweep",
		RunTitle:        "Run A",
		Evaluation: &research.EvaluationResult{
			Score:    0.91,
			Decision: "keep",
		},
	})

	assert.Contains(t, text, "Artifact report:task-1")
	assert.Contains(t, text, "experiment exp-12345678")
	assert.Contains(t, text, "evaluation 0.91 keep")
}

func TestShortArtifactIDTruncatesLongIDs(t *testing.T) {
	assert.Equal(t, "report:task-123456", shortArtifactID("report:task-1234567890"))
	assert.Equal(t, "short-id", shortArtifactID("short-id"))
}

func TestFormatResearchSummaryIncludesPromotedCandidate(t *testing.T) {
	text := formatResearchSummary("Research state", research.SessionState{
		Objective:            "Find the best candidate",
		Stage:                research.StageDecision,
		PromotedExperimentID: "exp-1234567890",
	})

	assert.Contains(t, text, "promoted exp-12345678")
}

func TestFormatExperimentComparisonBuildsLineageBoard(t *testing.T) {
	text := formatExperimentComparison(research.SessionState{
		ActiveExperimentID:   "exp-a2",
		PromotedExperimentID: "exp-a1",
		Experiments: []research.ExperimentPlan{
			{
				ID:            "exp-a1",
				Title:         "Seed A",
				LineageRootID: "exp-a1",
				Generation:    0,
				Status:        research.ExperimentEvaluated,
				LatestEval: &research.EvaluationResult{
					Score:    0.92,
					Decision: research.DecisionKeep,
				},
			},
			{
				ID:                 "exp-a2",
				Title:              "Mutated A",
				LineageRootID:      "exp-a1",
				ParentExperimentID: "exp-a1",
				Generation:         1,
				EvolutionDecision:  research.DecisionMutate,
				Status:             research.ExperimentEvaluated,
				LatestEval: &research.EvaluationResult{
					Score:    0.88,
					Decision: research.DecisionMutate,
				},
			},
			{
				ID:            "exp-b1",
				Title:         "Seed B",
				LineageRootID: "exp-b1",
				Generation:    0,
				Status:        research.ExperimentEvaluated,
				LatestEval: &research.EvaluationResult{
					Score:    0.51,
					Decision: research.DecisionDiscard,
				},
			},
		},
	})

	assert.Contains(t, text, "Lineage board:")
	assert.Contains(t, text, "Root exp-a1 Seed A [PROMOTED] | best 0.92")
	assert.Contains(t, text, "exp-a2 G1 Mutated A [ACTIVE, MUTATE]")
	assert.Contains(t, text, "Root exp-b1 Seed B | best 0.51")
	assert.Contains(t, text, "Actions: /experiment promote <id>")
}
