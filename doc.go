// Package task provides a generic asynchronous task runner. Tasks are
// submitted via HTTP, queued, and executed by registered workers.
// Domain packages provide Worker implementations for specific task types.
//
// Class: domain
// UseWhen: Async/background work.
package task
