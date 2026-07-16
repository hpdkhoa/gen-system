// nit/nit_test.go — tests for the AST-NIT serialiser
package nit

import (
	"strings"
	"testing"

	"gen-system/generator/internal/ast"
)

// ─── Helpers ──────────────────────────────────────────────────

func goModule(pkg string, nodes ...*ast.Node) *ast.Module {
	return &ast.Module{
		Language:    ast.LangGo,
		FilePath:    "pkg/test.go",
		PackageName: pkg,
		Nodes:       nodes,
	}
}

func funcNode(name string, exported bool, params []ast.Parameter, returns []ast.TypeRef, children ...*ast.Node) *ast.Node {
	return &ast.Node{
		Kind:     ast.NodeFunction,
		Name:     name,
		Exported: exported,
		Params:   params,
		Returns:  returns,
		Children: children,
	}
}

func callNode(callee string) *ast.Node {
	return &ast.Node{Kind: ast.NodeCall, Callee: callee}
}

func structNode(name string, fields ...ast.Field) *ast.Node {
	return &ast.Node{
		Kind:     ast.NodeStruct,
		Name:     name,
		Exported: true,
		Fields:   fields,
	}
}

func unknownNode(reason string) *ast.Node {
	return &ast.Node{
		Kind:          ast.NodeUnknown,
		UnknownReason: reason,
	}
}

func linesContaining(lines []string, sub string) []string {
	var out []string
	for _, l := range lines {
		if strings.Contains(l, sub) {
			out = append(out, l)
		}
	}
	return out
}

// ─── Package line ─────────────────────────────────────────────

func TestSerialize_PackageLine(t *testing.T) {
	m := goModule("handler")
	lines := Serialize(m, DefaultOptions)
	if len(lines) == 0 {
		t.Fatal("Serialize returned no lines")
	}
	if lines[0] != "PKG  handler" {
		t.Errorf("first line = %q, want %q", lines[0], "PKG  handler")
	}
}

func TestSerialize_NilModule(t *testing.T) {
	lines := Serialize(nil, DefaultOptions)
	if len(lines) != 0 {
		t.Errorf("nil module should return no lines, got %v", lines)
	}
}

// ─── Import ───────────────────────────────────────────────────

func TestSerialize_Import(t *testing.T) {
	imp := &ast.Node{
		Kind:       ast.NodeImport,
		ImportPath: "fmt",
	}
	m := goModule("main", imp)
	lines := Serialize(m, DefaultOptions)
	impLines := linesContaining(lines, "IMP")
	if len(impLines) == 0 {
		t.Fatalf("no IMP line; all lines: %v", lines)
	}
	if !strings.Contains(impLines[0], "fmt") {
		t.Errorf("import line = %q, want fmt", impLines[0])
	}
}

func TestSerialize_ImportWithAlias(t *testing.T) {
	imp := &ast.Node{
		Kind:        ast.NodeImport,
		ImportPath:  "database/sql",
		ImportAlias: "db",
	}
	m := goModule("main", imp)
	lines := Serialize(m, DefaultOptions)
	impLines := linesContaining(lines, "IMP")
	if len(impLines) == 0 {
		t.Fatal("no IMP line")
	}
	if !strings.Contains(impLines[0], "as db") {
		t.Errorf("expected alias in line: %q", impLines[0])
	}
}

// ─── Function ─────────────────────────────────────────────────

func TestSerialize_Function_Basic(t *testing.T) {
	fn := funcNode("CreateUser", true,
		[]ast.Parameter{{Name: "name", Type: ast.TypeRef{Name: "string"}}},
		[]ast.TypeRef{{Name: "error"}},
	)
	m := goModule("svc", fn)
	lines := Serialize(m, DefaultOptions)
	fnLines := linesContaining(lines, "FN")
	if len(fnLines) == 0 {
		t.Fatalf("no FN line; all: %v", lines)
	}
	line := fnLines[0]
	if !strings.Contains(line, "CreateUser") {
		t.Errorf("function name missing: %q", line)
	}
	if !strings.Contains(line, "name:string") {
		t.Errorf("param missing: %q", line)
	}
	if !strings.Contains(line, "error") {
		t.Errorf("return type missing: %q", line)
	}
}

func TestSerialize_Function_WithCallees(t *testing.T) {
	fn := funcNode("Run", true, nil, nil,
		callNode("db.Exec"),
		callNode("log.Printf"),
		callNode("validate"),
	)
	m := goModule("svc", fn)
	lines := Serialize(m, DefaultOptions)
	fnLines := linesContaining(lines, "FN")
	if len(fnLines) == 0 {
		t.Fatal("no FN line")
	}
	line := fnLines[0]
	if !strings.Contains(line, "[calls:") {
		t.Errorf("calls list missing: %q", line)
	}
	if !strings.Contains(line, "db.Exec") {
		t.Errorf("db.Exec callee missing: %q", line)
	}
}

func TestSerialize_Function_MaxCallees(t *testing.T) {
	children := make([]*ast.Node, 10)
	for i := range children {
		children[i] = callNode("fn" + string(rune('A'+i)))
	}
	fn := funcNode("Big", true, nil, nil, children...)
	m := goModule("p", fn)

	opts := Options{MaxCallees: 3}
	lines := Serialize(m, opts)
	fnLines := linesContaining(lines, "FN")
	if len(fnLines) == 0 {
		t.Fatal("no FN line")
	}
	// Count commas in calls list = callees - 1
	if idx := strings.Index(fnLines[0], "[calls:"); idx >= 0 {
		callsPart := fnLines[0][idx:]
		commas := strings.Count(callsPart, ",")
		if commas >= 3 {
			t.Errorf("MaxCallees=3 should limit to 3 entries (2 commas); got %d commas in %q", commas, callsPart)
		}
	}
}

// ─── Method ───────────────────────────────────────────────────

func TestSerialize_Method(t *testing.T) {
	n := &ast.Node{
		Kind:     ast.NodeMethod,
		Name:     "Save",
		Exported: true,
		Receiver: &ast.TypeRef{Name: "UserRepo"},
		Params:   []ast.Parameter{{Name: "u", Type: ast.TypeRef{Name: "User"}}},
		Returns:  []ast.TypeRef{{Name: "error"}},
	}
	m := goModule("repo", n)
	lines := Serialize(m, DefaultOptions)
	mthLines := linesContaining(lines, "MTH")
	if len(mthLines) == 0 {
		t.Fatalf("no MTH line; all: %v", lines)
	}
	if !strings.Contains(mthLines[0], "UserRepo.Save") {
		t.Errorf("method line = %q, want UserRepo.Save", mthLines[0])
	}
}

// ─── Struct ───────────────────────────────────────────────────

func TestSerialize_Struct(t *testing.T) {
	n := structNode("User",
		ast.Field{Name: "ID", Type: ast.TypeRef{Name: "int"}, Exported: true},
		ast.Field{Name: "Name", Type: ast.TypeRef{Name: "string"}, Exported: true},
	)
	m := goModule("model", n)
	lines := Serialize(m, DefaultOptions)
	strLines := linesContaining(lines, "STR")
	if len(strLines) == 0 {
		t.Fatal("no STR line")
	}
	if !strings.Contains(strLines[0], "User") {
		t.Errorf("struct name missing: %q", strLines[0])
	}
	if !strings.Contains(strLines[0], "ID:int") {
		t.Errorf("field ID:int missing: %q", strLines[0])
	}
}

func TestSerialize_Struct_OmitFieldTypes(t *testing.T) {
	n := structNode("User",
		ast.Field{Name: "ID", Type: ast.TypeRef{Name: "int"}, Exported: true},
	)
	m := goModule("model", n)
	opts := Options{OmitFieldTypes: true}
	lines := Serialize(m, opts)
	strLines := linesContaining(lines, "STR")
	if len(strLines) == 0 {
		t.Fatal("no STR line")
	}
	if strings.Contains(strLines[0], ":int") {
		t.Errorf("OmitFieldTypes: type should be omitted but got: %q", strLines[0])
	}
	if !strings.Contains(strLines[0], "ID") {
		t.Errorf("field name should still appear: %q", strLines[0])
	}
}

// ─── Unknown node ─────────────────────────────────────────────

func TestSerialize_UnknownAlwaysEmitted(t *testing.T) {
	n := unknownNode("unsupported_directive")
	// Even with ExportedOnly=true, unknowns must appear.
	m := goModule("p", n)
	opts := Options{ExportedOnly: true}
	lines := Serialize(m, opts)
	unkLines := linesContaining(lines, "UNK")
	if len(unkLines) == 0 {
		t.Fatal("UNK line must appear even with ExportedOnly=true")
	}
	if !strings.Contains(unkLines[0], "unsupported_directive") {
		t.Errorf("unknown reason missing: %q", unkLines[0])
	}
}

// ─── ExportedOnly filter ──────────────────────────────────────

func TestSerialize_ExportedOnly_FiltersUnexported(t *testing.T) {
	exported := funcNode("PublicFn", true, nil, nil)
	unexported := funcNode("privateFn", false, nil, nil)
	m := goModule("svc", exported, unexported)
	opts := Options{ExportedOnly: true}
	lines := Serialize(m, opts)
	fnLines := linesContaining(lines, "FN")
	for _, l := range fnLines {
		if strings.Contains(l, "privateFn") {
			t.Errorf("ExportedOnly: unexported fn leaked: %q", l)
		}
	}
	if len(fnLines) == 0 {
		t.Error("ExportedOnly should keep exported fn PublicFn")
	}
}

// ─── CA Gen Procedure ─────────────────────────────────────────

func TestSerialize_Procedure(t *testing.T) {
	n := &ast.Node{
		Kind:     ast.NodeProcedure,
		Name:     "PROCESS-ORDER",
		Exported: true,
		Children: []*ast.Node{
			callNode("READ-ORDERS"),
			callNode("WRITE-AUDIT"),
		},
	}
	m := &ast.Module{
		Language:    ast.LangCAGen,
		FilePath:    "order.ad",
		PackageName: "PROCESS-ORDER",
		Nodes:       []*ast.Node{n},
	}
	lines := Serialize(m, DefaultOptions)
	// v18.4 (R3): the previous filter was linesContaining(lines, "PRO"),
	// which also matched the PKG line "PKG  PROCESS-ORDER" because the
	// package name contains the substring "PRO". proLines[0] then held
	// the PKG line and the READ-ORDERS assertion failed even though the
	// serializer's output was correct. Filter by the actual NIT prefix
	// "PRO  " (with the two-space gap after the kind code), which only
	// occurs at the start of a procedure record.
	proLines := linesContaining(lines, "PRO  ")
	if len(proLines) == 0 {
		t.Fatalf("no PRO line; all: %v", lines)
	}
	// Also defensively skip any line that happens to start with "PKG " —
	// it can never be a procedure record. This keeps the test correct
	// even if someone changes the NIT prefix format later.
	proLine := ""
	for _, l := range proLines {
		if strings.HasPrefix(l, "PRO  ") {
			proLine = l
			break
		}
	}
	if proLine == "" {
		t.Fatalf("no line starts with \"PRO  \"; all: %v", lines)
	}
	if !strings.Contains(proLine, "PROCESS-ORDER") {
		t.Errorf("procedure name missing: %q", proLine)
	}
	if !strings.Contains(proLine, "READ-ORDERS") {
		t.Errorf("callee READ-ORDERS missing: %q", proLine)
	}
}

// ─── TokenEstimate ────────────────────────────────────────────

func TestTokenEstimate_NonZero(t *testing.T) {
	fn := funcNode("Hello", true, nil, nil)
	m := goModule("p", fn)
	lines := Serialize(m, DefaultOptions)
	est := TokenEstimate(lines)
	if est <= 0 {
		t.Errorf("TokenEstimate = %d, want > 0", est)
	}
}

func TestTokenEstimate_LessThanRawSource(t *testing.T) {
	// Serialised skeleton should be smaller than raw Go source.
	rawSrc := `package handler

import (
	"net/http"
	"encoding/json"
)

// CreateUserHandler handles POST /users.
// It validates the request, calls the service layer, and returns the created user.
func CreateUserHandler(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	user, err := userService.Create(req.Name, req.Email)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	json.NewEncoder(w).Encode(user)
}
`
	fe := ast.ForLanguage(ast.LangGo)
	m, err := fe.Parse("handler.go", []byte(rawSrc))
	if err != nil || m == nil {
		t.Skip("Go frontend parse failed — skipping token comparison")
	}
	lines := Serialize(m, DefaultOptions)
	nitTokens := TokenEstimate(lines)
	rawTokens := (len(rawSrc) + 3) / 4
	if nitTokens >= rawTokens {
		t.Errorf("NIT tokens (%d) should be < raw tokens (%d)", nitTokens, rawTokens)
	}
}

// ─── SerializeToString ────────────────────────────────────────

func TestSerializeToString_NonEmpty(t *testing.T) {
	m := goModule("p", funcNode("F", true, nil, nil))
	out := SerializeToString(m, DefaultOptions)
	if out == "" {
		t.Error("SerializeToString returned empty string")
	}
	if !strings.Contains(out, "\n") {
		t.Error("SerializeToString should join lines with newlines")
	}
}

// ─── Enum ─────────────────────────────────────────────────────

func TestSerialize_Enum(t *testing.T) {
	n := &ast.Node{
		Kind:     ast.NodeEnum,
		Name:     "Status",
		Exported: true,
		EnumMembers: []ast.EnumMember{
			{Name: "Active", Value: "0"},
			{Name: "Inactive", Value: "1"},
		},
	}
	m := goModule("model", n)
	lines := Serialize(m, DefaultOptions)
	enmLines := linesContaining(lines, "ENM")
	if len(enmLines) == 0 {
		t.Fatal("no ENM line")
	}
	if !strings.Contains(enmLines[0], "Active") {
		t.Errorf("enum member missing: %q", enmLines[0])
	}
}
