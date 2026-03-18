-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS task_run_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    parent_session_id TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    prompt TEXT NOT NULL DEFAULT '',
    event_kind TEXT NOT NULL,
    status TEXT NOT NULL,
    detail TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    FOREIGN KEY (session_id) REFERENCES sessions (id) ON DELETE CASCADE,
    FOREIGN KEY (parent_session_id) REFERENCES sessions (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_task_run_events_session_id_id
ON task_run_events (session_id, id DESC);

CREATE INDEX IF NOT EXISTS idx_task_run_events_parent_session_id_id
ON task_run_events (parent_session_id, id DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_task_run_events_parent_session_id_id;
DROP INDEX IF EXISTS idx_task_run_events_session_id_id;
DROP TABLE IF EXISTS task_run_events;
-- +goose StatementEnd
