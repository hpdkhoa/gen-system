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
	// File is the doc file the chunk came from, as a clean slash path.
	// v26.58 (D053): lets a second ingest replace the chunks of that file.
	// Empty on chunks stored before v26.58.
	File string `json:"file,omitempty"`
}

// EmbeddingStore holds all ingested chunks in memory + on disk.
type EmbeddingStore struct {
	Chunks []Chunk `json:"chunks"`

	// loadErr is set when the store file exists but did not parse.
	// v26.58 (D052): Save refuses to write over such a file.
	loadErr error
}

// Load reads the store from disk. Returns empty store if absent.
func Load() *EmbeddingStore {
	s := &EmbeddingStore{}
	data, err := os.ReadFile(storePath)
	if err != nil {
		return s
	}
	// v26.58 (D052): the Unmarshal error was dropped, so a damaged file gave
	// an empty or partial store that IngestDir then saved over the file, and
	// the chunks that did not parse were lost. Warn, and keep the error so
	// Save refuses to overwrite the file.
	if err := json.Unmarshal(data, s); err != nil {
		s.loadErr = fmt.Errorf("%s did not parse: %w", storePath, err)
		fmt.Printf("  [rag] ⚠️  %v\n", s.loadErr)
	}
	return s
}

// Save persists the store to .gen/rag_store.json.
func (s *EmbeddingStore) Save() error {
	if s.loadErr != nil {
		return fmt.Errorf("refusing to overwrite: %w (fix or remove the file)", s.loadErr)
	}
	_ = os.MkdirAll(".gen", 0755)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(storePath, data, 0644)
}

// Add appends a chunk to the store (does not save — call Save() when done).
func (s *EmbeddingStore) Add(source, text string, embedding []float64) {
	// v26.58 (D053): removeFile can shrink the store, so len(s.Chunks) may name
	// an ID that a kept chunk already has. Step past IDs in use.
	n := len(s.Chunks)
	for s.hasID(fmt.Sprintf("%s-%d", source, n)) {
		n++
	}
	s.Chunks = append(s.Chunks, Chunk{
		ID:        fmt.Sprintf("%s-%d", source, n),
		Source:    source,
		Text:      text,
		Embedding: embedding,
	})
}

// hasID reports whether a chunk with this ID is in the store.
func (s *EmbeddingStore) hasID(id string) bool {
	for _, c := range s.Chunks {
		if c.ID == id {
			return true
		}
	}
	return false
}

// removeFile drops the chunks of source that came from file. Chunks stored
// before v26.58 have no file; those of source whose text is in texts are
// dropped too, so a re-ingest does not add a second copy of them.
// v26.58 (D053).
func (s *EmbeddingStore) removeFile(source, file string, texts map[string]bool) {
	kept := s.Chunks[:0]
	for _, c := range s.Chunks {
		if c.Source == source && (c.File == file || c.File == "" && texts[c.Text]) {
			continue
		}
		kept = append(kept, c)
	}
	s.Chunks = kept
}

// Stats returns a summary string for logging.
func (s *EmbeddingStore) Stats() string {
	sources := map[string]int{}
	for _, c := range s.Chunks {
		sources[c.Source]++
	}
	return fmt.Sprintf("%d chunks across %d sources", len(s.Chunks), len(sources))
}
