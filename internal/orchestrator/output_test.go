package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseWorkerOutputParsesJSON(t *testing.T) {
	out := ParseWorkerOutput(`{"status":"succeeded","summary":"done","success_signal":"evidence_ready","citations":["ref-1"],"method_plan":{"summary":"plan","pipeline_steps":["step1"]},"execution":{"runtime_id":"docker.python-sci.v1","commands":["python train.py"],"verification_summary":"smoke test passed","verification_passed":true,"output_files":["results.json"]},"review":{"decision":"weak_accept","summary":"Readable draft","scores":{"overall":3.8,"writing_quality":4.2},"readable_paper":true},"comparison":{"summary":"Close on methods","motivation_alignment":4.0,"methodology_alignment":3.5}}`)
	assert.Equal(t, "succeeded", out.Status)
	assert.Equal(t, "done", out.Summary)
	assert.Equal(t, "evidence_ready", out.SuccessSignal)
	assert.Equal(t, []string{"ref-1"}, out.Citations)
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
