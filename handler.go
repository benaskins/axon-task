package task

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/benaskins/axon"
)

type SubmitRequest struct {
	Type   string          `json:"type"`   // "claude_session" | "image_generation"
	Params json.RawMessage `json:"params"` // type-specific payload
}

type ClaudeSessionParams struct {
	Description string `json:"description"`
	RequestedBy string `json:"requested_by,omitempty"`
	Username    string `json:"username,omitempty"`
}

type SubmitResponse struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
}

type IssueAgentCertRequest struct {
	Slug     string `json:"slug"`
	Username string `json:"username"`
}

type TaskHandler struct {
	executor *Executor
	repoPath string
}

func NewTaskHandler(executor *Executor, repoPath string) *TaskHandler {
	return &TaskHandler{executor: executor, repoPath: repoPath}
}

func (h *TaskHandler) SubmitTask(w http.ResponseWriter, r *http.Request) {
	req, ok := axon.DecodeJSON[SubmitRequest](w, r)
	if !ok {
		return
	}

	clientCN, _ := r.Context().Value(ClientCNKey).(string)

	switch req.Type {
	case "claude_session":
		h.submitClaudeSession(w, req.Params, clientCN)
	case "image_generation":
		h.submitImageGeneration(w, req.Params, clientCN)
	default:
		axon.WriteError(w, http.StatusBadRequest, "type must be \"claude_session\" or \"image_generation\"")
	}
}

func (h *TaskHandler) submitClaudeSession(w http.ResponseWriter, raw json.RawMessage, clientCN string) {
	var params ClaudeSessionParams
	if err := json.Unmarshal(raw, &params); err != nil {
		axon.WriteError(w, http.StatusBadRequest, "invalid params for claude_session")
		return
	}

	if params.Description == "" {
		axon.WriteError(w, http.StatusBadRequest, "description is required")
		return
	}
	if params.RequestedBy == "" {
		axon.WriteError(w, http.StatusBadRequest, "requested_by is required")
		return
	}
	if params.Username == "" {
		axon.WriteError(w, http.StatusBadRequest, "username is required")
		return
	}
	if !axon.ValidSlug.MatchString(params.Username) {
		axon.WriteError(w, http.StatusBadRequest, "username must be lowercase alphanumeric with hyphens between words")
		return
	}

	slog.Info("task submitted",
		"type", "claude_session",
		"requested_by", params.RequestedBy,
		"username", params.Username,
		"client_cn", clientCN,
		"description_len", len(params.Description),
	)

	task, err := h.executor.Submit(params.Description, params.RequestedBy, params.Username)
	if err != nil {
		slog.Error("failed to submit task", "error", err, "requested_by", params.RequestedBy, "client_cn", clientCN)
		axon.WriteError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	axon.WriteJSON(w, http.StatusAccepted, SubmitResponse{
		TaskID: task.ID,
		Status: string(task.Status),
	})
}

func (h *TaskHandler) submitImageGeneration(w http.ResponseWriter, raw json.RawMessage, clientCN string) {
	var params ImageTaskParams
	if err := json.Unmarshal(raw, &params); err != nil {
		axon.WriteError(w, http.StatusBadRequest, "invalid params for image_generation")
		return
	}

	if params.Prompt == "" {
		axon.WriteError(w, http.StatusBadRequest, "prompt is required")
		return
	}
	if params.AgentSlug == "" {
		axon.WriteError(w, http.StatusBadRequest, "agent_slug is required")
		return
	}
	if params.ImageID == "" {
		axon.WriteError(w, http.StatusBadRequest, "image_id is required")
		return
	}

	slog.Info("image task submitted",
		"type", "image_generation",
		"agent_slug", params.AgentSlug,
		"image_id", params.ImageID,
		"prompt_len", len(params.Prompt),
		"client_cn", clientCN,
	)

	task, err := h.executor.SubmitImageTask(&params)
	if err != nil {
		slog.Error("failed to submit image task", "error", err, "agent_slug", params.AgentSlug, "client_cn", clientCN)
		axon.WriteError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	axon.WriteJSON(w, http.StatusAccepted, SubmitResponse{
		TaskID: task.ID,
		Status: string(task.Status),
	})
}

func (h *TaskHandler) GetTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		axon.WriteError(w, http.StatusBadRequest, "task id is required")
		return
	}

	task, ok := h.executor.Get(id)
	if !ok {
		axon.WriteError(w, http.StatusNotFound, "task not found")
		return
	}

	axon.WriteJSON(w, http.StatusOK, task)
}

func (h *TaskHandler) ListTasks(w http.ResponseWriter, r *http.Request) {
	agent := r.URL.Query().Get("agent")
	if agent == "" {
		axon.WriteError(w, http.StatusBadRequest, "agent query parameter is required")
		return
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	offset := 0
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	tasks, err := h.executor.Store().ListByAgent(r.Context(), agent, limit, offset)
	if err != nil {
		slog.Error("failed to list tasks", "agent", agent, "error", err)
		axon.WriteError(w, http.StatusInternalServerError, "failed to list tasks")
		return
	}

	if tasks == nil {
		tasks = []Task{}
	}

	axon.WriteJSON(w, http.StatusOK, tasks)
}

func (h *TaskHandler) IssueAgentCert(w http.ResponseWriter, r *http.Request) {
	req, ok := axon.DecodeJSON[IssueAgentCertRequest](w, r)
	if !ok {
		return
	}

	if req.Slug == "" || req.Username == "" {
		axon.WriteError(w, http.StatusBadRequest, "slug and username are required")
		return
	}

	if !axon.ValidSlug.MatchString(req.Slug) {
		axon.WriteError(w, http.StatusBadRequest, "slug must be lowercase alphanumeric with hyphens between words")
		return
	}
	if !axon.ValidSlug.MatchString(req.Username) {
		axon.WriteError(w, http.StatusBadRequest, "username must be lowercase alphanumeric with hyphens between words")
		return
	}

	clientCN, _ := r.Context().Value(ClientCNKey).(string)

	scriptPath := filepath.Join(h.repoPath, "scripts", "issue-agent-cert.sh")
	cmd := exec.Command(scriptPath, req.Slug, req.Username)
	cmd.Dir = h.repoPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error("agent cert issuance failed", "slug", req.Slug, "username", req.Username, "client_cn", clientCN, "error", err, "output", string(output))
		axon.WriteError(w, http.StatusInternalServerError, "cert issuance failed: "+err.Error())
		return
	}

	slog.Info("agent cert issued", "slug", req.Slug, "username", req.Username, "client_cn", clientCN)
	axon.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "slug": req.Slug})
}
