package orchestrator

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var workerLoopStatusPattern = regexp.MustCompile(`(?is)<agent_loop_status>\s*(continue|complete)\s*</agent_loop_status>`)

type IdeaPayload struct {
	Title          string   `json:"title,omitempty"`
	Summary        string   `json:"summary,omitempty"`
	NoveltyClaim   string   `json:"novelty_claim,omitempty"`
	Hypotheses     []string `json:"hypotheses,omitempty"`
	SupportingRefs []string `json:"supporting_refs,omitempty"`
}

func (p *IdeaPayload) UnmarshalJSON(data []byte) error {
	if p == nil {
		return nil
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*p = IdeaPayload{}
		return nil
	}
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var summary string
		if err := json.Unmarshal(data, &summary); err != nil {
			return err
		}
		summary = strings.TrimSpace(summary)
		*p = IdeaPayload{
			Title:   summary,
			Summary: summary,
		}
		return nil
	}
	type alias IdeaPayload
	var out alias
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*p = IdeaPayload(out)
	return nil
}

type ObjectionPayload struct {
	IdeaTitle    string   `json:"idea_title,omitempty"`
	Summary      string   `json:"summary,omitempty"`
	Severity     string   `json:"severity,omitempty"`
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
}

func (p *ObjectionPayload) UnmarshalJSON(data []byte) error {
	if p == nil {
		return nil
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*p = ObjectionPayload{}
		return nil
	}
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var summary string
		if err := json.Unmarshal(data, &summary); err != nil {
			return err
		}
		*p = ObjectionPayload{Summary: strings.TrimSpace(summary)}
		return nil
	}
	type alias ObjectionPayload
	var out alias
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*p = ObjectionPayload(out)
	return nil
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

type ReferencePayload struct {
	Key                string   `json:"key,omitempty"`
	Title              string   `json:"title,omitempty"`
	Authors            []string `json:"authors,omitempty"`
	Year               int      `json:"year,omitempty"`
	Venue              string   `json:"venue,omitempty"`
	DOI                string   `json:"doi,omitempty"`
	URL                string   `json:"url,omitempty"`
	ZoteroKey          string   `json:"zotero_key,omitempty"`
	FormattedReference string   `json:"formatted_reference,omitempty"`
	BibTeX             string   `json:"bibtex,omitempty"`
	KeyClaim           string   `json:"key_claim,omitempty"`
}

type WorkerOutput struct {
	Status          string             `json:"status,omitempty"`
	Summary         string             `json:"summary,omitempty"`
	SuccessSignal   string             `json:"success_signal,omitempty"`
	FailureSignal   string             `json:"failure_signal,omitempty"`
	EvidenceSummary []string           `json:"evidence_summary,omitempty"`
	Citations       []string           `json:"citations,omitempty"`
	References      []ReferencePayload `json:"references,omitempty"`
	Risks           []string           `json:"risks,omitempty"`
	Ideas           []IdeaPayload      `json:"ideas,omitempty"`
	Objections      []ObjectionPayload `json:"objections,omitempty"`
	MethodPlan      *MethodPlanPayload `json:"method_plan,omitempty"`
	Execution       *ExecutionPayload  `json:"execution,omitempty"`
	Review          *ReviewPayload     `json:"review,omitempty"`
	Comparison      *ComparisonPayload `json:"comparison,omitempty"`
	// RevisionFeedback contains specific, actionable revision instructions from
	// the revision-gate node, forwarded to the paper_writer on the next draft round.
	RevisionFeedback []string `json:"revision_feedback,omitempty"`
	// RevisionDecision is set by the revision-gate node: "accept" or "revise".
	RevisionDecision string `json:"revision_decision,omitempty"`
	// OverallScore is the aggregated quality score (0–5) used by the revision gate.
	OverallScore float64 `json:"overall_score,omitempty"`
}

func ParseWorkerOutput(raw string) WorkerOutput {
	cleaned := cleanWorkerOutput(raw)
	if cleaned == "" {
		return WorkerOutput{}
	}

	var out WorkerOutput
	if err := json.Unmarshal([]byte(cleaned), &out); err == nil {
		normalizeWorkerOutput(&out)
		return out
	}

	return WorkerOutput{
		Status:  "succeeded",
		Summary: raw,
	}
}

func StrictParseWorkerOutput(raw string) (WorkerOutput, error) {
	cleaned := cleanWorkerOutput(raw)
	if cleaned == "" {
		return WorkerOutput{}, fmt.Errorf("worker output is empty")
	}

	var out WorkerOutput
	if err := json.Unmarshal([]byte(cleaned), &out); err != nil {
		return WorkerOutput{}, fmt.Errorf("worker output must be valid JSON: %w", err)
	}
	normalizeWorkerOutput(&out)
	return out, nil
}

func ValidateWorkerOutputForRole(node NodeSpec, roleID string, out WorkerOutput) error {
	status := strings.TrimSpace(out.Status)
	switch status {
	case "succeeded", "failed":
	default:
		return fmt.Errorf("status must be \"succeeded\" or \"failed\"")
	}
	if strings.TrimSpace(out.Summary) == "" {
		return fmt.Errorf("summary is required")
	}

	if err := validateWorkerSignals(node, status, out); err != nil {
		return err
	}

	switch strings.TrimSpace(roleID) {
	case "research_agent":
		if node.ID == "node-corpus-retrieval" {
			if len(out.References) < 3 || len(out.EvidenceSummary) < 3 {
				return fmt.Errorf("corpus retrieval requires at least 3 references[] and 3 evidence_summary[] entries")
			}
			for i, ref := range out.References {
				if err := validateReferencePayload(ref); err != nil {
					return fmt.Errorf("references[%d]: %w", i, err)
				}
			}
		}
	case "idea_maker":
		if node.ID == "node-idea-gate" && len(out.Ideas) == 0 {
			return fmt.Errorf("idea_maker must emit ideas[]")
		}
	case "idea_hater":
		if node.ID == "node-idea-gate" && len(out.Objections) == 0 {
			return fmt.Errorf("idea_hater must emit objections[]")
		}
	case "chief_scientist":
		switch node.ID {
		case "node-idea-gate":
			if out.SuccessSignal == "" && out.FailureSignal == "" {
				return fmt.Errorf("chief_scientist must emit a routing signal at idea gate")
			}
		case "node-revision-gate":
			switch out.RevisionDecision {
			case "accept":
				if status != "succeeded" {
					return fmt.Errorf("revision_gate accept decision requires status=succeeded")
				}
			case "revise":
				if status != "failed" {
					return fmt.Errorf("revision_gate revise decision requires status=failed")
				}
				if len(out.RevisionFeedback) == 0 {
					return fmt.Errorf("revision_gate revise decision requires revision_feedback[]")
				}
			default:
				return fmt.Errorf("revision_gate requires revision_decision=accept|revise")
			}
		}
	case "method_planner":
		if out.MethodPlan == nil || strings.TrimSpace(out.MethodPlan.Summary) == "" || len(out.MethodPlan.PipelineSteps) == 0 {
			return fmt.Errorf("method_planner requires method_plan.summary and method_plan.pipeline_steps")
		}
	case "execution_agent":
		if out.Execution == nil {
			return fmt.Errorf("execution_agent requires execution payload")
		}
	case "advisor_agent", "domain_expert_reviewer":
		if out.Review == nil {
			return fmt.Errorf("%s requires review payload", roleID)
		}
	case "judge_agent":
		if out.Review == nil {
			return fmt.Errorf("judge_agent requires review payload")
		}
		if out.Review.Scores.Overall <= 0 {
			return fmt.Errorf("judge_agent requires review.scores.overall > 0")
		}
	case "paper_comparison_reviewer":
		if out.Comparison == nil {
			return fmt.Errorf("paper_comparison_reviewer requires comparison payload")
		}
	}

	return nil
}

func cleanWorkerOutput(raw string) string {
	cleaned := strings.TrimSpace(raw)
	cleaned = strings.TrimSpace(strings.TrimPrefix(cleaned, "```json"))
	cleaned = strings.TrimSpace(strings.TrimPrefix(cleaned, "```"))
	cleaned = strings.TrimSpace(strings.TrimSuffix(cleaned, "```"))
	cleaned = strings.TrimSpace(workerLoopStatusPattern.ReplaceAllString(cleaned, ""))
	if strings.HasPrefix(cleaned, "{") && strings.HasSuffix(cleaned, "}") {
		return cleaned
	}
	start := strings.Index(cleaned, "{")
	end := strings.LastIndex(cleaned, "}")
	if start >= 0 && end > start {
		candidate := strings.TrimSpace(cleaned[start : end+1])
		if json.Valid([]byte(candidate)) {
			return candidate
		}
	}
	return cleaned
}

func normalizeWorkerOutput(out *WorkerOutput) {
	if out == nil {
		return
	}
	out.Status = strings.TrimSpace(out.Status)
	out.Summary = strings.TrimSpace(out.Summary)
	out.SuccessSignal = strings.TrimSpace(out.SuccessSignal)
	out.FailureSignal = strings.TrimSpace(out.FailureSignal)
	out.Citations = normalizePayloadStrings(out.Citations)
	out.References = normalizeReferencePayloads(out.References)
	out.EvidenceSummary = normalizePayloadStrings(out.EvidenceSummary)
	out.Risks = normalizePayloadStrings(out.Risks)
	out.RevisionFeedback = normalizePayloadStrings(out.RevisionFeedback)
	if out.MethodPlan != nil {
		out.MethodPlan.Summary = strings.TrimSpace(out.MethodPlan.Summary)
		out.MethodPlan.PipelineSteps = normalizePayloadStrings(out.MethodPlan.PipelineSteps)
		out.MethodPlan.AcceptanceChecks = normalizePayloadStrings(out.MethodPlan.AcceptanceChecks)
		out.MethodPlan.ImplementationNotes = normalizePayloadStrings(out.MethodPlan.ImplementationNotes)
		out.MethodPlan.RuntimeHints = normalizePayloadStrings(out.MethodPlan.RuntimeHints)
	}
	if out.Execution != nil {
		out.Execution.RuntimeID = strings.TrimSpace(out.Execution.RuntimeID)
		out.Execution.Commands = normalizePayloadStrings(out.Execution.Commands)
		out.Execution.VerificationSummary = strings.TrimSpace(out.Execution.VerificationSummary)
		out.Execution.StdoutExcerpt = strings.TrimSpace(out.Execution.StdoutExcerpt)
		out.Execution.StderrExcerpt = strings.TrimSpace(out.Execution.StderrExcerpt)
		out.Execution.OutputFiles = normalizePayloadStrings(out.Execution.OutputFiles)
		out.Execution.LogHighlights = normalizePayloadStrings(out.Execution.LogHighlights)
	}
	if out.Review != nil {
		out.Review.Decision = strings.TrimSpace(out.Review.Decision)
		out.Review.Summary = strings.TrimSpace(out.Review.Summary)
		out.Review.Strengths = normalizePayloadStrings(out.Review.Strengths)
		out.Review.Weaknesses = normalizePayloadStrings(out.Review.Weaknesses)
		out.Review.Questions = normalizePayloadStrings(out.Review.Questions)
	}
	if out.Comparison != nil {
		out.Comparison.Summary = strings.TrimSpace(out.Comparison.Summary)
		out.Comparison.Strengths = normalizePayloadStrings(out.Comparison.Strengths)
		out.Comparison.Weaknesses = normalizePayloadStrings(out.Comparison.Weaknesses)
	}
}

func validateWorkerSignals(node NodeSpec, status string, out WorkerOutput) error {
	successSignal := strings.TrimSpace(out.SuccessSignal)
	failureSignal := strings.TrimSpace(out.FailureSignal)
	if status == "succeeded" && failureSignal != "" {
		return fmt.Errorf("succeeded output must not set failure_signal")
	}
	if status == "failed" && successSignal != "" {
		return fmt.Errorf("failed output must not set success_signal")
	}
	if failureSignal != "" && failureSignal != node.FailureSignal {
		return fmt.Errorf("failure_signal %q does not match node contract", failureSignal)
	}
	if successSignal == "" {
		return nil
	}
	if successSignal == node.SuccessSignal {
		return nil
	}
	if _, ok := node.DynamicRoutes[successSignal]; ok {
		return nil
	}
	return fmt.Errorf("success_signal %q does not match node contract", successSignal)
}

func normalizePayloadStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeReferencePayloads(items []ReferencePayload) []ReferencePayload {
	if len(items) == 0 {
		return nil
	}
	out := make([]ReferencePayload, 0, len(items))
	for _, item := range items {
		item.Key = strings.TrimSpace(item.Key)
		item.Title = strings.TrimSpace(item.Title)
		item.Authors = normalizePayloadStrings(item.Authors)
		item.Venue = strings.TrimSpace(item.Venue)
		item.DOI = strings.TrimSpace(item.DOI)
		item.URL = strings.TrimSpace(item.URL)
		item.ZoteroKey = strings.TrimSpace(item.ZoteroKey)
		item.FormattedReference = strings.TrimSpace(item.FormattedReference)
		item.BibTeX = strings.TrimSpace(item.BibTeX)
		item.KeyClaim = strings.TrimSpace(item.KeyClaim)
		if item.Key == "" && item.ZoteroKey != "" {
			item.Key = item.ZoteroKey
		}
		if item.Title == "" && item.FormattedReference == "" && item.BibTeX == "" {
			continue
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func validateReferencePayload(ref ReferencePayload) error {
	if strings.TrimSpace(ref.Key) == "" {
		return fmt.Errorf("key is required")
	}
	if strings.TrimSpace(ref.Title) == "" {
		return fmt.Errorf("title is required")
	}
	if len(normalizePayloadStrings(ref.Authors)) == 0 {
		return fmt.Errorf("authors[] is required")
	}
	if ref.Year <= 0 {
		return fmt.Errorf("year must be > 0")
	}
	if strings.TrimSpace(ref.FormattedReference) == "" {
		return fmt.Errorf("formatted_reference is required")
	}
	if strings.TrimSpace(ref.BibTeX) == "" {
		return fmt.Errorf("bibtex is required")
	}
	if strings.TrimSpace(ref.DOI) == "" && strings.TrimSpace(ref.URL) == "" && strings.TrimSpace(ref.ZoteroKey) == "" {
		return fmt.Errorf("one of doi, url, or zotero_key is required")
	}
	return nil
}
