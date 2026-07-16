// generator/internal/planner_v2/entity_validation.go — v23.25 (task A): a
// deterministic guard that every planned entity traces to a noun in the idea.
// The planner historically over-generated (5 entities for a 2-noun idea —
// adding OrderItem / Customer / Discount that the idea never mentioned). The
// v23.20 prompt got the simple idea down to ~2, but a prompt is probabilistic;
// this is the durable backstop: tokenize the idea, and for each planned entity
// require a token match (exact, singular/plural, or substring). Entities with
// NO anchor get one re-prompt; if still unanchored they are dropped with a
// logged warning rather than silently shipped.
package planner_v2

import (
	"fmt"
	"strings"

	"gen-system/generator/internal/ai"
)

// validateEntities filters specs to those whose name traces to a noun in the
// idea text. Unanchored specs trigger a single re-prompt of ai.PlanEntities;
// any that remain unanchored after that are dropped. Returns the kept specs.
//
// goal/features/archType/components mirror the BuildPlanV2 call so a re-prompt
// uses the identical inputs. A nil/empty re-prompt result leaves the first-pass
// anchored specs untouched (never makes things worse).
func validateEntities(specs []EntitySpec, goal string, features []string, archType string, components []string) []EntitySpec {
	anchors := ideaTokens(goal, features)

	kept, unanchored := partitionByAnchor(specs, anchors)
	if len(unanchored) == 0 {
		return specs // all anchored — nothing to do
	}

	for _, u := range unanchored {
		fmt.Printf("     ⚠️  entity %q has no anchor in the idea — re-checking\n", u.Name)
	}

	// One re-prompt. The prompt already nudges toward idea nouns (v23.20); a
	// second sample sometimes drops the hallucinated entity on its own.
	raw, err := ai.PlanEntities(goal, features, archType, components)
	if err == nil && len(raw) > 0 {
		reSpecs := make([]EntitySpec, 0, len(raw))
		for _, r := range raw {
			reSpecs = append(reSpecs, EntitySpec{Name: r.Name, Fields: r.Fields})
		}
		reKept, reUnanchored := partitionByAnchor(reSpecs, anchors)
		// Accept the re-prompt only if it is at least as anchored as the first
		// pass (fewer or equal unanchored). Otherwise keep the first-pass kept.
		if len(reUnanchored) <= len(unanchored) {
			for _, d := range reUnanchored {
				fmt.Printf("     ⚠️  dropping unanchored entity %q (not in idea after re-prompt)\n", d.Name)
			}
			return reKept
		}
	}

	// Re-prompt didn't help — drop the first-pass unanchored entities.
	for _, d := range unanchored {
		fmt.Printf("     ⚠️  dropping unanchored entity %q (not traceable to the idea)\n", d.Name)
	}
	return kept
}

// partitionByAnchor splits specs into (anchored, unanchored) against the token
// set. A spec is anchored when its name matches a token exactly, as a singular/
// plural variant, or as a substring in either direction (so "OrderItem" anchors
// on "order" and "user" anchors on "users").
func partitionByAnchor(specs []EntitySpec, anchors map[string]bool) (anchored, unanchored []EntitySpec) {
	for _, s := range specs {
		if entityAnchored(s.Name, anchors) {
			anchored = append(anchored, s)
		} else {
			unanchored = append(unanchored, s)
		}
	}
	return
}

func entityAnchored(name string, anchors map[string]bool) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return false
	}
	for _, cand := range []string{n, singular(n), plural(n)} {
		if anchors[cand] {
			return true
		}
		// substring either direction against every token
		for tok := range anchors {
			if len(tok) >= 4 && (strings.Contains(cand, tok) || strings.Contains(tok, cand)) {
				return true
			}
		}
	}
	return false
}

// ideaTokens builds the anchor set from the goal text plus the extracted
// feature strings. Tokens shorter than 3 chars and a small stop-word set are
// dropped so common words don't anchor everything.
func ideaTokens(goal string, features []string) map[string]bool {
	set := map[string]bool{}
	add := func(text string) {
		for _, w := range strings.FieldsFunc(text, func(r rune) bool {
			return !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9')
		}) {
			w = strings.ToLower(w)
			if len(w) < 3 || stopWords[w] {
				continue
			}
			set[w] = true
			set[singular(w)] = true
			set[plural(w)] = true
		}
	}
	add(goal)
	for _, f := range features {
		add(f)
	}
	return set
}

// singular/plural are intentionally tiny — enough for the common -s / -es / -ies
// English noun shapes the planner produces, no linguistics library (offline).
func singular(w string) string {
	switch {
	case strings.HasSuffix(w, "ies") && len(w) > 3:
		return w[:len(w)-3] + "y"
	case strings.HasSuffix(w, "ses") && len(w) > 3:
		return w[:len(w)-2]
	case strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") && len(w) > 1:
		return w[:len(w)-1]
	}
	return w
}

func plural(w string) string {
	switch {
	case strings.HasSuffix(w, "y") && len(w) > 1:
		return w[:len(w)-1] + "ies"
	case strings.HasSuffix(w, "s"), strings.HasSuffix(w, "x"), strings.HasSuffix(w, "ch"), strings.HasSuffix(w, "sh"):
		return w + "es"
	}
	return w + "s"
}

var stopWords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "that": true,
	"can": true, "has": true, "have": true, "are": true, "but": true,
	"only": true, "its": true, "their": true, "they": true, "them": true,
	"app": true, "system": true, "application": true, "user": false, // user is a legit entity noun
	"add": true, "new": true, "create": true, "make": true, "build": true,
	"should": true, "would": true, "this": true, "from": true, "into": true,
	"each": true, "every": true, "any": true, "all": true, "some": true,
	"when": true, "then": true, "else": true, "must": true, "may": true,
	"per": true, "via": true, "use": true, "using": true, "given": true,
}
