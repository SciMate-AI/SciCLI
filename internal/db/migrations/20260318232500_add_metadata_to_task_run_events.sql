-- +goose Up
-- +goose StatementBegin
ALTER TABLE task_run_events ADD COLUMN metadata_json TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE task_run_events DROP COLUMN metadata_json;
-- +goose StatementEnd
