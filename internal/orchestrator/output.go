package orchestrator

import (
	"encoding/json"
	"strings"
)

type IdeaPayload struct {
	Title          string   `json:"title,omitempty"`
	Summary        string   `json:"summary,omitempty"`
	NoveltyClaim   string   `json:"novelty_claim,omitempty"`
	Hypotheses     []string `json:"hypotheses,omitempty"`
	SupportingRefs []string `json:"supporting_refs,omitempty"`
}

type ObjectionPayload struct {
	IdeaTitle    string   `json:"idea_title,omitempty"`
	Summary      string   `json:"summary,omitempty"`
	Severity     string   `json:"severity,omitempty"`
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
}

type MethodPlanPayload struct {
	Summary             string   `json:"summary,omitempty"`
	PipelineSteps       []string `json:"pipeline_steps,omitempty"`
	AcceptanceChecks    []string `json:"acceptance_checks,omitempty"`
	ImplementationNotes []string `json:"implementation_notes,omitempty"`
	RuntimeHints        []string `json:"runtime_hints,omitempty"`
}

type ExecutionPayload struct {
	RuntimeID           string   `json:"runtime_id,omitempty"`
	Commands            []string `json:"commands,omitempty"`
	VerificationSummary string   `json:"verification_summary,omitempty"`
	VerificationPassed  bool     `json:"verification_passed,omitempty"`
	Executed            bool     `json:"executed,omitempty"`
	Blocked             bool     `json:"blocked,omitempty"`
	ExitCode            int      `json:"exit_code,omitempty"`
	StdoutExcerpt       string   `json:"stdout_excerpt,omitempty"`
	StderrExcerpt       string   `json:"stderr_excerpt,omitempty"`
	OutputFiles         []string `json:"output_files,omitempty"`
	LogHighlights       []string `json:"log_highlights,omitempty"`
}

type ReviewScorePayload struct {
	Overall              float64 `json:"overall,omitempty"`
	IdeaQuality          float64 `json:"idea_quality,omitempty"`
	MethodSoundness      float64 `json:"method_soundness,omitempty"`
	ResultInterpretation float64 `json:"result_interpretation,omitempty"`
	WritingQuality       float64 `json:"writing_quality,omitempty"`
}

type ReviewPayload struct {
	Decision            string             `json:"decision,omitempty"`
	Summary             string             `json:"summary,omitempty"`
	Strengths           []string           `json:"strengths,omitempty"`
	Weaknesses          []string           `json:"weaknesses,omitempty"`
	Questions           []string           `json:"questions,omitempty"`
	Confidence          float64            `json:"confidence,omitempty"`
	ReadablePaper       bool               `json:"readable_paper,omitempty"`
	NovelInsightPresent bool               `json:"novel_insight_present,omitempty"`
	CodeRuns            bool               `json:"code_runs,omitempty"`
	Scores              ReviewScorePayload `json:"scores,omitempty"`
}

type ComparisonPayload struct {
	Summary               string   `json:"summary,omitempty"`
	Strengths             []string `json:"strengths,omitempty"`
	Weaknesses            []string `json:"weaknesses,omitempty"`
	MotivationAlignment   float64  `json:"motivation_alignment,omitempty"`
	MethodologyAlignment  float64  `json:"methodology_alignment,omitempty"`
	NoveltyAlignment      float64  `json:"novelty_alignment,omitempty"`
	ExperimentalAlignment float64  `json:"experimental_alignment,omitempty"`
	Confidence            float64  `json:"confidence,omitempty"`
}

type WorkerOutput struct {
	Status          string             `json:"status,omitempty"`
	Summary         string             `json:"summary,omitempty"`
	SuccessSignal   string             `json:"success_signal,omitempty"`
	FailureSignal   string             `json:"failure_signal,omitempty"`
	EvidenceSummary []string           `json:"evidence_summary,omitempty"`
	Citations       []string           `json:"citations,omitempty"`
	Risks           []string           `json:"risks,omitempty"`
	Ideas           []IdeaPayload      `json:"ideas,omitempty"`
	Objections      []ObjectionPayload `json:"objections,omitempty"`
	MethodPlan      *MethodPlanPayload `json:"method_plan,omitempty"`
	Execution       *ExecutionPayload  `json:"execution,omitempty"`
	Review          *ReviewPayload     `json:"review,omitempty"`
	Comparison      *ComparisonPayload `json:"comparison,omitempty"`
}

func ParseWorkerOutput(raw string) WorkerOutput {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return WorkerOutput{}
	}

	cleaned = strings.TrimSpace(strings.TrimPrefix(cleaned, "```json"))
	cleaned = strings.TrimSpace(strings.TrimPrefix(cleaned, "```"))
	cleaned = strings.TrimSpace(strings.TrimSuffix(cleaned, "```"))

	var out WorkerOutput
	if err := json.Unmarshal([]byte(cleaned), &out); err == nil {
		out.Status = strings.TrimSpace(out.Status)
		out.Summary = strings.TrimSpace(out.Summary)
		out.SuccessSignal = strings.TrimSpace(out.SuccessSignal)
		out.FailureSignal = strings.TrimSpace(out.FailureSignal)
		return out
	}

	return WorkerOutput{
		Status:  "succeeded",
		Summary: raw,
	}
}
