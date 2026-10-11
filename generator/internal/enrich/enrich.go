// generator/internal/enrich/enrich.go (v26.10, session 4): the enrich-eval tier.
//
// This package is the concrete, LLM-backed implementation of belief.Enricher. It
// reads a routine's parser facts (and body when available) and asks a local Ollama
// model to describe, in plain language a human can verify, what the routine does:
// the action / computation / precondition beliefs the deterministic parser cannot
// derive from the syntax alone.
//
// BOUNDARY: this package imports internal/ai (which spawns the model) and is wired
// only by generation-side binaries (gen, gen-server). understand-server must never
// import it. That keeps the air-gapped enterprise binary free of the AI stack and
// of any subprocess/network path, exactly as the project's hard constraints require.
// The belief store depends on the belief.Enricher interface, not on this package,
// so the store itself stays LLM-free and importable by understand-server.
//
// GRACEFUL ABSENCE: if Ollama is unreachable, Enrich returns an error, which the
// belief store treats as "no enrich beliefs", and parser-eval + human-eval continue
// to work. Nothing here is required for the store to function.
package enrich

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/ai"
	"github.com/hpdkhoa/gen-system/generator/internal/belief"
)

// Enricher is the LLM-backed belief.Enricher. Zero value is usable.
type Enricher struct {
	// MaxBodyChars bounds how much routine source is sent to the model (0 = a
	// sensible default). Keeps prompts small on large functions.
	MaxBodyChars int
}

// New returns a ready LLM enricher.
func New() *Enricher { return &Enricher{MaxBodyChars: 2000} }

// enrichSchema constrains the model to a small, predictable JSON shape: a list of
// {kind, description} objects. kind is limited to the enrich-tier vocabulary.
var enrichSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "observations": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "kind": {"type": "string", "enum": ["action", "computation", "precondition"]},
          "description": {"type": "string"}
        },
        "required": ["kind", "description"]
      }
    }
  },
  "required": ["observations"]
}`)

type enrichResponse struct {
	Observations []struct {
		Kind        string `json:"kind"`
		Description string `json:"description"`
	} `json:"observations"`
}

// Enrich implements belief.Enricher. It builds a prompt from the parser facts,
// asks the local model for behavioral observations, and maps them back to
// enrich-tier beliefs. Any failure (no model, bad JSON) yields (nil, err) and the
// store skips enrichment for this routine.
func (e *Enricher) Enrich(fact belief.EnrichFact) ([]belief.EnrichBelief, error) {
	prompt := e.buildPrompt(fact)
	raw, err := ai.AskLLMJSON(prompt, ai.ModelFor(ai.RolePlan), enrichSchema)
	if err != nil {
		return nil, err
	}
	var resp enrichResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil, err
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
	return out, nil
}

// mapKind converts a model kind string to a known enrich-tier belief.Kind. Unknown
// strings are dropped (the model is not allowed to invent kinds).
func mapKind(s string) belief.Kind {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "action":
		return belief.KindAction
	case "computation":
		return belief.KindComputation
	case "precondition":
		return belief.KindPrecondition
	default:
		return ""
	}
}

func (e *Enricher) buildPrompt(fact belief.EnrichFact) string {
	max := e.MaxBodyChars
	if max <= 0 {
		max = 2000
	}
	body := fact.Body
	if len(body) > max {
		body = body[:max] + "\n… (truncated)"
	}

	var sb strings.Builder
	sb.WriteString("You are analysing ONE routine from an existing codebase to help a ")
	sb.WriteString("human reviewer understand what it does. A banking analyst must be able to ")
	sb.WriteString("verify every line you write against the code, so accuracy matters far more ")
	sb.WriteString("than completeness: a missing observation is fine, a WRONG one is not.\n\n")

	// ESTABLISHED FACTS: the prompt presents them as authoritative constraints the model must
	// build on, not as background. This is the prevention half of the trust model:
	// anchoring the description to what the parser proved reduces hallucination at
	// the source, before the post-hoc validation gate ever runs.
	sb.WriteString("Routine: ")
	sb.WriteString(fact.Subject)
	if fact.Language != "" {
		sb.WriteString(" (" + fact.Language + ")")
	}
	sb.WriteString("\n\n")
	sb.WriteString("ESTABLISHED FACTS about this routine — determined by a compiler-accurate ")
	sb.WriteString("parser. These are CERTAIN. Do NOT contradict them, and do not restate them:\n")
	if fact.Params != "" {
		sb.WriteString("  • Inputs: " + fact.Params + "\n")
	} else {
		sb.WriteString("  • Inputs: (none)\n")
	}
	if fact.Calls != "" {
		sb.WriteString("  • Calls: " + fact.Calls + "\n")
	}
	if fact.Effects != "" {
		sb.WriteString("  • Side effects observed: " + fact.Effects + "\n")
	} else {
		sb.WriteString("  • Side effects observed: NONE — the parser saw no database or filesystem write.\n")
	}
	if fact.DocComment != "" {
		sb.WriteString("  • Doc comment: " + strings.TrimSpace(fact.DocComment) + "\n")
	}

	// Explicit guardrails that mirror exactly what the validation gate checks, so
	// a well-behaved model never trips it.
	sb.WriteString("\nHARD RULES:\n")
	sb.WriteString("  1. Do NOT claim the routine writes, saves, persists, or stores data unless ")
	sb.WriteString("its observed side effects include a database or filesystem effect above.\n")
	sb.WriteString("  2. Do NOT claim it is pure / read-only / has no side effects if any effect is listed above.\n")
	sb.WriteString("  3. Describe ONLY behaviour you can point to in the source below. If the source ")
	sb.WriteString("does not clearly show a calculation or a precondition, OMIT that observation — do not guess.\n")
	sb.WriteString("  4. Prefer the established facts over your reading of the code if they seem to disagree.\n")

	if body != "" {
		sb.WriteString("\nSource:\n")
		sb.WriteString(body)
		sb.WriteString("\n")
	} else {
		sb.WriteString("\n(No source body available — describe only what the facts above support, ")
		sb.WriteString("most likely just the action.)\n")
	}

	sb.WriteString("\nReturn a JSON object {\"observations\": [ {\"kind\":..., \"description\":...} ]} where kind is one of:\n")
	sb.WriteString("  - action: one plain sentence for what the routine accomplishes overall (always safe to give).\n")
	sb.WriteString("  - computation: a specific calculation it performs, quoted from the logic (e.g. \"new balance = old balance − amount\"). OMIT if the source shows none.\n")
	sb.WriteString("  - precondition: a state/value it checks and rejects BEFORE mutating (e.g. \"rejects if amount would make stock negative\"). OMIT if the source shows no such guard.\n")
	sb.WriteString("At most one observation per kind. When unsure, return fewer observations. ")
	sb.WriteString("If you can only support the action, return just that.\n")
	return sb.String()
}

// ─── Independent producer (session 9) ─────────────────────────

// Routine is one unit the producer will enrich: the parser facts plus its source.
// The caller (the gen-enrich binary) builds these from a loaded context, keeping
// the enrich package decoupled from internal/context.
type Routine struct {
	SymbolID   string
	Subject    string
	Name       string
	Language   string
	Params     string
	Calls      string
	Effects    string
	Body       string
	DocComment string
}

// RunResult reports what an out-of-band enrichment pass did.
type RunResult struct {
	Enriched int // routines that produced at least one observation
	Skipped  int // routines the model returned nothing for (or errored)
	Beliefs  int // total observations stored
	// SaveErrors counts routines whose beliefs could not be saved. v26.58
	// (follow up of D020): the result of the save was ignored, so a lost
	// write still counted as enriched.
	SaveErrors int
}

// Run is the independent producer entry point. It enriches each routine with the
// local model and writes the results into the belief store as persistent enrich
// beliefs (surviving future parser rebuilds, fingerprinted against the source).
// It is deliberately decoupled: the caller loads the project and supplies the
// routines, so this package never imports internal/context. Best-effort per
// routine: a model failure on one routine skips it and the run continues.
func (e *Enricher) Run(store *belief.Store, routines []Routine) RunResult {
	var res RunResult
	for _, r := range routines {
		fact := belief.EnrichFact{
			SymbolID: r.SymbolID, Subject: r.Subject, Name: r.Name,
			Language: r.Language, Params: r.Params, Calls: r.Calls,
			Effects: r.Effects, Body: r.Body, DocComment: r.DocComment,
		}
		props, err := e.Enrich(fact)
		if err != nil || len(props) == 0 {
			res.Skipped++
			continue
		}
		// Replace any prior enrich beliefs for this routine, then store fresh ones.
		store.ClearEnrichForSymbol(r.SymbolID)
		inputs := make([]belief.EnrichInput, 0, len(props))
		for _, p := range props {
			inputs = append(inputs, belief.EnrichInput{
				SymbolID: r.SymbolID, Subject: r.Subject,
				Kind: p.Kind, Object: p.Object, Body: r.Body,
			})
		}
		if err := store.UpsertEnrichBeliefs(inputs); err != nil {
			res.SaveErrors++
			continue
		}
		res.Enriched++
		res.Beliefs += len(inputs)
	}
	return res
}

// SummarizeServices rolls up each service's member beliefs into a scope summary.
// It reads the already-produced beliefs (never raw source), so each prompt is a
// handful of one-line member claims. Writes summaries into the store. Best-effort.
func (e *Enricher) SummarizeServices(store *belief.Store) int {
	svcMembers := store.ServiceMembers()
	names := make([]string, 0, len(svcMembers))
	for s := range svcMembers {
		names = append(names, s)
	}
	sort.Strings(names)

	n := 0
	for _, svc := range names {
		memberIDs := svcMembers[svc]
		members := store.MembersForSummary(memberIDs)
		if len(members) == 0 {
			continue
		}
		prose, err := e.summarizeScope(belief.ScopeService, svc, members)
		if err != nil || prose == "" {
			continue
		}
		if err := store.UpsertSummary(belief.SummaryInput{
			Scope: belief.ScopeService, Name: svc, Object: prose, MemberIDs: memberIDs,
		}); err != nil {
			continue // v26.58: a summary that was not saved is not counted
		}
		n++
	}
	return n
}

// summarizeScope asks the model for one paragraph describing a scope from its
// members' one-line claims.
func (e *Enricher) summarizeScope(scope, name string, members []belief.ScopeMemberSummary) (string, error) {
	var sb strings.Builder
	sb.WriteString("You are writing a ONE-PARAGRAPH summary of a ")
	sb.WriteString(scope)
	sb.WriteString(" in an existing codebase, for a human reviewer. Base it ONLY on the ")
	sb.WriteString("member descriptions below — do not invent capabilities not listed. Be ")
	sb.WriteString("concrete and concise.\n\n")
	sb.WriteString(scope)
	sb.WriteString(": ")
	sb.WriteString(name)
	sb.WriteString("\nMembers:\n")
	for _, m := range members {
		sb.WriteString("  • " + m.Subject + ": " + m.Claim + "\n")
	}
	sb.WriteString("\nReturn a JSON object {\"summary\": \"…\"} with a single paragraph.\n")

	raw, err := ai.AskLLMJSON(sb.String(), ai.ModelFor(ai.RolePlan), summarySchema)
	if err != nil {
		return "", err
	}
	var resp struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Summary), nil
}

var summarySchema = json.RawMessage(`{
  "type": "object",
  "properties": { "summary": {"type": "string"} },
  "required": ["summary"]
}`)

// Available reports whether the local model responds. Cheap best-effort probe the
// generation binaries can call before wiring the enricher; a false result means
// the store should run parser-eval only. It never errors; it returns a bool.
func Available() bool {
	_, err := ai.AskLLMWithModelRaw("ping: reply with the single word ok", ai.ModelFor(ai.RolePlan))
	return err == nil
}

// compile-time assertion that Enricher satisfies the interface.
var _ belief.Enricher = (*Enricher)(nil)
