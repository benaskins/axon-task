package task

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	fact "github.com/benaskins/axon-fact"
)

func TestExecutor_EmitsTaskLifecycleEvents(t *testing.T) {
	store := newMemoryStore()
	es := fact.NewMemoryStore()

	executor := NewExecutor("claude", "/tmp", "test", store)
	executor.EventStore = es
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
}

func TestExecutor_EmitsTaskFailedEvent(t *testing.T) {
	store := newMemoryStore()
	es := fact.NewMemoryStore()

	executor := NewExecutor("claude", "/tmp", "test", store)
	executor.EventStore = es
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

func TestExecutor_NoEventsWithoutEventStore(t *testing.T) {
	store := newMemoryStore()

	executor := NewExecutor("claude", "/tmp", "test", store)
	executor.RegisterWorker("test_type", &mockWorker{})
	// EventStore deliberately nil
	defer executor.Shutdown()

	params, _ := json.Marshal(map[string]string{})
	_, err := executor.SubmitTask("test_type", "no events", "agent-a", "ben", "", params)
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}

	// No panic, no error — events are silently skipped
}
