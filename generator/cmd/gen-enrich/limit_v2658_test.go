// D016: gen-enrich with --limit N filled the body map only from the first N
// routines. A changed routine outside the limit had no body, so the final
// rebuild skipped its stale check and kept the old description with Stale false.
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/belief"
	gencontext "github.com/hpdkhoa/gen-system/generator/internal/context"
)

func TestD016_LimitKeepsStaleCheckForAllRoutines(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "main.go")
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("package main\n\nfunc Alpha() int { return 1 }\n\nfunc Beta() int { return 2 }\n\nfunc main() {}\n")

	// First run with no limit records the description and body of Beta.
	ctx := gencontext.LoadContext(root)
	all, _ := routinesAndBodies(&ctx, 0)
	var betaID, betaBody string
	for _, r := range all {
		if r.Name == "Beta" {
			betaID, betaBody = r.SymbolID, r.Body
		}
	}
	if betaID == "" || betaBody == "" {
		t.Fatalf("Beta not found or empty body: %+v", all)
	}
	store := belief.NewStore(root)
	store.UpsertEnrichBeliefs([]belief.EnrichInput{{SymbolID: betaID, Subject: "Beta", Kind: belief.KindComputation, Object: "returns two", Body: betaBody}})

	// Change Beta, then run with --limit 1 so Beta is outside the limit.
	write("package main\n\nfunc Alpha() int { return 1 }\n\nfunc Beta() int { return 3 }\n\nfunc main() {}\n")
	ctx = gencontext.LoadContext(root)
	run, bodies := routinesAndBodies(&ctx, 1)
	if len(run) != 1 {
		t.Fatalf("limit 1 gave %d routines to run", len(run))
	}
	for _, r := range run {
		if r.SymbolID == betaID {
			t.Fatalf("test setup: Beta is inside the limit")
		}
	}
	store = belief.NewStore(root)
	store.SetBodyProvider(func(id string) string { return bodies[id] })
	store.RebuildFromIndex(ctx.Index, ctx.Effects)

	found := false
	for _, b := range store.All() {
		if b.Source == belief.SourceEnrich && b.Subject == "Beta" {
			found = true
			if !b.Stale {
				t.Errorf("Beta changed but its enrich belief is not stale: %+v", b)
			}
		}
	}
	if !found {
		t.Fatal("enrich belief for Beta missing after rebuild")
	}
}
