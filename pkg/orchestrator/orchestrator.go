package orchestrator

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"sysagent/pkg/db"
	"sysagent/pkg/ollama"
	"sysagent/pkg/pipeline"
)

// Orchestrator manages the deterministic execution loop across tiered models.
type Orchestrator struct {
	Store  *db.Store
	Client *ollama.Client
}

// DecomposedPlan represents the output from Step 1 (The Tiny Decomposer).
type DecomposedPlan struct {
	Goal  string   `json:"goal"`
	Steps []string `json:"steps"`
}

func New(store *db.Store, client *ollama.Client) *Orchestrator {
	return &Orchestrator{
		Store:  store,
		Client: client,
	}
}

// selectCriticModel routes to 7B or 14B depending on retry/attempt counts
func selectCriticModel(retryCount int) string {
	if retryCount >= 2 {
		return "qwen2.5-coder:14b"
	}
	return "qwen2.5-coder:7b"
}

// promptUserForApproval displays the generated steps and waits for explicit 'y' confirmation
func promptUserForApproval(steps []ollama.TaskStep) bool {
	if len(steps) == 0 {
		fmt.Println("⚠️ [Orchestrator Guard] No valid executable steps remain after filtering.")
		return false
	}

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println(" 🛡️  SYSAGENT EXECUTION APPROVAL REQUIRED")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("The Architect proposes executing the following steps:\n")

	for idx, s := range steps {
		reason := s.Reason
		if reason == "" {
			reason = "No explanation provided"
		}
		fmt.Printf("  [%d] Purpose: %s\n", idx+1, reason)
		fmt.Printf("      Command: \033[1;33m%s\033[0m\n\n", s.Command)
	}
	fmt.Println(strings.Repeat("=", 60))

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Proceed with execution? [y/N]: ")

	input, err := reader.ReadString('\n')
	if err != nil {
		fmt.Println("\n⚠️ Failed to read input or stdin closed. Skipping execution.")
		return false
	}

	cleanInput := strings.ToLower(strings.TrimSpace(input))
	if cleanInput == "y" || cleanInput == "yes" {
		fmt.Println("✅ [Approval Granted] Executing phase commands...\n")
		return true
	}

	fmt.Println("🛑 [Execution Aborted] User declined approval. Skipping this phase.\n")
	return false
}

// Step 1: Tiny Decomposer (1.5B Model) splits high-level goal into simple steps
func (o *Orchestrator) DecomposeGoal(ctx context.Context, userInstruction string) (*DecomposedPlan, error) {
	prompt := fmt.Sprintf(`Break down the following system administration task into a sequential list of high-level atomic goals.
Do not write shell commands. Return ONLY a valid JSON object matching this schema:
{
  "goal": "summary of user goal",
  "steps": ["step 1 description", "step 2 description"]
}

User Task: %s
[END]`, userInstruction)

	resp, err := o.Client.GenerateWithModel(ctx, "qwen2.5-coder:1.5b", prompt)
	if err != nil {
		return nil, fmt.Errorf("decomposer model error: %w", err)
	}

	// Clean code block markdown if present
	cleanJSON := strings.TrimPrefix(resp, "```json")
	cleanJSON = strings.TrimPrefix(cleanJSON, "```")
	cleanJSON = strings.TrimSuffix(cleanJSON, "```")
	cleanJSON = strings.TrimSpace(cleanJSON)

	var plan DecomposedPlan
	if err := json.Unmarshal([]byte(cleanJSON), &plan); err != nil {
		return nil, fmt.Errorf("decomposer JSON parse error: %w (raw response: %s)", err, resp)
	}
	return &plan, nil
}

// Step 2 & 4 Execution Loop: Handles execution, tracking, and escalations
func (o *Orchestrator) Run(ctx context.Context, userInstruction string) error {
	fmt.Println("🧩 [Step 1] Decomposing task with Tiny Decomposer (1.5B)...")
	decomposed, err := o.DecomposeGoal(ctx, userInstruction)
	if err != nil {
		fmt.Printf("⚠️ Decomposition failed: %v. Falling back to single-stage planning.\n", err)
		decomposed = &DecomposedPlan{
			Goal:  userInstruction,
			Steps: []string{userInstruction},
		}
	}

	fmt.Printf("\n📋 High-Level Pipeline Goal: %s\n", decomposed.Goal)
	for i, step := range decomposed.Steps {
		fmt.Printf("  %d. %s\n", i+1, step)
	}
	fmt.Println()

	// Track total command execution counts across steps to enforce global Loop Guard
	commandExecutionCounts := make(map[string]int)
	const maxCommandLimit = 3

	// Deterministic Outer Loop managed strictly by Go
	for i, stepDesc := range decomposed.Steps {
		fmt.Printf("────── 🛠️ Phase %d/%d: %s ──────\n", i+1, len(decomposed.Steps), stepDesc)

		// Fetch current mental model context from SQLite
		var profStrs []string
		if profileEntries, err := o.Store.GetSystemProfile(); err == nil {
			for _, p := range profileEntries {
				profStrs = append(profStrs, fmt.Sprintf("%s.%s = %s", p.Category, p.Attribute, p.Value))
			}
		}

		var quirkStrs []string
		if quirks, err := o.Store.GetSystemQuirks(); err == nil {
			for _, q := range quirks {
				quirkStrs = append(quirkStrs, fmt.Sprintf("[%s]: %s", q.ContextKey, q.Description))
			}
		}

		// 🚨 INJECT MANDATORY EXECUTION CONSTRAINTS
		quirkStrs = append(quirkStrs,
			"[COMMAND_RULE_1]: NEVER output interactive text editors (e.g., 'nano', 'vim', 'vi'). ALWAYS use non-interactive stream edits or redirection (e.g., 'sed', 'tee -a', 'echo \"...\" | sudo tee').",
			"[COMMAND_RULE_2]: NEVER issue reboot or shutdown commands directly (e.g., 'reboot', 'shutdown -h now'). Suggest them as recommendations instead.",
			"[COMMAND_RULE_3]: PipeWire, WirePlumber, and PulseAudio run as USER services. ALWAYS use 'systemctl --user' or 'pactl' instead of 'systemctl' or 'pacmd'.",
		)

		// 🧠 Step 2: The Large Architect (7B Model) plans commands for ONLY this single step
		fmt.Println("🤖 Architect (7B) generating execution plan for this step...")
		plan, err := o.Client.GeneratePlanWithMemory(stepDesc, nil, profStrs, quirkStrs)
		if err != nil {
			fmt.Printf("❌ Planning error for phase %d: %v\n", i+1, err)
			continue
		}

		// Deduplicate and filter forbidden interactive commands
		seenCmds := make(map[string]bool)
		var cleanSteps []ollama.TaskStep

		for _, s := range plan.Steps {
			cmdKey := strings.TrimSpace(s.Command)

			// 🚫 GUARD 1: Filter out interactive editor phases completely
			if strings.Contains(cmdKey, "nano ") || strings.Contains(cmdKey, "vim ") || strings.Contains(cmdKey, "vi ") {
				fmt.Printf("⚠️ [Orchestrator Guard] Filtered out interactive editor step: '%s'\n", cmdKey)
				continue
			}

			// 🚫 GUARD 2: Filter out dangerous reboot/shutdown steps
			if cmdKey == "sudo reboot" || cmdKey == "reboot" || strings.Contains(cmdKey, "shutdown") {
				fmt.Printf("⚠️ [Orchestrator Guard] Filtered out reboot step: '%s'\n", cmdKey)
				continue
			}

			if !seenCmds[cmdKey] && cmdKey != "" {
				seenCmds[cmdKey] = true
				cleanSteps = append(cleanSteps, s)
			}
		}
		plan.Steps = cleanSteps

		// 🛡️ HUMAN-IN-THE-LOOP (HITL) APPROVAL GATE
		if !promptUserForApproval(plan.Steps) {
			continue // Skip execution for this phase if not explicitly approved
		}

		// Step 3: Pure Execution Pass via Pipeline Engine
		engine := pipeline.NewEngine(plan)
		executionMetrics := engine.ExecuteGraph(ctx)

		// Process execution results and check for failures
		for _, metric := range executionMetrics {
			cmdStr := strings.TrimSpace(metric.Command)
			commandExecutionCounts[cmdStr]++

			// 🚨 LOOP GUARD CHECK
			if commandExecutionCounts[cmdStr] >= maxCommandLimit {
				fmt.Printf("\n\033[1;31m🚨 [LOOP GUARD TRIPPED] Command '%s' has attempted execution %d times.\n"+
					"    Halting pipeline to prevent recursive loop or resource exhaustion.\033[0m\n",
					cmdStr, commandExecutionCounts[cmdStr])
				return fmt.Errorf("loop guard tripped on command: %s", cmdStr)
			}

			// Store metric to SQLite
			vector, _ := o.Client.GetEmbedding(metric.Command)
			_ = o.Store.LogTaskResult(metric, vector)

			// Step 4: Failure Handling & Escalated Critic
			if !metric.Success && metric.ErrorMsg != "Skipped due to upstream parent failure." {
				fmt.Printf("❌ Phase %s failed: %s\n", metric.ID, metric.ErrorMsg)

				// Determine Critic model dynamically using selectCriticModel
				retryCount := commandExecutionCounts[cmdStr]
				criticModel := selectCriticModel(retryCount)

				if criticModel == "qwen2.5-coder:14b" {
					fmt.Println("⚡ [Escalation Gateway] Routing diagnostic pass to 14B Deep Critic model...")
				}

				fmt.Printf("🤖 [%s Critic] Analyzing execution failure...\n", criticModel)
				analysis, err := o.Client.AnalyzeCommandFailureWithModel(criticModel, metric.Command, metric.ErrorMsg)
				if err == nil {
					fmt.Printf("\033[1;35m--- Diagnostic Recommendation ---\n%s\n-----------------------------------\033[0m\n\n", analysis)
				}
			}
		}
	}

	fmt.Println("\n🎉 Orchestrated pipeline execution finished.")
	return nil
}
