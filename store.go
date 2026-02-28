package task

import "context"

// Store abstracts task persistence operations.
type Store interface {
	// Save persists a task (insert or update).
	Save(ctx context.Context, task *Task) error

	// Get retrieves a task by ID. Returns nil if not found.
	Get(ctx context.Context, id string) (*Task, error)

	// ListByAgent returns tasks for an agent, newest first.
	ListByAgent(ctx context.Context, agentSlug string, limit, offset int) ([]Task, error)

	// RunMigrations applies the database schema.
	RunMigrations(ctx context.Context) error
}
