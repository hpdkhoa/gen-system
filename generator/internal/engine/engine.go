package engine

import (
	"fmt"
	"path/filepath"

	"github.com/hpdkhoa/gen-system/generator/internal/compiler"
	"github.com/hpdkhoa/gen-system/generator/internal/frontend"
	"github.com/hpdkhoa/gen-system/generator/internal/frontend/router"
	"github.com/hpdkhoa/gen-system/generator/internal/mobile"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
	"github.com/hpdkhoa/gen-system/generator/internal/schema/smartdiff"
	"github.com/hpdkhoa/gen-system/generator/internal/schema/versioning"
)

// RunAll is the main generation pipeline.
// Order:
//  1. Compile IR + evolution merge (new/old IR via CompileV2)
//  2. Incremental build plan (only changed entities rebuilt)
//  3. DB table + model
//  4. Repository + service + DTO mapper + validation
//  5. Handler (template, then mutation)
//  6. Frontend + mobile
//  7. Snapshot save
func RunAll(entities []schema.Entity, path string) {

	// ✅ FIX: Load previous IR snapshot for evolution merge
	// v26.58 (follow up 7): the snapshot of the OUTPUT folder. It came from the
	// working directory, so a stale snapshot there skipped entities in a fresh
	// output folder, and every run overwrote the caller's own snapshot.
	// v26.59 (N11): the error was dropped, so a broken snapshot read as a first
	// run. RunAll regenerated every entity of an existing app and then wrote a
	// new snapshot over the broken one. Stop before writing anything instead.
	oldIR, err := compiler.LoadIRAt(path)
	if err != nil {
		fmt.Printf("❌ Cannot read the IR snapshot %s: %v\n", filepath.Join(path, ".gen", "ir_snapshot.json"), err)
		fmt.Println("   Generation stopped and nothing was written. Fix the file, or delete it to regenerate every entity.")
		return
	}

	// Build schema file from entities slice
	sf := schema.SchemaFile{Entities: entities}

	// ✅ FIX: Compile new IR
	newIR, err := compiler.Compile(sf)
	if err != nil {
		fmt.Println("⚠️ IR compile warning:", err, "— continuing with RunAll")
	}

	// ✅ FIX: Evolution merge + incremental build plan
	_, buildPlan, _ := compiler.CompileV3(oldIR, newIR)

	// Build lookup: which entities changed
	changedSet := map[string]bool{}
	for _, name := range buildPlan.Entities {
		changedSet[name] = true
	}
	for _, name := range buildPlan.Changed {
		changedSet[name] = true
	}
	// v26.59 (N06): a first run is a folder with no previous IR. It was an
	// empty change set, but since D072 a rerun with an unchanged schema also
	// gives one, so every rerun regenerated every entity. The snapshot is
	// saved only after the entity loop, so a run that stopped before that
	// still counts as a first run next time.
	firstRun := len(oldIR.Entities) == 0

	for _, e := range entities {

		// ✅ FIX: Incremental — skip unchanged entities on subsequent runs
		if !firstRun && !changedSet[e.Name] {
			fmt.Println("⏭  Skipping unchanged entity:", e.Name)
			continue
		}

		fmt.Println("🚀 Processing:", e.Name)

		// ── DIFF + SMART MIGRATION ─────────────────────────────────
		old, err := versioning.LoadEntitySnapshot(path, e.Name)
		if err == nil {
			oldEntity := schema.Entity{
				Name:   old.Name,
				Fields: old.Fields,
			}
			diff := smartdiff.Compare(oldEntity, e)
			genSmartMigration(e.Name, diff, path)
		}

		// ── SCHEMA LAYER ───────────────────────────────────────────
		genTableWithRelations(e, path)
		genModelWithRelations(e, path)

		// ── DATA LAYER ─────────────────────────────────────────────
		genRepoQuery(e, path)
		genRepoTx(e, path)

		// ── SERVICE LAYER ──────────────────────────────────────────
		genServiceFull(e, path)
		genServiceTx(e, path)

		// ── DTO + VALIDATION ───────────────────────────────────────
		genDTOMapper(e, path)
		genValidation(e, path)

		// ── HANDLER ───────────────────────────────────────────────
		// GenerateHandler runs FIRST — creates the file with a proper
		// package declaration and import block. If the file already
		// exists it does nothing, preserving any existing logic.
		// The template renderer then fills the body if it is a new file.
		GenerateHandler(e, path)
		renderBackend(e, path)

		// ── AST MUTATION ──────────────────────────────────────────
		// Only runs after the file exists. Safely upserts route.
		// Skips AI mutation — it runs separately via: gen ai <path>
		RunMutationLayer(e, path)

		// ── FRONTEND ───────────────────────────────────────────────
		// v26.58 (follow up of D032): report a failed write, as the mobile
		// screen below does; the batch path keeps going.
		if err := frontend.GenerateForm(e, path); err != nil {
			fmt.Printf("⚠️  %v\n", err)
		}
		if err := frontend.GenerateList(e, path); err != nil {
			fmt.Printf("⚠️  %v\n", err)
		}
		renderFrontend(e, path)

		// ── MOBILE ─────────────────────────────────────────────────
		if err := mobile.GenerateScreen(e, path); err != nil {
			fmt.Printf("⚠️  %v\n", err) // v26.57 (D003): report, the batch path keeps going
		}
		renderMobile(e, path)

		// ── SNAPSHOT ───────────────────────────────────────────────
		versioning.SaveEntitySnapshot(path, e)
	}

	// ✅ FIX: Save IR snapshot for next evolution cycle
	if err := compiler.SaveIRAt(path, newIR); err != nil {
		fmt.Println("⚠️ Could not save IR snapshot:", err)
	}

	// Global router
	if err := router.GenerateRouter(entities, path); err != nil {
		fmt.Printf("⚠️  %v\n", err) // v26.58 (follow up of D034)
	}
}
