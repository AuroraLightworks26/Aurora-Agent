package main

import (
	"context"
	"fmt"
	"strings"
	"sysagent/pkg/db"
	"sysagent/pkg/ollama"
	"sysagent/pkg/orchestrator"
	"sysagent/pkg/pipeline"
)

// handlePipelineExecution runs the orchestrator's core execution loop
func handlePipelineExecution(store *db.Store, client *ollama.Client, userInstruction string) {
	orch := orchestrator.New(store, client)
	ctx := context.Background()

	if err := orch.Run(ctx, userInstruction); err != nil {
		fmt.Printf("\n🛑 Execution stopped: %v\n", err)
	}
}

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
