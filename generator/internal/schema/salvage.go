// generator/internal/schema/salvage.go — v23.1 (round 3): salvage is now
// anchor-based (find each "name", then its "fields" value) so it recovers ALL
// entities even when the model's outer-object brackets are mismatched. Round 2
// required a brace-balanced outer object and silently dropped entities whose
// surrounding brackets were broken (e.g. Task in the real deepseek-coder run).
package schema

import (
	"encoding/json"
	"regexp"
	"strings"
)

var (
	// trailing comma before } or ]  ->  drop the comma
	reTrailingComma = regexp.MustCompile(`,(\s*[}\]])`)
	// runs of whitespace (incl. the tab storms this model emits) -> single space
	reWhitespaceRun = regexp.MustCompile(`[ \t]{2,}`)
	// a spaces-only key like "due date" -> "due_date" (only inside quotes)
	reSpacedKey = regexp.MustCompile(`"([A-Za-z0-9_]+) +([A-Za-z0-9_ ]+?)"\s*:`)
)

// RepairJSON applies mechanical fixes that don't change meaning: normalises
// smart quotes, collapses whitespace runs, removes trailing commas, and
// snaps spaced identifier keys to snake_case. It does NOT try to balance
// brackets — that's SalvageEntities' job.
func RepairJSON(s string) string {
	s = strings.NewReplacer(
		"\u201c", `"`, "\u201d", `"`, // smart double quotes
		"\u2018", "'", "\u2019", "'", // smart single quotes
		"\r", "",
	).Replace(s)
	s = reWhitespaceRun.ReplaceAllString(s, " ")
	// snap spaced keys: "due date" -> "due_date" (repeat to catch >1 space group)
	for i := 0; i < 3; i++ {
		s = reSpacedKey.ReplaceAllString(s, `"${1}_${2}":`)
	}
	s = strings.ReplaceAll(s, " _", "_")
	s = reTrailingComma.ReplaceAllString(s, "$1")
	return s
}

// SalvageEntities scans raw model text for entities by anchoring on each
// "name": "X" and then extracting the nearest following "fields": <value>,
// reading just that value's balanced brackets. This is robust to the kind of
// corruption deepseek-coder emits — junk objects, doubled/mismatched brackets,
// nested {"parentEntities":...} blobs — that prevents the *outer* entity object
// from ever closing cleanly. Round 3: previously we required a brace-balanced
// outer object, which silently dropped the 2nd entity (e.g. Task) whenever its
// surrounding brackets were broken.
func SalvageEntities(raw string) SchemaFile {
	var sf SchemaFile
	repaired := RepairJSON(raw)
	seen := map[string]bool{}
	for _, anchor := range findNameAnchors(repaired) {
		name := anchor.name
		if name == "" || seen[name] {
			continue
		}
		fieldsRaw := extractFieldsAfter(repaired, anchor.end, anchor.nextStart)
		if fieldsRaw == "" {
			continue
		}
		fields, err := fieldsFromRaw(json.RawMessage(fieldsRaw))
		if err != nil || len(fields) == 0 {
			continue
		}
		seen[name] = true
		sf.Entities = append(sf.Entities, Entity{Name: name, Fields: fields})
	}
	return sf
}

type nameAnchor struct {
	name      string
	end       int // index just after the matched "name":"X"
	nextStart int // index where the next name anchor begins (or len)
}

var reNameKey = regexp.MustCompile(`"name"\s*:\s*"([^"]+)"`)

// findNameAnchors returns every "name":"X" occurrence with the span to the next
// one, so field extraction is bounded to a single entity's region.
func findNameAnchors(s string) []nameAnchor {
	locs := reNameKey.FindAllStringSubmatchIndex(s, -1)
	var out []nameAnchor
	for i, m := range locs {
		next := len(s)
		if i+1 < len(locs) {
			next = locs[i+1][0]
		}
		out = append(out, nameAnchor{name: s[m[2]:m[3]], end: m[1], nextStart: next})
	}
	return out
}

var reFieldsKey = regexp.MustCompile(`"fields"\s*:\s*`)

// extractFieldsAfter finds the "fields" value within [from,to) and returns the
// balanced {...} or [...] substring for it, ignoring everything else.
func extractFieldsAfter(s string, from, to int) string {
	if to > len(s) {
		to = len(s)
	}
	if from < 0 || from >= to {
		return ""
	}
	region := s[from:to]
	loc := reFieldsKey.FindStringIndex(region)
	if loc == nil {
		return ""
	}
	start := from + loc[1]
	for start < to && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n') {
		start++
	}
	if start >= to {
		return ""
	}
	open := s[start]
	var closeCh byte
	switch open {
	case '{':
		closeCh = '}'
	case '[':
		closeCh = ']'
	default:
		return ""
	}
	return balancedSlice(s, start, open, closeCh, to)
}

// balancedSlice returns s[start:end+1] where end closes the bracket opened at
// start, honouring quoted strings. Bounded by limit to avoid runaway scans.
func balancedSlice(s string, start int, open, closeCh byte, limit int) string {
	depth := 0
	inStr := false
	esc := false
	for i := start; i < limit; i++ {
		c := s[i]
		if inStr {
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case open:
			depth++
		case closeCh:
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}
