package tasks

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

func base64Decode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

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

// ImageTaskParams holds the parameters specific to image generation tasks.
// Sent as part of the task submission, not persisted.
type ImageTaskParams struct {
	Prompt         string `json:"prompt"`
	ReferenceImage string `json:"reference_image,omitempty"` // base64-encoded PNG
	AgentSlug      string `json:"agent_slug"`
	UserID         string `json:"user_id"`
	ConversationID string `json:"conversation_id,omitempty"`
	ImageID        string `json:"image_id"` // pre-assigned by chat service
	Private        bool   `json:"private,omitempty"`
}

// imageTask pairs a task with its image-specific parameters (not persisted).
type imageTask struct {
	task   *Task
	params *ImageTaskParams
}

type Executor struct {
	claudePath string
	repoPath   string
	model      string
	store      Store

	// Image generation dependencies (nil if not configured)
	imageGen        ImageGenerator
	privateImageGen ImageGenerator
	imageStore      *ImageStore

	queue      chan *Task
	imageQueue chan imageTask
	done       chan struct{}
}

func NewExecutor(claudePath, repoPath, model string, store Store) *Executor {
	e := &Executor{
		claudePath: claudePath,
		repoPath:   repoPath,
		model:      model,
		store:      store,
		queue:      make(chan *Task, MaxQueueSize),
		imageQueue: make(chan imageTask, MaxQueueSize),
		done:       make(chan struct{}),
	}
	go e.worker()
	go e.imageWorker()
	return e
}

// SetImageGen configures image generation support.
func (e *Executor) SetImageGen(gen ImageGenerator, imgStore *ImageStore) {
	e.imageGen = gen
	e.imageStore = imgStore
}

func (e *Executor) SetPrivateImageGen(gen ImageGenerator) {
	e.privateImageGen = gen
}

func (e *Executor) Submit(description, requestedBy, username string) (*Task, error) {
	task := &Task{
		ID:          uuid.New().String(),
		Type:        "claude_session",
		Status:      StatusQueued,
		Description: description,
		RequestedBy: requestedBy,
		Username:    username,
		CreatedAt:   time.Now(),
	}

	if err := e.store.Save(context.Background(), task); err != nil {
		return nil, fmt.Errorf("persist task: %w", err)
	}

	select {
	case e.queue <- task:
		return task, nil
	default:
		task.Status = StatusFailed
		task.Error = "queue full"
		if err := e.store.Save(context.Background(), task); err != nil {
			slog.Error("failed to persist queue-full status", "task_id", task.ID, "error", err)
		}
		return nil, fmt.Errorf("task queue full (max %d)", MaxQueueSize)
	}
}

// SubmitImageTask submits an image generation task.
func (e *Executor) SubmitImageTask(params *ImageTaskParams) (*Task, error) {
	if e.imageGen == nil {
		return nil, fmt.Errorf("image generation not configured")
	}

	task := &Task{
		ID:          params.ImageID,
		Type:        "image_generation",
		Status:      StatusQueued,
		Description: params.Prompt,
		RequestedBy: params.AgentSlug,
		Username:    params.UserID,
		CreatedAt:   time.Now(),
	}

	if err := e.store.Save(context.Background(), task); err != nil {
		return nil, fmt.Errorf("persist task: %w", err)
	}

	select {
	case e.imageQueue <- imageTask{task: task, params: params}:
		return task, nil
	default:
		task.Status = StatusFailed
		task.Error = "image queue full"
		if err := e.store.Save(context.Background(), task); err != nil {
			slog.Error("failed to persist queue-full status", "task_id", task.ID, "error", err)
		}
		return nil, fmt.Errorf("image task queue full (max %d)", MaxQueueSize)
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

// Store returns the executor's underlying store for direct access (e.g. ListByAgent).
func (e *Executor) Store() Store {
	return e.store
}

func (e *Executor) Shutdown() {
	close(e.done)
}

func (e *Executor) worker() {
	for {
		select {
		case task := <-e.queue:
			e.execute(task)
		case <-e.done:
			return
		}
	}
}

func (e *Executor) imageWorker() {
	for {
		select {
		case it := <-e.imageQueue:
			e.executeImage(it.task, it.params)
		case <-e.done:
			return
		}
	}
}

func (e *Executor) execute(task *Task) {
	now := time.Now()
	task.Status = StatusRunning
	task.StartedAt = &now
	if err := e.store.Save(context.Background(), task); err != nil {
		slog.Error("failed to persist running status", "task_id", task.ID, "error", err)
	}

	slog.Info("executing task", "task_id", task.ID, "description", task.Description)

	prompt := buildPrompt(task.Description)

	ctx, cancel := context.WithTimeout(context.Background(), TaskTimeout)
	defer cancel()

	output, err := e.runClaude(ctx, task, prompt)
	completed := time.Now()

	task.CompletedAt = &completed
	if err != nil {
		task.Status = StatusFailed
		task.Error = err.Error()
		if output != "" {
			task.Summary = truncate(output, 2000)
		}
		slog.Error("task failed", "task_id", task.ID, "error", err)
	} else {
		task.Status = StatusCompleted
		task.Summary = truncate(output, 2000)
		slog.Info("task completed", "task_id", task.ID)
	}

	if err := e.store.Save(context.Background(), task); err != nil {
		slog.Error("failed to persist task result", "task_id", task.ID, "error", err)
	}
}

const imageTimeout = 5 * time.Minute

func (e *Executor) executeImage(task *Task, params *ImageTaskParams) {
	now := time.Now()
	task.Status = StatusRunning
	task.StartedAt = &now
	if err := e.store.Save(context.Background(), task); err != nil {
		slog.Error("failed to persist running status", "task_id", task.ID, "error", err)
	}

	slog.Info("generating image", "task_id", task.ID, "prompt_len", len(params.Prompt), "has_reference", params.ReferenceImage != "")

	ctx, cancel := context.WithTimeout(context.Background(), imageTimeout)
	defer cancel()

	// Decode reference image if provided
	var refImage []byte
	if params.ReferenceImage != "" {
		decoded, err := base64Decode(params.ReferenceImage)
		if err != nil {
			slog.Warn("failed to decode reference image, continuing without", "error", err)
		} else {
			refImage = decoded
		}
	}

	gen := e.imageGen
	if params.Private && e.privateImageGen != nil {
		gen = e.privateImageGen
	}

	imageData, err := gen.GenerateImage(ctx, params.Prompt, refImage)
	completed := time.Now()
	task.CompletedAt = &completed

	if err != nil {
		task.Status = StatusFailed
		task.Error = err.Error()
		slog.Error("image generation failed", "task_id", task.ID, "error", err)
	} else {
		if err := e.imageStore.SaveWithID(params.ImageID, imageData); err != nil {
			task.Status = StatusFailed
			task.Error = fmt.Sprintf("failed to save image: %v", err)
			slog.Error("failed to save image", "task_id", task.ID, "error", err)
		} else {
			task.Status = StatusCompleted
			task.ArtifactID = params.ImageID
			task.Summary = fmt.Sprintf("Generated image: %s", params.ImageID)
			slog.Info("image generated", "task_id", task.ID, "image_id", params.ImageID)
		}
	}

	if err := e.store.Save(context.Background(), task); err != nil {
		slog.Error("failed to persist image task result", "task_id", task.ID, "error", err)
	}
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

func buildPrompt(description string) string {
	return fmt.Sprintf(`You are modifying the Aurelia chat service on behalf of an AI agent.

Scope — you may ONLY modify files in:
- services/chat/ (Go backend + SvelteKit frontend)

Agent skills architecture — if the change involves adding or modifying an agent skill,
you must integrate it in ALL of these places:
1. services/chat/tools.go — tool definition (buildXxxTool function) + add to AvailableSkills + case in buildToolsForAgent
2. services/chat/handler.go — tool execution case in streamChat
3. services/chat/agents.go — system prompt guidance in buildSystemPrompt
4. services/chat/tool_router.go — add to the few-shot examples in the prompt

Steps:
1. Read the project CLAUDE.md for context.
2. Make the requested change.
3. Run 'just test chat' to verify tests pass. Fix any failures.
4. Add tests for new behavior.
5. Stage all changed files with 'git add'.
6. Write a conventional commit message to .commit-message (the file, not a command).
   Just the message text. The wrapper script handles committing for you.

Do NOT run any VCS commit/push commands or ship services.
Do NOT modify infrastructure, other services, or system config.

Change request: %s`, description)
}

func sanitizeName(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	// Keep only alphanumeric and hyphens
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
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "...(truncated)"
}
