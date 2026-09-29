package rag

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

const embedModel = "nomic-embed-text"

// ollamaEmbedURL returns the embeddings endpoint, honouring OLLAMA_HOST.
// v26.58 (D051): the URL was the constant http://localhost:11434, so Embed
// ignored OLLAMA_HOST (which internal/ai honours) and no test could fake the
// endpoint. Same rules as ai.ollamaURL.
func ollamaEmbedURL() string {
	host := strings.TrimRight(os.Getenv("OLLAMA_HOST"), "/")
	if host == "" {
		host = "http://localhost:11434"
	}
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "http://" + host
	}
	return host + "/api/embeddings"
}

// Embed calls Ollama's /api/embeddings endpoint to get a vector
// representation of text. Uses nomic-embed-text (small, fast).
// Returns nil if Ollama is unavailable.
func Embed(text string) ([]float64, error) {
	payload := map[string]string{
		"model":  embedModel,
		"prompt": text,
	}
	body, _ := json.Marshal(payload)

	resp, err := http.Post(ollamaEmbedURL(), "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama embeddings unavailable: %w", err)
	}
	defer resp.Body.Close()

	// v26.58 (D051): the status was never read, so an error reply (a model
	// that is not pulled) decoded to an empty vector with a nil error, and
	// IngestFile stored and counted a chunk that Search always skips. A non 200
	// status or an empty vector is now an error, so the chunk is skipped.
	var result struct {
		Embedding []float64 `json:"embedding"`
		Error     string    `json:"error"`
	}
	if resp.StatusCode != http.StatusOK {
		_ = json.NewDecoder(resp.Body).Decode(&result)
		if result.Error != "" {
			return nil, fmt.Errorf("ollama embeddings: HTTP %d: %s", resp.StatusCode, result.Error)
		}
		return nil, fmt.Errorf("ollama embeddings: HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("embed response parse: %w", err)
	}
	if len(result.Embedding) == 0 {
		return nil, fmt.Errorf("ollama embeddings: empty embedding for model %s", embedModel)
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
