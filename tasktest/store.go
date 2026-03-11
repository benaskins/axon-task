package tasktest

import (
	"context"
	"sort"
	"sync"

	"github.com/benaskins/axon-task"
)

// MemoryStore implements task.Store using an in-memory map. Used for tests.
type MemoryStore struct {
	mu    sync.Mutex
	items map[string]*task.Task
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{items: make(map[string]*task.Task)}
}

func (s *MemoryStore) Save(_ context.Context, t *task.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *t
	s.items[t.ID] = &copy
	return nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (*task.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.items[id]
	if !ok {
		return nil, nil
	}
	copy := *t
	return &copy, nil
}

func (s *MemoryStore) ListByAgent(_ context.Context, agentSlug string, limit, offset int) ([]task.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var matching []task.Task
	for _, t := range s.items {
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
