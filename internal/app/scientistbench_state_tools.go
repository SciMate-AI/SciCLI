package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/SciMate-AI/scicli/internal/llm/agent"
	llmtools "github.com/SciMate-AI/scicli/internal/llm/tools"
	"github.com/SciMate-AI/scicli/internal/orchestrator"
	"github.com/SciMate-AI/scicli/internal/scientistbench"
)

const (
	scientistBenchToolRouteDecision   = "scientistbench_submit_route_decision"
	scientistBenchToolResearchPack    = "scientistbench_submit_research_pack"
	scientistBenchToolIdeas           = "scientistbench_submit_ideas"
	scientistBenchToolObjections      = "scientistbench_submit_objections"
	scientistBenchToolMethodPlan      = "scientistbench_submit_method_plan"
	scientistBenchToolArtifact        = "scientistbench_submit_artifact"
	scientistBenchToolExecutionReport = "scientistbench_submit_execution_report"
	scientistBenchToolReview          = "scientistbench_submit_review"
	scientistBenchToolComparison      = "scientistbench_submit_comparison"
	scientistBenchToolMemory          = "scientistbench_record_memory"
)

type scientistBenchStateTool struct {
	info    llmtools.ToolInfo
	handler func(context.Context, llmtools.ToolCall) (llmtools.ToolResponse, error)
}

type scientistBenchToolCommon struct {
	Summary       string   `json:"summary,omitempty"`
	Status        string   `json:"status,omitempty"`
	SuccessSignal string   `json:"success_signal,omitempty"`
	FailureSignal string   `json:"failure_signal,omitempty"`
	Risks         []string `json:"risks,omitempty"`
}

type scientistBenchRouteDecisionParams struct {
	scientistBenchToolCommon
	OverallScore     float64  `json:"overall_score,omitempty"`
	RevisionDecision string   `json:"revision_decision,omitempty"`
	RevisionFeedback []string `json:"revision_feedback,omitempty"`
}

type scientistBenchResearchPackParams struct {
	scientistBenchToolCommon
	EvidenceSummary []string                        `json:"evidence_summary,omitempty"`
	Citations       []string                        `json:"citations,omitempty"`
	References      []orchestrator.ReferencePayload `json:"references,omitempty"`
}

type scientistBenchIdeasParams struct {
	scientistBenchToolCommon
	Ideas []orchestrator.IdeaPayload `json:"ideas,omitempty"`
}

type scientistBenchObjectionsParams struct {
	scientistBenchToolCommon
	Objections []orchestrator.ObjectionPayload `json:"objections,omitempty"`
}

type scientistBenchMethodPlanParams struct {
	scientistBenchToolCommon
	MethodPlan orchestrator.MethodPlanPayload `json:"method_plan"`
}

type scientistBenchArtifactParams struct {
	scientistBenchToolCommon
	ArtifactKind string            `json:"artifact_kind,omitempty"`
	Label        string            `json:"label,omitempty"`
	Path         string            `json:"path,omitempty"`
	URI          string            `json:"uri,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	QualityFlags []string          `json:"quality_flags,omitempty"`
	Citations    []string          `json:"citations,omitempty"`
}

type scientistBenchExecutionParams struct {
	scientistBenchToolCommon
	Execution orchestrator.ExecutionPayload `json:"execution"`
}

type scientistBenchReviewParams struct {
	scientistBenchToolCommon
	Review orchestrator.ReviewPayload `json:"review"`
}

type scientistBenchComparisonParams struct {
	scientistBenchToolCommon
	Comparison orchestrator.ComparisonPayload `json:"comparison"`
}

type scientistBenchMemoryParams struct {
	Kind    string   `json:"kind"`
	Summary string   `json:"summary"`
	Details []string `json:"details,omitempty"`
}

func (t *scientistBenchStateTool) Info() llmtools.ToolInfo {
	return t.info
}

func (t *scientistBenchStateTool) Run(ctx context.Context, call llmtools.ToolCall) (llmtools.ToolResponse, error) {
	if t == nil || t.handler == nil {
		return llmtools.NewTextErrorResponse("scientistbench tool is not configured"), nil
	}
	return t.handler(ctx, call)
}

func (app *App) scientistBenchWorkerTools(
	caseID string,
	run scientistbench.RunRecord,
	node orchestrator.NodeSpec,
	profile orchestrator.WorkerProfile,
) []llmtools.BaseTool {
	base := agent.RoleWorkerTools(profile.ToolProfile, app.Permissions, app.History, app.LSPClients, app.Skills)
	stateTools := app.scientistBenchStateTools(caseID, run, node, profile)
	if len(stateTools) == 0 {
		return base
	}
	return append(base, stateTools...)
}

func scientistBenchToolInfo(name, description string, parameters map[string]any, required []string) llmtools.ToolInfo {
	return llmtools.ToolInfo{
		Name:        name,
		Description: description,
		Parameters:  parameters,
		Required:    required,
	}
}

func toolStringProp(description string) map[string]any {
	return map[string]any{
		"type":        "string",
		"description": description,
	}
}

func toolStringArrayProp(description string) map[string]any {
	return map[string]any{
		"type":        "array",
		"description": description,
		"items":       map[string]any{"type": "string"},
	}
}

func toolNumberProp(description string) map[string]any {
	return map[string]any{
		"type":        "number",
		"description": description,
	}
}

func decodeScientistBenchToolInput(input string, dest any) error {
	if strings.TrimSpace(input) == "" {
		return fmt.Errorf("tool input is required")
	}
	if err := json.Unmarshal([]byte(input), dest); err != nil {
		return fmt.Errorf("invalid tool input JSON: %w", err)
	}
	return nil
}

func scientistBenchStateUpdateOpKey(call llmtools.ToolCall) string {
	name := strings.TrimSpace(call.Name)
	id := strings.TrimSpace(call.ID)
	if id == "" {
		return name
	}
	if name == "" {
		return id
	}
	return name + ":" + id
}

func (app *App) scientistBenchStateTools(
	caseID string,
	run scientistbench.RunRecord,
	node orchestrator.NodeSpec,
	profile orchestrator.WorkerProfile,
) []llmtools.BaseTool {
	makeTool := func(info llmtools.ToolInfo, handler func(context.Context, llmtools.ToolCall) (llmtools.ToolResponse, error)) llmtools.BaseTool {
		return &scientistBenchStateTool{info: info, handler: handler}
	}

	tools := make([]llmtools.BaseTool, 0, 2)
	switch strings.TrimSpace(profile.RoleID) {
	case "chief_scientist":
		tools = append(tools, makeTool(
			scientistBenchToolInfo(
				scientistBenchToolRouteDecision,
				"Commit the chief scientist's routing decision into shared ScientistBench state. Use this instead of emitting machine-readable JSON in chat.",
				map[string]any{
					"summary":           toolStringProp("Short decision summary."),
					"status":            toolStringProp(`"succeeded" or "failed". Defaults to succeeded.`),
					"success_signal":    toolStringProp("Node success signal or dynamic route signal."),
					"failure_signal":    toolStringProp("Node failure signal when rejecting or failing."),
					"overall_score":     toolNumberProp("Aggregated score for revision or aggregate decisions when relevant."),
					"revision_decision": toolStringProp(`For node-revision-gate only: "accept" or "revise".`),
					"revision_feedback": toolStringArrayProp("Specific actionable revision items."),
					"risks":             toolStringArrayProp("Key remaining risks or unresolved concerns."),
				},
				[]string{"summary"},
			),
			func(ctx context.Context, call llmtools.ToolCall) (llmtools.ToolResponse, error) {
				var params scientistBenchRouteDecisionParams
				if err := decodeScientistBenchToolInput(call.Input, &params); err != nil {
					return llmtools.NewTextErrorResponse(err.Error()), nil
				}
				output := orchestrator.WorkerOutput{
					Status:           strings.TrimSpace(params.Status),
					Summary:          strings.TrimSpace(params.Summary),
					SuccessSignal:    strings.TrimSpace(params.SuccessSignal),
					FailureSignal:    strings.TrimSpace(params.FailureSignal),
					Risks:            sanitizeStrings(params.Risks),
					OverallScore:     params.OverallScore,
					RevisionDecision: strings.TrimSpace(params.RevisionDecision),
					RevisionFeedback: sanitizeStrings(params.RevisionFeedback),
				}
				details := sanitizeStrings(append(output.RevisionFeedback, output.SuccessSignal, output.FailureSignal))
				return app.submitScientistBenchToolOutput(ctx, caseID, run.ID, node, scientistBenchToolRouteDecision, scientistBenchStateUpdateOpKey(call), output, details, nil, nil)
			},
		))
	case "research_agent":
		tools = append(tools, makeTool(
			scientistBenchToolInfo(
				scientistBenchToolResearchPack,
				"Commit the research evidence pack into shared ScientistBench state. This is the machine-readable deliverable for research nodes.",
				map[string]any{
					"summary":          toolStringProp("Short evidence-pack summary."),
					"status":           toolStringProp(`"succeeded" or "failed". Defaults to succeeded.`),
					"success_signal":   toolStringProp("Optional explicit node success signal."),
					"failure_signal":   toolStringProp("Optional explicit node failure signal."),
					"evidence_summary": toolStringArrayProp("Concrete evidence bullets."),
					"citations":        toolStringArrayProp("Human-readable citations."),
					"references": map[string]any{
						"type":        "array",
						"description": "Normalized bibliography records.",
						"items":       map[string]any{"type": "object"},
					},
					"risks": toolStringArrayProp("Research or reproducibility risks."),
				},
				[]string{"summary", "evidence_summary", "references"},
			),
			func(ctx context.Context, call llmtools.ToolCall) (llmtools.ToolResponse, error) {
				var params scientistBenchResearchPackParams
				if err := decodeScientistBenchToolInput(call.Input, &params); err != nil {
					return llmtools.NewTextErrorResponse(err.Error()), nil
				}
				output := orchestrator.WorkerOutput{
					Status:          strings.TrimSpace(params.Status),
					Summary:         strings.TrimSpace(params.Summary),
					SuccessSignal:   strings.TrimSpace(params.SuccessSignal),
					FailureSignal:   strings.TrimSpace(params.FailureSignal),
					EvidenceSummary: sanitizeStrings(params.EvidenceSummary),
					Citations:       sanitizeStrings(params.Citations),
					References:      params.References,
					Risks:           sanitizeStrings(params.Risks),
				}
				details := append([]string{}, output.EvidenceSummary...)
				for _, ref := range output.References {
					if title := strings.TrimSpace(ref.Title); title != "" {
						details = append(details, title)
					}
				}
				return app.submitScientistBenchToolOutput(ctx, caseID, run.ID, node, scientistBenchToolResearchPack, scientistBenchStateUpdateOpKey(call), output, sanitizeStrings(details), nil, nil)
			},
		))
	case "idea_maker":
		tools = append(tools, makeTool(
			scientistBenchToolInfo(
				scientistBenchToolIdeas,
				"Commit the candidate idea set into shared ScientistBench state.",
				map[string]any{
					"summary":        toolStringProp("Short idea-set summary."),
					"status":         toolStringProp(`"succeeded" or "failed". Defaults to succeeded.`),
					"success_signal": toolStringProp("Optional explicit node success signal."),
					"failure_signal": toolStringProp("Optional explicit node failure signal."),
					"ideas": map[string]any{
						"type":        "array",
						"description": "Idea objects with title, summary, novelty_claim, hypotheses, supporting_refs.",
						"items":       map[string]any{"type": "object"},
					},
					"risks": toolStringArrayProp("Key risks in the idea set."),
				},
				[]string{"summary", "ideas"},
			),
			func(ctx context.Context, call llmtools.ToolCall) (llmtools.ToolResponse, error) {
				var params scientistBenchIdeasParams
				if err := decodeScientistBenchToolInput(call.Input, &params); err != nil {
					return llmtools.NewTextErrorResponse(err.Error()), nil
				}
				output := orchestrator.WorkerOutput{
					Status:        strings.TrimSpace(params.Status),
					Summary:       strings.TrimSpace(params.Summary),
					SuccessSignal: strings.TrimSpace(params.SuccessSignal),
					FailureSignal: strings.TrimSpace(params.FailureSignal),
					Ideas:         params.Ideas,
					Risks:         sanitizeStrings(params.Risks),
				}
				details := make([]string, 0, len(output.Ideas))
				for _, idea := range output.Ideas {
					details = append(details, strings.TrimSpace(idea.Title))
				}
				return app.submitScientistBenchToolOutput(ctx, caseID, run.ID, node, scientistBenchToolIdeas, scientistBenchStateUpdateOpKey(call), output, sanitizeStrings(details), nil, nil)
			},
		))
	case "idea_hater":
		tools = append(tools, makeTool(
			scientistBenchToolInfo(
				scientistBenchToolObjections,
				"Commit the objection set into shared ScientistBench state.",
				map[string]any{
					"summary":        toolStringProp("Short critique summary."),
					"status":         toolStringProp(`"succeeded" or "failed". Defaults to succeeded.`),
					"success_signal": toolStringProp("Optional explicit node success signal."),
					"failure_signal": toolStringProp("Optional explicit node failure signal."),
					"objections": map[string]any{
						"type":        "array",
						"description": "Objection objects with idea_title, summary, severity, evidence_refs.",
						"items":       map[string]any{"type": "object"},
					},
					"risks": toolStringArrayProp("Residual risks."),
				},
				[]string{"summary", "objections"},
			),
			func(ctx context.Context, call llmtools.ToolCall) (llmtools.ToolResponse, error) {
				var params scientistBenchObjectionsParams
				if err := decodeScientistBenchToolInput(call.Input, &params); err != nil {
					return llmtools.NewTextErrorResponse(err.Error()), nil
				}
				output := orchestrator.WorkerOutput{
					Status:        strings.TrimSpace(params.Status),
					Summary:       strings.TrimSpace(params.Summary),
					SuccessSignal: strings.TrimSpace(params.SuccessSignal),
					FailureSignal: strings.TrimSpace(params.FailureSignal),
					Objections:    params.Objections,
					Risks:         sanitizeStrings(params.Risks),
				}
				details := make([]string, 0, len(output.Objections))
				for _, objection := range output.Objections {
					details = append(details, strings.TrimSpace(objection.Summary))
				}
				return app.submitScientistBenchToolOutput(ctx, caseID, run.ID, node, scientistBenchToolObjections, scientistBenchStateUpdateOpKey(call), output, sanitizeStrings(details), nil, nil)
			},
		))
	case "method_planner":
		tools = append(tools, makeTool(
			scientistBenchToolInfo(
				scientistBenchToolMethodPlan,
				"Commit the method plan into shared ScientistBench state.",
				map[string]any{
					"summary":        toolStringProp("Short method-plan summary."),
					"status":         toolStringProp(`"succeeded" or "failed". Defaults to succeeded.`),
					"success_signal": toolStringProp("Optional explicit node success signal."),
					"failure_signal": toolStringProp("Optional explicit node failure signal."),
					"method_plan": map[string]any{
						"type":        "object",
						"description": "Method plan with summary, pipeline_steps, acceptance_checks, implementation_notes, runtime_hints.",
					},
					"risks": toolStringArrayProp("Key implementation risks."),
				},
				[]string{"summary", "method_plan"},
			),
			func(ctx context.Context, call llmtools.ToolCall) (llmtools.ToolResponse, error) {
				var params scientistBenchMethodPlanParams
				if err := decodeScientistBenchToolInput(call.Input, &params); err != nil {
					return llmtools.NewTextErrorResponse(err.Error()), nil
				}
				output := orchestrator.WorkerOutput{
					Status:        strings.TrimSpace(params.Status),
					Summary:       strings.TrimSpace(params.Summary),
					SuccessSignal: strings.TrimSpace(params.SuccessSignal),
					FailureSignal: strings.TrimSpace(params.FailureSignal),
					MethodPlan:    &params.MethodPlan,
					Risks:         sanitizeStrings(params.Risks),
				}
				details := append([]string{}, params.MethodPlan.PipelineSteps...)
				details = append(details, params.MethodPlan.AcceptanceChecks...)
				return app.submitScientistBenchToolOutput(ctx, caseID, run.ID, node, scientistBenchToolMethodPlan, scientistBenchStateUpdateOpKey(call), output, sanitizeStrings(details), nil, nil)
			},
		))
	case "execution_agent":
		tools = append(tools, makeTool(
			scientistBenchToolInfo(
				scientistBenchToolExecutionReport,
				"Commit the execution report into shared ScientistBench state. This is the machine-readable deliverable for runtime validation.",
				map[string]any{
					"summary":        toolStringProp("Short execution summary."),
					"status":         toolStringProp(`"succeeded" or "failed". Defaults to succeeded.`),
					"success_signal": toolStringProp("Optional explicit node success signal."),
					"failure_signal": toolStringProp("Optional explicit node failure signal."),
					"execution": map[string]any{
						"type":        "object",
						"description": "Execution payload with runtime_id, commands, verification_summary, verification_passed, output_files, log_highlights, stdout/stderr excerpts.",
					},
					"risks": toolStringArrayProp("Key blockers or runtime risks."),
				},
				[]string{"summary", "execution"},
			),
			func(ctx context.Context, call llmtools.ToolCall) (llmtools.ToolResponse, error) {
				var params scientistBenchExecutionParams
				if err := decodeScientistBenchToolInput(call.Input, &params); err != nil {
					return llmtools.NewTextErrorResponse(err.Error()), nil
				}
				output := orchestrator.WorkerOutput{
					Status:        strings.TrimSpace(params.Status),
					Summary:       strings.TrimSpace(params.Summary),
					SuccessSignal: strings.TrimSpace(params.SuccessSignal),
					FailureSignal: strings.TrimSpace(params.FailureSignal),
					Execution:     &params.Execution,
					Risks:         sanitizeStrings(params.Risks),
				}
				details := append([]string{}, params.Execution.OutputFiles...)
				details = append(details, params.Execution.LogHighlights...)
				details = append(details, params.Execution.VerificationSummary)
				return app.submitScientistBenchToolOutput(ctx, caseID, run.ID, node, scientistBenchToolExecutionReport, scientistBenchStateUpdateOpKey(call), output, sanitizeStrings(details), nil, nil)
			},
		))
	case "advisor_agent", "judge_agent", "domain_expert_reviewer":
		tools = append(tools, makeTool(
			scientistBenchToolInfo(
				scientistBenchToolReview,
				"Commit the review payload into shared ScientistBench state.",
				map[string]any{
					"summary":        toolStringProp("Short review summary."),
					"status":         toolStringProp(`"succeeded" or "failed". Defaults to succeeded.`),
					"success_signal": toolStringProp("Optional explicit node success signal."),
					"failure_signal": toolStringProp("Optional explicit node failure signal."),
					"review": map[string]any{
						"type":        "object",
						"description": "Review payload with decision, summary, strengths, weaknesses, questions, confidence, readability flags, and scores.",
					},
					"risks": toolStringArrayProp("Critical review concerns."),
				},
				[]string{"summary", "review"},
			),
			func(ctx context.Context, call llmtools.ToolCall) (llmtools.ToolResponse, error) {
				var params scientistBenchReviewParams
				if err := decodeScientistBenchToolInput(call.Input, &params); err != nil {
					return llmtools.NewTextErrorResponse(err.Error()), nil
				}
				output := orchestrator.WorkerOutput{
					Status:        strings.TrimSpace(params.Status),
					Summary:       strings.TrimSpace(params.Summary),
					SuccessSignal: strings.TrimSpace(params.SuccessSignal),
					FailureSignal: strings.TrimSpace(params.FailureSignal),
					Review:        &params.Review,
					Risks:         sanitizeStrings(params.Risks),
				}
				details := append([]string{}, params.Review.Weaknesses...)
				details = append(details, params.Review.Questions...)
				return app.submitScientistBenchToolOutput(ctx, caseID, run.ID, node, scientistBenchToolReview, scientistBenchStateUpdateOpKey(call), output, sanitizeStrings(details), nil, nil)
			},
		))
	case "paper_comparison_reviewer":
		tools = append(tools, makeTool(
			scientistBenchToolInfo(
				scientistBenchToolComparison,
				"Commit the paper-comparison payload into shared ScientistBench state.",
				map[string]any{
					"summary":        toolStringProp("Short comparison summary."),
					"status":         toolStringProp(`"succeeded" or "failed". Defaults to succeeded.`),
					"success_signal": toolStringProp("Optional explicit node success signal."),
					"failure_signal": toolStringProp("Optional explicit node failure signal."),
					"comparison": map[string]any{
						"type":        "object",
						"description": "Comparison payload with alignment scores, strengths, weaknesses, and confidence.",
					},
					"risks": toolStringArrayProp("Key alignment gaps or blockers."),
				},
				[]string{"summary", "comparison"},
			),
			func(ctx context.Context, call llmtools.ToolCall) (llmtools.ToolResponse, error) {
				var params scientistBenchComparisonParams
				if err := decodeScientistBenchToolInput(call.Input, &params); err != nil {
					return llmtools.NewTextErrorResponse(err.Error()), nil
				}
				output := orchestrator.WorkerOutput{
					Status:        strings.TrimSpace(params.Status),
					Summary:       strings.TrimSpace(params.Summary),
					SuccessSignal: strings.TrimSpace(params.SuccessSignal),
					FailureSignal: strings.TrimSpace(params.FailureSignal),
					Comparison:    &params.Comparison,
					Risks:         sanitizeStrings(params.Risks),
				}
				details := []string{
					fmt.Sprintf("motivation=%.2f", params.Comparison.MotivationAlignment),
					fmt.Sprintf("methodology=%.2f", params.Comparison.MethodologyAlignment),
					fmt.Sprintf("novelty=%.2f", params.Comparison.NoveltyAlignment),
					fmt.Sprintf("experimental=%.2f", params.Comparison.ExperimentalAlignment),
				}
				return app.submitScientistBenchToolOutput(ctx, caseID, run.ID, node, scientistBenchToolComparison, scientistBenchStateUpdateOpKey(call), output, details, nil, nil)
			},
		))
	default:
		tools = append(tools, makeTool(
			scientistBenchToolInfo(
				scientistBenchToolArtifact,
				"Commit the role output as an artifact plus node status into shared ScientistBench state. Use this for code, paper, and figure producing roles.",
				map[string]any{
					"summary":        toolStringProp("Short artifact summary."),
					"status":         toolStringProp(`"succeeded" or "failed". Defaults to succeeded.`),
					"success_signal": toolStringProp("Optional explicit node success signal."),
					"failure_signal": toolStringProp("Optional explicit node failure signal."),
					"artifact_kind":  toolStringProp("Artifact kind, e.g. code, paper_draft, figure, log."),
					"label":          toolStringProp("Artifact label."),
					"path":           toolStringProp("Filesystem path if produced."),
					"uri":            toolStringProp("Optional URI."),
					"metadata": map[string]any{
						"type":        "object",
						"description": "Artifact metadata map.",
					},
					"quality_flags": toolStringArrayProp("Artifact quality flags."),
					"citations":     toolStringArrayProp("Optional citations to carry into artifact metadata."),
					"risks":         toolStringArrayProp("Residual risks or blockers."),
				},
				[]string{"summary"},
			),
			func(ctx context.Context, call llmtools.ToolCall) (llmtools.ToolResponse, error) {
				var params scientistBenchArtifactParams
				if err := decodeScientistBenchToolInput(call.Input, &params); err != nil {
					return llmtools.NewTextErrorResponse(err.Error()), nil
				}
				output := orchestrator.WorkerOutput{
					Status:        strings.TrimSpace(params.Status),
					Summary:       strings.TrimSpace(params.Summary),
					SuccessSignal: strings.TrimSpace(params.SuccessSignal),
					FailureSignal: strings.TrimSpace(params.FailureSignal),
					Citations:     sanitizeStrings(params.Citations),
					Risks:         sanitizeStrings(params.Risks),
				}
				artifact := &scientistbench.Artifact{
					Kind:         scientistbench.ArtifactKind(strings.TrimSpace(params.ArtifactKind)),
					Label:        strings.TrimSpace(params.Label),
					Path:         strings.TrimSpace(params.Path),
					URI:          strings.TrimSpace(params.URI),
					Metadata:     normalizeScientistBenchArtifactMetadata(params.Metadata),
					QualityFlags: sanitizeStrings(params.QualityFlags),
				}
				if artifact.Kind == "" {
					artifact.Kind = artifactKindForNode(node)
				}
				if artifact.Label == "" {
					artifact.Label = artifactLabelForNode(node)
				}
				details := sanitizeStrings([]string{artifact.Label, artifact.Path, artifact.URI})
				return app.submitScientistBenchToolOutput(ctx, caseID, run.ID, node, scientistBenchToolArtifact, scientistBenchStateUpdateOpKey(call), output, details, artifact, nil)
			},
		))
	}

	tools = append(tools, makeTool(
		scientistBenchToolInfo(
			scientistBenchToolMemory,
			"Persist a concise cross-step memory entry for later ScientistBench workers. This is optional and should not be used as the only terminal deliverable.",
			map[string]any{
				"kind":    toolStringProp("Memory kind, e.g. research_gap, execution_blocker, reviewer_focus."),
				"summary": toolStringProp("One-sentence durable memory."),
				"details": toolStringArrayProp("Optional supporting bullets."),
			},
			[]string{"kind", "summary"},
		),
		func(ctx context.Context, call llmtools.ToolCall) (llmtools.ToolResponse, error) {
			var params scientistBenchMemoryParams
			if err := decodeScientistBenchToolInput(call.Input, &params); err != nil {
				return llmtools.NewTextErrorResponse(err.Error()), nil
			}
			runDetails := sanitizeStrings(params.Details)
			err := app.recordScientistBenchMemory(ctx, caseID, run.ID, node, scientistBenchToolMemory, scientistBenchStateUpdateOpKey(call), strings.TrimSpace(params.Kind), strings.TrimSpace(params.Summary), runDetails)
			if err != nil {
				return llmtools.NewTextErrorResponse(err.Error()), nil
			}
			return llmtools.NewTextResponse("ScientistBench memory recorded"), nil
		},
	))

	return tools
}

func (app *App) submitScientistBenchToolOutput(
	ctx context.Context,
	caseID string,
	runID string,
	node orchestrator.NodeSpec,
	updateName string,
	opKey string,
	output orchestrator.WorkerOutput,
	memoryDetails []string,
	explicitArtifact *scientistbench.Artifact,
	explicitReview *scientistbench.Review,
) (llmtools.ToolResponse, error) {
	if app.ScientistBench == nil {
		return llmtools.NewTextErrorResponse("scientist bench service is not configured"), nil
	}

	item, err := app.ScientistBench.Get(ctx, caseID)
	if err != nil {
		return llmtools.NewTextErrorResponse(err.Error()), nil
	}
	run, ok := findScientistBenchRun(item, runID)
	if !ok {
		return llmtools.NewTextErrorResponse("scientist bench run not found"), nil
	}

	output = normalizeScientistBenchToolOutput(output)
	output = scientistBenchDefaultToolOutput(output, node)
	if run.Role == "execution_agent" && output.Execution != nil {
		output = app.reconcileScientistBenchRuntimeExecution(ctx, run, node, output)
	}
	if err := orchestrator.ValidateWorkerOutputForRole(node, run.Role, output); err != nil {
		return llmtools.NewTextErrorResponse(err.Error()), nil
	}

	_, err = app.ScientistBench.MutateCase(ctx, caseID, func(item *scientistbench.Case) error {
		currentRun, ok := findScientistBenchRun(*item, runID)
		if !ok {
			return fmt.Errorf("scientist bench run %s not found", runID)
		}
		if scientistBenchRunHasAppliedStateUpdate(currentRun, updateName, opKey) {
			return nil
		}
		now := time.Now().Unix()
		currentRun.Status = "running"
		currentRun.TaskRunStatus = "running"
		currentRun.OutputSummary = truncateScientistBenchText(firstNonEmpty(output.Summary, currentRun.OutputSummary), maxScientistBenchSummaryLen)
		currentRun.SignalsEmitted = normalizeRunSignals(output, node)
		currentRun.StateUpdates = appendUniqueString(currentRun.StateUpdates, updateName)
		currentRun.StateUpdateOps = appendUniqueString(currentRun.StateUpdateOps, opKey)
		item.Runs = upsertScientistBenchRunLocal(item.Runs, currentRun)

		applyScientistBenchTypedRoleState(item, currentRun, node, output, now)

		if explicitArtifact != nil {
			artifact := buildScientistBenchExplicitArtifact(*explicitArtifact, currentRun, node, output, now)
			item.Artifacts = scientistBenchUpsertArtifactLocal(item.Artifacts, artifact)
		} else {
			for _, artifact := range scientistBenchDeterministicArtifacts(currentRun, node, output, now) {
				item.Artifacts = scientistBenchUpsertArtifactLocal(item.Artifacts, artifact)
			}
		}
		if explicitReview != nil {
			item.Reviews = scientistBenchUpsertReviewLocal(item.Reviews, *explicitReview)
		}
		recordScientistBenchDerivedMemory(item, currentRun, node, updateName, output, memoryDetails, now)
		*item = refreshScientistBenchMetrics(*item)
		*item = scientistbench.SyncWorkflowState(*item)
		return nil
	})
	if err != nil {
		return llmtools.NewTextErrorResponse(err.Error()), nil
	}
	return llmtools.NewTextResponse("ScientistBench state updated"), nil
}

func (app *App) recordScientistBenchMemory(
	ctx context.Context,
	caseID string,
	runID string,
	node orchestrator.NodeSpec,
	updateName string,
	opKey string,
	kind string,
	summary string,
	details []string,
) error {
	if app.ScientistBench == nil {
		return nil
	}
	_, err := app.ScientistBench.MutateCase(ctx, caseID, func(item *scientistbench.Case) error {
		run, ok := findScientistBenchRun(*item, runID)
		if !ok {
			return fmt.Errorf("scientist bench run %s not found", runID)
		}
		if scientistBenchRunHasAppliedStateUpdate(run, updateName, opKey) {
			return nil
		}
		run.Status = "running"
		run.TaskRunStatus = "running"
		run.StateUpdates = appendUniqueString(run.StateUpdates, updateName)
		run.StateUpdateOps = appendUniqueString(run.StateUpdateOps, opKey)
		item.Runs = upsertScientistBenchRunLocal(item.Runs, run)
		item.Memories = scientistBenchUpsertMemoryLocal(item.Memories, scientistbench.MemoryEntry{
			ID:        scientistBenchMemoryID(runID, kind),
			RunID:     runID,
			Stage:     firstNonEmpty(node.Stage, item.GraphState.CurrentStage),
			NodeID:    node.ID,
			Role:      run.Role,
			Kind:      kind,
			Summary:   summary,
			Details:   sanitizeStrings(details),
			CreatedAt: time.Now().Unix(),
		})
		*item = scientistbench.SyncWorkflowState(*item)
		return nil
	})
	return err
}

func scientistBenchDefaultToolOutput(output orchestrator.WorkerOutput, node orchestrator.NodeSpec) orchestrator.WorkerOutput {
	if strings.TrimSpace(output.Status) == "" {
		output.Status = "succeeded"
	}
	if strings.EqualFold(output.Status, "failed") {
		output.SuccessSignal = ""
		if strings.TrimSpace(output.FailureSignal) == "" {
			output.FailureSignal = node.FailureSignal
		}
		return output
	}
	output.FailureSignal = ""
	if strings.TrimSpace(output.SuccessSignal) == "" {
		output.SuccessSignal = node.SuccessSignal
	}
	return output
}

func normalizeScientistBenchToolOutput(output orchestrator.WorkerOutput) orchestrator.WorkerOutput {
	output.Status = strings.TrimSpace(output.Status)
	output.Summary = strings.TrimSpace(output.Summary)
	output.SuccessSignal = strings.TrimSpace(output.SuccessSignal)
	output.FailureSignal = strings.TrimSpace(output.FailureSignal)
	output.EvidenceSummary = sanitizeStrings(output.EvidenceSummary)
	output.Citations = sanitizeStrings(output.Citations)
	output.Risks = sanitizeStrings(output.Risks)
	output.RevisionDecision = strings.TrimSpace(output.RevisionDecision)
	output.RevisionFeedback = sanitizeStrings(output.RevisionFeedback)
	for i := range output.References {
		output.References[i].Key = strings.TrimSpace(output.References[i].Key)
		output.References[i].Title = strings.TrimSpace(output.References[i].Title)
		output.References[i].Authors = sanitizeStrings(output.References[i].Authors)
		output.References[i].Venue = strings.TrimSpace(output.References[i].Venue)
		output.References[i].DOI = strings.TrimSpace(output.References[i].DOI)
		output.References[i].URL = strings.TrimSpace(output.References[i].URL)
		output.References[i].ZoteroKey = strings.TrimSpace(output.References[i].ZoteroKey)
		output.References[i].FormattedReference = strings.TrimSpace(output.References[i].FormattedReference)
		output.References[i].BibTeX = strings.TrimSpace(output.References[i].BibTeX)
		output.References[i].KeyClaim = strings.TrimSpace(output.References[i].KeyClaim)
	}
	for i := range output.Ideas {
		output.Ideas[i].Title = strings.TrimSpace(output.Ideas[i].Title)
		output.Ideas[i].Summary = strings.TrimSpace(output.Ideas[i].Summary)
		output.Ideas[i].NoveltyClaim = strings.TrimSpace(output.Ideas[i].NoveltyClaim)
		output.Ideas[i].Hypotheses = sanitizeStrings(output.Ideas[i].Hypotheses)
		output.Ideas[i].SupportingRefs = sanitizeStrings(output.Ideas[i].SupportingRefs)
	}
	for i := range output.Objections {
		output.Objections[i].IdeaTitle = strings.TrimSpace(output.Objections[i].IdeaTitle)
		output.Objections[i].Summary = strings.TrimSpace(output.Objections[i].Summary)
		output.Objections[i].Severity = strings.TrimSpace(output.Objections[i].Severity)
		output.Objections[i].EvidenceRefs = sanitizeStrings(output.Objections[i].EvidenceRefs)
	}
	if output.MethodPlan != nil {
		output.MethodPlan.Summary = strings.TrimSpace(output.MethodPlan.Summary)
		output.MethodPlan.PipelineSteps = sanitizeStrings(output.MethodPlan.PipelineSteps)
		output.MethodPlan.AcceptanceChecks = sanitizeStrings(output.MethodPlan.AcceptanceChecks)
		output.MethodPlan.ImplementationNotes = sanitizeStrings(output.MethodPlan.ImplementationNotes)
		output.MethodPlan.RuntimeHints = sanitizeStrings(output.MethodPlan.RuntimeHints)
	}
	if output.Execution != nil {
		output.Execution.RuntimeID = strings.TrimSpace(output.Execution.RuntimeID)
		output.Execution.Commands = sanitizeStrings(output.Execution.Commands)
		output.Execution.VerificationSummary = strings.TrimSpace(output.Execution.VerificationSummary)
		output.Execution.StdoutExcerpt = strings.TrimSpace(output.Execution.StdoutExcerpt)
		output.Execution.StderrExcerpt = strings.TrimSpace(output.Execution.StderrExcerpt)
		output.Execution.OutputFiles = sanitizeStrings(output.Execution.OutputFiles)
		output.Execution.LogHighlights = sanitizeStrings(output.Execution.LogHighlights)
	}
	if output.Review != nil {
		output.Review.Decision = strings.TrimSpace(output.Review.Decision)
		output.Review.Summary = strings.TrimSpace(output.Review.Summary)
		output.Review.Strengths = sanitizeStrings(output.Review.Strengths)
		output.Review.Weaknesses = sanitizeStrings(output.Review.Weaknesses)
		output.Review.Questions = sanitizeStrings(output.Review.Questions)
	}
	if output.Comparison != nil {
		output.Comparison.Summary = strings.TrimSpace(output.Comparison.Summary)
		output.Comparison.Strengths = sanitizeStrings(output.Comparison.Strengths)
		output.Comparison.Weaknesses = sanitizeStrings(output.Comparison.Weaknesses)
	}
	return output
}

func applyScientistBenchTypedRoleState(
	item *scientistbench.Case,
	run scientistbench.RunRecord,
	node orchestrator.NodeSpec,
	output orchestrator.WorkerOutput,
	now int64,
) {
	if item == nil {
		return
	}
	if reducer, ok := scientistBenchRoleReducerFor(node.ID, run.Role); ok && reducer.Reduce != nil {
		reducer.Reduce(item, run, output, now)
		return
	}
}

func recordScientistBenchDerivedMemory(
	item *scientistbench.Case,
	run scientistbench.RunRecord,
	node orchestrator.NodeSpec,
	updateName string,
	output orchestrator.WorkerOutput,
	details []string,
	createdAt int64,
) {
	if item == nil {
		return
	}
	summary := strings.TrimSpace(output.Summary)
	if summary == "" {
		return
	}
	item.Memories = scientistBenchUpsertMemoryLocal(item.Memories, scientistbench.MemoryEntry{
		ID:        scientistBenchMemoryID(run.ID, updateName),
		RunID:     run.ID,
		Stage:     firstNonEmpty(node.Stage, item.GraphState.CurrentStage),
		NodeID:    node.ID,
		Role:      run.Role,
		Kind:      updateName,
		Summary:   summary,
		Details:   sanitizeStrings(details),
		CreatedAt: createdAt,
	})
}

func scientistBenchDeterministicArtifacts(
	run scientistbench.RunRecord,
	node orchestrator.NodeSpec,
	output orchestrator.WorkerOutput,
	createdAt int64,
) []scientistbench.Artifact {
	items := scientistBenchArtifactsForOutput(run, node, output, createdAt)
	for i := range items {
		items[i].ID = fmt.Sprintf("artifact-%s-%d", run.ID, i)
		if items[i].Metadata == nil {
			items[i].Metadata = map[string]string{}
		}
		items[i].Metadata["state_tool"] = "true"
	}
	return items
}

func buildScientistBenchExplicitArtifact(
	artifact scientistbench.Artifact,
	run scientistbench.RunRecord,
	node orchestrator.NodeSpec,
	output orchestrator.WorkerOutput,
	createdAt int64,
) scientistbench.Artifact {
	if artifact.ID == "" {
		artifact.ID = "artifact-" + run.ID + "-0"
	}
	if artifact.Kind == "" {
		artifact.Kind = artifactKindForNode(node)
	}
	if artifact.Label == "" {
		artifact.Label = artifactLabelForNode(node)
	}
	artifact.ProducerRole = run.Role
	artifact.ProducerRunID = run.ID
	artifact.CreatedAt = createdAt
	if artifact.Metadata == nil {
		artifact.Metadata = map[string]string{}
	}
	artifact.Metadata["summary"] = firstNonEmpty(artifact.Metadata["summary"], output.Summary)
	artifact.Metadata["node_id"] = node.ID
	artifact.Metadata["session_id"] = run.SessionID
	artifact.Metadata["status"] = output.Status
	artifact.Metadata["state_tool"] = "true"
	if len(output.Citations) > 0 {
		artifact.Metadata["citations"] = strings.Join(output.Citations, " | ")
	}
	if len(output.Risks) > 0 {
		artifact.Metadata["risks"] = strings.Join(output.Risks, " | ")
	}
	return artifact
}

func scientistBenchMemoryID(runID string, kind string) string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "memory"
	}
	kind = strings.ReplaceAll(kind, " ", "_")
	return "memory-" + runID + "-" + kind
}

func normalizeScientistBenchArtifactMetadata(items map[string]string) map[string]string {
	if len(items) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(items))
	for key, value := range items {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		out[key] = value
	}
	return out
}

func filterIdeasNotFromRun(items []scientistbench.IdeaCandidate, runID string) []scientistbench.IdeaCandidate {
	out := make([]scientistbench.IdeaCandidate, 0, len(items))
	prefix := "idea-" + runID + "-"
	for _, item := range items {
		if strings.HasPrefix(item.ID, prefix) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func filterObjectionsNotFromRun(items []scientistbench.IdeaObjection, runID string) []scientistbench.IdeaObjection {
	out := make([]scientistbench.IdeaObjection, 0, len(items))
	prefix := "obj-" + runID + "-"
	for _, item := range items {
		if strings.HasPrefix(item.ID, prefix) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func scientistBenchUpsertArtifactLocal(existing []scientistbench.Artifact, incoming scientistbench.Artifact) []scientistbench.Artifact {
	for i, item := range existing {
		if item.ID == incoming.ID {
			existing[i] = incoming
			return existing
		}
	}
	return append(existing, incoming)
}

func scientistBenchUpsertReviewLocal(existing []scientistbench.Review, incoming scientistbench.Review) []scientistbench.Review {
	for i, item := range existing {
		if item.ID == incoming.ID {
			existing[i] = incoming
			return existing
		}
	}
	return append(existing, incoming)
}

func scientistBenchUpsertMemoryLocal(existing []scientistbench.MemoryEntry, incoming scientistbench.MemoryEntry) []scientistbench.MemoryEntry {
	for i, item := range existing {
		if item.ID == incoming.ID {
			existing[i] = incoming
			return existing
		}
	}
	return append(existing, incoming)
}
