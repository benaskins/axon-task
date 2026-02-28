package tasktest

import (
	"context"
	"sort"
	"sync"

	tasks "github.com/benaskins/axon-tasks"
)

// MemoryStore implements tasks.Store using an in-memory map. Used for tests.
type MemoryStore struct {
	mu    sync.Mutex
	tasks map[string]*tasks.Task
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{tasks: make(map[string]*tasks.Task)}
}

func (s *MemoryStore) RunMigrations(_ context.Context) error {
	return nil
}

func (s *MemoryStore) Save(_ context.Context, task *tasks.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *task
	s.tasks[task.ID] = &copy
	return nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (*tasks.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, nil
	}
	copy := *t
	return &copy, nil
}

func (s *MemoryStore) ListByAgent(_ context.Context, agentSlug string, limit, offset int) ([]tasks.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var matching []tasks.Task
	for _, t := range s.tasks {
		if t.RequestedBy == agentSlug {
			matching = append(matching, *t)
		}
	}

	sort.Slice(matching, func(i, j int) bool {
		return matching[i].CreatedAt.After(matching[j].CreatedAt)
	})

	if offset >= len(matching) {
		return nil, nil
	}
	matching = matching[offset:]
	if limit < len(matching) {
		matching = matching[:limit]
	}
	return matching, nil
}
