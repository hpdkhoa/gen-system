package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gen-system/generator/internal/ai"
	"gen-system/generator/internal/schema"
)

// badProvider emits the failure modes seen in the field with a real model:
//   - uses fmt/time without importing them (auto-import must add them)
//   - misuses the store contract: treats Get's bool as an error (must be stubbed)
type badProvider struct{}

func (badProvider) Operations(idea string, e schema.Entity, all []schema.Entity) ([]ai.Operation, error) {
	return []ai.Operation{
		{ // GOOD body: uses fmt + time but doesn't import them -> auto-import fixes
			Name: "Touch", HTTPMethod: "POST",
			Body: "_ = fmt.Sprintf(\"%v\", time.Now())\nreturn map[string]any{\"ok\": true}, nil",
		},
		{ // BAD body: store.Get returns (T,bool), this treats it as error -> stub
			Name: "BrokenGet", HTTPMethod: "POST",
			Body: "o, err := s.store.Get(id)\nif err != nil {\nreturn nil, err\n}\nreturn o, nil",
		},
		{ // GOOD body: correct (T,bool) usage with fmt
			Name: "GoodGet", HTTPMethod: "POST",
			Body: "o, ok := s.store.Get(id)\nif !ok {\nreturn nil, fmt.Errorf(\"not found: %d\", id)\n}\nreturn o, nil",
		},
	}, nil
}

func TestServiceLogic_BadBodiesAlwaysCompile(t *testing.T) {
	dir := t.TempDir()
	e := schema.Entity{Name: "Discount"}
	GenerateServiceLogic("a discounts app", e, []schema.Entity{e}, badProvider{}, dir)

	svc := filepath.Join(dir, "backend/internal/service/discount_service.go")
	data, err := os.ReadFile(svc)
	if err != nil {
		t.Fatalf("service file not written: %v", err)
	}
	src := string(data)

	// auto-import must have added fmt and time
	if !strings.Contains(src, "\"fmt\"") {
		t.Error("expected fmt import to be auto-derived")
	}
	if !strings.Contains(src, "\"time\"") {
		t.Error("expected time import to be auto-derived")
	}
	// the broken (T,bool)-as-error op must be stubbed
	if !strings.Contains(src, "not implemented: BrokenGet") {
		t.Error("BrokenGet should have been dropped to a not-implemented stub")
	}
	// the good ops must survive (not stubbed)
	if strings.Contains(src, "not implemented: Touch") {
		t.Error("Touch is valid and must not be stubbed")
	}
	if strings.Contains(src, "not implemented: GoodGet") {
		t.Error("GoodGet is valid and must not be stubbed")
	}
}
