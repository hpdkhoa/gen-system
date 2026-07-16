// (v23.14 Phase 6) Spring-mode guard added: when GEN_BACKEND=spring this Go generator is a no-op; the Spring Boot project is emitted by backend_spring.go.
// generator/internal/engine/handler_gen.go — v23.8: GenerateHandler also emits
// the typed validator (genValidation) so the handler's validation import always
// resolves regardless of task ordering. v23.2: GenerateHandler now emits a
// COMPLETE CRUD handler (list/create/get/update/delete) over the in-memory
// store, plus the shared writeJSON helper and a table test — replacing the old
// one-line `// TODO: implement` stub. Always overwrites so re-runs converge.
package engine

import (
	"gen-system/generator/internal/schema"
)

func GenerateHandler(e schema.Entity, path string) {
	if springEnabled() {
		return // Spring mode emits @RestController via GenerateSpringApp
	}
	genHandlerHelper(path)
	genValidation(e, path) // v23.8: ensure validator exists before handler imports it
	genHandlerInMem(e, path)
	genHandlerTest(e, path)
}
