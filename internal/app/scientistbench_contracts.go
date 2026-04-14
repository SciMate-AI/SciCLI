package app

import (
	"strconv"
	"strings"

	"github.com/SciMate-AI/scicli/internal/orchestrator"
	"github.com/SciMate-AI/scicli/internal/scientistbench"
)

type scientistBenchNodeContract struct {
	NodeID            string
	PhaseID           string
	WritableSlices    []string
	CompletionUpdates []string
}

type scientistBenchRoleReducer struct {
	NodeID         string
	RoleID         string
	RequiredUpdate string
	WritableSlices []string
	Reduce         func(*scientistbench.Case, scientistbench.RunRecord, orchestrator.WorkerOutput, int64)
}

func lookupScientistBenchNodeContract(nodeID string) (scientistBenchNodeContract, bool) {
	contract, ok := scientistBenchNodeContracts()[strings.TrimSpace(nodeID)]
	return contract, ok
}

func scientistBenchRoleReducerFor(nodeID, roleID string) (scientistBenchRoleReducer, bool) {
	reducer, ok := scientistBenchRoleReducers()[strings.TrimSpace(nodeID)+"::"+strings.TrimSpace(roleID)]
	return reducer, ok
}

func scientistBenchNodeContracts() map[string]scientistBenchNodeContract {
	return map[string]scientistBenchNodeContract{
		"node-case-intake":      {NodeID: "node-case-intake", PhaseID: "planning", WritableSlices: []string{"workflow", "termination"}, CompletionUpdates: []string{scientistBenchToolRouteDecision}},
		"node-research-plan":    {NodeID: "node-research-plan", PhaseID: "research", WritableSlices: []string{"artifacts", "memories"}, CompletionUpdates: []string{scientistBenchToolResearchPack}},
		"node-corpus-retrieval": {NodeID: "node-corpus-retrieval", PhaseID: "research", WritableSlices: []string{"artifacts", "memories"}, CompletionUpdates: []string{scientistBenchToolResearchPack}},
		"node-idea-gate":        {NodeID: "node-idea-gate", PhaseID: "ideation", WritableSlices: []string{"idea_module", "artifacts", "memories"}, CompletionUpdates: []string{scientistBenchToolIdeas, scientistBenchToolObjections, scientistBenchToolRouteDecision}},
		"node-method-plan":      {NodeID: "node-method-plan", PhaseID: "method", WritableSlices: []string{"idea_module", "artifacts", "memories"}, CompletionUpdates: []string{scientistBenchToolMethodPlan}},
		"node-implementation":   {NodeID: "node-implementation", PhaseID: "implementation", WritableSlices: []string{"artifacts", "memories"}, CompletionUpdates: []string{scientistBenchToolArtifact}},
		"node-execution":        {NodeID: "node-execution", PhaseID: "implementation", WritableSlices: []string{"artifacts", "scores", "memories"}, CompletionUpdates: []string{scientistBenchToolExecutionReport}},
		"node-analysis-figures": {NodeID: "node-analysis-figures", PhaseID: "implementation", WritableSlices: []string{"artifacts", "memories"}, CompletionUpdates: []string{scientistBenchToolArtifact}},
		"node-paper-draft":      {NodeID: "node-paper-draft", PhaseID: "paper_writing", WritableSlices: []string{"artifacts", "memories"}, CompletionUpdates: []string{scientistBenchToolArtifact}},
		"node-advisor-review":   {NodeID: "node-advisor-review", PhaseID: "peer_review", WritableSlices: []string{"reviews", "scores", "memories"}, CompletionUpdates: []string{scientistBenchToolReview}},
		"node-judge-review":     {NodeID: "node-judge-review", PhaseID: "peer_review", WritableSlices: []string{"reviews", "scores", "memories"}, CompletionUpdates: []string{scientistBenchToolReview}},
		"node-domain-review":    {NodeID: "node-domain-review", PhaseID: "peer_review", WritableSlices: []string{"reviews", "scores", "memories"}, CompletionUpdates: []string{scientistBenchToolReview}},
		"node-paper-compare":    {NodeID: "node-paper-compare", PhaseID: "peer_review", WritableSlices: []string{"reviews", "scores", "memories"}, CompletionUpdates: []string{scientistBenchToolComparison}},
		"node-revision-gate":    {NodeID: "node-revision-gate", PhaseID: "peer_review", WritableSlices: []string{"reviews", "memories"}, CompletionUpdates: []string{scientistBenchToolRouteDecision}},
		"node-aggregate":        {NodeID: "node-aggregate", PhaseID: "aggregation", WritableSlices: []string{"termination", "scores", "memories"}, CompletionUpdates: []string{scientistBenchToolRouteDecision}},
	}
}

func scientistBenchRoleReducers() map[string]scientistBenchRoleReducer {
	return map[string]scientistBenchRoleReducer{
		"node-idea-gate::idea_maker": {
			NodeID: "node-idea-gate", RoleID: "idea_maker", RequiredUpdate: scientistBenchToolIdeas, WritableSlices: []string{"idea_module"},
			Reduce: reduceScientistBenchIdeaMaker,
		},
		"node-idea-gate::idea_hater": {
			NodeID: "node-idea-gate", RoleID: "idea_hater", RequiredUpdate: scientistBenchToolObjections, WritableSlices: []string{"idea_module"},
			Reduce: reduceScientistBenchIdeaHater,
		},
		"node-idea-gate::chief_scientist": {
			NodeID: "node-idea-gate", RoleID: "chief_scientist", RequiredUpdate: scientistBenchToolRouteDecision, WritableSlices: []string{"idea_module"},
			Reduce: reduceScientistBenchChiefScientist,
		},
		"node-revision-gate::chief_scientist": {
			NodeID: "node-revision-gate", RoleID: "chief_scientist", RequiredUpdate: scientistBenchToolRouteDecision, WritableSlices: []string{"reviews"},
			Reduce: reduceScientistBenchChiefScientist,
		},
		"node-method-plan::method_planner": {
			NodeID: "node-method-plan", RoleID: "method_planner", RequiredUpdate: scientistBenchToolMethodPlan, WritableSlices: []string{"idea_module"},
			Reduce: reduceScientistBenchMethodPlanner,
		},
		"node-advisor-review::advisor_agent": {
			NodeID: "node-advisor-review", RoleID: "advisor_agent", RequiredUpdate: scientistBenchToolReview, WritableSlices: []string{"reviews", "scores"},
			Reduce: reduceScientistBenchReviewer,
		},
		"node-judge-review::judge_agent": {
			NodeID: "node-judge-review", RoleID: "judge_agent", RequiredUpdate: scientistBenchToolReview, WritableSlices: []string{"reviews", "scores"},
			Reduce: reduceScientistBenchReviewer,
		},
		"node-domain-review::domain_expert_reviewer": {
			NodeID: "node-domain-review", RoleID: "domain_expert_reviewer", RequiredUpdate: scientistBenchToolReview, WritableSlices: []string{"reviews", "scores"},
			Reduce: reduceScientistBenchReviewer,
		},
		"node-paper-compare::paper_comparison_reviewer": {
			NodeID: "node-paper-compare", RoleID: "paper_comparison_reviewer", RequiredUpdate: scientistBenchToolComparison, WritableSlices: []string{"reviews", "scores"},
			Reduce: reduceScientistBenchComparisonReviewer,
		},
	}
}

func reduceScientistBenchIdeaMaker(item *scientistbench.Case, run scientistbench.RunRecord, output orchestrator.WorkerOutput, now int64) {
	item.IdeaModule.Status = "drafted"
	item.IdeaModule.AcceptedIdeas = filterIdeasNotFromRun(item.IdeaModule.AcceptedIdeas, run.ID)
	for i, idea := range output.Ideas {
		item.IdeaModule.AcceptedIdeas = append(item.IdeaModule.AcceptedIdeas, scientistbench.IdeaCandidate{
			ID:             "idea-" + run.ID + "-" + strconv.Itoa(i),
			Title:          strings.TrimSpace(idea.Title),
			Summary:        strings.TrimSpace(idea.Summary),
			NoveltyClaim:   strings.TrimSpace(idea.NoveltyClaim),
			Hypotheses:     sanitizeStrings(idea.Hypotheses),
			SupportingRefs: sanitizeStrings(idea.SupportingRefs),
			CreatedAt:      now,
		})
	}
}

func reduceScientistBenchIdeaHater(item *scientistbench.Case, run scientistbench.RunRecord, output orchestrator.WorkerOutput, now int64) {
	item.IdeaModule.Status = "criticized"
	item.IdeaModule.Objections = filterObjectionsNotFromRun(item.IdeaModule.Objections, run.ID)
	for i, objection := range output.Objections {
		item.IdeaModule.Objections = append(item.IdeaModule.Objections, scientistbench.IdeaObjection{
			ID:           "obj-" + run.ID + "-" + strconv.Itoa(i),
			IdeaID:       strings.TrimSpace(objection.IdeaTitle),
			Summary:      strings.TrimSpace(objection.Summary),
			Severity:     strings.TrimSpace(objection.Severity),
			EvidenceRefs: sanitizeStrings(objection.EvidenceRefs),
			CreatedAt:    now,
		})
	}
}

func reduceScientistBenchChiefScientist(item *scientistbench.Case, run scientistbench.RunRecord, output orchestrator.WorkerOutput, now int64) {
	switch run.NodeID {
	case "node-idea-gate":
		item.IdeaModule.Status = "accepted"
		if output.FailureSignal == "idea_gate_rejected" || strings.EqualFold(output.Status, "failed") {
			item.IdeaModule.Status = "rejected"
			item.IdeaModule.RejectedIdeas = append(item.IdeaModule.RejectedIdeas, item.IdeaModule.AcceptedIdeas...)
			item.IdeaModule.AcceptedIdeas = nil
		}
	case "node-revision-gate":
		if len(output.RevisionFeedback) > 0 {
			item.Reviews = scientistBenchUpsertReviewLocal(item.Reviews, scientistbench.Review{
				ID:           "review-" + run.ID + "-revision-gate",
				Type:         scientistbench.ReviewAdvisor,
				ReviewerRole: "revision_gate",
				Decision:     output.RevisionDecision,
				Summary:      output.Summary,
				Weaknesses:   output.RevisionFeedback,
				Scores: scientistbench.ReviewScores{
					Overall: output.OverallScore,
				},
				CreatedAt: now,
			})
		}
	}
}

func reduceScientistBenchMethodPlanner(item *scientistbench.Case, _ scientistbench.RunRecord, output orchestrator.WorkerOutput, _ int64) {
	if output.MethodPlan != nil {
		item.IdeaModule.Status = "planned"
	}
}

func reduceScientistBenchReviewer(item *scientistbench.Case, run scientistbench.RunRecord, output orchestrator.WorkerOutput, now int64) {
	if output.Review == nil {
		return
	}
	item.Reviews = scientistBenchUpsertReviewLocal(item.Reviews, scientistbench.Review{
		ID:               "review-" + run.ID,
		Type:             reviewTypeForRole(run.Role),
		ReviewerRole:     run.Role,
		TargetArtifactID: reviewTargetArtifactID(*item, run.Role),
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
		item.Scores.PaperGeneration.CodeRuns = output.Review.CodeRuns || hasExecutionEvidence(*item)
		item.Scores.PaperGeneration.IdeaQuality = output.Review.Scores.IdeaQuality
		item.Scores.PaperGeneration.MethodSoundness = output.Review.Scores.MethodSoundness
		item.Scores.PaperGeneration.ResultInterpretation = output.Review.Scores.ResultInterpretation
		item.Scores.PaperGeneration.WritingQuality = output.Review.Scores.WritingQuality
		item.Scores.PaperGeneration.OverallSuccess = computePaperGenerationOverall(item.Scores.PaperGeneration, output.Review.Scores.Overall)
	}
}

func reduceScientistBenchComparisonReviewer(item *scientistbench.Case, run scientistbench.RunRecord, output orchestrator.WorkerOutput, now int64) {
	if output.Comparison == nil {
		return
	}
	item.Reviews = scientistBenchUpsertReviewLocal(item.Reviews, scientistbench.Review{
		ID:               "review-" + run.ID,
		Type:             scientistbench.ReviewPaperComparison,
		ReviewerRole:     run.Role,
		TargetArtifactID: reviewTargetArtifactID(*item, run.Role),
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
