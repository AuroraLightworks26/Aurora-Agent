package pipeline

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sysagent/pkg/ollama"
	"time"
)

// TaskResult captures structural metadata about a completed execution phase.
type TaskResult struct {
	ID       string
	Command  string
	Success  bool
	ErrorMsg string
}

// Engine orchestrates the execution of task graphs.
type Engine struct {
	Plan *ollama.PipelinePlan
}

func NewEngine(plan *ollama.PipelinePlan) *Engine {
	return &Engine{Plan: plan}
}

func (e *Engine) ExecuteGraph(ctx context.Context) []TaskResult {
	var wg sync.WaitGroup
	var resultsMutex sync.Mutex
	results := make([]TaskResult, 0, len(e.Plan.Steps))

	dependencyFailureMap := make(map[string]bool)
	var depMutex sync.RWMutex

	completedChannels := make(map[string]chan bool, len(e.Plan.Steps))
	for _, step := range e.Plan.Steps {
		completedChannels[step.ID] = make(chan bool, 1)
	}

	commandCounts := make(map[string]int)
	const maxRepeatedCommands = 3

	fmt.Printf("\n⚡ Initiating Execution Engine for %d structural phases...\n", len(e.Plan.Steps))

	for _, step := range e.Plan.Steps {
		resultsMutex.Lock()
		commandCounts[step.Command]++
		if commandCounts[step.Command] >= maxRepeatedCommands {
			fmt.Printf("\n\033[1;31m🚨 [LOOP GUARD TRIPPED] Command '%s' has executed %d times recursively.\n"+
				"    Aborting execution flow to prevent infinite loop or host resource exhaustion.\033[0m\n",
				step.Command, commandCounts[step.Command])

			res := TaskResult{
				ID:       step.ID,
				Command:  step.Command,
				Success:  false,
				ErrorMsg: "Execution halted by internal Loop Guard circuit breaker (repeated command limit reached).",
			}
			results = append(results, res)
			resultsMutex.Unlock()
			break
		}
		resultsMutex.Unlock()

		isInteractive := strings.Contains(step.Command, "sudo")
		wg.Add(1)

		go func(s ollama.TaskStep, interactive bool) {
			defer wg.Done()

			for _, depID := range s.DependsOn {
				ch, exists := completedChannels[depID]
				if exists {
					select {
					case success := <-ch:
						ch <- success
						if !success {
							depMutex.Lock()
							dependencyFailureMap[s.ID] = true
							depMutex.Unlock()
						}
					case <-ctx.Done():
						return
					}
				}
			}

			depMutex.RLock()
			isSkipped := dependencyFailureMap[s.ID]
			depMutex.RUnlock()

			if isSkipped {
				fmt.Printf("⏭️  [Skipped] %s: Bypassed due to parent dependency failure.\n", s.ID)
				res := TaskResult{
					ID:       s.ID,
					Command:  s.Command,
					Success:  false,
					ErrorMsg: "Skipped due to upstream parent failure.",
				}
				resultsMutex.Lock()
				results = append(results, res)
				resultsMutex.Unlock()

				completedChannels[s.ID] <- false
				return
			}

			if err := ctx.Err(); err != nil {
				return
			}

			cmd := exec.CommandContext(ctx, "bash", "-c", s.Command)

			// Route interactive/sudo commands to the active TTY for password prompts
			if interactive {
				fmt.Printf("🔑 [Elevation Bypass] Phase %s requires administrative context. Routing to system TTY...\n", s.ID)
				fmt.Printf("🛠️  [Executing] %s: %s\n", s.ID, s.Command)

				cmd.Stdin = os.Stdin
				cmd.Stderr = os.Stderr

				var stdoutBuf strings.Builder
				cmd.Stdout = &stdoutBuf

				err := cmd.Run()
				outputStr := strings.TrimSpace(stdoutBuf.String())

				res := TaskResult{
					ID:      s.ID,
					Command: s.Command,
					Success: err == nil,
				}

				if err != nil {
					res.ErrorMsg = fmt.Sprintf("%v", err)
					fmt.Printf("❌ [Failed] %s: %v\n", s.ID, err)
					completedChannels[s.ID] <- false
				} else {
					fmt.Printf("✅ [Completed] %s\n", s.ID)
					if outputStr != "" {
						fmt.Printf("\033[1;34m--- Output from %s ---\033[0m\n%s\n\033[1;34m-----------------------\033[0m\n\n", s.ID, outputStr)
					}
					completedChannels[s.ID] <- true
				}

				resultsMutex.Lock()
				results = append(results, res)
				resultsMutex.Unlock()
				return
			}

			// Standard execution context (runs directly as the user invoking sysagent)
			fmt.Printf("🛠️  [Executing] %s: %s\n", s.ID, s.Command)

			cmd.Stdin = os.Stdin
			output, err := cmd.CombinedOutput()
			outputStr := strings.TrimSpace(string(output))

			res := TaskResult{
				ID:      s.ID,
				Command: s.Command,
				Success: err == nil,
			}

			if err != nil {
				res.ErrorMsg = fmt.Sprintf("%v: %s", err, outputStr)
				fmt.Printf("❌ [Failed] %s: %v\n", s.ID, err)
				if outputStr != "" {
					fmt.Printf("\033[1;31m%s\033[0m\n", outputStr)
				}
				completedChannels[s.ID] <- false
			} else {
				fmt.Printf("✅ [Completed] %s\n", s.ID)
				if outputStr != "" {
					fmt.Printf("\033[1;34m--- Output from %s ---\033[0m\n%s\n\033[1;34m-----------------------\033[0m\n\n", s.ID, outputStr)
				}
				completedChannels[s.ID] <- true
			}

			resultsMutex.Lock()
			results = append(results, res)
			resultsMutex.Unlock()
		}(step, isInteractive)

		if isInteractive {
			wg.Wait()
		} else {
			time.Sleep(50 * time.Millisecond)
		}
	}

	wg.Wait()
	return results
}
