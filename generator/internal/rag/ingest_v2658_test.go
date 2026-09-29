// D053: IngestDir loaded the store and IngestFile appended every chunk, so a
// second ingest of the same folder stored each chunk twice and Search filled
// its top k with copies. A second ingest must replace the chunks of each
// source file.
package rag

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeDoc(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func chunkTexts(s *EmbeddingStore) []string {
	var out []string
	for _, c := range s.Chunks {
		out = append(out, c.Text)
	}
	return out
}

func TestIngestDir_SecondIngestReplaces(t *testing.T) {
	t.Chdir(t.TempDir())
	fakeEmbed(t, http.StatusOK, map[string]any{"embedding": []float64{0.5, 0.5}})
	writeDoc(t, filepath.Join("docs", "fiber", "a.md"), "Fiber routes are registered with app.Get.")
	writeDoc(t, filepath.Join("docs", "fiber", "b.md"), "Fiber middleware runs with app.Use.")

	if err := IngestDir("fiber", "./docs/fiber/"); err != nil {
		t.Fatal(err)
	}
	first := len(Load().Chunks)
	if first != 2 {
		t.Fatalf("first ingest stored %d chunks, want 2", first)
	}
	if err := IngestDir("fiber", "./docs/fiber/"); err != nil {
		t.Fatal(err)
	}
	if n := len(Load().Chunks); n != first {
		t.Fatalf("second ingest: store holds %d chunks, want %d: %q", n, first, chunkTexts(Load()))
	}
	// The same folder named another way is the same files.
	if err := IngestDir("fiber", "docs/fiber"); err != nil {
		t.Fatal(err)
	}
	if n := len(Load().Chunks); n != first {
		t.Fatalf("third ingest: store holds %d chunks, want %d", n, first)
	}
	ids := map[string]bool{}
	for _, c := range Load().Chunks {
		if ids[c.ID] {
			t.Errorf("duplicate chunk id %s", c.ID)
		}
		ids[c.ID] = true
	}
}

func TestIngestDir_ChangedFileReplacesOldText(t *testing.T) {
	t.Chdir(t.TempDir())
	fakeEmbed(t, http.StatusOK, map[string]any{"embedding": []float64{0.5, 0.5}})
	doc := filepath.Join("docs", "a.md")
	writeDoc(t, doc, "old text about routing")
	writeDoc(t, filepath.Join("other", "c.md"), "tailwind utility classes")
	if err := IngestDir("fiber", "docs"); err != nil {
		t.Fatal(err)
	}
	if err := IngestDir("tailwind", "other"); err != nil {
		t.Fatal(err)
	}
	writeDoc(t, doc, "new text about routing")
	if err := IngestDir("fiber", "docs"); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(chunkTexts(Load()), "|")
	if got != "tailwind utility classes|new text about routing" {
		t.Fatalf("store texts = %q", got)
	}
}

// A store written before v26.58 has no file on its chunks. A re-ingest must
// not add a second copy of the same text for the same source.
func TestIngestDir_OldStoreWithoutFileIsNotDoubled(t *testing.T) {
	t.Chdir(t.TempDir())
	fakeEmbed(t, http.StatusOK, map[string]any{"embedding": []float64{0.5, 0.5}})
	writeDoc(t, filepath.Join("docs", "a.md"), "Fiber routes are registered with app.Get.")
	old := &EmbeddingStore{}
	old.Add("fiber", "Fiber routes are registered with app.Get.", []float64{1, 0})
	old.Add("fiber", "a chunk from a file that is gone", []float64{1, 0})
	if err := old.Save(); err != nil {
		t.Fatal(err)
	}
	if err := IngestDir("fiber", "docs"); err != nil {
		t.Fatal(err)
	}
	if n := len(Load().Chunks); n != 2 {
		t.Fatalf("store holds %d chunks, want 2: %q", n, chunkTexts(Load()))
	}
}

// When every embed call fails, the chunks from the last good ingest stay.
func TestIngestDir_FailedEmbedKeepsOldChunks(t *testing.T) {
	t.Chdir(t.TempDir())
	fakeEmbed(t, http.StatusOK, map[string]any{"embedding": []float64{0.5, 0.5}})
	writeDoc(t, filepath.Join("docs", "a.md"), "Fiber routes are registered with app.Get.")
	if err := IngestDir("fiber", "docs"); err != nil {
		t.Fatal(err)
	}
	fakeEmbed(t, http.StatusNotFound, map[string]string{"error": "model not found"})
	if err := IngestDir("fiber", "docs"); err != nil {
		t.Fatal(err)
	}
	if n := len(Load().Chunks); n != 1 {
		t.Fatalf("store holds %d chunks after a failed re-ingest, want 1", n)
	}
}
