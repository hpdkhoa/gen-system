package flow

import (
	"regexp"
	"sort"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
)

// v26.66: COBOL data flow for beliefs and effects. A COBOL paragraph has no
// parameters, so its inputs and the data it writes come from its statements:
// MOVE a TO b reads a and writes b, COMPUTE b = a + c reads a and c and writes
// b, a condition reads every name in it, WRITE rec writes rec. File statements
// also mark the paragraph as touching the filesystem.
//
// The reader is syntactic. It collects every word shaped like a data name and
// drops COBOL keywords; the belief layer then keeps only names the index knows
// as data items, so a missed keyword cannot become a belief.

// cobolFileStatements are the file I/O verbs the COBOL frontend emits as
// NodeAssign (ast.cobolFileVerbs).
var cobolFileStatements = map[string]bool{
	"OPEN": true, "CLOSE": true, "READ": true, "WRITE": true,
	"REWRITE": true, "DELETE": true, "START": true,
}

var cobolDataName = regexp.MustCompile(`^[A-Z][A-Z0-9-]*$`)

// cobolKeywords are the words that look like data names but are part of the
// statement syntax. FUNCTION is handled apart: the word after it is an
// intrinsic function, not data.
var cobolKeywords = map[string]bool{
	"TO": true, "FROM": true, "BY": true, "INTO": true, "GIVING": true, "ROUNDED": true,
	"REMAINDER": true, "OF": true, "IN": true, "AND": true, "OR": true, "NOT": true,
	"IS": true, "THAN": true, "GREATER": true, "LESS": true, "EQUAL": true, "EQUALS": true,
	"ZERO": true, "ZEROS": true, "ZEROES": true, "SPACE": true, "SPACES": true,
	"HIGH-VALUE": true, "HIGH-VALUES": true, "LOW-VALUE": true, "LOW-VALUES": true,
	"QUOTE": true, "QUOTES": true, "ALL": true, "NULL": true, "NULLS": true,
	"TRUE": true, "FALSE": true, "WHEN": true, "OTHER": true, "ALSO": true, "THRU": true,
	"THROUGH": true, "CORRESPONDING": true, "CORR": true, "ON": true, "SIZE": true,
	"ERROR": true, "WITH": true, "NO": true, "ADVANCING": true, "UPON": true,
	"DELIMITED": true, "POINTER": true, "TALLYING": true, "REPLACING": true,
	"LEADING": true, "TRAILING": true, "FIRST": true, "CHARACTERS": true, "BEFORE": true,
	"AFTER": true, "INITIAL": true, "NUMERIC": true, "ALPHABETIC": true,
	"ALPHABETIC-LOWER": true, "ALPHABETIC-UPPER": true, "POSITIVE": true,
	"NEGATIVE": true, "INPUT": true, "OUTPUT": true, "I-O": true, "EXTEND": true,
	"AT": true, "END": true, "NEXT": true, "RECORD": true, "KEY": true, "INVALID": true,
	"LINE": true, "LINES": true, "PAGE": true, "ADDRESS": true, "LENGTH": true,
	"USING": true, "REFERENCE": true, "CONTENT": true, "VALUE": true, "RETURNING": true,
	"UP": true, "DOWN": true, "CONVERTING": true, "COUNT": true, "DELIMITER": true,
	"STANDARD-1": true, "LOCK": true, "THEN": true, "ELSE": true,
}

// cobolNames returns the data-name shaped words of a statement text, without
// keywords, literals and the intrinsic function after FUNCTION.
func cobolNames(words []string) []string {
	var out []string
	for i := 0; i < len(words); i++ {
		w := strings.TrimRight(strings.ToUpper(words[i]), ".,;")
		w = strings.TrimLeft(w, ":(")
		w = strings.TrimRight(w, ")")
		if w == "FUNCTION" {
			i++ // the intrinsic's name
			continue
		}
		if strings.HasPrefix(w, "END-") || cobolKeywords[w] || !cobolDataName.MatchString(w) {
			continue
		}
		out = append(out, w)
	}
	return out
}

// splitAt returns the words before and after the first occurrence of one of
// the keywords; after is nil when none occurs.
func splitAt(words []string, keys ...string) (before, after []string) {
	for i, w := range words {
		u := strings.TrimRight(strings.ToUpper(w), ".,")
		for _, k := range keys {
			if u == k {
				return words[:i], words[i+1:]
			}
		}
	}
	return words, nil
}

// cobolDataFlow adds the names node n reads and writes to the two sets, and
// marks a file statement as a filesystem effect.
func cobolDataFlow(n *ast.Node, efx *FuncEffects, reads, writes map[string]bool) {
	add := func(set map[string]bool, words []string) {
		for _, w := range cobolNames(words) {
			set[w] = true
		}
	}
	switch n.Kind {
	case ast.NodeBranch:
		// IF and WHEN conditions, the EVALUATE subject.
		add(reads, strings.Fields(n.RawSource))
		return
	case ast.NodeCall:
		// CALL "X" USING a b, EXEC SQL ... :host-var: the arguments are read.
		for _, a := range n.CallArgs {
			add(reads, strings.Fields(a))
		}
		for _, w := range strings.Fields(n.RawSource) {
			if strings.HasPrefix(w, ":") {
				add(reads, []string{w})
			}
		}
		return
	case ast.NodeAssign:
	default:
		return
	}
	verb := strings.ToUpper(n.Name)
	words := strings.Fields(n.RawSource)
	if len(words) > 0 && strings.EqualFold(words[0], verb) {
		words = words[1:]
	}
	if cobolFileStatements[verb] {
		efx.Effects[EffectFS] = true
		if verb == "WRITE" || verb == "REWRITE" || verb == "DELETE" {
			efx.FSWrite = true
		}
	}
	switch verb {
	case "MOVE":
		src, dst := splitAt(words, "TO")
		add(reads, src)
		add(writes, dst)
	case "COMPUTE":
		dst, expr := splitAt(words, "=", "EQUAL")
		add(writes, dst)
		add(reads, expr)
	case "ADD", "SUBTRACT", "MULTIPLY", "DIVIDE":
		operands, giving := splitAt(words, "GIVING")
		if giving != nil {
			add(reads, operands)
			add(writes, giving)
			return
		}
		// ADD a TO b, SUBTRACT a FROM b, MULTIPLY a BY b, DIVIDE a INTO b:
		// b is read and written.
		src, dst := splitAt(operands, "TO", "FROM", "BY", "INTO")
		add(reads, src)
		add(reads, dst)
		add(writes, dst)
	case "INITIALIZE", "ACCEPT":
		add(writes, words)
	case "SET":
		dst, src := splitAt(words, "TO", "UP", "DOWN")
		add(writes, dst)
		add(reads, src)
	case "STRING", "UNSTRING":
		src, dst := splitAt(words, "INTO")
		add(reads, src)
		add(writes, dst)
	case "INSPECT":
		add(reads, words)
		if len(words) > 0 {
			add(writes, words[:1])
		}
	case "DISPLAY":
		add(reads, words)
	case "WRITE", "REWRITE":
		rec, from := splitAt(words, "FROM")
		add(writes, rec)
		add(reads, from)
	case "READ":
		_, into := splitAt(words, "INTO")
		add(writes, into)
	}
}

func sortedKeys(set map[string]bool) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
