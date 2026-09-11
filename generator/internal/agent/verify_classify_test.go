// verify_classify_test.go — v26.34: the Understand verify pass reports
// internal resolution and counts external and literal calls instead of
// reporting them as unresolved project symbols. Runs without a stdlib so the
// classification, not the stdlib pass, is what is under test.
package agent

import (
	"os"
	"path/filepath"
	"testing"

	agentctx "github.com/hpdkhoa/gen-system/generator/internal/context"
)

func TestVerifyClassifiesExternalAndLiteralCalls(t *testing.T) {
	prev := LoadOpts
	LoadOpts = agentctx.LoadContextOpts{}
	t.Cleanup(func() { LoadOpts = prev })

	root := t.TempDir()
	files := map[string]string{
		"internal/store/store.go": `package store

import "sync"

type Store struct{ mu sync.Mutex }

func New() *Store { return &Store{} }

func (s *Store) Get(id int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return id
}
`,
		"internal/service/svc.go": `package service

import (
	"errors"
	"fmt"

	"app/internal/store"
)

type Service struct{ st *store.Store }

func NewService() *Service { return &Service{st: store.New()} }

func (s *Service) Run(id int) (int, error) {
	if id < 0 {
		return 0, errors.New("negative")
	}
	func() { fmt.Println("in literal") }()
	return s.st.Get(id), nil
}
`,
	}
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	rep := runUnderstandVerify(root)
	if len(rep.Findings) != 0 {
		t.Fatalf("no finding expected; every unresolved call here is external or a literal, got %+v", rep.Findings)
	}
	if rep.TotalEdges == 0 || rep.ResolvedEdges != rep.TotalEdges {
		t.Fatalf("internal edges should all resolve: %d/%d", rep.ResolvedEdges, rep.TotalEdges)
	}
	// errors.New (import with no project symbols) and s.mu.Lock (bare name no
	// project symbol defines). The deferred Unlock is a defer node, not a call
	// edge, and the call inside the literal is not a separate edge; both are
	// pre-existing extraction behaviour and not under test here.
	if rep.ExternalEdges < 2 {
		t.Fatalf("errors.New and s.mu.Lock should count as external, got %d", rep.ExternalEdges)
	}
	if rep.LiteralEdges != 1 {
		t.Fatalf("one function literal called in place, got %d", rep.LiteralEdges)
	}
	if rep.ResolutionPct() != 100 {
		t.Fatalf("internal resolution should be 100, got %.1f", rep.ResolutionPct())
	}
}
