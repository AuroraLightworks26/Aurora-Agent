package main

import (
	"fmt"
	"os"
	"strings"
	"sysagent/pkg/db"
	"sysagent/pkg/ollama"
)

// handleHistorySearch processes semantic vector lookups against the SQLite telemetry ledger
func handleHistorySearch(store *db.Store, client *ollama.Client, query string) {
	fmt.Printf("🔍 Performing semantic history search for: \"%s\"...\n", query)

	queryVector, err := client.GetEmbedding(query)
	if err != nil {
		fmt.Printf("❌ Failed to generate search embedding vector: %v\n", err)
		os.Exit(1)
	}

	// Search records matching with a relaxed >= 0.50 score to catch broad conceptual contexts
	memories, err := store.SearchRelevantMemories(queryVector, 0.50)
	if err != nil {
		fmt.Printf("❌ History search error: %v\n", err)
		os.Exit(1)
	}

	if len(memories) == 0 {
		fmt.Println("ℹ️  No relevant historical entries found matching that context.")
		return
	}

	fmt.Printf("\n📚 Found %d Match(es) inside Agent Knowledge Ledger:\n", len(memories))
	fmt.Println(strings.Repeat("─", 60))

	for i, mem := range memories {
		status := "\033[1;32m✅ SUCCESS\033[0m"
		if !mem.Success {
			status = fmt.Sprintf("\033[1;31m❌ FAILED\033[0m (Error: %s)", mem.ErrorMessage)
		}

		fmt.Printf("[%d] Match Score: \033[1;35m%.2f%%\033[0m\n", i+1, mem.Similarity*100)
		fmt.Printf("    💻 Command: `\033[1;36m%s\033[0m`\n", mem.Command)
		fmt.Printf("    📊 Status:  %s\n", status)
		fmt.Println(strings.Repeat("─", 60))
	}
}
