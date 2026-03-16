# axon-task

Generic asynchronous task runner with pluggable workers.

## Build & Test

```bash
go test ./...
go vet ./...
```

## Key Files

- `executor.go` — task execution engine
- `domain_events.go` — task lifecycle events
- `doc.go` — package documentation
