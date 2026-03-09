package task

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	fact "github.com/benaskins/axon-fact"
	"github.com/google/uuid"
)

type TaskStatus string

const (
	StatusQueued    TaskStatus = "queued"
	StatusRunning   TaskStatus = "running"
	StatusCompleted TaskStatus = "completed"
	StatusFailed    TaskStatus = "failed"
)

const (
	MaxQueueSize = 5
	TaskTimeout  = 10 * time.Minute
)

type Task struct {
	ID          string     `json:"task_id"`
	Type        string     `json:"type"`
	Status      TaskStatus `json:"status"`
	Description string     `json:"description"`
	RequestedBy string     `json:"requested_by,omitempty"`
	Username    string     `json:"username,omitempty"`
	Summary     string     `json:"result_summary,omitempty"`
	Error       string     `json:"error,omitempty"`
	ArtifactID  string     `json:"artifact_id,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// Worker executes a task. Domain packages implement this interface.
// The params argument contains the JSON-encoded task-specific parameters.

type Worker interface {
	Execute(ctx context.Context, params json.RawMessage) error
}

type queuedTask struct {
	task   *Task
	params json.RawMessage
}

type Executor struct {
	store   Store
	workers map[string]Worker
	mu      sync.RWMutex

	// Claude session config (built-in worker)
	claudePath string
	repoPath   string
	model      string

	// PromptBuilder builds the prompt sent to Claude for a task.
	// If nil, DefaultPromptBuilder is used.
	PromptBuilder func(description string) string

	// EventStore records domain events. Optional.
	EventStore fact.EventStore

	queue chan queuedTask
	done  chan struct{}
}

func NewExecutor(claudePath, repoPath, model string, store Store) *Executor {
	e := &Executor{
		claudePath: claudePath,
		repoPath:   repoPath,
		model:      model,
		store:      store,
		workers:    make(map[string]Worker),
		queue:      make(chan queuedTask, MaxQueueSize),
		done:       make(chan struct{}),
	}

	// Default to in-memory event store with task projector.
	projector := NewTaskProjector(store, store)
	e.EventStore = fact.NewMemoryStore(fact.WithProjector(projector))

	go e.worker()
	return e
}

// RegisterWorker registers a worker for a given task type.
func (e *Executor) RegisterWorker(taskType string, w Worker) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.workers[taskType] = w
}

// Submit submits a Claude session task (convenience method).
func (e *Executor) Submit(description, requestedBy, username string) (*Task, error) {
	params := ClaudeSessionParams{
		Description: description,
		RequestedBy: requestedBy,
		Username:    username,
	}
	raw, _ := json.Marshal(params)
	return e.SubmitTask("claude_session", description, requestedBy, username, "", raw)
}

// SubmitTask submits a task of any registered type.
func (e *Executor) SubmitTask(taskType, description, requestedBy, username, taskID string, params json.RawMessage) (*Task, error) {
	if taskID == "" {
		taskID = uuid.New().String()
	}

	now := time.Now()
	task := &Task{
		ID:          taskID,
		Type:        taskType,
		Status:      StatusQueued,
		Description: description,
		RequestedBy: requestedBy,
		Username:    username,
		CreatedAt:   now,
	}

	// Emit task.submitted — projector creates the task in the read model
	if err := emit(context.Background(), e.EventStore, task.ID, TaskSubmitted{
		TaskID:      task.ID,
		Type:        task.Type,
		Description: task.Description,
		RequestedBy: task.RequestedBy,
		Username:    task.Username,
		CreatedAt:   now,
	}); err != nil {
		return nil, fmt.Errorf("emit task.submitted: %w", err)
	}

	snapshot := *task
	select {
	case e.queue <- queuedTask{task: task, params: params}:
		return &snapshot, nil
	default:
		completed := time.Now()
		if err := emit(context.Background(), e.EventStore, task.ID, TaskFailed{
			TaskID:      task.ID,
			Error:       "queue full",
			CompletedAt: completed,
		}); err != nil {
			slog.Error("failed to emit task.failed for queue-full", "task_id", task.ID, "error", err)
		}
		return nil, fmt.Errorf("task queue full (max %d)", MaxQueueSize)
	}
}

func (e *Executor) Get(id string) (*Task, bool) {
	task, err := e.store.Get(context.Background(), id)
	if err != nil {
		slog.Error("failed to get task from store", "task_id", id, "error", err)
		return nil, false
	}
	if task == nil {
		return nil, false
	}
	return task, true
}

// Store returns the executor's underlying store for direct access.
func (e *Executor) Store() Store {
	return e.store
}

// Shutdown stops the worker loop and marks any remaining queued tasks as failed.
func (e *Executor) Shutdown() {
	close(e.done)

	// Drain queued tasks that will never be processed
	for {
		select {
		case qt := <-e.queue:
			qt.task.Status = StatusFailed
			qt.task.Error = "server shutting down"
			if err := e.store.Save(context.Background(), qt.task); err != nil {
				slog.Error("failed to mark queued task as failed during shutdown", "task_id", qt.task.ID, "error", err)
			}
		default:
			return
		}
	}
}

func (e *Executor) worker() {
	for {
		select {
		case qt := <-e.queue:
			e.execute(qt.task, qt.params)
		case <-e.done:
			return
		}
	}
}

func (e *Executor) execute(task *Task, params json.RawMessage) {
	now := time.Now()
	task.StartedAt = &now

	if err := emit(context.Background(), e.EventStore, task.ID, TaskStarted{
		TaskID:    task.ID,
		StartedAt: now,
	}); err != nil {
		slog.Error("failed to emit task.started", "task_id", task.ID, "error", err)
	}

	slog.Info("executing task", "task_id", task.ID, "type", task.Type, "description", task.Description)

	ctx, cancel := context.WithTimeout(context.Background(), TaskTimeout)
	defer cancel()

	var err error
	if task.Type == "claude_session" {
		err = e.executeClaude(ctx, task, params)
	} else {
		e.mu.RLock()
		w, ok := e.workers[task.Type]
		e.mu.RUnlock()
		if !ok {
			err = fmt.Errorf("no worker registered for task type %q", task.Type)
		} else {
			err = w.Execute(ctx, params)
		}
	}

	completed := time.Now()
	durationMs := completed.Sub(*task.StartedAt).Milliseconds()

	if err != nil {
		task.Status = StatusFailed
		task.Error = err.Error()
		slog.Error("task failed", "task_id", task.ID, "error", err)

		if emitErr := emit(context.Background(), e.EventStore, task.ID, TaskFailed{
			TaskID:      task.ID,
			Error:       task.Error,
			CompletedAt: completed,
			DurationMs:  durationMs,
		}); emitErr != nil {
			slog.Error("failed to emit task.failed", "task_id", task.ID, "error", emitErr)
		}
	} else {
		task.Status = StatusCompleted
		slog.Info("task completed", "task_id", task.ID)

		if emitErr := emit(context.Background(), e.EventStore, task.ID, TaskCompleted{
			TaskID:      task.ID,
			Summary:     task.Summary,
			ArtifactID:  task.ArtifactID,
			CompletedAt: completed,
			DurationMs:  durationMs,
		}); emitErr != nil {
			slog.Error("failed to emit task.completed", "task_id", task.ID, "error", emitErr)
		}
	}
}

func (e *Executor) executeClaude(ctx context.Context, task *Task, params json.RawMessage) error {
	pb := e.PromptBuilder
	if pb == nil {
		pb = DefaultPromptBuilder
	}
	prompt := pb(task.Description)

	output, err := e.runClaude(ctx, task, prompt)
	if err != nil {
		if output != "" {
			task.Summary = truncate(output, 2000)
		}
		return err
	}
	task.Summary = truncate(output, 2000)
	return nil
}

func (e *Executor) runClaude(ctx context.Context, task *Task, prompt string) (string, error) {
	wrapperPath := filepath.Join(e.repoPath, "scripts", "claude-isolated")
	taskName := fmt.Sprintf("agent-%s", sanitizeName(task.RequestedBy))

	args := []string{
		"--name", taskName,
		"--print",
		"--permission-mode", "bypassPermissions",
		"--allowedTools",
		"Read",
		"Glob",
		"Grep",
		"Edit(services/chat/**)",
		"Write(services/chat/**)",
		"Write(.commit-message)",
		"Bash(just test chat)",
		"Bash(just chat-test-one *)",
		"Bash(git add *)",
		"Bash(git status)",
		"Bash(git diff *)",
	}
	if e.model != "" {
		args = append(args, "--model", e.model)
	}
	args = append(args, "-p", prompt)

	cmd := exec.CommandContext(ctx, wrapperPath, args...)
	cmd.Dir = e.repoPath
	cmd.Env = append(filterEnv(os.Environ()),
		fmt.Sprintf("AURELIA_TASK_ID=%s", task.ID),
		fmt.Sprintf("AURELIA_AGENT_SLUG=%s", task.RequestedBy),
		fmt.Sprintf("AURELIA_AGENT_USERNAME=%s", task.Username),
		fmt.Sprintf("CLAUDE_PATH=%s", e.claudePath),
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		errMsg := stderr.String()
		if errMsg == "" {
			errMsg = err.Error()
		}
		return stdout.String(), fmt.Errorf("claude exited with error: %s", errMsg)
	}

	return stdout.String(), nil
}

func filterEnv(env []string) []string {
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		key := e
		if idx := strings.IndexByte(e, '='); idx >= 0 {
			key = e[:idx]
		}
		switch key {
		case "CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT":
			continue
		default:
			filtered = append(filtered, e)
		}
	}
	return filtered
}

// DefaultPromptBuilder is the generic prompt used when no custom PromptBuilder is set.
func DefaultPromptBuilder(description string) string {
	return fmt.Sprintf(`You are modifying code on behalf of an AI agent.

Steps:
1. Read any project documentation for context.
2. Make the requested change.
3. Run tests to verify. Fix any failures.
4. Add tests for new behavior.
5. Stage all changed files with 'git add'.
6. Write a conventional commit message to .commit-message (the file, not a command).

Do NOT run any VCS commit/push commands.

Change request: %s`, description)
}

func sanitizeName(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	result := b.String()
	if result == "" {
		return "unknown"
	}
	return result
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "...(truncated)"
}
