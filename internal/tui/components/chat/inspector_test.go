package chat

import (
	"strings"
	"testing"

	"github.com/SciMate-AI/scicli/internal/message"
	"github.com/SciMate-AI/scicli/internal/research"
	"github.com/SciMate-AI/scicli/internal/taskrun"
	zone "github.com/lrstanley/bubblezone"
)

func TestDeriveRunStatusRunningWhenAssistantUnfinished(t *testing.T) {
	msgs := []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "do work"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "partial"}}},
	}

	status, detail := deriveRunStatus(msgs)
	if status != taskrun.StatusRunning {
		t.Fatalf("expected running, got %q", status)
	}
	if detail == "" {
		t.Fatalf("expected detail")
	}
}

func TestDeriveRunStatusCompleteWhenAssistantEndsTurn(t *testing.T) {
	msgs := []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "finish"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{
			message.TextContent{Text: "done"},
			message.Finish{Reason: message.FinishReasonEndTurn},
		}},
	}

	status, _ := deriveRunStatus(msgs)
	if status != taskrun.StatusComplete {
		t.Fatalf("expected complete, got %q", status)
	}
}

func TestDeriveRunStatusBlockedWhenPermissionDenied(t *testing.T) {
	msgs := []message.Message{
		{Role: message.Assistant, Parts: []message.ContentPart{
			message.Finish{Reason: message.FinishReasonPermissionDenied},
		}},
	}

	status, _ := deriveRunStatus(msgs)
	if status != taskrun.StatusBlocked {
		t.Fatalf("expected blocked, got %q", status)
	}
}

func TestTimelineSnapshotMatchesConsoleQueryMetadata(t *testing.T) {
	snapshot := timelineSnapshot{
		ToolName: "bash",
		Detail:   "Waiting on permission",
		Metadata: taskrun.EventMetadata{
			ToolInputPreview: "{\"command\":\"npm publish\"}",
			FinishReason:     "permission_denied",
			PermissionReason: "Permission approval required before running bash",
		},
	}

	if !snapshot.matchesConsoleQuery("npm publish") {
		t.Fatalf("expected tool input preview to match query")
	}
	if !snapshot.matchesConsoleQuery("approval bash") {
		t.Fatalf("expected permission reason to match query")
	}
	if snapshot.matchesConsoleQuery("python") {
		t.Fatalf("did not expect unrelated query to match")
	}
}

func TestTimelineSnapshotMetadataLine(t *testing.T) {
	snapshot := timelineSnapshot{
		Metadata: taskrun.EventMetadata{
			ToolInputPreview: "{\"command\":\"go test ./...\"}",
			FinishReason:     "end_turn",
		},
	}

	line := snapshot.MetadataLine(80)
	if line == "" {
		t.Fatalf("expected metadata line to render")
	}
	if !containsAll(line, []string{"input", "go test", "finish end_turn"}) {
		t.Fatalf("unexpected metadata line: %q", line)
	}
}

func TestRenderLineageSectionShowsPromotedAndActive(t *testing.T) {
	zone.NewGlobal()
	cmp := &inspectorCmp{
		width:  80,
		height: 40,
		research: research.SessionState{
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
						Score:    0.91,
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
			},
		},
	}

	view := cmp.renderLineageSection(80)
	if !containsAll(view, []string{
		"Lineage",
		"Root exp-a1 Seed A [PROMOTED, ACTIVE-LINEAGE] | best 0.91 | 2 generations",
		"Current exp-a2 G1 Mutated A [ACTIVE, MUTATE]",
		"Next /experiment evolve [title]",
	}) {
		t.Fatalf("unexpected lineage view: %q", view)
	}
}

func TestRenderActionSectionShowsClickableActionLabels(t *testing.T) {
	zone.NewGlobal()
	cmp := &inspectorCmp{
		width:  80,
		height: 40,
		research: research.SessionState{
			ActiveExperimentID: "exp-a1",
			Experiments: []research.ExperimentPlan{
				{
					ID:     "exp-a1",
					Title:  "Candidate A",
					Status: research.ExperimentEvaluated,
					Runs:   []research.ExperimentRun{{ID: "run-1", SessionID: "run-1"}},
					LatestEval: &research.EvaluationResult{
						Score:    0.86,
						Decision: research.DecisionMutate,
					},
				},
				{
					ID:    "exp-b1",
					Title: "Candidate B",
				},
			},
		},
	}

	view := cmp.renderActionSection(80)
	if !containsAll(view, []string{"Actions", "Evaluate", "Promote", "Evolve", "Compare"}) {
		t.Fatalf("unexpected action section: %q", view)
	}
}

func containsAll(text string, parts []string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}
