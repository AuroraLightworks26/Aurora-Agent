package ollama

import (
	"encoding/json"
	"testing"
)

func TestPipelinePlanParsingWithReflections(t *testing.T) {
	rawJSON := `{
		"reasoning": "Detected PipeWire backend and added a new bluetooth quirk based on environment discovery.",
		"mental_model_updates": {
			"new_quirks": [
				{
					"context_key": "bluetooth_audio",
					"description": "Phone requires explicit trusted pairing via bluetoothctl."
				}
			],
			"profile_updates": [
				{
					"category": "audio",
					"attribute": "active_backend",
					"value": "PipeWire"
				}
			]
		},
		"steps": [
			{
				"id": "1",
				"command": "wpctl status",
				"depends_on": [],
				"reason": "Inspect active audio sinks and sources."
			}
		]
	}`

	var plan PipelinePlan
	err := json.Unmarshal([]byte(rawJSON), &plan)
	if err != nil {
		t.Fatalf("failed to parse plan with reflections schema: %v", err)
	}

	if len(plan.Steps) != 1 {
		t.Errorf("expected 1 execution step, got %d", len(plan.Steps))
	}

	if len(plan.MentalModelUpdates.NewQuirks) != 1 {
		t.Fatalf("expected 1 new quirk, got %d", len(plan.MentalModelUpdates.NewQuirks))
	}
}
