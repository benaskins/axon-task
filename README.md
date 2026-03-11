# axon-task

> Domain package · Part of the [lamina](https://github.com/benaskins/lamina-mono) workspace

Generic asynchronous task runner with pluggable workers. Tasks are submitted via HTTP, queued in memory, and dispatched to registered `Worker` implementations. Domain packages provide workers for specific task types (e.g. image generation, code sessions). Persistence is abstracted behind a `Store` interface with a Postgres implementation included.

## Getting started

```
go get github.com/benaskins/axon-task@latest
```

Requires Go 1.24+.

axon-task is a domain package — it provides types and HTTP handlers that you assemble in your own composition root. See [`example/main.go`](example/main.go) for a minimal wiring example.

## Key types

- **`Worker`** — interface for task execution (`Execute(ctx, params json.RawMessage) error`)
- **`Task`** — task definition with status tracking (queued, running, completed, failed)
- **`Executor`** — queues tasks and dispatches to registered workers
- **`Store`** — persistence interface for task state (`Save`, `Get`, `ListByAgent`)
- **`TaskHandler`** — HTTP handlers for task submission, retrieval, and listing
- **`tasktest.MemoryStore`** — in-memory store for tests

## License

MIT
