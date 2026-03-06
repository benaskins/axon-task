package task

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestFilterEnv(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"HOME=/home/test",
		"CLAUDECODE=1",
		"CLAUDE_CODE_ENTRYPOINT=something",
		"OTHER_VAR=value",
	}

	filtered := filterEnv(env)

	if len(filtered) != 3 {
		t.Fatalf("expected 3 env vars, got %d: %v", len(filtered), filtered)
	}

	for _, e := range filtered {
		if e == "CLAUDECODE=1" || e == "CLAUDE_CODE_ENTRYPOINT=something" {
			t.Errorf("should have filtered out %s", e)
		}
	}
}

func TestDefaultPromptBuilder(t *testing.T) {
	prompt := DefaultPromptBuilder("change the greeting to say hello")
	if prompt == "" {
		t.Fatal("prompt should not be empty")
	}
	if !contains(prompt, "change the greeting to say hello") {
		t.Error("prompt should contain the description")
	}
	if !contains(prompt, ".commit-message") {
		t.Error("prompt should instruct Claude to write commit message to file")
	}
	if contains(prompt, "git commit") {
		t.Error("prompt should not instruct Claude to commit")
	}
	if contains(prompt, "deploy") {
		t.Error("prompt should not instruct Claude to deploy")
	}
	if contains(prompt, "Aurelia") {
		t.Error("default prompt should not reference Aurelia")
	}
}

func TestCustomPromptBuilder(t *testing.T) {
	store := newMemoryStore()
	executor := NewExecutor("claude", "/tmp", "test", store)
	defer executor.Shutdown()

	called := false
	executor.PromptBuilder = func(description string) string {
		called = true
		return "custom: " + description
	}

	result := executor.PromptBuilder("do something")
	if !called {
		t.Error("custom prompt builder should have been called")
	}
	if result != "custom: do something" {
		t.Errorf("unexpected prompt: %s", result)
	}
}

func TestTruncate(t *testing.T) {
	short := "hello"
	if truncate(short, 10) != "hello" {
		t.Error("short string should not be truncated")
	}

	long := "hello world this is a long string"
	result := truncate(long, 10)
	if len(result) > 30 { // 10 + "...(truncated)"
		t.Errorf("truncated string too long: %s", result)
	}
}

func TestSubmitAndGet(t *testing.T) {
	tmpDir := t.TempDir()
	scriptsDir := filepath.Join(tmpDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		t.Fatal(err)
	}
	mockWrapper := filepath.Join(scriptsDir, "claude-isolated")
	err := os.WriteFile(mockWrapper, []byte("#!/bin/sh\necho 'task completed successfully'\n"), 0755)
	if err != nil {
		t.Fatal(err)
	}

	store := newMemoryStore()
	executor := NewExecutor("claude", tmpDir, "test-model", store)
	defer executor.Shutdown()

	task, err := executor.Submit("test change", "test-agent", "ben")
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}

	if task.ID == "" {
		t.Error("task ID should not be empty")
	}
	if task.Status != StatusQueued {
		t.Errorf("expected queued, got %s", task.Status)
	}
	if task.Type != "claude_session" {
		t.Errorf("expected claude_session type, got %s", task.Type)
	}

	// Wait for execution
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, ok := executor.Get(task.ID)
		if !ok {
			t.Fatal("task not found")
		}
		if got.Status == StatusCompleted || got.Status == StatusFailed {
			if got.Status != StatusCompleted {
				t.Errorf("expected completed, got %s: %s", got.Status, got.Error)
			}
			if got.Summary == "" {
				t.Error("summary should not be empty")
			}
			if got.StartedAt == nil {
				t.Error("started_at should be set")
			}
			if got.CompletedAt == nil {
				t.Error("completed_at should be set")
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Error("task did not complete in time")
}

func TestSubmitQueueFull(t *testing.T) {
	tmpDir := t.TempDir()
	scriptsDir := filepath.Join(tmpDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		t.Fatal(err)
	}
	mockWrapper := filepath.Join(scriptsDir, "claude-isolated")
	err := os.WriteFile(mockWrapper, []byte("#!/bin/sh\nsleep 60\n"), 0755)
	if err != nil {
		t.Fatal(err)
	}

	store := newMemoryStore()
	executor := NewExecutor("claude", tmpDir, "test-model", store)
	defer executor.Shutdown()

	// Fill the queue (1 running + 5 queued = 6 total, 7th should fail)
	for i := 0; i < MaxQueueSize+1; i++ {
		_, _ = executor.Submit("task "+string(rune('A'+i)), "test", "ben")
	}

	// Next one should fail
	_, err = executor.Submit("overflow task", "test", "ben")
	if err == nil {
		t.Error("expected queue full error")
	}
}

func TestRegisterAndSubmitWorker(t *testing.T) {
	store := newMemoryStore()
	executor := NewExecutor("claude", "/tmp", "test", store)
	defer executor.Shutdown()

	// Register a mock worker
	executor.RegisterWorker("test_type", &mockWorker{})

	params, _ := json.Marshal(map[string]string{"key": "value"})
	task, err := executor.SubmitTask("test_type", "test task", "agent", "user", "", params)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for execution
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, ok := executor.Get(task.ID)
		if !ok {
			t.Fatal("task not found")
		}
		if got.Status == StatusCompleted {
			if got.Summary != "mock completed" {
				t.Errorf("expected summary 'mock completed', got %q", got.Summary)
			}
			return
		}
		if got.Status == StatusFailed {
			t.Fatalf("task failed: %s", got.Error)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Error("task did not complete in time")
}

func TestSubmitUnregisteredWorker(t *testing.T) {
	store := newMemoryStore()
	executor := NewExecutor("claude", "/tmp", "test", store)
	defer executor.Shutdown()

	params, _ := json.Marshal(map[string]string{})
	task, err := executor.SubmitTask("unknown_type", "test", "agent", "user", "", params)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for failure
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, ok := executor.Get(task.ID)
		if !ok {
			t.Fatal("task not found")
		}
		if got.Status == StatusFailed {
			if !contains(got.Error, "no worker registered") {
				t.Errorf("expected 'no worker registered' error, got %q", got.Error)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Error("task did not fail in time")
}

type mockWorker struct{}

func (w *mockWorker) Execute(_ context.Context, task *Task, _ json.RawMessage) error {
	task.Summary = "mock completed"
	return nil
}

func TestGetNonexistent(t *testing.T) {
	store := newMemoryStore()
	executor := NewExecutor("claude", "/tmp", "test", store)
	defer executor.Shutdown()

	_, ok := executor.Get("nonexistent-id")
	if ok {
		t.Error("expected not found")
	}
}

func TestMemoryStoreListByAgent(t *testing.T) {
	store := newMemoryStore()

	tasks := []Task{
		{ID: "1", Type: "claude_session", Status: StatusCompleted, Description: "task 1", RequestedBy: "agent-a", CreatedAt: time.Now().Add(-2 * time.Hour)},
		{ID: "2", Type: "claude_session", Status: StatusRunning, Description: "task 2", RequestedBy: "agent-a", CreatedAt: time.Now().Add(-1 * time.Hour)},
		{ID: "3", Type: "claude_session", Status: StatusCompleted, Description: "task 3", RequestedBy: "agent-b", CreatedAt: time.Now()},
	}
	for i := range tasks {
		store.Save(nil, &tasks[i])
	}

	results, err := store.ListByAgent(nil, "agent-a", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 tasks for agent-a, got %d", len(results))
	}
	if results[0].ID != "2" {
		t.Errorf("expected task 2 first (newest), got %s", results[0].ID)
	}

	results, err = store.ListByAgent(nil, "agent-a", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 task with limit=1, got %d", len(results))
	}

	results, err = store.ListByAgent(nil, "agent-a", 50, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 tasks with high offset, got %d", len(results))
	}
}

// newMemoryStore is an in-package test helper that creates a memory store.
// For external test use, see tasktest.NewMemoryStore().
func newMemoryStore() *memoryStore {
	return &memoryStore{tasks: make(map[string]*Task)}
}

type memoryStore struct {
	mu    sync.Mutex
	tasks map[string]*Task
}

func (s *memoryStore) RunMigrations(_ context.Context) error {
	return nil
}

func (s *memoryStore) Save(_ context.Context, task *Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *task
	s.tasks[task.ID] = &copy
	return nil
}

func (s *memoryStore) Get(_ context.Context, id string) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, nil
	}
	copy := *t
	return &copy, nil
}

func (s *memoryStore) ListByAgent(_ context.Context, agentSlug string, limit, offset int) ([]Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var matching []Task
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

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Ensure the test uses the right import for unused variable check
var _ = fmt.Sprintf
