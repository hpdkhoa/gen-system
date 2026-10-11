// verify_understand.go, v25.5 (step 2): the Understand track validates the
// Generate track's output.
//
// After the Go compile/vet/test loop passes, this runs the generated backend
// through gen-system's own comprehension engine (context.LoadContext) and asserts
// properties `go build` cannot see:
//
//  1. RESOLUTION: every call in the generated code resolves to a real symbol.
//     go build already guarantees this for Go, but the number is the first real
//     internal benchmark and the identical check that matters for non-Go source.
//  2. ORPHAN OPERATIONS: a handler/test references a service method that is not
//     in the index (the v23.24 "continue dropped Checkout" class of bug, caught
//     structurally rather than after it ships).
//  3. EFFECT SANITY: a read-only operation (Get/List/Find/…) whose inferred
//     effects include a DB write is a semantic bug go build waves through.
//
// GATING. VerifyMode controls what happens on a finding:
//   - VerifyOff (default when RunFromPlan is called directly): skip entirely.
//   - VerifyStop: report findings, do not modify anything, return them so the
//     caller can leave staged output for human review + write a findings report.
//   - VerifyAutofix: route each fixable finding through the existing repair loop
//     (dispatchRepairs), then re-report.
//
// Both modes always report to the human (emit + structured record), so a human
// sees the findings before the system acts on them. That is a hard constraint.
//
// BOUNDARY. This lives in the agent (generation) package and uses agentctx,
// which agent already imports. understand-server never links agent, so the
// archtest boundary is unaffected.
package agent

import (
	"fmt"
	"sort"
	"strings"

	agentctx "github.com/hpdkhoa/gen-system/generator/internal/context"
	"github.com/hpdkhoa/gen-system/generator/internal/flow"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

// VerifyMode selects the Understand-validation behaviour.
type VerifyMode int

const (
	VerifyOff VerifyMode = iota
	VerifyStop
	VerifyAutofix
)

// VerifyUnderstandMode is a package-level hook the CLI/server sets before
// RunFromPlan (like EventHook / HealObserver). Default VerifyOff keeps existing
// callers byte-identical.
var VerifyUnderstandMode = VerifyOff

// LastVerifyReport holds the most recent Understand-verify result so the CLI can
// write a findings report after RunFromPlan returns. verifyHalted is true when
// the last run stopped for human review (VerifyStop with findings).
var (
	LastVerifyReport VerifyReport
	verifyHalted     bool
)

// VerifyHalted reports whether the last RunFromPlan stopped for human review.
func VerifyHalted() bool { return verifyHalted }

// ParseVerifyMode maps the --verify-understand flag value to a VerifyMode.
// "" / "off" → Off; "stop" → Stop; "autofix"/"heal" → Autofix.
//
// v26.58 (D021): an unknown word used to give VerifyOff with no message, so a
// typo such as "sotp" turned the gate off while the user thought it ran. Off
// is now named explicitly and every other word is an error the CLI reports.
func ParseVerifyMode(s string) (VerifyMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "off":
		return VerifyOff, nil
	case "stop":
		return VerifyStop, nil
	case "autofix", "heal", "fix":
		return VerifyAutofix, nil
	default:
		return VerifyOff, fmt.Errorf("unknown --verify-understand value %q (want off, stop or autofix)", s)
	}
}

// VerifyFinding is one problem the Understand pass found.
type VerifyFinding struct {
	Kind   string // "unresolved" | "orphan" | "effect"
	Symbol string // the offending symbol or call site
	File   string // file it lives in (for repair routing)
	Detail string // human explanation
}

// VerifyReport is the full result of one Understand validation pass.
//
// v26.34: the population is split the way bench/resolution.go splits it.
// TotalEdges counts internal edges only: calls from a project caller to a
// name some project symbol defines. ExternalEdges counts calls to names no
// project symbol defines (stdlib, third party, methods on stdlib types),
// whether or not the stdlib pass resolved them. LiteralEdges counts anonymous
// functions called in place. Neither of the last two is a defect, and neither
// sits in the denominator of ResolutionPct any more.
type VerifyReport struct {
	ResolvedEdges   int
	UnresolvedEdges int
	TotalEdges      int
	ExternalEdges   int
	LiteralEdges    int
	Findings        []VerifyFinding
}

// ResolutionPct is the project-only resolution recall as a percentage.
func (r VerifyReport) ResolutionPct() float64 {
	if r.TotalEdges == 0 {
		return 100
	}
	return 100 * float64(r.ResolvedEdges) / float64(r.TotalEdges)
}

// readOpPrefixes are the operation-name prefixes treated as read-only. A method
// whose name starts with one of these should not carry a DB-write effect. Mirrors
// the read-detection used when forcing POST vs GET in service_logic.
var readOpPrefixes = []string{"get", "list", "find", "fetch", "search", "query", "view"}

// runUnderstandVerify loads the generated backend through the Understand engine
// and builds a VerifyReport. It never modifies anything; acting on the findings
// is the caller's job (per VerifyMode).
func runUnderstandVerify(backendPath string) VerifyReport {
	uctx := agentctx.LoadContextWithOpts(backendPath, LoadOpts)
	rep := VerifyReport{}
	if uctx.Index == nil {
		return rep
	}

	// ── Check 1: resolution recall over internal project call edges ──
	//
	// v26.34: before this, every call from a project function counted, and a
	// callee was "a project defect" unless it was one of 16 builtin names. On a
	// generated backend that meant 234 of 287 edges were stdlib calls
	// (errors.New, http.Error, t.Fatalf, s.mu.Lock) reported as unresolved
	// project symbols, an 18.5 percent "recall", and a halt in stop mode over
	// nothing. The rule is now the benchmark's: an edge is internal when its
	// bare callee name is one some project symbol defines. Everything else is
	// external and is counted, not reported. A dropped service operation is
	// still caught: by the orphan check below, and by the compile gate before
	// it. The known limit: a call to a project receiver's method that no
	// project symbol defines at all is classified external here.
	classifier := uctx.Index.NewEdgeClassifier()
	edges := uctx.Index.AllCallEdges()
	for _, e := range edges {
		// Gate on caller provenance so the number stays project-only (the same
		// provenance rule the stdlib metric uses).
		caller, ok := uctx.Index.ByID(e.CallerID)
		if ok && caller.Provenance != symbols.ProvProject {
			continue
		}
		class, toProject := classifier.Classify(e)
		switch class {
		case symbols.EdgeLiteral:
			rep.LiteralEdges++
			continue
		case symbols.EdgeExternal:
			rep.ExternalEdges++
			continue
		}
		rep.TotalEdges++
		if toProject {
			rep.ResolvedEdges++
			continue
		}
		rep.UnresolvedEdges++
		if ok {
			rep.Findings = append(rep.Findings, VerifyFinding{
				Kind:   "unresolved",
				Symbol: e.CalleeName,
				File:   caller.DefinedIn,
				Detail: fmt.Sprintf("call to %q from %s does not resolve to any project symbol", e.CalleeName, caller.Name),
			})
		}
	}

	// ── Check 2: orphan operations: a service method referenced but absent ──
	// Build the set of defined service method names, then look for references to
	// <Entity>Service.<Op> whose target is not defined. We approximate via the
	// index: any call whose CalleeName matches a *Service method shape but has no
	// CalleeID and whose base type is a known service is an orphan.
	definedMethods := map[string]bool{}
	for _, s := range uctx.Index.AllSymbols() {
		if s.Kind == symbols.SymMethod && s.ReceiverType != "" {
			definedMethods[s.ReceiverType+"."+s.Name] = true
		}
	}
	for _, e := range edges {
		if e.CalleeID != "" || e.CalleeName == "" {
			continue
		}
		// Heuristic: dotted call on a *Service receiver.
		if strings.Contains(e.CalleeName, "Service.") && !definedMethods[strings.TrimPrefix(e.CalleeName, "s.")] {
			caller, ok := uctx.Index.ByID(e.CallerID)
			file := ""
			if ok {
				file = caller.DefinedIn
			}
			rep.Findings = append(rep.Findings, VerifyFinding{
				Kind:   "orphan",
				Symbol: e.CalleeName,
				File:   file,
				Detail: fmt.Sprintf("service operation %q is referenced but not defined; an earlier step may have dropped the op", e.CalleeName),
			})
		}
	}

	// ── Check 3: effect sanity: a read-op that writes to the DB ──
	for _, s := range uctx.Index.AllSymbols() {
		if s.Kind != symbols.SymMethod || s.Provenance != symbols.ProvProject {
			continue
		}
		if !isReadOpName(s.Name) {
			continue
		}
		fe := uctx.Effects[s.ID]
		if fe == nil || fe.Effects == nil {
			continue
		}
		// v26.58 (D100): test the write, not any DB access. EffectDB is also
		// set for a SELECT through Query or QueryRow, so every correct Get and
		// List of a PG store was reported as a read that writes.
		if fe.Effects[flow.EffectDB] && fe.DBWrite {
			rep.Findings = append(rep.Findings, VerifyFinding{
				Kind:   "effect",
				Symbol: s.ReceiverType + "." + s.Name,
				File:   s.DefinedIn,
				Detail: fmt.Sprintf("read-style operation %s.%s has an inferred DB-WRITE effect; a read should not mutate", s.ReceiverType, s.Name),
			})
		}
	}

	// Stable order so reports/diffs are deterministic.
	sort.Slice(rep.Findings, func(i, j int) bool {
		if rep.Findings[i].Kind != rep.Findings[j].Kind {
			return rep.Findings[i].Kind < rep.Findings[j].Kind
		}
		return rep.Findings[i].Symbol < rep.Findings[j].Symbol
	})
	return rep
}

// isReadOpName reports whether an operation name is read-only by prefix.
func isReadOpName(name string) bool {
	l := strings.ToLower(name)
	for _, p := range readOpPrefixes {
		if strings.HasPrefix(l, p) {
			return true
		}
	}
	return false
}

// runVerifyStage runs the Understand pass (when enabled) and acts on it per
// VerifyUnderstandMode. Returns the report (nil-safe) and whether generation
// should halt for human review (VerifyStop with findings). Called from
// RunFromPlan after the validation loop.
func runVerifyStage(backendPath, rootPath string, ctx agentctx.Context) (VerifyReport, bool) {
	if VerifyUnderstandMode == VerifyOff {
		return VerifyReport{}, false
	}
	emit("verify", "\n🔎 Understand pass: checking the generated output with the Understand engine...\n")
	rep := runUnderstandVerify(backendPath)

	emit("verify", fmt.Sprintf("  [verify] resolution: %.1f%% (%d/%d internal call edges resolved; %d external, %d literal excluded)\n",
		rep.ResolutionPct(), rep.ResolvedEdges, rep.TotalEdges, rep.ExternalEdges, rep.LiteralEdges))
	recordLifecycle("verify", fmt.Sprintf("resolution %.1f%% (%d/%d), %d finding(s)",
		rep.ResolutionPct(), rep.ResolvedEdges, rep.TotalEdges, len(rep.Findings)))

	if len(rep.Findings) == 0 {
		emit("verify", "  [verify] ✅ no structural findings; the generated output is internally consistent\n")
		return rep, false
	}

	// Report every finding to the human, always.
	for _, f := range rep.Findings {
		emit("verify", fmt.Sprintf("  [verify] ⚠️  %s: %s\n", strings.ToUpper(f.Kind), f.Detail))
	}

	switch VerifyUnderstandMode {
	case VerifyAutofix:
		emit("verify", "  [verify] autofix mode: fixable findings go to the repair loop\n")
		var files []string
		var evidence strings.Builder
		seen := map[string]bool{}
		for _, f := range rep.Findings {
			if f.File != "" && !seen[f.File] {
				seen[f.File] = true
				files = append(files, f.File)
			}
			if f.File != "" {
				fmt.Fprintf(&evidence, "%s: %s %s: %s\n", f.File, f.Kind, f.Symbol, f.Detail)
			}
		}
		if len(files) > 0 {
			dispatchRepairs(files, evidence.String(), ctx, rootPath, 99) // round 99 = verify-driven
		}
		// Re-run once to report the post-repair state (does not re-heal).
		after := runUnderstandVerify(backendPath)
		emit("verify", fmt.Sprintf("  [verify] after repair: %d finding(s) remain\n", len(after.Findings)))
		recordLifecycle("verify", fmt.Sprintf("autofix done: %d→%d findings", len(rep.Findings), len(after.Findings)))
		return after, false
	default: // VerifyStop
		emit("verify", fmt.Sprintf("  [verify] ⛔ stop mode: %d finding(s); leaving the output for human review\n", len(rep.Findings)))
		recordLifecycle("verify", "stop mode: halted for human review")
		return rep, true
	}
}

// WriteFindingsReport renders a VerifyReport as a plain-text findings file. Used
// by the CLI in stop mode on a staged continue so the human has a written
// artifact alongside the .evolve-review directory.
func WriteFindingsReport(rep VerifyReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "gen-system: Understand verification findings\n")
	fmt.Fprintf(&b, "=============================================\n\n")
	fmt.Fprintf(&b, "Resolution: %.1f%%  (%d/%d internal call edges resolved; %d external, %d literal excluded)\n",
		rep.ResolutionPct(), rep.ResolvedEdges, rep.TotalEdges, rep.ExternalEdges, rep.LiteralEdges)
	fmt.Fprintf(&b, "Findings:   %d\n\n", len(rep.Findings))
	if len(rep.Findings) == 0 {
		fmt.Fprintf(&b, "No structural findings. The generated output is internally consistent.\n")
		return b.String()
	}
	for i, f := range rep.Findings {
		fmt.Fprintf(&b, "%d. [%s] %s\n", i+1, strings.ToUpper(f.Kind), f.Symbol)
		if f.File != "" {
			fmt.Fprintf(&b, "   file:   %s\n", f.File)
		}
		fmt.Fprintf(&b, "   detail: %s\n\n", f.Detail)
	}
	return b.String()
}
