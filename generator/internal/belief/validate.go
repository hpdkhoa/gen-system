// generator/internal/belief/validate.go — v26.13 (session 7): the parser-validation
// gate for enrich (model-written) beliefs.
//
// The principle (settled with the owner): validate enrich claims against PARSER-EVAL,
// not raw source — the parser is already the trustworthy distillation of the source,
// so comparing to it is deterministic and free. Only STRUCTURAL claims are
// mechanically checkable (effect / data-change / calls). SEMANTIC claims
// (computation / precondition) cannot be checked without the very reasoning the LLM
// was brought in to do, so they are routed to a human ("needs-human"), never
// auto-blessed. A claim that positively contradicts a parser fact is flagged
// "contradicts" and (via the existing Source gate) never steers generation.
//
// This is DETECTION. Prevention (feeding parser facts into the prompt as hard
// constraints) is session 8. The two compose: prevention lowers the error rate,
// detection catches the structurally-checkable errors that slip through, and human
// verification is the only validator for the semantic claims.
package belief

import "strings"

// parserFacts is the deterministic ground truth for one routine, distilled from the
// parser beliefs already derived for it. Used as the oracle for validation.
type parserFacts struct {
	hasDBWrite bool
	hasFSWrite bool
	effects    string // raw "db, fs" for notes
}

// factsFromParserBeliefs distills the checkable ground truth from a routine's
// parser beliefs.
func factsFromParserBeliefs(parserBeliefs []*Belief) parserFacts {
	f := parserFacts{}
	for _, b := range parserBeliefs {
		switch b.Kind {
		case KindEffect:
			f.effects = b.Object
			if strings.Contains(b.Object, "db") {
				f.hasDBWrite = true
			}
			if strings.Contains(b.Object, "fs") {
				f.hasFSWrite = true
			}
		case KindDataChange:
			f.effects = joinNonEmpty(f.effects, b.Object)
			if strings.Contains(b.Object, "database") {
				f.hasDBWrite = true
			}
			if strings.Contains(b.Object, "filesystem") {
				f.hasFSWrite = true
			}
		}
	}
	return f
}

// validateEnrich classifies one enrich belief against the parser facts, setting
// b.Validated and b.ValidationNote. Mutates b in place.
func validateEnrich(b *Belief, f parserFacts) {
	switch b.Kind {
	case KindComputation, KindPrecondition:
		// Semantic — not mechanically checkable. A human is the only validator.
		b.Validated = "needs-human"
		b.ValidationNote = "semantic claim: verify against the source by hand"
		return
	}

	// Structural / action claims: look for assertions the parser can contradict.
	text := strings.ToLower(b.Object)

	// Claim of persistence when the parser saw no state-changing effect at all.
	if assertsPersistence(text) && !f.hasDBWrite && !f.hasFSWrite {
		b.Validated = "contradicts"
		b.ValidationNote = "claims it writes/saves data, but the parser observed no db/fs effect"
		return
	}
	// Claim it performs no writes when the parser DID see one.
	if assertsPurity(text) && (f.hasDBWrite || f.hasFSWrite) {
		b.Validated = "contradicts"
		b.ValidationNote = "claims no state change, but the parser observed: " + f.effects
		return
	}

	b.Validated = "consistent"
}

// assertsPersistence detects language claiming the routine persists state.
// Two lessons are encoded here, both found by real runs:
//  1. The phrasing test must tolerate words BETWEEN verb and target — "saves the
//     order to the database" is a persistence claim that a naive "saves to"
//     substring misses.
//  2. It must NOT fire on incidental nouns. "creates a handler with an in-memory
//     store" mentions "store" but claims no persistence, and an explicit
//     in-memory/cache qualifier is a positive signal of NON-persistence.
//
// So: an unambiguous phrase, OR (a persistence VERB + a durable-storage TARGET),
// with in-memory phrasing vetoing the match.
func assertsPersistence(text string) bool {
	// Explicit non-persistence beats everything — the claim itself says it's not durable.
	for _, neg := range []string{"in-memory", "in memory", "memory-only", "temporary", "cache"} {
		if strings.Contains(text, neg) {
			return false
		}
	}
	for _, kw := range []string{"persists", "inserts into", "writes to", "saves to", "stores in", "writes it to"} {
		if strings.Contains(text, kw) {
			return true
		}
	}
	// Verb + durable target. "store"/"record" are excluded as targets: they're too
	// often incidental nouns (a *Store receiver, "a record" as a value).
	verbs := []string{"save", "saves", "persist", "persisted", "write", "writes", "insert", "inserts"}
	targets := []string{"database", "db", "table", "disk", "repository", "persistent storage"}
	hasVerb, hasTarget := false, false
	for _, v := range verbs {
		if containsWord(text, v) {
			hasVerb = true
			break
		}
	}
	for _, t := range targets {
		if containsWord(text, t) {
			hasTarget = true
			break
		}
	}
	return hasVerb && hasTarget
}

// containsWord reports whether text contains word as a whole word, so "store"
// doesn't match inside "restore" and "db" doesn't match inside "dbg".
func containsWord(text, word string) bool {
	i := 0
	for {
		j := strings.Index(text[i:], word)
		if j < 0 {
			return false
		}
		j += i
		beforeOK := j == 0 || !isWordByte(text[j-1])
		end := j + len(word)
		afterOK := end >= len(text) || !isWordByte(text[end])
		if beforeOK && afterOK {
			return true
		}
		i = j + 1
		if i >= len(text) {
			return false
		}
	}
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// assertsPurity detects language claiming the routine has no side effects.
func assertsPurity(text string) bool {
	for _, kw := range []string{"pure function", "no side effect", "read-only", "does not modify", "without modifying", "no state change"} {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

func joinNonEmpty(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + ", " + b
}

// IsBlockedForSteering reports whether a belief must NOT influence generation. A
// contradicting enrich belief is blocked even if something tried to promote it;
// belief steering already gates on Source==human, so this is a belt-and-braces
// guard for any future path that reads enrich beliefs directly.
func (b *Belief) IsBlockedForSteering() bool {
	return b.Source == SourceEnrich && b.Validated == "contradicts"
}
