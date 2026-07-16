package rag

import (
	"encoding/json"
	"fmt"
	"os"
)

const storePath = ".gen/rag_store.json"

// Chunk is a single unit of framework documentation with its embedding.
type Chunk struct {
	ID        string    `json:"id"`
	Source    string    `json:"source"`    // e.g. "fiber", "tailwind"
	Text      string    `json:"text"`      // the actual doc text
	Embedding []float64 `json:"embedding"` // from Ollama nomic-embed-text
}

// EmbeddingStore holds all ingested chunks in memory + on disk.
type EmbeddingStore struct {
	Chunks []Chunk `json:"chunks"`
}

// Load reads the store from disk. Returns empty store if absent.
func Load() *EmbeddingStore {
	s := &EmbeddingStore{}
	data, err := os.ReadFile(storePath)
	if err != nil {
		return s
	}
	_ = json.Unmarshal(data, s)
	return s
}

// Save persists the store to .gen/rag_store.json.
func (s *EmbeddingStore) Save() error {
	_ = os.MkdirAll(".gen", 0755)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(storePath, data, 0644)
}

// Add appends a chunk to the store (does not save — call Save() when done).
func (s *EmbeddingStore) Add(source, text string, embedding []float64) {
	s.Chunks = append(s.Chunks, Chunk{
		ID:        fmt.Sprintf("%s-%d", source, len(s.Chunks)),
		Source:    source,
		Text:      text,
		Embedding: embedding,
	})
}

// Stats returns a summary string for logging.
func (s *EmbeddingStore) Stats() string {
	sources := map[string]int{}
	for _, c := range s.Chunks {
		sources[c.Source]++
	}
	return fmt.Sprintf("%d chunks across %d sources", len(s.Chunks), len(sources))
}
