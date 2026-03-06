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
// Workers may update the task's Summary and ArtifactID fields.
type Worker interface {
	Execute(ctx context.Context, task *Task, params json.RawMessage) error
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

	task := &Task{
		ID:          taskID,
		Type:        taskType,
		Status:      StatusQueued,
		Description: description,
		RequestedBy: requestedBy,
		Username:    username,
		CreatedAt:   time.Now(),
	}

	if err := e.store.Save(context.Background(), task); err != nil {
		return nil, fmt.Errorf("persist task: %w", err)
	}

	snapshot := *task
	select {
	case e.queue <- queuedTask{task: task, params: params}:
		return &snapshot, nil
	default:
		task.Status = StatusFailed
		task.Error = "queue full"
		if err := e.store.Save(context.Background(), task); err != nil {
			slog.Error("failed to persist queue-full status", "task_id", task.ID, "error", err)
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

func (e *Executor) Shutdown() {
	close(e.done)
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
	task.Status = StatusRunning
	task.StartedAt = &now
	if err := e.store.Save(context.Background(), task); err != nil {
		slog.Error("failed to persist running status", "task_id", task.ID, "error", err)
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
			err = w.Execute(ctx, task, params)
		}
	}

	completed := time.Now()
	task.CompletedAt = &completed
	if err != nil {
		task.Status = StatusFailed
		task.Error = err.Error()
		slog.Error("task failed", "task_id", task.ID, "error", err)
	} else {
		task.Status = StatusCompleted
		slog.Info("task completed", "task_id", task.ID)
	}

	if err := e.store.Save(context.Background(), task); err != nil {
		slog.Error("failed to persist task result", "task_id", task.ID, "error", err)
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
