---
module: github.com/benaskins/axon-task
kind: service
---

# axon-task

Generic asynchronous task runner with pluggable workers.

## Build & Test

```bash
go test ./...
go vet ./...
```

## Key Files

- `executor.go`  - task execution engine with worker dispatch
- `handler.go`  - HTTP handlers (submit, get, list, agent cert issuance)
- `store.go`  - ReadStore, ReadModelWriter, and Store interfaces
- `postgres_store.go`  - PostgreSQL Store implementation
- `domain_events.go`  - task lifecycle events (TaskSubmitted, TaskStarted, TaskCompleted, TaskFailed)
- `projectors.go`  - event sourcing projectors (TaskProjector, DefaultProjectors)
- `options.go`  - functional options for Executor (WithEventStore, WithPromptBuilder)
- `tls.go`  - TLS/mTLS support (LoadTLSConfig, RequireClientCert)
- `migrations.go`  - embedded SQL migrations
- `tasktest/store.go`  - in-memory store for tests
- `doc.go`  - package documentation
- `CLAUDE.md`  - Claude-specific documentation