package task

import (
	"context"
	"time"

	fact "github.com/benaskins/axon-fact"
)

func taskStream(taskID string) string {
	return "task-" + taskID
}

// emit appends a domain event to the event store. Returns nil if es is nil (no-op).
func emit(ctx context.Context, es fact.EventStore, taskID string, data fact.EventTyper) error {
	if es == nil {
		return nil
	}
	stream := taskStream(taskID)
	ev, err := fact.NewEvent(stream, data)
	if err != nil {
		return err
	}
	return es.Append(ctx, stream, []fact.Event{ev})
}

// Task events

type TaskSubmitted struct {
	TaskID      string    `json:"task_id"`
	Type        string    `json:"type"`
	Description string    `json:"description"`
	RequestedBy string    `json:"requested_by,omitempty"`
	Username    string    `json:"username,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

func (e TaskSubmitted) EventType() string { return "task.submitted" }

type TaskStarted struct {
	TaskID    string    `json:"task_id"`
	StartedAt time.Time `json:"started_at"`
}

func (e TaskStarted) EventType() string { return "task.started" }

type TaskCompleted struct {
	TaskID      string    `json:"task_id"`
	Summary     string    `json:"summary,omitempty"`
	ArtifactID  string    `json:"artifact_id,omitempty"`
	CompletedAt time.Time `json:"completed_at"`
	DurationMs  int64     `json:"duration_ms"`
}

func (e TaskCompleted) EventType() string { return "task.completed" }

type TaskFailed struct {
	TaskID      string    `json:"task_id"`
	Error       string    `json:"error"`
	CompletedAt time.Time `json:"completed_at"`
	DurationMs  int64     `json:"duration_ms"`
}

func (e TaskFailed) EventType() string { return "task.failed" }
