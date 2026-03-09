package task

import (
	"context"
	"encoding/json"

	fact "github.com/benaskins/axon-fact"
)

// DefaultProjectors returns the standard set of projectors for axon-task.
func DefaultProjectors(reader ReadStore, writer ReadModelWriter) []fact.Projector {
	return []fact.Projector{
		NewTaskProjector(reader, writer),
	}
}

// TaskProjector projects task lifecycle events into the read model.
type TaskProjector struct {
	reader ReadStore
	writer ReadModelWriter
}

func NewTaskProjector(reader ReadStore, writer ReadModelWriter) *TaskProjector {
	return &TaskProjector{reader: reader, writer: writer}
}

func (p *TaskProjector) Handle(ctx context.Context, e fact.Event) error {
	switch e.Type {
	case "task.submitted":
		var data TaskSubmitted
		if err := json.Unmarshal(e.Data, &data); err != nil {
			return err
		}
		return p.writer.Save(ctx, &Task{
			ID:          data.TaskID,
			Type:        data.Type,
			Status:      StatusQueued,
			Description: data.Description,
			RequestedBy: data.RequestedBy,
			Username:    data.Username,
			CreatedAt:   data.CreatedAt,
		})

	case "task.started":
		var data TaskStarted
		if err := json.Unmarshal(e.Data, &data); err != nil {
			return err
		}
		task, err := p.reader.Get(ctx, data.TaskID)
		if err != nil || task == nil {
			return err
		}
		task.Status = StatusRunning
		task.StartedAt = &data.StartedAt
		return p.writer.Save(ctx, task)

	case "task.completed":
		var data TaskCompleted
		if err := json.Unmarshal(e.Data, &data); err != nil {
			return err
		}
		task, err := p.reader.Get(ctx, data.TaskID)
		if err != nil || task == nil {
			return err
		}
		task.Status = StatusCompleted
		task.Summary = data.Summary
		task.ArtifactID = data.ArtifactID
		task.CompletedAt = &data.CompletedAt
		return p.writer.Save(ctx, task)

	case "task.failed":
		var data TaskFailed
		if err := json.Unmarshal(e.Data, &data); err != nil {
			return err
		}
		task, err := p.reader.Get(ctx, data.TaskID)
		if err != nil || task == nil {
			return err
		}
		task.Status = StatusFailed
		task.Error = data.Error
		task.CompletedAt = &data.CompletedAt
		return p.writer.Save(ctx, task)
	}
	return nil
}
