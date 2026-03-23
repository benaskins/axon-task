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
- **`Executor`** — queues tasks and dispatches to registered workers; configured via `Option` functions
- **`ReadStore`** — read-only interface (`Get`, `ListByAgent`)
- **`ReadModelWriter`** — write interface for projectors (`Save`)
- **`Store`** — combines `ReadStore` and `ReadModelWriter`; `PostgresStore` is the included implementation
- **`TaskHandler`** — HTTP handlers for task submission, retrieval, listing, and agent cert issuance
- **`TaskProjector`** — projects task lifecycle events (via axon-fact) into the read model
- **`tasktest.MemoryStore`** — in-memory store for tests

## Event sourcing

Task lifecycle is modelled as domain events (`TaskSubmitted`, `TaskStarted`, `TaskCompleted`, `TaskFailed`) using axon-fact. Supply a durable `fact.EventStore` via `WithEventStore` and wire up `DefaultProjectors` to project events into the read model.

## TLS / mTLS

`LoadTLSConfig` loads a CA certificate for client verification. The `RequireClientCert` middleware rejects requests without a verified client certificate and adds the client CN to the request context.

## Options

`NewExecutor` accepts functional options:

- `WithEventStore(es)` — use a durable event store instead of the default in-memory one
- `WithPromptBuilder(pb)` — customise the prompt used for Claude session tasks

## License

MIT
