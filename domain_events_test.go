package task

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestExecutor_EmitsTaskLifecycleEvents(t *testing.T) {
	store := newMemoryStore()

	executor := NewExecutor("claude", "/tmp", "test", store)
	es := executor.eventStore // auto-defaulted with projectors
	executor.RegisterWorker("test_type", &mockWorker{})
	defer executor.Shutdown()

	params, _ := json.Marshal(map[string]string{"key": "value"})
	submitted, err := executor.SubmitTask("test_type", "do something", "agent-a", "ben", "", params)
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}

	// Wait for completion
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, ok := executor.Get(submitted.ID)
		if ok && got.Status == StatusCompleted {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	stream := taskStream(submitted.ID)
	events, err := es.Load(context.Background(), stream)
	if err != nil {
		t.Fatalf("failed to load events: %v", err)
	}

	if len(events) != 3 {
		t.Fatalf("expected 3 events (submitted, started, completed), got %d", len(events))
	}

	if events[0].Type != "task.submitted" {
		t.Errorf("expected task.submitted, got %s", events[0].Type)
	}
	if events[1].Type != "task.started" {
		t.Errorf("expected task.started, got %s", events[1].Type)
	}
	if events[2].Type != "task.completed" {
		t.Errorf("expected task.completed, got %s", events[2].Type)
	}

	// Verify submitted event data
	var sub TaskSubmitted
	if err := json.Unmarshal(events[0].Data, &sub); err != nil {
		t.Fatalf("failed to unmarshal task.submitted: %v", err)
	}
	if sub.Description != "do something" {
		t.Errorf("expected description 'do something', got %s", sub.Description)
	}
	if sub.Type != "test_type" {
		t.Errorf("expected type 'test_type', got %s", sub.Type)
	}

	// Verify completed event has duration
	var comp TaskCompleted
	if err := json.Unmarshal(events[2].Data, &comp); err != nil {
		t.Fatalf("failed to unmarshal task.completed: %v", err)
	}
	if comp.DurationMs < 0 {
		t.Errorf("expected non-negative duration, got %d", comp.DurationMs)
	}

	// Verify projectors updated the read model
	got, ok := executor.Get(submitted.ID)
	if !ok {
		t.Fatal("task should exist in read model")
	}
	if got.Status != StatusCompleted {
		t.Errorf("expected completed status in read model, got %s", got.Status)
	}
}

func TestExecutor_EmitsTaskFailedEvent(t *testing.T) {
	store := newMemoryStore()

	executor := NewExecutor("claude", "/tmp", "test", store)
	es := executor.eventStore
	// No worker registered — task will fail
	defer executor.Shutdown()

	params, _ := json.Marshal(map[string]string{})
	submitted, err := executor.SubmitTask("unknown_type", "will fail", "agent-a", "ben", "", params)
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}

	// Wait for failure
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, ok := executor.Get(submitted.ID)
		if ok && got.Status == StatusFailed {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	stream := taskStream(submitted.ID)
	events, err := es.Load(context.Background(), stream)
	if err != nil {
		t.Fatalf("failed to load events: %v", err)
	}

	if len(events) != 3 {
		t.Fatalf("expected 3 events (submitted, started, failed), got %d", len(events))
	}

	if events[2].Type != "task.failed" {
		t.Errorf("expected task.failed, got %s", events[2].Type)
	}

	var failed TaskFailed
	if err := json.Unmarshal(events[2].Data, &failed); err != nil {
		t.Fatalf("failed to unmarshal task.failed: %v", err)
	}
	if !containsSubstr(failed.Error, "no worker registered") {
		t.Errorf("expected 'no worker registered' error, got %s", failed.Error)
	}
}

func TestExecutor_AutoDefaultsEventStore(t *testing.T) {
	store := newMemoryStore()

	executor := NewExecutor("claude", "/tmp", "test", store)
	defer executor.Shutdown()

	if executor.eventStore == nil {
		t.Fatal("EventStore should be auto-defaulted")
	}

	// Verify projectors are wired — submit a task and check the read model
	executor.RegisterWorker("test_type", &mockWorker{})
	params, _ := json.Marshal(map[string]string{})
	submitted, err := executor.SubmitTask("test_type", "auto-default test", "agent-a", "ben", "", params)
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}

	// Task should be immediately visible in the read model (created by projector)
	got, ok := executor.Get(submitted.ID)
	if !ok {
		t.Fatal("task should exist in read model after submit")
	}
	if got.Status != StatusQueued {
		t.Errorf("expected queued status, got %s", got.Status)
	}
}
