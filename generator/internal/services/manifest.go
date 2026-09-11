// manifest.go — v25.6: the DESIGN MANIFEST — every output of the planning phase,
// captured before a single line of the app is generated.
//
// WHY. A build turns a plain-English idea into a running product across six
// layers, but until now the intermediate DESIGN — the entities, their fields and
// relations, the extracted features, and the task graph — existed only as an
// in-memory *planner.Plan that vanished after the build. This writes that whole
// pre-code design into a `design-manifest/` folder INSIDE the generated output so
// it can be reviewed and kept as a first-class artifact:
//
//	<outDir>/design-manifest/
//	  design-manifest.html   — a single self-contained reviewable document
//	  plan.json              — the raw Plan (goal, features, entities, tasks)
//	  schema.json            — just the entities, in the schema-file shape
//
// The manifest is a SIDE-ARTIFACT: the build proceeds by default (no pause). It
// is written right after planning succeeds, so even if generation later fails the
// design is preserved. Deterministic, offline, no model call.
package services

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hpdkhoa/gen-system/generator/internal/planner"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

// ManifestPaths reports where the manifest artifacts were written, for the UI and
// the build job.
type ManifestPaths struct {
	Dir      string `json:"dir"`
	HTML     string `json:"html"`
	PlanJSON string `json:"plan_json"`
	Schema   string `json:"schema_json"`
}

// WriteDesignManifest renders plan into <outDir>/design-manifest/ and returns the
// paths written. goal is the raw idea text (plan.Goal may be normalized). Errors
// are returned but a caller should treat manifest failure as non-fatal to the
// build — the design doc is valuable, not load-bearing.
func WriteDesignManifest(outDir, goal string, plan planner.Plan) (ManifestPaths, error) {
	dir := filepath.Join(outDir, "design-manifest")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return ManifestPaths{}, fmt.Errorf("create manifest dir: %w", err)
	}

	// Raw plan.json — the complete intermediate representation.
	planPath := filepath.Join(dir, "plan.json")
	if b, err := json.MarshalIndent(plan, "", "  "); err == nil {
		_ = os.WriteFile(planPath, b, 0644)
	}

	// schema.json — just the entities, in the schema-file shape the engine reads,
	// so the design can be replayed deterministically with `gen apply schema.json`.
	schemaPath := filepath.Join(dir, "schema.json")
	sf := schema.SchemaFile{Entities: plan.Entities}
	if b, err := json.MarshalIndent(sf, "", "  "); err == nil {
		_ = os.WriteFile(schemaPath, b, 0644)
	}

	// design-manifest.html — the reviewable document.
	htmlPath := filepath.Join(dir, "design-manifest.html")
	doc := renderManifestHTML(goal, plan)
	if err := os.WriteFile(htmlPath, []byte(doc), 0644); err != nil {
		return ManifestPaths{}, fmt.Errorf("write manifest html: %w", err)
	}

	return ManifestPaths{
		Dir:      dir,
		HTML:     htmlPath,
		PlanJSON: planPath,
		Schema:   schemaPath,
	}, nil
}

// renderManifestHTML builds the standalone review document from the plan. No
// external assets, no build step — matches the project's document conventions
// (dark editorial). Everything is escaped; the plan is model-derived text.
func renderManifestHTML(goal string, plan planner.Plan) string {
	var b strings.Builder
	esc := html.EscapeString

	b.WriteString(`<!DOCTYPE html><html lang="en"><head><meta charset="utf-8">`)
	b.WriteString(`<meta name="viewport" content="width=device-width,initial-scale=1">`)
	b.WriteString(`<title>Design Manifest — ` + esc(truncate(goal, 60)) + `</title>`)
	b.WriteString(`<style>
:root{--bg:#0e1116;--panel:#161b22;--panel2:#1c2230;--ink:#e7ecf3;--dim:#9aa6b8;--faint:#6c7688;--line:#262c3a;--accent:#7cc5ff;--green:#7ee0a2;--amber:#ffb454;--mag:#c78cff;}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--ink);font-family:ui-monospace,"JetBrains Mono",SFMono-Regular,Menlo,monospace;line-height:1.6;font-size:14px}
.wrap{max-width:1000px;margin:0 auto;padding:48px 32px 100px}
h1{font-size:26px;font-weight:800;letter-spacing:-.02em;margin:0 0 4px}
.sub{color:var(--dim);font-size:13px;margin-bottom:4px}
.meta{color:var(--faint);font-size:12px;border-bottom:1px solid var(--line);padding-bottom:20px;margin-bottom:32px}
h2{font-size:17px;margin:40px 0 6px;padding-top:6px}
h2 .n{color:var(--accent);margin-right:8px}
.goal{border-left:3px solid var(--accent);padding:10px 16px;background:var(--panel);border-radius:0 8px 8px 0;color:#dfe6ef;margin:16px 0}
.card{border:1px solid var(--line);border-radius:10px;background:var(--panel);padding:14px 16px;margin:12px 0}
.card h3{margin:0 0 6px;font-size:15px;color:#fff}
.card h3 .badge{font-size:10px;font-weight:700;padding:1px 7px;border-radius:20px;background:rgba(124,197,255,.14);color:var(--accent);margin-left:8px}
table{width:100%;border-collapse:collapse;margin:8px 0;font-size:12.6px}
th,td{text-align:left;padding:6px 10px;border-bottom:1px solid var(--line);vertical-align:top}
th{color:var(--faint);text-transform:uppercase;font-size:10px;letter-spacing:.7px}
tr:last-child td{border-bottom:0}
.pill{display:inline-block;font-size:11px;padding:1px 8px;border-radius:20px;background:var(--panel2);color:var(--dim);margin:2px 4px 2px 0}
.rel{color:var(--mag)}
.tasks{display:flex;flex-wrap:wrap;gap:6px;margin-top:8px}
.task{font-size:11px;border:1px solid var(--line);border-radius:6px;padding:3px 8px;background:var(--panel2);color:var(--dim)}
.task b{color:var(--green)}
.empty{color:var(--faint);font-style:italic}
.foot{margin-top:48px;border-top:1px solid var(--line);padding-top:16px;color:var(--faint);font-size:11px}
ul.notes{margin:8px 0;padding-left:18px}ul.notes li{margin:4px 0;color:#d5dce6}
</style></head><body><div class="wrap">`)

	// Header
	b.WriteString(`<h1>Design Manifest</h1>`)
	b.WriteString(`<div class="sub">Everything the system designed from your idea — before any code was generated.</div>`)
	b.WriteString(`<div class="meta">Generated ` + esc(time.Now().Format("2006-01-02 15:04")) +
		` · ` + fmt.Sprintf("%d entities · %d features · %d tasks", len(plan.Entities), len(plan.Features), len(plan.Tasks)) +
		` · deterministic, offline</div>`)

	// 1. The idea
	b.WriteString(`<h2><span class="n">1</span>The idea</h2>`)
	src := goal
	if strings.TrimSpace(src) == "" {
		src = plan.Goal
	}
	b.WriteString(`<div class="goal">` + esc(src) + `</div>`)

	// 2. Features
	b.WriteString(`<h2><span class="n">2</span>Features extracted</h2>`)
	if len(plan.Features) == 0 {
		b.WriteString(`<p class="empty">No named features were extracted; the design is entity-driven (see below).</p>`)
	} else {
		for _, f := range plan.Features {
			b.WriteString(`<div class="card"><h3>` + esc(f.Name) + `</h3>`)
			if f.Description != "" {
				b.WriteString(`<p style="color:#d5dce6;margin:0 0 6px">` + esc(f.Description) + `</p>`)
			}
			if len(f.Entities) > 0 {
				b.WriteString(`<div>`)
				for _, e := range f.Entities {
					b.WriteString(`<span class="pill">` + esc(e) + `</span>`)
				}
				b.WriteString(`</div>`)
			}
			if len(f.Actions) > 0 {
				b.WriteString(`<div style="margin-top:4px">`)
				for _, a := range f.Actions {
					b.WriteString(`<span class="pill" style="color:var(--amber)">` + esc(a) + `</span>`)
				}
				b.WriteString(`</div>`)
			}
			b.WriteString(`</div>`)
		}
	}

	// 3. Data model (schema/design)
	b.WriteString(`<h2><span class="n">3</span>Data model &amp; schema</h2>`)
	if len(plan.Entities) == 0 {
		b.WriteString(`<p class="empty">No entities were designed.</p>`)
	} else {
		for _, e := range plan.Entities {
			b.WriteString(`<div class="card"><h3>` + esc(e.Name))
			b.WriteString(`<span class="badge">` + fmt.Sprintf("%d fields", len(e.Fields)) + `</span></h3>`)
			b.WriteString(`<table><tr><th>Field</th><th>Type</th></tr>`)
			for _, fld := range e.Fields {
				b.WriteString(`<tr><td>` + esc(fld.Name) + `</td><td>` + esc(fld.Type) + `</td></tr>`)
			}
			b.WriteString(`</table>`)
			if len(e.Relations) > 0 {
				b.WriteString(`<div style="margin-top:6px">`)
				for _, r := range e.Relations {
					b.WriteString(`<span class="pill rel">` + esc(r.Type) + ` → ` + esc(r.Target))
					if r.ForeignKey != "" {
						b.WriteString(` (` + esc(r.ForeignKey) + `)`)
					}
					b.WriteString(`</span>`)
				}
				b.WriteString(`</div>`)
			}
			b.WriteString(`</div>`)
		}
	}

	// 4. Build plan (task graph)
	b.WriteString(`<h2><span class="n">4</span>Build plan</h2>`)
	if len(plan.Tasks) == 0 {
		b.WriteString(`<p class="empty">No tasks in the plan.</p>`)
	} else {
		b.WriteString(`<div class="tasks">`)
		for _, t := range plan.Tasks {
			label := t.Type
			if t.EntityName != "" {
				label += " · " + t.EntityName
			}
			b.WriteString(`<span class="task"><b>` + esc(t.ID) + `</b> ` + esc(label) + `</span>`)
		}
		b.WriteString(`</div>`)
	}

	// 5. Notes
	if len(plan.Notes) > 0 {
		b.WriteString(`<h2><span class="n">5</span>Planner notes</h2><ul class="notes">`)
		for _, n := range plan.Notes {
			b.WriteString(`<li>` + esc(n) + `</li>`)
		}
		b.WriteString(`</ul>`)
	}

	b.WriteString(`<div class="foot">Raw design is in <code>plan.json</code> (full plan) and <code>schema.json</code> ` +
		`(entities only — replay with <code>gen apply schema.json</code>). This manifest was written before code generation ran.</div>`)
	b.WriteString(`</div></body></html>`)
	return b.String()
}

// truncate shortens s to n runes with an ellipsis, for the <title>.
func truncate(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
