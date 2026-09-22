package pipeline

import (
	"context"
	"strings"
	"sysagent/pkg/ollama"
	"testing"
)

func TestExecutorLoopGuard(t *testing.T) {
	plan := &ollama.PipelinePlan{
		Steps: []ollama.TaskStep{
			{ID: "step1", Command: "echo 'hello'"},
			{ID: "step2", Command: "echo 'hello'"},
			{ID: "step3", Command: "echo 'hello'"}, // 3rd repeat trips breaker
			{ID: "step4", Command: "echo 'hello'"},
		},
	}

	engine := NewEngine(plan)
	ctx := context.Background()

	results := engine.ExecuteGraph(ctx)

	if len(results) > 3 {
		t.Fatalf("expected engine to abort at 3 iterations, but ran %d steps", len(results))
	}

	lastResult := results[len(results)-1]
	if !strings.Contains(lastResult.ErrorMsg, "Loop Guard circuit breaker") {
		t.Errorf("expected loop guard error message, got: %s", lastResult.ErrorMsg)
	}
}
