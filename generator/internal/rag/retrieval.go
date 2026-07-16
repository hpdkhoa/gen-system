package rag

import (
	"fmt"
	"math"
	"sort"
)

// Search finds the top-K most relevant chunks for a query string.
// Uses cosine similarity between the query embedding and stored embeddings.
// Returns up to k chunk texts, or fewer if the store has fewer chunks.
func Search(query string, k int) []string {
	store := Load()
	if len(store.Chunks) == 0 {
		return nil // no docs ingested yet
	}

	queryVec, err := Embed(query)
	if err != nil || len(queryVec) == 0 {
		return nil // embedding unavailable
	}

	type scored struct {
		text  string
		score float64
	}
	var results []scored

	for _, chunk := range store.Chunks {
		if len(chunk.Embedding) == 0 {
			continue
		}
		sim := cosine(queryVec, chunk.Embedding)
		results = append(results, scored{text: chunk.Text, score: sim})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	var out []string
	for i := 0; i < k && i < len(results); i++ {
		if results[i].score > 0.3 { // minimum relevance threshold
			out = append(out, results[i].text)
		}
	}
	return out
}

// cosine computes the cosine similarity between two equal-length vectors.
func cosine(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// SearchSummary returns a formatted string of top-K results for prompt injection.
func SearchSummary(query string, k int) string {
	chunks := Search(query, k)
	if len(chunks) == 0 {
		return ""
	}
	result := fmt.Sprintf("RELEVANT FRAMEWORK DOCS (%d chunks):\n", len(chunks))
	for i, c := range chunks {
		maxLen := 400
		if len(c) > maxLen {
			c = c[:maxLen] + "…"
		}
		result += fmt.Sprintf("[%d] %s\n\n", i+1, c)
	}
	return result
}
