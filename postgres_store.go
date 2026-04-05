package task

import (
	"context"
	"database/sql"
)

// PostgresStore implements Store using PostgreSQL.
// The caller is responsible for opening the database (via pool.NewPool)
// and running migrations (via migration.Run) before constructing the store.
type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore wraps an existing database connection as a PostgresStore.
func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) Save(ctx context.Context, task *Task) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tasks (id, type, status, description, requested_by, username, summary, error, artifact_id, created_at, started_at, completed_at)
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
		FROM tasks
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
		FROM tasks
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
