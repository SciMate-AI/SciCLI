package research

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
	"github.com/SciMate-AI/scicli/internal/taskrun"
	"github.com/google/uuid"
)

type Stage string

const (
	StageObjective  Stage = "objective"
	StageHypothesis Stage = "hypothesis"
	StageExperiment Stage = "experiment"
	StageEvaluation Stage = "evaluation"
	StageDecision   Stage = "decision"
)

type Hypothesis struct {
	ID      string `json:"id,omitempty"`
	Summary string `json:"summary,omitempty"`
	Status  string `json:"status,omitempty"`
}

type ExperimentStatus string

const (
	ExperimentPlanned   ExperimentStatus = "planned"
	ExperimentRunning   ExperimentStatus = "running"
	ExperimentCompleted ExperimentStatus = "completed"
	ExperimentEvaluated ExperimentStatus = "evaluated"
	ExperimentFailed    ExperimentStatus = "failed"
	ExperimentCanceled  ExperimentStatus = "canceled"
)

type ExperimentRun struct {
	ID              string         `json:"id,omitempty"`
	SessionID       string         `json:"session_id,omitempty"`
	Title           string         `json:"title,omitempty"`
	Status          taskrun.Status `json:"status,omitempty"`
	Detail          string         `json:"detail,omitempty"`
	Artifacts       []ArtifactRef  `json:"artifacts,omitempty"`
	CreatedAt       int64          `json:"created_at,omitempty"`
	UpdatedAt       int64          `json:"updated_at,omitempty"`
	ParentSessionID string         `json:"parent_session_id,omitempty"`
}

type ArtifactKind string

const (
	ArtifactPrompt       ArtifactKind = "prompt"
	ArtifactRunLog       ArtifactKind = "run_log"
	ArtifactCodeSnapshot ArtifactKind = "code_snapshot"
	ArtifactReport       ArtifactKind = "report"
	ArtifactReference    ArtifactKind = "reference"
)

type ArtifactRef struct {
	ID              string            `json:"id,omitempty"`
	Kind            ArtifactKind      `json:"kind,omitempty"`
	Label           string            `json:"label,omitempty"`
	Path            string            `json:"path,omitempty"`
	URI             string            `json:"uri,omitempty"`
	Summary         string            `json:"summary,omitempty"`
	SourceSessionID string            `json:"source_session_id,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	CreatedAt       int64             `json:"created_at,omitempty"`
}

type ArtifactRecord struct {
	Artifact           ArtifactRef       `json:"artifact"`
	ExperimentID       string            `json:"experiment_id,omitempty"`
	ExperimentTitle    string            `json:"experiment_title,omitempty"`
	ExperimentStatus   ExperimentStatus  `json:"experiment_status,omitempty"`
	ParentExperimentID string            `json:"parent_experiment_id,omitempty"`
	RunID              string            `json:"run_id,omitempty"`
	RunTitle           string            `json:"run_title,omitempty"`
	RunStatus          taskrun.Status    `json:"run_status,omitempty"`
	Evaluation         *EvaluationResult `json:"evaluation,omitempty"`
}

type LineageGroup struct {
	RootID    string           `json:"root_id,omitempty"`
	RootTitle string           `json:"root_title,omitempty"`
	BestScore float64          `json:"best_score,omitempty"`
	Plans     []ExperimentPlan `json:"plans,omitempty"`
}

type SelectionDecision string

const (
	DecisionKeep    SelectionDecision = "keep"
	DecisionDiscard SelectionDecision = "discard"
	DecisionMutate  SelectionDecision = "mutate"
	DecisionBranch  SelectionDecision = "branch"
)

type EvaluationResult struct {
	Score     float64           `json:"score,omitempty"`
	Decision  SelectionDecision `json:"decision,omitempty"`
	Summary   string            `json:"summary,omitempty"`
	CreatedAt int64             `json:"created_at,omitempty"`
}

type ExperimentPlan struct {
	ID                 string            `json:"id,omitempty"`
	Title              string            `json:"title,omitempty"`
	HypothesisID       string            `json:"hypothesis_id,omitempty"`
	ParentExperimentID string            `json:"parent_experiment_id,omitempty"`
	LineageRootID      string            `json:"lineage_root_id,omitempty"`
	Generation         int               `json:"generation,omitempty"`
	EvolutionDecision  SelectionDecision `json:"evolution_decision,omitempty"`
	Prompt             string            `json:"prompt,omitempty"`
	Rationale          string            `json:"rationale,omitempty"`
	Status             ExperimentStatus  `json:"status,omitempty"`
	Runs               []ExperimentRun   `json:"runs,omitempty"`
	LatestEval         *EvaluationResult `json:"latest_evaluation,omitempty"`
	CreatedAt          int64             `json:"created_at,omitempty"`
	UpdatedAt          int64             `json:"updated_at,omitempty"`
}

type SessionState struct {
	SessionID            string           `json:"session_id"`
	Objective            string           `json:"objective,omitempty"`
	Domain               string           `json:"domain,omitempty"`
	Stage                Stage            `json:"stage,omitempty"`
	SuccessCriteria      []string         `json:"success_criteria,omitempty"`
	Hypotheses           []Hypothesis     `json:"hypotheses,omitempty"`
	ActiveExperimentID   string           `json:"active_experiment_id,omitempty"`
	PromotedExperimentID string           `json:"promoted_experiment_id,omitempty"`
	Experiments          []ExperimentPlan `json:"experiments,omitempty"`
	UpdatedAt            int64            `json:"updated_at,omitempty"`
}

func (s SessionState) HasContent() bool {
	return strings.TrimSpace(s.Objective) != "" ||
		strings.TrimSpace(s.Domain) != "" ||
		len(s.SuccessCriteria) > 0 ||
		len(s.Hypotheses) > 0 ||
		len(s.Experiments) > 0
}

type Service interface {
	pubsub.Suscriber[SessionState]
	Get(ctx context.Context, sessionID string) (SessionState, error)
	Save(ctx context.Context, state SessionState) (SessionState, error)
	SetObjective(ctx context.Context, sessionID string, objective string) (SessionState, error)
	AddExperiment(ctx context.Context, sessionID string, title string, prompt string) (SessionState, ExperimentPlan, error)
	AddExperimentCandidate(ctx context.Context, sessionID string, plan ExperimentPlan) (SessionState, ExperimentPlan, error)
	CloneExperiment(ctx context.Context, sessionID string, experimentID string) (SessionState, ExperimentPlan, error)
	EvolveExperiment(ctx context.Context, sessionID string, experimentID string, title string) (SessionState, ExperimentPlan, error)
	SetActiveExperiment(ctx context.Context, sessionID string, experimentID string) (SessionState, error)
	PromoteExperiment(ctx context.Context, sessionID string, experimentID string) (SessionState, ExperimentPlan, error)
	AttachTaskRun(ctx context.Context, sessionID string, experimentID string, run taskrun.Run) (SessionState, ExperimentPlan, error)
	EvaluateExperiment(ctx context.Context, sessionID string, experimentID string, score float64, decision string, summary string) (SessionState, ExperimentPlan, error)
	SyncTaskRun(ctx context.Context, run taskrun.Run) error
	SyncTaskArtifacts(ctx context.Context, runSessionID string, artifacts []ArtifactRef) error
	Delete(ctx context.Context, sessionID string) error
}

type persistedState struct {
	Sessions map[string]SessionState `json:"sessions"`
}

type service struct {
	*pubsub.Broker[SessionState]
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
		Broker: pubsub.NewBroker[SessionState](),
		path:   filepath.Join(dataDir, "research.json"),
	}, nil
}

func (s *service) Get(_ context.Context, sessionID string) (SessionState, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return SessionState{}, fmt.Errorf("session ID is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.loadLocked()
	if err != nil {
		return SessionState{}, err
	}
	state, ok := data.Sessions[sessionID]
	if !ok {
		return SessionState{
			SessionID: sessionID,
			Stage:     StageObjective,
		}, nil
	}
	return normalizeState(state), nil
}

func (s *service) Save(_ context.Context, state SessionState) (SessionState, error) {
	state = normalizeState(state)
	if strings.TrimSpace(state.SessionID) == "" {
		return SessionState{}, fmt.Errorf("session ID is required")
	}
	state.UpdatedAt = time.Now().Unix()

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.loadLocked()
	if err != nil {
		return SessionState{}, err
	}
	if data.Sessions == nil {
		data.Sessions = make(map[string]SessionState)
	}
	data.Sessions[state.SessionID] = state
	if err := s.saveLocked(data); err != nil {
		return SessionState{}, err
	}
	s.Publish(pubsub.UpdatedEvent, state)
	return state, nil
}

func (s *service) SetObjective(ctx context.Context, sessionID string, objective string) (SessionState, error) {
	objective = strings.TrimSpace(objective)
	if objective == "" {
		return SessionState{}, fmt.Errorf("research objective cannot be empty")
	}
	state, err := s.Get(ctx, sessionID)
	if err != nil {
		return SessionState{}, err
	}
	state.Objective = objective
	if state.Stage == "" {
		state.Stage = StageObjective
	}
	return s.Save(ctx, state)
}

func (s *service) AddExperiment(ctx context.Context, sessionID string, title string, prompt string) (SessionState, ExperimentPlan, error) {
	return s.AddExperimentCandidate(ctx, sessionID, ExperimentPlan{
		Title:  title,
		Prompt: prompt,
	})
}

func (s *service) AddExperimentCandidate(ctx context.Context, sessionID string, plan ExperimentPlan) (SessionState, ExperimentPlan, error) {
	plan = normalizeExperiment(plan)
	if plan.Title == "" {
		return SessionState{}, ExperimentPlan{}, fmt.Errorf("experiment title cannot be empty")
	}

	state, err := s.Get(ctx, sessionID)
	if err != nil {
		return SessionState{}, ExperimentPlan{}, err
	}

	now := time.Now().Unix()
	if plan.ID == "" {
		plan.ID = "exp-" + uuid.NewString()
	}
	if parent, ok := findExperiment(state, plan.ParentExperimentID); ok {
		rootID := strings.TrimSpace(parent.LineageRootID)
		if rootID == "" {
			rootID = parent.ID
		}
		plan.LineageRootID = rootID
		plan.Generation = parent.Generation + 1
	}
	if strings.TrimSpace(plan.LineageRootID) == "" {
		plan.LineageRootID = plan.ID
	}
	if plan.ParentExperimentID != "" && plan.EvolutionDecision == "" {
		if parent, ok := findExperiment(state, plan.ParentExperimentID); ok && parent.LatestEval != nil {
			switch parent.LatestEval.Decision {
			case DecisionMutate, DecisionBranch:
				plan.EvolutionDecision = parent.LatestEval.Decision
			}
		}
	}
	plan.Status = ExperimentPlanned
	plan.CreatedAt = now
	plan.UpdatedAt = now
	plan.Runs = nil
	plan.LatestEval = nil

	state.Experiments = append(state.Experiments, plan)
	state.ActiveExperimentID = plan.ID
	if state.Stage == "" || state.Stage == StageObjective || state.Stage == StageHypothesis {
		state.Stage = StageExperiment
	}

	state, err = s.Save(ctx, state)
	if err != nil {
		return SessionState{}, ExperimentPlan{}, err
	}
	plan, _ = findExperiment(state, plan.ID)
	return state, plan, nil
}

func (s *service) CloneExperiment(ctx context.Context, sessionID string, experimentID string) (SessionState, ExperimentPlan, error) {
	state, err := s.Get(ctx, sessionID)
	if err != nil {
		return SessionState{}, ExperimentPlan{}, err
	}
	source, ok := findExperiment(state, experimentID)
	if !ok {
		return SessionState{}, ExperimentPlan{}, fmt.Errorf("experiment %s not found", experimentID)
	}

	title := source.Title
	if !strings.HasPrefix(strings.ToLower(title), "rerun:") {
		title = "Rerun: " + title
	}
	evolutionDecision := SelectionDecision("")
	if source.LatestEval != nil {
		evolutionDecision = source.LatestEval.Decision
	}
	return s.AddExperimentCandidate(ctx, sessionID, ExperimentPlan{
		Title:              title,
		Prompt:             source.Prompt,
		Rationale:          source.Rationale,
		ParentExperimentID: source.ID,
		EvolutionDecision:  evolutionDecision,
	})
}

func (s *service) EvolveExperiment(ctx context.Context, sessionID string, experimentID string, title string) (SessionState, ExperimentPlan, error) {
	state, err := s.Get(ctx, sessionID)
	if err != nil {
		return SessionState{}, ExperimentPlan{}, err
	}
	source, ok := findExperiment(state, experimentID)
	if !ok {
		return SessionState{}, ExperimentPlan{}, fmt.Errorf("experiment %s not found", experimentID)
	}
	if source.LatestEval == nil {
		return SessionState{}, ExperimentPlan{}, fmt.Errorf("experiment %s has not been evaluated yet", experimentID)
	}
	switch source.LatestEval.Decision {
	case DecisionMutate, DecisionBranch:
	default:
		return SessionState{}, ExperimentPlan{}, fmt.Errorf("experiment %s decision %q does not create a new generation", experimentID, source.LatestEval.Decision)
	}

	title = strings.TrimSpace(title)
	if title == "" {
		prefix := "Mutate"
		if source.LatestEval.Decision == DecisionBranch {
			prefix = "Branch"
		}
		title = prefix + ": " + source.Title
	}

	return s.AddExperimentCandidate(ctx, sessionID, ExperimentPlan{
		Title:              title,
		Prompt:             source.Prompt,
		Rationale:          source.Rationale,
		ParentExperimentID: source.ID,
	})
}

func (s *service) SetActiveExperiment(ctx context.Context, sessionID string, experimentID string) (SessionState, error) {
	experimentID = strings.TrimSpace(experimentID)
	if experimentID == "" {
		return SessionState{}, fmt.Errorf("experiment ID is required")
	}

	state, err := s.Get(ctx, sessionID)
	if err != nil {
		return SessionState{}, err
	}
	if _, ok := findExperiment(state, experimentID); !ok {
		return SessionState{}, fmt.Errorf("experiment %s not found", experimentID)
	}
	state.ActiveExperimentID = experimentID
	return s.Save(ctx, state)
}

func (s *service) PromoteExperiment(ctx context.Context, sessionID string, experimentID string) (SessionState, ExperimentPlan, error) {
	experimentID = strings.TrimSpace(experimentID)
	if experimentID == "" {
		return SessionState{}, ExperimentPlan{}, fmt.Errorf("experiment ID is required")
	}

	state, err := s.Get(ctx, sessionID)
	if err != nil {
		return SessionState{}, ExperimentPlan{}, err
	}
	plan, ok := findExperiment(state, experimentID)
	if !ok {
		return SessionState{}, ExperimentPlan{}, fmt.Errorf("experiment %s not found", experimentID)
	}
	if plan.LatestEval == nil {
		return SessionState{}, ExperimentPlan{}, fmt.Errorf("experiment %s has not been evaluated yet", experimentID)
	}
	if plan.LatestEval.Decision == DecisionDiscard {
		return SessionState{}, ExperimentPlan{}, fmt.Errorf("discarded experiment %s cannot be promoted", experimentID)
	}

	state.PromotedExperimentID = experimentID
	state.ActiveExperimentID = experimentID
	state.Stage = StageDecision
	state, err = s.Save(ctx, state)
	if err != nil {
		return SessionState{}, ExperimentPlan{}, err
	}
	plan, _ = findExperiment(state, experimentID)
	return state, plan, nil
}

func (s *service) AttachTaskRun(ctx context.Context, sessionID string, experimentID string, run taskrun.Run) (SessionState, ExperimentPlan, error) {
	experimentID = strings.TrimSpace(experimentID)
	if experimentID == "" {
		return SessionState{}, ExperimentPlan{}, fmt.Errorf("experiment ID is required")
	}
	if strings.TrimSpace(run.SessionID) == "" {
		return SessionState{}, ExperimentPlan{}, fmt.Errorf("task run session ID is required")
	}

	state, err := s.Get(ctx, sessionID)
	if err != nil {
		return SessionState{}, ExperimentPlan{}, err
	}

	planIndex := findExperimentIndex(state, experimentID)
	if planIndex < 0 {
		return SessionState{}, ExperimentPlan{}, fmt.Errorf("experiment %s not found", experimentID)
	}

	now := run.UpdatedAt
	if now == 0 {
		now = time.Now().Unix()
	}
	plan := state.Experiments[planIndex]
	runIndex := findExperimentRunIndex(plan, run.SessionID)
	if runIndex < 0 {
		plan.Runs = append(plan.Runs, ExperimentRun{
			ID:              run.SessionID,
			SessionID:       run.SessionID,
			ParentSessionID: run.ParentSessionID,
			Title:           strings.TrimSpace(run.Title),
			Status:          run.Status,
			Detail:          strings.TrimSpace(run.Detail),
			CreatedAt:       now,
			UpdatedAt:       now,
		})
	} else {
		planRun := plan.Runs[runIndex]
		planRun.ParentSessionID = run.ParentSessionID
		if strings.TrimSpace(run.Title) != "" {
			planRun.Title = strings.TrimSpace(run.Title)
		}
		if run.Status != "" {
			planRun.Status = run.Status
		}
		planRun.Detail = strings.TrimSpace(run.Detail)
		if planRun.CreatedAt == 0 {
			planRun.CreatedAt = now
		}
		planRun.UpdatedAt = now
		plan.Runs[runIndex] = planRun
	}

	plan.Status = experimentStatusFromTaskStatus(run.Status, plan.LatestEval != nil)
	plan.UpdatedAt = now
	state.Experiments[planIndex] = plan
	state.Stage = stageFromExperiment(plan)
	if strings.TrimSpace(state.ActiveExperimentID) == "" {
		state.ActiveExperimentID = experimentID
	}

	state, err = s.Save(ctx, state)
	if err != nil {
		return SessionState{}, ExperimentPlan{}, err
	}
	plan, _ = findExperiment(state, experimentID)
	return state, plan, nil
}

func (s *service) EvaluateExperiment(ctx context.Context, sessionID string, experimentID string, score float64, decision string, summary string) (SessionState, ExperimentPlan, error) {
	experimentID = strings.TrimSpace(experimentID)
	summary = strings.TrimSpace(summary)
	if experimentID == "" {
		return SessionState{}, ExperimentPlan{}, fmt.Errorf("experiment ID is required")
	}
	if summary == "" {
		return SessionState{}, ExperimentPlan{}, fmt.Errorf("evaluation summary cannot be empty")
	}
	selection, err := NormalizeSelectionDecision(decision)
	if err != nil {
		return SessionState{}, ExperimentPlan{}, err
	}

	state, err := s.Get(ctx, sessionID)
	if err != nil {
		return SessionState{}, ExperimentPlan{}, err
	}
	planIndex := findExperimentIndex(state, experimentID)
	if planIndex < 0 {
		return SessionState{}, ExperimentPlan{}, fmt.Errorf("experiment %s not found", experimentID)
	}

	now := time.Now().Unix()
	plan := state.Experiments[planIndex]
	plan.LatestEval = &EvaluationResult{
		Score:     score,
		Decision:  selection,
		Summary:   summary,
		CreatedAt: now,
	}
	plan.Status = ExperimentEvaluated
	plan.UpdatedAt = now
	state.Experiments[planIndex] = plan
	if selection == DecisionKeep || selection == DecisionDiscard {
		state.Stage = StageDecision
	} else {
		state.Stage = StageEvaluation
	}

	state, err = s.Save(ctx, state)
	if err != nil {
		return SessionState{}, ExperimentPlan{}, err
	}
	plan, _ = findExperiment(state, experimentID)
	return state, plan, nil
}

func (s *service) SyncTaskRun(_ context.Context, run taskrun.Run) error {
	if strings.TrimSpace(run.SessionID) == "" {
		return nil
	}

	s.mu.Lock()
	data, err := s.loadLocked()
	if err != nil {
		s.mu.Unlock()
		return err
	}

	var updated SessionState
	found := false
	for sessionID, state := range data.Sessions {
		for planIndex, plan := range state.Experiments {
			runIndex := findExperimentRunIndex(plan, run.SessionID)
			if runIndex < 0 {
				continue
			}
			now := run.UpdatedAt
			if now == 0 {
				now = time.Now().Unix()
			}
			planRun := plan.Runs[runIndex]
			planRun.ParentSessionID = run.ParentSessionID
			if strings.TrimSpace(run.Title) != "" {
				planRun.Title = strings.TrimSpace(run.Title)
			}
			if run.Status != "" {
				planRun.Status = run.Status
			}
			planRun.Detail = strings.TrimSpace(run.Detail)
			if planRun.CreatedAt == 0 {
				planRun.CreatedAt = now
			}
			planRun.UpdatedAt = now
			plan.Runs[runIndex] = planRun
			plan.Status = experimentStatusFromTaskStatus(run.Status, plan.LatestEval != nil)
			plan.UpdatedAt = now
			state.Experiments[planIndex] = plan
			state.Stage = stageFromExperiment(plan)
			state.UpdatedAt = now
			state = normalizeState(state)
			data.Sessions[sessionID] = state
			updated = state
			found = true
			break
		}
		if found {
			break
		}
	}

	if !found {
		s.mu.Unlock()
		return nil
	}
	if err := s.saveLocked(data); err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	s.Publish(pubsub.UpdatedEvent, updated)
	return nil
}

func (s *service) SyncTaskArtifacts(_ context.Context, runSessionID string, artifacts []ArtifactRef) error {
	runSessionID = strings.TrimSpace(runSessionID)
	if runSessionID == "" || len(artifacts) == 0 {
		return nil
	}

	s.mu.Lock()
	data, err := s.loadLocked()
	if err != nil {
		s.mu.Unlock()
		return err
	}

	var updated SessionState
	found := false
	for sessionID, state := range data.Sessions {
		for planIndex, plan := range state.Experiments {
			runIndex := findExperimentRunIndex(plan, runSessionID)
			if runIndex < 0 {
				continue
			}
			planRun := plan.Runs[runIndex]
			planRun.Artifacts = mergeArtifacts(planRun.Artifacts, artifacts)
			planRun.UpdatedAt = time.Now().Unix()
			plan.Runs[runIndex] = planRun
			plan.UpdatedAt = planRun.UpdatedAt
			state.Experiments[planIndex] = plan
			state.UpdatedAt = planRun.UpdatedAt
			state = normalizeState(state)
			data.Sessions[sessionID] = state
			updated = state
			found = true
			break
		}
		if found {
			break
		}
	}

	if !found {
		s.mu.Unlock()
		return nil
	}
	if err := s.saveLocked(data); err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	s.Publish(pubsub.UpdatedEvent, updated)
	return nil
}

func (s *service) Delete(_ context.Context, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("session ID is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.loadLocked()
	if err != nil {
		return err
	}
	state, ok := data.Sessions[sessionID]
	if !ok {
		return nil
	}
	state = normalizeState(state)
	delete(data.Sessions, sessionID)
	if err := s.saveLocked(data); err != nil {
		return err
	}
	s.Publish(pubsub.DeletedEvent, state)
	return nil
}

func (s *service) loadLocked() (*persistedState, error) {
	content, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return &persistedState{Sessions: map[string]SessionState{}}, nil
		}
		return nil, fmt.Errorf("failed to read research state: %w", err)
	}
	var state persistedState
	if err := json.Unmarshal(content, &state); err != nil {
		return nil, fmt.Errorf("failed to decode research state: %w", err)
	}
	if state.Sessions == nil {
		state.Sessions = make(map[string]SessionState)
	}
	for id, item := range state.Sessions {
		state.Sessions[id] = normalizeState(item)
	}
	return &state, nil
}

func (s *service) saveLocked(state *persistedState) error {
	if state == nil {
		state = &persistedState{Sessions: map[string]SessionState{}}
	}
	if state.Sessions == nil {
		state.Sessions = make(map[string]SessionState)
	}
	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode research state: %w", err)
	}
	if err := os.WriteFile(s.path, payload, 0o600); err != nil {
		return fmt.Errorf("failed to write research state: %w", err)
	}
	return nil
}

func normalizeState(state SessionState) SessionState {
	state.SessionID = strings.TrimSpace(state.SessionID)
	state.Objective = strings.TrimSpace(state.Objective)
	state.Domain = strings.TrimSpace(state.Domain)
	state.ActiveExperimentID = strings.TrimSpace(state.ActiveExperimentID)
	state.PromotedExperimentID = strings.TrimSpace(state.PromotedExperimentID)
	if state.Stage == "" {
		state.Stage = StageObjective
	}
	for i, experiment := range state.Experiments {
		state.Experiments[i] = normalizeExperiment(experiment)
	}
	slices.SortFunc(state.Experiments, func(a, b ExperimentPlan) int {
		if a.UpdatedAt == b.UpdatedAt {
			return strings.Compare(a.ID, b.ID)
		}
		if a.UpdatedAt > b.UpdatedAt {
			return -1
		}
		return 1
	})
	if state.ActiveExperimentID != "" && findExperimentIndex(state, state.ActiveExperimentID) < 0 {
		state.ActiveExperimentID = ""
	}
	if state.PromotedExperimentID != "" && findExperimentIndex(state, state.PromotedExperimentID) < 0 {
		state.PromotedExperimentID = ""
	}
	if state.ActiveExperimentID == "" && len(state.Experiments) > 0 {
		state.ActiveExperimentID = state.Experiments[0].ID
	}
	if state.UpdatedAt == 0 {
		state.UpdatedAt = time.Now().Unix()
	}
	return state
}

func normalizeExperiment(plan ExperimentPlan) ExperimentPlan {
	plan.ID = strings.TrimSpace(plan.ID)
	plan.Title = strings.TrimSpace(plan.Title)
	plan.HypothesisID = strings.TrimSpace(plan.HypothesisID)
	plan.ParentExperimentID = strings.TrimSpace(plan.ParentExperimentID)
	plan.LineageRootID = strings.TrimSpace(plan.LineageRootID)
	plan.EvolutionDecision = SelectionDecision(strings.TrimSpace(string(plan.EvolutionDecision)))
	if plan.LineageRootID == "" && plan.ID != "" {
		plan.LineageRootID = plan.ID
	}
	plan.Prompt = strings.TrimSpace(plan.Prompt)
	plan.Rationale = strings.TrimSpace(plan.Rationale)
	if plan.Status == "" {
		plan.Status = ExperimentPlanned
	}
	if plan.CreatedAt == 0 {
		plan.CreatedAt = time.Now().Unix()
	}
	if plan.UpdatedAt == 0 {
		plan.UpdatedAt = plan.CreatedAt
	}
	for i, run := range plan.Runs {
		plan.Runs[i] = normalizeExperimentRun(run)
	}
	slices.SortFunc(plan.Runs, func(a, b ExperimentRun) int {
		if a.UpdatedAt == b.UpdatedAt {
			return strings.Compare(a.SessionID, b.SessionID)
		}
		if a.UpdatedAt > b.UpdatedAt {
			return -1
		}
		return 1
	})
	if plan.LatestEval != nil {
		decision, _ := NormalizeSelectionDecision(string(plan.LatestEval.Decision))
		summary := strings.TrimSpace(plan.LatestEval.Summary)
		plan.LatestEval = &EvaluationResult{
			Score:     plan.LatestEval.Score,
			Decision:  decision,
			Summary:   summary,
			CreatedAt: plan.LatestEval.CreatedAt,
		}
		if plan.LatestEval.CreatedAt == 0 {
			plan.LatestEval.CreatedAt = plan.UpdatedAt
		}
	}
	return plan
}

func normalizeExperimentRun(run ExperimentRun) ExperimentRun {
	run.ID = strings.TrimSpace(run.ID)
	run.SessionID = strings.TrimSpace(run.SessionID)
	run.ParentSessionID = strings.TrimSpace(run.ParentSessionID)
	run.Title = strings.TrimSpace(run.Title)
	run.Detail = strings.TrimSpace(run.Detail)
	for i, artifact := range run.Artifacts {
		run.Artifacts[i] = normalizeArtifact(artifact)
	}
	slices.SortFunc(run.Artifacts, func(a, b ArtifactRef) int {
		if a.CreatedAt == b.CreatedAt {
			return strings.Compare(a.ID, b.ID)
		}
		if a.CreatedAt > b.CreatedAt {
			return -1
		}
		return 1
	})
	if run.CreatedAt == 0 {
		run.CreatedAt = time.Now().Unix()
	}
	if run.UpdatedAt == 0 {
		run.UpdatedAt = run.CreatedAt
	}
	return run
}

func normalizeArtifact(artifact ArtifactRef) ArtifactRef {
	artifact.ID = strings.TrimSpace(artifact.ID)
	artifact.Label = strings.TrimSpace(artifact.Label)
	artifact.Path = strings.TrimSpace(artifact.Path)
	artifact.URI = strings.TrimSpace(artifact.URI)
	artifact.Summary = strings.TrimSpace(artifact.Summary)
	artifact.SourceSessionID = strings.TrimSpace(artifact.SourceSessionID)
	if artifact.CreatedAt == 0 {
		artifact.CreatedAt = time.Now().Unix()
	}
	if artifact.Metadata == nil {
		artifact.Metadata = map[string]string{}
	}
	return artifact
}

func mergeArtifacts(existing []ArtifactRef, incoming []ArtifactRef) []ArtifactRef {
	if len(incoming) == 0 {
		return existing
	}
	index := make(map[string]int, len(existing))
	merged := append([]ArtifactRef{}, existing...)
	for i, artifact := range merged {
		key := artifactKey(artifact)
		if key != "" {
			index[key] = i
		}
	}
	for _, artifact := range incoming {
		artifact = normalizeArtifact(artifact)
		key := artifactKey(artifact)
		if idx, ok := index[key]; ok {
			merged[idx] = artifact
			continue
		}
		index[key] = len(merged)
		merged = append(merged, artifact)
	}
	return merged
}

func ArtifactIndex(state SessionState) []ArtifactRecord {
	items := make([]ArtifactRecord, 0)
	for _, experiment := range state.Experiments {
		for _, run := range experiment.Runs {
			for _, artifact := range run.Artifacts {
				items = append(items, ArtifactRecord{
					Artifact:           normalizeArtifact(artifact),
					ExperimentID:       experiment.ID,
					ExperimentTitle:    experiment.Title,
					ExperimentStatus:   experiment.Status,
					ParentExperimentID: experiment.ParentExperimentID,
					RunID:              run.SessionID,
					RunTitle:           run.Title,
					RunStatus:          run.Status,
					Evaluation:         experiment.LatestEval,
				})
			}
		}
	}
	slices.SortFunc(items, func(a, b ArtifactRecord) int {
		if a.Artifact.CreatedAt == b.Artifact.CreatedAt {
			return strings.Compare(a.Artifact.ID, b.Artifact.ID)
		}
		if a.Artifact.CreatedAt > b.Artifact.CreatedAt {
			return -1
		}
		return 1
	})
	return items
}

func LineageGroups(state SessionState) []LineageGroup {
	groupsByRoot := make(map[string]*LineageGroup)
	for _, plan := range state.Experiments {
		rootID := strings.TrimSpace(plan.LineageRootID)
		if rootID == "" {
			rootID = plan.ID
		}
		group := groupsByRoot[rootID]
		if group == nil {
			group = &LineageGroup{
				RootID:    rootID,
				BestScore: -1,
			}
			groupsByRoot[rootID] = group
		}
		if plan.ID == rootID || group.RootTitle == "" {
			group.RootTitle = plan.Title
		}
		if plan.LatestEval != nil && plan.LatestEval.Score > group.BestScore {
			group.BestScore = plan.LatestEval.Score
		}
		group.Plans = append(group.Plans, plan)
	}

	groups := make([]LineageGroup, 0, len(groupsByRoot))
	for _, group := range groupsByRoot {
		slices.SortFunc(group.Plans, func(a, b ExperimentPlan) int {
			if a.Generation != b.Generation {
				return a.Generation - b.Generation
			}
			if a.UpdatedAt > b.UpdatedAt {
				return -1
			}
			if a.UpdatedAt < b.UpdatedAt {
				return 1
			}
			return strings.Compare(a.ID, b.ID)
		})
		groups = append(groups, *group)
	}

	slices.SortFunc(groups, func(a, b LineageGroup) int {
		aPromoted := a.RootID == state.PromotedExperimentID
		bPromoted := b.RootID == state.PromotedExperimentID
		if aPromoted != bPromoted {
			if aPromoted {
				return -1
			}
			return 1
		}
		if a.BestScore != b.BestScore {
			if a.BestScore > b.BestScore {
				return -1
			}
			return 1
		}
		return strings.Compare(a.RootID, b.RootID)
	})
	return groups
}

func FilterArtifactIndex(items []ArtifactRecord, query string) []ArtifactRecord {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return append([]ArtifactRecord{}, items...)
	}

	out := make([]ArtifactRecord, 0, len(items))
	for _, item := range items {
		if artifactRecordMatchesQuery(item, query) {
			out = append(out, item)
		}
	}
	return out
}

func FindArtifact(items []ArtifactRecord, artifactID string) (ArtifactRecord, bool) {
	artifactID = strings.TrimSpace(artifactID)
	if artifactID == "" {
		return ArtifactRecord{}, false
	}

	for _, item := range items {
		if item.Artifact.ID == artifactID {
			return item, true
		}
	}

	var matched ArtifactRecord
	found := false
	for _, item := range items {
		if strings.HasPrefix(item.Artifact.ID, artifactID) {
			if found {
				return ArtifactRecord{}, false
			}
			matched = item
			found = true
		}
	}
	return matched, found
}

func artifactRecordMatchesQuery(item ArtifactRecord, query string) bool {
	content := strings.ToLower(strings.Join([]string{
		string(item.Artifact.Kind),
		item.Artifact.ID,
		item.Artifact.Label,
		item.Artifact.Path,
		item.Artifact.URI,
		item.Artifact.Summary,
		item.Artifact.SourceSessionID,
		item.ExperimentID,
		item.ExperimentTitle,
		string(item.ExperimentStatus),
		item.ParentExperimentID,
		item.RunID,
		item.RunTitle,
		string(item.RunStatus),
		artifactRecordEvaluationText(item.Evaluation),
		artifactMetadataText(item.Artifact.Metadata),
	}, "\n"))

	for _, token := range strings.Fields(query) {
		if !strings.Contains(content, token) {
			return false
		}
	}
	return true
}

func artifactRecordEvaluationText(eval *EvaluationResult) string {
	if eval == nil {
		return ""
	}
	return strings.Join([]string{
		fmt.Sprintf("%.4f", eval.Score),
		string(eval.Decision),
		eval.Summary,
	}, "\n")
}

func NormalizeSelectionDecision(decision string) (SelectionDecision, error) {
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case string(DecisionKeep), "promote":
		return DecisionKeep, nil
	case string(DecisionDiscard), "drop":
		return DecisionDiscard, nil
	case string(DecisionMutate), "rerun":
		return DecisionMutate, nil
	case string(DecisionBranch), "fork":
		return DecisionBranch, nil
	default:
		return "", fmt.Errorf("invalid selection decision %q; use keep, discard, mutate, or branch", strings.TrimSpace(decision))
	}
}

func artifactMetadataText(metadata map[string]string) string {
	if len(metadata) == 0 {
		return ""
	}

	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	parts := make([]string, 0, len(keys)*2)
	for _, key := range keys {
		parts = append(parts, key, metadata[key])
	}
	return strings.Join(parts, "\n")
}

func artifactKey(artifact ArtifactRef) string {
	if strings.TrimSpace(artifact.ID) != "" {
		return artifact.ID
	}
	return strings.Join([]string{
		string(artifact.Kind),
		artifact.Label,
		artifact.Path,
		artifact.URI,
		artifact.SourceSessionID,
	}, "|")
}

func findExperiment(state SessionState, experimentID string) (ExperimentPlan, bool) {
	index := findExperimentIndex(state, experimentID)
	if index < 0 {
		return ExperimentPlan{}, false
	}
	return state.Experiments[index], true
}

func findExperimentIndex(state SessionState, experimentID string) int {
	experimentID = strings.TrimSpace(experimentID)
	for i, experiment := range state.Experiments {
		if experiment.ID == experimentID {
			return i
		}
	}
	return -1
}

func findExperimentRunIndex(plan ExperimentPlan, sessionID string) int {
	sessionID = strings.TrimSpace(sessionID)
	for i, run := range plan.Runs {
		if run.SessionID == sessionID {
			return i
		}
	}
	return -1
}

func experimentStatusFromTaskStatus(status taskrun.Status, hasEvaluation bool) ExperimentStatus {
	if hasEvaluation {
		return ExperimentEvaluated
	}
	switch status {
	case taskrun.StatusQueued, taskrun.StatusRunning:
		return ExperimentRunning
	case taskrun.StatusComplete:
		return ExperimentCompleted
	case taskrun.StatusCanceled:
		return ExperimentCanceled
	case taskrun.StatusFailed, taskrun.StatusBlocked:
		return ExperimentFailed
	default:
		return ExperimentPlanned
	}
}

func stageFromExperiment(plan ExperimentPlan) Stage {
	if plan.LatestEval != nil {
		if plan.LatestEval.Decision == DecisionKeep || plan.LatestEval.Decision == DecisionDiscard {
			return StageDecision
		}
		return StageEvaluation
	}
	return StageExperiment
}
