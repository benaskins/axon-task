package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"time"
)

// ImageGenerator abstracts image generation for testing.
type ImageGenerator interface {
	GenerateImage(ctx context.Context, prompt string, referenceImage []byte) ([]byte, error)
}

// ComfyUIClient talks to the ComfyUI REST API.
type ComfyUIClient struct {
	baseURL          string
	httpClient       *http.Client
	workflow         map[string]any
	fallbackWorkflow map[string]any
}

// NewComfyUIClient creates a client pointing at a ComfyUI instance.
func NewComfyUIClient(baseURL string) *ComfyUIClient {
	return &ComfyUIClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 5 * time.Minute},
	}
}

// SetWorkflow sets the ComfyUI workflow template to use for generation.
func (c *ComfyUIClient) SetWorkflow(workflow map[string]any) {
	c.workflow = workflow
}

// SetFallbackWorkflow sets a simpler workflow to use when no reference image is available.
func (c *ComfyUIClient) SetFallbackWorkflow(workflow map[string]any) {
	c.fallbackWorkflow = workflow
}

// UploadImage uploads an image to ComfyUI's input directory via multipart form.
func (c *ComfyUIClient) UploadImage(imageData []byte, filename string) (string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("image", filename)
	if err != nil {
		return "", fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(imageData); err != nil {
		return "", fmt.Errorf("write image data: %w", err)
	}
	writer.Close()

	resp, err := c.httpClient.Post(c.baseURL+"/upload/image", writer.FormDataContentType(), &buf)
	if err != nil {
		return "", fmt.Errorf("upload image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("comfyui upload returned %d: %s", resp.StatusCode, string(b))
	}

	var result struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode upload response: %w", err)
	}
	return result.Name, nil
}

// QueuePrompt sends a workflow to ComfyUI and returns the prompt ID.
func (c *ComfyUIClient) QueuePrompt(prompt string, referenceImageFilename string) (string, error) {
	workflow := c.buildWorkflow(prompt, referenceImageFilename)

	body, err := json.Marshal(map[string]any{"prompt": workflow})
	if err != nil {
		return "", fmt.Errorf("marshal workflow: %w", err)
	}

	resp, err := c.httpClient.Post(c.baseURL+"/prompt", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("queue prompt: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("comfyui returned %d: %s", resp.StatusCode, string(b))
	}

	var result struct {
		PromptID string `json:"prompt_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	return result.PromptID, nil
}

// GetOutputFilename checks the history for a completed prompt and returns the output filename.
func (c *ComfyUIClient) GetOutputFilename(promptID string) (string, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/history/" + promptID)
	if err != nil {
		return "", fmt.Errorf("get history: %w", err)
	}
	defer resp.Body.Close()

	var history map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&history); err != nil {
		return "", fmt.Errorf("decode history: %w", err)
	}

	entry, ok := history[promptID]
	if !ok {
		return "", fmt.Errorf("prompt %s not ready", promptID)
	}

	entryMap, ok := entry.(map[string]any)
	if !ok {
		return "", fmt.Errorf("unexpected history format")
	}

	outputs, ok := entryMap["outputs"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("no outputs in history")
	}

	for _, nodeOutput := range outputs {
		nodeMap, ok := nodeOutput.(map[string]any)
		if !ok {
			continue
		}
		images, ok := nodeMap["images"].([]any)
		if !ok || len(images) == 0 {
			continue
		}
		img, ok := images[0].(map[string]any)
		if !ok {
			continue
		}
		filename, ok := img["filename"].(string)
		if ok {
			return filename, nil
		}
	}

	return "", fmt.Errorf("no images found in outputs")
}

// GetImage downloads a generated image from ComfyUI.
func (c *ComfyUIClient) GetImage(filename string) ([]byte, error) {
	u := c.baseURL + "/view?" + url.Values{"filename": {filename}}.Encode()
	resp, err := c.httpClient.Get(u)
	if err != nil {
		return nil, fmt.Errorf("get image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("comfyui returned %d fetching image", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// GenerateImage queues a prompt, polls for completion, and returns the image bytes.
func (c *ComfyUIClient) GenerateImage(ctx context.Context, prompt string, referenceImage []byte) ([]byte, error) {
	var refFilename string
	if referenceImage != nil {
		uploaded, err := c.UploadImage(referenceImage, "reference.png")
		if err != nil {
			slog.Warn("failed to upload reference image, falling back to no-face workflow", "error", err)
		} else {
			refFilename = uploaded
		}
	}

	promptID, err := c.QueuePrompt(prompt, refFilename)
	if err != nil {
		return nil, err
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	timeout := time.After(4 * time.Minute)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout:
			return nil, fmt.Errorf("timeout waiting for image generation")
		case <-ticker.C:
			filename, err := c.GetOutputFilename(promptID)
			if err == nil {
				return c.GetImage(filename)
			}
		}
	}
}

func (c *ComfyUIClient) buildWorkflow(prompt string, referenceImage string) map[string]any {
	if c.workflow == nil {
		return map[string]any{"prompt": prompt}
	}

	workflowSource := c.workflow
	if referenceImage == "" && c.workflowHasPlaceholder() && c.fallbackWorkflow != nil {
		slog.Info("no reference image available, using fallback workflow")
		workflowSource = c.fallbackWorkflow
	}

	data, err := json.Marshal(workflowSource)
	if err != nil {
		return map[string]any{"prompt": prompt}
	}
	var wf map[string]any
	if err := json.Unmarshal(data, &wf); err != nil {
		return map[string]any{"prompt": prompt}
	}

	if referenceImage != "" {
		for _, node := range wf {
			nodeMap, ok := node.(map[string]any)
			if !ok {
				continue
			}
			inputs, ok := nodeMap["inputs"].(map[string]any)
			if !ok {
				continue
			}
			if inputs["image"] == "REFERENCE_PLACEHOLDER" {
				inputs["image"] = referenceImage
			}
		}
	}

	positiveNodeID := ""
	for _, node := range wf {
		nodeMap, ok := node.(map[string]any)
		if !ok {
			continue
		}
		if nodeMap["class_type"] == "KSampler" {
			inputs, ok := nodeMap["inputs"].(map[string]any)
			if !ok {
				continue
			}
			if ref, ok := inputs["positive"].([]any); ok && len(ref) > 0 {
				positiveNodeID, _ = ref[0].(string)
			}
			break
		}
	}

	if positiveNodeID != "" {
		if node, ok := wf[positiveNodeID].(map[string]any); ok {
			if inputs, ok := node["inputs"].(map[string]any); ok {
				inputs["text"] = prompt
			}
		}
	}

	return wf
}

func (c *ComfyUIClient) workflowHasPlaceholder() bool {
	data, err := json.Marshal(c.workflow)
	if err != nil {
		return false
	}
	return bytes.Contains(data, []byte("REFERENCE_PLACEHOLDER"))
}
