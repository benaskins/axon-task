package task

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestHandler(t *testing.T) (*TaskHandler, *memoryStore) {
	t.Helper()
	store := newMemoryStore()
	executor := NewExecutor("claude", "/tmp", "test", store)
	t.Cleanup(executor.Shutdown)
	return NewTaskHandler(executor, "/tmp"), store
}

// --- GetTask tests ---

func TestGetTask_Valid(t *testing.T) {
	h, store := newTestHandler(t)

	task := &Task{ID: "abc-123", Type: "test", Status: StatusCompleted, CreatedAt: time.Now()}
	store.Save(context.Background(), task)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /tasks/{id}", h.GetTask)

	req := httptest.NewRequest("GET", "/tasks/abc-123", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var got Task
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "abc-123" {
		t.Errorf("expected task ID abc-123, got %s", got.ID)
	}
}

func TestGetTask_MissingID(t *testing.T) {
	h, _ := newTestHandler(t)

	// Calling the handler directly without a path value for {id}
	req := httptest.NewRequest("GET", "/tasks/", nil)
	rec := httptest.NewRecorder()
	h.GetTask(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetTask_NotFound(t *testing.T) {
	h, _ := newTestHandler(t)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /tasks/{id}", h.GetTask)

	req := httptest.NewRequest("GET", "/tasks/nonexistent", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- ListTasks tests ---

func TestListTasks_ReturnsTasks(t *testing.T) {
	h, store := newTestHandler(t)

	for i := 0; i < 3; i++ {
		task := &Task{
			ID:          "task-" + string(rune('a'+i)),
			Type:        "test",
			Status:      StatusCompleted,
			RequestedBy: "my-agent",
			CreatedAt:   time.Now().Add(time.Duration(-i) * time.Hour),
		}
		store.Save(context.Background(), task)
	}

	req := httptest.NewRequest("GET", "/tasks?agent=my-agent", nil)
	rec := httptest.NewRecorder()
	h.ListTasks(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var tasks []Task
	if err := json.NewDecoder(rec.Body).Decode(&tasks); err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}
}

func TestListTasks_MissingAgent(t *testing.T) {
	h, _ := newTestHandler(t)

	req := httptest.NewRequest("GET", "/tasks", nil)
	rec := httptest.NewRecorder()
	h.ListTasks(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListTasks_DefaultLimitAndOffset(t *testing.T) {
	h, store := newTestHandler(t)

	// Create 60 tasks — default limit is 50
	for i := 0; i < 60; i++ {
		task := &Task{
			ID:          "task-" + string(rune(i)),
			Type:        "test",
			Status:      StatusCompleted,
			RequestedBy: "agent-x",
			CreatedAt:   time.Now().Add(time.Duration(-i) * time.Minute),
		}
		store.Save(context.Background(), task)
	}

	req := httptest.NewRequest("GET", "/tasks?agent=agent-x", nil)
	rec := httptest.NewRecorder()
	h.ListTasks(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var tasks []Task
	json.NewDecoder(rec.Body).Decode(&tasks)
	if len(tasks) != 50 {
		t.Errorf("expected default limit of 50, got %d", len(tasks))
	}
}

func TestListTasks_ClampsLimitTo100(t *testing.T) {
	h, _ := newTestHandler(t)

	// Request limit=200 — should be ignored (stays at default 50)
	req := httptest.NewRequest("GET", "/tasks?agent=test&limit=200", nil)
	rec := httptest.NewRecorder()
	h.ListTasks(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestListTasks_CustomLimitAndOffset(t *testing.T) {
	h, store := newTestHandler(t)

	for i := 0; i < 10; i++ {
		task := &Task{
			ID:          "t-" + string(rune('a'+i)),
			Type:        "test",
			Status:      StatusCompleted,
			RequestedBy: "agent-y",
			CreatedAt:   time.Now().Add(time.Duration(-i) * time.Minute),
		}
		store.Save(context.Background(), task)
	}

	req := httptest.NewRequest("GET", "/tasks?agent=agent-y&limit=3&offset=2", nil)
	rec := httptest.NewRecorder()
	h.ListTasks(rec, req)

	var tasks []Task
	json.NewDecoder(rec.Body).Decode(&tasks)
	if len(tasks) != 3 {
		t.Errorf("expected 3 tasks with limit=3, got %d", len(tasks))
	}
}

func TestListTasks_EmptyResult(t *testing.T) {
	h, _ := newTestHandler(t)

	req := httptest.NewRequest("GET", "/tasks?agent=nobody", nil)
	rec := httptest.NewRecorder()
	h.ListTasks(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var tasks []Task
	json.NewDecoder(rec.Body).Decode(&tasks)
	if len(tasks) != 0 {
		t.Errorf("expected empty list, got %d", len(tasks))
	}
}

// --- SubmitTask tests ---

func TestSubmitTask_GenericWithRegisteredWorker(t *testing.T) {
	store := newMemoryStore()
	executor := NewExecutor("claude", "/tmp", "test", store)
	t.Cleanup(executor.Shutdown)
	executor.RegisterWorker("image_gen", &mockWorker{})

	h := NewTaskHandler(executor, "/tmp")

	body, _ := json.Marshal(SubmitRequest{
		Type:   "image_gen",
		Params: json.RawMessage(`{"prompt":"a cat"}`),
	})
	req := httptest.NewRequest("POST", "/tasks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.SubmitTask(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp SubmitResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.TaskID == "" {
		t.Error("expected non-empty task ID")
	}
	if resp.Status != "queued" {
		t.Errorf("expected queued status, got %s", resp.Status)
	}
}

func TestSubmitTask_UnknownType(t *testing.T) {
	h, _ := newTestHandler(t)

	body, _ := json.Marshal(SubmitRequest{
		Type:   "nonexistent_type",
		Params: json.RawMessage(`{}`),
	})
	req := httptest.NewRequest("POST", "/tasks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.SubmitTask(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["error"] == "" {
		t.Error("expected error message")
	}
}

// --- submitClaudeSession tests ---

func TestSubmitClaudeSession_Valid(t *testing.T) {
	h, _ := newTestHandler(t)

	body, _ := json.Marshal(SubmitRequest{
		Type: "claude_session",
		Params: json.RawMessage(`{
			"description": "fix the bug",
			"requested_by": "test-agent",
			"username": "ben"
		}`),
	})
	req := httptest.NewRequest("POST", "/tasks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.SubmitTask(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp SubmitResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.TaskID == "" {
		t.Error("expected non-empty task ID")
	}
}

func TestSubmitClaudeSession_MissingDescription(t *testing.T) {
	h, _ := newTestHandler(t)

	body, _ := json.Marshal(SubmitRequest{
		Type: "claude_session",
		Params: json.RawMessage(`{
			"requested_by": "test-agent",
			"username": "ben"
		}`),
	})
	req := httptest.NewRequest("POST", "/tasks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.SubmitTask(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSubmitClaudeSession_MissingRequestedBy(t *testing.T) {
	h, _ := newTestHandler(t)

	body, _ := json.Marshal(SubmitRequest{
		Type: "claude_session",
		Params: json.RawMessage(`{
			"description": "fix the bug",
			"username": "ben"
		}`),
	})
	req := httptest.NewRequest("POST", "/tasks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.SubmitTask(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSubmitClaudeSession_MissingUsername(t *testing.T) {
	h, _ := newTestHandler(t)

	body, _ := json.Marshal(SubmitRequest{
		Type: "claude_session",
		Params: json.RawMessage(`{
			"description": "fix the bug",
			"requested_by": "test-agent"
		}`),
	})
	req := httptest.NewRequest("POST", "/tasks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.SubmitTask(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSubmitClaudeSession_InvalidUsername(t *testing.T) {
	h, _ := newTestHandler(t)

	body, _ := json.Marshal(SubmitRequest{
		Type: "claude_session",
		Params: json.RawMessage(`{
			"description": "fix the bug",
			"requested_by": "test-agent",
			"username": "INVALID_USER"
		}`),
	})
	req := httptest.NewRequest("POST", "/tasks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.SubmitTask(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	if !contains(resp["error"], "username") {
		t.Errorf("expected username validation error, got %q", resp["error"])
	}
}

func TestSubmitClaudeSession_InvalidParams(t *testing.T) {
	h, _ := newTestHandler(t)

	body, _ := json.Marshal(SubmitRequest{
		Type:   "claude_session",
		Params: json.RawMessage(`not json`),
	})
	req := httptest.NewRequest("POST", "/tasks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.SubmitTask(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
