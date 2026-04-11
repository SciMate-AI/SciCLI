package scientistbench

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/SciMate-AI/scicli/internal/config"
	"github.com/SciMate-AI/scicli/internal/pubsub"
	"github.com/google/uuid"
)

type CaseMode string

const (
	ModePaperGeneration CaseMode = "paper_generation"
	ModeReproduction    CaseMode = "reproduction"
	ModeJoint           CaseMode = "joint"
)

type BenchmarkLevel string

const (
	Level1 BenchmarkLevel = "level1"
	Level2 BenchmarkLevel = "level2"
)

type CaseStatus string

const (
	StatusPlanning    CaseStatus = "planning"
	StatusRunning     CaseStatus = "running"
	StatusResolved    CaseStatus = "resolved"
	StatusNotResolved CaseStatus = "not_resolved"
	StatusBlocked     CaseStatus = "blocked"
)

type TerminationSignal string

const (
	SignalCaseResolved    TerminationSignal = "case_resolved"
	SignalCaseNotResolved TerminationSignal = "case_not_resolved"
	SignalBudgetExhausted TerminationSignal = "budget_exhausted"
	SignalHardBlocked     TerminationSignal = "hard_blocked"
	SignalManualStop      TerminationSignal = "manual_stop"
)

type ReviewType string

const (
	ReviewDomainExpert    ReviewType = "domain_expert"
	ReviewAdvisor         ReviewType = "advisor"
	ReviewJudge           ReviewType = "judge"
	ReviewPaperComparison ReviewType = "paper_comparison"
)

type ArtifactKind string

const (
	ArtifactPaperDraft    ArtifactKind = "paper_draft"
	ArtifactFigure        ArtifactKind = "figure"
	ArtifactTable         ArtifactKind = "table"
	ArtifactCode          ArtifactKind = "code"
	ArtifactLog           ArtifactKind = "log"
	ArtifactResultBundle  ArtifactKind = "result_bundle"
	ArtifactReview        ArtifactKind = "review"
	ArtifactCitation      ArtifactKind = "citation_bundle"
	ArtifactAdvisorReport ArtifactKind = "advisor_report"
	ArtifactJudgeReport   ArtifactKind = "judge_report"
	ArtifactDockerLog     ArtifactKind = "docker_log"
	ArtifactLatexPDF      ArtifactKind = "latex_pdf"
)

type Budget struct {
	MaxWallClockMinutes int     `json:"max_wall_clock_minutes,omitempty"`
	MaxAgentSteps       int     `json:"max_agent_steps,omitempty"`
	MaxToolCalls        int     `json:"max_tool_calls,omitempty"`
	MaxJudgeRepeats     int     `json:"max_judge_repeats,omitempty"`
	MaxCostUSD          float64 `json:"max_cost_usd,omitempty"`
	MaxParallelWorkers  int     `json:"max_parallel_workers,omitempty"`
}

type Reference struct {
	ID              string   `json:"id,omitempty"`
	Title           string   `json:"title,omitempty"`
	Authors         []string `json:"authors,omitempty"`
	Year            int      `json:"year,omitempty"`
	Venue           string   `json:"venue,omitempty"`
	URL             string   `json:"url,omitempty"`
	PDFURL          string   `json:"pdf_url,omitempty"`
	Abstract        string   `json:"abstract,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	LocalArtifactID string   `json:"local_artifact_id,omitempty"`
	Priority        string   `json:"priority,omitempty"`
}

type Dataset struct {
	Name               string   `json:"name,omitempty"`
	SourceType         string   `json:"source_type,omitempty"`
	URI                string   `json:"uri,omitempty"`
	License            string   `json:"license,omitempty"`
	TaskType           string   `json:"task_type,omitempty"`
	Splits             []string `json:"splits,omitempty"`
	EvaluationProtocol string   `json:"evaluation_protocol,omitempty"`
	Notes              string   `json:"notes,omitempty"`
}

type TargetPaper struct {
	PaperID              string   `json:"paper_id,omitempty"`
	Title                string   `json:"title,omitempty"`
	Authors              []string `json:"authors,omitempty"`
	Year                 int      `json:"year,omitempty"`
	Venue                string   `json:"venue,omitempty"`
	URL                  string   `json:"url,omitempty"`
	PDFURL               string   `json:"pdf_url,omitempty"`
	Abstract             string   `json:"abstract,omitempty"`
	GroundTruthArtifacts []string `json:"ground_truth_artifacts,omitempty"`
	EvaluationNotes      string   `json:"evaluation_notes,omitempty"`
}

type MaskedMethodSpec struct {
	PipelineSummary  []string `json:"pipeline_summary,omitempty"`
	KnownComponents  []string `json:"known_components,omitempty"`
	HiddenComponents []string `json:"hidden_components,omitempty"`
	AllowedClues     []string `json:"allowed_clues,omitempty"`
	ForbiddenLeakage []string `json:"forbidden_leakage,omitempty"`
}

type Inputs struct {
	ReferenceSet     []Reference      `json:"reference_set,omitempty"`
	CoreIdea         string           `json:"core_idea,omitempty"`
	Dataset          Dataset          `json:"dataset,omitempty"`
	TargetPaper      TargetPaper      `json:"target_paper,omitempty"`
	MaskedMethodSpec MaskedMethodSpec `json:"masked_method_spec,omitempty"`
	Constraints      []string         `json:"constraints,omitempty"`
}

type GraphState struct {
	CurrentStage   string   `json:"current_stage,omitempty"`
	ActiveNode     string   `json:"active_node,omitempty"`
	ActiveRole     string   `json:"active_role,omitempty"`
	// ConcurrentNodes holds the IDs of nodes being run in parallel with ActiveNode.
	ConcurrentNodes []string `json:"concurrent_nodes,omitempty"`
	// ReceivedSignals records signals emitted by concurrent nodes for fan-in checks.
	ReceivedSignals []string `json:"received_signals,omitempty"`
	// RevisionRound tracks how many paper revision cycles have completed (0 = initial draft).
	RevisionRound int `json:"revision_round,omitempty"`
	// MaxRevisions caps the total number of revision cycles (0 means use default of 2).
	MaxRevisions int `json:"max_revisions,omitempty"`
	// StageRetries tracks how many times the chief scientist has dynamically re-routed
	// back to an earlier stage, keyed by the source node ID. Used to prevent infinite
	// loops when the CS keeps requesting retries.
	StageRetries   map[string]int `json:"stage_retries,omitempty"`
	PendingNodes   []string `json:"pending_nodes,omitempty"`
	CompletedNodes []string `json:"completed_nodes,omitempty"`
	BlockedNodes   []string `json:"blocked_nodes,omitempty"`
}

type IdeaCandidate struct {
	ID             string   `json:"id,omitempty"`
	Title          string   `json:"title,omitempty"`
	Summary        string   `json:"summary,omitempty"`
	NoveltyClaim   string   `json:"novelty_claim,omitempty"`
	Hypotheses     []string `json:"hypotheses,omitempty"`
	SupportingRefs []string `json:"supporting_refs,omitempty"`
	CreatedAt      int64    `json:"created_at,omitempty"`
}

type IdeaObjection struct {
	ID           string   `json:"id,omitempty"`
	IdeaID       string   `json:"idea_id,omitempty"`
	Summary      string   `json:"summary,omitempty"`
	Severity     string   `json:"severity,omitempty"`
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
	CreatedAt    int64    `json:"created_at,omitempty"`
}

type IdeaModule struct {
	Status        string          `json:"status,omitempty"`
	AcceptedIdeas []IdeaCandidate `json:"accepted_ideas,omitempty"`
	RejectedIdeas []IdeaCandidate `json:"rejected_ideas,omitempty"`
	Objections    []IdeaObjection `json:"objections,omitempty"`
}

type RunRecord struct {
	ID               string   `json:"id,omitempty"`
	ParentRunID      string   `json:"parent_run_id,omitempty"`
	SessionID        string   `json:"session_id,omitempty"`
	Role             string   `json:"role,omitempty"`
	RoleInstance     string   `json:"role_instance,omitempty"`
	NodeID           string   `json:"node_id,omitempty"`
	Status           string   `json:"status,omitempty"`
	StartedAt        int64    `json:"started_at,omitempty"`
	FinishedAt       int64    `json:"finished_at,omitempty"`
	InputSummary     string   `json:"input_summary,omitempty"`
	OutputSummary    string   `json:"output_summary,omitempty"`
	TaskRunSessionID string   `json:"taskrun_session_id,omitempty"`
	TaskRunStatus    string   `json:"taskrun_status,omitempty"`
	ToolCalls        []string `json:"tool_calls,omitempty"`
	SignalsEmitted   []string `json:"signals_emitted,omitempty"`
	Error            string   `json:"error,omitempty"`
}

type Artifact struct {
	ID            string            `json:"artifact_id,omitempty"`
	Kind          ArtifactKind      `json:"kind,omitempty"`
	Label         string            `json:"label,omitempty"`
	URI           string            `json:"uri,omitempty"`
	Path          string            `json:"path,omitempty"`
	ProducerRole  string            `json:"producer_role,omitempty"`
	ProducerRunID string            `json:"producer_run_id,omitempty"`
	CreatedAt     int64             `json:"created_at,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	QualityFlags  []string          `json:"quality_flags,omitempty"`
}

type ReviewScores struct {
	Overall              float64 `json:"overall,omitempty"`
	IdeaQuality          float64 `json:"idea_quality,omitempty"`
	MethodSoundness      float64 `json:"method_soundness,omitempty"`
	ResultInterpretation float64 `json:"result_interpretation,omitempty"`
	WritingQuality       float64 `json:"writing_quality,omitempty"`
}

type Review struct {
	ID                  string       `json:"review_id,omitempty"`
	Type                ReviewType   `json:"review_type,omitempty"`
	ReviewerRole        string       `json:"reviewer_role,omitempty"`
	TargetArtifactID    string       `json:"target_artifact_id,omitempty"`
	TargetPaperID       string       `json:"target_paper_id,omitempty"`
	CreatedAt           int64        `json:"created_at,omitempty"`
	Decision            string       `json:"decision,omitempty"`
	Summary             string       `json:"summary,omitempty"`
	Strengths           []string     `json:"strengths,omitempty"`
	Weaknesses          []string     `json:"weaknesses,omitempty"`
	Questions           []string     `json:"questions,omitempty"`
	Scores              ReviewScores `json:"scores,omitempty"`
	Confidence          float64      `json:"confidence,omitempty"`
	EvidenceArtifactIDs []string     `json:"evidence_artifact_ids,omitempty"`
}

type PaperGenerationScores struct {
	ReadablePaper        bool    `json:"readable_paper,omitempty"`
	NovelInsightPresent  bool    `json:"novel_insight_present,omitempty"`
	CodeRuns             bool    `json:"code_runs,omitempty"`
	IdeaQuality          float64 `json:"idea_quality,omitempty"`
	MethodSoundness      float64 `json:"method_soundness,omitempty"`
	ResultInterpretation float64 `json:"result_interpretation,omitempty"`
	WritingQuality       float64 `json:"writing_quality,omitempty"`
	OverallSuccess       float64 `json:"overall_success,omitempty"`
}

type ReproductionScores struct {
	Completeness    float64   `json:"completeness,omitempty"`
	CorrectnessMean float64   `json:"correctness_mean,omitempty"`
	CorrectnessStd  float64   `json:"correctness_std,omitempty"`
	AdvisorReports  int       `json:"advisor_reports,omitempty"`
	JudgeScores     []float64 `json:"judge_scores,omitempty"`
}

type PaperComparisonScores struct {
	MotivationAlignment   float64 `json:"motivation_alignment,omitempty"`
	MethodologyAlignment  float64 `json:"methodology_alignment,omitempty"`
	NoveltyAlignment      float64 `json:"novelty_alignment,omitempty"`
	ExperimentalAlignment float64 `json:"experimental_alignment,omitempty"`
}

type AggregateScores struct {
	PaperGeneration PaperGenerationScores `json:"paper_generation,omitempty"`
	Reproduction    ReproductionScores    `json:"reproduction,omitempty"`
	PaperComparison PaperComparisonScores `json:"paper_comparison,omitempty"`
}

type Termination struct {
	Signal       TerminationSignal `json:"signal,omitempty"`
	Reason       string            `json:"reason,omitempty"`
	Resolved     bool              `json:"resolved,omitempty"`
	TerminatedAt int64             `json:"terminated_at,omitempty"`
}

type Case struct {
	ID            string          `json:"case_id"`
	Suite         string          `json:"suite,omitempty"`
	Mode          CaseMode        `json:"mode,omitempty"`
	Level         BenchmarkLevel  `json:"level,omitempty"`
	RootSessionID string          `json:"root_session_id,omitempty"`
	Title         string          `json:"title,omitempty"`
	CreatedAt     int64           `json:"created_at,omitempty"`
	UpdatedAt     int64           `json:"updated_at,omitempty"`
	Status        CaseStatus      `json:"status,omitempty"`
	Budget        Budget          `json:"budget,omitempty"`
	Inputs        Inputs          `json:"inputs,omitempty"`
	GraphState    GraphState      `json:"graph_state,omitempty"`
	IdeaModule    IdeaModule      `json:"idea_module,omitempty"`
	Runs          []RunRecord     `json:"runs,omitempty"`
	Artifacts     []Artifact      `json:"artifacts,omitempty"`
	Reviews       []Review        `json:"reviews,omitempty"`
	Scores        AggregateScores `json:"scores,omitempty"`
	Termination   Termination     `json:"termination,omitempty"`
}

type CreateCaseInput struct {
	ID            string         `json:"id,omitempty"`
	Mode          CaseMode       `json:"mode,omitempty"`
	Level         BenchmarkLevel `json:"level,omitempty"`
	RootSessionID string         `json:"root_session_id,omitempty"`
	Title         string         `json:"title,omitempty"`
	Budget        Budget         `json:"budget,omitempty"`
	Inputs        Inputs         `json:"inputs,omitempty"`
}

type Service interface {
	pubsub.Suscriber[Case]
	CreateCase(ctx context.Context, input CreateCaseInput) (Case, error)
	Get(ctx context.Context, caseID string) (Case, error)
	List(ctx context.Context) ([]Case, error)
	Save(ctx context.Context, item Case) (Case, error)
	UpdateGraphState(ctx context.Context, caseID string, graph GraphState) (Case, error)
	SetTermination(ctx context.Context, caseID string, signal TerminationSignal, reason string) (Case, error)
	UpsertRun(ctx context.Context, caseID string, run RunRecord) (Case, error)
	UpsertArtifact(ctx context.Context, caseID string, artifact Artifact) (Case, error)
	UpsertReview(ctx context.Context, caseID string, review Review) (Case, error)
	UpdateScores(ctx context.Context, caseID string, scores AggregateScores) (Case, error)
	Delete(ctx context.Context, caseID string) error
}

type persistedState struct {
	Cases map[string]Case `json:"cases"`
}

type service struct {
	*pubsub.Broker[Case]
	path string
	mu   sync.Mutex
}

func NewService() (Service, error) {
	dataDir := strings.TrimSpace(config.DataDirectory())
	if dataDir == "" {
		return nil, fmt.Errorf("data directory is not configured")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}
	return &service{
		Broker: pubsub.NewBroker[Case](),
		path:   filepath.Join(dataDir, "scientist-bench.json"),
	}, nil
}

func (s *service) CreateCase(ctx context.Context, input CreateCaseInput) (Case, error) {
	item := Case{
		ID:            strings.TrimSpace(input.ID),
		Suite:         "scientist-bench",
		Mode:          input.Mode,
		Level:         input.Level,
		RootSessionID: strings.TrimSpace(input.RootSessionID),
		Title:         strings.TrimSpace(input.Title),
		Budget:        input.Budget,
		Inputs:        input.Inputs,
		Status:        StatusPlanning,
	}
	if item.ID == "" {
		item.ID = "case-" + uuid.NewString()
	}
	if item.Mode == "" {
		item.Mode = ModeJoint
	}
	if item.Level == "" {
		item.Level = Level1
	}
	return s.Save(ctx, item)
}

func (s *service) Get(_ context.Context, caseID string) (Case, error) {
	caseID = strings.TrimSpace(caseID)
	if caseID == "" {
		return Case{}, fmt.Errorf("case ID is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.loadLocked()
	if err != nil {
		return Case{}, err
	}
	item, ok := state.Cases[caseID]
	if !ok {
		return Case{}, fmt.Errorf("case %s not found", caseID)
	}
	return normalizeCase(item), nil
}

func (s *service) List(_ context.Context) ([]Case, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.loadLocked()
	if err != nil {
		return nil, err
	}

	out := make([]Case, 0, len(state.Cases))
	for _, item := range state.Cases {
		out = append(out, normalizeCase(item))
	}
	slices.SortFunc(out, func(a, b Case) int {
		if a.UpdatedAt == b.UpdatedAt {
			return strings.Compare(a.ID, b.ID)
		}
		if a.UpdatedAt > b.UpdatedAt {
			return -1
		}
		return 1
	})
	return out, nil
}

func (s *service) Save(_ context.Context, item Case) (Case, error) {
	item = normalizeCase(item)
	if item.ID == "" {
		return Case{}, fmt.Errorf("case ID is required")
	}
	now := time.Now().Unix()
	if item.CreatedAt == 0 {
		item.CreatedAt = now
	}
	item.UpdatedAt = now

	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.loadLocked()
	if err != nil {
		return Case{}, err
	}
	state.Cases[item.ID] = item
	if err := s.saveLocked(state); err != nil {
		return Case{}, err
	}
	s.Publish(pubsub.UpdatedEvent, item)
	return item, nil
}

func (s *service) UpdateGraphState(ctx context.Context, caseID string, graph GraphState) (Case, error) {
	item, err := s.Get(ctx, caseID)
	if err != nil {
		return Case{}, err
	}
	item.GraphState = graph
	if item.Status == "" || item.Status == StatusPlanning {
		item.Status = StatusRunning
	}
	return s.Save(ctx, item)
}

func (s *service) SetTermination(ctx context.Context, caseID string, signal TerminationSignal, reason string) (Case, error) {
	item, err := s.Get(ctx, caseID)
	if err != nil {
		return Case{}, err
	}
	item.Termination = Termination{
		Signal:       signal,
		Reason:       strings.TrimSpace(reason),
		Resolved:     signal == SignalCaseResolved,
		TerminatedAt: time.Now().Unix(),
	}
	switch signal {
	case SignalCaseResolved:
		item.Status = StatusResolved
	case SignalCaseNotResolved, SignalBudgetExhausted:
		item.Status = StatusNotResolved
	default:
		item.Status = StatusBlocked
	}
	return s.Save(ctx, item)
}

func (s *service) UpsertRun(ctx context.Context, caseID string, run RunRecord) (Case, error) {
	item, err := s.Get(ctx, caseID)
	if err != nil {
		return Case{}, err
	}
	run = normalizeRun(run)
	item.Runs = upsertRun(item.Runs, run)
	return s.Save(ctx, item)
}

func (s *service) UpsertArtifact(ctx context.Context, caseID string, artifact Artifact) (Case, error) {
	item, err := s.Get(ctx, caseID)
	if err != nil {
		return Case{}, err
	}
	artifact = normalizeArtifact(artifact)
	item.Artifacts = upsertArtifact(item.Artifacts, artifact)
	return s.Save(ctx, item)
}

func (s *service) UpsertReview(ctx context.Context, caseID string, review Review) (Case, error) {
	item, err := s.Get(ctx, caseID)
	if err != nil {
		return Case{}, err
	}
	review = normalizeReview(review)
	item.Reviews = upsertReview(item.Reviews, review)
	return s.Save(ctx, item)
}

func (s *service) UpdateScores(ctx context.Context, caseID string, scores AggregateScores) (Case, error) {
	item, err := s.Get(ctx, caseID)
	if err != nil {
		return Case{}, err
	}
	item.Scores = scores
	return s.Save(ctx, item)
}

func (s *service) Delete(_ context.Context, caseID string) error {
	caseID = strings.TrimSpace(caseID)
	if caseID == "" {
		return fmt.Errorf("case ID is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.loadLocked()
	if err != nil {
		return err
	}
	item, ok := state.Cases[caseID]
	if !ok {
		return fmt.Errorf("case %s not found", caseID)
	}
	delete(state.Cases, caseID)
	if err := s.saveLocked(state); err != nil {
		return err
	}
	s.Publish(pubsub.DeletedEvent, item)
	return nil
}

func (s *service) loadLocked() (*persistedState, error) {
	content, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return &persistedState{Cases: map[string]Case{}}, nil
		}
		return nil, fmt.Errorf("failed to read scientist bench state: %w", err)
	}
	var state persistedState
	if err := json.Unmarshal(content, &state); err != nil {
		return nil, fmt.Errorf("failed to decode scientist bench state: %w", err)
	}
	if state.Cases == nil {
		state.Cases = map[string]Case{}
	}
	for id, item := range state.Cases {
		state.Cases[id] = normalizeCase(item)
	}
	return &state, nil
}

func (s *service) saveLocked(state *persistedState) error {
	if state == nil {
		state = &persistedState{Cases: map[string]Case{}}
	}
	if state.Cases == nil {
		state.Cases = map[string]Case{}
	}
	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode scientist bench state: %w", err)
	}
	if err := os.WriteFile(s.path, payload, 0o600); err != nil {
		return fmt.Errorf("failed to write scientist bench state: %w", err)
	}
	return nil
}

func normalizeCase(item Case) Case {
	item.ID = strings.TrimSpace(item.ID)
	item.Suite = strings.TrimSpace(item.Suite)
	item.RootSessionID = strings.TrimSpace(item.RootSessionID)
	if item.Suite == "" {
		item.Suite = "scientist-bench"
	}
	if item.Mode == "" {
		item.Mode = ModeJoint
	}
	if item.Level == "" {
		item.Level = Level1
	}
	item.Title = strings.TrimSpace(item.Title)
	if item.Status == "" {
		item.Status = StatusPlanning
	}
	item.Inputs = normalizeInputs(item.Inputs)
	item.GraphState = normalizeGraphState(item.GraphState)
	item.IdeaModule = normalizeIdeaModule(item.IdeaModule)
	for i, run := range item.Runs {
		item.Runs[i] = normalizeRun(run)
	}
	for i, artifact := range item.Artifacts {
		item.Artifacts[i] = normalizeArtifact(artifact)
	}
	for i, review := range item.Reviews {
		item.Reviews[i] = normalizeReview(review)
	}
	item.Termination = normalizeTermination(item.Termination)
	slices.SortFunc(item.Runs, func(a, b RunRecord) int {
		if a.StartedAt == b.StartedAt {
			return strings.Compare(a.ID, b.ID)
		}
		if a.StartedAt > b.StartedAt {
			return -1
		}
		return 1
	})
	slices.SortFunc(item.Artifacts, func(a, b Artifact) int {
		if a.CreatedAt == b.CreatedAt {
			return strings.Compare(a.ID, b.ID)
		}
		if a.CreatedAt > b.CreatedAt {
			return -1
		}
		return 1
	})
	slices.SortFunc(item.Reviews, func(a, b Review) int {
		if a.CreatedAt == b.CreatedAt {
			return strings.Compare(a.ID, b.ID)
		}
		if a.CreatedAt > b.CreatedAt {
			return -1
		}
		return 1
	})
	return item
}

func normalizeInputs(inputs Inputs) Inputs {
	inputs.CoreIdea = strings.TrimSpace(inputs.CoreIdea)
	for i, ref := range inputs.ReferenceSet {
		inputs.ReferenceSet[i] = normalizeReference(ref)
	}
	inputs.Dataset = normalizeDataset(inputs.Dataset)
	inputs.TargetPaper = normalizeTargetPaper(inputs.TargetPaper)
	inputs.MaskedMethodSpec = normalizeMaskedMethodSpec(inputs.MaskedMethodSpec)
	for i, constraint := range inputs.Constraints {
		inputs.Constraints[i] = strings.TrimSpace(constraint)
	}
	return inputs
}

func normalizeReference(ref Reference) Reference {
	ref.ID = strings.TrimSpace(ref.ID)
	ref.Title = strings.TrimSpace(ref.Title)
	ref.Venue = strings.TrimSpace(ref.Venue)
	ref.URL = strings.TrimSpace(ref.URL)
	ref.PDFURL = strings.TrimSpace(ref.PDFURL)
	ref.Abstract = strings.TrimSpace(ref.Abstract)
	ref.LocalArtifactID = strings.TrimSpace(ref.LocalArtifactID)
	ref.Priority = strings.TrimSpace(ref.Priority)
	for i, author := range ref.Authors {
		ref.Authors[i] = strings.TrimSpace(author)
	}
	for i, tag := range ref.Tags {
		ref.Tags[i] = strings.TrimSpace(tag)
	}
	return ref
}

func normalizeDataset(dataset Dataset) Dataset {
	dataset.Name = strings.TrimSpace(dataset.Name)
	dataset.SourceType = strings.TrimSpace(dataset.SourceType)
	dataset.URI = strings.TrimSpace(dataset.URI)
	dataset.License = strings.TrimSpace(dataset.License)
	dataset.TaskType = strings.TrimSpace(dataset.TaskType)
	dataset.EvaluationProtocol = strings.TrimSpace(dataset.EvaluationProtocol)
	dataset.Notes = strings.TrimSpace(dataset.Notes)
	for i, split := range dataset.Splits {
		dataset.Splits[i] = strings.TrimSpace(split)
	}
	return dataset
}

func normalizeTargetPaper(paper TargetPaper) TargetPaper {
	paper.PaperID = strings.TrimSpace(paper.PaperID)
	paper.Title = strings.TrimSpace(paper.Title)
	paper.Venue = strings.TrimSpace(paper.Venue)
	paper.URL = strings.TrimSpace(paper.URL)
	paper.PDFURL = strings.TrimSpace(paper.PDFURL)
	paper.Abstract = strings.TrimSpace(paper.Abstract)
	paper.EvaluationNotes = strings.TrimSpace(paper.EvaluationNotes)
	for i, author := range paper.Authors {
		paper.Authors[i] = strings.TrimSpace(author)
	}
	for i, artifactID := range paper.GroundTruthArtifacts {
		paper.GroundTruthArtifacts[i] = strings.TrimSpace(artifactID)
	}
	return paper
}

func normalizeMaskedMethodSpec(spec MaskedMethodSpec) MaskedMethodSpec {
	for i, item := range spec.PipelineSummary {
		spec.PipelineSummary[i] = strings.TrimSpace(item)
	}
	for i, item := range spec.KnownComponents {
		spec.KnownComponents[i] = strings.TrimSpace(item)
	}
	for i, item := range spec.HiddenComponents {
		spec.HiddenComponents[i] = strings.TrimSpace(item)
	}
	for i, item := range spec.AllowedClues {
		spec.AllowedClues[i] = strings.TrimSpace(item)
	}
	for i, item := range spec.ForbiddenLeakage {
		spec.ForbiddenLeakage[i] = strings.TrimSpace(item)
	}
	return spec
}

func normalizeGraphState(graph GraphState) GraphState {
	graph.CurrentStage = strings.TrimSpace(graph.CurrentStage)
	graph.ActiveNode = strings.TrimSpace(graph.ActiveNode)
	graph.ActiveRole = strings.TrimSpace(graph.ActiveRole)
	graph.PendingNodes = normalizeStrings(graph.PendingNodes)
	graph.CompletedNodes = normalizeStrings(graph.CompletedNodes)
	graph.BlockedNodes = normalizeStrings(graph.BlockedNodes)
	return graph
}

func normalizeIdeaModule(module IdeaModule) IdeaModule {
	module.Status = strings.TrimSpace(module.Status)
	for i, idea := range module.AcceptedIdeas {
		module.AcceptedIdeas[i] = normalizeIdea(idea)
	}
	for i, idea := range module.RejectedIdeas {
		module.RejectedIdeas[i] = normalizeIdea(idea)
	}
	for i, objection := range module.Objections {
		module.Objections[i] = normalizeObjection(objection)
	}
	return module
}

func normalizeIdea(idea IdeaCandidate) IdeaCandidate {
	idea.ID = strings.TrimSpace(idea.ID)
	idea.Title = strings.TrimSpace(idea.Title)
	idea.Summary = strings.TrimSpace(idea.Summary)
	idea.NoveltyClaim = strings.TrimSpace(idea.NoveltyClaim)
	idea.Hypotheses = normalizeStrings(idea.Hypotheses)
	idea.SupportingRefs = normalizeStrings(idea.SupportingRefs)
	if idea.CreatedAt == 0 {
		idea.CreatedAt = time.Now().Unix()
	}
	return idea
}

func normalizeObjection(objection IdeaObjection) IdeaObjection {
	objection.ID = strings.TrimSpace(objection.ID)
	objection.IdeaID = strings.TrimSpace(objection.IdeaID)
	objection.Summary = strings.TrimSpace(objection.Summary)
	objection.Severity = strings.TrimSpace(objection.Severity)
	objection.EvidenceRefs = normalizeStrings(objection.EvidenceRefs)
	if objection.CreatedAt == 0 {
		objection.CreatedAt = time.Now().Unix()
	}
	return objection
}

func normalizeRun(run RunRecord) RunRecord {
	run.ID = strings.TrimSpace(run.ID)
	if run.ID == "" {
		run.ID = "run-" + uuid.NewString()
	}
	run.ParentRunID = strings.TrimSpace(run.ParentRunID)
	run.SessionID = strings.TrimSpace(run.SessionID)
	run.Role = strings.TrimSpace(run.Role)
	run.RoleInstance = strings.TrimSpace(run.RoleInstance)
	run.NodeID = strings.TrimSpace(run.NodeID)
	run.Status = strings.TrimSpace(run.Status)
	run.InputSummary = strings.TrimSpace(run.InputSummary)
	run.OutputSummary = strings.TrimSpace(run.OutputSummary)
	run.TaskRunSessionID = strings.TrimSpace(run.TaskRunSessionID)
	run.TaskRunStatus = strings.TrimSpace(run.TaskRunStatus)
	run.ToolCalls = normalizeStrings(run.ToolCalls)
	run.SignalsEmitted = normalizeStrings(run.SignalsEmitted)
	run.Error = strings.TrimSpace(run.Error)
	if run.StartedAt == 0 {
		run.StartedAt = time.Now().Unix()
	}
	return run
}

func normalizeArtifact(artifact Artifact) Artifact {
	artifact.ID = strings.TrimSpace(artifact.ID)
	if artifact.ID == "" {
		artifact.ID = "artifact-" + uuid.NewString()
	}
	artifact.Label = strings.TrimSpace(artifact.Label)
	artifact.URI = strings.TrimSpace(artifact.URI)
	artifact.Path = strings.TrimSpace(artifact.Path)
	artifact.ProducerRole = strings.TrimSpace(artifact.ProducerRole)
	artifact.ProducerRunID = strings.TrimSpace(artifact.ProducerRunID)
	artifact.QualityFlags = normalizeStrings(artifact.QualityFlags)
	if artifact.Metadata == nil {
		artifact.Metadata = map[string]string{}
	}
	if artifact.CreatedAt == 0 {
		artifact.CreatedAt = time.Now().Unix()
	}
	return artifact
}

func normalizeReview(review Review) Review {
	review.ID = strings.TrimSpace(review.ID)
	if review.ID == "" {
		review.ID = "review-" + uuid.NewString()
	}
	review.ReviewerRole = strings.TrimSpace(review.ReviewerRole)
	review.TargetArtifactID = strings.TrimSpace(review.TargetArtifactID)
	review.TargetPaperID = strings.TrimSpace(review.TargetPaperID)
	review.Decision = strings.TrimSpace(review.Decision)
	review.Summary = strings.TrimSpace(review.Summary)
	review.Strengths = normalizeStrings(review.Strengths)
	review.Weaknesses = normalizeStrings(review.Weaknesses)
	review.Questions = normalizeStrings(review.Questions)
	review.EvidenceArtifactIDs = normalizeStrings(review.EvidenceArtifactIDs)
	if review.CreatedAt == 0 {
		review.CreatedAt = time.Now().Unix()
	}
	return review
}

func normalizeTermination(term Termination) Termination {
	term.Reason = strings.TrimSpace(term.Reason)
	if term.Signal == SignalCaseResolved {
		term.Resolved = true
	}
	if term.Signal != "" && term.TerminatedAt == 0 {
		term.TerminatedAt = time.Now().Unix()
	}
	return term
}

func normalizeStrings(items []string) []string {
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
	return out
}

func upsertRun(existing []RunRecord, incoming RunRecord) []RunRecord {
	for i, item := range existing {
		if item.ID == incoming.ID {
			existing[i] = incoming
			return existing
		}
	}
	return append(existing, incoming)
}

func upsertArtifact(existing []Artifact, incoming Artifact) []Artifact {
	for i, item := range existing {
		if item.ID == incoming.ID {
			existing[i] = incoming
			return existing
		}
	}
	return append(existing, incoming)
}

func upsertReview(existing []Review, incoming Review) []Review {
	for i, item := range existing {
		if item.ID == incoming.ID {
			existing[i] = incoming
			return existing
		}
	}
	return append(existing, incoming)
}
