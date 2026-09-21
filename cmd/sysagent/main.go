package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sysagent/pkg/db"
	"sysagent/pkg/ollama"
	"syscall"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// 🚨 BACKGROUND SIGNAL MONITOR: Capture termination requests safely
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\n🛑 Early termination requested. Cleaning up model processes...")

		// Instantly notify local Ollama daemon to unload memory models
		stopPayload := map[string]interface{}{
			"model":      "qwen2.5-coder:14b",
			"keep_alive": "0s",
		}
		jsonData, _ := json.Marshal(stopPayload)
		_, _ = http.Post("http://localhost:11434/api/generate", "application/json", bytes.NewBuffer(jsonData))

		stopPayload["model"] = "qwen2.5-coder:1.5b-base"
		jsonData, _ = json.Marshal(stopPayload)
		_, _ = http.Post("http://localhost:11434/api/generate", "application/json", bytes.NewBuffer(jsonData))

		os.Exit(130) // Standard Linux script abort exit status code status
	}()

	store, err := db.NewStore("agent_knowledge.db")
	if err != nil {
		fmt.Printf("Database initialization critical failure: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	client := ollama.NewClient("qwen2.5-coder:14b")

	switch os.Args[1] {
	case "--history":
		if len(os.Args) < 3 {
			fmt.Println("Error: Please provide a search query. Usage: sysagent --history \"your search term\"")
			os.Exit(1)
		}
		searchQuery := strings.Join(os.Args[2:], " ")
		handleHistorySearch(store, client, searchQuery)
		return

	case "--forget":
		if len(os.Args) < 3 {
			fmt.Println("Error: Please provide a target term. Usage: sysagent --forget \"memory context to wipe\"")
			os.Exit(1)
		}
		targetQuery := strings.Join(os.Args[2:], " ")
		handleForgetSearch(store, client, targetQuery)
		return

	case "--forget-all":
		if len(os.Args) < 3 {
			fmt.Println("Error: Please provide a target query. Usage: sysagent --forget-all \"context loop to clean\"")
			os.Exit(1)
		}
		targetQuery := strings.Join(os.Args[2:], " ")
		handleForgetAllSearch(store, client, targetQuery)
		return
	}

	userInstruction := strings.Join(os.Args[1:], " ")
	handlePipelineExecution(store, client, userInstruction)
}

func printUsage() {
	fmt.Println("Usage:")
	fmt.Println("  sysagent \"your high level operational instruction\"  (Runs an ad-hoc pipeline task)")
	fmt.Println("  sysagent --history \"your search query\"              (Semantically queries past logs)")
	fmt.Println("  sysagent --forget \"memory to wipe\"                 (Deletes a specific historical record)")
	fmt.Println("  sysagent --forget-all \"memory to wipe\"             (Bulk deletes all matching records)")
}
