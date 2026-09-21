package pipeline

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"sysagent/pkg/ollama"
	"syscall"
	"time"
)

// TaskResult captures structural metadata about a completed execution phase.
type TaskResult struct {
	ID       string
	Command  string
	Success  bool
	ErrorMsg string
}

// Engine orchestrates the concurrent execution of task graphs.
type Engine struct {
	Plan *ollama.PipelinePlan
}

func NewEngine(plan *ollama.PipelinePlan) *Engine {
	return &Engine{Plan: plan}
}

// ExecuteGraph runs tracks while dropping privileges to 'sysagent' EXCEPT when explicit sudo elevation is active.
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

	// 🔒 Pre-cache the 'sysagent' restricted system credentials
	var sandboxAttr *syscall.SysProcAttr
	agentUser, err := user.Lookup("sysagent")
	if err == nil {
		uid, _ := strconv.Atoi(agentUser.Uid)
		gid, _ := strconv.Atoi(agentUser.Gid)
		sandboxAttr = &syscall.SysProcAttr{
			Credential: &syscall.Credential{
				Uid: uint32(uid),
				Gid: uint32(gid),
			},
		}
	}

	fmt.Printf("\n⚡ Initiating Execution Engine for %d structural phases...\n", len(e.Plan.Steps))

	for _, step := range e.Plan.Steps {
		isInteractive := strings.Contains(step.Command, "sudo")
		wg.Add(1)

		go func(s ollama.TaskStep, interactive bool) {
			defer wg.Done()

			// Block execution thread until parent dependency channels unlock
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

			// 🚨 ADAPTIVE ELEVATION GATEWAYS
			if interactive {
				// If sudo is active, DO NOT drop credentials to 'sysagent'.
				// We keep your active user execution frame context so sudo can bind to your active keyboard.
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

			// Standard Sandboxed Track: Enforce sysagent restrictions for unprivileged tasks
			if sandboxAttr != nil {
				cmd.SysProcAttr = sandboxAttr
			}

			fmt.Printf("🔒 [Sandboxed] Phase %s running as user 'sysagent'\n", s.ID)
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
