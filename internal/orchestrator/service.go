package orchestrator

import (
	"fmt"
	"slices"
	"strings"

	"github.com/SciMate-AI/scicli/internal/scientistbench"
)

type RoleSpec struct {
	ID                   string   `json:"role_id"`
	DisplayName          string   `json:"display_name,omitempty"`
	Category             string   `json:"category,omitempty"`
	InteractionMode      string   `json:"interaction_mode,omitempty"`
	ExecutionMode        string   `json:"execution_mode,omitempty"`
	CanSpawn             bool     `json:"can_spawn,omitempty"`
	CanVote              bool     `json:"can_vote,omitempty"`
	CanReview            bool     `json:"can_review,omitempty"`
	AllowedToolClasses   []string `json:"allowed_tool_classes,omitempty"`
	ForbiddenToolClasses []string `json:"forbidden_tool_classes,omitempty"`
	Inputs               []string `json:"inputs,omitempty"`
	Outputs              []string `json:"outputs,omitempty"`
	SuccessSignals       []string `json:"success_signals,omitempty"`
	FailureSignals       []string `json:"failure_signals,omitempty"`
}

type RetryPolicy struct {
	MaxRetries          int  `json:"max_retries,omitempty"`
	RequiresNewEvidence bool `json:"requires_new_evidence,omitempty"`
}

type WorkerToolProfile string

const (
	WorkerToolProfileReadOnly     WorkerToolProfile = "read_only"
	WorkerToolProfileResearch     WorkerToolProfile = "research"
	WorkerToolProfileDeliberation WorkerToolProfile = "deliberation"
	WorkerToolProfileCode         WorkerToolProfile = "code"
	WorkerToolProfileExecution    WorkerToolProfile = "execution"
)

type WorkerProfile struct {
	RoleID         string            `json:"role_id"`
	SessionLabel   string            `json:"session_label,omitempty"`
	ToolProfile    WorkerToolProfile `json:"tool_profile,omitempty"`
	PromptPreamble string            `json:"prompt_preamble,omitempty"`
}

type NodeSpec struct {
	ID              string      `json:"node_id"`
	Type            string      `json:"node_type,omitempty"`
	Stage           string      `json:"stage,omitempty"`
	EntryConditions []string    `json:"entry_conditions,omitempty"`
	AssignedRoles   []string    `json:"assigned_roles,omitempty"`
	Inputs          []string    `json:"inputs,omitempty"`
	Outputs         []string    `json:"outputs,omitempty"`
	SuccessSignal   string      `json:"success_signal,omitempty"`
	FailureSignal   string      `json:"failure_signal,omitempty"`
	RetryPolicy     RetryPolicy `json:"retry_policy,omitempty"`
	NextOnSuccess   string      `json:"next_on_success,omitempty"`
	NextOnFailure   string      `json:"next_on_failure,omitempty"`
}

type Service interface {
	Roles() []RoleSpec
	Nodes() []NodeSpec
	GetRole(id string) (RoleSpec, bool)
	GetNode(id string) (NodeSpec, bool)
	WorkerProfileForRole(id string) (WorkerProfile, bool)
	BootstrapCase(item scientistbench.Case) (scientistbench.Case, error)
	ApplySignal(item scientistbench.Case, signal string) (scientistbench.Case, error)
}

type service struct {
	roles map[string]RoleSpec
	nodes map[string]NodeSpec
	order []string
}

func NewService() Service {
	roleList := defaultRoles()
	nodeList := defaultNodes()

	roles := make(map[string]RoleSpec, len(roleList))
	for _, role := range roleList {
		roles[role.ID] = role
	}

	nodes := make(map[string]NodeSpec, len(nodeList))
	order := make([]string, 0, len(nodeList))
	for _, node := range nodeList {
		nodes[node.ID] = node
		order = append(order, node.ID)
	}

	return &service{
		roles: roles,
		nodes: nodes,
		order: order,
	}
}

func (s *service) Roles() []RoleSpec {
	out := make([]RoleSpec, 0, len(s.roles))
	for _, id := range sortedRoleIDs(s.roles) {
		out = append(out, s.roles[id])
	}
	return out
}

func (s *service) Nodes() []NodeSpec {
	out := make([]NodeSpec, 0, len(s.order))
	for _, id := range s.order {
		out = append(out, s.nodes[id])
	}
	return out
}

func (s *service) GetRole(id string) (RoleSpec, bool) {
	role, ok := s.roles[strings.TrimSpace(id)]
	return role, ok
}

func (s *service) GetNode(id string) (NodeSpec, bool) {
	node, ok := s.nodes[strings.TrimSpace(id)]
	return node, ok
}

func (s *service) WorkerProfileForRole(id string) (WorkerProfile, bool) {
	switch strings.TrimSpace(id) {
	case "chief_scientist":
		return WorkerProfile{
			RoleID:       "chief_scientist",
			SessionLabel: "Chief Scientist",
			ToolProfile:  WorkerToolProfileDeliberation,
			PromptPreamble: strings.TrimSpace(`
You are the Chief Scientist in a multi-agent scientist benchmark workflow.
Your job is to make bounded control decisions, synthesize prior role outputs, and choose whether the active node should advance or fail.
Do not invent evidence. Base decisions on prior node outputs and explicit case constraints.
`),
		}, true
	case "research_agent":
		return WorkerProfile{
			RoleID:       "research_agent",
			SessionLabel: "Research Agent",
			ToolProfile:  WorkerToolProfileResearch,
			PromptPreamble: strings.TrimSpace(`
You are the Research Agent in a multi-agent scientist benchmark workflow.
Your job is to retrieve evidence, align references, summarize relevant methods, and identify reproduction risks.
Prefer citing concrete evidence and avoid unsupported claims.
`),
		}, true
	case "idea_maker":
		return WorkerProfile{
			RoleID:       "idea_maker",
			SessionLabel: "Idea Maker",
			ToolProfile:  WorkerToolProfileDeliberation,
			PromptPreamble: strings.TrimSpace(`
You are the Idea Maker in a scientist benchmark workflow.
Generate testable, specific, non-trivial candidate ideas grounded in the provided references and case context.
Avoid vague novelty claims. State hypotheses clearly.
`),
		}, true
	case "idea_hater":
		return WorkerProfile{
			RoleID:       "idea_hater",
			SessionLabel: "Idea Hater",
			ToolProfile:  WorkerToolProfileDeliberation,
			PromptPreamble: strings.TrimSpace(`
You are the Idea Hater in a scientist benchmark workflow.
Your job is to reject weak, derivative, underspecified, or unverifiable ideas.
Look for hidden assumptions, missing evidence, evaluation gaps, and non-novel claims.
`),
		}, true
	case "method_planner":
		return WorkerProfile{
			RoleID:       "method_planner",
			SessionLabel: "Method Planner",
			ToolProfile:  WorkerToolProfileDeliberation,
			PromptPreamble: strings.TrimSpace(`
You are the Method Planner in a scientist benchmark workflow.
Convert accepted ideas and available evidence into a concrete, testable method specification.
Be explicit about pipeline stages, assumptions, acceptance checks, and key implementation risks.
`),
		}, true
	case "code_agent":
		return WorkerProfile{
			RoleID:       "code_agent",
			SessionLabel: "Code Agent",
			ToolProfile:  WorkerToolProfileCode,
			PromptPreamble: strings.TrimSpace(`
You are the Code Agent in a scientist benchmark workflow.
Implement the planned method concretely in the repository, minimize unnecessary edits, and prefer executable, testable changes.
If a method step is underspecified, make the smallest defensible assumption and state it.
`),
		}, true
	case "execution_agent":
		return WorkerProfile{
			RoleID:       "execution_agent",
			SessionLabel: "Execution Agent",
			ToolProfile:  WorkerToolProfileExecution,
			PromptPreamble: strings.TrimSpace(`
You are the Execution Agent in a scientist benchmark workflow.
Run the prepared implementation, validate whether it executes, capture failures precisely, and summarize runtime evidence.
Prefer short verification loops over broad speculative execution.
`),
		}, true
	case "figure_agent":
		return WorkerProfile{
			RoleID:       "figure_agent",
			SessionLabel: "Figure Agent",
			ToolProfile:  WorkerToolProfileCode,
			PromptPreamble: strings.TrimSpace(`
You are the Figure Agent in a scientist benchmark workflow.
Turn execution outputs into clear, reproducible figures and tables.
Prefer plots and summaries that directly support scientific claims and later paper writing.
`),
		}, true
	case "paper_writer":
		return WorkerProfile{
			RoleID:       "paper_writer",
			SessionLabel: "Paper Writer",
			ToolProfile:  WorkerToolProfileCode,
			PromptPreamble: strings.TrimSpace(`
You are the Paper Writer in a scientist benchmark workflow.
Produce a readable, well-structured paper draft with explicit motivation, method, results, and limitations.
Prefer clear figure references, concrete claims, and LaTeX-ready structure over decorative prose.
`),
		}, true
	case "domain_expert_reviewer":
		return WorkerProfile{
			RoleID:       "domain_expert_reviewer",
			SessionLabel: "Domain Expert Reviewer",
			ToolProfile:  WorkerToolProfileDeliberation,
			PromptPreamble: strings.TrimSpace(`
You are the Domain Expert Reviewer in a scientist benchmark workflow.
Review the generated paper like a conference reviewer.
Give calibrated numerical scores, concise review-style feedback, and explicit judgments about readability, novelty, and execution validity.
`),
		}, true
	case "advisor_agent":
		return WorkerProfile{
			RoleID:       "advisor_agent",
			SessionLabel: "Advisor Agent",
			ToolProfile:  WorkerToolProfileDeliberation,
			PromptPreamble: strings.TrimSpace(`
You are the Advisor Agent in a scientist benchmark workflow.
Produce a detailed correctness analysis of the implementation and execution evidence.
Call out subtle mismatches between the intended method and the observed implementation.
`),
		}, true
	case "judge_agent":
		return WorkerProfile{
			RoleID:       "judge_agent",
			SessionLabel: "Judge Agent",
			ToolProfile:  WorkerToolProfileDeliberation,
			PromptPreamble: strings.TrimSpace(`
You are the Judge Agent in a scientist benchmark workflow.
Score the advisor report on a calibrated 1-5 correctness scale.
Focus on whether the reproduced method is actually faithful and complete, not just runnable.
`),
		}, true
	case "paper_comparison_reviewer":
		return WorkerProfile{
			RoleID:       "paper_comparison_reviewer",
			SessionLabel: "Paper Comparison Reviewer",
			ToolProfile:  WorkerToolProfileDeliberation,
			PromptPreamble: strings.TrimSpace(`
You are the Paper Comparison Reviewer in a scientist benchmark workflow.
Compare the generated paper against the target paper using ICLR-style criteria.
Assess motivation, methodology, novelty, and experimental alignment with concrete justification.
`),
		}, true
	default:
		return WorkerProfile{
			RoleID:       strings.TrimSpace(id),
			SessionLabel: "Task Agent",
			ToolProfile:  WorkerToolProfileReadOnly,
			PromptPreamble: strings.TrimSpace(`
You are a role-specific worker in a scientist benchmark workflow.
Execute only the responsibilities implied by your assigned node and role.
`),
		}, strings.TrimSpace(id) != ""
	}
}

func (s *service) BootstrapCase(item scientistbench.Case) (scientistbench.Case, error) {
	item = scientistbench.Case(item)
	if strings.TrimSpace(item.ID) == "" {
		return scientistbench.Case{}, fmt.Errorf("case ID is required")
	}
	if strings.TrimSpace(item.GraphState.ActiveNode) != "" {
		item.GraphState.PendingNodes = s.pendingNodes(item.GraphState.ActiveNode, item.GraphState.CompletedNodes, item.GraphState.BlockedNodes)
		if item.Status == "" {
			item.Status = scientistbench.StatusPlanning
		}
		return item, nil
	}

	firstNode, ok := s.GetNode("node-case-intake")
	if !ok {
		return scientistbench.Case{}, fmt.Errorf("bootstrap node is not registered")
	}

	item.GraphState.CurrentStage = firstNode.Stage
	item.GraphState.ActiveNode = firstNode.ID
	item.GraphState.ActiveRole = firstAssignedRole(firstNode.AssignedRoles)
	item.GraphState.PendingNodes = s.pendingNodes(firstNode.ID, nil, nil)
	if item.Status == "" || item.Status == scientistbench.StatusPlanning {
		item.Status = scientistbench.StatusPlanning
	}
	return item, nil
}

func (s *service) ApplySignal(item scientistbench.Case, signal string) (scientistbench.Case, error) {
	signal = strings.TrimSpace(signal)
	if signal == "" {
		return scientistbench.Case{}, fmt.Errorf("signal is required")
	}

	var err error
	item, err = s.BootstrapCase(item)
	if err != nil {
		return scientistbench.Case{}, err
	}

	current, ok := s.GetNode(item.GraphState.ActiveNode)
	if !ok {
		return scientistbench.Case{}, fmt.Errorf("active node %s is not registered", item.GraphState.ActiveNode)
	}

	switch signal {
	case current.SuccessSignal:
		item.GraphState.CompletedNodes = appendUnique(item.GraphState.CompletedNodes, current.ID)
		return s.advanceToNext(item, current.NextOnSuccess, signal)
	case current.FailureSignal:
		item.GraphState.BlockedNodes = appendUnique(item.GraphState.BlockedNodes, current.ID)
		return s.advanceToNext(item, current.NextOnFailure, signal)
	default:
		return scientistbench.Case{}, fmt.Errorf("signal %s is not accepted by node %s", signal, current.ID)
	}
}

func (s *service) advanceToNext(item scientistbench.Case, nextNodeID string, signal string) (scientistbench.Case, error) {
	switch signal {
	case string(scientistbench.SignalCaseResolved):
		item.Termination = scientistbench.Termination{
			Signal:   scientistbench.SignalCaseResolved,
			Resolved: true,
		}
		item.Status = scientistbench.StatusResolved
		item.GraphState.PendingNodes = nil
		return item, nil
	case string(scientistbench.SignalCaseNotResolved):
		item.Termination = scientistbench.Termination{
			Signal: scientistbench.SignalCaseNotResolved,
		}
		item.Status = scientistbench.StatusNotResolved
		item.GraphState.PendingNodes = nil
		return item, nil
	}

	nextNodeID = strings.TrimSpace(nextNodeID)
	if nextNodeID == "" {
		item.Status = scientistbench.StatusBlocked
		item.GraphState.PendingNodes = s.pendingNodes(item.GraphState.ActiveNode, item.GraphState.CompletedNodes, item.GraphState.BlockedNodes)
		return item, nil
	}

	nextNode, ok := s.GetNode(nextNodeID)
	if !ok {
		return scientistbench.Case{}, fmt.Errorf("next node %s is not registered", nextNodeID)
	}

	item.GraphState.CurrentStage = nextNode.Stage
	item.GraphState.ActiveNode = nextNode.ID
	item.GraphState.ActiveRole = firstAssignedRole(nextNode.AssignedRoles)
	item.GraphState.PendingNodes = s.pendingNodes(nextNode.ID, item.GraphState.CompletedNodes, item.GraphState.BlockedNodes)
	item.Status = scientistbench.StatusRunning
	return item, nil
}

func (s *service) pendingNodes(activeNode string, completed []string, blocked []string) []string {
	completedSet := make(map[string]struct{}, len(completed))
	for _, id := range completed {
		completedSet[id] = struct{}{}
	}
	blockedSet := make(map[string]struct{}, len(blocked))
	for _, id := range blocked {
		blockedSet[id] = struct{}{}
	}

	activeFound := false
	out := make([]string, 0, len(s.order))
	for _, id := range s.order {
		if id == activeNode {
			activeFound = true
			continue
		}
		if !activeFound {
			continue
		}
		if _, ok := completedSet[id]; ok {
			continue
		}
		if _, ok := blockedSet[id]; ok {
			continue
		}
		out = append(out, id)
	}
	return out
}

func defaultRoles() []RoleSpec {
	return []RoleSpec{
		{
			ID:              "chief_scientist",
			DisplayName:     "Chief Scientist",
			Category:        "control",
			InteractionMode: "ag2_chat",
			ExecutionMode:   "langgraph_node_worker",
			CanVote:         true,
			Inputs:          []string{"case", "budget", "signals"},
			Outputs:         []string{"stage_decision", "budget_decision"},
			SuccessSignals:  []string{"case_initialized", "case_resolved"},
			FailureSignals:  []string{"case_not_resolved"},
		},
		{
			ID:                 "research_agent",
			DisplayName:        "Research Agent",
			Category:           "analysis",
			InteractionMode:    "ag2_chat",
			ExecutionMode:      "langgraph_node_worker",
			CanVote:            true,
			AllowedToolClasses: []string{"mcp_search", "pdf_reader", "citation_bundle"},
			Inputs:             []string{"question_bundle", "source_policy", "budget"},
			Outputs:            []string{"evidence_pack", "citation_bundle", "risk_report"},
			SuccessSignals:     []string{"research_plan_ready", "evidence_ready"},
			FailureSignals:     []string{"planning_failed", "missing_sources"},
		},
		{
			ID:              "idea_maker",
			DisplayName:     "Idea Maker",
			Category:        "ideation",
			InteractionMode: "ag2_chat",
			ExecutionMode:   "langgraph_node_worker",
			CanVote:         true,
			Inputs:          []string{"evidence_pack", "core_idea"},
			Outputs:         []string{"idea_candidate"},
			SuccessSignals:  []string{"idea_gate_passed"},
			FailureSignals:  []string{"idea_gate_rejected"},
		},
		{
			ID:              "idea_hater",
			DisplayName:     "Idea Hater",
			Category:        "critique",
			InteractionMode: "ag2_chat",
			ExecutionMode:   "langgraph_node_worker",
			CanVote:         true,
			Inputs:          []string{"idea_candidate", "evidence_pack"},
			Outputs:         []string{"objection_log"},
			SuccessSignals:  []string{"idea_gate_passed"},
			FailureSignals:  []string{"idea_gate_rejected"},
		},
		{
			ID:              "method_planner",
			DisplayName:     "Method Planner",
			Category:        "planning",
			InteractionMode: "ag2_chat",
			ExecutionMode:   "langgraph_node_worker",
			CanVote:         true,
			Inputs:          []string{"accepted_idea_set", "evidence_pack"},
			Outputs:         []string{"method_spec", "acceptance_tests"},
			SuccessSignals:  []string{"method_spec_ready"},
			FailureSignals:  []string{"method_underdefined"},
		},
		{
			ID:                 "code_agent",
			DisplayName:        "Code Agent",
			Category:           "implementation",
			InteractionMode:    "ag2_chat",
			ExecutionMode:      "langgraph_node_worker",
			AllowedToolClasses: []string{"code_edit", "shell", "repo_read"},
			Inputs:             []string{"method_spec", "acceptance_tests"},
			Outputs:            []string{"repo_patch", "scripts"},
			SuccessSignals:     []string{"code_ready"},
			FailureSignals:     []string{"implementation_failed"},
		},
		{
			ID:                 "execution_agent",
			DisplayName:        "Execution Agent",
			Category:           "runtime",
			InteractionMode:    "ag2_chat",
			ExecutionMode:      "langgraph_node_worker",
			AllowedToolClasses: []string{"docker_runtime", "shell", "hpc_workflow"},
			Inputs:             []string{"repo_patch", "runtime_spec"},
			Outputs:            []string{"runtime_logs", "result_bundle"},
			SuccessSignals:     []string{"execution_complete"},
			FailureSignals:     []string{"code_not_executable"},
		},
		{
			ID:                 "figure_agent",
			DisplayName:        "Figure Agent",
			Category:           "analysis",
			InteractionMode:    "ag2_chat",
			ExecutionMode:      "langgraph_node_worker",
			AllowedToolClasses: []string{"plotting", "table_builder"},
			Inputs:             []string{"metrics", "raw_outputs"},
			Outputs:            []string{"figures", "tables"},
			SuccessSignals:     []string{"analysis_ready"},
			FailureSignals:     []string{"result_not_interpretable"},
		},
		{
			ID:                 "paper_writer",
			DisplayName:        "Paper Writer",
			Category:           "writing",
			InteractionMode:    "ag2_chat",
			ExecutionMode:      "langgraph_node_worker",
			AllowedToolClasses: []string{"latex", "citation_bundle", "figure_assets"},
			Inputs:             []string{"citation_bundle", "figures", "tables", "result_bundle"},
			Outputs:            []string{"paper_draft", "latex_project"},
			SuccessSignals:     []string{"paper_draft_ready"},
			FailureSignals:     []string{"paper_not_readable"},
		},
		{
			ID:              "domain_expert_reviewer",
			DisplayName:     "Domain Expert Reviewer",
			Category:        "review",
			InteractionMode: "ag2_chat",
			ExecutionMode:   "langgraph_node_worker",
			CanVote:         true,
			CanReview:       true,
			Inputs:          []string{"paper_draft", "citation_bundle", "figures"},
			Outputs:         []string{"review", "scores"},
			SuccessSignals:  []string{"paper_review_ready"},
			FailureSignals:  []string{"review_incomplete"},
		},
		{
			ID:              "advisor_agent",
			DisplayName:     "Advisor Agent",
			Category:        "review",
			InteractionMode: "ag2_chat",
			ExecutionMode:   "langgraph_node_worker",
			CanVote:         true,
			CanReview:       true,
			Inputs:          []string{"repo_patch", "runtime_logs", "method_spec"},
			Outputs:         []string{"advisor_report"},
			SuccessSignals:  []string{"advisor_report_ready"},
			FailureSignals:  []string{"correctness_not_assessable"},
		},
		{
			ID:              "judge_agent",
			DisplayName:     "Judge Agent",
			Category:        "review",
			InteractionMode: "ag2_chat",
			ExecutionMode:   "langgraph_node_worker",
			CanVote:         true,
			CanReview:       true,
			Inputs:          []string{"advisor_report"},
			Outputs:         []string{"judge_score"},
			SuccessSignals:  []string{"judge_scores_ready"},
			FailureSignals:  []string{"judge_failed"},
		},
		{
			ID:              "paper_comparison_reviewer",
			DisplayName:     "Paper Comparison Reviewer",
			Category:        "review",
			InteractionMode: "ag2_chat",
			ExecutionMode:   "langgraph_node_worker",
			CanVote:         true,
			CanReview:       true,
			Inputs:          []string{"paper_draft", "target_paper"},
			Outputs:         []string{"comparison_review"},
			SuccessSignals:  []string{"paper_compare_ready"},
			FailureSignals:  []string{"comparison_failed"},
		},
	}
}

func defaultNodes() []NodeSpec {
	return []NodeSpec{
		{
			ID:            "node-case-intake",
			Type:          "control",
			Stage:         "planning",
			AssignedRoles: []string{"chief_scientist"},
			Outputs:       []string{"case_bundle"},
			SuccessSignal: "case_initialized",
			FailureSignal: "invalid_case_input",
			NextOnSuccess: "node-research-plan",
		},
		{
			ID:            "node-research-plan",
			Type:          "plan",
			Stage:         "research_planning",
			AssignedRoles: []string{"research_agent"},
			Inputs:        []string{"case_bundle"},
			Outputs:       []string{"research_plan"},
			SuccessSignal: "research_plan_ready",
			FailureSignal: "planning_failed",
			RetryPolicy:   RetryPolicy{MaxRetries: 2, RequiresNewEvidence: false},
			NextOnSuccess: "node-corpus-retrieval",
		},
		{
			ID:            "node-corpus-retrieval",
			Type:          "retrieval",
			Stage:         "corpus_retrieval",
			AssignedRoles: []string{"research_agent"},
			Inputs:        []string{"research_plan"},
			Outputs:       []string{"evidence_pack", "citation_bundle"},
			SuccessSignal: "evidence_ready",
			FailureSignal: "missing_sources",
			RetryPolicy:   RetryPolicy{MaxRetries: 2, RequiresNewEvidence: true},
			NextOnSuccess: "node-idea-gate",
		},
		{
			ID:            "node-idea-gate",
			Type:          "debate_gate",
			Stage:         "ideation",
			AssignedRoles: []string{"idea_maker", "idea_hater", "chief_scientist"},
			Inputs:        []string{"evidence_pack", "core_idea"},
			Outputs:       []string{"accepted_idea_set", "rejected_idea_set"},
			SuccessSignal: "idea_gate_passed",
			FailureSignal: "idea_gate_rejected",
			RetryPolicy:   RetryPolicy{MaxRetries: 2, RequiresNewEvidence: true},
			NextOnSuccess: "node-method-plan",
		},
		{
			ID:            "node-method-plan",
			Type:          "planning",
			Stage:         "method_planning",
			AssignedRoles: []string{"method_planner"},
			Inputs:        []string{"accepted_idea_set", "evidence_pack"},
			Outputs:       []string{"method_spec"},
			SuccessSignal: "method_spec_ready",
			FailureSignal: "method_underdefined",
			RetryPolicy:   RetryPolicy{MaxRetries: 2, RequiresNewEvidence: false},
			NextOnSuccess: "node-implementation",
		},
		{
			ID:            "node-implementation",
			Type:          "implementation",
			Stage:         "implementation",
			AssignedRoles: []string{"code_agent"},
			Inputs:        []string{"method_spec", "acceptance_tests"},
			Outputs:       []string{"repo_patch"},
			SuccessSignal: "code_ready",
			FailureSignal: "implementation_failed",
			RetryPolicy:   RetryPolicy{MaxRetries: 2, RequiresNewEvidence: false},
			NextOnSuccess: "node-execution",
		},
		{
			ID:            "node-execution",
			Type:          "runtime",
			Stage:         "execution",
			AssignedRoles: []string{"execution_agent"},
			Inputs:        []string{"repo_patch", "runtime_spec"},
			Outputs:       []string{"runtime_logs", "result_bundle"},
			SuccessSignal: "execution_complete",
			FailureSignal: "code_not_executable",
			RetryPolicy:   RetryPolicy{MaxRetries: 2, RequiresNewEvidence: false},
			NextOnSuccess: "node-analysis-figures",
		},
		{
			ID:            "node-analysis-figures",
			Type:          "analysis",
			Stage:         "analysis",
			AssignedRoles: []string{"figure_agent"},
			Inputs:        []string{"metrics", "raw_outputs"},
			Outputs:       []string{"figures", "tables"},
			SuccessSignal: "analysis_ready",
			FailureSignal: "result_not_interpretable",
			RetryPolicy:   RetryPolicy{MaxRetries: 1, RequiresNewEvidence: true},
			NextOnSuccess: "node-paper-draft",
		},
		{
			ID:            "node-paper-draft",
			Type:          "writing",
			Stage:         "paper_writing",
			AssignedRoles: []string{"paper_writer"},
			Inputs:        []string{"citation_bundle", "figures", "tables", "result_bundle"},
			Outputs:       []string{"paper_draft"},
			SuccessSignal: "paper_draft_ready",
			FailureSignal: "paper_not_readable",
			RetryPolicy:   RetryPolicy{MaxRetries: 2, RequiresNewEvidence: false},
			NextOnSuccess: "node-advisor-review",
		},
		{
			ID:            "node-advisor-review",
			Type:          "review",
			Stage:         "advisor_review",
			AssignedRoles: []string{"advisor_agent"},
			Inputs:        []string{"repo_patch", "runtime_logs", "method_spec"},
			Outputs:       []string{"advisor_report"},
			SuccessSignal: "advisor_report_ready",
			FailureSignal: "correctness_not_assessable",
			RetryPolicy:   RetryPolicy{MaxRetries: 1, RequiresNewEvidence: true},
			NextOnSuccess: "node-judge-review",
		},
		{
			ID:            "node-judge-review",
			Type:          "review",
			Stage:         "judge_review",
			AssignedRoles: []string{"judge_agent"},
			Inputs:        []string{"advisor_report"},
			Outputs:       []string{"judge_scores"},
			SuccessSignal: "judge_scores_ready",
			FailureSignal: "judge_failed",
			RetryPolicy:   RetryPolicy{MaxRetries: 1, RequiresNewEvidence: false},
			NextOnSuccess: "node-domain-review",
		},
		{
			ID:            "node-domain-review",
			Type:          "review",
			Stage:         "domain_review",
			AssignedRoles: []string{"domain_expert_reviewer"},
			Inputs:        []string{"paper_draft", "citation_bundle", "figures"},
			Outputs:       []string{"review", "scores"},
			SuccessSignal: "paper_review_ready",
			FailureSignal: "review_incomplete",
			RetryPolicy:   RetryPolicy{MaxRetries: 1, RequiresNewEvidence: false},
			NextOnSuccess: "node-paper-compare",
		},
		{
			ID:            "node-paper-compare",
			Type:          "review",
			Stage:         "comparison_review",
			AssignedRoles: []string{"paper_comparison_reviewer"},
			Inputs:        []string{"paper_draft", "target_paper"},
			Outputs:       []string{"comparison_review"},
			SuccessSignal: "paper_compare_ready",
			FailureSignal: "comparison_failed",
			RetryPolicy:   RetryPolicy{MaxRetries: 1, RequiresNewEvidence: false},
			NextOnSuccess: "node-aggregate",
		},
		{
			ID:            "node-aggregate",
			Type:          "aggregation",
			Stage:         "aggregation",
			AssignedRoles: []string{"chief_scientist"},
			Inputs:        []string{"scores", "reviews", "artifacts"},
			Outputs:       []string{"case_decision"},
			SuccessSignal: string(scientistbench.SignalCaseResolved),
			FailureSignal: string(scientistbench.SignalCaseNotResolved),
		},
	}
}

func firstAssignedRole(roles []string) string {
	if len(roles) == 0 {
		return ""
	}
	return roles[0]
}

func appendUnique(items []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return items
	}
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}

func sortedRoleIDs(items map[string]RoleSpec) []string {
	out := make([]string, 0, len(items))
	for id := range items {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}
