package task

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"

	fact "github.com/benaskins/axon-fact"
)

// EventTyper is implemented by all domain event structs.
type EventTyper interface {
	EventType() string
}

// newEvent creates a fact.Event from a domain event struct.
func newEvent(stream string, data EventTyper) (fact.Event, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return fact.Event{}, err
	}
	return fact.Event{
		ID:     generateEventID(),
		Stream: stream,
		Type:   data.EventType(),
		Data:   raw,
	}, nil
}

func generateEventID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func taskStream(taskID string) string {
	return "task-" + taskID
}

// emit appends a domain event to the event store. Returns nil if es is nil (no-op).
func emit(ctx context.Context, es fact.EventStore, taskID string, data EventTyper) error {
	if es == nil {
		return nil
	}
	stream := taskStream(taskID)
	ev, err := newEvent(stream, data)
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
