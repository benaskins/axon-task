//go:build ignore

// Example showing how to wire up axon-task with a custom worker.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	task "github.com/benaskins/axon-task"
	"github.com/benaskins/axon-task/tasktest"
)

// ResizeParams defines the input for the resize worker.
type ResizeParams struct {
	ImageURL string `json:"image_url"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

// ResizeWorker implements task.Worker for image resizing.
type ResizeWorker struct{}

func (w *ResizeWorker) Execute(ctx context.Context, params json.RawMessage) error {
	var p ResizeParams
	if err := json.Unmarshal(params, &p); err != nil {
		return fmt.Errorf("decode params: %w", err)
	}
	log.Printf("resizing %s to %dx%d", p.ImageURL, p.Width, p.Height)
	// ... perform the resize ...
	return nil
}

func main() {
	store := tasktest.NewMemoryStore()
	executor := task.NewExecutor("", "", "", store)
	executor.RegisterWorker("resize", &ResizeWorker{})

	handler := task.NewTaskHandler(executor, "")

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/tasks", handler.SubmitTask)
	mux.HandleFunc("GET /api/tasks/{id}", handler.GetTask)
	mux.HandleFunc("GET /api/tasks", handler.ListTasks)

	log.Println("listening on :8090")
	log.Fatal(http.ListenAndServe(":8090", mux))
}
