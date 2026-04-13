package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseWorkerOutputParsesJSON(t *testing.T) {
	out := ParseWorkerOutput(`{"status":"succeeded","summary":"done","success_signal":"evidence_ready","citations":["ref-1"],"references":[{"key":"smith2024","title":"Paper","authors":["A. Smith"],"year":2024,"venue":"ICML","url":"https://example.com/paper","formatted_reference":"Smith, A. (2024). Paper. ICML.","bibtex":"@inproceedings{smith2024,title={Paper}}"}],"method_plan":{"summary":"plan","pipeline_steps":["step1"]},"execution":{"runtime_id":"docker.python-sci.v1","commands":["python train.py"],"verification_summary":"smoke test passed","verification_passed":true,"output_files":["results.json"]},"review":{"decision":"weak_accept","summary":"Readable draft","scores":{"overall":3.8,"writing_quality":4.2},"readable_paper":true},"comparison":{"summary":"Close on methods","motivation_alignment":4.0,"methodology_alignment":3.5}}`)
	assert.Equal(t, "succeeded", out.Status)
	assert.Equal(t, "done", out.Summary)
	assert.Equal(t, "evidence_ready", out.SuccessSignal)
	assert.Equal(t, []string{"ref-1"}, out.Citations)
	if assert.Len(t, out.References, 1) {
		assert.Equal(t, "smith2024", out.References[0].Key)
		assert.Equal(t, "Paper", out.References[0].Title)
	}
	if assert.NotNil(t, out.MethodPlan) {
		assert.Equal(t, "plan", out.MethodPlan.Summary)
		assert.Equal(t, []string{"step1"}, out.MethodPlan.PipelineSteps)
	}
	if assert.NotNil(t, out.Execution) {
		assert.Equal(t, "docker.python-sci.v1", out.Execution.RuntimeID)
		assert.True(t, out.Execution.VerificationPassed)
		assert.Equal(t, []string{"results.json"}, out.Execution.OutputFiles)
	}
	if assert.NotNil(t, out.Review) {
		assert.Equal(t, "weak_accept", out.Review.Decision)
		assert.True(t, out.Review.ReadablePaper)
		assert.Equal(t, 4.2, out.Review.Scores.WritingQuality)
	}
	if assert.NotNil(t, out.Comparison) {
		assert.Equal(t, "Close on methods", out.Comparison.Summary)
		assert.Equal(t, 4.0, out.Comparison.MotivationAlignment)
	}
}

func TestParseWorkerOutputFallsBackToPlainSummary(t *testing.T) {
	out := ParseWorkerOutput("plain text summary")
	assert.Equal(t, "succeeded", out.Status)
	assert.Equal(t, "plain text summary", out.Summary)
}

func TestStrictParseWorkerOutputRejectsPlainText(t *testing.T) {
	_, err := StrictParseWorkerOutput("plain text summary")
	assert.Error(t, err)
}

func TestValidateWorkerOutputForRoleRequiresCorpusEvidence(t *testing.T) {
	err := ValidateWorkerOutputForRole(
		NodeSpec{ID: "node-corpus-retrieval", SuccessSignal: "evidence_ready", FailureSignal: "missing_sources"},
		"research_agent",
		WorkerOutput{Status: "succeeded", Summary: "done", SuccessSignal: "evidence_ready"},
	)
	assert.Error(t, err)
}

func TestValidateWorkerOutputForRoleAllowsDynamicSignal(t *testing.T) {
	err := ValidateWorkerOutputForRole(
		NodeSpec{
			ID:            "node-idea-gate",
			SuccessSignal: "idea_gate_passed",
			FailureSignal: "idea_gate_rejected",
			DynamicRoutes: map[string]string{"research_insufficient": "node-corpus-retrieval"},
		},
		"chief_scientist",
		WorkerOutput{Status: "succeeded", Summary: "need more evidence", SuccessSignal: "research_insufficient"},
	)
	assert.NoError(t, err)
}

func TestValidateWorkerOutputForRoleRequiresStructuredReferences(t *testing.T) {
	err := ValidateWorkerOutputForRole(
		NodeSpec{ID: "node-corpus-retrieval", SuccessSignal: "evidence_ready", FailureSignal: "missing_sources"},
		"research_agent",
		WorkerOutput{
			Status:          "succeeded",
			Summary:         "done",
			SuccessSignal:   "evidence_ready",
			EvidenceSummary: []string{"gap", "dataset", "metric"},
			References: []ReferencePayload{
				{Key: "ref-1", Title: "Paper A", Authors: []string{"A. Smith"}, Year: 2024, URL: "https://example.com/a", FormattedReference: "Smith, A. (2024). Paper A.", BibTeX: "@article{ref-1,title={Paper A}}"},
				{Key: "ref-2", Title: "Paper B", Authors: []string{"B. Jones"}, Year: 2023, URL: "https://example.com/b", FormattedReference: "Jones, B. (2023). Paper B.", BibTeX: "@article{ref-2,title={Paper B}}"},
				{Key: "ref-3", Title: "Paper C", Authors: []string{"C. Lee"}, Year: 2022, URL: "https://example.com/c", FormattedReference: "", BibTeX: "@article{ref-3,title={Paper C}}"},
			},
		},
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "formatted_reference is required")
}
