package main

import (
	"fmt"
	"os"
	"strings"
	"sysagent/pkg/db"
	"sysagent/pkg/ollama"
)

// handleForgetAllSearch semantically locates and bulk deletes all memories above the match threshold
func handleForgetAllSearch(store *db.Store, client *ollama.Client, query string) {
	fmt.Printf("🧹 Semantically scanning memory ledger for bulk removal of: \"%s\"...\n", query)

	queryVector, err := client.GetEmbedding(query)
	if err != nil {
		fmt.Printf("❌ Failed to generate context vector: %v\n", err)
		os.Exit(1)
	}

	// Fetch memories matching the search vector
	memories, err := store.SearchRelevantMemories(queryVector, 0.50)
	if err != nil {
		fmt.Printf("❌ History extraction failure: %v\n", err)
		os.Exit(1)
	}

	totalFound := len(memories)
	if totalFound == 0 {
		fmt.Println("ℹ️  No historical entries found matching that semantic footprint.")
		return
	}

	fmt.Printf("\n🚨 Bulk Match Warning: Found \033[1;31m%d records\033[0m matching your cleanup criteria.\n", totalFound)
	fmt.Println(strings.Repeat("─", 60))
	fmt.Println("Top target categories scheduled for removal:")

	// Show a quick scannable preview of up to 3 commands being targeted
	previewCount := 3
	if totalFound < previewCount {
		previewCount = totalFound
	}
	for i := 0; i < previewCount; i++ {
		fmt.Printf("    • [`\033[1;36m%s\033[0m`]\n", memories[i].Command)
	}
	if totalFound > 3 {
		fmt.Printf("    ... and %d more entries.\n", totalFound-3)
	}
	fmt.Println(strings.Repeat("─", 60))

	var confirmation string
	fmt.Printf("⚠️  Are you ABSOLUTELY certain you want to permanently delete all %d entries? (y/N): ", totalFound)
	fmt.Scanln(&confirmation)
	confirmation = strings.ToLower(strings.TrimSpace(confirmation))

	if confirmation != "y" && confirmation != "yes" {
		fmt.Println("🛑 Bulk operation aborted. No rows were altered.")
		return
	}

	// 🚨 Loop and purge all matched records by ID
	successCount := 0
	for _, target := range memories {
		if err := store.DeleteMemory(target.ID); err == nil {
			successCount++
		}
	}

	fmt.Printf("🗑️  Bulk purge finished successfully! Dropped \033[1;32m%d of %d\033[0m memory lines from your database ledger.\n", successCount, totalFound)
}
