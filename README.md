# axon-task

> Domain package · Part of the [lamina](https://github.com/benaskins/lamina-mono) workspace

Generic asynchronous task runner with pluggable workers. Tasks are submitted via HTTP, queued in memory, and dispatched to registered `Worker` implementations. Domain packages provide workers for specific task types (e.g. image generation, code sessions). Persistence is abstracted behind a `Store` interface with a Postgres implementation included.

## Getting started

```
go get github.com/benaskins/axon-task@latest
```

Requires Go 1.24+.

axon-task is a domain package — it provides types and HTTP handlers that you assemble in your own composition root. See [`example/main.go`](example/main.go) for a minimal wiring example.

```go
// Define a worker for your task type.
type ResizeWorker struct{}

func (w *ResizeWorker) Execute(ctx context.Context, params json.RawMessage) error {
    var p struct{ URL string; Width, Height int }
    if err := json.Unmarshal(params, &p); err != nil {
        return err
    }
    // ... perform resize ...
    return nil
}

// Wire up the executor with a store and register workers.
store := task.NewPostgresStore(db)
executor := task.NewExecutor("claude", "/srv/app", "sonnet", store)
executor.RegisterWorker("resize", &ResizeWorker{})

handler := task.NewTaskHandler(executor, "")

mux := http.NewServeMux()
mux.HandleFunc("POST /api/tasks", handler.SubmitTask)
mux.HandleFunc("GET /api/tasks/{id}", handler.GetTask)
mux.HandleFunc("GET /api/tasks", handler.ListTasks)
log.Fatal(http.ListenAndServe(":8090", mux))
```

## Key types

- **`Worker`** — interface for task execution (`Execute(ctx, params json.RawMessage) error`)
- **`Task`** — task definition with status tracking (queued, running, completed, failed)
- **`Executor`** — queues tasks and dispatches to registered workers
- **`Store`** — persistence interface for task state (`Save`, `Get`, `ListByAgent`)
- **`TaskHandler`** — HTTP handlers for task submission, retrieval, and listing
- **`tasktest.MemoryStore`** — in-memory store for tests

## License

MIT
