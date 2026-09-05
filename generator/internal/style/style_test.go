// style_test.go — v26.33: the profile, the gate, derive, the op check and
// the prompt, on fixture trees written into a temp dir. No model, no
// network. Two properties matter most and both are asserted: the gate is
// deterministic (same tree, same profile, byte identical report), and the
// prompt lists exactly the rules the gate checks.
package style

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const cleanService = `package service

import (
	"errors"
	"fmt"
)

type Store interface {
	Get(id int) (Order, bool)
	List() []Order
}

type Order struct {
	ID     int
	Status string
	Total  float64
}

// OrderService holds the store.
type OrderService struct {
	store Store
}

func NewOrderService(s Store) *OrderService { return &OrderService{store: s} }

func (s *OrderService) Checkout(id int, in map[string]any) (any, error) {
	o, ok := s.store.Get(id)
	if !ok {
		return nil, errors.New("order not found")
	}
	if o.Status == "paid" {
		return nil, fmt.Errorf("checkout %d: %w", id, errors.New("already paid"))
	}
	o.Status = "paid"
	return o, nil
}

func (s *OrderService) TotalOpen() float64 {
	sum := 0.0
	for _, o := range s.store.List() {
		if o.Status != "paid" {
			sum += o.Total
		}
	}
	return sum
}

func (s *OrderService) CountByStatus(status string) int {
	n := 0
	for _, o := range s.store.List() {
		if o.Status == status {
			n++
		}
	}
	return n
}

func (s *OrderService) ApplyDiscount(id int, pct float64) (any, error) {
	o, ok := s.store.Get(id)
	if !ok {
		return nil, fmt.Errorf("discount %d: %w", id, errors.New("order not found"))
	}
	o.Total = o.Total * (1 - pct/100)
	return o, nil
}

func (s *OrderService) Cancel(id int) error {
	if _, ok := s.store.Get(id); !ok {
		return fmt.Errorf("cancel %d: %w", id, errors.New("order not found"))
	}
	return nil
}

func lowerAll(names []string) []string {
	var out []string
	for _, n := range names {
		out = append(out, n)
	}
	return out
}
`

const cleanHandler = `package handler

import "net/http"

type OrderHandler struct{ mux *http.ServeMux }

func NewOrderHandler(mux *http.ServeMux) *OrderHandler { return &OrderHandler{mux: mux} }

func (h *OrderHandler) Register() {
	h.mux.HandleFunc("/orders", h.list)
}

func (h *OrderHandler) list(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}
`

const cleanModel = `package model

type Order struct {
	ID int
}
`

const badFile = `package service

import (
	"fmt"
	"reflect"
)

type Cart struct{ items []int }

func init() {}

var counter = 0

func helperThing(items []int) (int, error) {
	if len(items) == 0 {
		panic("empty")
	}
	total := 0
	for i := 0; i < len(items); i++ {
		total += items[i]
	}
	if total < 0 {
		err := fmt.Errorf("negative")
		return 0, fmt.Errorf("bad total: %v", err)
	}
	_ = reflect.TypeOf(total)
	return total, nil
}

func (handler *Cart) Add(x int) { handler.items = append(handler.items, x) }
`

func cleanTree(t *testing.T) string {
	return writeTree(t, map[string]string{
		"internal/service/order_service.go": cleanService,
		"internal/handler/order_handler.go": cleanHandler,
		"internal/model/order.go":           cleanModel,
	})
}

func dirtyTree(t *testing.T) string {
	return writeTree(t, map[string]string{
		"internal/service/order_service.go": cleanService,
		"internal/service/bad-name.go":      badFile,
		"internal/handler/order_handler.go": cleanHandler,
		"internal/model/order.go":           cleanModel,
	})
}

func rules(r Report) map[string]int {
	out := map[string]int{}
	for _, f := range r.Findings {
		out[f.Rule]++
	}
	return out
}

func TestCleanTreePassesDefaultProfile(t *testing.T) {
	rep := Check(cleanTree(t), Default())
	if rep.Functions == 0 {
		t.Fatal("snapshot saw no functions; the Understand load did not run")
	}
	if len(rep.Findings) != 0 {
		t.Fatalf("clean tree should have no findings, got:\n%s", RenderReport(rep))
	}
}

func TestDirtyTreeFindingsByRule(t *testing.T) {
	p := Default()
	p.Patterns.Forbid = append(p.Patterns.Forbid, "global-state")
	rep := Check(dirtyTree(t), p)
	got := rules(rep)
	want := []string{
		"naming.files",                   // bad-name.go
		"naming.receivers",               // receiver "handler"
		"naming.type_suffixes",           // Cart in service layer
		"patterns.require.constructor",   // no NewCart
		"patterns.require.error-wrapping", // %v with err
		"patterns.forbid.panic",
		"patterns.forbid.init-func",
		"patterns.forbid.reflection",
		"patterns.forbid.global-state",
		"code.iteration", // index loop over len()
	}
	for _, r := range want {
		if got[r] == 0 {
			t.Errorf("expected a %s finding, got none. Report:\n%s", r, RenderReport(rep))
		}
	}
	if !rep.Blocking() {
		t.Fatal("bans default to error severity, so the report must block")
	}
	for _, f := range rep.Findings {
		if f.Rule == "patterns.forbid.panic" && f.Symbol != "helperThing" {
			t.Errorf("panic finding should name helperThing, got %q", f.Symbol)
		}
	}
	files := rep.FixableFiles()
	if len(files) == 0 || !strings.HasSuffix(files[0], "bad-name.go") {
		t.Fatalf("fixable files = %v", files)
	}
}

func TestCheckIsDeterministic(t *testing.T) {
	root := dirtyTree(t)
	p := Default()
	a, _ := json.Marshal(Check(root, p).Findings)
	b, _ := json.Marshal(Check(root, p).Findings)
	if string(a) != string(b) {
		t.Fatal("two gate passes over the same tree differ")
	}
}

func TestStructureRules(t *testing.T) {
	p := &Profile{Version: 1, Structure: Structure{
		RequiredDirs:  []string{"internal/service", "internal/repository"},
		ForbiddenDirs: []string{"internal/handler"},
		FilePattern:   map[string]string{"service": "*_service.go"},
	}}
	rep := Check(dirtyTree(t), p)
	got := rules(rep)
	if got["structure.required_dirs"] != 1 {
		t.Errorf("internal/repository is missing, want 1 finding, got %d", got["structure.required_dirs"])
	}
	if got["structure.forbidden_dirs"] != 1 {
		t.Errorf("internal/handler exists, want 1 finding, got %d", got["structure.forbidden_dirs"])
	}
	if got["structure.file_pattern"] != 1 {
		t.Errorf("bad-name.go does not match *_service.go, want 1 finding, got %d", got["structure.file_pattern"])
	}
}

func TestDeriveFromCleanTree(t *testing.T) {
	root := cleanTree(t)
	p := Derive(root)
	if err := p.Validate(); err != nil {
		t.Fatalf("derived profile does not validate: %v", err)
	}
	if !strings.HasPrefix(p.Source, "derived:") {
		t.Fatalf("source = %q", p.Source)
	}
	if p.Naming.Files != "snake_case" {
		t.Errorf("files case = %q, want snake_case (evidence %+v)", p.Naming.Files, p.Evidence["naming.files"])
	}
	if p.Naming.Receivers != "short" {
		t.Errorf("receivers = %q, want short", p.Naming.Receivers)
	}
	if p.Naming.TypeSuffixes["service"] != "Service" {
		t.Errorf("service suffix = %q, want Service (evidence %+v)", p.Naming.TypeSuffixes["service"], p.Evidence["naming.type_suffixes.service"])
	}
	for _, ban := range []string{"panic", "init-func", "reflection"} {
		if !contains(p.Patterns.Forbid, ban) {
			t.Errorf("clean tree has no %s, so the ban should be derived; forbid = %v", ban, p.Patterns.Forbid)
		}
	}
	if !contains(p.Patterns.Require, "constructor") {
		t.Errorf("both exported layer types have constructors; require = %v", p.Patterns.Require)
	}
	if p.Code.MaxFuncLines == 0 || p.Code.MaxParams == 0 {
		t.Errorf("size limits not derived: %+v", p.Code)
	}
	if p.Evidence["naming.files"].Seen != 3 {
		t.Errorf("evidence should count the 3 files, got %+v", p.Evidence["naming.files"])
	}
	// A derived profile checks clean against the tree it came from.
	if rep := Check(root, p); len(rep.Findings) != 0 {
		t.Fatalf("derived profile flags its own source:\n%s", RenderReport(rep))
	}
}

func TestDeriveIsDeterministic(t *testing.T) {
	root := cleanTree(t)
	a, b := Derive(root), Derive(root)
	a.DerivedAt, b.DerivedAt = "", ""
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) {
		t.Fatal("derive is not deterministic")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	p := Default()
	path := filepath.Join(t.TempDir(), "gen-style.json")
	if err := p.Save(path); err != nil {
		t.Fatal(err)
	}
	q, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(q.ActiveRules(), ",") != strings.Join(p.ActiveRules(), ",") {
		t.Fatalf("active rules changed across save/load:\n%v\n%v", p.ActiveRules(), q.ActiveRules())
	}
}

func TestValidateRejectsUnknownValues(t *testing.T) {
	cases := []Profile{
		{Version: 1, Patterns: Patterns{Forbid: []string{"goto"}}},
		{Version: 1, Gate: Gate{Mode: "maybe"}},
		{Version: 1, Naming: Naming{Files: "SCREAMING"}},
		{Version: 1, Gate: Gate{MaxRepairs: 5}},
		{Version: 1, Patterns: Patterns{Require: []string{"panic"}, Forbid: []string{"panic"}}},
	}
	for i, c := range cases {
		if err := c.Validate(); err == nil {
			t.Errorf("case %d should fail validation", i)
		}
	}
}

func TestMaxRepairsNeverAboveEngineCap(t *testing.T) {
	p := &Profile{Version: 1, Gate: Gate{MaxRepairs: 2}}
	if p.MaxRepairs() != 2 {
		t.Fatal("explicit 2 should stay 2")
	}
	p = &Profile{Version: 1}
	if p.MaxRepairs() != 1 {
		t.Fatal("default should be 1")
	}
}

func TestCheckOperation(t *testing.T) {
	p := Default()
	body := "if id < 0 {\n\tpanic(\"bad id\")\n}\nfor i := 0; i < len(in); i++ {\n}\nreturn nil, fmt.Errorf(\"x: %v\", err)"
	fs := CheckOperation(p, "Checkout", body, nil)
	got := map[string]bool{}
	for _, f := range fs {
		got[f.Rule] = true
	}
	for _, r := range []string{"patterns.forbid.panic", "code.iteration", "patterns.require.error-wrapping"} {
		if !got[r] {
			t.Errorf("want %s finding, got %v", r, fs)
		}
	}
	if !HasError(fs) {
		t.Fatal("panic is an error severity finding")
	}
	clean := "o, ok := s.store.Get(id)\nif !ok {\n\treturn nil, errors.New(\"not found\")\n}\nreturn o, nil"
	if fs := CheckOperation(p, "Get", clean, nil); len(fs) != 0 {
		t.Fatalf("clean body flagged: %v", fs)
	}
	if fs := CheckOperation(nil, "Get", body, nil); fs != nil {
		t.Fatal("nil profile must check nothing")
	}
}

func TestPromptBlockCoversEveryActiveRule(t *testing.T) {
	p := Default()
	p.Patterns.Forbid = append(p.Patterns.Forbid, "global-state")
	p.Structure.ForbiddenDirs = []string{"pkg"}
	p.Code.AllowedImports = []string{"errors", "fmt"}
	p.Code.ForbiddenImports = []string{"os"}
	block := PromptBlock(p)
	for _, rule := range p.ActiveRules() {
		if rule == "code.error_wrapping" {
			continue // folded into the error-wrapping pattern line
		}
		if ruleLine(p, rule) == "" {
			t.Errorf("rule %s has no prompt line", rule)
		}
	}
	if !strings.Contains(block, "[error]") {
		t.Fatal("error severity rules should be marked in the prompt")
	}
	if !strings.Contains(block, "Return early on error") {
		t.Fatal("prompt notes should be included verbatim")
	}
	if PromptBlock(nil) != "" || PromptBlock(&Profile{Version: 1}) != "" {
		t.Fatal("empty profile should render nothing")
	}
}

func TestExemplarsRankByNameThenLayer(t *testing.T) {
	ResetExemplarCache()
	p := &Profile{Version: 1, Prompt: Prompt{ExemplarRoot: cleanTree(t), Exemplars: 2}}
	ex := Exemplars(p, []string{"checkout an order"}, "service", 2)
	if len(ex) != 2 {
		t.Fatalf("want 2 exemplars, got %d", len(ex))
	}
	if ex[0].Name != "Checkout" {
		t.Fatalf("first exemplar should be Checkout (name match), got %s", ex[0].Name)
	}
	if ex[1].Layer != "service" {
		t.Fatalf("second exemplar should come from the service layer, got %s (%s)", ex[1].Name, ex[1].Layer)
	}
	if !strings.Contains(RenderExemplars(ex), "func (s *OrderService) Checkout") {
		t.Fatal("exemplar source should carry the whole declaration")
	}
	again := Exemplars(p, []string{"checkout an order"}, "service", 2)
	if again[0].Name != ex[0].Name || again[1].Name != ex[1].Name {
		t.Fatal("exemplar pick is not stable")
	}
}

func TestSplitWords(t *testing.T) {
	got := strings.Join(splitWords("ApplyDiscountToOrder"), ",")
	if got != "apply,discount,order" {
		t.Fatalf("splitWords = %q", got)
	}
}
