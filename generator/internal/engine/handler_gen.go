// (v23.14 Phase 6) Spring-mode guard added: when GEN_BACKEND=spring this Go generator is a no-op; the Spring Boot project is emitted by backend_spring.go.
// generator/internal/engine/handler_gen.go — v23.8: GenerateHandler also emits
// the typed validator (genValidation) so the handler's validation import always
// resolves regardless of task ordering. v23.2: GenerateHandler now emits a
// COMPLETE CRUD handler (list/create/get/update/delete) over the in-memory
// store, plus the shared writeJSON helper and a table test — replacing the old
// one-line stub. Always overwrites so re-runs converge.
package engine

import (
	"errors"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

// GenerateHandler writes the CRUD handler, its validator and its test.
// v26.58 (D029): returns the write errors so the executor fails the task.
func GenerateHandler(e schema.Entity, path string) error {
	if springEnabled() {
		return nil // Spring mode emits @RestController via GenerateSpringApp
	}
	return errors.Join(
		genHandlerHelper(path),
		genValidation(e, path), // v23.8: ensure validator exists before handler imports it
		genHandlerInMem(e, path),
		genHandlerTest(e, path),
	)
}
