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
	ID              string   `json:"node_id"`
	Type            string   `json:"node_type,omitempty"`
	Stage           string   `json:"stage,omitempty"`
	EntryConditions []string `json:"entry_conditions,omitempty"`
	AssignedRoles   []string `json:"assigned_roles,omitempty"`
	// RoleDependencies expresses an intra-node role DAG. A role becomes runnable
	// only after all listed predecessor roles have completed successfully.
	RoleDependencies map[string][]string `json:"role_dependencies,omitempty"`
	Inputs           []string            `json:"inputs,omitempty"`
	Outputs          []string            `json:"outputs,omitempty"`
	SuccessSignal    string              `json:"success_signal,omitempty"`
	FailureSignal    string              `json:"failure_signal,omitempty"`
	RetryPolicy      RetryPolicy         `json:"retry_policy,omitempty"`
	NextOnSuccess    string              `json:"next_on_success,omitempty"`
	NextOnFailure    string              `json:"next_on_failure,omitempty"`
	// ConcurrentSuccessors lists nodes launched in parallel alongside NextOnSuccess.
	ConcurrentSuccessors []string `json:"concurrent_successors,omitempty"`
	// RequiredPredecessorSignals lists signals that must all be received before this
	// node is eligible to run. Used for fan-in after parallel stages.
	RequiredPredecessorSignals []string `json:"required_predecessor_signals,omitempty"`
	// DynamicRoutes maps additional signal names → target node IDs that the chief
	// scientist (or any assigned role) may emit to override the normal NextOnSuccess /
	// NextOnFailure routing. This is the mechanism by which the CS can dynamically
	// redirect work to any earlier or later stage when it detects a quality issue.
	// Example: {"retry_research": "node-corpus-retrieval", "skip_experiment": "node-paper-draft"}
	DynamicRoutes map[string]string `json:"dynamic_routes,omitempty"`
	// MaxDynamicRetries caps how many times a DynamicRoute back-jump may repeat
	// for this node before the system forces the normal forward path instead.
	// 0 means unlimited (use StageRetries in GraphState to track).
	MaxDynamicRetries int `json:"max_dynamic_retries,omitempty"`
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
You are the Chief Scientist — the directing intelligence of a multi-agent research pipeline.
You are NOT just a validator. You are the decision-maker and orchestrator.

YOUR RESPONSIBILITIES vary by which node you are currently running:

1. node-case-intake: Parse the case inputs. Confirm all required fields are present.
   Emit idea_gate_passed if inputs are valid, invalid_case_input if critically broken.

2. node-idea-gate: Review ideas from idea_maker and objections from idea_hater.
   Check the pipeline status section — it tells you what evidence is available.
   Decision rules:
   - If NO evidence and NO core_idea: emit "research_insufficient" (triggers research retry).
   - If evidence is thin but a core_idea exists: proceed — generate hypotheses from the
     core_idea and your domain knowledge. Do not block just because evidence is sparse.
   - If ideas are weak/derivative after fair debate: emit "idea_gate_rejected".
   - If at least one strong, testable idea emerged: emit "idea_gate_passed".
   PRIORITY: lean toward proceeding. A retry costs a full research pass. Use it only
   when you have literally no grounding for ideation.

3. node-revision-gate: Read all review scores. Compute weighted mean.
   If mean >= 3.5 → emit "paper_quality_acceptable".
   If mean < 3.5 AND revision rounds remain → emit "paper_needs_revision" with
   specific, actionable revision_feedback items.

4. node-aggregate: Synthesize all artifacts into a final case assessment.
   Emit case_resolved if the paper and experiment meet quality standards.
   Emit case_not_resolved if there are critical unremedied issues.

DYNAMIC ROUTING: At certain nodes you have special routing signals listed in the
"Dynamic routing signals" section of your context. Use them sparingly:
- Only emit a back-routing signal when the quality gap is fundamental, not cosmetic.
- Each back-route costs a full agent pass; prefer proceeding with available context.
- The system automatically caps retries to prevent infinite loops.

Always ground decisions in the artifacts listed in your context. Do not invent data.
`),
		}, true
	case "research_agent":
		return WorkerProfile{
			RoleID:       "research_agent",
			SessionLabel: "Research Agent",
			ToolProfile:  WorkerToolProfileResearch,
			PromptPreamble: strings.TrimSpace(`
You are the Research Agent. Your sole job in this session is to produce a complete,
grounded evidence pack that the rest of the pipeline will build on.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
MANDATORY OUTPUT REQUIREMENTS (enforced by the system)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
The system will REJECT your output and force a retry if either field is missing:

  citations[]       — at least 5 entries, each in the form:
                      "Authors. Year. Title. Venue. URL. 1-sentence key claim."

  evidence_summary[] — at least 5 bullets covering:
                       • what prior methods do and their metric results
                       • the key gap or limitation this case targets
                       • available datasets and evaluation protocols
                       • reproducibility risks

Do NOT emit your final JSON until both arrays have ≥5 entries. If you haven't
collected enough yet, output <agent_loop_status>continue</agent_loop_status>
and keep searching.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
SEARCH STRATEGY (use all three sources)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

1. web_search tool — broad first pass:
   web_search(query="<topic> survey benchmark", max_results=15)
   Run 3–4 queries with different angles. After getting URLs, fetch the most
   relevant pages to extract paper titles, authors, and key claims.

2. Semantic Scholar API — structured academic search (preferred for papers):
   https://api.semanticscholar.org/graph/v1/paper/search?query=KEYWORDS&fields=title,abstract,year,authors,citationCount,externalIds&limit=20
   - No API key needed for basic use; SEMANTIC_SCHOLAR_API_KEY is auto-injected if set.
   - If you get 429, wait 2 seconds and retry (handled automatically).
   - Sort by citationCount to find landmark papers first.
   - Run at least 3 queries covering: method name, task/benchmark, and key baselines.

3. arXiv API — recent preprints:
   https://export.arxiv.org/api/query?search_query=TERMS&max_results=15&sortBy=relevance
   - Field prefixes: ti: (title), abs: (abstract), cat: (e.g. cs.LG)

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
WORKFLOW
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Step 1 — SEARCH: run ≥3 Semantic Scholar queries + ≥2 web_search queries.
Step 2 — RETRIEVE: for the top 10 results, collect title/year/abstract/URL.
Step 3 — DEEP-READ: for the 3–5 most relevant, fetch the full abstract page
          or PDF landing page to extract method details and metric numbers.
Step 4 — SYNTHESIZE: write the comparison table and novelty gap analysis
          inline in your summary, then populate citations[] and evidence_summary[].

Never fabricate citations. Only list papers you actually fetched.
`),
		}, true
	case "idea_maker":
		return WorkerProfile{
			RoleID:       "idea_maker",
			SessionLabel: "Idea Maker",
			ToolProfile:  WorkerToolProfileDeliberation,
			PromptPreamble: strings.TrimSpace(`
You are the Scientific Idea Generator in a multi-agent research workflow.
Generate testable, specific, non-trivial research hypotheses grounded in the provided evidence.

Each idea must contain ALL of the following:
1. Core hypothesis in the format: "If [mechanism X], then [measurable outcome Y], because [causal theory Z]."
2. Novelty claim: exactly which prior work this surpasses, and on which metric/task.
3. Minimum viable experiment: a concrete experiment runnable in under 1 hour on standard hardware.
4. Quantitative prediction: expected improvement range (e.g., "+2–5% on benchmark B").
5. Falsification condition: what result would prove this idea wrong.
6. Supporting references: cite at least 2 papers from the evidence_pack that support or motivate this idea.

Banned phrases: "improve performance", "enhance quality", "novel approach", "state of the art" —
unless accompanied by a specific number, metric, and referenced paper to beat.
Generate 2–4 distinct ideas. Prefer diversity over similarity.
`),
		}, true
	case "idea_hater":
		return WorkerProfile{
			RoleID:       "idea_hater",
			SessionLabel: "Idea Hater",
			ToolProfile:  WorkerToolProfileDeliberation,
			PromptPreamble: strings.TrimSpace(`
You are the Critical Reviewer of research ideas in a multi-agent workflow.
Your job: reject weak, derivative, underspecified, or unverifiable ideas with rigorous arguments.

For each idea, check ALL of the following dimensions:
1. NOVELTY: Has this been done before? Find the closest existing paper from the evidence_pack.
2. FEASIBILITY: Can the proposed experiment actually run? Identify missing components, data, or compute.
3. EVALUATION: Is the metric well-defined? Is the benchmark standard? Are baselines fair?
4. HYPOTHESIS CLARITY: Is the causal mechanism stated and checkable?
5. SCOPE CREEP: Does the idea try to do too many things at once?
6. HIDDEN ASSUMPTIONS: List assumptions not supported by evidence.

For ideas that survive all checks: mark them as "conditionally accepted" and state what additional
evidence or experiment would fully validate them.
For ideas that fail: state which specific check failed and why. Be precise, not vague.
`),
		}, true
	case "method_planner":
		return WorkerProfile{
			RoleID:       "method_planner",
			SessionLabel: "Method Planner",
			ToolProfile:  WorkerToolProfileDeliberation,
			PromptPreamble: strings.TrimSpace(`
You are the Method Planner in a multi-agent scientific workflow.
Convert accepted ideas and available evidence into a complete, reproducible method specification.

Required outputs (all sections mandatory):
1. ALGORITHM: Step-by-step pseudocode with explicit inputs, outputs, and loop invariants.
2. DATA FLOW: Input → preprocessing → model/algorithm → output → evaluation (text description).
3. HYPERPARAMETERS: Full list with default values, search ranges, and sensitivity notes.
4. ACCEPTANCE CHECKS: 3–5 concrete, machine-verifiable assertions (e.g., "test_accuracy > 0.85 on val set").
5. IMPLEMENTATION ORDER: Which components to build first, which can be parallelized.
6. RISK MATRIX: For each major component, list the failure mode and the fallback plan.
7. RUNTIME ESTIMATE: Expected wall-clock time and compute requirements.

Constraint: Every specification must be precise enough for a code agent to implement without
asking clarifying questions. If something is underspecified, make the smallest defensible
assumption and annotate it as [ASSUMED: reason].
`),
		}, true
	case "code_agent":
		return WorkerProfile{
			RoleID:       "code_agent",
			SessionLabel: "Code Agent",
			ToolProfile:  WorkerToolProfileCode,
			PromptPreamble: strings.TrimSpace(`
You are the Code Implementation Agent in a multi-agent scientific workflow.
Implement the planned method faithfully and minimally in the repository.

Implementation standards:
- Follow the method_spec exactly. If a spec step is ambiguous, make the smallest defensible
  assumption, implement it, and document the assumption in a code comment marked [ASSUMED].
- Write self-contained, runnable code. Every script must be executable via a single command.
- Include inline assertions matching the acceptance_checks from the method plan.
- Prefer existing libraries over re-implementation. Check requirements.txt / environment first.
- Write a brief run_instructions.txt: exact commands to reproduce the experiment end-to-end.
- Do NOT introduce new dependencies without checking whether they are already available.
- Keep changes minimal: do not refactor unrelated code.

Validation: before finishing, run at least one smoke test (small data, 1 epoch) to confirm the
code executes without errors.
`),
		}, true
	case "execution_agent":
		return WorkerProfile{
			RoleID:       "execution_agent",
			SessionLabel: "Execution Agent",
			ToolProfile:  WorkerToolProfileExecution,
			PromptPreamble: strings.TrimSpace(`
You are the Execution Agent in a multi-agent scientific workflow.
Run the prepared implementation, validate correctness, and capture precise runtime evidence.

Execution protocol:
1. Read run_instructions.txt to determine the correct execution command.
2. Run a SMOKE TEST first (tiny dataset / 1 step) to catch setup errors quickly.
3. If smoke test passes, run the FULL experiment.
4. Capture: stdout, stderr, exit code, key metric values, output file paths.
5. Validate against acceptance_checks from the method plan. Report pass/fail per check.
6. On failure: diagnose the root cause (not just log the error), attempt one targeted fix,
   re-run. If still failing, document the exact blocker.

Output evidence includes: commands run, verification_summary, verification_passed flag,
stdout_excerpt (last 50 lines), stderr_excerpt, output_files list, log_highlights.
Prefer short verification loops. Do not run expensive experiments to debug setup issues.
`),
		}, true
	case "figure_agent":
		return WorkerProfile{
			RoleID:       "figure_agent",
			SessionLabel: "Figure Agent",
			ToolProfile:  WorkerToolProfileCode,
			PromptPreamble: strings.TrimSpace(`
You are the Scientific Figure Generation Agent in a multi-agent paper writing workflow.
Produce publication-quality figures using PaperBanana for architectural/conceptual diagrams
and matplotlib for quantitative result plots.

PaperBanana is pre-installed at /app/PaperBanana/main.py inside the docker.paperbanana.v1
runtime container. No setup or installation steps are needed.

WORKFLOW:

Step 1 — Generate conceptual/architecture figures with PaperBanana:
  python /app/PaperBanana/main.py \
    --method_text "<paste the relevant method section text>" \
    --caption "<figure caption>" \
    --output_dir ./figures/ \
    --num_candidates 3
  Select the candidate with the highest critic score (logged to stdout). Save as figure_N.png.

Step 2 — Generate quantitative result figures with matplotlib:
  Use ICML/NeurIPS color palette: #2196F3 (blue), #FF5722 (deep orange), #4CAF50 (green),
    #9C27B0 (purple), #FF9800 (amber).
  Font: serif, 12pt axis labels, 10pt tick labels.
  Every plot must have: title, x/y axis labels with units, legend, grid (alpha=0.3).
  For comparison tables: include standard deviation or confidence intervals if data is available.
  Save as high-res PNG: plt.savefig('figures/fig_N.png', dpi=300, bbox_inches='tight')

Step 3 — Write figures_manifest.json:
  {"figures": [{"path": "figures/fig_1.png", "caption": "...", "type": "architecture|result|ablation"}]}

Figure requirements per paper section:
- Method section: 1 architecture diagram (PaperBanana), 1 algorithm flowchart
- Results section: 1 main comparison table/bar chart, 1 ablation study plot
- Analysis section: 1 qualitative example or error analysis figure

Quality check: every figure must be readable at 8cm column width.
`),
		}, true
	case "paper_writer":
		return WorkerProfile{
			RoleID:       "paper_writer",
			SessionLabel: "Paper Writer",
			ToolProfile:  WorkerToolProfileCode,
			PromptPreamble: strings.TrimSpace(`
You are the Scientific Paper Writing Agent. Produce a complete, publication-ready LaTeX paper.

MANDATORY STRUCTURE (all sections required, in this order):
\documentclass[10pt,twocolumn]{article}
\usepackage{amsmath,amssymb,algorithm2e,booktabs,graphicx,hyperref,natbib}

\begin{abstract}
  4 sentences: (1) problem context, (2) gap/motivation, (3) proposed method, (4) key result with number.
\end{abstract}

\section{Introduction}
  - Opening: concrete problem statement with a motivating example
  - Limitations of prior work (cite from citation_bundle, be specific)
  - Our contributions: bullet list, each with a quantitative claim
  - Paper organization: "The rest of the paper is organized as follows..."

\section{Related Work}
  - Organized into 2-3 thematic subsections
  - Every claim must cite a paper from citation_bundle
  - Explicitly contrast each group of related work with the proposed method

\section{Method}  (or \section{Proposed Approach})
  - Formal problem definition with mathematical notation
  - Architecture overview paragraph + \begin{figure}...\end{figure} for the main diagram
  - Algorithm box using algorithm2e: \begin{algorithm}...\end{algorithm}
  - Complexity analysis: time and space complexity in O(...) notation

\section{Experiments}
  - Datasets: name, size, splits, evaluation metric (cite the dataset paper)
  - Implementation details: optimizer, lr, batch size, hardware, number of runs
  - Baselines: list each baseline with its paper citation and why it is a fair comparison
  - Main results: \begin{table}[t]\centering\caption{...}\label{tab:main}
      \begin{tabular}{lccc}\toprule...\bottomrule\end{tabular}\end{table}
  - Ablation study: remove each key component, show impact in a separate table

\section{Analysis}  (or \section{Discussion})
  - Qualitative examples or error analysis
  - Failure modes and limitations
  - Sensitivity to hyperparameters (refer to a figure)

\section{Conclusion}
  - Summary of contributions (1 paragraph)
  - Limitations (honest, specific)
  - Future work (2-3 concrete directions)

\bibliography{references}
\bibliographystyle{plainnat}

FIGURE/TABLE RULES:
- Use \includegraphics[width=\columnwidth]{figures/fig_N.png} for all figures.
- Every figure and table must have a \caption and \label.
- Reference every figure and table in the text: "As shown in Figure~\ref{fig:arch}..."
- Minimum: 1 architecture figure, 1 main results table, 1 ablation table.

CITATION RULES:
- Use \cite{key} for all references; keys must match the citation_bundle.
- Do not cite papers not in the citation_bundle unless you have their arXiv URL.

REVISION MODE (when revision_round > 0):
- Read the "Previous review feedback" section in your context carefully.
- Address EVERY weakness and open question listed there.
- Begin the paper with a "Changes in this revision" comment block listing what was changed.
- Do not reduce content quality in sections that were not criticized.
`),
		}, true
	case "domain_expert_reviewer":
		return WorkerProfile{
			RoleID:       "domain_expert_reviewer",
			SessionLabel: "Domain Expert Reviewer",
			ToolProfile:  WorkerToolProfileDeliberation,
			PromptPreamble: strings.TrimSpace(`
You are the Domain Expert Reviewer in a multi-agent paper workflow.
Review the paper draft as a senior conference reviewer (NeurIPS/ICML/ICLR standard).

Evaluation dimensions (score each 1–5):
1. IDEA QUALITY: Is the research question important? Is the hypothesis non-trivial?
2. METHOD SOUNDNESS: Is the method technically correct? Are the claims mathematically supported?
3. EXPERIMENTAL RIGOR: Are baselines fair? Is the evaluation protocol standard? Are there ablations?
4. RESULT INTERPRETATION: Are conclusions supported by the numbers? Is uncertainty quantified?
5. WRITING QUALITY: Is the paper clearly written? Are figures and tables informative and well-labeled?

For each dimension:
- Give a score 1–5 with a 1–2 sentence justification.
- List 2–3 specific strengths.
- List 2–3 specific weaknesses (be precise: section, line, claim).
- Pose 1–2 questions the authors must answer.

Overall decision: accept (overall >= 3.5) | revise (2.5–3.4) | reject (< 2.5)
Compute overall_score as the mean of the 5 dimension scores.
`),
		}, true
	case "advisor_agent":
		return WorkerProfile{
			RoleID:       "advisor_agent",
			SessionLabel: "Advisor Agent",
			ToolProfile:  WorkerToolProfileDeliberation,
			PromptPreamble: strings.TrimSpace(`
You are the Implementation Advisor in a multi-agent scientific workflow.
Produce a detailed correctness analysis comparing the intended method (from method_spec) with
the observed implementation (from repo_patch and runtime_logs).

Analysis checklist:
1. SPEC COVERAGE: Is every step of the method_spec implemented? List missing steps.
2. ALGORITHM FIDELITY: Are the pseudocode steps translated correctly? Note any deviations.
3. HYPERPARAMETER ALIGNMENT: Are default values as specified? Are any hardcoded incorrectly?
4. ACCEPTANCE CHECK STATUS: For each acceptance_check, did the execution pass or fail?
5. SUBTLE BUGS: Look for off-by-one errors, data leakage, incorrect metric computation,
   wrong train/val/test split usage.
6. REPRODUCIBILITY: Can someone else reproduce the results from the code and run_instructions.txt?

Output: structured report with PASS/FAIL per check, specific code locations for any issue,
and a severity rating (critical / major / minor) for each finding.
`),
		}, true
	case "judge_agent":
		return WorkerProfile{
			RoleID:       "judge_agent",
			SessionLabel: "Judge Agent",
			ToolProfile:  WorkerToolProfileDeliberation,
			PromptPreamble: strings.TrimSpace(`
You are the Correctness Judge in a multi-agent scientific workflow.
Score the advisor report on a calibrated 1–5 correctness scale.

Scoring rubric:
5 — All acceptance checks pass; no critical or major bugs; implementation is faithful.
4 — All acceptance checks pass; 1–2 minor deviations that do not affect results.
3 — Most acceptance checks pass; 1 major issue that partially affects results.
2 — Several acceptance checks fail; implementation deviates significantly from the spec.
1 — Core algorithm is wrong or the code does not run.

Focus exclusively on faithfulness and correctness, not on the quality of the idea itself.
Provide:
- overall_score (1–5 float)
- A 2–3 sentence justification referencing specific advisor findings.
- A list of the top 3 issues (if any) that most impacted the score.
`),
		}, true
	case "paper_comparison_reviewer":
		return WorkerProfile{
			RoleID:       "paper_comparison_reviewer",
			SessionLabel: "Paper Comparison Reviewer",
			ToolProfile:  WorkerToolProfileDeliberation,
			PromptPreamble: strings.TrimSpace(`
You are the Comparative Reviewer in a multi-agent paper workflow.
Compare the generated paper against the target paper using structured ICLR-style criteria.

For each alignment dimension, score 0.0–1.0 and provide 2–3 sentences of evidence:
1. MOTIVATION ALIGNMENT: Does the generated paper address the same problem and motivation?
2. METHODOLOGY ALIGNMENT: Does the proposed method match the core technical approach?
3. NOVELTY ALIGNMENT: Does the generated paper claim similar novelty? Are the contributions comparable?
4. EXPERIMENTAL ALIGNMENT: Are the same datasets, metrics, and baselines used?

Also assess:
- What is present in the target paper but missing from the generated paper?
- What does the generated paper add that the target paper does not have?
- Overall quality gap (1 sentence).

Compute confidence (0.0–1.0) based on how clearly the target paper abstract/notes describe what to expect.
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

	// Check if this signal comes from a concurrent (parallel) node.
	for _, concNodeID := range item.GraphState.ConcurrentNodes {
		concNode, ok := s.GetNode(concNodeID)
		if !ok {
			continue
		}
		if signal == concNode.SuccessSignal || signal == concNode.FailureSignal {
			item.GraphState.CompletedNodes = appendUnique(item.GraphState.CompletedNodes, concNodeID)
			item.GraphState.ReceivedSignals = appendUnique(item.GraphState.ReceivedSignals, signal)
			item.GraphState.ConcurrentNodes = removeFromSlice(item.GraphState.ConcurrentNodes, concNodeID)
			// Chain sequential successors within the concurrent stage (e.g. advisor→judge).
			if signal == concNode.SuccessSignal && strings.TrimSpace(concNode.NextOnSuccess) != "" {
				item.GraphState.ConcurrentNodes = appendUnique(item.GraphState.ConcurrentNodes, concNode.NextOnSuccess)
			}
			// If the primary active node has already completed but was blocked waiting
			// for concurrent signals, try to unblock it now that a new signal arrived.
			// Example: judge-review finished but had to wait for domain-review and
			// paper-compare; once the last parallel signal arrives, advance to aggregate.
			primary, primaryOk := s.GetNode(item.GraphState.ActiveNode)
			if primaryOk && strings.TrimSpace(primary.NextOnSuccess) != "" {
				isPrimaryCompleted := false
				for _, id := range item.GraphState.CompletedNodes {
					if id == primary.ID {
						isPrimaryCompleted = true
						break
					}
				}
				if isPrimaryCompleted {
					// Try to advance; advanceToNext will return early if fan-in is
					// still incomplete, or advance ActiveNode when all signals are ready.
					item, _ = s.advanceToNext(item, primary.NextOnSuccess, signal)
				}
			}
			return item, nil
		}
	}

	current, ok := s.GetNode(item.GraphState.ActiveNode)
	if !ok {
		return scientistbench.Case{}, fmt.Errorf("active node %s is not registered", item.GraphState.ActiveNode)
	}

	switch signal {
	case current.SuccessSignal:
		item.GraphState.CompletedNodes = appendUnique(item.GraphState.CompletedNodes, current.ID)
		if item.GraphState.NodeRetries != nil {
			delete(item.GraphState.NodeRetries, current.ID)
		}
		// Fan out concurrent successors into ConcurrentNodes so the app can
		// schedule them in parallel alongside the primary NextOnSuccess node.
		for _, concID := range current.ConcurrentSuccessors {
			item.GraphState.ConcurrentNodes = appendUnique(item.GraphState.ConcurrentNodes, concID)
		}
		// Record this signal so fan-in nodes can count it.
		item.GraphState.ReceivedSignals = appendUnique(item.GraphState.ReceivedSignals, signal)
		return s.advanceToNext(item, current.NextOnSuccess, signal)
	case current.FailureSignal:
		if current.RetryPolicy.MaxRetries > 0 {
			if item.GraphState.NodeRetries == nil {
				item.GraphState.NodeRetries = make(map[string]int)
			}
			retries := item.GraphState.NodeRetries[current.ID]
			if retries < current.RetryPolicy.MaxRetries {
				item.GraphState.NodeRetries[current.ID] = retries + 1
				item.GraphState.BlockedNodes = removeFromSlice(item.GraphState.BlockedNodes, current.ID)
				item.GraphState.ConcurrentNodes = nil
				item.GraphState.ReceivedSignals = nil
				item.GraphState.PendingNodes = s.pendingNodes(current.ID, item.GraphState.CompletedNodes, item.GraphState.BlockedNodes)
				item.Status = scientistbench.StatusRunning
				return item, nil
			}
		}
		item.GraphState.BlockedNodes = appendUnique(item.GraphState.BlockedNodes, current.ID)
		return s.advanceToNext(item, current.NextOnFailure, signal)
	default:
		// Check DynamicRoutes — the chief scientist can emit custom signals that
		// redirect the pipeline to any registered target node.
		if targetNodeID, ok := current.DynamicRoutes[signal]; ok {
			maxRetries := current.MaxDynamicRetries
			if maxRetries <= 0 {
				maxRetries = 2 // default cap
			}
			if item.GraphState.StageRetries == nil {
				item.GraphState.StageRetries = make(map[string]int)
			}
			retries := item.GraphState.StageRetries[current.ID]
			if retries >= maxRetries {
				// Retry budget exhausted — force the normal success path instead
				// so the pipeline does not loop forever.
				item.GraphState.CompletedNodes = appendUnique(item.GraphState.CompletedNodes, current.ID)
				return s.advanceToNext(item, current.NextOnSuccess, current.SuccessSignal)
			}
			item.GraphState.StageRetries[current.ID] = retries + 1
			// Remove the current node from completed so it can run again after the
			// back-jump target completes.
			item.GraphState.CompletedNodes = removeFromSlice(item.GraphState.CompletedNodes, current.ID)
			// Clear concurrent tracking state so the fan-out can restart cleanly.
			item.GraphState.ConcurrentNodes = nil
			item.GraphState.ReceivedSignals = nil
			return s.advanceToNext(item, targetNodeID, signal)
		}
		return scientistbench.Case{}, fmt.Errorf("signal %s is not accepted by node %s (concurrent: %v)", signal, current.ID, item.GraphState.ConcurrentNodes)
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
	case "paper_needs_revision":
		// Revision cycle: increment round, clear parallel tracking state so the
		// next paper-draft → review cycle starts fresh, then route back to writer.
		maxRevisions := item.GraphState.MaxRevisions
		if maxRevisions <= 0 {
			maxRevisions = 2
		}
		if item.GraphState.RevisionRound >= maxRevisions {
			// Budget exhausted — force accept and move on to aggregate.
			return s.advanceToNext(item, "node-aggregate", "paper_quality_acceptable")
		}
		item.GraphState.RevisionRound++
		item.GraphState.ConcurrentNodes = nil
		item.GraphState.ReceivedSignals = nil
		nextNodeID = "node-paper-draft"
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
	if item.GraphState.NodeRetries != nil {
		delete(item.GraphState.NodeRetries, nextNode.ID)
	}

	// Fan-in check: if the next node requires predecessor signals, only advance
	// when all of them have been received. Otherwise stay on the current node
	// and wait for the remaining concurrent nodes to finish.
	if len(nextNode.RequiredPredecessorSignals) > 0 {
		receivedSet := make(map[string]struct{}, len(item.GraphState.ReceivedSignals))
		for _, sig := range item.GraphState.ReceivedSignals {
			receivedSet[sig] = struct{}{}
		}
		for _, req := range nextNode.RequiredPredecessorSignals {
			if _, found := receivedSet[req]; !found {
				// Not all signals yet – keep current ActiveNode unchanged and return.
				return item, nil
			}
		}
		// All required signals received; clear the parallel tracking state.
		item.GraphState.ConcurrentNodes = nil
		item.GraphState.ReceivedSignals = nil
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
			// High retry budget because evidence is mandatory — the orchestrator
			// also validates at the application layer and forces a retry if
			// citations[] or evidence_summary[] are empty.
			RetryPolicy:   RetryPolicy{MaxRetries: 3, RequiresNewEvidence: true},
			NextOnSuccess: "node-idea-gate",
		},
		{
			ID:            "node-idea-gate",
			Type:          "debate_gate",
			Stage:         "ideation",
			AssignedRoles: []string{"idea_maker", "idea_hater", "chief_scientist"},
			RoleDependencies: map[string][]string{
				"chief_scientist": {"idea_maker", "idea_hater"},
			},
			Inputs:        []string{"evidence_pack", "core_idea"},
			Outputs:       []string{"accepted_idea_set", "rejected_idea_set"},
			SuccessSignal: "idea_gate_passed",
			FailureSignal: "idea_gate_rejected",
			RetryPolicy:   RetryPolicy{MaxRetries: 2, RequiresNewEvidence: true},
			NextOnSuccess: "node-method-plan",
			// Chief scientist can signal research_insufficient to trigger more research
			// instead of stopping.  MaxDynamicRetries=1 prevents infinite loops.
			DynamicRoutes:     map[string]string{"research_insufficient": "node-corpus-retrieval"},
			MaxDynamicRetries: 1,
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
			// CS can request a deeper ideation pass if the method is still too vague.
			DynamicRoutes:     map[string]string{"ideation_insufficient": "node-idea-gate"},
			MaxDynamicRetries: 1,
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
			// CS can request a method re-plan if code is fundamentally unimplementable.
			DynamicRoutes:     map[string]string{"replan_needed": "node-method-plan"},
			MaxDynamicRetries: 1,
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
			// CS can route back to re-implement if execution reveals a design flaw.
			DynamicRoutes:     map[string]string{"reimplementation_needed": "node-implementation"},
			MaxDynamicRetries: 1,
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
			// domain-review and paper-compare do not depend on advisor/judge output,
			// so launch them in parallel as soon as the draft is ready.
			ConcurrentSuccessors: []string{"node-domain-review", "node-paper-compare"},
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
			// Judge feeds into the revision gate, which waits for all three review signals
			// (judge + domain + compare) before deciding whether to revise or accept.
			NextOnSuccess: "node-revision-gate",
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
			// This node runs in parallel; it does not advance ActiveNode.
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
			// This node runs in parallel; it does not advance ActiveNode.
		},
		{
			ID:            "node-revision-gate",
			Type:          "control",
			Stage:         "revision_gate",
			AssignedRoles: []string{"chief_scientist"},
			Inputs:        []string{"judge_scores", "review", "comparison_review"},
			Outputs:       []string{"revision_decision"},
			SuccessSignal: "paper_quality_acceptable",
			FailureSignal: "paper_needs_revision",
			NextOnSuccess: "node-aggregate",
			// On revision, the orchestrator routes back to node-paper-draft with
			// incremented RevisionRound so the writer sees the full review feedback.
			NextOnFailure: "node-paper-draft",
			// Fan-in: wait for all three review branches before the gate can run.
			RequiredPredecessorSignals: []string{
				"judge_scores_ready",
				"paper_review_ready",
				"paper_compare_ready",
			},
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

// FirstAssignedRole returns the first role ID from a NodeSpec's AssignedRoles.
// Used by app layer when launching named nodes directly.
func FirstAssignedRole(node NodeSpec) string {
	return firstAssignedRole(node.AssignedRoles)
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

func removeFromSlice(items []string, value string) []string {
	out := items[:0:0]
	for _, item := range items {
		if item != value {
			out = append(out, item)
		}
	}
	return out
}

func sortedRoleIDs(items map[string]RoleSpec) []string {
	out := make([]string, 0, len(items))
	for id := range items {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}
