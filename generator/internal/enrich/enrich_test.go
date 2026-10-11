// generator/internal/enrich/enrich_test.go (v26.10): tests the pure parts of the
// enricher (prompt building, kind mapping, response parsing). The live-model path
// (Enrich → Ollama, Available) is an on-box step; these tests never spawn a model.
package enrich

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/belief"
)

func TestMapKind(t *testing.T) {
	cases := map[string]belief.Kind{
		"action":       belief.KindAction,
		"Action":       belief.KindAction,
		"computation":  belief.KindComputation,
		"precondition": belief.KindPrecondition,
		"layer":        "", // not an enrich kind
		"":             "",
		"garbage":      "",
	}
	for in, want := range cases {
		if got := mapKind(in); got != want {
			t.Errorf("mapKind(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildPromptIncludesFacts(t *testing.T) {
	e := New()
	p := e.buildPrompt(belief.EnrichFact{
		Subject:  "OrderService.Checkout",
		Language: "go",
		Params:   "id int, in map[string]any",
		Calls:    "Store.Get, Store.Save",
		Effects:  "db",
		Body:     "func Checkout() {}",
	})
	for _, want := range []string{
		"OrderService.Checkout", "id int, in map[string]any",
		"Store.Get, Store.Save", "db", "func Checkout() {}",
		"action", "computation", "precondition",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

// TestPromptFramesFactsAsConstraints covers the session 8 prevention: parser facts must be
// presented as authoritative constraints with explicit hard rules, not background.
func TestPromptFramesFactsAsConstraints(t *testing.T) {
	e := New()
	p := e.buildPrompt(belief.EnrichFact{Subject: "F", Effects: "db", Body: "x"})
	for _, want := range []string{
		"ESTABLISHED FACTS", "CERTAIN", "Do NOT contradict",
		"HARD RULES", "Do NOT claim", "OMIT", "Prefer the established facts",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing constraint framing %q", want)
		}
	}
}

// TestPromptNoEffectStatedExplicitly: when the parser saw no effect, the prompt
// must say so explicitly, so the model won't invent a write (the #1 contradiction).
func TestPromptNoEffectStatedExplicitly(t *testing.T) {
	e := New()
	p := e.buildPrompt(belief.EnrichFact{Subject: "F", Body: "x"}) // no Effects
	if !strings.Contains(p, "NONE") {
		t.Errorf("prompt should explicitly state NO effect was observed:\n%s", p)
	}
}

// TestPromptNoBodyGuidance: with no source body, the model is told to describe
// only what the facts support (most likely just the action).
func TestPromptNoBodyGuidance(t *testing.T) {
	e := New()
	p := e.buildPrompt(belief.EnrichFact{Subject: "F", Effects: "db"}) // no Body
	if !strings.Contains(p, "No source body") {
		t.Errorf("prompt should guide the no-body case:\n%s", p)
	}
}

func TestBuildPromptTruncatesBody(t *testing.T) {
	e := &Enricher{MaxBodyChars: 10}
	long := strings.Repeat("x", 500)
	p := e.buildPrompt(belief.EnrichFact{Subject: "F", Body: long})
	if !strings.Contains(p, "truncated") {
		t.Error("expected long body to be truncated")
	}
	if strings.Count(p, "x") > 50 {
		t.Error("body was not actually truncated")
	}
}

func TestResponseParsing(t *testing.T) {
	// Simulate what Enrich does after AskLLMJSON returns, without a model.
	raw := `{"observations":[{"kind":"action","description":"places an order"},{"kind":"garbage","description":"x"},{"kind":"precondition","description":""}]}`
	var resp enrichResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var out []belief.EnrichBelief
	for _, o := range resp.Observations {
		k := mapKind(o.Kind)
		desc := strings.TrimSpace(o.Description)
		if k == "" || desc == "" {
			continue
		}
		out = append(out, belief.EnrichBelief{Kind: k, Object: desc})
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 valid observation, got %d", len(out))
	}
	if out[0].Kind != belief.KindAction || out[0].Object != "places an order" {
		t.Errorf("wrong observation: %+v", out[0])
	}
}
