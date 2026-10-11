// generator/internal/ai/op_binding.go, v23.25 (task B): added OpMisplaced +
// opVerb, a conservative negative post-filter the engine applies after
// generation to drop an op confidently bound to a different entity. Pairs with
// the original v23.24 positive-hint binding below.
// generator/internal/ai/op_binding.go, v23.24 (op-misplacement fix): a
// deterministic pass over the idea text that figures out which operation
// phrases most likely belong to a given entity, so the logic prompt can give a
// positive hint ("operations likely on THIS entity: …") instead of only
// forbidding wrong ones. This addresses the failure where Product received no
// operations because the prompt was purely prohibitive and the model
// over-corrected to an empty list.
//
// It is intentionally simple and explainable: split the idea into clauses,
// detect imperative verb phrases, and bind each phrase to the entity whose name
// (or a field) appears nearest in the same clause. No model call, no new deps.
package ai

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

// candidateOp is a verb phrase found in the idea and the entity it binds to.
type candidateOp struct {
	Phrase string // e.g. "adjust stock"
	Entity string // e.g. "Product"
	Verb   string // e.g. "adjust", the actionVerbs entry that found it
}

// actionVerbs are imperative verbs that typically introduce a business
// operation (beyond CRUD). Kept small and high-precision.
var actionVerbs = []string{
	"check out", "checkout", "apply", "adjust", "cancel", "approve", "reject",
	"submit", "refund", "ship", "fulfil", "fulfill", "archive", "activate",
	"deactivate", "assign", "complete", "close", "reopen", "publish",
	"unpublish", "verify", "confirm", "pay", "charge", "credit", "debit",
	"transfer", "increment", "decrement", "increase", "decrease", "restock",
	"reserve", "release", "redeem", "calculate", "compute", "recalculate",
}

// entKeys couples an entity name with the lowercase keywords (name + fields)
// used to associate idea phrases with it.
type entKeys struct {
	name string
	keys []string
}

// bindOperationsToEntities scans the idea and returns, per entity name, the list
// of operation phrases most likely belonging to it.
func bindOperationsToEntities(idea string, all []schema.Entity) map[string][]string {
	out := map[string][]string{}
	for ent, ops := range bindCandidateOps(idea, all) {
		for _, op := range ops {
			out[ent] = append(out[ent], op.Phrase)
		}
	}
	return out
}

// bindCandidateOps does the work of bindOperationsToEntities and keeps the verb
// that found each phrase, so OpMisplaced can compare verbs, not substrings.
func bindCandidateOps(idea string, all []schema.Entity) map[string][]candidateOp {
	out := map[string][]candidateOp{}
	lower := strings.ToLower(idea)

	// Build a lookup of entity-associated keywords: the entity name (singular &
	// rough plural) plus its field names.
	var ents []entKeys
	for _, e := range all {
		nl := strings.ToLower(e.Name)
		keys := []string{nl, nl + "s"}
		for _, f := range e.Fields {
			fn := strings.ToLower(f.Name)
			if fn != "id" && len(fn) > 2 {
				keys = append(keys, fn)
			}
		}
		ents = append(ents, entKeys{name: e.Name, keys: keys})
	}

	// Split into clauses on sentence/clause boundaries.
	clauses := splitClauses(lower)
	for _, cl := range clauses {
		for _, verb := range actionVerbs {
			idx := indexWord(cl, verb)
			if idx == -1 {
				continue
			}
			phrase := extractPhrase(cl, idx, verb)
			ent := nearestEntity(cl, idx, ents)
			if ent == "" {
				continue
			}
			out[ent] = appendUniqueOp(out[ent], candidateOp{Phrase: phrase, Entity: ent, Verb: verb})
		}
	}
	return out
}

// indexWord returns the index of the first occurrence of word in s that stands
// as a whole word, or -1.
//
// v26.58 (D069): the binding used strings.Index, so a verb matched inside a
// longer word: "payment reference" bound "pay" to Order and "relationship"
// bound "ship" to Product, and the logic prompt then asked the model to
// implement operations the idea never states. A match now counts only when
// the characters before and after it are not letters.
func indexWord(s, word string) int {
	from := 0
	for {
		i := strings.Index(s[from:], word)
		if i == -1 {
			return -1
		}
		at := from + i
		end := at + len(word)
		before, _ := utf8.DecodeLastRuneInString(s[:at])
		after, _ := utf8.DecodeRuneInString(s[end:])
		if (at == 0 || !unicode.IsLetter(before)) && (end == len(s) || !unicode.IsLetter(after)) {
			return at
		}
		from = at + 1
	}
}

// splitClauses breaks text on ; . and common conjunctions so a verb is matched
// against the entity in its own clause, not a neighbour's.
func splitClauses(s string) []string {
	seps := []string{";", ".", " and ", " but ", " then ", ","}
	parts := []string{s}
	for _, sep := range seps {
		var next []string
		for _, p := range parts {
			next = append(next, strings.Split(p, sep)...)
		}
		parts = next
	}
	var out []string
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// extractPhrase returns a short "verb + object" phrase starting at the verb.
func extractPhrase(clause string, idx int, verb string) string {
	rest := clause[idx:]
	words := strings.Fields(rest)
	// verb may be two words ("check out"); take up to 4 words total.
	n := 4
	if len(words) < n {
		n = len(words)
	}
	phrase := strings.Join(words[:n], " ")
	// trim trailing stop-words/noise
	phrase = strings.TrimRight(phrase, " .,;:")
	return phrase
}

// nearestEntity returns the entity whose keyword appears closest to the verb
// position within the clause.
func nearestEntity(clause string, verbIdx int, ents []entKeys) string {
	best := ""
	bestDist := 1 << 30
	for _, e := range ents {
		for _, k := range e.keys {
			ki := indexAllNearest(clause, k, verbIdx)
			if ki >= 0 && ki < bestDist {
				bestDist = ki
				best = e.name
			}
		}
	}
	return best
}

// indexAllNearest returns the minimal absolute distance from any occurrence of
// sub to pos, or -1 if sub is absent.
func indexAllNearest(s, sub string, pos int) int {
	best := -1
	start := 0
	for {
		i := strings.Index(s[start:], sub)
		if i == -1 {
			break
		}
		at := start + i
		d := at - pos
		if d < 0 {
			d = -d
		}
		if best == -1 || d < best {
			best = d
		}
		start = at + len(sub)
	}
	return best
}

// appendUniqueOp appends op unless a phrase equal to it (ignoring case) is
// already present, as appendUniqueCI does for the plain phrase list.
func appendUniqueOp(s []candidateOp, op candidateOp) []candidateOp {
	for _, x := range s {
		if strings.EqualFold(x.Phrase, op.Phrase) {
			return s
		}
	}
	return append(s, op)
}

func appendUniqueCI(s []string, v string) []string {
	for _, x := range s {
		if strings.EqualFold(x, v) {
			return s
		}
	}
	return append(s, v)
}

// OpMisplaced reports whether an operation named opName, generated on entity
// `target`, is confidently bound to a different entity by the idea text. That
// is, the verb that opName derives from appears in the idea next to another
// entity's keyword and not next to target's. It is deliberately conservative:
// it returns true only when (a) the op's verb is found in the idea, (b) that
// verb binds to some entity, and (c) that entity is not target and target is
// not also a plausible owner of the same verb. On any ambiguity it returns
// false (keep the op), so it acts only as a backstop. No model call.
//
// v23.25 (task B). Pairs with bindOperationsToEntities (the positive hint used
// in the prompt); this is the negative post-filter applied after generation.
func OpMisplaced(idea, target string, opName string, all []schema.Entity) bool {
	bindings := bindCandidateOps(idea, all)
	verb := opVerb(opName)
	if verb == "" {
		return false
	}
	targetLC := strings.ToLower(target)

	// v26.58 (D068): ownsVerb tested strings.Contains(phrase, verb), so the op
	// verb "check" (CheckStock) was found inside the Order phrase "check out an
	// order" and CheckStock was dropped from Product. An entity now owns the
	// verb only when one of its phrases was bound by that same verb.
	ownsVerb := func(entity string) bool {
		for _, op := range bindings[entity] {
			if verbKey(op.Verb) == verbKey(verb) {
				return true
			}
		}
		return false
	}

	// If target plausibly owns the verb, never drop.
	if ownsVerb(target) {
		return false
	}
	// Otherwise, drop only if exactly one other entity clearly owns it.
	owner := ""
	for _, e := range all {
		if strings.EqualFold(e.Name, target) {
			continue
		}
		if ownsVerb(e.Name) {
			if owner != "" {
				return false // two owners: ambiguous, keep
			}
			owner = e.Name
		}
	}
	_ = targetLC
	return owner != ""
}

// opVerb lowercases the leading verb token of a PascalCase op name and maps it
// onto the actionVerbs vocabulary so "ApplyDiscount" -> "apply",
// "AdjustStock" -> "adjust", "CheckOut" -> "check out".
func opVerb(opName string) string {
	// split PascalCase into words
	var words []string
	start := 0
	for i := 1; i < len(opName); i++ {
		c := opName[i]
		if c >= 'A' && c <= 'Z' {
			words = append(words, opName[start:i])
			start = i
		}
	}
	words = append(words, opName[start:])
	if len(words) == 0 {
		return ""
	}
	lead := strings.ToLower(words[0])
	// v26.58 (D068): the old loop also accepted a prefix in either direction, so
	// CheckStock gave "check", ReOpen gave "reject", PaymentStatus gave "pay"
	// and C gave "check". Only a whole verb maps now: the first two words joined
	// ("Re"+"Open" is "reopen"), else the lead word. Any other lead is returned
	// as is and binds to nothing, so OpMisplaced keeps the op.
	//
	// v26.58 (D070): Checkout gave "checkout" while CheckOut gave "check out",
	// and the idea may spell the verb either way, so OpMisplaced missed a
	// Checkout op on the wrong entity. Both spellings now map by verbKey to the
	// first actionVerbs entry, "check out", and ownsVerb compares verbKey too.
	// This replaces the old "check"+"out" special case.
	if len(words) > 1 {
		if v := canonicalVerb(lead + strings.ToLower(words[1])); v != "" {
			return v
		}
	}
	if v := canonicalVerb(lead); v != "" {
		return v
	}
	return lead
}

// verbKey is the spelling-free form of a verb: "check out" and "checkout" both
// give "checkout".
func verbKey(v string) string {
	return strings.ReplaceAll(v, " ", "")
}

// canonicalVerb returns the first actionVerbs entry whose verbKey is key, or "".
func canonicalVerb(key string) string {
	for _, v := range actionVerbs {
		if verbKey(v) == key {
			return v
		}
	}
	return ""
}
