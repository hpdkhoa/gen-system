// generator/internal/belief/belief_test.go — v26.9: unit tests for the belief
// store. Covers the relocated store (s1), Box1/Box2 staleness (s2), and the
// behavioral parser-eval kinds (s3).
//
// Fixtures build an index from an ast.Module (symbols derived by BuildTable). The
// behavioral rebuild emits, per callable: action (always, from the name), inputs
// (if params), calls (if resolved callee), effect + data-change (if effects).
package belief

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/flow"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

// funcIndex builds a module with one function of the given name and params.
func funcIndex(name string, params []ast.Parameter) *symbols.Index {
	m := &ast.Module{
		Language: ast.LangGo, FilePath: "/project/svc/user.go", PackageName: "svc",
		Nodes: []*ast.Node{{
			ID: "func:" + name, Kind: ast.NodeFunction, Name: name,
			Language: ast.LangGo, Span: ast.Span{Start: ast.Pos{Line: 10}}, Exported: true,
		}},
	}
	// Attach params onto the derived symbol via the node (BuildTable reads them).
	m.Nodes[0].Params = params
	idx := symbols.NewIndex()
	idx.AddModule(m)
	idx.ResolveAll()
	return idx
}

// symID returns the canonical symbol ID for the single function in funcIndex.
func symID(name string) string {
	return symbols.SymbolID("/project/svc/user.go", symbols.SymFunc, name)
}

func beliefOfKind(t *testing.T, bs *Store, k Kind) Belief {
	t.Helper()
	for _, b := range bs.All() {
		if b.Kind == k {
			return b
		}
	}
	t.Fatalf("no belief of kind %q found; have: %v", k, kinds(bs))
	return Belief{}
}

func kinds(bs *Store) []Kind {
	var ks []Kind
	for _, b := range bs.All() {
		ks = append(ks, b.Kind)
	}
	return ks
}

// ─── Session 1: relocation basics ─────────────────────────────

func TestCorrectUnknownBelief(t *testing.T) {
	bs := NewStore(t.TempDir())
	if err := bs.Correct(Correction{BeliefID: "nonexistent", CorrectedTo: "x"}); err == nil {
		t.Error("correcting a nonexistent belief should return an error")
	}
}

func TestDiffSameChecksumIsEmpty(t *testing.T) {
	bs := NewStore(t.TempDir())
	bs.checksum = "test123"
	if changes := bs.DiffSince("test123"); len(changes) != 0 {
		t.Errorf("diff with identical checksum should be empty, got %d", len(changes))
	}
}

func TestDerivedBeliefsAreParserSourced(t *testing.T) {
	bs := NewStore(t.TempDir())
	bs.RebuildFromIndex(funcIndex("CreateUser", nil), map[string]*flow.FuncEffects{})
	all := bs.All()
	if len(all) == 0 {
		t.Fatal("expected at least one derived belief")
	}
	for _, b := range all {
		if b.Source != SourceParser {
			t.Errorf("belief %q Source = %q, want %q", b.ID, b.Source, SourceParser)
		}
	}
}

func TestRootScopedPersistence(t *testing.T) {
	root := t.TempDir()
	idx := funcIndex("CreateUser", nil)
	bs := NewStore(root)
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	b := beliefOfKind(t, bs, KindAction)

	if err := bs.Correct(Correction{BeliefID: b.ID, CorrectedTo: "register a new user"}); err != nil {
		t.Fatalf("Correct failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".gen", "beliefs.json")); err != nil {
		t.Fatalf("beliefs.json not written under <root>/.gen: %v", err)
	}

	bs2 := NewStore(root)
	bs2.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	for _, x := range bs2.All() {
		if x.ID == b.ID {
			if !x.HumanVerified || x.CorrectedTo != "register a new user" || x.Source != SourceHuman {
				t.Errorf("reloaded correction wrong: %+v", x)
			}
			return
		}
	}
	t.Fatal("corrected belief not found after reload")
}

func TestCorrectionSurvivesReparse(t *testing.T) {
	root := t.TempDir()
	idx := funcIndex("CreateUser", nil)
	bs := NewStore(root)
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	b := beliefOfKind(t, bs, KindAction)
	if err := bs.Correct(Correction{BeliefID: b.ID, CorrectedTo: "create account"}); err != nil {
		t.Fatalf("Correct: %v", err)
	}
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	for _, x := range bs.All() {
		if x.ID == b.ID {
			if x.CorrectedTo != "create account" || !x.HumanVerified {
				t.Error("correction did not survive re-parse")
			}
			return
		}
	}
	t.Fatal("belief missing after re-parse")
}

// ─── Session 2: Box1/Box2 staleness ───────────────────────────
// We use the EFFECT belief as the value that changes: rebuild with db effects,
// then with fs effects, so the parser-derived Object moves under a correction.

func effectsFor(name string, k flow.EffectKind) map[string]*flow.FuncEffects {
	id := symID(name)
	return map[string]*flow.FuncEffects{
		id: {SymbolID: id, Effects: map[flow.EffectKind]bool{k: true}},
	}
}

func TestOverlaySilentOnMatch(t *testing.T) {
	root := t.TempDir()
	idx := funcIndex("SaveOrder", nil)
	bs := NewStore(root)
	bs.RebuildFromIndex(idx, effectsFor("SaveOrder", flow.EffectDB))
	b := beliefOfKind(t, bs, KindEffect)
	if err := bs.Correct(Correction{BeliefID: b.ID, CorrectedTo: "db"}); err != nil {
		t.Fatalf("Correct: %v", err)
	}
	bs.RebuildFromIndex(idx, effectsFor("SaveOrder", flow.EffectDB)) // unchanged
	if w := bs.Warnings(); len(w) != 0 {
		t.Errorf("expected 0 warnings on unchanged source, got %d: %+v", len(w), w)
	}
}

func TestWarnAndShowSourceOnMismatch(t *testing.T) {
	root := t.TempDir()
	idx := funcIndex("SaveOrder", nil)
	bs := NewStore(root)
	bs.RebuildFromIndex(idx, effectsFor("SaveOrder", flow.EffectDB))
	b := beliefOfKind(t, bs, KindEffect)
	if b.Object != "db" {
		t.Fatalf("precondition: derived effect = %q, want db", b.Object)
	}
	if err := bs.Correct(Correction{BeliefID: b.ID, CorrectedTo: "db (accounts table)"}); err != nil {
		t.Fatalf("Correct: %v", err)
	}
	// Source changes: same routine now inferred to touch fs, not db.
	bs.RebuildFromIndex(idx, effectsFor("SaveOrder", flow.EffectFS))

	warns := bs.Warnings()
	if len(warns) != 1 {
		t.Fatalf("expected 1 staleness warning, got %d: %+v", len(warns), warns)
	}
	if warns[0].HumanValue != "db (accounts table)" {
		t.Errorf("warning HumanValue = %q", warns[0].HumanValue)
	}
	if warns[0].SourceNow != "fs" {
		t.Errorf("warning SourceNow = %q, want fs (the NEW derived value)", warns[0].SourceNow)
	}
	for _, x := range bs.All() {
		if x.ID == b.ID {
			if !x.Stale {
				t.Error("belief should be flagged stale")
			}
			if x.CorrectedTo != "db (accounts table)" {
				t.Errorf("human value must still win: %q", x.CorrectedTo)
			}
			if x.SourceNow != "fs" {
				t.Errorf("belief.SourceNow = %q, want fs", x.SourceNow)
			}
		}
	}
}

func TestRecorrectClearsStaleness(t *testing.T) {
	root := t.TempDir()
	idx := funcIndex("SaveOrder", nil)
	bs := NewStore(root)
	bs.RebuildFromIndex(idx, effectsFor("SaveOrder", flow.EffectDB))
	b := beliefOfKind(t, bs, KindEffect)
	_ = bs.Correct(Correction{BeliefID: b.ID, CorrectedTo: "db"})
	bs.RebuildFromIndex(idx, effectsFor("SaveOrder", flow.EffectFS)) // stale
	if len(bs.Warnings()) != 1 {
		t.Fatal("precondition: expected stale warning")
	}
	if err := bs.Correct(Correction{BeliefID: b.ID, CorrectedTo: "fs"}); err != nil {
		t.Fatalf("re-Correct: %v", err)
	}
	if len(bs.Warnings()) != 0 {
		t.Errorf("re-correction should clear staleness, got %d", len(bs.Warnings()))
	}
	bs.RebuildFromIndex(idx, effectsFor("SaveOrder", flow.EffectFS))
	if len(bs.Warnings()) != 0 {
		t.Errorf("re-baselined fingerprint should stay clean, got %d", len(bs.Warnings()))
	}
}

func TestLegacyCorrectionRebaselinesNoWarn(t *testing.T) {
	root := t.TempDir()
	idx := funcIndex("SaveOrder", nil)
	bs := NewStore(root)
	bs.RebuildFromIndex(idx, effectsFor("SaveOrder", flow.EffectDB))
	b := beliefOfKind(t, bs, KindEffect)
	bs.corrections[b.ID] = Correction{BeliefID: b.ID, CorrectedTo: "db"} // no DerivedAt
	bs.RebuildFromIndex(idx, effectsFor("SaveOrder", flow.EffectDB))
	if len(bs.Warnings()) != 0 {
		t.Errorf("legacy correction should re-baseline without warning, got %d", len(bs.Warnings()))
	}
	bs.RebuildFromIndex(idx, effectsFor("SaveOrder", flow.EffectFS))
	if len(bs.Warnings()) != 1 {
		t.Errorf("after re-baseline, a real change should warn; got %d", len(bs.Warnings()))
	}
}

// ─── Session 3: behavioral parser-eval kinds ──────────────────

func TestActionBeliefFromName(t *testing.T) {
	bs := NewStore(t.TempDir())
	bs.RebuildFromIndex(funcIndex("CreateUser", nil), map[string]*flow.FuncEffects{})
	b := beliefOfKind(t, bs, KindAction)
	if b.Object != "create user" {
		t.Errorf("action Object = %q, want %q", b.Object, "create user")
	}
	if b.Source != SourceParser {
		t.Errorf("action Source = %q, want parser", b.Source)
	}
}

func TestInputsBeliefFromParams(t *testing.T) {
	params := []ast.Parameter{
		{Name: "id", Type: ast.TypeRef{Name: "int"}},
		{Name: "amount", Type: ast.TypeRef{Name: "float64"}},
	}
	bs := NewStore(t.TempDir())
	bs.RebuildFromIndex(funcIndex("AdjustStock", params), map[string]*flow.FuncEffects{})
	b := beliefOfKind(t, bs, KindInputs)
	if b.Object != "id int, amount float64" {
		t.Errorf("inputs Object = %q, want %q", b.Object, "id int, amount float64")
	}
}

func TestEffectAndDataChangeBeliefs(t *testing.T) {
	idx := funcIndex("SaveOrder", nil)
	bs := NewStore(t.TempDir())
	bs.RebuildFromIndex(idx, effectsFor("SaveOrder", flow.EffectDB))
	eff := beliefOfKind(t, bs, KindEffect)
	if eff.Object != "db" {
		t.Errorf("effect Object = %q, want db", eff.Object)
	}
	dc := beliefOfKind(t, bs, KindDataChange)
	if dc.Object != "database" {
		t.Errorf("data-change Object = %q, want database", dc.Object)
	}
}

func TestNoLegacyKindsEmitted(t *testing.T) {
	bs := NewStore(t.TempDir())
	bs.RebuildFromIndex(funcIndex("CreateUser", nil), map[string]*flow.FuncEffects{})
	for _, b := range bs.All() {
		switch b.Kind {
		case KindLayer, KindContract, KindDependency, KindOwnership:
			t.Errorf("legacy kind %q should no longer be emitted", b.Kind)
		}
	}
}

func TestKindParserDerivableClassification(t *testing.T) {
	parser := []Kind{KindInputs, KindCalls, KindDataChange, KindEffect, KindAction}
	for _, k := range parser {
		if !k.IsParserDerivable() {
			t.Errorf("%q should be parser-derivable", k)
		}
	}
	enrich := []Kind{KindComputation, KindPrecondition}
	for _, k := range enrich {
		if k.IsParserDerivable() {
			t.Errorf("%q should NOT be parser-derivable (enrich tier)", k)
		}
	}
}

// ─── Session 4: enrich-eval integration (fake enricher, no LLM) ────

type fakeEnricher struct {
	proposals []EnrichBelief
	err       error
	gotFact   *EnrichFact
}

func (f *fakeEnricher) Enrich(fact EnrichFact) ([]EnrichBelief, error) {
	f.gotFact = &fact
	return f.proposals, f.err
}

func TestEnrichBeliefsAppearWithSource(t *testing.T) {
	fe := &fakeEnricher{proposals: []EnrichBelief{
		{Kind: KindAction, Object: "creates a user account"},
		{Kind: KindPrecondition, Object: "rejects a duplicate email"},
	}}
	bs := NewStore(t.TempDir())
	bs.SetEnricher(fe)
	bs.RebuildFromIndex(funcIndex("CreateUser", nil), map[string]*flow.FuncEffects{})

	var gotAction, gotPre bool
	for _, b := range bs.All() {
		if b.Source == SourceEnrich {
			if b.Kind == KindAction && b.Object == "creates a user account" {
				gotAction = true
			}
			if b.Kind == KindPrecondition && b.Object == "rejects a duplicate email" {
				gotPre = true
			}
		}
	}
	if !gotAction || !gotPre {
		t.Errorf("enrich beliefs missing: action=%v pre=%v", gotAction, gotPre)
	}
}

func TestEnrichReceivesParserFacts(t *testing.T) {
	fe := &fakeEnricher{proposals: nil}
	params := []ast.Parameter{{Name: "id", Type: ast.TypeRef{Name: "int"}}}
	bs := NewStore(t.TempDir())
	bs.SetEnricher(fe)
	bs.RebuildFromIndex(funcIndex("AdjustStock", params), effectsFor("AdjustStock", flow.EffectDB))
	if fe.gotFact == nil {
		t.Fatal("enricher was not called")
	}
	if fe.gotFact.Params != "id int" {
		t.Errorf("fact.Params = %q, want %q", fe.gotFact.Params, "id int")
	}
	if fe.gotFact.Effects != "db" {
		t.Errorf("fact.Effects = %q, want db", fe.gotFact.Effects)
	}
}

func TestEnricherErrorIsGraceful(t *testing.T) {
	fe := &fakeEnricher{err: errFake}
	bs := NewStore(t.TempDir())
	bs.SetEnricher(fe)
	// Parser beliefs must still be present despite the enricher failing.
	bs.RebuildFromIndex(funcIndex("CreateUser", nil), map[string]*flow.FuncEffects{})
	if _, err := findKind(bs, KindAction); err != nil {
		t.Error("parser action belief should survive an enricher error")
	}
	for _, b := range bs.All() {
		if b.Source == SourceEnrich {
			t.Error("no enrich beliefs expected when the enricher errors")
		}
	}
}

func TestNilEnricherIsParserOnly(t *testing.T) {
	bs := NewStore(t.TempDir()) // no enricher
	bs.RebuildFromIndex(funcIndex("CreateUser", nil), map[string]*flow.FuncEffects{})
	for _, b := range bs.All() {
		if b.Source == SourceEnrich {
			t.Error("no enrich beliefs expected without an enricher")
		}
	}
}

func TestEnrichBeliefIsCorrectable(t *testing.T) {
	fe := &fakeEnricher{proposals: []EnrichBelief{{Kind: KindAction, Object: "does a thing"}}}
	bs := NewStore(t.TempDir())
	bs.SetEnricher(fe)
	bs.RebuildFromIndex(funcIndex("CreateUser", nil), map[string]*flow.FuncEffects{})
	var enrichID string
	for _, b := range bs.All() {
		if b.Source == SourceEnrich {
			enrichID = b.ID
		}
	}
	if enrichID == "" {
		t.Fatal("no enrich belief to correct")
	}
	if err := bs.Correct(Correction{BeliefID: enrichID, CorrectedTo: "creates a new user"}); err != nil {
		t.Fatalf("correcting an enrich belief failed: %v", err)
	}
	for _, b := range bs.All() {
		if b.ID == enrichID {
			if b.Source != SourceHuman || b.CorrectedTo != "creates a new user" {
				t.Errorf("enrich belief not properly corrected: %+v", b)
			}
		}
	}
}

func findKind(bs *Store, k Kind) (Belief, error) {
	for _, b := range bs.All() {
		if b.Kind == k {
			return b, nil
		}
	}
	return Belief{}, errFake
}

var errFake = fmtError("fake")

type fmtError string

func (e fmtError) Error() string { return string(e) }

// ─── Session 5: correction context for generation ─────────────

func TestCorrectionContextEmptyWithoutCorrections(t *testing.T) {
	bs := NewStore(t.TempDir())
	bs.RebuildFromIndex(funcIndex("CreateUser", nil), map[string]*flow.FuncEffects{})
	if cc := bs.CorrectionContext(); cc != "" {
		t.Errorf("expected empty correction context, got %q", cc)
	}
}

func TestCorrectionContextRendersHumanCorrections(t *testing.T) {
	root := t.TempDir()
	idx := funcIndex("AdjustStock", []ast.Parameter{{Name: "amount", Type: ast.TypeRef{Name: "int"}}})
	bs := NewStore(root)
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	act := beliefOfKind(t, bs, KindAction)
	if err := bs.Correct(Correction{BeliefID: act.ID, CorrectedTo: "reduce product stock, never below zero"}); err != nil {
		t.Fatalf("Correct: %v", err)
	}
	cc := bs.CorrectionContext()
	if !strings.Contains(cc, "reduce product stock, never below zero") {
		t.Errorf("correction context missing the human value:\n%s", cc)
	}
	if !strings.Contains(cc, "Human-verified") {
		t.Errorf("correction context missing header:\n%s", cc)
	}
	if !strings.Contains(cc, "AdjustStock does:") {
		t.Errorf("expected action rendered as 'X does:':\n%s", cc)
	}
}

func TestCorrectionContextOnlyHumanSourced(t *testing.T) {
	// A parser belief that is NOT human-corrected must not appear.
	bs := NewStore(t.TempDir())
	bs.RebuildFromIndex(funcIndex("CreateUser", nil), map[string]*flow.FuncEffects{})
	cc := bs.CorrectionContext()
	if strings.Contains(cc, "create user") {
		t.Errorf("uncorrected parser belief leaked into correction context:\n%s", cc)
	}
}

func TestCorrectionContextFlagsStale(t *testing.T) {
	root := t.TempDir()
	idx := funcIndex("SaveOrder", nil)
	bs := NewStore(root)
	bs.RebuildFromIndex(idx, effectsFor("SaveOrder", flow.EffectDB))
	eff := beliefOfKind(t, bs, KindEffect)
	_ = bs.Correct(Correction{BeliefID: eff.ID, CorrectedTo: "writes to accounts table"})
	// Source moves.
	bs.RebuildFromIndex(idx, effectsFor("SaveOrder", flow.EffectFS))
	cc := bs.CorrectionContext()
	if !strings.Contains(cc, "writes to accounts table") {
		t.Errorf("stale correction should still be included:\n%s", cc)
	}
	if !strings.Contains(cc, "current source now suggests") {
		t.Errorf("stale correction should carry a source-changed note:\n%s", cc)
	}
	if !strings.Contains(cc, "fs") {
		t.Errorf("stale note should include the new source value:\n%s", cc)
	}
}

func TestBuildCorrectionContextRoundTrip(t *testing.T) {
	root := t.TempDir()
	idx := funcIndex("CreateUser", nil)
	// First store: make a correction and persist.
	bs := NewStore(root)
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	act := beliefOfKind(t, bs, KindAction)
	_ = bs.Correct(Correction{BeliefID: act.ID, CorrectedTo: "register a customer"})
	// Second, independent call (as a generation caller would) reloads from disk.
	cc := BuildCorrectionContext(root, idx, map[string]*flow.FuncEffects{})
	if !strings.Contains(cc, "register a customer") {
		t.Errorf("BuildCorrectionContext did not reload the persisted correction:\n%s", cc)
	}
}

// ─── Session 7: parser-validation gate for enrich beliefs ─────

// enrichWith returns a fake enricher emitting fixed proposals.
func enrichWith(props ...EnrichBelief) *fakeEnricher {
	return &fakeEnricher{proposals: props}
}

// findBeliefByKindSrc returns the first belief matching kind+source.
func findBeliefByKindSrc(bs *Store, k Kind, src Source) *Belief {
	for _, b := range bs.All() {
		if b.Kind == k && b.Source == src {
			c := b
			return &c
		}
	}
	return nil
}

func TestValidate_SemanticIsNeedsHuman(t *testing.T) {
	bs := NewStore(t.TempDir())
	bs.SetEnricher(enrichWith(
		EnrichBelief{Kind: KindComputation, Object: "total = sum(items)"},
		EnrichBelief{Kind: KindPrecondition, Object: "rejects if amount is negative"},
	))
	bs.RebuildFromIndex(funcIndex("Checkout", nil), map[string]*flow.FuncEffects{})
	for _, k := range []Kind{KindComputation, KindPrecondition} {
		b := findBeliefByKindSrc(bs, k, SourceEnrich)
		if b == nil {
			t.Fatalf("missing enrich belief of kind %q", k)
		}
		if b.Validated != "needs-human" {
			t.Errorf("%q Validated = %q, want needs-human", k, b.Validated)
		}
	}
}

func TestValidate_ContradictsWhenClaimsWriteButNoEffect(t *testing.T) {
	// Parser sees NO effects for this routine; enrich claims it persists.
	bs := NewStore(t.TempDir())
	bs.SetEnricher(enrichWith(EnrichBelief{Kind: KindAction, Object: "saves the order to the database"}))
	bs.RebuildFromIndex(funcIndex("Checkout", nil), map[string]*flow.FuncEffects{})
	b := findBeliefByKindSrc(bs, KindAction, SourceEnrich)
	if b == nil {
		t.Fatal("missing enrich action belief")
	}
	if b.Validated != "contradicts" {
		t.Errorf("Validated = %q, want contradicts (no parser effect but claims a write)", b.Validated)
	}
	if !b.IsBlockedForSteering() {
		t.Error("a contradicting enrich belief must be blocked for steering")
	}
}

func TestValidate_ConsistentWhenWriteMatchesEffect(t *testing.T) {
	// Parser DOES see a db effect; enrich claiming a write is consistent.
	bs := NewStore(t.TempDir())
	bs.SetEnricher(enrichWith(EnrichBelief{Kind: KindAction, Object: "writes the order to the database"}))
	bs.RebuildFromIndex(funcIndex("Checkout", nil), effectsFor("Checkout", flow.EffectDB))
	b := findBeliefByKindSrc(bs, KindAction, SourceEnrich)
	if b == nil {
		t.Fatal("missing enrich action belief")
	}
	if b.Validated != "consistent" {
		t.Errorf("Validated = %q, want consistent (parser saw db + claim of write agree)", b.Validated)
	}
	if b.IsBlockedForSteering() {
		t.Error("a consistent belief should not be blocked")
	}
}

func TestValidate_ContradictsWhenClaimsPureButHasEffect(t *testing.T) {
	// Parser sees a db effect; enrich claims the routine is read-only.
	bs := NewStore(t.TempDir())
	bs.SetEnricher(enrichWith(EnrichBelief{Kind: KindAction, Object: "a read-only lookup with no side effect"}))
	bs.RebuildFromIndex(funcIndex("Checkout", nil), effectsFor("Checkout", flow.EffectDB))
	b := findBeliefByKindSrc(bs, KindAction, SourceEnrich)
	if b == nil {
		t.Fatal("missing enrich action belief")
	}
	if b.Validated != "contradicts" {
		t.Errorf("Validated = %q, want contradicts (claims pure but parser saw db)", b.Validated)
	}
}

func TestValidate_PlainActionIsConsistent(t *testing.T) {
	// A neutral action claim with no persistence/purity assertion just passes.
	bs := NewStore(t.TempDir())
	bs.SetEnricher(enrichWith(EnrichBelief{Kind: KindAction, Object: "creates a new user account"}))
	bs.RebuildFromIndex(funcIndex("CreateUser", nil), map[string]*flow.FuncEffects{})
	b := findBeliefByKindSrc(bs, KindAction, SourceEnrich)
	if b == nil || b.Validated != "consistent" {
		t.Errorf("plain action should be consistent, got %+v", b)
	}
}

func TestValidate_ParserAndHumanBeliefsUnvalidated(t *testing.T) {
	// Validation is only meaningful for enrich beliefs; parser beliefs carry none.
	bs := NewStore(t.TempDir())
	bs.RebuildFromIndex(funcIndex("CreateUser", nil), map[string]*flow.FuncEffects{})
	for _, b := range bs.All() {
		if b.Source == SourceParser && b.Validated != "" {
			t.Errorf("parser belief %q should have empty Validated, got %q", b.ID, b.Validated)
		}
	}
}

// ─── Session 9: enrich as persistent, source-fingerprinted tier ────

// symIDFor returns the real symbol ID for the single function in funcIndex, by
// reading it off a derived parser belief (avoids hardcoding the ID format).
func symIDFor(bs *Store, kind Kind) string {
	for _, b := range bs.All() {
		if b.Kind == kind && b.Source == SourceParser {
			return strings.TrimPrefix(b.ID, string(kind)+":")
		}
	}
	return ""
}

func TestUpsertEnrichPersistsAcrossReparse(t *testing.T) {
	root := t.TempDir()
	idx := funcIndex("Checkout", nil)

	bs := NewStore(root)
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	symID := symIDFor(bs, KindAction)
	bs.UpsertEnrichBeliefs([]EnrichInput{
		{SymbolID: symID, Subject: "Checkout", Kind: KindAction, Object: "processes an order", Body: "func Checkout(){}"},
	})

	// A parser rebuild must NOT wipe the enrich belief.
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	if findBeliefByKindSrc(bs, KindAction, SourceEnrich) == nil {
		t.Fatal("enrich belief did not survive a parser rebuild")
	}

	// And it must reload from disk in a fresh store.
	bs2 := NewStore(root)
	bs2.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	b := findBeliefByKindSrc(bs2, KindAction, SourceEnrich)
	if b == nil || b.Object != "processes an order" {
		t.Fatalf("enrich belief not reloaded from disk: %+v", b)
	}
}

func TestEnrichSourceStalenessOnBodyChange(t *testing.T) {
	root := t.TempDir()
	idx := funcIndex("Checkout", nil)

	bs := NewStore(root)
	// Body provider returns a body that we will later change.
	body := "func Checkout(){ old() }"
	bs.SetBodyProvider(func(id string) string { return body })
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	symID := symIDFor(bs, KindAction)
	bs.UpsertEnrichBeliefs([]EnrichInput{
		{SymbolID: symID, Subject: "Checkout", Kind: KindAction, Object: "does the old thing", Body: body},
	})

	// Rebuild with the SAME body → not stale.
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	b := findBeliefByKindSrc(bs, KindAction, SourceEnrich)
	if b == nil || b.Stale {
		t.Fatalf("enrich belief should not be stale on unchanged body: %+v", b)
	}

	// Change the body → rebuild → the enrich belief goes stale.
	body = "func Checkout(){ somethingCompletelyDifferent() }"
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	b = findBeliefByKindSrc(bs, KindAction, SourceEnrich)
	if b == nil || !b.Stale {
		t.Fatalf("enrich belief should be stale after body change: %+v", b)
	}
}

func TestClearEnrichForSymbol(t *testing.T) {
	root := t.TempDir()
	idx := funcIndex("Checkout", nil)
	bs := NewStore(root)
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	symID := symIDFor(bs, KindAction)
	bs.UpsertEnrichBeliefs([]EnrichInput{
		{SymbolID: symID, Subject: "Checkout", Kind: KindAction, Object: "x", Body: "b"},
		{SymbolID: symID, Subject: "Checkout", Kind: KindComputation, Object: "y", Body: "b"},
	})
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	if findBeliefByKindSrc(bs, KindAction, SourceEnrich) == nil {
		t.Fatal("precondition: enrich beliefs should exist")
	}
	bs.ClearEnrichForSymbol(symID)
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	if findBeliefByKindSrc(bs, KindAction, SourceEnrich) != nil {
		t.Error("enrich beliefs should be gone after ClearEnrichForSymbol")
	}
}

func TestPersistedFileOmitsParserBeliefs(t *testing.T) {
	// Parser beliefs are transient — the on-disk file must not carry them, only
	// the durable classes (corrections + enrich).
	root := t.TempDir()
	idx := funcIndex("CreateUser", nil)
	bs := NewStore(root)
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	symID := symIDFor(bs, KindAction)
	bs.UpsertEnrichBeliefs([]EnrichInput{
		{SymbolID: symID, Subject: "CreateUser", Kind: KindAction, Object: "makes a user", Body: "b"},
	})
	data, err := os.ReadFile(filepath.Join(root, ".gen", "beliefs.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "enrich_beliefs") {
		t.Error("persisted file should contain enrich_beliefs")
	}
	// The transient parser action belief's id "action:func:CreateUser" should not
	// be persisted as a top-level derived belief.
	if strings.Contains(s, "\"action:"+symID+"\"") {
		t.Error("transient parser belief leaked into the persisted file")
	}
}

func TestEnrichRevalidatedOnReparse(t *testing.T) {
	// An enrich belief claiming a write becomes 'contradicts' when the parser (on a
	// later rebuild) shows no effect, even though it was stored earlier.
	root := t.TempDir()
	idx := funcIndex("Checkout", nil)
	bs := NewStore(root)
	bs.RebuildFromIndex(idx, effectsFor("Checkout", flow.EffectDB))
	symID := symIDFor(bs, KindAction)
	bs.UpsertEnrichBeliefs([]EnrichInput{
		{SymbolID: symID, Subject: "Checkout", Kind: KindAction, Object: "writes to the database", Body: "b"},
	})
	// First rebuild WITH db effect → consistent.
	bs.RebuildFromIndex(idx, effectsFor("Checkout", flow.EffectDB))
	if b := findBeliefByKindSrc(bs, KindAction, SourceEnrich); b == nil || b.Validated != "consistent" {
		t.Fatalf("expected consistent while db effect present: %+v", b)
	}
	// Rebuild WITHOUT the effect → the same stored belief now contradicts.
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	if b := findBeliefByKindSrc(bs, KindAction, SourceEnrich); b == nil || b.Validated != "contradicts" {
		t.Fatalf("expected contradicts after effect disappeared: %+v", b)
	}
}

// ─── Session 10: hierarchical summaries + cascade ─────────────

// twoMethodIndex builds a module with two methods on the same service, so service
// grouping and roll-up have real members. NOTE: ast.Node has NO QualName field —
// a method's qualified name is DERIVED by the symbol table from Receiver.Name +
// "." + Name, so the fixture must set Receiver the way a real frontend does.
func twoMethodIndex() *symbols.Index {
	mk := func(recv, name string) *ast.Node {
		return &ast.Node{
			ID: "method:" + recv + "." + name, Kind: ast.NodeMethod, Name: name,
			Receiver: &ast.TypeRef{Name: recv}, Language: ast.LangGo,
			Span: ast.Span{Start: ast.Pos{Line: 1}}, Exported: true,
		}
	}
	m := &ast.Module{
		Language: ast.LangGo, FilePath: "/project/svc/order.go", PackageName: "svc",
		Nodes: []*ast.Node{mk("OrderService", "Checkout"), mk("OrderService", "Refund")},
	}
	idx := symbols.NewIndex()
	idx.AddModule(m)
	idx.ResolveAll()
	return idx
}

func TestServiceOf(t *testing.T) {
	if ServiceOf("OrderService.Checkout") != "OrderService" {
		t.Error("ServiceOf should return the receiver")
	}
	if ServiceOf("PlainFunc") != "" {
		t.Error("ServiceOf should be empty when there is no receiver")
	}
}

func TestServiceMembersGroupsByReceiver(t *testing.T) {
	bs := NewStore(t.TempDir())
	bs.RebuildFromIndex(twoMethodIndex(), map[string]*flow.FuncEffects{})
	sm := bs.ServiceMembers()
	if len(sm["OrderService"]) == 0 {
		t.Fatalf("expected OrderService members, got %v", sm)
	}
	// Both methods' action beliefs should be grouped under OrderService.
	subjects := map[string]bool{}
	for _, id := range sm["OrderService"] {
		for _, b := range bs.All() {
			if b.ID == id {
				subjects[b.Subject] = true
			}
		}
	}
	if !subjects["OrderService.Checkout"] || !subjects["OrderService.Refund"] {
		t.Errorf("both methods should be members: %v", subjects)
	}
}

func TestUpsertSummaryPersistsAndShows(t *testing.T) {
	root := t.TempDir()
	idx := twoMethodIndex()
	bs := NewStore(root)
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	members := bs.ServiceMembers()["OrderService"]
	bs.UpsertSummary(SummaryInput{
		Scope: ScopeService, Name: "OrderService",
		Object: "manages the order lifecycle: checkout and refund", MemberIDs: members,
	})
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	sums := bs.Summaries()
	if len(sums) != 1 || sums[0].Subject != "OrderService" || sums[0].Scope != ScopeService {
		t.Fatalf("summary not stored/returned correctly: %+v", sums)
	}
	// Reloads from disk.
	bs2 := NewStore(root)
	bs2.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	if len(bs2.Summaries()) != 1 {
		t.Error("summary did not reload from disk")
	}
}

func TestSummaryCascadesStaleOnMemberCorrection(t *testing.T) {
	root := t.TempDir()
	idx := twoMethodIndex()
	bs := NewStore(root)
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	members := bs.ServiceMembers()["OrderService"]
	bs.UpsertSummary(SummaryInput{
		Scope: ScopeService, Name: "OrderService",
		Object: "manages orders", MemberIDs: members,
	})
	// Not stale immediately after a rebuild with unchanged members.
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	if bs.Summaries()[0].Stale {
		t.Fatal("summary should not be stale before any member change")
	}
	// Correct a member belief → its value changes → summary must cascade stale.
	var memberID string
	for _, b := range bs.All() {
		if b.Kind == KindAction && b.Subject == "OrderService.Checkout" {
			memberID = b.ID
		}
	}
	if memberID == "" {
		t.Fatal("could not find a member action belief to correct")
	}
	if err := bs.Correct(Correction{BeliefID: memberID, CorrectedTo: "finalizes and pays an order"}); err != nil {
		t.Fatalf("Correct: %v", err)
	}
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	if !bs.Summaries()[0].Stale {
		t.Error("summary should be stale after a member belief changed (cascade)")
	}
}

// ─── v26.19: persistence-detection regression (found by the first real test run) ───

// TestAssertsPersistence_PhrasingAndFalsePositives pins the two lessons from the
// first on-box test run: the detector must tolerate words between verb and target,
// and must NOT fire on incidental nouns or explicitly in-memory phrasing.
func TestAssertsPersistence_PhrasingAndFalsePositives(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		// Must DETECT — persistence claims with words between verb and target.
		{"saves the order to the database", true},
		{"writes the order to the database", true},
		{"inserts the row into the users table", true},
		{"persists the account balance", true},
		{"writes to disk", true},
		// Must NOT detect — real claims from a live run over an in-memory app.
		{"creates a new bookmarkhandler with an in-memory store.", false},
		{"newbookmarkhandlerwith creates a new bookmarkhandler instance initialized with the provided bookmarkcrud store.", false},
		{"registers http handlers for bookmark-related routes on the provided servemux.", false},
		{"returns a new expensestore with an empty data map and next id set to 1.", false},
		{"creates a new project entry with a generated id and stores it in memory.", false},
		{"payloadint converts the value associated with a given key in a map to an integer.", false},
		{"creates a new user account", false},
	}
	for _, c := range cases {
		if got := assertsPersistence(c.text); got != c.want {
			t.Errorf("assertsPersistence(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

// TestContainsWord_WholeWordOnly guards the helper the detector relies on.
func TestContainsWord_WholeWordOnly(t *testing.T) {
	if containsWord("restore the file", "store") {
		t.Error("containsWord matched inside 'restore'")
	}
	if !containsWord("the store is open", "store") {
		t.Error("containsWord missed a standalone word")
	}
	if !containsWord("writes to db", "db") {
		t.Error("containsWord missed a trailing word")
	}
	if containsWord("dbg output", "db") {
		t.Error("containsWord matched inside 'dbg'")
	}
}

// TestStoreIsolation_NoCrossTestLeak pins the v26.19 fix: a store rooted at a fresh
// directory must NOT inherit beliefs from any other store. (Before this, tests
// sharing NewStore("") all read/wrote one relative .gen/beliefs.json, so persistent
// enrich beliefs from one test leaked into the next.)
func TestStoreIsolation_NoCrossTestLeak(t *testing.T) {
	idx := funcIndex("Checkout", nil)

	a := NewStore(t.TempDir())
	a.SetEnricher(enrichWith(EnrichBelief{Kind: KindAction, Object: "does a thing"}))
	a.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	if findBeliefByKindSrc(a, KindAction, SourceEnrich) == nil {
		t.Fatal("precondition: store A should have an enrich belief")
	}

	b := NewStore(t.TempDir()) // different root
	b.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	if got := findBeliefByKindSrc(b, KindAction, SourceEnrich); got != nil {
		t.Errorf("store B inherited an enrich belief from A: %+v", got)
	}
}

// ─── v26.21: flat-language (COBOL) grouping + summary tally ───

// cobolLikeIndex builds a module whose routines have NO receiver and NO dotted
// qualified name — the shape a COBOL program produces (paragraphs like
// APPLY-CREDIT inside PROGRAM-ID CUSTMGMT).
func cobolLikeIndex(program string, paras ...string) *symbols.Index {
	var nodes []*ast.Node
	// The module node carries the PROGRAM-ID; the symbol table turns it into a
	// SymModule symbol whose name is what grouping falls back to.
	nodes = append(nodes, &ast.Node{
		ID: "module:" + program, Kind: ast.NodeModule, Name: program,
		Language: ast.LangCOBOL, Span: ast.Span{Start: ast.Pos{Line: 1}},
	})
	for _, p := range paras {
		nodes = append(nodes, &ast.Node{
			ID: "proc:" + p, Kind: ast.NodeProcedure, Name: p,
			Language: ast.LangCOBOL, Span: ast.Span{Start: ast.Pos{Line: 2}},
		})
	}
	m := &ast.Module{
		Language: ast.LangCOBOL, FilePath: "/project/cbl/custmgmt.cbl",
		PackageName: program, Nodes: nodes,
	}
	idx := symbols.NewIndex()
	idx.AddModule(m)
	idx.ResolveAll()
	return idx
}

// TestCobolParagraphsGroupByProgram pins the v26.21 fix: COBOL paragraph names have
// no receiver, so before this they grouped into nothing and produced ZERO summaries.
// They must now group under the PROGRAM-ID.
func TestCobolParagraphsGroupByProgram(t *testing.T) {
	bs := NewStore(t.TempDir())
	bs.RebuildFromIndex(cobolLikeIndex("CUSTMGMT", "APPLY-CREDIT", "APPLY-FEES"),
		map[string]*flow.FuncEffects{})

	members := bs.ServiceMembers()
	if len(members["CUSTMGMT"]) == 0 {
		t.Fatalf("COBOL paragraphs should group under the PROGRAM-ID, got groups: %v", keysOf(members))
	}
	// Both paragraphs must be in the group.
	subjects := map[string]bool{}
	for _, id := range members["CUSTMGMT"] {
		for _, b := range bs.All() {
			if b.ID == id {
				subjects[b.Subject] = true
			}
		}
	}
	if !subjects["APPLY-CREDIT"] || !subjects["APPLY-FEES"] {
		t.Errorf("both paragraphs should be members, got %v", subjects)
	}
}

// TestCobolSummaryRollsUp proves the end state: a COBOL program can now produce a
// summary, which was impossible before (0 summaries on real COBOL runs).
func TestCobolSummaryRollsUp(t *testing.T) {
	root := t.TempDir()
	idx := cobolLikeIndex("CUSTMGMT", "APPLY-CREDIT", "APPLY-FEES")
	bs := NewStore(root)
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})

	members := bs.ServiceMembers()["CUSTMGMT"]
	if len(members) == 0 {
		t.Fatal("precondition: expected CUSTMGMT members")
	}
	bs.UpsertSummary(SummaryInput{
		Scope: ScopeService, Name: "CUSTMGMT",
		Object: "maintains customer records: applies credits and fees", MemberIDs: members,
	})
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	sums := bs.Summaries()
	if len(sums) != 1 || sums[0].Subject != "CUSTMGMT" {
		t.Fatalf("expected one CUSTMGMT summary, got %+v", sums)
	}
	if sums[0].Stale {
		t.Error("a freshly-written summary should not be stale")
	}
}

// TestReceiverGroupingStillWins guards against the COBOL fallback hijacking OO
// languages: a method with a receiver must still group by receiver, not by module.
func TestReceiverGroupingStillWins(t *testing.T) {
	bs := NewStore(t.TempDir())
	bs.RebuildFromIndex(twoMethodIndex(), map[string]*flow.FuncEffects{})
	m := bs.ServiceMembers()
	if len(m["OrderService"]) == 0 {
		t.Errorf("methods must group by receiver, got groups: %v", keysOf(m))
	}
	if len(m["svc"]) != 0 {
		t.Errorf("methods must NOT fall back to the package/module group, got: %v", keysOf(m))
	}
}

func keysOf(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ─── v26.22: module/project cascade on member ADD/REMOVE ───

// TestModuleCascadesOnServiceAdded pins the v26.22 fix: a module summary must go
// stale when a NEW service summary appears beneath it — not only when an existing
// member's content changes. Before this, module/project scopes replayed their
// stored member list, so an added or removed child was invisible.
func TestModuleCascadesOnServiceAdded(t *testing.T) {
	root := t.TempDir()
	idx := twoMethodIndex()
	bs := NewStore(root)
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})

	// One service summary, then a module summarising it.
	svcMembers := bs.ServiceMembers()["OrderService"]
	bs.UpsertSummary(SummaryInput{
		Scope: ScopeService, Name: "OrderService",
		Object: "manages orders", MemberIDs: svcMembers, Parent: "svc",
	})
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})

	var svcSummaryID string
	for _, s := range bs.Summaries() {
		if s.Scope == ScopeService {
			svcSummaryID = s.ID
		}
	}
	bs.UpsertSummary(SummaryInput{
		Scope: ScopeModule, Name: "svc",
		Object: "the ordering module", MemberIDs: []string{svcSummaryID},
	})
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	if moduleSummary(t, bs).Stale {
		t.Fatal("module summary should be fresh before any child is added")
	}

	// ADD a second service summary under the same module.
	bs.UpsertSummary(SummaryInput{
		Scope: ScopeService, Name: "PaymentService",
		Object: "takes payments", MemberIDs: nil, Parent: "svc",
	})
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	if !moduleSummary(t, bs).Stale {
		t.Error("module summary should cascade STALE when a new service appears beneath it")
	}
}

// TestModuleCascadesOnServiceRemoved is the mirror case: dropping a child must also
// move the module's fingerprint.
func TestModuleCascadesOnServiceRemoved(t *testing.T) {
	root := t.TempDir()
	idx := twoMethodIndex()
	bs := NewStore(root)
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})

	bs.UpsertSummary(SummaryInput{Scope: ScopeService, Name: "A", Object: "a", Parent: "svc"})
	bs.UpsertSummary(SummaryInput{Scope: ScopeService, Name: "B", Object: "b", Parent: "svc"})
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})

	var ids []string
	for _, s := range bs.Summaries() {
		if s.Scope == ScopeService {
			ids = append(ids, s.ID)
		}
	}
	bs.UpsertSummary(SummaryInput{
		Scope: ScopeModule, Name: "svc", Object: "module", MemberIDs: ids,
	})
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	if moduleSummary(t, bs).Stale {
		t.Fatal("module should be fresh immediately after summarising its two services")
	}

	bs.DeleteSummary(ScopeService, "B")
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	if !moduleSummary(t, bs).Stale {
		t.Error("module summary should cascade STALE when a child service is removed")
	}
}

func moduleSummary(t *testing.T, bs *Store) Belief {
	t.Helper()
	for _, s := range bs.Summaries() {
		if s.Scope == ScopeModule {
			return s
		}
	}
	t.Fatal("no module summary found")
	return Belief{}
}

// ─── v26.23: Warnings must be corrections ONLY ───

// TestWarningsExcludeStaleSummaries pins the v26.23 fix. The "N corrections are out
// of date" banner is about human corrections and shows "you said X / source now Y".
// A stale SUMMARY has neither value, so including it rendered empty rows under a
// misleading heading (seen on a real COBOL project: 3 stale summaries listed as
// corrections with both fields blank).
func TestWarningsExcludeStaleSummaries(t *testing.T) {
	root := t.TempDir()
	idx := twoMethodIndex()
	bs := NewStore(root)
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})

	members := bs.ServiceMembers()["OrderService"]
	bs.UpsertSummary(SummaryInput{
		Scope: ScopeService, Name: "OrderService", Object: "manages orders", MemberIDs: members,
	})
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})

	// Make the summary stale by correcting a member (changes the child fingerprint).
	var memberID string
	for _, b := range bs.All() {
		if b.Kind == KindAction && b.Subject == "OrderService.Checkout" {
			memberID = b.ID
		}
	}
	if memberID == "" {
		t.Fatal("no member belief to correct")
	}
	if err := bs.Correct(Correction{BeliefID: memberID, CorrectedTo: "finalizes an order"}); err != nil {
		t.Fatalf("Correct: %v", err)
	}
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})

	if bs.StaleSummaryCount() == 0 {
		t.Fatal("precondition: the summary should be stale after a member changed")
	}
	for _, w := range bs.Warnings() {
		if strings.HasPrefix(w.BeliefID, "summary:") {
			t.Errorf("Warnings must not include summaries, got %q", w.BeliefID)
		}
		if w.HumanValue == "" {
			t.Errorf("every warning must carry the human value it is comparing, got %+v", w)
		}
	}
}

// TestWarningsIncludeRealStaleCorrection is the positive case: a genuine stale human
// correction must still be reported (the v26.23 filter must not silence it).
func TestWarningsIncludeRealStaleCorrection(t *testing.T) {
	root := t.TempDir()
	bs := NewStore(root)
	// Start with one parameter, so an "inputs" belief exists to correct.
	bs.RebuildFromIndex(
		funcIndex("Checkout", []ast.Parameter{{Name: "id", Type: ast.TypeRef{Name: "int"}}}),
		map[string]*flow.FuncEffects{})

	var id string
	for _, b := range bs.All() {
		if b.Kind == KindInputs {
			id = b.ID
		}
	}
	if id == "" {
		t.Fatal("expected an inputs belief to correct")
	}
	if err := bs.Correct(Correction{BeliefID: id, CorrectedTo: "human value"}); err != nil {
		t.Fatalf("Correct: %v", err)
	}
	// Rebuild with DIFFERENT parameters so the derived inputs value changes and the
	// correction's fingerprint no longer matches.
	bs.RebuildFromIndex(
		funcIndex("Checkout", []ast.Parameter{
			{Name: "id", Type: ast.TypeRef{Name: "int"}},
			{Name: "amount", Type: ast.TypeRef{Name: "float64"}},
		}),
		map[string]*flow.FuncEffects{})

	found := false
	for _, w := range bs.Warnings() {
		if w.BeliefID == id {
			found = true
			if w.HumanValue != "human value" {
				t.Errorf("warning should carry the human value, got %q", w.HumanValue)
			}
		}
	}
	if !found {
		t.Error("a genuinely stale human correction must still be warned about")
	}
}
