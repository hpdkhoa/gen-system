// D052: Load dropped the json.Unmarshal error of .gen/rag_store.json. With a
// damaged file, IngestDir loaded an empty or partial store and Save wrote it
// over the file, so the chunks that did not parse were lost.
package rag

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

const damagedStore = `{"chunks": [{"id": "fiber-0", "source": "fiber", "text": "kept", "embedding": [1, 0]}, {"id": "fiber-1", "source": "fib`

func writeDamagedStore(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(".gen", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storePath, []byte(damagedStore), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIngestDir_DamagedStoreIsNotOverwritten(t *testing.T) {
	t.Chdir(t.TempDir())
	fakeEmbed(t, http.StatusOK, map[string]any{"embedding": []float64{0.5, 0.5}})
	writeDamagedStore(t)
	if err := os.MkdirAll("docs", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("docs", "a.md"), []byte("Fiber routes are registered with app.Get."), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := IngestDir("fiber", "docs"); err == nil {
		t.Error("IngestDir returned nil over a store file that did not parse")
	}
	got, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != damagedStore {
		t.Fatalf("damaged store was overwritten:\n%s", got)
	}
}

func TestSave_RefusesAfterFailedLoad(t *testing.T) {
	t.Chdir(t.TempDir())
	writeDamagedStore(t)
	s := Load()
	s.Add("fiber", "new", []float64{1})
	if err := s.Save(); err == nil {
		t.Error("Save returned nil over a store file that did not parse")
	}
	got, _ := os.ReadFile(storePath)
	if string(got) != damagedStore {
		t.Fatalf("damaged store was overwritten:\n%s", got)
	}
}

// A missing file is still an empty store that Save may create.
func TestLoad_MissingFileStillSaves(t *testing.T) {
	t.Chdir(t.TempDir())
	s := Load()
	s.Add("fiber", "x", []float64{1})
	if err := s.Save(); err != nil {
		t.Fatalf("Save on a new store: %v", err)
	}
	if n := len(Load().Chunks); n != 1 {
		t.Fatalf("reloaded %d chunks, want 1", n)
	}
}
