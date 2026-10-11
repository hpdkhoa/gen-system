// generator/internal/belief/summarize.go, v26.16 (session 10): hierarchical
// summaries. A summary belief describes a scope (a service, a module/package, or
// the whole project) by rolling up the beliefs of its members, not raw source.
// That keeps every summary prompt small (a handful of one-line child claims) and
// makes cascading staleness cheap: each summary fingerprints its members, so a
// change anywhere below re-flags the summaries above it.
//
// Scope levels:
//
//	service: members are the routine beliefs whose Subject shares a receiver
//	         (OrderService.Checkout, OrderService.Refund → service "OrderService").
//	module:  members are service summaries in the same package/layer (the caller,
//	         which has the index, supplies the grouping via UpsertSummary members).
//	project: members are the module summaries.
//
// The belief package can derive service membership itself from qualified names.
// Module/project grouping needs package/layer info the beliefs don't carry, so the
// caller (gen-enrich, which has the index) passes explicit member IDs for those.
package belief

import (
	"context"
	"sort"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/trace"
)

// Scope levels for summary beliefs.
const (
	ScopeService = "service"
	ScopeModule  = "module"
	ScopeProject = "project"
)

// summaryID builds a stable ID for a scope summary.
func summaryID(scope, name string) string {
	return "summary:" + scope + ":" + name
}

// ServiceOf returns the receiver/service part of a qualified routine name:
// "OrderService.Checkout" → "OrderService". Returns "" if there is no receiver.
func ServiceOf(qualName string) string {
	i := strings.LastIndex(qualName, ".")
	if i <= 0 {
		return ""
	}
	return qualName[:i]
}

// ServiceMembers returns, per scope name, the IDs of the routine beliefs that
// belong to it. Grouping uses the Group stamped at derive time (v26.21): the
// receiver for OO languages, the PROGRAM-ID/module for flat ones like COBOL. It
// falls back to splitting the qualified name for any belief without a Group (e.g.
// an enrich belief persisted by an older version).
// Only non-summary beliefs are considered (a summary is not a member of itself).
func (bs *Store) ServiceMembers() map[string][]string {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	out := map[string][]string{}
	for id, b := range bs.beliefs {
		if b.Kind == KindSummary {
			continue
		}
		grp := b.Group
		if grp == "" {
			grp = ServiceOf(b.Subject)
		}
		if grp == "" {
			continue
		}
		out[grp] = append(out[grp], id)
	}
	for svc := range out {
		sort.Strings(out[svc])
	}
	return out
}

// ScopeMemberSummary is one member's one-line description handed to the summarizer.
type ScopeMemberSummary struct {
	Subject string
	Claim   string
}

// MembersForSummary returns the one-line member descriptions for a set of belief
// IDs, preferring the most human-relevant kind per subject (human correction >
// enrich action > parser action). Used to build a summarizer prompt from beliefs
// rather than source.
func (bs *Store) MembersForSummary(memberIDs []string) []ScopeMemberSummary {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	// Group by subject, pick the best claim.
	best := map[string]*Belief{}
	rank := func(b *Belief) int {
		switch {
		case b.Source == SourceHuman:
			return 3
		case b.Source == SourceEnrich && b.Kind == KindAction:
			return 2
		case b.Kind == KindAction:
			return 1
		default:
			return 0
		}
	}
	for _, id := range memberIDs {
		b := bs.beliefs[id]
		if b == nil || b.Kind == KindSummary {
			continue
		}
		cur, ok := best[b.Subject]
		if !ok || rank(b) > rank(cur) {
			best[b.Subject] = b
		}
	}
	subjects := make([]string, 0, len(best))
	for s := range best {
		subjects = append(subjects, s)
	}
	sort.Strings(subjects)
	out := make([]ScopeMemberSummary, 0, len(subjects))
	for _, s := range subjects {
		b := best[s]
		claim := b.Object
		if b.CorrectedTo != "" {
			claim = b.CorrectedTo
		}
		out = append(out, ScopeMemberSummary{Subject: s, Claim: claim})
	}
	return out
}

// SummaryInput is one scope summary the producer wants to store.
type SummaryInput struct {
	Scope     string   // ScopeService / ScopeModule / ScopeProject
	Name      string   // scope name (service/package/project name)
	Object    string   // the rolled-up prose
	MemberIDs []string // beliefs summarized (for the cascade fingerprint)
	Parent    string   // enclosing scope name (service->module, module->project)
}

// UpsertSummary stores (or replaces) a scope summary, fingerprinting its members
// so a later change below cascades staleness. Persisted immediately.
//
// v26.58 (D020): returns the save error, so a lost write is not reported as done.
func (bs *Store) UpsertSummary(in SummaryInput) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	fp := bs.childFingerprintLocked(in.MemberIDs)
	id := summaryID(in.Scope, in.Name)
	bs.summaries[id] = &Belief{
		ID:               id,
		Kind:             KindSummary,
		Subject:          in.Name,
		Predicate:        "summarizes",
		Object:           in.Object,
		Source:           SourceEnrich,
		Scope:            in.Scope,
		ChildFingerprint: fp,
		MemberIDs:        append([]string(nil), in.MemberIDs...),
		Parent:           in.Parent,
	}
	return bs.saveLocked()
}

// reattachSummariesLocked folds persisted summaries into the live belief set,
// recomputing each one's child fingerprint and flagging it stale on a mismatch
// (the cascade). Caller holds mu. Must run after parser + enrich beliefs are in
// bs.beliefs so the fingerprint sees current member values.
func (bs *Store) reattachSummariesLocked() {
	// Process bottom-up (service -> module -> project), not in map order (v26.22).
	// A module's fingerprint is computed over its service summaries, so those must
	// already be attached and up to date; iterating the map at random could
	// fingerprint a parent before its children, making the cascade order-dependent.
	for _, scope := range []string{ScopeService, ScopeModule, ScopeProject} {
		for id, sm := range bs.summaries {
			if sm.Scope != scope {
				continue
			}
			members := bs.membersForScopeLocked(sm)
			cur := bs.childFingerprintLocked(members)
			// v26.58 (D019): Stale was only ever set to true, so a summary stayed
			// stale after its members returned to the summarized values. Compute it
			// fresh: clear first, then set when the fingerprint differs.
			resetSummaryStale(sm)
			if sm.ChildFingerprint != "" && cur != sm.ChildFingerprint {
				sm.Stale = true
				if sm.ValidationNote == "" {
					sm.ValidationNote = staleSummaryNote
				}
			}
			bs.applyCorrectionLocked(sm) // a human may correct a summary too
			bs.beliefs[id] = sm
		}
	}
	// Any summary with an unrecognised scope still gets attached.
	for id, sm := range bs.summaries {
		if _, done := bs.beliefs[id]; !done {
			resetSummaryStale(sm) // v26.58 (D019): only a correction can make it stale here
			bs.applyCorrectionLocked(sm)
			bs.beliefs[id] = sm
		}
	}
}

// staleSummaryNote is the note a summary carries while a member differs from the
// value it summarized.
const staleSummaryNote = "a member changed since this summary was written, re-summarize"

// resetSummaryStale clears the stale state a previous rebuild left on a summary,
// so each rebuild decides it again (v26.58, D019).
func resetSummaryStale(sm *Belief) {
	sm.Stale = false
	sm.SourceNow = ""
	if sm.ValidationNote == staleSummaryNote {
		sm.ValidationNote = ""
	}
}

// membersForScopeLocked recovers the current member IDs for a summary. For a
// service scope it re-derives from qualified names (so it tracks member add/remove);
// for module/project it uses the member set stored at upsert time (recomputed
// against current values, so a child's content change still cascades; a child's
// add/remove for those scopes requires a re-summarize with fresh members).
func (bs *Store) membersForScopeLocked(sm *Belief) []string {
	if sm.Scope == ScopeService {
		var ids []string
		for id, b := range bs.beliefs {
			if b.Kind == KindSummary {
				continue
			}
			grp := b.Group
			if grp == "" {
				grp = ServiceOf(b.Subject)
			}
			if grp == sm.Subject {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		return ids
	}
	// Module / project (v26.22): re-derive membership from the summary hierarchy so
	// these scopes cascade when a child is added or removed, not merely when a
	// child's content changes. A module's members are the service summaries whose
	// Parent names it; the project's members are the module summaries.
	//
	// Previously this replayed the stored MemberIDs, which meant a brand-new service
	// (or a deleted one) left the module summary looking fresh. That was the one
	// hierarchy gap called out in the v26.16 notes.
	childScope := ""
	switch sm.Scope {
	case ScopeModule:
		childScope = ScopeService
	case ScopeProject:
		childScope = ScopeModule
	default:
		return append([]string(nil), sm.MemberIDs...)
	}
	var ids []string
	for id, b := range bs.summaries {
		if b.Scope != childScope {
			continue
		}
		// Project scope collects every module; module scope collects only its own.
		if sm.Scope == ScopeProject || b.Parent == sm.Subject {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		// No children re-derivable (e.g. a module summarised before its services
		// existed): fall back to the stored set rather than reporting "no members",
		// which would wrongly fingerprint as empty and flag a false change.
		return append([]string(nil), sm.MemberIDs...)
	}
	sort.Strings(ids)
	return ids
}

// childFingerprintLocked hashes the id+value of each member belief so any add,
// remove, or content change moves the fingerprint. Caller holds mu. A nil slice
// yields "" (used for scopes with no re-derivable members).
func (bs *Store) childFingerprintLocked(memberIDs []string) string {
	if memberIDs == nil {
		return ""
	}
	ids := append([]string(nil), memberIDs...)
	sort.Strings(ids)
	var sb strings.Builder
	for _, id := range ids {
		b := bs.beliefs[id]
		if b == nil {
			// Members may be summary beliefs (module/project scopes) that live in
			// bs.summaries; check there before declaring a member gone (v26.22).
			b = bs.summaries[id]
		}
		if b == nil {
			sb.WriteString(id + "=<gone>;")
			continue
		}
		val := b.Object
		if b.CorrectedTo != "" {
			val = b.CorrectedTo
		}
		sb.WriteString(id + "=" + val + ";")
	}
	return hashSource(sb.String())
}

// DeleteSummary removes a scope summary (v26.22). Needed when a service or module
// disappears from the codebase. Without it a stale scope would linger forever, and
// its parent could never notice the child was gone.
func (bs *Store) DeleteSummary(scope, name string) {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	id := summaryID(scope, name)
	delete(bs.summaries, id)
	delete(bs.beliefs, id)
	if err := bs.saveLocked(); err != nil { // v26.58 (D020): log, do not hide
		trace.Error(context.Background(), "belief store save failed", "path", bs.diskPath, "err", err)
	}
}

// Summaries returns all scope summaries, scope- then name-sorted.
func (bs *Store) Summaries() []Belief {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	out := make([]Belief, 0, len(bs.summaries))
	for _, b := range bs.summaries {
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Scope != out[j].Scope {
			return out[i].Scope < out[j].Scope
		}
		return out[i].Subject < out[j].Subject
	})
	return out
}
