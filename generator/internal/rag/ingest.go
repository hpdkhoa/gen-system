package rag

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// IngestFile reads a file, chunks it, embeds each chunk, and adds to the store.
// Call store.Save() when finished ingesting all files.
func IngestFile(store *EmbeddingStore, source, filePath string) (int, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", filePath, err)
	}

	text := string(data)
	// Strip markdown code fences to focus on prose/API descriptions
	text = strings.ReplaceAll(text, "```", "")

	chunks := ChunkText(text, 100, 15) // ~100 words per chunk, 15-word overlap
	added := 0

	for _, chunk := range chunks {
		if strings.TrimSpace(chunk) == "" {
			continue
		}
		embedding, err := Embed(chunk)
		if err != nil {
			fmt.Printf("  [rag] ⚠️  embed failed for chunk — skipping: %v\n", err)
			continue
		}
		store.Add(source, chunk, embedding)
		added++
	}
	return added, nil
}

// IngestDir recursively ingests all .md and .txt files in a directory.
func IngestDir(source, dir string) error {
	store := Load()
	total := 0

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".md" && ext != ".txt" && ext != ".rst" {
			return nil
		}

		fmt.Printf("  [rag] Ingesting: %s\n", path)
		n, err := IngestFile(store, source, path)
		if err != nil {
			fmt.Printf("  [rag] ⚠️  %v\n", err)
			return nil // continue on single file errors
		}
		total += n
		return nil
	})
	if err != nil {
		return err
	}

	if err := store.Save(); err != nil {
		return fmt.Errorf("save store: %w", err)
	}

	fmt.Printf("  [rag] ✅ Ingested %d chunks from %s → %s\n",
		total, dir, store.Stats())
	return nil
}

// ListSources prints all ingested framework sources.
func ListSources(store *EmbeddingStore) {
	sources := map[string]int{}
	for _, c := range store.Chunks {
		sources[c.Source]++
	}
	if len(sources) == 0 {
		fmt.Println("  No frameworks ingested yet.")
		fmt.Println("  Run: gen ingest --framework fiber ./docs/fiber/")
		return
	}
	fmt.Println("  Ingested framework docs:")
	for src, count := range sources {
		fmt.Printf("    · %s (%d chunks)\n", src, count)
	}
}
