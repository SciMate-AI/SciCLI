package taskrun

import (
	"database/sql"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"

	"github.com/SciMate-AI/scicli/internal/session"
)

func TestQueueAndSnapshot(t *testing.T) {
	svc := NewService(nil)
	svc.Queue(session.Session{ID: "child-1", ParentSessionID: "parent-1", Title: "Child 1"}, "inspect logs")
	svc.Queue(session.Session{ID: "child-2", ParentSessionID: "parent-1", Title: "Child 2"}, "inspect tests")

	items := svc.Snapshot("parent-1")
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
}

func TestCancelMarksRunCanceled(t *testing.T) {
	svc := NewService(nil)
	svc.Queue(session.Session{ID: "child-1", ParentSessionID: "parent-1", Title: "Child 1"}, "inspect logs")

	called := false
	svc.RegisterCancel("child-1", func() {
		called = true
	})
	if err := svc.Cancel("child-1"); err != nil {
		t.Fatalf("cancel failed: %v", err)
	}
	if !called {
		t.Fatalf("expected cancel func to be called")
	}
	run, ok := svc.Get("child-1")
	if !ok {
		t.Fatalf("expected run to exist")
	}
	if run.Status != StatusCanceled {
		t.Fatalf("expected canceled status, got %q", run.Status)
	}
	timeline := svc.Timeline("child-1", 10)
	if len(timeline) != 2 {
		t.Fatalf("expected 2 events, got %d", len(timeline))
	}
	if timeline[1].Kind != EventCancelRequested {
		t.Fatalf("expected cancel-requested event, got %q", timeline[1].Kind)
	}
}

func TestPersistenceRehydratesRunsAndTimeline(t *testing.T) {
	conn := openTaskRunTestDB(t)

	svc := NewService(conn)
	svc.Queue(session.Session{ID: "child-1", ParentSessionID: "parent-1", Title: "Child 1"}, "inspect logs")
	svc.Start(session.Session{ID: "child-1", ParentSessionID: "parent-1", Title: "Child 1"})
	svc.UpdateDetail("child-1", "Running tool bash", "bash", &EventMetadata{ToolInputPreview: "{\"command\":\"go test ./...\"}"})
	svc.Finish("child-1", StatusComplete, "Finished successfully", &EventMetadata{FinishReason: "end_turn"})

	var persisted int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM task_run_events`).Scan(&persisted); err != nil {
		t.Fatalf("count persisted events: %v", err)
	}
	if persisted != 4 {
		t.Fatalf("expected 4 persisted events, got %d", persisted)
	}

	reloaded := NewService(conn)
	run, ok := reloaded.Get("child-1")
	if !ok {
		t.Fatalf("expected persisted run to be rehydrated")
	}
	if run.Status != StatusComplete {
		t.Fatalf("expected complete status, got %q", run.Status)
	}
	if run.FinishedAt == 0 {
		t.Fatalf("expected finished timestamp")
	}

	timeline := reloaded.Timeline("child-1", 10)
	if len(timeline) != 4 {
		t.Fatalf("expected 4 timeline events, got %d", len(timeline))
	}
	if timeline[0].Kind != EventQueued || timeline[len(timeline)-1].Kind != EventFinished {
		t.Fatalf("unexpected timeline order: first=%q last=%q", timeline[0].Kind, timeline[len(timeline)-1].Kind)
	}
	if timeline[2].ToolName != "bash" {
		t.Fatalf("expected tool name bash, got %q", timeline[2].ToolName)
	}
	if timeline[2].Metadata.ToolInputPreview == "" {
		t.Fatalf("expected tool input preview metadata to persist")
	}
	if timeline[3].Metadata.FinishReason != "end_turn" {
		t.Fatalf("expected finish reason metadata, got %q", timeline[3].Metadata.FinishReason)
	}
}

func openTaskRunTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dbPath := t.TempDir() + "/taskrun-test.db"
	conn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() {
		_ = conn.Close()
	})

	statements := []string{
		`CREATE TABLE sessions (id TEXT PRIMARY KEY);`,
		`CREATE TABLE task_run_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			parent_session_id TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			prompt TEXT NOT NULL DEFAULT '',
			tool_name TEXT NOT NULL DEFAULT '',
			metadata_json TEXT NOT NULL DEFAULT '',
			event_kind TEXT NOT NULL,
			status TEXT NOT NULL,
			detail TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
		);`,
	}
	for _, stmt := range statements {
		if _, err := conn.Exec(stmt); err != nil {
			t.Fatalf("exec schema: %v", err)
		}
	}
	return conn
}
