// cond_text.go — v26.25: render a token slice back into readable source text.
//
// NodeLoop.RawSource and NodeBranch.RawSource are the CONDITION LABEL a human
// reads on the rendered flowchart (the hexagon / diamond text). The frontends
// tokenize the condition and previously rejoined it with a plain space, which
// produced labels like
//
//	let i = 0 ; i < arr . length ; i + +
//
// where the source said `let i = 0; i < arr.length; i++`. The label matters.
// The phrase "renders the control-flow graph as a flowchart for human
// review" is a load-bearing claim, and a label a reader has to mentally
// re-assemble undercuts it.
//
// This is deliberately a RENDERER. It does not parse again. It applies spacing
// conventions to tokens, so it can never fail or change the node structure.
package ast

import "strings"

// tokens that hug what comes before them (no space in front)
var noSpaceBefore = map[string]bool{
	".": true, ",": true, ";": true, ":": true,
	")": true, "]": true, "(": true, "[": true,
	"++": true, "--": true,
}

// tokens that hug what comes after them (no space behind)
var noSpaceAfter = map[string]bool{
	".": true, "(": true, "[": true, "!": true,
}

// joinCondTokens renders tokens as source-like text for a flowchart label.
// Consecutive "+"/"-" pairs are folded back into the "++"/"--" operators the
// tokenizer split apart.
func joinCondTokens(toks []string) string {
	var b strings.Builder
	prev := ""
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if (t == "+" || t == "-") && i+1 < len(toks) && toks[i+1] == t {
			t += t
			i++
		}
		if b.Len() > 0 && !noSpaceBefore[t] && !noSpaceAfter[prev] {
			b.WriteString(" ")
		}
		b.WriteString(t)
		prev = t
	}
	return strings.TrimSpace(b.String())
}
