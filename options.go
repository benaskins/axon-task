package task

import fact "github.com/benaskins/axon-fact"

// Option configures an Executor during construction.
type Option func(*Executor)

// WithEventStore overrides the default in-memory event store.
// Use this to provide a durable event store (e.g., Postgres-backed).
// The caller is responsible for registering projectors on the provided store.
func WithEventStore(es fact.EventStore) Option {
	return func(e *Executor) {
		e.eventStore = es
	}
}

// WithPromptBuilder sets a custom prompt builder for Claude session tasks.
func WithPromptBuilder(pb func(description string) string) Option {
	return func(e *Executor) {
		e.promptBuilder = pb
	}
}
