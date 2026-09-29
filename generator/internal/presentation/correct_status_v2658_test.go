// correct_status_v2658_test.go — v26.58 (follow up of D020): since D020,
// Correct returns an error when the correction cannot be saved. handleCorrect
// answered every error with 400, which tells the client its request was bad.
// A failed save is a server fault: it must answer 500. An unknown belief ID
// stays a 400.
package presentation

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCorrect_FailedSaveIs500(t *testing.T) {
	t.Chdir(t.TempDir())
	root := t.TempDir()
	s := buildTestServer()
	chain, _, mid, _ := callChainServer()
	s.SetRoot(root, "", "")
	s.ReplaceContext(chain.index(), nil, nil)

	var br BeliefViewResponse
	rec := getJSON(t, s, "/api/present/beliefs")
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &br) != nil {
		t.Fatalf("beliefs: status %d; body: %s", rec.Code, rec.Body.String())
	}
	var target string
	for _, b := range br.Beliefs {
		if strings.Contains(b.ID, mid) || b.Subject == mid {
			target = b.ID
		}
	}
	if target == "" {
		t.Fatal("no belief about Mid")
	}

	// A file where the .gen folder should be makes every save fail.
	gen := filepath.Join(root, ".gen")
	if err := os.RemoveAll(gen); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gen, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec = postJSON(t, s, "/api/present/beliefs/correct", BeliefCorrection{BeliefID: target, CorrectedTo: "service"})
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("failed save: status %d, want 500; body: %s", rec.Code, rec.Body.String())
	}

	rec = postJSON(t, s, "/api/present/beliefs/correct", BeliefCorrection{BeliefID: "no-such-belief", CorrectedTo: "service"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown belief: status %d, want 400", rec.Code)
	}
}
