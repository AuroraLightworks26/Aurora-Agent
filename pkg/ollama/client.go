package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const DefaultURL = "http://localhost:11434"

type Client struct {
	URL        string
	HTTPClient *http.Client // 🚨 Add an internal reusable client handle field
}

// NewClient accepts an optional baseURL. If empty, it defaults to DefaultURL.
func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = DefaultURL
	}
	return &Client{
		URL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 180 * time.Second, // Global timeout to prevent VRAM swapping deadlocks
		},
	}
}

// GeneratePlanWithMemory queries the 14B model with historical memories, profiles, and quirks.
func (c *Client) GeneratePlanWithMemory(userPrompt string, memories []string, profileStrs []string, quirkStrs []string) (*PipelinePlan, error) {
	fmt.Println("  ⏳ [Ollama] Sending context payload to 7B Architect...")

	systemInstruction := `You are SysAgent, an expert Linux system administration engine.
Generate a strictly formatted JSON plan for the user request.

CRITICAL EXECUTION CONSTRAINTS:
1. NEVER output interactive file editor commands like 'nano', 'vim', 'vi', or 'emacs'.
2. NEVER create separate steps just to "edit" or "open" a file.
3. To modify or append to files, directly use non-interactive commands like 'echo "text" | sudo tee -a /file' or 'sed -i ...'.
4. NEVER issue 'reboot' or 'shutdown' commands.

Return JSON in this format:
{
  "reasoning": "explanation",
  "steps": [
    {
      "id": "phase_id",
      "command": "non-interactive bash command",
      "depends_on": [],
      "reason": "purpose"
    }
  ]
}`
	// 🧠 INJECT SYSTEM PROFILE STATE
	if len(profileStrs) > 0 {
		systemInstruction += "\n\n[HOST SYSTEM PROFILE (Cached Facts)]:\n" + strings.Join(profileStrs, "\n")
	}

	// 🧠 INJECT KNOWN QUIRKS
	if len(quirkStrs) > 0 {
		systemInstruction += "\n\n[KNOWN SYSTEM QUIRKS & TRAPS]:\n" + strings.Join(quirkStrs, "\n")
	}

	if len(memories) > 0 {
		systemInstruction += "\n\n[HISTORICAL EXECUTION CONTEXT]:\n" + strings.Join(memories, "\n")
	}

	schema := FormatSchema{
		Type: "object",
		Properties: map[string]interface{}{
			"reasoning": map[string]string{"type": "string"},
			"mental_model_updates": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"new_quirks": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"context_key": map[string]string{"type": "string"},
								"description": map[string]string{"type": "string"},
							},
							"required": []string{"context_key", "description"},
						},
					},
					"profile_updates": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"category":  map[string]string{"type": "string"},
								"attribute": map[string]string{"type": "string"},
								"value":     map[string]string{"type": "string"},
							},
							"required": []string{"category", "attribute", "value"},
						},
					},
				},
			},
			"steps": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id":         map[string]string{"type": "string"},
						"command":    map[string]string{"type": "string"},
						"depends_on": map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}},
						"reason":     map[string]string{"type": "string"},
					},
					"required": []string{"id", "command", "depends_on", "reason"},
				},
			},
		},
		Required: []string{"steps"},
	}

	payload := map[string]interface{}{
		"model":  "qwen2.5-coder:7b",
		"prompt": userPrompt,
		"system": systemInstruction,
		"stream": false,
		"format": schema,
		"options": OllamaOptions{
			NumCtx:      4096,
			Temperature: 0.0,
		},
		"keep_alive": 0,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := c.HTTPClient.Post(c.URL+"/api/generate", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed running http post to ollama: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed reading response body: %w", err)
	}

	fmt.Println("  ⚡ [Ollama] Plan received from 7B Architect. Parsing JSON...")

	var outerResp struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(bodyBytes, &outerResp); err != nil {
		return nil, fmt.Errorf("failed decoding outer ollama structure: %w", err)
	}

	var plan PipelinePlan
	if err := json.Unmarshal([]byte(outerResp.Response), &plan); err == nil && len(plan.Steps) > 0 {
		return &plan, nil
	}

	var flatSteps []TaskStep
	if err := json.Unmarshal([]byte(outerResp.Response), &flatSteps); err == nil && len(flatSteps) > 0 {
		plan.Steps = flatSteps
		return &plan, nil
	}

	return nil, fmt.Errorf("model response did not fit steps schema. Raw response content: %s", outerResp.Response)
}

// GetEmbedding queries the local nomic-embed-text model to translate text into a vector array.
func (c *Client) GetEmbedding(text string) ([]float32, error) {
	payload := EmbeddingRequest{
		Model:     "nomic-embed-text",
		Prompt:    text,
		KeepAlive: 0,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal embedding request: %w", err)
	}

	resp, err := c.HTTPClient.Post(c.URL+"/api/embeddings", "application/json", bytes.NewBuffer(jsonData))

	if err != nil {
		return nil, fmt.Errorf("failed running http post for embeddings: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed reading embedding response body: %w", err)
	}

	var embedResp EmbeddingResponse
	if err := json.Unmarshal(bodyBytes, &embedResp); err != nil {
		return nil, fmt.Errorf("failed decoding embedding data payload: %w", err)
	}

	return embedResp.Embedding, nil
}

// / AnalyzeCommandFailure runs the default 1.5B model to interpret terminal error outputs.
func (c *Client) AnalyzeCommandFailure(command string, errorMsg string) (string, error) {
	return c.AnalyzeCommandFailureWithModel("qwen2.5-coder:1.5b", command, errorMsg)
}

// GenerateWithModel sends a prompt to a specified Ollama model with custom options and stop tokens.
func (c *Client) GenerateWithModel(ctx context.Context, model string, prompt string) (string, error) {
	payload := map[string]interface{}{
		"model":      model,
		"prompt":     prompt,
		"stream":     false,
		"keep_alive": 0,
		"options": map[string]interface{}{
			"temperature": 0.1,
			"num_ctx":     2048,
			"stop":        []string{"[END]", "User:", "\n\nUser"},
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.URL+"/api/generate", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create http request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed executing HTTP post to ollama: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed reading response body: %w", err)
	}

	var outerResp struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(bodyBytes, &outerResp); err != nil {
		return "", fmt.Errorf("failed decoding ollama structure: %w", err)
	}

	cleanedResponse := strings.TrimSpace(outerResp.Response)
	cleanedResponse = strings.TrimSuffix(cleanedResponse, "[END]")
	return strings.TrimSpace(cleanedResponse), nil
}

func (c *Client) AnalyzeCommandFailureWithModel(model string, command string, errorMsg string) (string, error) {
	// ⏱️ Set a strict 45-second deadline so the Go client never blocks indefinitely
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	// Brief pause to allow the Ollama daemon to finish VRAM state transitions
	time.Sleep(500 * time.Millisecond)

	prompt := fmt.Sprintf("Context: A Linux system administration command failed.\n"+
		"Command executed: %s\n"+
		"Error output captured: %s\n\n"+
		"Identify if this is a missing command/package or a permission error, explain why it failed, and provide the correct command to fix it. End your response with '[END]'.\n\n"+
		"Explanation: ", command, errorMsg)

	payload := map[string]interface{}{
		"model":  model,
		"prompt": prompt,
		"stream": false,
		"options": map[string]interface{}{
			"temperature": 0.2,
			"num_ctx":     2048,
			"stop":        []string{"[END]"},
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.URL+"/api/generate", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama request timed out or failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var outerResp struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(bodyBytes, &outerResp); err != nil {
		return "", err
	}

	cleanedResponse := strings.TrimSpace(outerResp.Response)
	cleanedResponse = strings.TrimSuffix(cleanedResponse, "[END]")
	return "1. " + strings.TrimSpace(cleanedResponse), nil
}
