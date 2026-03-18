package taskrun

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/SciMate-AI/scicli/internal/pubsub"
	"github.com/SciMate-AI/scicli/internal/session"
)

const (
	recentTimelineLimit       = 64
	minProgressPersistSpacing = 2 * time.Second
)

type Status string

const (
	StatusQueued   Status = "queued"
	StatusRunning  Status = "running"
	StatusComplete Status = "complete"
	StatusBlocked  Status = "blocked"
	StatusCanceled Status = "canceled"
	StatusFailed   Status = "failed"
	StatusEmpty    Status = "empty"
	StatusIdle     Status = "idle"
)

type EventKind string

const (
	EventQueued          EventKind = "queued"
	EventStarted         EventKind = "started"
	EventProgress        EventKind = "progress"
	EventFinished        EventKind = "finished"
	EventCancelRequested EventKind = "cancel_requested"
)

type Run struct {
	SessionID       string `json:"sessionId"`
	ParentSessionID string `json:"parentSessionId"`
	Title           string `json:"title"`
	Prompt          string `json:"prompt,omitempty"`
	Status          Status `json:"status"`
	Detail          string `json:"detail,omitempty"`
	StartedAt       int64  `json:"startedAt,omitempty"`
	FinishedAt      int64  `json:"finishedAt,omitempty"`
	UpdatedAt       int64  `json:"updatedAt"`
}

type EventMetadata struct {
	ToolInputPreview string `json:"toolInputPreview,omitempty"`
	FinishReason     string `json:"finishReason,omitempty"`
	PermissionReason string `json:"permissionReason,omitempty"`
}

type Event struct {
	ID              int64         `json:"id"`
	SessionID       string        `json:"sessionId"`
	ParentSessionID string        `json:"parentSessionId"`
	Title           string        `json:"title"`
	Prompt          string        `json:"prompt,omitempty"`
	ToolName        string        `json:"toolName,omitempty"`
	Metadata        EventMetadata `json:"metadata,omitempty"`
	Status          Status        `json:"status"`
	Detail          string        `json:"detail,omitempty"`
	Kind            EventKind     `json:"kind"`
	CreatedAt       int64         `json:"createdAt"`
}

type Service interface {
	pubsub.Suscriber[Run]
	SubscribeEvents(context.Context) <-chan pubsub.Event[Event]
	Queue(sess session.Session, prompt string)
	Start(sess session.Session)
	UpdateDetail(sessionID string, detail string, toolName string, metadata *EventMetadata)
	Finish(sessionID string, status Status, detail string, metadata *EventMetadata)
	RegisterCancel(sessionID string, cancel context.CancelFunc)
	ClearCancel(sessionID string)
	Cancel(sessionID string) error
	Snapshot(parentSessionID string) []Run
	Timeline(sessionID string, limit int) []Event
	Get(sessionID string) (Run, bool)
}

type service struct {
	*pubsub.Broker[Run]
	events   *pubsub.Broker[Event]
	db       *sql.DB
	mu       sync.RWMutex
	runs     map[string]Run
	cancels  map[string]context.CancelFunc
	children map[string]map[string]struct{}
	recent   map[string][]Event
}

func NewService(conn *sql.DB) Service {
	svc := &service{
		Broker:   pubsub.NewBroker[Run](),
		events:   pubsub.NewBroker[Event](),
		db:       conn,
		runs:     make(map[string]Run),
		cancels:  make(map[string]context.CancelFunc),
		children: make(map[string]map[string]struct{}),
		recent:   make(map[string][]Event),
	}
	svc.loadPersistedRuns()
	return svc
}

func (s *service) SubscribeEvents(ctx context.Context) <-chan pubsub.Event[Event] {
	return s.events.Subscribe(ctx)
}

func (s *service) Queue(sess session.Session, prompt string) {
	s.recordEvent(Event{
		SessionID:       sess.ID,
		ParentSessionID: sess.ParentSessionID,
		Title:           fallbackTaskTitle(sess.Title),
		Prompt:          strings.TrimSpace(prompt),
		Status:          StatusQueued,
		Detail:          "Queued",
		Kind:            EventQueued,
	})
}

func (s *service) Start(sess session.Session) {
	s.recordEvent(Event{
		SessionID:       sess.ID,
		ParentSessionID: sess.ParentSessionID,
		Title:           fallbackTaskTitle(sess.Title),
		Status:          StatusRunning,
		Detail:          "Starting",
		Kind:            EventStarted,
	})
}

func (s *service) UpdateDetail(sessionID string, detail string, toolName string, metadata *EventMetadata) {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return
	}
	toolName = strings.TrimSpace(toolName)
	normalizedMetadata := normalizeEventMetadata(metadata)

	s.mu.RLock()
	run, ok := s.runs[sessionID]
	s.mu.RUnlock()
	if !ok {
		return
	}

	now := time.Now().Unix()
	if toolName == "" && normalizedMetadata.IsZero() && run.Detail == detail && now-run.UpdatedAt < int64(minProgressPersistSpacing/time.Second) {
		return
	}

	s.recordEvent(Event{
		SessionID:       run.SessionID,
		ParentSessionID: run.ParentSessionID,
		Title:           run.Title,
		Prompt:          run.Prompt,
		ToolName:        toolName,
		Metadata:        normalizedMetadata,
		Status:          StatusRunning,
		Detail:          detail,
		Kind:            EventProgress,
	})
}

func (s *service) Finish(sessionID string, status Status, detail string, metadata *EventMetadata) {
	s.mu.RLock()
	run, ok := s.runs[sessionID]
	s.mu.RUnlock()
	if !ok {
		return
	}

	s.mu.Lock()
	delete(s.cancels, sessionID)
	s.mu.Unlock()

	s.recordEvent(Event{
		SessionID:       run.SessionID,
		ParentSessionID: run.ParentSessionID,
		Title:           run.Title,
		Prompt:          run.Prompt,
		Metadata:        normalizeEventMetadata(metadata),
		Status:          status,
		Detail:          strings.TrimSpace(detail),
		Kind:            EventFinished,
	})
}

func (s *service) RegisterCancel(sessionID string, cancel context.CancelFunc) {
	if cancel == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancels[sessionID] = cancel
}

func (s *service) ClearCancel(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cancels, sessionID)
}

func (s *service) Cancel(sessionID string) error {
	s.mu.Lock()
	cancel, ok := s.cancels[sessionID]
	if ok {
		delete(s.cancels, sessionID)
	}
	run, runOk := s.runs[sessionID]
	s.mu.Unlock()

	if !ok {
		return fmt.Errorf("task %q is not running", sessionID)
	}

	if runOk {
		s.recordEvent(Event{
			SessionID:       run.SessionID,
			ParentSessionID: run.ParentSessionID,
			Title:           run.Title,
			Prompt:          run.Prompt,
			Status:          StatusCanceled,
			Detail:          "Cancel requested",
			Kind:            EventCancelRequested,
		})
	}

	cancel()
	return nil
}

func (s *service) Snapshot(parentSessionID string) []Run {
	s.mu.RLock()
	defer s.mu.RUnlock()

	children := s.children[parentSessionID]
	if len(children) == 0 {
		return nil
	}

	ids := make([]string, 0, len(children))
	for id := range children {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b string) int {
		if s.runs[a].UpdatedAt == s.runs[b].UpdatedAt {
			return strings.Compare(a, b)
		}
		if s.runs[a].UpdatedAt > s.runs[b].UpdatedAt {
			return -1
		}
		return 1
	})

	out := make([]Run, 0, len(ids))
	for _, id := range ids {
		out = append(out, s.runs[id])
	}
	return out
}

func (s *service) Timeline(sessionID string, limit int) []Event {
	if limit <= 0 {
		limit = 20
	}
	if s.db == nil {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return copyRecentEvents(s.recent[sessionID], limit)
	}

	rows, err := s.db.QueryContext(
		context.Background(),
		`SELECT id, session_id, parent_session_id, title, prompt, tool_name, metadata_json, event_kind, status, detail, created_at
		FROM task_run_events
		WHERE session_id = ?
		ORDER BY id DESC
		LIMIT ?`,
		sessionID,
		limit,
	)
	if err != nil {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return copyRecentEvents(s.recent[sessionID], limit)
	}
	defer rows.Close()

	events := make([]Event, 0, limit)
	for rows.Next() {
		var event Event
		var metadataJSON string
		if err := rows.Scan(
			&event.ID,
			&event.SessionID,
			&event.ParentSessionID,
			&event.Title,
			&event.Prompt,
			&event.ToolName,
			&metadataJSON,
			&event.Kind,
			&event.Status,
			&event.Detail,
			&event.CreatedAt,
		); err != nil {
			return nil
		}
		event.Metadata = decodeEventMetadata(metadataJSON)
		events = append(events, event)
	}
	slices.Reverse(events)
	return events
}

func (s *service) Get(sessionID string) (Run, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.runs[sessionID]
	return run, ok
}

func (s *service) recordEvent(event Event) {
	event.SessionID = strings.TrimSpace(event.SessionID)
	event.ParentSessionID = strings.TrimSpace(event.ParentSessionID)
	if event.SessionID == "" || event.ParentSessionID == "" {
		return
	}
	event.Title = fallbackTaskTitle(event.Title)
	event.Prompt = strings.TrimSpace(event.Prompt)
	event.ToolName = strings.TrimSpace(event.ToolName)
	event.Metadata = normalizeEventMetadata(&event.Metadata)
	event.Detail = strings.TrimSpace(event.Detail)
	if event.CreatedAt == 0 {
		event.CreatedAt = time.Now().Unix()
	}

	if persisted, err := s.persistEvent(event); err == nil {
		event = persisted
	}

	s.mu.Lock()
	existing, exists := s.runs[event.SessionID]
	run := mergeRun(existing, event)
	s.runs[event.SessionID] = run
	if s.children[event.ParentSessionID] == nil {
		s.children[event.ParentSessionID] = make(map[string]struct{})
	}
	s.children[event.ParentSessionID][event.SessionID] = struct{}{}
	s.recent[event.SessionID] = appendRecentEvent(s.recent[event.SessionID], event)
	s.mu.Unlock()

	runEventType := pubsub.CreatedEvent
	if exists {
		runEventType = pubsub.UpdatedEvent
	}
	s.Publish(runEventType, run)
	s.events.Publish(pubsub.CreatedEvent, event)
}

func (s *service) persistEvent(event Event) (Event, error) {
	if s.db == nil {
		return event, nil
	}

	result, err := s.db.ExecContext(
		context.Background(),
		`INSERT INTO task_run_events (
			session_id,
			parent_session_id,
			title,
			prompt,
			tool_name,
			metadata_json,
			event_kind,
			status,
			detail,
			created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.SessionID,
		event.ParentSessionID,
		event.Title,
		event.Prompt,
		event.ToolName,
		encodeEventMetadata(event.Metadata),
		event.Kind,
		event.Status,
		event.Detail,
		event.CreatedAt,
	)
	if err != nil {
		return event, err
	}
	if id, idErr := result.LastInsertId(); idErr == nil {
		event.ID = id
	}
	return event, nil
}

func (s *service) loadPersistedRuns() {
	if s.db == nil {
		return
	}

	rows, err := s.db.QueryContext(
		context.Background(),
		`SELECT id, session_id, parent_session_id, title, prompt, tool_name, metadata_json, event_kind, status, detail, created_at
		FROM task_run_events
		ORDER BY id ASC`,
	)
	if err != nil {
		return
	}
	defer rows.Close()

	s.mu.Lock()
	defer s.mu.Unlock()

	for rows.Next() {
		var event Event
		var metadataJSON string
		if err := rows.Scan(
			&event.ID,
			&event.SessionID,
			&event.ParentSessionID,
			&event.Title,
			&event.Prompt,
			&event.ToolName,
			&metadataJSON,
			&event.Kind,
			&event.Status,
			&event.Detail,
			&event.CreatedAt,
		); err != nil {
			return
		}
		event.Metadata = decodeEventMetadata(metadataJSON)
		run := mergeRun(s.runs[event.SessionID], event)
		s.runs[event.SessionID] = run
		if s.children[event.ParentSessionID] == nil {
			s.children[event.ParentSessionID] = make(map[string]struct{})
		}
		s.children[event.ParentSessionID][event.SessionID] = struct{}{}
	}
}

func mergeRun(existing Run, event Event) Run {
	run := existing
	if run.SessionID == "" {
		run.SessionID = event.SessionID
	}
	if strings.TrimSpace(event.ParentSessionID) != "" {
		run.ParentSessionID = event.ParentSessionID
	}
	if strings.TrimSpace(event.Title) != "" {
		run.Title = event.Title
	}
	if strings.TrimSpace(event.Prompt) != "" {
		run.Prompt = event.Prompt
	}
	if event.Status != "" {
		run.Status = event.Status
	}
	if strings.TrimSpace(event.Detail) != "" || run.Detail == "" {
		run.Detail = event.Detail
	}
	run.UpdatedAt = event.CreatedAt

	switch event.Kind {
	case EventStarted:
		if run.StartedAt == 0 {
			run.StartedAt = event.CreatedAt
		}
		run.FinishedAt = 0
	case EventFinished:
		if run.StartedAt == 0 {
			run.StartedAt = event.CreatedAt
		}
		run.FinishedAt = event.CreatedAt
	case EventCancelRequested:
		if run.StartedAt == 0 {
			run.StartedAt = event.CreatedAt
		}
	}

	return run
}

func appendRecentEvent(events []Event, event Event) []Event {
	events = append(events, event)
	if len(events) <= recentTimelineLimit {
		return events
	}
	return append([]Event{}, events[len(events)-recentTimelineLimit:]...)
}

func copyRecentEvents(events []Event, limit int) []Event {
	if len(events) == 0 {
		return nil
	}
	if limit >= len(events) {
		return append([]Event{}, events...)
	}
	return append([]Event{}, events[len(events)-limit:]...)
}

func fallbackTaskTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "Delegated Task"
	}
	return title
}

func normalizeEventMetadata(metadata *EventMetadata) EventMetadata {
	if metadata == nil {
		return EventMetadata{}
	}
	return EventMetadata{
		ToolInputPreview: strings.TrimSpace(metadata.ToolInputPreview),
		FinishReason:     strings.TrimSpace(metadata.FinishReason),
		PermissionReason: strings.TrimSpace(metadata.PermissionReason),
	}
}

func (m EventMetadata) IsZero() bool {
	return m.ToolInputPreview == "" && m.FinishReason == "" && m.PermissionReason == ""
}

func encodeEventMetadata(metadata EventMetadata) string {
	if metadata.IsZero() {
		return ""
	}
	payload, err := json.Marshal(metadata)
	if err != nil {
		return ""
	}
	return string(payload)
}

func decodeEventMetadata(raw string) EventMetadata {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return EventMetadata{}
	}
	var metadata EventMetadata
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return EventMetadata{}
	}
	return normalizeEventMetadata(&metadata)
}
