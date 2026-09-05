// generator/internal/engine/service_logic.go — v23.25 (task B): sanitizeOperations
// drops an op the idea confidently binds to a different entity (ai.OpMisplaced)
// before the compile gate. Conservative; ambiguity keeps the op.
// generator/internal/engine/service_logic.go — v23.18: (1) service file imports
// are now auto-derived from what the model bodies reference (fmt/time/strconv/…)
// instead of a fixed errors/model/store block, fixing "undefined: fmt|time";
// (2) sanitizeOperations now does a REAL per-op `go build` against the store/
// model contract (not just gofmt), so a body that misuses the (T,bool) store
// API or won't type-check is dropped to a not-implemented stub — guaranteeing
// the service layer always builds green regardless of model output.
// (v23.14 Phase 6) Spring-mode guard added: when GEN_BACKEND=spring this Go generator is a no-op; the Spring Boot project is emitted by backend_spring.go.
// generator/internal/engine/service_logic.go — v23.10 (Phase 4-B): generates a
// business-logic SERVICE layer whose method bodies come from a LogicProvider
// (live Ollama or deterministic replay). CRUD handlers are left untouched; the
// service is additive. Each operation is injected into a FIXED signature so
// injection is mechanical:
//
//	func (s *<E>Service) <Name>(id int, in map[string]any) (any, error) { <body> }
//
// Trust/containment: bodies are gofmt-checked; an operation whose body fails to
// format is dropped to a safe `return nil, errors.New("not implemented: <Name>")`
// stub so the service ALWAYS compiles. The backend's existing go vet/go build
// self-heal loop is the final gate. A generated service test calls each op with a
// nil payload and asserts it honors the (any,error) contract without panicking.
package engine

import (
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gen-system/generator/internal/ai"
	"gen-system/generator/internal/schema"
)

// GenerateServiceLogic builds backend/internal/service/<entity>_service.go using
// the supplied provider + idea text, generates its test, and writes the business
// routes file. Safe to call for every entity; an entity with no operations still
// gets a compiling (empty-but-valid) service so handler wiring is uniform.
func GenerateServiceLogic(idea string, e schema.Entity, all []schema.Entity, provider ai.LogicProvider, path string) {
	if springEnabled() {
		// Phase 6 Spring backend is CRUD scaffolding; the LLM business-logic
		// service layer is a Go-backend feature and is not injected into Java.
		return
	}
	ops, err := provider.Operations(idea, e, all)
	if err != nil {
		// Provider failure (e.g. model unreachable) must not break the build:
		// emit a CRUD-only service with no operations and move on.
		ops = nil
	}
	ops = sanitizeOperations(idea, e, all, ops, provider)

	// Preserve existing operations. When regenerating a service that already has
	// a file (e.g. `gen continue` adds Order.Refund), the provider only proposes
	// the DELTA ops for this run; overwriting would drop the entity's existing
	// Checkout/ApplyDiscount and leave the generated test referencing methods
	// that no longer exist (go vet fails). Merge: newly-proposed ops win on a
	// name collision; any existing op not re-proposed is carried forward.
	ops = mergeWithExistingOps(e, ops, path)

	genServiceFile(e, ops, path)
	genServiceTest(e, ops, path)
	genServiceRoutes(e, ops, path)
	writePayloadHelpers(path) // shared service/payload.go (idempotent, one per app)
}

// mergeWithExistingOps reads the operations already present in the entity's
// service file (if any) and merges them under the freshly-proposed ops. New ops
// override an existing op of the same name; existing ops absent from the new set
// are preserved verbatim (name, HTTP method, subpath, and body).
func mergeWithExistingOps(e schema.Entity, newOps []ai.Operation, path string) []ai.Operation {
	file := filepath.Join(path, "backend/internal/service/"+routeName(e.Name)+"_service.go")
	data, err := os.ReadFile(file)
	if err != nil {
		return newOps // no existing file → nothing to preserve
	}
	existing := parseExistingOps(string(data), typeName(e.Name))
	if len(existing) == 0 {
		return newOps
	}
	byName := map[string]bool{}
	for _, op := range newOps {
		byName[strings.ToLower(op.Name)] = true
	}
	merged := append([]ai.Operation{}, newOps...)
	for _, ex := range existing {
		if !byName[strings.ToLower(ex.Name)] {
			merged = append(merged, ex)
		}
	}
	return merged
}

// parseExistingOps recovers operations from a generated service file: each
// "func (s *<N>Service) <Name>(id int, in map[string]any) (any, error) { … }"
// becomes an Operation with its body (statements between the outer braces) and a
// route derived the same way the generator derives it.
func parseExistingOps(src, structName string) []ai.Operation {
	var ops []ai.Operation
	marker := "func (s *" + structName + "Service) "
	for {
		i := strings.Index(src, marker)
		if i == -1 {
			break
		}
		rest := src[i+len(marker):]
		// method name = up to '('
		paren := strings.IndexByte(rest, '(')
		if paren == -1 {
			break
		}
		name := strings.TrimSpace(rest[:paren])
		// find the opening brace of the body
		brace := strings.IndexByte(rest, '{')
		if brace == -1 {
			break
		}
		// balance braces to find the matching close
		depth := 0
		end := -1
		for j := brace; j < len(rest); j++ {
			switch rest[j] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					end = j
				}
			}
			if end != -1 {
				break
			}
		}
		if end == -1 {
			break
		}
		body := strings.TrimSpace(rest[brace+1 : end])
		if name != "" && body != "" && isExportedIdent(name) {
			ops = append(ops, ai.Operation{
				Name:       name,
				HTTPMethod: "POST",
				Subpath:    deriveSubpath(name),
				Body:       body,
			})
		}
		src = rest[end:]
	}
	return ops
}

func isExportedIdent(s string) bool {
	return s != "" && s[0] >= 'A' && s[0] <= 'Z'
}

// sanitizeOperations validates each operation so the emitted file ALWAYS
// compiles: it requires a Go-identifier name + non-empty body, dedups names, and
// COMPILE-CHECKS the assembled method against a realistic service context (the
// store interface + model type + the same auto-derived imports the real file
// gets). An operation whose body does not type-check — wrong store contract
// (e.g. treating Get's bool as an error), undefined identifiers, bad imports — is
// dropped to a `return nil, errors.New("not implemented: <Name>")` stub. This is
// the containment guarantee: whatever the model emits, the service builds green.
//
// v23.18: upgraded from a syntax-only gofmt check (which passed bodies that used
// fmt/time without importing them, or misused the (T,bool) store contract) to a
// genuine `go build` per operation.
// maxRepairAttempts is how many times a failing operation body is sent back to
// the model (with the build error) before falling back to a stub. v23.23.
const maxRepairAttempts = 2

func sanitizeOperations(idea string, e schema.Entity, all []schema.Entity, ops []ai.Operation, provider ai.LogicProvider) []ai.Operation {
	n := typeName(e.Name)
	repairer, canRepair := provider.(ai.OperationRepairer)
	seen := map[string]bool{}
	var out []ai.Operation
	for _, op := range ops {
		if strings.TrimSpace(op.Name) == "" {
			continue // an unnamed operation is dropped, not coerced to a fallback
		}
		name := exportName(op.Name)
		if name == "" || strings.TrimSpace(op.Body) == "" || seen[strings.ToLower(name)] {
			continue
		}
		// v23.25 (task B): conservative misplacement post-filter. If the idea
		// confidently binds this op's verb to a DIFFERENT entity (and not to
		// this one), drop it here rather than emit a method on the wrong entity.
		if ai.OpMisplaced(idea, e.Name, name, all) {
			fmt.Printf("     ⚠️  dropping op %q from %s — idea binds it to another entity\n", name, e.Name)
			continue
		}
		seen[strings.ToLower(name)] = true
		op.Name = name
		// All generated service operations take (id int, in map[string]any) and
		// mutate state through the store, so they are POST by nature. The model
		// sometimes guesses GET for an action like "Checkout", which then makes
		// the dispatcher reject a POSTed call with 405. Normalize to POST unless
		// the op is an explicit read (name starts with Get/List/Find/Fetch).
		if isReadOp(name) {
			if strings.TrimSpace(op.HTTPMethod) == "" {
				op.HTTPMethod = "GET"
			}
		} else {
			op.HTTPMethod = "POST"
		}
		// Give the operation a clean, predictable URL segment when the model
		// didn't supply one (or supplied the raw lowercased name). e.g.
		// ApplyDiscount -> "discount", AdjustStock -> "adjust-stock". This makes
		// the generated routes match what a caller would naturally guess.
		if strings.TrimSpace(op.Subpath) == "" {
			op.Subpath = deriveSubpath(name)
		}

		// Compile gate (v23.22: real-field stand-in). v23.23: if it fails AND
		// the provider can repair, feed the build error back to the model and
		// retry up to maxRepairAttempts before stubbing. The more real the
		// logic, the more likely a near-miss (e.g. a missing type assertion on
		// the payload) is fixed on the second pass instead of discarded.
		ok, buildErr := operationCompiles(e, n, op)
		if !ok && canRepair {
			for attempt := 1; attempt <= maxRepairAttempts; attempt++ {
				fmt.Printf("     [logic] %s.%s did not compile; repair attempt %d/%d…\n",
					e.Name, name, attempt, maxRepairAttempts)
				fixed, rerr := repairer.RepairOperation(idea, e, all, op, buildErr)
				if rerr != nil {
					break // model/transport failure — stop trying, stub below
				}
				fixed.Name = name // keep route/name stable
				if strings.TrimSpace(fixed.HTTPMethod) == "" {
					fixed.HTTPMethod = op.HTTPMethod
				}
				if fixed.Subpath == "" {
					fixed.Subpath = op.Subpath
				}
				ok, buildErr = operationCompiles(e, n, fixed)
				recordRepairAttempt(ok) // WP1: count every repair round-trip
				if ok {
					op = fixed
					fmt.Printf("     [logic] %s.%s repaired ✓\n", e.Name, name)
					break
				}
				op = fixed // feed the latest (still-failing) body into the next attempt
			}
		}
		if !ok {
			op.Body = fmt.Sprintf("return nil, errors.New(%q)", "not implemented: "+name)
			op.Helpers = nil
		}
		recordOpOutcome(!ok) // WP1: emitted op, stubbed or real

		// v26.33 style gate. Only a body that COMPILES is checked: style is a
		// second, separate gate that can never turn logic into a stub. A
		// violation is sent back to the model the way a build error is, with
		// the rules named, under its own cap (StyleMaxRepairs, at most 2). A
		// repaired body is accepted only if it still compiles; otherwise the
		// compiling, off-style body is kept and the violation stays recorded.
		if ok && StyleGate != nil {
			if findings := StyleGate(name, op.Body, op.Helpers); findings != "" {
				recordStyleViolation()
				fmt.Printf("     [style] %s.%s compiles but breaks team rules:\n%s", e.Name, name, indent(findings))
				if canRepair {
					for attempt := 1; attempt <= StyleMaxRepairs; attempt++ {
						fmt.Printf("     [style] %s.%s style repair attempt %d/%d…\n", e.Name, name, attempt, StyleMaxRepairs)
						fixed, rerr := repairer.RepairOperation(idea, e, all, op,
							"STYLE VIOLATIONS (the code compiles; keep the behaviour, fix the style):\n"+findings)
						if rerr != nil {
							break
						}
						fixed.Name, fixed.HTTPMethod, fixed.Subpath = name, op.HTTPMethod, op.Subpath
						compiles, _ := operationCompiles(e, n, fixed)
						clean := compiles && StyleGate(name, fixed.Body, fixed.Helpers) == ""
						recordStyleRepair(clean)
						if clean {
							op = fixed
							fmt.Printf("     [style] %s.%s repaired ✓\n", e.Name, name)
							break
						}
						if compiles {
							op = fixed // closer, still off style; feed it into the next attempt
							findings = StyleGate(name, fixed.Body, fixed.Helpers)
						}
					}
				}
			}
		}
		out = append(out, op)
	}
	return out
}

// StyleGate (v26.33) checks one compiled operation body against the team
// style profile and returns the rendered findings, or "" when clean. Set by
// the agent when a profile is active in gate or autofix mode; nil means the
// gate is off and this file behaves exactly as v26.32. Package-level seam,
// like the LogicStats counters.
var StyleGate func(name, body string, helpers []string) string

// StyleMaxRepairs caps op-level style repairs. The agent sets it from the
// profile; it is never allowed above maxRepairAttempts.
var StyleMaxRepairs = 1

func indent(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		b.WriteString("         " + line + "\n")
	}
	return b.String()
}

// standInModelStruct emits a `model_<Struct>` type for the compile gate that
// carries the SAME fields the real generated model gets (ID + fieldsForEntity,
// deduped by identifier/json-tag), with the SAME goType mapping. This is what
// lets a model-written body that reads o.Total / p.Stock type-check in the gate
// exactly as it does in the real package. v23.22.
func standInModelStruct(e schema.Entity, structName string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "type model_%s struct {\n", structName)
	b.WriteString("\tID int `json:\"id\"`\n")
	usedIdent := map[string]bool{"id": true}
	usedTag := map[string]bool{"id": true}
	for _, f := range fieldsForEntity(e) {
		ident := exportName(f.Name)
		tag := jsonTag(f.Name)
		if ident == "" || usedIdent[strings.ToLower(ident)] || usedTag[strings.ToLower(tag)] {
			continue
		}
		usedIdent[strings.ToLower(ident)] = true
		usedTag[strings.ToLower(tag)] = true
		fmt.Fprintf(&b, "\t%s %s `json:\"%s\"`\n", ident, goType(f.Type), tag)
	}
	b.WriteString("}\n")
	return b.String()
}

// operationCompiles type-checks a single operation by assembling a minimal but
// realistic service package (model type + store interface + the op's method and
// helpers + auto-derived imports) in a temp dir and running `go build`. Returns
// true only if it builds. Falls back to a gofmt syntax check if the toolchain is
// unavailable (e.g. no `go` on PATH), so generation still works offline.
// operationCompiles type-checks one operation against the store/model contract.
// v23.23: returns the build error output (empty when it compiles) so the caller
// can feed it back to the model for a repair attempt before falling back to a stub.
func operationCompiles(e schema.Entity, structName string, op ai.Operation) (bool, string) {
	method := renderMethod(structName, op)

	// If go isn't available, degrade to the old syntax-only check.
	if _, err := exec.LookPath("go"); err != nil {
		_, ferr := format.Source([]byte("package service\nimport \"errors\"\nvar _ = errors.New\n" + method))
		if ferr != nil {
			return false, ferr.Error()
		}
		return true, ""
	}

	dir, err := os.MkdirTemp("", "gen-op-*")
	if err != nil {
		return false, err.Error()
	}
	defer os.RemoveAll(dir)

	// Self-contained package: local model + store types (mirroring the in-mem
	// store contract: Get/Update return (T,bool); Create returns T; Delete bool)
	// + the op under its real signature + imports.
	var b strings.Builder
	b.WriteString("package service\n\n")
	// Build imports from the op body AND the payload helpers (so strconv, used by
	// the helpers, is imported). The helpers are appended to the source the
	// import-detector scans.
	helperFuncs := payloadHelperFuncs()
	b.WriteString(buildServiceImportsWith([]ai.Operation{op}, helperFuncs))
	// Local stand-ins for app/backend/internal/model and store so the temp pkg
	// is self-contained (the real packages are not on the temp module path).
	// v23.22: the stand-in struct carries the entity's REAL fields (same ID +
	// fieldsForEntity loop as genModelInMem), so a body referencing a real
	// field type-checks here identically to the real package.
	b.WriteString(standInModelStruct(e, structName))
	fmt.Fprintf(&b, "type store_%sCRUD interface {\n", structName)
	fmt.Fprintf(&b, "\tCreate(m model_%s) (model_%s, error)\n", structName, structName)
	fmt.Fprintf(&b, "\tList() []model_%s\n", structName)
	fmt.Fprintf(&b, "\tGet(id int) (model_%s, bool)\n", structName)
	fmt.Fprintf(&b, "\tUpdate(id int, m model_%s) (model_%s, bool)\n", structName, structName)
	b.WriteString("\tDelete(id int) bool\n}\n\n")
	fmt.Fprintf(&b, "type %sService struct{ store store_%sCRUD }\n\n", structName, structName)
	b.WriteString("var _ = errors.New\n\n")
	// The same payload helpers the real service package gets (funcs only; their
	// strconv import is handled by buildServiceImportsWith above), so a body
	// using payloadInt/payloadFloat/payloadString compiles in the gate exactly
	// as in the generated package.
	b.WriteString(helperFuncs)
	b.WriteString(method)
	b.WriteString("\n")
	for _, h := range op.Helpers {
		b.WriteString(strings.TrimSpace(h))
		b.WriteString("\n\n")
	}

	// Rewrite the references the method makes to the REAL packages onto the
	// local stand-ins so the temp package is self-contained.
	src := b.String()
	src = strings.ReplaceAll(src, "model."+structName, "model_"+structName)
	src = strings.ReplaceAll(src, "store."+structName+"CRUD", "store_"+structName+"CRUD")
	// Drop the now-unneeded app imports (added by buildServiceImports) to avoid
	// "cannot find package"; the local stand-ins replace them.
	src = strings.ReplaceAll(src, "\t\"app/backend/internal/model\"\n", "")
	src = strings.ReplaceAll(src, "\t\"app/backend/internal/store\"\n", "")

	if err := os.WriteFile(filepath.Join(dir, "svc.go"), []byte(src), 0o644); err != nil {
		return false, err.Error()
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module gentmp\n\ngo 1.22\n"), 0o644); err != nil {
		return false, err.Error()
	}
	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		return false, cleanBuildError(string(out))
	}
	return true, ""
}

// cleanBuildError trims the temp-dir noise from a go build error so the message
// fed back to the model is about the code, not the sandbox path.
func cleanBuildError(out string) string {
	out = strings.TrimSpace(out)
	if out == "" {
		return "build failed"
	}
	// Drop the leading "# gentmp" package header line if present.
	lines := strings.Split(out, "\n")
	var kept []string
	for _, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "# ") {
			continue
		}
		// Strip "./svc.go:" file prefix; keep the line:col + message.
		ln = strings.TrimPrefix(strings.TrimSpace(ln), "./svc.go:")
		kept = append(kept, ln)
	}
	msg := strings.Join(kept, "\n")
	if len(msg) > 800 {
		msg = msg[:800] + " …(truncated)"
	}
	return msg
}

func renderMethod(structName string, op ai.Operation) string {
	doc := op.Doc
	if doc == "" {
		doc = op.Name + " is a generated business operation."
	}
	return fmt.Sprintf("// %s\nfunc (s *%sService) %s(id int, in map[string]any) (any, error) {\n\t%s\n}\n",
		doc, structName, op.Name, strings.TrimSpace(op.Body))
}

// servicePayloadHelpers is the body of the shared service/payload.go file (one
// per generated app, NOT per entity — they live in package service and would
// redeclare if emitted per-entity). Operation bodies read payload values via
// these helpers instead of type assertions (which panic on the float64 that
// JSON numbers always decode to). The prompt mandates their use.
const servicePayloadHelpers = `// Code generated by gen-system (payload helpers). DO NOT EDIT.
package service

import "strconv"

// payloadInt reads in[key] as an int, accepting JSON float64/int/string forms.
// ok is false when the key is absent or not numeric. Never panics.
func payloadInt(in map[string]any, key string) (int, bool) {
	switch v := in[key].(type) {
	case float64:
		return int(v), true
	case float32:
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	case string:
		n, err := strconv.Atoi(v)
		return n, err == nil
	default:
		return 0, false
	}
}

// payloadFloat reads in[key] as a float64. Never panics.
func payloadFloat(in map[string]any, key string) (float64, bool) {
	switch v := in[key].(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case string:
		f, err := strconv.ParseFloat(v, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

// payloadString reads in[key] as a string. Never panics.
func payloadString(in map[string]any, key string) (string, bool) {
	s, ok := in[key].(string)
	return s, ok
}

var _ = payloadInt
var _ = payloadFloat
var _ = payloadString
`

// writePayloadHelpers emits the shared service/payload.go once per app.
func writePayloadHelpers(path string) {
	src := servicePayloadHelpers
	if formatted, err := format.Source([]byte(src)); err == nil {
		src = string(formatted)
	}
	writeFileMk(filepath.Join(path, "backend/internal/service/payload.go"), src)
}

// payloadHelperFuncs returns just the helper function declarations (no package
// header / no import line) for embedding inside the compile-gate temp package.
func payloadHelperFuncs() string {
	// Strip the leading "// Code generated…\npackage service\n\nimport \"strconv\"\n"
	// header from the const, leaving the funcs.
	s := servicePayloadHelpers
	if i := strings.Index(s, "import \"strconv\"\n"); i != -1 {
		s = s[i+len("import \"strconv\"\n"):]
	}
	return s
}

// genServiceFile renders the service file for one entity.
func genServiceFile(e schema.Entity, ops []ai.Operation, path string) {
	n := typeName(e.Name)
	var b strings.Builder
	b.WriteString("// Code generated by gen-system (business logic). DO NOT EDIT.\n")
	b.WriteString("package service\n\n")
	// v23.18: derive the import set from what the operation bodies + helpers
	// actually reference, instead of a fixed errors/model/store block. Model-
	// written bodies routinely use fmt/time/strconv/sort/strings/math/etc.; a
	// fixed import block left those undefined ("undefined: fmt") and broke the
	// build. errors/model/store are always included (the struct + stub use them).
	b.WriteString(buildServiceImports(ops))
	b.WriteString("// " + n + "Service holds business operations beyond CRUD.\n")
	fmt.Fprintf(&b, "type %sService struct{ store store.%sCRUD }\n\n", n, n)
	fmt.Fprintf(&b, "// New%sService builds the service over any %sCRUD store.\n", n, n)
	fmt.Fprintf(&b, "func New%sService(s store.%sCRUD) *%sService { return &%sService{store: s} }\n\n", n, n, n, n)
	// Keep the model.<N> import referenced even if no op mentions it, so the
	// import never goes unused (which would fail the build).
	fmt.Fprintf(&b, "var _ model.%s\nvar _ = errors.New\n\n", n)

	for _, op := range ops {
		b.WriteString(renderMethod(n, op))
		b.WriteString("\n")
		for _, h := range op.Helpers {
			b.WriteString(strings.TrimSpace(h))
			b.WriteString("\n\n")
		}
	}

	src := b.String()
	if formatted, err := format.Source([]byte(src)); err == nil {
		src = string(formatted)
	}
	writeFileMk(filepath.Join(path, "backend/internal/service/"+routeName(e.Name)+"_service.go"), src)
}

// serviceStdlibPkgs are the standard-library packages a model body may use that
// are safe to import on demand. Each maps the selector prefix (e.g. "fmt.") to
// its import path. Only these are auto-added; anything else the model references
// will fail the per-op compile gate and be stubbed (so the build stays green).
var serviceStdlibPkgs = map[string]string{
	"fmt":      "fmt",
	"time":     "time",
	"strconv":  "strconv",
	"strings":  "strings",
	"sort":     "sort",
	"math":     "math",
	"errors":   "errors",
	"json":     "encoding/json",
	"rand":     "math/rand",
	"regexp":   "regexp",
	"unicode":  "unicode",
	"utf8":     "unicode/utf8",
	"bytes":    "bytes",
	"bufio":    "bufio",
	"io":       "io",
	"os":       "os",
	"context":  "context",
	"sync":     "sync",
	"slices":   "slices",
	"maps":     "maps",
	"bits":     "math/bits",
	"big":      "math/big",
	"cmplx":    "math/cmplx",
	"base64":   "encoding/base64",
	"hex":      "encoding/hex",
	"sha256":   "crypto/sha256",
	"md5":      "crypto/md5",
	"url":      "net/url",
	"filepath": "path/filepath",
	"path":     "path",
}

// buildServiceImports returns the import block for a service file, including only
// the stdlib packages the operation bodies/helpers actually reference, plus the
// always-needed errors/model/store. Unused imports (which would themselves break
// the build) are never added.
func buildServiceImports(ops []ai.Operation) string {
	return buildServiceImportsWith(ops, "")
}

// buildServiceImportsWith is buildServiceImports plus extra source text whose
// package selectors (e.g. strconv. in the payload helpers) are also considered.
func buildServiceImportsWith(ops []ai.Operation, extra string) string {
	used := map[string]bool{}
	var code strings.Builder
	for _, op := range ops {
		code.WriteString(op.Body)
		code.WriteString("\n")
		for _, h := range op.Helpers {
			code.WriteString(h)
			code.WriteString("\n")
		}
	}
	code.WriteString(extra)
	src := code.String()
	for sel, imp := range serviceStdlibPkgs {
		// match a package selector use: "<sel>." not preceded by an identifier
		// char (so "myfmt." or "x.time." don't false-positive).
		if containsSelector(src, sel) {
			used[imp] = true
		}
	}
	// errors is always used (the not-implemented stub + var _ = errors.New).
	used["errors"] = true

	// Stable, gofmt-style grouping: stdlib block, blank line, app block.
	var stdlib []string
	for imp := range used {
		stdlib = append(stdlib, imp)
	}
	sortStrings(stdlib)

	var b strings.Builder
	b.WriteString("import (\n")
	for _, imp := range stdlib {
		fmt.Fprintf(&b, "\t%q\n", imp)
	}
	b.WriteString("\n\t\"app/backend/internal/model\"\n\t\"app/backend/internal/store\"\n)\n\n")
	return b.String()
}

// containsSelector reports whether src uses the package selector sel (e.g. "fmt")
// as in "fmt.", with the preceding char not being an identifier char (so it is a
// real package reference, not a field/struct selector like x.fmt.).
func containsSelector(src, sel string) bool {
	needle := sel + "."
	from := 0
	for {
		i := strings.Index(src[from:], needle)
		if i < 0 {
			return false
		}
		pos := from + i
		if pos == 0 || !isIdentByte(src[pos-1]) {
			return true
		}
		from = pos + len(needle)
	}
}

func isIdentByte(c byte) bool {
	return c == '_' ||
		(c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9')
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// /<collection>/{id}/<subpath> (or /<collection>/<subpath> when there is no id
// semantics). The handler decodes the JSON body into `in`, calls the op, writes
// (any,error) as JSON / HTTP error. CRUD routes remain in the handler file.
func genServiceRoutes(e schema.Entity, ops []ai.Operation, path string) {
	if len(ops) == 0 {
		return // no business routes file when there are no operations
	}
	n := typeName(e.Name)
	coll := collPath(e)         // e.g. "/orders"
	opsPrefix := coll + "-ops/" // e.g. "/orders-ops/"  — distinct from the CRUD "/orders/" so net/http's mux does not collide
	var b strings.Builder
	b.WriteString("// Code generated by gen-system (business routes). DO NOT EDIT.\n")
	b.WriteString("package handler\n\n")
	b.WriteString("import (\n\t\"encoding/json\"\n\t\"net/http\"\n\t\"strconv\"\n\t\"strings\"\n\n\t\"app/backend/internal/store\"\n\t\"app/backend/internal/service\"\n)\n\n")
	fmt.Fprintf(&b, "type %sServiceHandler struct{ svc *service.%sService }\n\n", n, n)
	fmt.Fprintf(&b, "func New%sServiceHandler(s store.%sCRUD) *%sServiceHandler {\n\treturn &%sServiceHandler{svc: service.New%sService(s)}\n}\n\n", n, n, n, n, n)
	// A SINGLE dispatcher owns the ops prefix and routes by the trailing segment,
	// so multiple operations never register conflicting mux patterns.
	fmt.Fprintf(&b, "// Register mounts business operations at %s{id}/{op}.\n", opsPrefix)
	fmt.Fprintf(&b, "func (h *%sServiceHandler) Register(mux *http.ServeMux) {\n", n)
	fmt.Fprintf(&b, "\tmux.HandleFunc(%q, h.dispatch)\n", opsPrefix)
	b.WriteString("}\n\n")
	fmt.Fprintf(&b, "func (h *%sServiceHandler) dispatch(w http.ResponseWriter, r *http.Request) {\n", n)
	// Defense in depth: a model-written operation body must never crash the
	// server. Recover from any panic and return 500 so the connection is always
	// answered (no dropped/empty responses).
	b.WriteString("\tdefer func() {\n\t\tif rec := recover(); rec != nil {\n\t\t\thttp.Error(w, \"internal error\", http.StatusInternalServerError)\n\t\t}\n\t}()\n")
	fmt.Fprintf(&b, "\trest := strings.TrimPrefix(r.URL.Path, %q)\n", opsPrefix)
	b.WriteString("\tparts := strings.Split(strings.Trim(rest, \"/\"), \"/\")\n")
	b.WriteString("\tif len(parts) < 2 {\n\t\thttp.NotFound(w, r)\n\t\treturn\n\t}\n")
	b.WriteString("\tid, err := strconv.Atoi(parts[0])\n\tif err != nil {\n\t\thttp.Error(w, \"bad id\", http.StatusBadRequest)\n\t\treturn\n\t}\n")
	b.WriteString("\tin := map[string]any{}\n\tif r.Body != nil {\n\t\t_ = json.NewDecoder(r.Body).Decode(&in)\n\t}\n")
	b.WriteString("\tvar (\n\t\tout any\n\t\toErr error\n\t)\n")
	b.WriteString("\tswitch parts[1] {\n")
	for _, op := range ops {
		sub := op.Subpath
		if sub == "" {
			sub = strings.ToLower(op.Name)
		}
		fmt.Fprintf(&b, "\tcase %q:\n", sub)
		fmt.Fprintf(&b, "\t\tif r.Method != %q {\n\t\t\thttp.Error(w, \"method not allowed\", http.StatusMethodNotAllowed)\n\t\t\treturn\n\t\t}\n", op.HTTPMethod)
		fmt.Fprintf(&b, "\t\tout, oErr = h.svc.%s(id, in)\n", op.Name)
	}
	b.WriteString("\tdefault:\n\t\thttp.NotFound(w, r)\n\t\treturn\n\t}\n")
	b.WriteString("\tif oErr != nil {\n\t\thttp.Error(w, oErr.Error(), http.StatusBadRequest)\n\t\treturn\n\t}\n")
	b.WriteString("\twriteJSON(w, http.StatusOK, out)\n}\n")

	src := b.String()
	if formatted, err := format.Source([]byte(src)); err == nil {
		src = string(formatted)
	}
	writeFileMk(filepath.Join(path, "backend/internal/handler/"+typeName(e.Name)+"_service_handler.go"), src)
}

// isReadOp reports whether an operation name denotes a read (so it may stay GET).
// Everything else is treated as a state-changing POST.
func isReadOp(name string) bool {
	for _, p := range []string{"Get", "List", "Find", "Fetch", "Search", "Query", "View"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// deriveSubpath turns an operation name into a clean kebab-case URL segment,
// dropping a leading filler verb so the route reads naturally:
//
//	ApplyDiscount -> "discount"   AdjustStock -> "adjust-stock"
//	Checkout      -> "checkout"   RecalculateTotal -> "recalculate-total"
func deriveSubpath(name string) string {
	words := strings.Fields(strings.ToLower(splitCamel(name)))
	// Drop a leading low-information verb when there's a more specific noun after.
	filler := map[string]bool{"apply": true, "do": true, "perform": true, "make": true, "set": true}
	if len(words) > 1 && filler[words[0]] {
		words = words[1:]
	}
	return strings.Join(words, "-")
}

// lowerFirst lowercases the first rune of s (e.g. "Order" -> "order"), used to
// name the shared store variable per entity in the generated main.go.
func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'A' && r[0] <= 'Z' {
		r[0] = r[0] - 'A' + 'a'
	}
	return string(r)
}

// hasServiceHandler reports whether a business-route handler file was generated
// for the entity type-name n (i.e. it had at least one business operation).
func hasServiceHandler(path, n string) bool {
	_, err := os.Stat(filepath.Join(path, "backend/internal/handler/"+n+"_service_handler.go"))
	return err == nil
}

// memModeHasAnyServiceHandler reports whether any entity has a business-route
// handler (so main.go must import the store package to construct them).
func memModeHasAnyServiceHandler(path string, names []string) bool {
	for _, n := range names {
		if hasServiceHandler(path, n) {
			return true
		}
	}
	return false
}

// genServiceTest writes a runtime contract test that calls each generated
// operation with several payloads (empty, and populated with common numeric/
// string keys) against a store that already contains one record, asserting the
// op never panics. Exercising a POPULATED payload against a PRESENT record is
// essential: many panics (unchecked type assertions on in[...] values) only fire
// on the value-processing path, which an empty payload skips. v23.24.
func genServiceTest(e schema.Entity, ops []ai.Operation, path string) {
	if len(ops) == 0 {
		return
	}
	n := typeName(e.Name)
	var b strings.Builder
	b.WriteString("// Code generated by gen-system (business logic test). DO NOT EDIT.\n")
	b.WriteString("package service\n\n")
	b.WriteString("import (\n\t\"testing\"\n\n\t\"app/backend/internal/model\"\n\t\"app/backend/internal/store\"\n)\n\n")
	fmt.Fprintf(&b, "// Test%sService_OperationsHonorContract calls each generated op with empty AND\n", n)
	b.WriteString("// populated payloads against a store holding one record, asserting it returns\n")
	b.WriteString("// (any, error) without panicking. A business error is acceptable; a panic is not.\n")
	fmt.Fprintf(&b, "func Test%sService_OperationsHonorContract(t *testing.T) {\n", n)
	fmt.Fprintf(&b, "\tst := store.New%sStore()\n", n)
	// Seed one record so store.Get(1) succeeds and the body reaches its logic.
	fmt.Fprintf(&b, "\t_, _ = st.Create(model.%s{})\n", n)
	fmt.Fprintf(&b, "\ts := New%sService(st)\n", n)
	// A set of payloads covering empty + common numeric/string keys with sane
	// values, so unchecked assertions on the value path are exercised.
	b.WriteString("\tpayloads := []map[string]any{\n")
	b.WriteString("\t\t{},\n")
	b.WriteString("\t\t{\"amount\": float64(1), \"quantity\": float64(1), \"qty\": float64(1), \"delta\": float64(1), \"count\": float64(1), \"percent\": float64(10), \"percentage\": float64(10), \"value\": float64(1), \"price\": float64(1), \"total\": float64(1), \"status\": \"open\", \"reason\": \"x\"},\n")
	b.WriteString("\t\t{\"amount\": float64(-1), \"quantity\": float64(-1), \"delta\": float64(-1), \"percent\": float64(-1)},\n")
	b.WriteString("\t}\n")
	for _, op := range ops {
		fmt.Fprintf(&b, "\tfor _, in := range payloads {\n\t\tfunc() {\n\t\t\tdefer func() {\n\t\t\t\tif rec := recover(); rec != nil {\n\t\t\t\t\tt.Errorf(%q+\" panicked on payload %%v: %%v\", in, rec)\n\t\t\t\t}\n\t\t\t}()\n\t\t\t_, _ = s.%s(1, in)\n\t\t}()\n\t}\n", op.Name, op.Name)
	}
	b.WriteString("}\n")
	src := b.String()
	if formatted, err := format.Source([]byte(src)); err == nil {
		src = string(formatted)
	}
	writeFileMk(filepath.Join(path, "backend/internal/service/"+routeName(e.Name)+"_service_test.go"), src)
}
