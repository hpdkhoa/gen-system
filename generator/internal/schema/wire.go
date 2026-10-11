// generator/internal/schema/wire.go (v26.57): the one place that decides how an
// entity and its fields appear on the wire of a generated app.
//
// Until v26.57 the Go backend (internal/engine) and the TypeScript client
// (internal/frontend) each derived these names with their own rules. The
// backend served /blogposts with the JSON key user_id; the client called
// /blogPosts and read userId. Every call of a multi-word entity got 404, and
// every multi-word field read undefined (defects D002 and D004). Both
// generators now call the functions below, so the two sides cannot drift.
//
// The rules are the historical backend rules, unchanged, so apps generated
// before v26.57 keep their routes, table names and column names.
package schema

import "strings"

// DefaultBackendPort is the port the generated backend listens on when PORT is
// not set, and the port the generated .env and the mobile client name.
// v26.58: before, .env said 28080 while main.go listened on 8080.
const DefaultBackendPort = "8080"

// SplitIdent splits a string on any run of non-alphanumeric characters,
// dropping empties. "due date" -> [due date]; "user-id" -> [user id].
func SplitIdent(s string) []string {
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

// ExportName turns an arbitrary field name into a valid exported Go identifier:
// split on non-alphanumeric runs, Title-case each part, join, guard a leading
// digit.
func ExportName(s string) string {
	parts := SplitIdent(s)
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

// JSONKey is the JSON key (and SQL column) of a field: the SplitIdent parts,
// lower-cased, joined with "_". "created at" -> "created_at"; "createdAt" ->
// "createdat".
func JSONKey(s string) string {
	parts := SplitIdent(s)
	if len(parts) == 0 {
		return "field"
	}
	for i := range parts {
		parts[i] = strings.ToLower(parts[i])
	}
	return strings.Join(parts, "_")
}

// RouteName turns an entity name into a lower-case path segment with no
// spaces or punctuation ("Order Item" -> "orderitem", "BlogPost" -> "blogpost").
func RouteName(s string) string {
	return strings.ToLower(strings.Join(SplitIdent(s), ""))
}

// Plural appends "s" unless the word already ends in "s".
func Plural(s string) string {
	if strings.HasSuffix(s, "s") {
		return s
	}
	return s + "s"
}

// CollectionPath is the REST collection path the generated backend serves for
// an entity, e.g. "/blogposts". Item routes are CollectionPath + "/{id}".
func CollectionPath(entity string) string { return "/" + Plural(RouteName(entity)) }

// SplitCamel inserts a space at lower→upper and letter→acronym boundaries so
// SplitIdent/JSONKey produce snake_case ("UserID" -> "User ID" -> "user_id").
// Already-delimited names pass through unchanged.
func SplitCamel(s string) string {
	var b strings.Builder
	rs := []rune(s)
	for i, r := range rs {
		if i > 0 {
			prev := rs[i-1]
			upper := r >= 'A' && r <= 'Z'
			prevLower := prev >= 'a' && prev <= 'z'
			prevDigit := prev >= '0' && prev <= '9'
			if upper && (prevLower || prevDigit) {
				b.WriteByte(' ')
			} else if upper && i+1 < len(rs) {
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

// FKFieldName is the field name of the foreign key a belongsTo relation adds
// ("UserID" -> "User ID", JSON key "user_id").
func FKFieldName(name string) string { return SplitCamel(name) }

// RelationFields derives the int foreign-key fields of an entity's belongsTo
// relations. The name comes from ForeignKey, else <Target>ID.
func RelationFields(e Entity) []Field {
	var out []Field
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
		out = append(out, Field{Name: FKFieldName(name), Type: "int"})
	}
	return out
}

// ModelFields returns the fields the generated model struct carries besides
// the built-in id, in order: the explicit fields, then the relation foreign
// keys. A field whose identifier or JSON key repeats an earlier one, or the
// built-in id, is dropped. The model, the stores, the SQL table and the
// TypeScript interface all use this list, so they agree on the exact set of
// keys.
func ModelFields(e Entity) []Field {
	var out []Field
	usedIdent := map[string]bool{"id": true}
	usedTag := map[string]bool{"id": true}
	add := func(f Field) {
		ident := strings.ToLower(ExportName(f.Name))
		tag := strings.ToLower(JSONKey(f.Name))
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
	for _, f := range RelationFields(e) {
		add(f)
	}
	return out
}

// GoFieldType is the Go type the generated model gives a field of the given
// schema type. Unknown types become string. Time values travel as ISO-8601
// strings so the in-memory app needs no time dependency.
func GoFieldType(t string) string {
	switch strings.ToLower(t) {
	case "int", "integer":
		return "int"
	case "bool", "boolean":
		return "bool"
	case "float", "float64", "double", "decimal":
		return "float64"
	default:
		return "string"
	}
}
