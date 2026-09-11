// backend_inmem.go — v23.27 (task E): store Create returns (model,error); the
// collection POST handler now returns 409 when Create fails (e.g. FK violation),
// instead of swallowing the DB error and falsely returning 201.
// generator/internal/engine/backend_inmem.go — v23.15: emitGoMod now ALWAYS
// (re)writes go.mod for the current mode instead of early-returning on a
// pre-existing file, and removes a stale go.sum in memory mode; NEW
// cleanStaleBackend() scrubs obsolete artifacts (stale backend/go.mod+go.sum,
// backend/cmd/main.go, memory-mode backend/internal/db) from a reused output
// dir. This fixes "missing go.sum entry for go.mod file (lib/pq v1.10.9)" and
// the wrong-import-path cmd/main.go that aborted generation when regenerating
// into a directory a previous (postgres-mode) run had written.
// v23.11 (Phase 1): postgres mode
// now pins lib/pq v1.12.3 (was v1.10.9 — a version with no go.sum anywhere on
// the box) AND emits a matching go.sum next to go.mod, so the generated app
// builds OFFLINE without `go mod download`. This fixes the "missing go.sum
// entry for go.mod file" failure that aborted generation in postgres mode.
// v23.10 (Phase 4-B): emitMain now
// constructs ONE store per entity and injects it into BOTH the CRUD handler and
// the business-ops service handler (shared state); GenerateBackendApp also writes
// the build+bundle ship scripts. v23.8: fieldsForEntity now also
// folds belongsTo relations into int FK fields (relationFields), so model +
// in-mem store + PG store + migration all carry e.g. UserID/user_id. v23.2.2/B:
// coherent in-memory + optional postgres (see backend_postgres.go); CRUD
// backend generator. Field/entity names are now sanitized into valid Go
// identifiers (exportName/typeName) and clean routes/JSON tags (routeName/
// jsonTag), fixing the "more than one character in rune literal" / "unexpected
// literal 'due date'" failure when the model emits names with spaces.
package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

// goType maps schema field types to Go types for the generated model.
func goType(t string) string {
	switch strings.ToLower(t) {
	case "int", "integer":
		return "int"
	case "bool", "boolean":
		return "bool"
	case "float", "float64", "double", "decimal":
		return "float64"
	case "time", "datetime", "date", "timestamp":
		return "string" // ISO-8601 string keeps the in-mem app dependency-free
	default:
		return "string"
	}
}

func emitGoMod(path string) {
	p := filepath.Join(path, "go.mod")
	os.MkdirAll(path, 0755)

	// v23.15: ALWAYS (re)write go.mod to match the CURRENT mode — never trust a
	// pre-existing one. Previously this early-returned if go.mod existed, so a
	// directory reused across runs (or across modes) kept whatever go.mod the
	// *earlier* run wrote. A stale postgres-mode go.mod pinning an old lib/pq
	// (e.g. v1.10.9) with NO matching go.sum then poisoned every later build
	// with "missing go.sum entry for go.mod file" — exactly the failure seen in
	// the field. Rewriting unconditionally keeps go.mod + go.sum in lock-step
	// with the mode actually being generated.
	mod := "module app\n\ngo 1.22\n"
	sumPath := filepath.Join(path, "go.sum")
	if postgresEnabled() {
		// v23.11: pin the SAME lib/pq version the gen-system parent module
		// vendors (v1.12.3). v1.10.9's checksum is on no offline box, so it
		// failed `go vet`/`go build` with "missing go.sum entry". Keeping
		// versions in lock-step means the hashes below are guaranteed available
		// wherever gen-system itself built.
		mod += "\nrequire github.com/lib/pq v1.12.3\n"
		os.WriteFile(p, []byte(mod), 0644)

		// v23.11: emit a matching go.sum so the project builds OFFLINE (no
		// `go mod download`, no proxy). These two lines are the authoritative
		// hashes for lib/pq v1.12.3 — identical to the gen-system parent go.sum,
		// which builds clean. lib/pq is the sole external dep (postgres mode).
		sum := "github.com/lib/pq v1.12.3 h1:tTWxr2YLKwIvK90ZXEw8GP7UFHtcbTtty8zsI+YjrfQ=\n" +
			"github.com/lib/pq v1.12.3/go.mod h1:/p+8NSbOcwzAEI7wiMXFlgydTwcgTr3OSKMsD2BitpA=\n"
		os.WriteFile(sumPath, []byte(sum), 0644)
		return
	}

	// Memory mode: no external requires, so go.mod has none and go.sum must be
	// ABSENT. v23.15: actively remove a stale go.sum left by a prior
	// postgres-mode run in this same directory — otherwise an orphaned go.sum
	// referencing lib/pq trips the build even though nothing imports it.
	os.WriteFile(p, []byte(mod), 0644)
	os.Remove(sumPath)
}

// cleanStaleBackend removes backend artifacts that the CURRENT generator no
// longer produces but an OLDER run (or a different mode) may have left in a
// reused output directory. Without this, a directory generated into more than
// once accumulates conflicting files that break `go vet`/`go build`:
//
//   - backend/go.mod + backend/go.sum: a pre-v23.2 per-backend module file that
//     shadows the single project-root go.mod this generator writes. `go vet`
//     runs inside backend/, finds the stale (possibly v1.10.9, go.sum-less)
//     module file, and fails before the root module is ever consulted.
//   - backend/cmd/main.go: an obsolete SECOND `package main` (old convention,
//     imported "backend/internal/db") — the current generator emits only
//     backend/main.go, so this is a dangling, wrong-import-path duplicate.
//   - backend/internal/db/: the postgres connector + migrations. In MEMORY mode
//     nothing imports it, but it still imports lib/pq and drags the dep into the
//     build graph; remove it so memory-mode output is truly dependency-free.
//     (Postgres mode regenerates this tree, so we only prune it when memory.)
//
// v23.15.
func cleanStaleBackend(path string) {
	be := filepath.Join(path, "backend")

	// Stale per-backend module files (old layout). The current layout keeps a
	// single go.mod/go.sum at the project root (path/go.mod).
	os.Remove(filepath.Join(be, "go.mod"))
	os.Remove(filepath.Join(be, "go.sum"))

	// Obsolete second entrypoint; only backend/main.go is current.
	os.RemoveAll(filepath.Join(be, "cmd"))

	// Postgres-only connector tree: keep it in postgres mode (it's regenerated),
	// drop it in memory mode (it would otherwise pull in lib/pq unused).
	if !postgresEnabled() {
		os.RemoveAll(filepath.Join(be, "internal", "db"))
	}
}

// genModelInMem writes backend/internal/model/<lower>.go — a plain struct with
// an ID and JSON tags. Deterministic field order (schema order).
func genModelInMem(e schema.Entity, path string) {
	n := typeName(e.Name)
	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by gen-system. DO NOT EDIT.\npackage model\n\n")
	fmt.Fprintf(&b, "type %s struct {\n", n)
	fmt.Fprintf(&b, "\tID int `json:\"id\"`\n")
	// v23.2.2: skip any model-supplied field that collides with the built-in
	// ID (by identifier OR json tag) or with an earlier field — otherwise
	// `go vet` rejects duplicate struct fields / repeated json tags (the model
	// often emits its own "id"). Comparison is case-insensitive.
	usedIdent := map[string]bool{"id": true}
	usedTag := map[string]bool{"id": true}
	for _, f := range fieldsForEntity(e) {
		ident := exportName(f.Name)
		tag := jsonTag(f.Name)
		if usedIdent[strings.ToLower(ident)] || usedTag[strings.ToLower(tag)] {
			continue
		}
		usedIdent[strings.ToLower(ident)] = true
		usedTag[strings.ToLower(tag)] = true
		fmt.Fprintf(&b, "\t%s %s `json:\"%s\"`\n", ident, goType(f.Type), tag)
	}
	b.WriteString("}\n")
	writeFileMk(filepath.Join(path, "backend/internal/model/"+routeName(e.Name)+".go"), b.String())
}

// fieldsForEntity returns the entity's fields filtered to those usable as Go
// struct fields, preserving order. Shared by model and PG store so they agree
// on the exact set of columns (avoids INSERT/SELECT mismatches). It drops
// fields colliding with the built-in ID or with an earlier field.
//
// v23.8: belongsTo relations now contribute an int foreign-key field (e.g.
// belongsTo User -> UserID int `json:"user_id"`), appended after the explicit
// fields and run through the SAME dedup so the FK appears exactly once in the
// model struct, the in-memory store, the PG store, and the SQL migration.
func fieldsForEntity(e schema.Entity) []schema.Field {
	var out []schema.Field
	usedIdent := map[string]bool{"id": true}
	usedTag := map[string]bool{"id": true}
	add := func(f schema.Field) {
		ident := strings.ToLower(exportName(f.Name))
		tag := strings.ToLower(jsonTag(f.Name))
		if ident == "" || usedIdent[ident] || usedTag[tag] {
			return
		}
		usedIdent[ident] = true
		usedTag[tag] = true
		out = append(out, f)
	}
	for _, f := range e.Fields {
		add(f)
	}
	for _, f := range relationFields(e) {
		add(f)
	}
	return out
}

// relationFields derives synthetic foreign-key fields from an entity's
// belongsTo relations. Each FK is an int column. The field name comes from the
// relation's ForeignKey (falling back to <Target>ID), normalised through
// splitCamel so a camelCase name like "UserID" yields UserID + json:"user_id"
// (splitIdent alone can't split a delimiter-free camelCase word).
// hasMany/other relation types add no column on this side (the FK lives on the
// child entity).
func relationFields(e schema.Entity) []schema.Field {
	var out []schema.Field
	for _, r := range e.Relations {
		if !strings.EqualFold(r.Type, "belongsTo") {
			continue
		}
		name := r.ForeignKey
		if strings.TrimSpace(name) == "" {
			if strings.TrimSpace(r.Target) == "" {
				continue
			}
			name = r.Target + "ID"
		}
		out = append(out, schema.Field{Name: fkFieldName(name), Type: "int"})
	}
	return out
}

// fkFieldName is the canonical Go-ish field name for a foreign key, used when
// synthesizing the relation column field. genTable/genMigration then turn this
// into the SQL column via jsonTag, so this is the SINGLE point that decides the
// synthesized FK field shape. Kept as splitCamel(name) to preserve the
// historical column spelling "user_id" for "UserID".
func fkFieldName(name string) string { return splitCamel(name) }

// resolveFKColumn returns the SQL column name the CREATE TABLE actually emitted
// for the foreign key whose raw key is `name`. genTable names columns
// jsonTag(f.Name) over fieldsForEntity(e), which dedupes explicit vs synthesized
// fields. We mirror that exactly: find the emitted field whose column matches
// the FK (comparing on the normalized jsonTag of both the synthesized and the
// raw forms) and return its column; fall back to the synthesized spelling when
// no explicit field is present. This guarantees the FK migration references a
// column that exists, whether the FK came from a relation or an explicit field.
// v23.26 (task E).
func resolveFKColumn(e schema.Entity, name string) string {
	synth := jsonTag(fkFieldName(name)) // e.g. "user_id"
	raw := jsonTag(name)                // e.g. "userid" (explicit field spelling)
	for _, f := range fieldsForEntity(e) {
		col := jsonTag(f.Name)
		if col == synth || col == raw {
			return col
		}
	}
	return synth
}

// splitCamel inserts a space at lower→upper and letter→ID-acronym boundaries so
// downstream splitIdent/jsonTag produce snake_case ("UserID" -> "User ID" ->
// json "user_id"; "OrderItemID" -> "order_item_id"). Already-delimited names
// (spaces, underscores) pass through unchanged because those delimiters survive.
func splitCamel(s string) string {
	var b strings.Builder
	rs := []rune(s)
	for i, r := range rs {
		if i > 0 {
			prev := rs[i-1]
			upper := r >= 'A' && r <= 'Z'
			prevLower := prev >= 'a' && prev <= 'z'
			prevDigit := prev >= '0' && prev <= '9'
			// boundary: lower|digit -> Upper  (userID, user2ID)
			if upper && (prevLower || prevDigit) {
				b.WriteByte(' ')
			} else if upper && i+1 < len(rs) {
				// boundary inside an acronym before a trailing word: "IDs" stays,
				// but "HTTPServer" -> "HTTP Server" (Upper followed by lower).
				next := rs[i+1]
				prevUpper := prev >= 'A' && prev <= 'Z'
				if prevUpper && next >= 'a' && next <= 'z' {
					b.WriteByte(' ')
				}
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

// genStoreInMem writes backend/internal/store/<lower>.go — a thread-safe
// in-memory CRUD store with auto-incrementing IDs.
func genStoreInMem(e schema.Entity, path string) {
	n := typeName(e.Name)
	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by gen-system. DO NOT EDIT.\npackage store\n\n")
	fmt.Fprintf(&b, "import (\n\t\"sort\"\n\t\"sync\"\n\n\t\"app/backend/internal/model\"\n)\n\n")
	fmt.Fprintf(&b, "type %sStore struct {\n\tmu   sync.RWMutex\n\tdata map[int]model.%s\n\tnext int\n}\n\n", n, n)
	fmt.Fprintf(&b, "func New%sStore() *%sStore {\n\treturn &%sStore{data: map[int]model.%s{}, next: 1}\n}\n\n", n, n, n, n)
	// Create — v23.27 (task E): returns (model, error) so the pg store can
	// surface DB errors (e.g. FK violations) to the handler. In-memory never
	// errors, so it returns nil.
	fmt.Fprintf(&b, "func (s *%sStore) Create(m model.%s) (model.%s, error) {\n\ts.mu.Lock()\n\tdefer s.mu.Unlock()\n\tm.ID = s.next\n\ts.next++\n\ts.data[m.ID] = m\n\treturn m, nil\n}\n\n", n, n, n)
	// List (sorted by ID for determinism)
	fmt.Fprintf(&b, "func (s *%sStore) List() []model.%s {\n\ts.mu.RLock()\n\tdefer s.mu.RUnlock()\n\tout := make([]model.%s, 0, len(s.data))\n\tfor _, v := range s.data {\n\t\tout = append(out, v)\n\t}\n\tsort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })\n\treturn out\n}\n\n", n, n, n)
	// Get
	fmt.Fprintf(&b, "func (s *%sStore) Get(id int) (model.%s, bool) {\n\ts.mu.RLock()\n\tdefer s.mu.RUnlock()\n\tv, ok := s.data[id]\n\treturn v, ok\n}\n\n", n, n)
	// Update
	fmt.Fprintf(&b, "func (s *%sStore) Update(id int, m model.%s) (model.%s, bool) {\n\ts.mu.Lock()\n\tdefer s.mu.Unlock()\n\tif _, ok := s.data[id]; !ok {\n\t\treturn model.%s{}, false\n\t}\n\tm.ID = id\n\ts.data[id] = m\n\treturn m, true\n}\n\n", n, n, n, n)
	// Delete
	fmt.Fprintf(&b, "func (s *%sStore) Delete(id int) bool {\n\ts.mu.Lock()\n\tdefer s.mu.Unlock()\n\tif _, ok := s.data[id]; !ok {\n\t\treturn false\n\t}\n\tdelete(s.data, id)\n\treturn true\n}\n", n)
	_ = sort.Ints
	writeFileMk(filepath.Join(path, "backend/internal/store/"+routeName(e.Name)+".go"), b.String())
}

// genHandlerInMem writes backend/internal/handler/<Name>_handler.go — full CRUD
// HTTP handlers over the store. Routes are registered via Register<Name>Routes.
func genHandlerInMem(e schema.Entity, path string) {
	n := typeName(e.Name)
	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by gen-system. DO NOT EDIT.\npackage handler\n\n")
	fmt.Fprintf(&b, "import (\n\t\"encoding/json\"\n\t\"net/http\"\n\t\"strconv\"\n\t\"strings\"\n\n\t\"app/backend/internal/model\"\n\t\"app/backend/internal/store\"\n\t\"app/backend/internal/validation\"\n)\n\n")
	fmt.Fprintf(&b, "type %sHandler struct{ store store.%sCRUD }\n\n", n, n)
	fmt.Fprintf(&b, "// New%sHandler uses the in-memory store (default, used by tests).\nfunc New%sHandler() *%sHandler { return &%sHandler{store: store.New%sStore()} }\n\n", n, n, n, n, n)
	fmt.Fprintf(&b, "// New%sHandlerWith injects any store implementing %sCRUD.\nfunc New%sHandlerWith(s store.%sCRUD) *%sHandler { return &%sHandler{store: s} }\n\n", n, n, n, n, n, n)
	// Register routes onto a ServeMux. Collection + item paths.
	coll := collPath(e)
	fmt.Fprintf(&b, "func (h *%sHandler) Register(mux *http.ServeMux) {\n", n)
	fmt.Fprintf(&b, "\tmux.HandleFunc(\"%s\", h.collection)\n", coll)
	fmt.Fprintf(&b, "\tmux.HandleFunc(\"%s/\", h.item)\n}\n\n", coll)
	// collection: GET list, POST create
	fmt.Fprintf(&b, "func (h *%sHandler) collection(w http.ResponseWriter, r *http.Request) {\n\tswitch r.Method {\n\tcase http.MethodGet:\n\t\twriteJSON(w, http.StatusOK, h.store.List())\n\tcase http.MethodPost:\n\t\tvar m model.%s\n\t\tif err := json.NewDecoder(r.Body).Decode(&m); err != nil {\n\t\t\thttp.Error(w, \"bad json\", http.StatusBadRequest)\n\t\t\treturn\n\t\t}\n\t\tif err := validation.Validate%s(m); err != nil {\n\t\t\thttp.Error(w, err.Error(), http.StatusBadRequest)\n\t\t\treturn\n\t\t}\n\t\tcreated, err := h.store.Create(m)\n\t\tif err != nil {\n\t\t\thttp.Error(w, err.Error(), http.StatusConflict)\n\t\t\treturn\n\t\t}\n\t\twriteJSON(w, http.StatusCreated, created)\n\tdefault:\n\t\thttp.Error(w, \"method not allowed\", http.StatusMethodNotAllowed)\n\t}\n}\n\n", n, n, n)
	// item: GET one, PUT update, DELETE
	fmt.Fprintf(&b, "func (h *%sHandler) item(w http.ResponseWriter, r *http.Request) {\n\tidStr := strings.TrimPrefix(r.URL.Path, \"%s/\")\n\tid, err := strconv.Atoi(idStr)\n\tif err != nil {\n\t\thttp.Error(w, \"bad id\", http.StatusBadRequest)\n\t\treturn\n\t}\n\tswitch r.Method {\n\tcase http.MethodGet:\n\t\tif v, ok := h.store.Get(id); ok {\n\t\t\twriteJSON(w, http.StatusOK, v)\n\t\t} else {\n\t\t\thttp.Error(w, \"not found\", http.StatusNotFound)\n\t\t}\n\tcase http.MethodPut:\n\t\tvar m model.%s\n\t\tif err := json.NewDecoder(r.Body).Decode(&m); err != nil {\n\t\t\thttp.Error(w, \"bad json\", http.StatusBadRequest)\n\t\t\treturn\n\t\t}\n\t\tif err := validation.Validate%s(m); err != nil {\n\t\t\thttp.Error(w, err.Error(), http.StatusBadRequest)\n\t\t\treturn\n\t\t}\n\t\tif v, ok := h.store.Update(id, m); ok {\n\t\t\twriteJSON(w, http.StatusOK, v)\n\t\t} else {\n\t\t\thttp.Error(w, \"not found\", http.StatusNotFound)\n\t\t}\n\tcase http.MethodDelete:\n\t\tif h.store.Delete(id) {\n\t\t\tw.WriteHeader(http.StatusNoContent)\n\t\t} else {\n\t\t\thttp.Error(w, \"not found\", http.StatusNotFound)\n\t\t}\n\tdefault:\n\t\thttp.Error(w, \"method not allowed\", http.StatusMethodNotAllowed)\n\t}\n}\n\n", n, coll, n, n)
	writeFileMk(filepath.Join(path, "backend/internal/handler/"+typeName(e.Name)+"_handler.go"), b.String())
}

// genHandlerHelper writes the shared writeJSON helper once.
func genHandlerHelper(path string) {
	p := filepath.Join(path, "backend/internal/handler/handler.go")
	if _, err := os.Stat(p); err == nil {
		return
	}
	src := "// Code generated by gen-system. DO NOT EDIT.\npackage handler\n\nimport (\n\t\"encoding/json\"\n\t\"net/http\"\n)\n\nfunc writeJSON(w http.ResponseWriter, status int, v interface{}) {\n\tw.Header().Set(\"Content-Type\", \"application/json\")\n\tw.WriteHeader(status)\n\t_ = json.NewEncoder(w).Encode(v)\n}\n"
	writeFileMk(p, src)
}

// genHandlerTest writes a table test that exercises create→list→get→delete for
// the entity, hitting the handlers via httptest (no network, no DB).
func genHandlerTest(e schema.Entity, path string) {
	n := typeName(e.Name)
	coll := collPath(e)
	// v23.8: build a JSON body that satisfies Validate<n> (every required,
	// non-FK string/int field gets a non-zero value), so create returns 201 now
	// that the handler validates. FK and bool/float fields are omitted (not
	// required). Keys are the model's json tags.
	fkTags := map[string]bool{}
	for _, f := range relationFields(e) {
		fkTags[strings.ToLower(jsonTag(f.Name))] = true
	}
	var pairs []string
	for _, f := range fieldsForEntity(e) {
		tag := jsonTag(f.Name)
		if fkTags[strings.ToLower(tag)] {
			continue
		}
		switch goType(f.Type) {
		case "string":
			pairs = append(pairs, fmt.Sprintf("%q:%q", tag, "x"))
		case "int":
			pairs = append(pairs, fmt.Sprintf("%q:%d", tag, 1))
		}
	}
	body := "{" + strings.Join(pairs, ",") + "}"
	var b strings.Builder
	fmt.Fprintf(&b, "package handler\n\n")
	fmt.Fprintf(&b, "import (\n\t\"net/http\"\n\t\"net/http/httptest\"\n\t\"strings\"\n\t\"testing\"\n)\n\n")
	fmt.Fprintf(&b, "func Test%sCRUD(t *testing.T) {\n", n)
	fmt.Fprintf(&b, "\tmux := http.NewServeMux()\n\tNew%sHandler().Register(mux)\n\n", n)
	fmt.Fprintf(&b, "\t// create (valid body satisfies validation)\n\tcreate := httptest.NewRequest(http.MethodPost, \"%s\", strings.NewReader(`%s`))\n\tcw := httptest.NewRecorder()\n\tmux.ServeHTTP(cw, create)\n\tif cw.Code != http.StatusCreated {\n\t\tt.Fatalf(\"create: want 201, got %%d\", cw.Code)\n\t}\n\n", coll, body)
	fmt.Fprintf(&b, "\t// list\n\tlist := httptest.NewRequest(http.MethodGet, \"%s\", nil)\n\tlw := httptest.NewRecorder()\n\tmux.ServeHTTP(lw, list)\n\tif lw.Code != http.StatusOK {\n\t\tt.Fatalf(\"list: want 200, got %%d\", lw.Code)\n\t}\n\n", coll)
	fmt.Fprintf(&b, "\t// get the created item (id=1)\n\tget := httptest.NewRequest(http.MethodGet, \"%s/1\", nil)\n\tgw := httptest.NewRecorder()\n\tmux.ServeHTTP(gw, get)\n\tif gw.Code != http.StatusOK {\n\t\tt.Fatalf(\"get: want 200, got %%d\", gw.Code)\n\t}\n\n", coll)
	fmt.Fprintf(&b, "\t// delete it\n\tdel := httptest.NewRequest(http.MethodDelete, \"%s/1\", nil)\n\tdw := httptest.NewRecorder()\n\tmux.ServeHTTP(dw, del)\n\tif dw.Code != http.StatusNoContent {\n\t\tt.Fatalf(\"delete: want 204, got %%d\", dw.Code)\n\t}\n}\n", coll)
	writeFileMk(filepath.Join(path, "backend/internal/handler/"+typeName(e.Name)+"_handler_test.go"), b.String())
}

// emitMain writes backend/main.go. In memory mode it wires in-memory stores
// directly (no DB import). In postgres mode it selects the backend at runtime
// via STORE_BACKEND (default memory): postgres opens the DB, auto-migrates,
// and injects the PG stores.
func emitMain(entities []schema.Entity, path string) {
	names := make([]string, 0, len(entities))
	seen := map[string]bool{}
	for _, e := range entities {
		tn := typeName(e.Name)
		if tn == "" || seen[tn] {
			continue
		}
		seen[tn] = true
		names = append(names, tn)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("// Code generated by gen-system. DO NOT EDIT.\npackage main\n\n")

	if !postgresEnabled() {
		// memory-only main. Phase 4-B: construct ONE store per entity and inject
		// it into both the CRUD handler and (if present) the business-ops handler
		// so they share state. Without this they'd hold separate stores and an
		// order created via CRUD would be invisible to its service ops.
		anySvc := memModeHasAnyServiceHandler(path, names)
		b.Reset()
		b.WriteString("// Code generated by gen-system. DO NOT EDIT.\npackage main\n\n")
		b.WriteString("import (\n\t\"log\"\n\t\"net/http\"\n\n\t\"app/backend/internal/handler\"\n\t\"app/backend/internal/store\"\n)\n\n")
		b.WriteString("func main() {\n\tmux := http.NewServeMux()\n")
		for _, n := range names {
			fmt.Fprintf(&b, "\t%sStore := store.New%sStore()\n", lowerFirst(n), n)
			fmt.Fprintf(&b, "\thandler.New%sHandlerWith(%sStore).Register(mux)\n", n, lowerFirst(n))
			if hasServiceHandler(path, n) {
				fmt.Fprintf(&b, "\thandler.New%sServiceHandler(%sStore).Register(mux)\n", n, lowerFirst(n))
			}
		}
		_ = anySvc
		b.WriteString("\n\tmux.HandleFunc(\"/health\", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(\"ok\")) })\n")
		b.WriteString("\n\tlog.Println(\"listening on :8080 (in-memory)\")\n\tlog.Fatal(http.ListenAndServe(\":8080\", mux))\n}\n")
		writeFileMk(filepath.Join(path, "backend/main.go"), b.String())
		return
	}

	// dual-backend main (memory default; STORE_BACKEND=postgres switches)
	b.WriteString("import (\n\t\"log\"\n\t\"net/http\"\n\t\"os\"\n\n\t\"app/backend/internal/db\"\n\t\"app/backend/internal/handler\"\n\t\"app/backend/internal/store\"\n)\n\n")
	b.WriteString("func main() {\n\tmux := http.NewServeMux()\n\n")
	b.WriteString("\tif os.Getenv(\"STORE_BACKEND\") == \"postgres\" {\n")
	b.WriteString("\t\td, err := db.Open()\n\t\tif err != nil {\n\t\t\tlog.Fatal(err)\n\t\t}\n\t\tif err := db.Migrate(d); err != nil {\n\t\t\tlog.Fatal(err)\n\t\t}\n")
	for _, n := range names {
		fmt.Fprintf(&b, "\t\t%sStore := store.New%sPGStore(d)\n", lowerFirst(n), n)
		fmt.Fprintf(&b, "\t\thandler.New%sHandlerWith(%sStore).Register(mux)\n", n, lowerFirst(n))
		if hasServiceHandler(path, n) {
			fmt.Fprintf(&b, "\t\thandler.New%sServiceHandler(%sStore).Register(mux)\n", n, lowerFirst(n))
		}
	}
	b.WriteString("\t\tlog.Println(\"listening on :8080 (postgres)\")\n")
	b.WriteString("\t} else {\n")
	for _, n := range names {
		fmt.Fprintf(&b, "\t\t%sStore := store.New%sStore()\n", lowerFirst(n), n)
		fmt.Fprintf(&b, "\t\thandler.New%sHandlerWith(%sStore).Register(mux)\n", n, lowerFirst(n))
		if hasServiceHandler(path, n) {
			fmt.Fprintf(&b, "\t\thandler.New%sServiceHandler(%sStore).Register(mux)\n", n, lowerFirst(n))
		}
	}
	b.WriteString("\t\tlog.Println(\"listening on :8080 (in-memory)\")\n\t}\n\n")
	b.WriteString("\tmux.HandleFunc(\"/health\", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(\"ok\")) })\n")
	b.WriteString("\tlog.Fatal(http.ListenAndServe(\":8080\", mux))\n}\n")
	writeFileMk(filepath.Join(path, "backend/main.go"), b.String())
}

// exportName turns an arbitrary field name into a valid EXPORTED Go identifier.
// v23.2.1: the model sometimes emits names with spaces/punctuation (e.g.
// "due date"), which are not valid Go identifiers and broke the generated
// model (more-than-one-char rune / unexpected literal). We split on any
// non-alphanumeric run, Title-case each part, join, and guard a leading digit.
func exportName(s string) string {
	parts := splitIdent(s)
	if len(parts) == 0 {
		return "Field"
	}
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(strings.ToUpper(p[:1]) + p[1:])
	}
	out := b.String()
	if out[0] >= '0' && out[0] <= '9' {
		out = "F" + out
	}
	return out
}

// jsonTag normalises a field name to a stable snake_case JSON key.
func jsonTag(s string) string {
	parts := splitIdent(s)
	if len(parts) == 0 {
		return "field"
	}
	for i := range parts {
		parts[i] = strings.ToLower(parts[i])
	}
	return strings.Join(parts, "_")
}

// routeName turns an entity name into a lowercase route/path segment with no
// spaces or punctuation (e.g. "Order Item" -> "orderitem").
func routeName(s string) string {
	return strings.ToLower(strings.Join(splitIdent(s), ""))
}

// plural appends "s" unless the word already ends in "s" (so "Tasks" stays
// "tasks", not "taskss"). Deliberately simple — good enough for route/table.
func plural(s string) string {
	if strings.HasSuffix(s, "s") {
		return s
	}
	return s + "s"
}

// collPath is the REST collection path for an entity, e.g. "/tasks".
func collPath(e schema.Entity) string { return "/" + plural(routeName(e.Name)) }

// tableName is the SQL table for an entity, e.g. "tasks".
func tableName(e schema.Entity) string { return plural(routeName(e.Name)) }

// typeName turns an entity name into a valid exported Go type identifier.
func typeName(s string) string { return exportName(s) }

// splitIdent splits a string on any run of non-alphanumeric characters,
// dropping empties. "due date" -> [due date]; "user-id" -> [user id].
func splitIdent(s string) []string {
	var parts []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			parts = append(parts, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return parts
}

// writeFileMk writes content, creating parent dirs.
func writeFileMk(p, content string) {
	os.MkdirAll(filepath.Dir(p), 0755)
	os.WriteFile(p, []byte(content), 0644)
}
