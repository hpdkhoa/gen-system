package cli

import (
	"fmt"
	"os"

	"gen-system/generator/internal/rag"
)

// Ingest handles `gen ingest --framework <name> <docs-dir>`.
// Reads all .md/.txt files in the directory, embeds them with
// Ollama nomic-embed-text, and saves to .gen/rag_store.json.
//
// Usage:
//
//	gen ingest --framework fiber ./docs/fiber/
//	gen ingest --framework tailwind ./tailwind-docs/
//	gen ingest --list
func Ingest() {
	args := os.Args[2:]

	if len(args) == 0 || (len(args) == 1 && args[0] == "--list") {
		store := rag.Load()
		rag.ListSources(store)
		return
	}

	framework := "unknown"
	var dir string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--framework":
			if i+1 < len(args) {
				i++
				framework = args[i]
			}
		default:
			dir = args[i]
		}
	}

	if dir == "" {
		fmt.Println("Usage: gen ingest --framework <name> <docs-dir>")
		fmt.Println("       gen ingest --list")
		return
	}

	if _, err := os.Stat(dir); err != nil {
		fmt.Printf("❌ Directory not found: %s\n", dir)
		return
	}

	fmt.Printf("📚 Ingesting docs for framework: %s\n", framework)
	fmt.Printf("   Directory: %s\n", dir)
	fmt.Println("   Embedding model: nomic-embed-text (Ollama)")
	fmt.Println("   Requires: ollama pull nomic-embed-text")
	fmt.Println("")

	if err := rag.IngestDir(framework, dir); err != nil {
		fmt.Printf("❌ Ingest failed: %s\n", err)
		return
	}

	fmt.Printf("\n✅ Docs ingested and saved to .gen/rag_store.json\n")
	fmt.Printf("   These will now be retrieved automatically during repairs.\n")
}
