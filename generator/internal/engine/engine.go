package engine

import (
	"fmt"

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
	oldIR, _ := compiler.LoadPreviousIR()

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
	// First run: changedSet is empty — treat ALL as new
	firstRun := len(changedSet) == 0

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
		frontend.GenerateForm(e, path)
		frontend.GenerateList(e, path)
		renderFrontend(e, path)

		// ── MOBILE ─────────────────────────────────────────────────
		mobile.GenerateScreen(e, path)
		renderMobile(e, path)

		// ── SNAPSHOT ───────────────────────────────────────────────
		versioning.SaveEntitySnapshot(path, e)
	}

	// ✅ FIX: Save IR snapshot for next evolution cycle
	if err := compiler.SaveIR(newIR); err != nil {
		fmt.Println("⚠️ Could not save IR snapshot:", err)
	}

	// Global router
	router.GenerateRouter(entities, path)
}
