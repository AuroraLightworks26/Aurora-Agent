package main

import (
	"fmt"
	"os"
	"sort" // 🚨 IMPORT GO'S SORT PACKAGE
	"strings"
	"sysagent/pkg/db"
	"sysagent/pkg/ollama"
)

// handleForgetSearch semantically looks up a memory and removes it based on user confirmation
func handleForgetSearch(store *db.Store, client *ollama.Client, query string) {
	fmt.Printf("🔍 Semantically searching memory log ledger to forget: \"%s\"...\n", query)

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

	if len(memories) == 0 {
		fmt.Println("ℹ️  No historical entries found matching that semantic footprint.")
		return
	}

	// 🚨 NATIVE SORTING: Sort the slice in descending order by Similarity score
	// This forces the absolute highest percentage match straight to index 0.
	sort.Slice(memories, func(i, j int) bool {
		return memories[i].Similarity > memories[j].Similarity
	})

	// Target the guaranteed highest-matching individual entry
	target := memories[0]

	fmt.Printf("\n🎯 Highest Match Identified (Score: \033[1;35m%.2f%%\033[0m):\n", target.Similarity*100)
	fmt.Println(strings.Repeat("─", 60))
	fmt.Printf("    💻 Command: `\033[1;36m%s\033[0m`\n", target.Command)
	status := "\033[1;32m✅ SUCCESS\033[0m"
	if !target.Success {
		status = fmt.Sprintf("\033[1;31m❌ FAILED\033[0m (Error: %s)", target.ErrorMessage)
	}
	fmt.Printf("    📊 Status:  %s\n", status)
	fmt.Println(strings.Repeat("─", 60))

	// 💡 UX ENHANCEMENT: Inform the user if there are more sibling duplicates waiting in line
	if len(memories) > 1 {
		fmt.Printf("ℹ️  Note: There are %d other matching entries in the database database ledger.\n", len(memories)-1)
	}

	var confirmation string
	fmt.Print("⚠️  Are you absolutely sure you want to permanently delete this memory entry? (y/N): ")
	fmt.Scanln(&confirmation)
	confirmation = strings.ToLower(strings.TrimSpace(confirmation))

	if confirmation != "y" && confirmation != "yes" {
		fmt.Println("🛑 Operation canceled. Memory remains untouched.")
		return
	}

	err = store.DeleteMemory(target.ID)
	if err != nil {
		fmt.Printf("❌ Failed to remove entry from database: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("🗑️  Memory successfully unindexed and dropped from local database ledger.")
}
