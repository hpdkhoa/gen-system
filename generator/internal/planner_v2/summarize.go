// generator/internal/planner_v2/summarize.go — v23.24 (task 4A): builds the
// compact "what already exists" block that grounds the continue-planner so the
// model proposes a DELTA against the real codebase rather than re-describing it.
//
// Entities + fields come from the loaded context's IR (compiler-accurate).
// Operations are detected index-first (resolved methods whose receiver is an
// <Entity>Service) with a deterministic file-scan fallback over
// service/<entity>_service.go, per the chosen design.
package planner_v2

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/context"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

// ExistingState is the structured snapshot the continue-planner summarizes from.
type ExistingState struct {
	// Entities maps entity name → ordered "field:type" descriptors.
	Entities map[string][]string
	// EntityOrder preserves a stable entity ordering for deterministic prompts.
	EntityOrder []string
	// Ops maps entity name → operation (service method) names already present.
	Ops map[string][]string
}

// SummarizeExisting reads entities/fields from the context IR and operations
// from the symbol index (fallback: service file scan rooted at ctx.Root).
func SummarizeExisting(ctx *context.Context) ExistingState {
	st := ExistingState{
		Entities: map[string][]string{},
		Ops:      map[string][]string{},
	}
	if ctx == nil {
		return st
	}

	// Entities/fields: prefer the TARGET's own model structs (recovery), which
	// are tied to the actual codebase. A .gen IR snapshot lives in the working
	// directory, not the target, so it is unreliable for an arbitrary path and
	// is used only when no model structs are found.
	irEntities := RecoverEntitiesFromModels(ctx.Root)
	usedRecovery := len(irEntities) > 0
	if !usedRecovery {
		for _, e := range ctx.IR.Entities {
			fields := make([]schema.Field, 0, len(e.Fields))
			for _, f := range e.Fields {
				fields = append(fields, schema.Field{Name: f.Name, Type: f.Type})
			}
			irEntities = append(irEntities, schema.Entity{Name: e.Name, Fields: fields})
		}
	}
	for _, e := range irEntities {
		descs := make([]string, 0, len(e.Fields))
		for _, f := range e.Fields {
			descs = append(descs, f.Name+":"+f.Type)
		}
		st.Entities[e.Name] = descs
		st.EntityOrder = append(st.EntityOrder, e.Name)
	}
	sort.Strings(st.EntityOrder)

	// Operations: index-first.
	indexed := opsFromIndex(ctx.Index, st.EntityOrder)
	for ent, ops := range indexed {
		st.Ops[ent] = ops
	}
	// Fallback: for any entity with no ops found in the index, scan its service
	// file on disk (handles projects loaded without a fully resolved index).
	for _, ent := range st.EntityOrder {
		if len(st.Ops[ent]) == 0 {
			if ops := opsFromServiceFile(ctx.Root, ent); len(ops) > 0 {
				st.Ops[ent] = ops
			}
		}
	}
	return st
}

// opsFromIndex returns entity → service-method names, using resolved symbols
// whose receiver type is "<Entity>Service".
func opsFromIndex(idx *symbols.Index, entityOrder []string) map[string][]string {
	out := map[string][]string{}
	if idx == nil {
		return out
	}
	want := map[string]string{} // "<Entity>Service" → Entity
	for _, e := range entityOrder {
		want[e+"Service"] = e
	}
	for _, s := range idx.AllSymbols() {
		if s.Kind != symbols.SymMethod {
			continue
		}
		ent, ok := want[s.ReceiverType]
		if !ok {
			continue
		}
		if isExportedName(s.Name) {
			out[ent] = appendUniqueStr(out[ent], s.Name)
		}
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out
}

var serviceMethodRe = regexp.MustCompile(`func \(s \*(\w+)Service\) (\w+)\(`)

// opsFromServiceFile scans backend/internal/service/<entity>_service.go for
// exported method names on <Entity>Service. Best-effort; returns nil on miss.
func opsFromServiceFile(root, entity string) []string {
	if root == "" {
		return nil
	}
	candidates := []string{
		filepath.Join(root, "backend", "internal", "service", strings.ToLower(entity)+"_service.go"),
		filepath.Join(root, "internal", "service", strings.ToLower(entity)+"_service.go"),
	}
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var ops []string
		for _, m := range serviceMethodRe.FindAllStringSubmatch(string(data), -1) {
			recv, name := m[1], m[2]
			if recv == entity && isExportedName(name) {
				ops = appendUniqueStr(ops, name)
			}
		}
		sort.Strings(ops)
		return ops
	}
	return nil
}

// RecoverEntitiesFromModels reads backend/internal/model/*.go structs directly
// and reconstructs []schema.Entity. This is the fallback used when the IR
// snapshot (.gen/ir.json) is absent — e.g. continuing a generated app from a
// fresh checkout where no IR was carried. The model files are the deterministic
// inverse of what the generator wrote, so this recovers the true entity set.
func RecoverEntitiesFromModels(root string) []schema.Entity {
	if root == "" {
		return nil
	}
	dirs := []string{
		filepath.Join(root, "backend", "internal", "model"),
		filepath.Join(root, "internal", "model"),
	}
	var out []schema.Entity
	for _, dir := range dirs {
		ents, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, de := range ents {
			if de.IsDir() || !strings.HasSuffix(de.Name(), ".go") || strings.HasSuffix(de.Name(), "_test.go") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, de.Name()))
			if err != nil {
				continue
			}
			out = append(out, parseModelStructs(string(data))...)
		}
		if len(out) > 0 {
			break // first model dir that yields structs wins
		}
	}
	return out
}

var (
	structHeadRe  = regexp.MustCompile(`type (\w+) struct \{`)
	structFieldRe = regexp.MustCompile(`^\s*(\w+)\s+([\w\.\[\]\*]+)\s+` + "`" + `json:"([^"]+)"`)
)

// parseModelStructs extracts entities from a single model file's source.
func parseModelStructs(src string) []schema.Entity {
	var out []schema.Entity
	lines := strings.Split(src, "\n")
	i := 0
	for i < len(lines) {
		head := structHeadRe.FindStringSubmatch(lines[i])
		if head == nil {
			i++
			continue
		}
		name := head[1]
		i++
		var fields []schema.Field
		for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "}") {
			if m := structFieldRe.FindStringSubmatch(lines[i]); m != nil {
				goType := m[2]
				jsonTag := m[3]
				if comma := strings.Index(jsonTag, ","); comma != -1 {
					jsonTag = jsonTag[:comma]
				}
				fields = append(fields, schema.Field{
					Name: jsonTag,
					Type: goTypeToSchemaType(goType),
				})
			}
			i++
		}
		out = append(out, schema.Entity{Name: name, Fields: fields})
	}
	return out
}

// goTypeToSchemaType reverse-maps generated Go field types back to schema types.
func goTypeToSchemaType(goType string) string {
	switch strings.TrimPrefix(goType, "*") {
	case "int", "int64", "int32":
		return "int"
	case "float64", "float32":
		return "float"
	case "bool":
		return "bool"
	case "time.Time":
		return "time"
	case "string":
		return "string"
	default:
		return "string"
	}
}

// RenderExistingSummary turns the snapshot into the compact text block injected
// into the continue prompt.
func RenderExistingSummary(st ExistingState) string {
	if len(st.EntityOrder) == 0 {
		return "(the project currently has no recognized entities)"
	}
	var b strings.Builder
	for _, ent := range st.EntityOrder {
		fields := st.Entities[ent]
		b.WriteString(fmt.Sprintf("- %s { %s }", ent, strings.Join(fields, ", ")))
		if ops := st.Ops[ent]; len(ops) > 0 {
			b.WriteString(fmt.Sprintf("  ops: [%s]", strings.Join(ops, ", ")))
		} else {
			b.WriteString("  ops: [none]")
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// isExportedName reports whether a Go identifier is exported (starts upper).
func isExportedName(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	return c >= 'A' && c <= 'Z'
}

func appendUniqueStr(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}
