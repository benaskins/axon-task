-- +goose Up
CREATE TABLE IF NOT EXISTS tasks (
    id           TEXT PRIMARY KEY,
    type         TEXT NOT NULL DEFAULT 'claude_session',
    status       TEXT NOT NULL,
    description  TEXT NOT NULL,
    requested_by TEXT NOT NULL,
    username     TEXT NOT NULL DEFAULT '',
    summary      TEXT NOT NULL DEFAULT '',
    error        TEXT NOT NULL DEFAULT '',
    artifact_id  TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL,
    started_at   TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    deleted_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_tasks_agent
    ON tasks(requested_by, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_tasks_status
    ON tasks(status) WHERE status IN ('queued', 'running');

-- +goose Down
DROP TABLE IF EXISTS tasks;
