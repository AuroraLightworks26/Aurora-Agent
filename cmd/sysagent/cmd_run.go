package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sysagent/pkg/db"
	"sysagent/pkg/ollama"
	"sysagent/pkg/pipeline"
)

// handlePipelineExecution contains our verified core operational DAG loop
func handlePipelineExecution(store *db.Store, client *ollama.Client, userInstruction string) {
	fmt.Println("🧠 Scanning local historical records for relevant system knowledge...")
	queryVector, err := client.GetEmbedding(userInstruction)

	var contextMemories []string
	if err == nil {
		memories, err := store.SearchRelevantMemories(queryVector, 0.70)
		if err == nil && len(memories) > 0 {
			fmt.Printf("💡 Found %d relevant historical execution logs. Injecting into Planner model...\n", len(memories))
			for _, mem := range memories {
				status := "SUCCESS"
				if !mem.Success {
					status = fmt.Sprintf("FAILED (Error: %s)", mem.ErrorMessage)
				}
				contextMemories = append(contextMemories, fmt.Sprintf("- Command: `%s` was run previously. Status: %s", mem.Command, status))
			}
		}
	}

	fmt.Println("🤖 Analyzing system task and constructing execution plan...")
	plan, err := client.GeneratePlanWithMemory(userInstruction, contextMemories)
	if err != nil {
		fmt.Printf("Planning engine error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n📋 Proposed Pipeline Execution Plan:\n")
	for _, step := range plan.Steps {
		fmt.Printf("  🏷️  [Phase %s]: `\033[1;32m%s\033[0m` \n", step.ID, step.Command)
		fmt.Printf("     ↳ Purpose: %s\n", step.Reason)
		if len(step.DependsOn) > 0 {
			fmt.Printf("     ↳ Dependencies: %v\n", step.DependsOn)
		}
		fmt.Println()
	}

	var confirmation string
	fmt.Print("⚠️  Execute this entire pipeline graph? (y/N): ")
	fmt.Scanln(&confirmation)
	confirmation = strings.ToLower(strings.TrimSpace(confirmation))

	if confirmation != "y" && confirmation != "yes" {
		fmt.Println("🛑 Execution aborted by user. No commands were run.")
		os.Exit(0)
	}

	engine := pipeline.NewEngine(plan)
	ctx := context.Background()
	executionMetrics := engine.ExecuteGraph(ctx)

	fmt.Println("\n💾 Vector-indexing and cataloging pipeline results to database ledger...")

	hasFailures := false
	var failedMetrics []pipeline.TaskResult

	for _, metric := range executionMetrics {
		if !metric.Success && metric.ErrorMsg != "Skipped due to upstream parent failure." {
			hasFailures = true
			failedMetrics = append(failedMetrics, metric)
		}

		// Intercept Engine Hardware Errors
		if strings.Contains(metric.ErrorMsg, "signal:") {
			fmt.Printf("\n\033[1;33m⚠️  [ENGINE NOTICE] Phase %s failed via system kill termination signal.\n"+
				"    This usually points to a host hardware restriction or an Out-Of-Memory (OOM) ceiling event,\n"+
				"    rather than an inherent bug within your command shell syntax.\033[0m\n", metric.ID)
		}

		vector, err := client.GetEmbedding(metric.Command)
		if err != nil {
			vector = make([]float32, 0)
		}

		if err := store.LogTaskResult(metric, vector); err != nil {
			fmt.Printf("Telemetry storage failure for step %s: %v\n", metric.ID, err)
		}

		// 🚨 AUTONOMOUS INTERPRETER PASS
		// If a command explicitly fails, invoke the 1.5B model to analyze the text output
		if !metric.Success && metric.ErrorMsg != "Skipped due to upstream parent failure." && !strings.Contains(metric.ErrorMsg, "signal:") {
			fmt.Printf("\n🤖 [Qwen-1.5B] Interpreting failure for phase %s...\n", metric.ID)
			analysis, err := client.AnalyzeCommandFailure(metric.Command, metric.ErrorMsg)
			if err == nil {
				fmt.Printf("\033[1;35m%s\033[0m\n\n", analysis)
			} else {
				fmt.Printf("⚠️  Interpreter model failed to execute: %v\n", err)
			}
		}

	}

	// 🚨 NEW: Dynamic Troubleshooting Guidance Layer
	if hasFailures {
		generateTroubleshootingGuidance(userInstruction, failedMetrics)
	}

	fmt.Println("🎉 Operations workflow pipeline cycle concluded successfully.")
}

// generateTroubleshootingGuidance analyzes errors and tells the user exactly how to rewrite the prompt.
func generateTroubleshootingGuidance(originalPrompt string, failures []pipeline.TaskResult) {
	fmt.Printf("\n\033[1;36m💡 [SysAgent Optimizer] Analysis of execution hurdles detected:\033[0m\n")
	fmt.Println(strings.Repeat("─", 65))

	for _, f := range failures {
		if strings.Contains(f.ErrorMsg, "signal: killed") {
			fmt.Println("❌ Issue: The engine hit a hardware Out-Of-Memory (OOM) limit running parallel forks.")
			fmt.Println("👉 Guidance: Simplify your prompt by splitting it up into smaller, sequential instructions.")
			fmt.Println("📝 Refactored Prompt Recommendation:")
			fmt.Printf("   Instead of combined commands, run them one-by-one, e.g.:\n")
			fmt.Printf("   \033[1;32m./sysagent \"only show active memory metrics\"\033[0m\n")
		} else if strings.Contains(strings.ToLower(f.ErrorMsg), "permission denied") {
			fmt.Println("❌ Issue: Elevated administrative privileges are required to access this resource.")
			fmt.Println("👉 Guidance: Restate your prompt to include `sudo` parameters.")
			fmt.Println("📝 Refactored Prompt Recommendation:")
			fmt.Printf("   Change your prompt to include sudo, e.g.:\n")
			fmt.Printf("   \033[1;32m./sysagent \"sudo %s\"\033[0m\n", f.Command)
		} else if strings.Contains(f.Command, "/var/log/auth.log") && strings.Contains(f.ErrorMsg, "exit status 1") {
			fmt.Println("❌ Issue: The file `/var/log/auth.log` does not exist on this machine.")
			fmt.Println("👉 Guidance: Your Debian system uses systemd-journald for authentication metrics logs.")
			fmt.Println("📝 Refactored Prompt Recommendation:")
			fmt.Printf("   Query the journal service instead using a command like:\n")
			fmt.Printf("   \033[1;32m./sysagent \"sudo journalctl -u ssh --no-pager\"\033[0m\n")
		} else {
			fmt.Printf("❌ Issue in Phase [%s]: Command returned error: %s\n", f.ID, f.ErrorMsg)
			fmt.Println("👉 Guidance: If this command relies on specific conditions, explicitly guide the planner in your prompt.")
			fmt.Println("📝 Refactored Prompt Suggestion:")
			fmt.Printf("   Try adding explicit guardrails: \033[1;32m\"If available, check %s, otherwise skip gracefully\"\033[0m\n", f.Command)
		}
	}
	fmt.Println(strings.Repeat("─", 65))
}
