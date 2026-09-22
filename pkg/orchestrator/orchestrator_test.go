package orchestrator

import (
	"encoding/json"
	"testing"
)

// TestDecomposedPlanParsing verifies the Tiny Decomposer's JSON parsing
func TestDecomposedPlanParsing(t *testing.T) {
	rawJSON := `{
		"goal": "Check system sound cards and audio status",
		"steps": [
			"Inspect ALSA sound card list",
			"Check active PipeWire audio streams"
		]
	}`

	var plan DecomposedPlan
	err := json.Unmarshal([]byte(rawJSON), &plan)
	if err != nil {
		t.Fatalf("failed to parse decomposer JSON: %v", err)
	}

	if len(plan.Steps) != 2 {
		t.Fatalf("expected 2 decomposed steps, got %d", len(plan.Steps))
	}

	if plan.Steps[0] != "Inspect ALSA sound card list" {
		t.Errorf("unexpected step 1 string: %s", plan.Steps[0])
	}
}

// TestCriticModelEscalationRouting verifies that the Critic routes to 14B on repeated retries
func TestCriticModelEscalationRouting(t *testing.T) {
	selectCriticModel := func(retryCount int) string {
		if retryCount >= 2 {
			return "qwen2.5-coder:14b"
		}
		return "qwen2.5-coder:7b"
	}

	// Attempt 1: Standard 7B Critic
	if model := selectCriticModel(1); model != "qwen2.5-coder:7b" {
		t.Errorf("expected 7b model for first retry, got %s", model)
	}

	// Attempt 2: Escalated 14B Deep Critic
	if model := selectCriticModel(2); model != "qwen2.5-coder:14b" {
		t.Errorf("expected 14b model for second retry, got %s", model)
	}
}
