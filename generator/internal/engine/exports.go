// (v23.15) GenerateBackendApp now calls cleanStaleBackend(path) before emitGoMod so a reused output dir can't carry a poisoning stale backend/go.mod, backend/cmd/main.go, or memory-mode backend/internal/db from an earlier run.
// (v23.14 Phase 6) Spring-mode guard added: when GEN_BACKEND=spring this Go generator is a no-op; the Spring Boot project is emitted by backend_spring.go.
// generator/internal/engine/exports.go (v23.8): GenerateBackendApp also emits a
// trailing FK-constraint migration (genForeignKeyMigration) in postgres mode.
// v23.2: exported generators now emit
// the coherent in-memory CRUD set (backend_inmem.go) instead of the old
// mutually-inconsistent layers. GenerateModel→model, GenerateRepo→store,
// GenerateService/RenderBackend→no-op (folded into handler+store), and
// GenerateBackendApp emits go.mod + main.go from the full entity set.
package engine

import (
	"errors"

	"github.com/hpdkhoa/gen-system/generator/internal/frontend"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

// ─────────────────────────────────────────────
// Exported engine entry points for the agent executor.
// Coherent (entity, path) → void signatures producing a go-build-green,
// runnable in-memory CRUD backend (module "app").
//
// v26.58 (D029): the backend generators return the file system errors of their
// writes (joined, after every write was tried), so the executor fails the task
// instead of counting it as done. Callers that ignore the result still compile.
// ─────────────────────────────────────────────

func GenerateModel(e schema.Entity, path string) error {
	if springEnabled() {
		return nil // Spring mode emits JPA @Entity via GenerateSpringApp
	}
	return errors.Join(emitGoMod(path), genModelInMem(e, path))
}

func GenerateRepo(e schema.Entity, path string) error {
	if springEnabled() {
		return nil // Spring mode emits Spring Data repositories via GenerateSpringApp
	}
	err := genStoreInMem(e, path)
	if postgresEnabled() {
		err = errors.Join(err, genStorePostgres(e, path))
	}
	return err
}

// GenerateService is folded into the handler+store layer for the in-memory
// backend; kept as a no-op so the agent's generate_service task is harmless.
func GenerateService(e schema.Entity, path string) {}

func GenerateValidation(e schema.Entity, path string) error {
	if springEnabled() {
		return nil // Spring mode validates via DTO bean-validation annotations
	}
	return genValidation(e, path)
}

func GenerateTable(e schema.Entity, path string) {
	// No SQL table for the in-memory backend.
}

// RenderBackend is a no-op for the in-memory backend (handlers are emitted
// fully by GenerateHandler; nothing to render from templates).
func RenderBackend(e schema.Entity, path string) {}

// GenerateBackendApp emits the app-level files that need every entity:
// go.mod, the per-entity store interfaces, backend/main.go, and (when
// GEN_BACKEND=postgres) the db connector + migration runner + migrations.
func GenerateBackendApp(entities []schema.Entity, path string) error {
	if springEnabled() {
		// Phase 6: emit a Spring Boot 3.x / Java 17 Maven project instead of the
		// Go backend. Ship scripts + Dockerfile are Spring-aware (Maven build).
		// v26.59 (N07): the Spring write errors were dropped and this returned
		// nil; every part is still tried and the errors are joined.
		return errors.Join(
			GenerateSpringApp(entities, path),
			writeSpringShipScripts(path),
			GenerateSpringDockerfile(path),
		)
	}
	// v23.15: scrub stale artifacts from a reused output directory before
	// emitting anything, so an older run's files can't poison this build (a
	// leftover backend/go.mod pinning an old lib/pq with no go.sum, an obsolete
	// backend/cmd/main.go, or a memory-mode-irrelevant backend/internal/db).
	errs := []error{
		cleanStaleBackend(path),
		emitGoMod(path),
		emitStoreInterfaces(entities, path),
	}
	if postgresEnabled() {
		errs = append(errs, emitDBConnector(path), emitMigrationRunner(path))
		for i, e := range entities {
			errs = append(errs, genPGMigration(e, i+1, path))
		}
		errs = append(errs, genForeignKeyMigration(entities, path)) // v23.8: trailing FK constraints
	}
	errs = append(errs, emitMain(entities, path))
	// Phase 4-B: emit build + bundle scripts (artifact only; no deploy).
	errs = append(errs, writeShipScripts("app", path))
	// Phase 5: emit a multi-stage Dockerfile (companion to docker-compose.yml).
	errs = append(errs, GenerateDockerfile(path))
	return errors.Join(errs...)
}

func RenderFrontend(e schema.Entity, path string) {
	renderFrontend(e, path)
}

func RenderMobile(e schema.Entity, path string) {
	renderMobile(e, path)
}

// GenerateTypeScript generates the TypeScript interface file for the entity.
func GenerateTypeScript(e schema.Entity, path string) {
	frontend.GenerateTypeScript(e, path)
}
