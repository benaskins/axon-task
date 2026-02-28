package task

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
)

const schema = `
CREATE SCHEMA IF NOT EXISTS task_runner;

CREATE TABLE IF NOT EXISTS task_runner.tasks (
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
    ON task_runner.tasks(requested_by, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_tasks_status
    ON task_runner.tasks(status) WHERE status IN ('queued', 'running');
`

// PostgresStore implements Store using PostgreSQL.
type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore opens a connection pool and returns a PostgresStore.
func NewPostgresStore(databaseURL string) (*PostgresStore, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &PostgresStore{db: db}, nil
}

func (s *PostgresStore) RunMigrations(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return err
	}
	// Add columns that may be missing on tables created before these were added.
	// Each statement is idempotent (IF NOT EXISTS / catches "already exists").
	alterStmts := []string{
		`ALTER TABLE task_runner.tasks ADD COLUMN IF NOT EXISTS type TEXT NOT NULL DEFAULT 'claude_session'`,
		`ALTER TABLE task_runner.tasks ADD COLUMN IF NOT EXISTS username TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE task_runner.tasks ADD COLUMN IF NOT EXISTS artifact_id TEXT NOT NULL DEFAULT ''`,
	}
	for _, stmt := range alterStmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("alter table: %w", err)
		}
	}
	return nil
}

func (s *PostgresStore) Save(ctx context.Context, task *Task) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO task_runner.tasks (id, type, status, description, requested_by, username, summary, error, artifact_id, created_at, started_at, completed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (id) DO UPDATE SET
			status       = EXCLUDED.status,
			summary      = EXCLUDED.summary,
			error        = EXCLUDED.error,
			artifact_id  = EXCLUDED.artifact_id,
			started_at   = EXCLUDED.started_at,
			completed_at = EXCLUDED.completed_at
	`, task.ID, task.Type, task.Status, task.Description, task.RequestedBy, task.Username,
		task.Summary, task.Error, task.ArtifactID, task.CreatedAt, task.StartedAt, task.CompletedAt)
	return err
}

func (s *PostgresStore) Get(ctx context.Context, id string) (*Task, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, type, status, description, requested_by, username, summary, error, artifact_id, created_at, started_at, completed_at
		FROM task_runner.tasks
		WHERE id = $1 AND deleted_at IS NULL
	`, id)

	var t Task
	err := row.Scan(&t.ID, &t.Type, &t.Status, &t.Description, &t.RequestedBy, &t.Username,
		&t.Summary, &t.Error, &t.ArtifactID, &t.CreatedAt, &t.StartedAt, &t.CompletedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *PostgresStore) ListByAgent(ctx context.Context, agentSlug string, limit, offset int) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, status, description, requested_by, username, summary, error, artifact_id, created_at, started_at, completed_at
		FROM task_runner.tasks
		WHERE requested_by = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, agentSlug, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.Type, &t.Status, &t.Description, &t.RequestedBy, &t.Username,
			&t.Summary, &t.Error, &t.ArtifactID, &t.CreatedAt, &t.StartedAt, &t.CompletedAt); err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (s *PostgresStore) Close() error {
	return s.db.Close()
}
