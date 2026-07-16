package rag

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const ollamaEmbedURL = "http://localhost:11434/api/embeddings"
const embedModel = "nomic-embed-text"

// Embed calls Ollama's /api/embeddings endpoint to get a vector
// representation of text. Uses nomic-embed-text (small, fast).
// Returns nil if Ollama is unavailable.
func Embed(text string) ([]float64, error) {
	payload := map[string]string{
		"model":  embedModel,
		"prompt": text,
	}
	body, _ := json.Marshal(payload)

	resp, err := http.Post(ollamaEmbedURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama embeddings unavailable: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Embedding []float64 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("embed response parse: %w", err)
	}
	return result.Embedding, nil
}

// ChunkText splits a long doc into overlapping chunks of ~500 chars.
// Each chunk has ~50-char overlap with the next for context continuity.
func ChunkText(text string, size, overlap int) []string {
	words := strings.Fields(text)
	var chunks []string
	i := 0
	for i < len(words) {
		end := i + size
		if end > len(words) {
			end = len(words)
		}
		chunks = append(chunks, strings.Join(words[i:end], " "))
		i += size - overlap
		if i < 0 {
			break
		}
	}
	return chunks
}
