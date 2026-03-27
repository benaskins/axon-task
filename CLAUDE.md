@AGENTS.md

## Conventions
- `Worker` interface is the sole extension point  - domain packages implement workers, not this repo
- Functional options pattern: `NewExecutor(store, opts...)` with `WithEventStore`, `WithPromptBuilder`, etc.
- Event-sourced task lifecycle: TaskSubmitted -> TaskStarted -> TaskCompleted/TaskFailed
- `tasktest/store.go` provides in-memory store for tests  - use it, don't mock Store manually
- Projectors materialise read models from events  - use `DefaultProjectors` unless you need custom behaviour

## Constraints
- This is a generic runner  - domain-specific workers belong in their own packages (e.g., axon-lens provides ImageWorker)
- Depends on axon and axon-fact  - do not add dependencies on other axon-* service packages
- Do not add task-type-specific logic here; the executor dispatches to registered workers by type string

## Testing
- `go test ./...` runs all tests
- `go vet ./...` for lint
- Use `tasktest.NewStore()` for test fixtures
