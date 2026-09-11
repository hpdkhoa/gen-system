// generator/internal/belief/belief.go — v26.7 (session 1: belief store relocated
// out of internal/presentation into its own low-level package so BOTH the
// Understand track and the Generate track may import it — presentation is
// forbidden from importing internal/context, and the generator is forbidden from
// importing presentation, so the shared belief layer could not live in either.
//
// This package imports ONLY symbols + flow (generation-free, no internal/context,
// no presentation, no ai/engine/agent/planner). That keeps the understand-server
// boundary intact: the belief package is a leaf both sides can depend on.
//
// Session-1 scope: relocate + two honesty fixes carried in without behavior change
// to any caller:
//  1. Store is now project-ROOT scoped. The old store hardcoded diskPath
//     ".gen/beliefs.json" relative to the process CWD, so corrections made while
//     viewing project A were reloaded when viewing project B from the same shell.
//     NewStore(root) writes <root>/.gen/beliefs.json.
//  2. The hardcoded per-kind Confidence float (0.95/0.85/0.75) was a priority
//     LABEL masquerading as a probability. It is retained for wire-compat but a
//     new Source field ("parser"/"enrich"/"human") now records where a belief
//     actually came from — the honest replacement the later sessions build on.
//
// The behavioral-kind vocabulary, the derived/asserted (Box1/Box2) split, the
// staleness warning, the LLM enrich tier, and correction-steered generation are
// LATER sessions. This file deliberately preserves the current kinds and the
// current rebuildFromIndex logic so session 1 is a pure move.
package belief

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/flow"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

// Kind classifies a belief in HUMAN-REVIEWABLE, behavioral terms — what a person
// reading unfamiliar code would jot in a notebook — rather than compiler-internal
// analysis tags. A banking analyst can read "this routine's inputs are X; it
// writes to the account record; it calls Y" and say yes/no; they cannot
// meaningfully verify "this symbol is in layer service" or "effect: pure".
//
// The kind is BLENDED across the three evals (session design): parser-eval sets an
// initial kind from what the parser can see for certain; enrich-eval (session 4,
// local LLM) and human-eval (Box 2) may refine or override it. Each kind below is
// annotated with which eval can HONESTLY produce it.
type Kind string

const (
	// Parser-eval kinds — emitted deterministically, offline, from index data.
	KindInputs     Kind = "inputs"      // "its inputs are: id int, amount float64"   (from Params)
	KindCalls      Kind = "calls"       // "it calls: OrderService.Place"              (from call edges)
	KindDataChange Kind = "data-change" // "it writes to: orders (db)"                 (from RefWrite + effects)
	KindEffect     Kind = "effect"      // "it touches: db"                            (from effect inference)
	KindAction     Kind = "action"      // "it appears to: create user"  (SHALLOW parser guess from name+effect;
	//                                     the rich version is enrich-eval's job)

	// Enrich-eval kinds — the parser cannot honestly produce these; a local LLM
	// reading the body does (session 4). Declared now so the vocabulary is stable
	// and human corrections/steering can reference them.
	KindComputation  Kind = "computation"  // "computes new-balance = old − amount"
	KindPrecondition Kind = "precondition" // "refuses if amount would make balance negative"

	// Summary — a rolled-up description of a SCOPE (service / module / project),
	// produced by summarizing the BELIEFS of the members below it rather than raw
	// source (session 10). Subject is the scope name; Object is the prose. Persisted
	// + child-fingerprinted so a change below cascades staleness upward.
	KindSummary Kind = "summary"

	// Deprecated legacy kinds (pre-v26.9). Retained so old persisted corrections
	// and any external reference still resolve; no longer emitted by the rebuild.
	KindLayer      Kind = "layer"
	KindOwnership  Kind = "ownership"
	KindContract   Kind = "contract"
	KindDependency Kind = "dependency"
)

// IsParserDerivable reports whether the parser can emit this kind deterministically.
// enrich-eval / human-eval kinds return false — they need reasoning or a person.
func (k Kind) IsParserDerivable() bool {
	switch k {
	case KindInputs, KindCalls, KindDataChange, KindEffect, KindAction:
		return true
	default:
		return false
	}
}

// Source records WHERE a belief came from. This is the honest replacement for the
// old hardcoded Confidence float: instead of pretending a call edge is "0.95
// probable", we state that it was produced deterministically by the parser.
// Later sessions add SourceEnrich (local LLM) and lean on SourceHuman for the
// durable/asserted (Box 2) tier.
type Source string

const (
	SourceParser Source = "parser" // deterministic, from the offline parser/index
	SourceEnrich Source = "enrich" // from the local LLM enrichment module (session 4)
	SourceHuman  Source = "human"  // asserted or corrected by a person (Box 2)
)

// Belief is one claim the system holds about the target code.
type Belief struct {
	ID        string `json:"id"`
	Kind      Kind   `json:"kind"`
	Subject   string `json:"subject"`
	Predicate string `json:"predicate"`
	Object    string `json:"object"`

	// Source is the origin tier (parser/enrich/human). Preferred over Confidence.
	Source Source `json:"source"`

	// Confidence is retained for wire/UI compatibility. It is a per-kind priority
	// hint, NOT a measured probability; do not compute with it. New code should
	// branch on Source instead.
	Confidence float64 `json:"confidence"`

	// HumanVerified is true once a human confirmed or corrected this belief.
	HumanVerified bool   `json:"human_verified"`
	CorrectedTo   string `json:"corrected_to,omitempty"`

	// Stale is set when a human correction exists for this belief but the
	// parser-derived value has since changed (source moved under the assertion).
	// The human's value still wins (Object/CorrectedTo), but Stale + SourceNow
	// let the UI show BOTH: "you said X; the code now says Y".
	Stale     bool   `json:"stale,omitempty"`
	SourceNow string `json:"source_now,omitempty"` // fresh parser-derived Object when Stale

	// Validation records how an enrich (model-written) belief was checked against
	// the deterministic parser facts (session 7). It is only meaningful for
	// Source==enrich beliefs:
	//   "consistent"  — the claim agrees with (or doesn't contradict) parser facts.
	//   "contradicts" — the claim asserts something the parser positively denies
	//                    (e.g. a db write the parser never observed). ValidationNote
	//                    explains. A contradicting belief is shown flagged and never
	//                    steers generation.
	//   "needs-human" — a semantic claim (computation / precondition) that CANNOT be
	//                    mechanically validated; only a person can confirm it.
	// Empty for parser/human beliefs (nothing to validate).
	Validated      string `json:"validated,omitempty"`
	ValidationNote string `json:"validation_note,omitempty"`

	// SourceHash is the fingerprint of the routine's SOURCE at the time an enrich
	// belief was written (session 9). Enrich claims are about the body, so they go
	// stale when the body changes: on rebuild, if the routine's current source hash
	// differs from this, the enrich belief is flagged Stale (a signal to re-enrich).
	// Empty for parser/human beliefs. Distinct from a human correction's DerivedAt
	// fingerprint, which tracks the derived value rather than the source text.
	SourceHash string `json:"source_hash,omitempty"`

	// ChildFingerprint is set on summary beliefs (session 10): a hash of the member
	// beliefs summarized. On rebuild the current fingerprint is recomputed and a
	// mismatch flags the summary Stale, cascading a change below up the hierarchy.
	// Scope names the level: "service" / "module" / "project".
	ChildFingerprint string   `json:"child_fingerprint,omitempty"`
	Scope            string   `json:"scope,omitempty"`
	MemberIDs        []string `json:"member_ids,omitempty"` // summary members (for cascade recompute)

	// Parent names the enclosing scope for a summary belief (v26.22): a service
	// summary's parent is its module, a module summary's parent is the project.
	// Storing it makes module/project membership RE-DERIVABLE at rebuild time, so
	// those scopes cascade on a member being ADDED or REMOVED — not just on a
	// member's content changing. Empty for routine beliefs and for project scope.
	Parent string `json:"parent,omitempty"`

	// Group is the scope this belief's routine belongs to, captured at derive time
	// (v26.21). For OO languages it's the receiver ("OrderService"); for COBOL and
	// other flat languages it's the PROGRAM-ID / package, because paragraph names
	// like APPLY-CREDIT have no receiver and would otherwise group into nothing.
	// Empty means the routine has no natural scope. Summary beliefs leave it empty.
	Group string `json:"group,omitempty"`
}

// Correction is a human override of a specific belief. Corrections are stored in a
// SEPARATE map from beliefs (the asserted / Box-2 tier) and re-applied on top after
// every rebuild-from-source, so a human's judgment survives re-parsing.
//
// DerivedAt is the FINGERPRINT: the parser-derived Object value at the moment the
// human made this correction. On a later re-parse the store compares the NEW
// derived value against DerivedAt — if they differ, the source has moved under the
// human's assertion, so the overlaid belief is flagged Stale and carries the new
// value (SourceNow) for side-by-side review. This is the Box-2-overlays-Box-1 rule
// with staleness detection: never silently keep (a lie) nor silently drop (data loss).
type Correction struct {
	BeliefID    string `json:"belief_id"`
	CorrectedTo string `json:"corrected_to"`
	HumanNote   string `json:"human_note,omitempty"`
	// DerivedAt is the source-derived Object value when this correction was made.
	// Empty means "baseline unknown" (e.g. a correction persisted before v26.8);
	// such a correction is re-baselined on the next rebuild rather than warned,
	// so upgrading never produces spurious warnings.
	DerivedAt string `json:"derived_at,omitempty"`
}

// DiffEntry is one changed understanding, reported by DiffSince.
type DiffEntry struct {
	Kind    string `json:"kind"` // "added" | "removed" | "changed"
	Subject string `json:"subject"`
	Before  string `json:"before,omitempty"`
	After   string `json:"after,omitempty"`
}

// Store persists and manages beliefs + corrections for one project.
type Store struct {
	mu          sync.Mutex
	beliefs     map[string]*Belief
	corrections map[string]Correction
	// enrichBeliefs is the THIRD persistence class (session 9): model-written
	// beliefs that PERSIST across parser rebuilds (unlike parser beliefs, which are
	// wiped) and are re-attached on each rebuild, then re-validated + stale-checked
	// against the current source. Keyed by belief ID. Written either inline by an
	// injected Enricher or out-of-band by the gen-enrich producer.
	enrichBeliefs map[string]*Belief
	// summaries are rolled-up scope descriptions (service/module/project), persisted
	// and cascaded (session 10). Keyed by summary belief ID. Each stores a
	// ChildFingerprint of the member beliefs it summarized; on rebuild the current
	// fingerprint is recomputed and a mismatch flags the summary stale.
	summaries map[string]*Belief
	diskPath  string
	checksum  string

	// enricher (enrich-eval tier) and bodyProvider are optional and injected only
	// by generation-side callers; understand-server leaves them nil. Guarded by mu.
	enricher     Enricher
	bodyProvider BodyProvider
}

// NewStore builds a Store whose persistence file is <root>/.gen/beliefs.json.
// An empty root falls back to ".gen/beliefs.json" (legacy CWD behavior) so an
// unconfigured caller still works, but every real caller should pass the loaded
// project root. Existing corrections on disk are loaded immediately.
func NewStore(root string) *Store {
	dir := ".gen"
	if strings.TrimSpace(root) != "" {
		dir = filepath.Join(root, ".gen")
	}
	bs := &Store{
		beliefs:       make(map[string]*Belief),
		corrections:   make(map[string]Correction),
		enrichBeliefs: make(map[string]*Belief),
		summaries:     make(map[string]*Belief),
		diskPath:      filepath.Join(dir, "beliefs.json"),
	}
	bs.load()
	return bs
}

// RebuildFromIndex regenerates all DERIVED beliefs from the current index +
// effects, then overlays any stored human corrections on top. Beliefs are wiped
// and rebuilt (source is truth); corrections are preserved and re-applied.
func (bs *Store) RebuildFromIndex(idx *symbols.Index, efx map[string]*flow.FuncEffects) {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	// Wipe derived beliefs; corrections and enrich beliefs are kept in their own
	// maps (parser=transient, human corrections + enrich=persistent).
	bs.beliefs = make(map[string]*Belief)

	// Collect parser facts per symbol so persisted enrich beliefs can be
	// re-validated against the CURRENT deterministic ground truth on every rebuild.
	factsBySym := make(map[string]parserFacts)
	// file -> module/PROGRAM-ID, for grouping flat languages (v26.21).
	moduleByFile := buildModuleByFile(idx)
	groupBySym := make(map[string]string)

	// Only describe project functions/methods/procedures — the things a human
	// reviews. Skip non-callable symbols (structs, vars) and external/stdlib code.
	for _, sym := range idx.AllSymbols() {
		if sym.Provenance.IsExternal() {
			continue
		}
		if !isCallable(sym.Kind) {
			continue
		}
		parserBeliefs := deriveParserBeliefs(sym, idx, efx, moduleByFile)
		for _, b := range parserBeliefs {
			bs.applyCorrectionLocked(b)
			bs.beliefs[b.ID] = b
		}
		factsBySym[sym.ID] = factsFromParserBeliefs(parserBeliefs)
		if len(parserBeliefs) > 0 {
			groupBySym[sym.ID] = parserBeliefs[0].Group
		}

		// Inline enrich-eval (when an Enricher is injected in-process): best-effort.
		// Its output is now PERSISTED into bs.enrichBeliefs so it survives future
		// parser rebuilds, exactly like a human correction.
		for _, b := range bs.enrichLocked(sym, parserBeliefs) {
			bs.enrichBeliefs[b.ID] = b
		}
	}

	// Re-attach persisted enrich beliefs (from a prior inline run OR from the
	// out-of-band gen-enrich producer). Each is re-validated against the current
	// parser facts and stale-checked against the current routine source, then
	// overlaid — a human correction on top of an enrich belief still wins.
	bs.reattachEnrichLocked(factsBySym, groupBySym)

	// Re-attach scope summaries LAST (they depend on member beliefs already being
	// present) and cascade staleness from any changed member (session 10).
	bs.reattachSummariesLocked()

	bs.computeChecksumLocked()
	bs.saveLocked()
}

// reattachEnrichLocked folds persisted enrich beliefs into the live belief set,
// re-validating and stale-checking each against current source. Caller holds mu.
func (bs *Store) reattachEnrichLocked(factsBySym map[string]parserFacts, groupBySym map[string]string) {
	for id, eb := range bs.enrichBeliefs {
		symID := enrichSymID(id)
		// If the routine no longer exists in the index, keep the belief but mark it
		// stale (its subject is gone) rather than dropping a human-reviewable record.
		facts, symStillPresent := factsBySym[symID]

		// Re-validate against current facts (source may have changed the effects).
		validateEnrich(eb, facts)

		// Inherit the grouping scope from the routine's parser beliefs (v26.21) so
		// producer-written enrich beliefs roll up even though the producer, which
		// runs out-of-band, has no scope information of its own.
		if eb.Group == "" {
			eb.Group = groupBySym[symID]
		}

		// Source staleness: compare the stored fingerprint to the current body.
		if bs.bodyProvider != nil && eb.SourceHash != "" {
			cur := hashSource(bs.bodyProvider(symID))
			if cur != "" && cur != eb.SourceHash {
				eb.Stale = true
				if eb.ValidationNote == "" {
					eb.ValidationNote = "the routine's source changed since this was written — re-enrich"
				}
			}
		}
		if !symStillPresent {
			eb.Stale = true
		}
		bs.applyCorrectionLocked(eb) // a human correction of this enrich belief wins
		bs.beliefs[id] = eb
	}
}

// enrichLocked calls the injected Enricher for one symbol and maps its proposals
// to beliefs with stable IDs and SourceEnrich. Caller holds bs.mu. Any failure
// yields nil — enrichment is strictly additive and never blocks a rebuild.
func (bs *Store) enrichLocked(sym *symbols.Symbol, parserBeliefs []*Belief) []*Belief {
	if bs.enricher == nil {
		return nil
	}
	fact := EnrichFact{
		SymbolID:   sym.ID,
		Subject:    sym.QualName,
		Name:       sym.Name,
		Language:   string(sym.Language),
		DocComment: sym.DocComment,
	}
	if fact.Subject == "" {
		fact.Subject = sym.Name
	}
	// Reuse the already-derived parser beliefs as pre-formatted context.
	for _, b := range parserBeliefs {
		switch b.Kind {
		case KindInputs:
			fact.Params = b.Object
		case KindCalls:
			fact.Calls = b.Object
		case KindEffect:
			fact.Effects = b.Object
		}
	}
	if bs.bodyProvider != nil {
		fact.Body = bs.bodyProvider(sym.ID)
	}

	proposals, err := bs.enricher.Enrich(fact)
	if err != nil {
		return nil
	}
	// Distill the deterministic ground truth once, to validate each proposal.
	facts := factsFromParserBeliefs(parserBeliefs)
	srcHash := hashSource(fact.Body)
	grp := ""
	if len(parserBeliefs) > 0 {
		grp = parserBeliefs[0].Group // same routine => same scope
	}
	var out []*Belief
	for _, p := range proposals {
		if p.Object == "" {
			continue
		}
		b := &Belief{
			ID:         "enrich:" + string(p.Kind) + ":" + sym.ID,
			Kind:       p.Kind,
			Subject:    fact.Subject,
			Predicate:  "described_as",
			Object:     p.Object,
			Source:     SourceEnrich,
			SourceHash: srcHash, // fingerprint the body for staleness (session 9)
			Group:      grp,     // join the routine's scope for roll-up (v26.21)
		}
		validateEnrich(b, facts) // session 7: gate against parser facts
		out = append(out, b)
	}
	return out
}

// EnrichFact is the read-only view of one routine handed to an Enricher: the
// parser-known facts, so the LLM enriches WITH the deterministic context rather
// than re-deriving it. Body is the raw source of the routine when available.
type EnrichFact struct {
	SymbolID   string
	Subject    string // qualified name
	Name       string
	Language   string
	Params     string // pre-formatted "id int, amount float64"
	Calls      string // pre-formatted "OrderService.Place, ..."
	Effects    string // pre-formatted "db, fs"
	Body       string // routine source, if the caller can supply it
	DocComment string
}

// EnrichBelief is one belief an Enricher proposes. Kind should be an enrich-tier
// kind (action / computation / precondition); Object is the plain-language claim.
type EnrichBelief struct {
	Kind   Kind
	Object string
}

// Enricher turns parser facts into richer, plain-language beliefs — the enrich-eval
// tier. The concrete implementation (local LLM) lives in a SEPARATE package that
// imports internal/ai and is wired ONLY by the generation-side binaries; the belief
// store depends on this interface, never on the LLM, so understand-server stays
// air-gapped and free of generation packages. A nil Enricher (or one returning an
// error / empty slice) simply yields no enrich beliefs — parser + human tiers are
// unaffected. Enrich MUST be side-effect free and safe to skip.
type Enricher interface {
	Enrich(fact EnrichFact) ([]EnrichBelief, error)
}

// SetEnricher injects (or clears, with nil) the enrich-eval provider. Off by
// default: an unconfigured store performs parser-eval only.
func (bs *Store) SetEnricher(e Enricher) {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	bs.enricher = e
}

// BodyProvider optionally supplies a routine's source text for enrichment. The
// index/effects alone don't carry bodies; a caller that has them (e.g. via the
// AST cache) can provide one. Nil is fine — enrichment then works from facts only.
type BodyProvider func(symbolID string) string

// SetBodyProvider injects a source-text lookup used to populate EnrichFact.Body.
func (bs *Store) SetBodyProvider(bp BodyProvider) {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	bs.bodyProvider = bp
}

// isCallable reports whether a symbol kind names a routine we describe behaviorally.
func isCallable(k symbols.SymbolKind) bool {
	switch k {
	case symbols.SymFunc, symbols.SymMethod, symbols.SymProcedure:
		return true
	default:
		return false
	}
}

// deriveParserBeliefs produces the behavioral, human-reviewable beliefs the PARSER
// can state for certain about one routine — the parser-eval tier. It never guesses
// at meaning it cannot see (computation / precondition are left to enrich-eval).
//
// Object is written as the human-readable value that the UI shows and that a human
// verifies/corrects; each belief's ID is stable across re-parses so corrections and
// staleness fingerprints (session 2) keep matching.
func deriveParserBeliefs(sym *symbols.Symbol, idx *symbols.Index, efx map[string]*flow.FuncEffects, moduleByFile map[string]string) []*Belief {
	var out []*Belief
	subj := sym.QualName
	if subj == "" {
		subj = sym.Name
	}

	// inputs — from the declared parameters.
	if len(sym.Params) > 0 {
		out = append(out, &Belief{
			ID:        "inputs:" + sym.ID,
			Kind:      KindInputs,
			Subject:   subj,
			Predicate: "takes_inputs",
			Object:    formatParams(sym.Params),
			Source:    SourceParser,
		})
	}

	// calls — the routines this one invokes (resolved callees only).
	if callees := resolvedCallees(sym.ID, idx); len(callees) > 0 {
		out = append(out, &Belief{
			ID:        "calls:" + sym.ID,
			Kind:      KindCalls,
			Subject:   subj,
			Predicate: "calls",
			Object:    strings.Join(callees, ", "),
			Source:    SourceParser,
		})
	}

	// effect + data-change — from inferred effects. db/fs mean the routine changes
	// stored state; that is the "change-catalog" observation in the user's terms.
	if e, ok := efx[sym.ID]; ok {
		effs := effectList(e)
		if len(effs) > 0 {
			out = append(out, &Belief{
				ID:        "effect:" + sym.ID,
				Kind:      KindEffect,
				Subject:   subj,
				Predicate: "touches",
				Object:    strings.Join(effs, ", "),
				Source:    SourceParser,
			})
		}
		if changes := stateChanges(effs); changes != "" {
			out = append(out, &Belief{
				ID:        "data-change:" + sym.ID,
				Kind:      KindDataChange,
				Subject:   subj,
				Predicate: "writes_to",
				Object:    changes,
				Source:    SourceParser,
			})
		}
	}

	// action — a SHALLOW parser guess (verb from the name + what it touches). This
	// is deliberately crude; session 4's enrich-eval replaces it with a real
	// description read from the body. Marked SourceParser so a human sees it as a
	// machine guess to verify.
	if act := shallowAction(sym); act != "" {
		out = append(out, &Belief{
			ID:        "action:" + sym.ID,
			Kind:      KindAction,
			Subject:   subj,
			Predicate: "appears_to",
			Object:    act,
			Source:    SourceParser,
		})
	}

	// Stamp the grouping scope on every derived belief (v26.21) so summaries can roll
	// up flat languages. Receiver-based for OO; PROGRAM-ID/package for COBOL et al.
	grp := groupForSymbol(sym, moduleByFile)
	for _, b := range out {
		b.Group = grp
	}

	return out
}

// groupForSymbol determines the scope a routine belongs to for summary roll-up.
// Receiver first (Go methods, Java classes: "OrderService.Checkout" -> OrderService),
// falling back to the enclosing MODULE symbol — which for COBOL is the PROGRAM-ID —
// so flat languages whose routine names carry no receiver (COBOL paragraphs like
// APPLY-CREDIT) group into their program instead of into nothing.
//
// NOTE: symbols.Symbol has NO PackageName field; the program name lives on the
// SymModule symbol for the same file, so it is resolved via the index by DefinedIn.
func groupForSymbol(sym *symbols.Symbol, moduleByFile map[string]string) string {
	if sym.ReceiverType != "" {
		return sym.ReceiverType
	}
	qual := sym.QualName
	if qual == "" {
		qual = sym.Name
	}
	if i := strings.LastIndex(qual, "."); i > 0 {
		return qual[:i]
	}
	return moduleByFile[sym.DefinedIn]
}

// buildModuleByFile maps each file to the module/program symbol declared in it
// (COBOL: the PROGRAM-ID). Computed once per rebuild so grouping stays O(n).
func buildModuleByFile(idx *symbols.Index) map[string]string {
	out := map[string]string{}
	if idx == nil {
		return out
	}
	for _, s := range idx.AllSymbols() {
		if s.Kind != symbols.SymModule || s.DefinedIn == "" {
			continue
		}
		name := s.QualName
		if name == "" {
			name = s.Name
		}
		if name != "" {
			out[s.DefinedIn] = name
		}
	}
	return out
}

// formatParams renders parameters as "name type, name type".
func formatParams(ps []ast.Parameter) string {
	parts := make([]string, 0, len(ps))
	for _, p := range ps {
		name := p.Name
		typ := p.Type.Name
		switch {
		case name != "" && typ != "":
			parts = append(parts, name+" "+typ)
		case typ != "":
			parts = append(parts, typ)
		case name != "":
			parts = append(parts, name)
		}
	}
	return strings.Join(parts, ", ")
}

// resolvedCallees returns the sorted, de-duplicated qualified names this symbol
// calls, for edges the resolver linked to a real callee.
func resolvedCallees(callerID string, idx *symbols.Index) []string {
	seen := make(map[string]bool)
	var names []string
	for _, edge := range idx.AllCallEdges() {
		if edge.CallerID != callerID || edge.CalleeID == "" {
			continue
		}
		name := edge.CalleeName
		if callee, ok := idx.ByID(edge.CalleeID); ok && callee.QualName != "" {
			name = callee.QualName
		}
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// effectList returns the concrete (non-pure, non-unknown) effects, sorted.
func effectList(e *flow.FuncEffects) []string {
	var effs []string
	for k, on := range e.Effects {
		if !on {
			continue
		}
		if k == flow.EffectPure || k == flow.EffectUnknown {
			continue
		}
		effs = append(effs, string(k))
	}
	sort.Strings(effs)
	return effs
}

// stateChanges maps state-touching effects to a human phrase. db/fs are the ones
// that persist a change ("change-catalog"); network is a side effect but not a
// local state write, so it is reported under effect only.
func stateChanges(effs []string) string {
	var parts []string
	for _, e := range effs {
		switch e {
		case string(flow.EffectDB):
			parts = append(parts, "database")
		case string(flow.EffectFS):
			parts = append(parts, "filesystem")
		}
	}
	return strings.Join(parts, ", ")
}

// shallowAction composes a crude "verb noun" guess from the routine name and its
// dominant effect. Intentionally low-fidelity — a placeholder the enrich tier
// upgrades. Returns "" when it has nothing meaningful to say.
func shallowAction(sym *symbols.Symbol) string {
	name := sym.Name
	if name == "" {
		return ""
	}
	verb, rest := splitLeadingVerb(name)
	if verb == "" {
		return ""
	}
	phrase := verb
	if rest != "" {
		phrase += " " + rest
	}
	return phrase
}

// splitLeadingVerb splits a CamelCase / snake identifier into a lowercased leading
// word and a spaced remainder: "CreateUser" → ("create","user"); "adjust_stock" →
// ("adjust","stock").
func splitLeadingVerb(id string) (string, string) {
	// Normalize snake_case and kebab to camel boundaries first.
	id = strings.ReplaceAll(id, "_", " ")
	id = strings.ReplaceAll(id, "-", " ")
	var words []string
	if strings.Contains(id, " ") {
		words = strings.Fields(id)
	} else {
		words = splitCamel(id)
	}
	if len(words) == 0 {
		return "", ""
	}
	first := strings.ToLower(words[0])
	rest := strings.ToLower(strings.Join(words[1:], " "))
	return first, rest
}

// splitCamel splits CamelCase into words: "CreateUserAccount" → [Create User Account].
func splitCamel(s string) []string {
	var words []string
	start := 0
	for i := 1; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' && s[i-1] >= 'a' && s[i-1] <= 'z' {
			words = append(words, s[start:i])
			start = i
		}
	}
	words = append(words, s[start:])
	return words
}

// applyCorrectionLocked overlays a stored human correction (Box 2) onto a
// freshly-derived belief (Box 1), running the staleness comparison. On entry,
// b.Object holds the FRESH parser-derived value. Caller holds bs.mu.
//
// Cases:
//   - No correction        → nothing to do (pure derived belief).
//   - DerivedAt empty       → baseline unknown (correction predates fingerprinting,
//     or was just made without one): re-baseline to the
//     current derived value. No warning — we can't compare
//     against a baseline we never recorded.
//   - DerivedAt == fresh    → source unchanged: overlay silently.
//   - DerivedAt != fresh    → source moved under the assertion: overlay the human
//     value BUT flag Stale and record SourceNow so the UI
//     shows both the human's value and the current source.
func (bs *Store) applyCorrectionLocked(b *Belief) {
	c, ok := bs.corrections[b.ID]
	if !ok {
		return
	}
	freshDerived := b.Object // Box 1 value as of this rebuild

	// Overlay the human's value (Box 2 wins) regardless of staleness.
	b.CorrectedTo = c.CorrectedTo
	b.HumanVerified = true
	b.Source = SourceHuman

	switch {
	case c.DerivedAt == "":
		// Re-baseline silently: record the current derived value as the fingerprint.
		c.DerivedAt = freshDerived
		bs.corrections[b.ID] = c
	case c.DerivedAt != freshDerived:
		// Staleness: the code changed since the human corrected this.
		b.Stale = true
		b.SourceNow = freshDerived
	}
}

// All returns a stable, ID-sorted copy of the current beliefs.
func (bs *Store) All() []Belief {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	out := make([]Belief, 0, len(bs.beliefs))
	for _, b := range bs.beliefs {
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ─── Correction context for generation (session 5) ────────────

// CorrectionContext renders the human-verified beliefs for a project as a compact,
// plain-language block suitable for prepending/appending to a generation prompt.
// This is the bridge that lets human corrections STEER continue/evolve: a person
// corrects the machine's understanding in the Understand UI, and that correction
// arrives at the planner as an explicit constraint the model is told to honor.
//
// Only human-sourced beliefs are included — machine guesses (parser/enrich) are
// the model's to reconsider, but a human's correction is authoritative. A stale
// correction (source moved under it) is still included but flagged, so the model
// is told the human's intent AND that the code has since changed.
//
// Returns "" when there are no human corrections, so callers can append
// unconditionally without emitting an empty section.
func (bs *Store) CorrectionContext() string {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	// Stable ordering for reproducible prompts.
	ids := make([]string, 0, len(bs.beliefs))
	for id, b := range bs.beliefs {
		if b.HumanVerified && b.Source == SourceHuman {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return ""
	}
	sort.Strings(ids)

	var sb strings.Builder
	sb.WriteString("Human-verified facts about the existing code (these were reviewed and ")
	sb.WriteString("corrected by a person — treat them as authoritative and do not contradict them):\n")
	for _, id := range ids {
		b := bs.beliefs[id]
		sb.WriteString("- ")
		sb.WriteString(correctionSentence(b))
		if b.Stale && b.SourceNow != "" {
			sb.WriteString(" (note: the current source now suggests \"")
			sb.WriteString(b.SourceNow)
			sb.WriteString("\"; prefer the human's statement unless the instruction says otherwise)")
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// correctionSentence renders one corrected belief as a readable claim, using the
// human's corrected value.
func correctionSentence(b *Belief) string {
	val := b.CorrectedTo
	if val == "" {
		val = b.Object
	}
	switch b.Kind {
	case KindInputs:
		return b.Subject + " takes inputs: " + val
	case KindCalls:
		return b.Subject + " calls: " + val
	case KindDataChange:
		return b.Subject + " changes data in: " + val
	case KindEffect:
		return b.Subject + " has effects: " + val
	case KindAction:
		return b.Subject + " does: " + val
	case KindComputation:
		return b.Subject + " computes: " + val
	case KindPrecondition:
		return b.Subject + " requires: " + val
	default:
		return b.Subject + ": " + val
	}
}

// BuildCorrectionContext is a convenience for generation callers: it constructs a
// root-scoped store, rebuilds beliefs from the given index/effects so corrections
// re-apply and match, and returns the correction block. It performs parser-eval
// only (no enricher) — a fast, offline, side-effect-free read of the persisted
// human corrections. Returns "" when the project has no corrections.
func BuildCorrectionContext(root string, idx *symbols.Index, efx map[string]*flow.FuncEffects) string {
	bs := NewStore(root)
	bs.RebuildFromIndex(idx, efx)
	return bs.CorrectionContext()
}

// StaleWarning describes one asserted belief whose parser-derived value moved
// since the human corrected it. Both values are carried so a caller can present
// "you said HumanValue; the source now says SourceNow" side by side.
type StaleWarning struct {
	BeliefID   string `json:"belief_id"`
	Kind       Kind   `json:"kind"`
	Subject    string `json:"subject"`
	HumanValue string `json:"human_value"`
	SourceNow  string `json:"source_now"`
}

// Warnings returns one entry per currently-stale asserted belief, ID-sorted. Empty
// when nothing is stale. Call after a rebuild to surface staleness to the UI/CLI.
// Warnings lists HUMAN CORRECTIONS that have gone out of date — the code changed
// under an assertion a person made, so the UI can show "you said X; the source now
// says Y" and let them reconcile.
//
// v26.23: this deliberately EXCLUDES other stale things. A stale enrich belief or a
// stale scope summary is also "out of date", but neither has a human value or a
// source value to compare, so including them rendered empty rows under a heading
// about corrections. Staleness for those is surfaced on the row/summary itself.
func (bs *Store) Warnings() []StaleWarning {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	var out []StaleWarning
	for _, b := range bs.beliefs {
		if !b.Stale {
			continue
		}
		// Only a human correction produces the two-value comparison this warning is for.
		if b.Source != SourceHuman || b.CorrectedTo == "" {
			continue
		}
		out = append(out, StaleWarning{
			BeliefID:   b.ID,
			Kind:       b.Kind,
			Subject:    b.Subject,
			HumanValue: b.CorrectedTo,
			SourceNow:  b.SourceNow,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BeliefID < out[j].BeliefID })
	return out
}

// StaleSummaryCount reports how many scope summaries are out of date, so the UI can
// prompt a re-summarise without pretending they are human corrections (v26.23).
func (bs *Store) StaleSummaryCount() int {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	n := 0
	for _, s := range bs.summaries {
		if s.Stale {
			n++
		}
	}
	return n
}

// Correct records a human correction and applies it to the live belief. The
// parser-derived value being corrected is captured as the fingerprint (DerivedAt)
// so a future re-parse can detect if the source moves away from it. Re-correcting
// a stale belief re-baselines the fingerprint to the current source value and
// clears the stale flag (the human has just reconciled with the new source).
func (bs *Store) Correct(c Correction) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	b, ok := bs.beliefs[c.BeliefID]
	if !ok {
		return fmt.Errorf("belief %q not found", c.BeliefID)
	}
	// Fingerprint = the current source-derived value. If the belief is currently
	// flagged stale, the fresh source value lives in SourceNow; otherwise Object
	// still holds the derived value (a first correction hasn't been overlaid yet
	// in a way that loses it — Object equals the derived value at derive time).
	derived := b.Object
	if b.Stale && b.SourceNow != "" {
		derived = b.SourceNow
	}
	c.DerivedAt = derived

	bs.corrections[c.BeliefID] = c
	b.CorrectedTo = c.CorrectedTo
	b.HumanVerified = true
	b.Source = SourceHuman
	b.Stale = false
	b.SourceNow = ""
	bs.saveLocked()
	return nil
}

// DiffSince reports unverified beliefs as "changed" when the checksum moved.
func (bs *Store) DiffSince(prevChecksum string) []DiffEntry {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	if prevChecksum == bs.checksum {
		return nil
	}
	var changes []DiffEntry
	for _, b := range bs.beliefs {
		if !b.HumanVerified {
			changes = append(changes, DiffEntry{Kind: "changed", Subject: b.Subject, After: b.Object})
		}
	}
	return changes
}

// CurrentChecksum returns the current belief-set checksum.
func (bs *Store) CurrentChecksum() string {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	return bs.checksum
}

func (bs *Store) computeChecksumLocked() {
	keys := make([]string, 0, len(bs.beliefs))
	for k := range bs.beliefs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteString(bs.beliefs[k].Object)
	}
	body := sb.String()
	bs.checksum = fmt.Sprintf("%x", len(bs.beliefs)) + body[:minInt(len(body), 16)]
}

// persisted is the on-disk shape. Only the DURABLE classes are stored: human
// corrections and enrich beliefs. Parser beliefs are transient (rebuilt from
// source every time) and deliberately NOT persisted. The legacy Beliefs field is
// still read for backward-compat (old files that stored everything): any enrich
// beliefs found there are migrated into EnrichBeliefs.
type persisted struct {
	Beliefs       map[string]*Belief    `json:"beliefs,omitempty"` // legacy read-only
	Corrections   map[string]Correction `json:"corrections"`
	EnrichBeliefs map[string]*Belief    `json:"enrich_beliefs,omitempty"`
	Summaries     map[string]*Belief    `json:"summaries,omitempty"`
}

func (bs *Store) saveLocked() {
	pb := persisted{Corrections: bs.corrections, EnrichBeliefs: bs.enrichBeliefs, Summaries: bs.summaries}
	data, err := json.MarshalIndent(pb, "", "  ")
	if err != nil {
		return
	}
	if dir := filepath.Dir(bs.diskPath); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	_ = os.WriteFile(bs.diskPath, data, 0o644)
}

func (bs *Store) load() {
	data, err := os.ReadFile(bs.diskPath)
	if err != nil {
		return // missing or unreadable → start empty
	}
	var pb persisted
	if err := json.Unmarshal(data, &pb); err != nil {
		return
	}
	if pb.Corrections != nil {
		bs.corrections = pb.Corrections
	}
	if pb.EnrichBeliefs != nil {
		bs.enrichBeliefs = pb.EnrichBeliefs
	}
	if pb.Summaries != nil {
		bs.summaries = pb.Summaries
	}
	// Backward-compat: migrate any enrich beliefs from a legacy combined Beliefs map.
	for id, b := range pb.Beliefs {
		if b != nil && b.Source == SourceEnrich {
			if _, exists := bs.enrichBeliefs[id]; !exists {
				bs.enrichBeliefs[id] = b
			}
		}
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// hashSource returns a short stable fingerprint of a routine's source text. Empty
// input yields "" (no fingerprint — used to mean "unknown body").
func hashSource(src string) string {
	src = strings.TrimSpace(src)
	if src == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(src))
	return hex.EncodeToString(sum[:8]) // 16 hex chars is plenty for change-detection
}

// enrichSymID recovers the symbol ID from an enrich belief ID of the form
// "enrich:<kind>:<symID>". Returns "" if the shape is unexpected.
func enrichSymID(beliefID string) string {
	if !strings.HasPrefix(beliefID, "enrich:") {
		return ""
	}
	rest := beliefID[len("enrich:"):]
	i := strings.IndexByte(rest, ':')
	if i < 0 {
		return ""
	}
	return rest[i+1:]
}

// EnrichInput is one model observation the out-of-band producer wants to store,
// paired with the routine it describes and that routine's source (for the staleness
// fingerprint). Kind should be an enrich-tier kind.
type EnrichInput struct {
	SymbolID string
	Subject  string
	Kind     Kind
	Object   string
	Body     string // routine source at enrichment time (fingerprinted)
}

// UpsertEnrichBeliefs is the write path for the INDEPENDENT producer (gen-enrich):
// it stores model-written beliefs that then persist across parser rebuilds. Each
// input is fingerprinted against its source and validated later (on the next
// rebuild, against fresh parser facts). Persisted immediately. Safe to call with
// the store otherwise idle; it does not require an index.
func (bs *Store) UpsertEnrichBeliefs(inputs []EnrichInput) {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	for _, in := range inputs {
		if in.Object == "" || in.SymbolID == "" {
			continue
		}
		subj := in.Subject
		if subj == "" {
			subj = in.SymbolID
		}
		id := "enrich:" + string(in.Kind) + ":" + in.SymbolID
		bs.enrichBeliefs[id] = &Belief{
			ID:         id,
			Kind:       in.Kind,
			Subject:    subj,
			Predicate:  "described_as",
			Object:     in.Object,
			Source:     SourceEnrich,
			SourceHash: hashSource(in.Body),
		}
	}
	bs.saveLocked()
}

// ClearEnrichForSymbol drops all persisted enrich beliefs for one routine — used
// by the producer before re-enriching it, so stale kinds don't linger.
func (bs *Store) ClearEnrichForSymbol(symbolID string) {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	for id := range bs.enrichBeliefs {
		if enrichSymID(id) == symbolID {
			delete(bs.enrichBeliefs, id)
		}
	}
	bs.saveLocked()
}
