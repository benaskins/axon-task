package task

import "context"

// ReadStore provides read-only access to tasks.
type ReadStore interface {
	// Get retrieves a task by ID. Returns nil if not found.
	Get(ctx context.Context, id string) (*Task, error)

	// ListByAgent returns tasks for an agent, newest first.
	ListByAgent(ctx context.Context, agentSlug string, limit, offset int) ([]Task, error)
}

// ReadModelWriter provides write operations for projectors to build the read model.
type ReadModelWriter interface {
	// Save persists a task (insert or update).
	Save(ctx context.Context, task *Task) error
}

// Store combines ReadStore and ReadModelWriter. Composition roots provide
// a concrete implementation that satisfies both interfaces.
type Store interface {
	ReadStore
	ReadModelWriter
}
