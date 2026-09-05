// generator/internal/style/profile.go — v26.33: a team style profile.
//
// A profile is what a team believes its code should look like: naming, folder
// layout, the patterns it wants and the ones it bans, size limits, and free
// text for the model. It is loaded from JSON (gen-style.json), or derived from
// an existing repository through the Understand graph (derive.go). Both roads
// produce the same struct, and the same gate (gate.go) checks generated code
// against it.
//
// Every rule is a belief with a source. A derived profile records the evidence
// it was built from (how many symbols were seen, how many agreed), so a human
// can tell a rule the team wrote from a rule the tool guessed. That mirrors
// the parser / enrich / human split in internal/belief: the gate only
// enforces what somebody stated, and it says where each statement came from.
package style

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Severity of a rule. An "error" finding can block a run in gate mode; a
// "warn" finding is always reported and never blocks.
type Severity string

const (
	SevError Severity = "error"
	SevWarn  Severity = "warn"
)

// Mode is what the pipeline does with the profile.
//
//	off      the profile is ignored
//	report   rules go into the prompt; findings are reported, nothing blocks
//	gate     report, plus op level style repair, plus a halt on error findings
//	autofix  gate, plus file level findings routed to the repair loop once
type Mode string

const (
	ModeOff     Mode = "off"
	ModeReport  Mode = "report"
	ModeGate    Mode = "gate"
	ModeAutofix Mode = "autofix"
)

// Profile is the whole style contract. Zero fields mean "no rule".
type Profile struct {
	Version   int    `json:"version"`
	Name      string `json:"name,omitempty"`
	Source    string `json:"source,omitempty"`     // "human" or "derived:<root>"
	DerivedAt string `json:"derived_at,omitempty"` // RFC3339 when derived

	Naming    Naming    `json:"naming"`
	Structure Structure `json:"structure"`
	Patterns  Patterns  `json:"patterns"`
	Code      Code      `json:"code"`
	Prompt    Prompt    `json:"prompt"`
	Gate      Gate      `json:"gate"`

	// Severity overrides per rule id, e.g. {"code.max_func_lines": "error"}.
	// Unlisted rules use the default in defaultSeverity.
	Severity map[string]Severity `json:"severity,omitempty"`

	// Evidence per rule id, written by Derive. Seen is how many items the
	// rule was measured on; Agree is how many already followed it.
	Evidence map[string]Evidence `json:"evidence,omitempty"`
}

// Naming rules. Case names: "PascalCase", "camelCase", "snake_case",
// "kebab-case", "" (no rule).
type Naming struct {
	Files         string            `json:"files,omitempty"`
	ExportedFuncs string            `json:"exported_funcs,omitempty"`
	LocalFuncs    string            `json:"local_funcs,omitempty"`
	Types         string            `json:"types,omitempty"`
	Receivers     string            `json:"receivers,omitempty"` // "short" (1 to 3 letters) or ""
	TypeSuffixes  map[string]string `json:"type_suffixes,omitempty"`
	// TypeSuffixes maps a layer ("service", "handler", "repo") to the suffix
	// an exported struct in that layer must carry, e.g. "service": "Service".
}

// Structure rules, paths relative to the checked root, forward slashes.
type Structure struct {
	Layout        string            `json:"layout,omitempty"` // "layered", "flat", "feature" (prompt only)
	RequiredDirs  []string          `json:"required_dirs,omitempty"`
	ForbiddenDirs []string          `json:"forbidden_dirs,omitempty"`
	FilePattern   map[string]string `json:"file_pattern,omitempty"`
	// FilePattern maps a layer to a glob every Go file in that layer's
	// directory must match, e.g. "service": "*_service.go".
}

// Patterns the team requires or bans. Known ids are listed in KnownPatterns.
type Patterns struct {
	Require []string `json:"require,omitempty"`
	Forbid  []string `json:"forbid,omitempty"`
}

// Code shape rules. Zero means no rule.
type Code struct {
	Paradigm         string   `json:"paradigm,omitempty"`      // "struct-methods", "procedural", "functional" (prompt only)
	Iteration        string   `json:"iteration,omitempty"`     // "range" or "index" (Go); "stream" or "loop" (TS, prompt only)
	MaxFuncLines     int      `json:"max_func_lines,omitempty"`
	MaxBranches      int      `json:"max_branches_per_func,omitempty"`
	MaxParams        int      `json:"max_params,omitempty"`
	ErrorWrapping    string   `json:"error_wrapping,omitempty"` // "wrap" (%w when passing err) or ""
	AllowedImports   []string `json:"allowed_imports,omitempty"`
	ForbiddenImports []string `json:"forbidden_imports,omitempty"`
}

// Prompt settings: what the model is told beyond the checked rules.
type Prompt struct {
	Notes        []string `json:"notes,omitempty"`         // given to the model verbatim, one per line
	Exemplars    int      `json:"exemplars,omitempty"`     // reference functions shown, from ExemplarRoot
	ExemplarRoot string   `json:"exemplar_root,omitempty"` // a repository read through the Understand graph
}

// Gate settings.
type Gate struct {
	Mode       Mode `json:"mode,omitempty"`        // default report
	MaxRepairs int  `json:"max_repairs,omitempty"` // op level style repairs, default 1, capped at 2
}

// Evidence is what Derive saw for one rule.
type Evidence struct {
	Seen  int    `json:"seen"`
	Agree int    `json:"agree"`
	Note  string `json:"note,omitempty"`
}

// KnownPatterns are the pattern ids the gate can check. Anything else in
// Require or Forbid is rejected by Validate rather than silently ignored.
var KnownPatterns = map[string]string{
	"constructor":    "every exported struct in a layer directory has a New<Type> function",
	"repository":     "an internal/repository directory exists with at least one exported type",
	"service-layer":  "an internal/service directory exists with at least one exported type",
	"error-wrapping": "fmt.Errorf that passes an error uses %w",
	"panic":          "no panic( call outside tests",
	"global-state":   "no mutable package level var outside tests",
	"init-func":      "no func init()",
	"reflection":     "no import of reflect",
}

var knownCases = map[string]bool{"": true, "PascalCase": true, "camelCase": true, "snake_case": true, "kebab-case": true}

// Load reads a profile from a JSON file and validates it.
func Load(path string) (*Profile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Profile
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("style profile %s: %w", path, err)
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("style profile %s: %w", path, err)
	}
	return &p, nil
}

// Save writes the profile as indented JSON with stable key order.
func (p *Profile) Save(path string) error {
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

// Validate rejects values the gate would not know what to do with.
func (p *Profile) Validate() error {
	if p.Version == 0 {
		p.Version = 1
	}
	if p.Version != 1 {
		return fmt.Errorf("unsupported version %d", p.Version)
	}
	for field, v := range map[string]string{
		"naming.files": p.Naming.Files, "naming.exported_funcs": p.Naming.ExportedFuncs,
		"naming.local_funcs": p.Naming.LocalFuncs, "naming.types": p.Naming.Types,
	} {
		if !knownCases[v] {
			return fmt.Errorf("%s: unknown case style %q", field, v)
		}
	}
	if p.Naming.Receivers != "" && p.Naming.Receivers != "short" {
		return fmt.Errorf("naming.receivers: %q (want \"short\" or empty)", p.Naming.Receivers)
	}
	for _, id := range append(append([]string{}, p.Patterns.Require...), p.Patterns.Forbid...) {
		if _, ok := KnownPatterns[id]; !ok {
			return fmt.Errorf("patterns: unknown id %q", id)
		}
	}
	for _, id := range p.Patterns.Require {
		for _, f := range p.Patterns.Forbid {
			if id == f {
				return fmt.Errorf("patterns: %q is both required and forbidden", id)
			}
		}
	}
	switch p.Code.ErrorWrapping {
	case "", "wrap":
	default:
		return fmt.Errorf("code.error_wrapping: %q (want \"wrap\" or empty)", p.Code.ErrorWrapping)
	}
	switch p.Code.Iteration {
	case "", "range", "index", "stream", "loop":
	default:
		return fmt.Errorf("code.iteration: %q", p.Code.Iteration)
	}
	switch p.Gate.Mode {
	case "", ModeOff, ModeReport, ModeGate, ModeAutofix:
	default:
		return fmt.Errorf("gate.mode: %q", p.Gate.Mode)
	}
	if p.Gate.MaxRepairs < 0 || p.Gate.MaxRepairs > 2 {
		return fmt.Errorf("gate.max_repairs: %d (0 to 2)", p.Gate.MaxRepairs)
	}
	for id, sev := range p.Severity {
		if sev != SevError && sev != SevWarn {
			return fmt.Errorf("severity[%s]: %q", id, sev)
		}
	}
	return nil
}

// EffectiveMode returns the mode with the default applied.
func (p *Profile) EffectiveMode() Mode {
	if p == nil || p.Gate.Mode == "" {
		if p == nil {
			return ModeOff
		}
		return ModeReport
	}
	return p.Gate.Mode
}

// MaxRepairs returns the op level style repair cap with the default applied.
// It never exceeds 2, the engine's own compile repair cap, so a profile
// cannot loosen or lengthen the existing gate.
func (p *Profile) MaxRepairs() int {
	if p == nil {
		return 0
	}
	if p.Gate.MaxRepairs == 0 {
		return 1
	}
	return p.Gate.MaxRepairs
}

// defaultSeverity is the severity a rule has unless the profile overrides it.
// Correctness adjacent rules (bans, imports, structure) default to error;
// shape and naming rules default to warn.
func defaultSeverity(rule string) Severity {
	switch {
	case strings.HasPrefix(rule, "patterns.forbid."),
		strings.HasPrefix(rule, "structure."),
		rule == "code.forbidden_imports",
		rule == "code.allowed_imports":
		return SevError
	}
	return SevWarn
}

// SeverityOf returns the severity for a rule id.
func (p *Profile) SeverityOf(rule string) Severity {
	if p != nil && p.Severity != nil {
		if s, ok := p.Severity[rule]; ok {
			return s
		}
	}
	return defaultSeverity(rule)
}

// ActiveRules lists the rule ids this profile turns on, sorted. The gate and
// the prompt both walk this, so what the model is told and what is checked
// cannot drift apart.
func (p *Profile) ActiveRules() []string {
	if p == nil {
		return nil
	}
	var out []string
	add := func(on bool, id string) {
		if on {
			out = append(out, id)
		}
	}
	add(p.Naming.Files != "", "naming.files")
	add(p.Naming.ExportedFuncs != "", "naming.exported_funcs")
	add(p.Naming.LocalFuncs != "", "naming.local_funcs")
	add(p.Naming.Types != "", "naming.types")
	add(p.Naming.Receivers != "", "naming.receivers")
	add(len(p.Naming.TypeSuffixes) > 0, "naming.type_suffixes")
	add(len(p.Structure.RequiredDirs) > 0, "structure.required_dirs")
	add(len(p.Structure.ForbiddenDirs) > 0, "structure.forbidden_dirs")
	add(len(p.Structure.FilePattern) > 0, "structure.file_pattern")
	for _, id := range p.Patterns.Require {
		out = append(out, "patterns.require."+id)
	}
	for _, id := range p.Patterns.Forbid {
		out = append(out, "patterns.forbid."+id)
	}
	add(p.Code.MaxFuncLines > 0, "code.max_func_lines")
	add(p.Code.MaxBranches > 0, "code.max_branches_per_func")
	add(p.Code.MaxParams > 0, "code.max_params")
	add(p.Code.ErrorWrapping == "wrap", "code.error_wrapping")
	add(p.Code.Iteration == "range" || p.Code.Iteration == "index", "code.iteration")
	add(len(p.Code.AllowedImports) > 0, "code.allowed_imports")
	add(len(p.Code.ForbiddenImports) > 0, "code.forbidden_imports")
	sort.Strings(out)
	return out
}

// Default is a reasonable Go profile for the backends gen-system emits. It is
// the profile `gen style print` shows, and the one a team edits first.
func Default() *Profile {
	return &Profile{
		Version: 1,
		Name:    "gen-system default (Go backend)",
		Source:  "human",
		Naming: Naming{
			Files:         "snake_case",
			ExportedFuncs: "PascalCase",
			LocalFuncs:    "camelCase",
			Types:         "PascalCase",
			Receivers:     "short",
			TypeSuffixes:  map[string]string{"service": "Service", "handler": "Handler"},
		},
		Structure: Structure{
			Layout:       "layered",
			RequiredDirs: []string{"internal/model", "internal/service", "internal/handler"},
			FilePattern:  map[string]string{"service": "*_service*.go", "handler": "*_handler.go"},
		},
		Patterns: Patterns{
			Require: []string{"constructor", "error-wrapping"},
			Forbid:  []string{"panic", "init-func", "reflection"},
		},
		Code: Code{
			Paradigm:      "struct-methods",
			Iteration:     "range",
			MaxFuncLines:  60,
			MaxBranches:   12,
			MaxParams:     5,
			ErrorWrapping: "wrap",
		},
		Prompt: Prompt{
			Notes: []string{
				"Return early on error; do not nest the happy path.",
				"Name booleans as questions: isPaid, hasStock.",
			},
			Exemplars: 2,
		},
		Gate: Gate{Mode: ModeReport, MaxRepairs: 1},
	}
}
