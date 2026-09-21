package ollama

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const DefaultURL = "http://localhost:11435"

type Client struct {
	URL        string
	Model      string
	HTTPClient *http.Client // 🚨 Add an internal reusable client handle field
}

func NewClient(model string) *Client {
	return &Client{
		URL:   "http://localhost:11434", // Adjust if your local Ollama port differs
		Model: model,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second, // 🚨 CRITICAL GUARDRAIL: Automatically breaks the lock if Ollama hangs for more than 10 seconds
		},
	}
}

// GeneratePlanWithMemory queries the 14B model, handles structural JSON schemas, and adapts to variations.
func (c *Client) GeneratePlanWithMemory(userPrompt string, memories []string) (*PipelinePlan, error) {
	systemInstruction := "You are a senior Linux systems engineering planner. Break the user's high-level request down into individual, low-level shell commands. Identify which commands can run concurrently and which depend on previous steps. " +
		"CRITICAL MANDATE: You MUST always generate the requested steps and commands. Even if historical data shows a command failed or implies a file does not exist, do not return an empty array. Always produce the functional commands so the system execution engine can evaluate them."

	if len(memories) > 0 {
		systemInstruction += "\n\nCRITICAL CONTEXT: Here is historical data about how this system handled similar commands in the past. Use this history to avoid repeating previous mistakes or to mimic successful structural sequences:\n" + strings.Join(memories, "\n")
	}

	schema := FormatSchema{
		Type: "object",
		Properties: map[string]interface{}{
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
		"model":  c.Model,
		"prompt": userPrompt,
		"system": systemInstruction,
		"stream": false,
		"format": schema,
		"options": OllamaOptions{
			NumCtx:      4096,
			Temperature: 0.0,
		},
		"keep_alive": 0, // 🚨 CRITICAL: Forces Ollama to flush the 14B model from VRAM instantly after inference
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := http.Post(c.URL+"/api/generate", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed running http post to ollama: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed reading response body: %w", err)
	}

	// 1. Decode Ollama's main wrapper block
	var outerResp struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(bodyBytes, &outerResp); err != nil {
		return nil, fmt.Errorf("failed decoding outer ollama structure: %w", err)
	}

	var plan PipelinePlan

	// 2. ADAPTIVE PARSING LAYER: Try parsing as the standard wrapped JSON object first
	if err := json.Unmarshal([]byte(outerResp.Response), &plan); err == nil && len(plan.Steps) > 0 {
		return &plan, nil
	}

	// 3. Fallback: If 'steps' array was empty or failed, try parsing it as a direct un-nested slice
	var flatSteps []TaskStep
	if err := json.Unmarshal([]byte(outerResp.Response), &flatSteps); err == nil && len(flatSteps) > 0 {
		plan.Steps = flatSteps
		return &plan, nil
	}

	// 4. Detailed error boundary fallback to see exactly what the model spit out if both fail
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

// AnalyzeCommandFailure runs the lightweight 1.5B base model to interpret terminal error outputs.
func (c *Client) AnalyzeCommandFailure(command string, errorMsg string) (string, error) {
	// 🚨 FORCE TARGET BOUNDARY MARKER IN THE INSTRUCTION PROMPT
	prompt := fmt.Sprintf("Context: A Linux system administration command failed.\n"+
		"Command executed: %s\n"+
		"Error output captured: %s\n\n"+
		"Identify if this is a missing command/package or a permission error, explain why it failed, and provide the correct command to fix it. End your response with '[END]'.\n\n"+
		"Explanation: ", command, errorMsg)

	payload := map[string]interface{}{
		"model":      "qwen2.5-coder:1.5b",
		"prompt":     prompt,
		"stream":     false,
		"keep_alive": 0,
		"options": map[string]interface{}{
			"temperature": 0.3, // 🚨 Slightly increase creativity to prevent immediate token starvation loops
			"num_ctx":     2048,
			"stop":        []string{"[END]"}, // 🚨 NARROW LIMITS: Rely entirely on your unique marker token tag to cut generations
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	resp, err := c.HTTPClient.Post(c.URL+"/api/generate", "application/json", bytes.NewBuffer(jsonData))

	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// fmt.Printf("\n🔍 [DEBUG] Raw Ollama API Response Payload: %s\n", string(bodyBytes))

	var outerResp struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(bodyBytes, &outerResp); err != nil {
		return "", err
	}

	// 🚨 CLEANUP: In case the stop gate lets a tiny boundary fragment slip into the string buffer, trim it away
	cleanedResponse := strings.TrimSpace(outerResp.Response)
	cleanedResponse = strings.TrimSuffix(cleanedResponse, "[END]")
	cleanedResponse = strings.TrimSpace(cleanedResponse)

	return "1. " + cleanedResponse, nil
}
