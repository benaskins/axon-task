# axon-task

A generic asynchronous task runner with pluggable workers. Part of [lamina](https://github.com/benaskins/lamina) — each axon package can be used independently.

Tasks are submitted via HTTP, queued, and executed by registered workers. Domain packages provide `Worker` implementations for specific task types.

## Install

```
go get github.com/benaskins/axon-task@latest
```

Requires Go 1.24+.

## Usage

```go
store, _ := task.NewPostgresStore(databaseURL)
executor := task.NewExecutor(claudePath, repoPath, model, store)

// Register domain workers
executor.RegisterWorker("image_generation", imageWorker)

handler := task.NewTaskHandler(executor, repoPath)

mux.HandleFunc("POST /api/tasks", handler.SubmitTask)
mux.HandleFunc("GET /api/tasks/{id}", handler.GetTask)
mux.HandleFunc("GET /api/tasks", handler.ListTasks)
```

### Key types

- `Worker` — interface for task execution (`Execute(ctx, params)`)
- `Task` — task definition with status tracking
- `Executor` — queues tasks and dispatches to registered workers
- `Store` — persistence interface for task state
- `TaskHandler` — HTTP handler for task submission and status

## License

MIT — see [LICENSE](LICENSE).
