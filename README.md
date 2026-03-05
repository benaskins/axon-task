# axon-task

An asynchronous task runner that executes Claude Code sessions and image generation jobs.

Tasks are submitted via HTTP, queued, and executed with progress tracking.

## Install

```
go get github.com/benaskins/axon-task@latest
```

Requires Go 1.24+.

## Usage

```go
store, _ := task.NewPostgresStore(databaseURL)
executor := task.NewExecutor(claudePath, repoPath, model, store)
handler := task.NewTaskHandler(executor, repoPath)

mux.HandleFunc("POST /api/tasks", handler.SubmitTask)
mux.HandleFunc("GET /api/tasks/{id}", handler.GetTask)
mux.HandleFunc("GET /api/tasks", handler.ListTasks)
```

### Key types

- `Task` — task definition with status tracking
- `Executor` — queues and executes tasks (Claude sessions, image generation)
- `Store` — persistence interface for task state
- `PostgresStore` — PostgreSQL implementation
- `TaskHandler` — HTTP handler for task submission and status
- `ComfyUIClient` — ComfyUI image generation client
- `ImageStore` — local image file storage with thumbnails

## License

MIT — see [LICENSE](LICENSE).
