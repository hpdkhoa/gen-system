// continue_svc.go (v25.8): server-side `continue` (delta against existing source)
// with a review gate the Studio can drive (Approve / Reject), the server analog
// of the CLI consent gate.
//
// Flow:
//  1. Plan(idea, target): load the target, summarize what exists, plan the
//     delta, detect conflicts (planner_v2.DetectConflicts), and publish a
//     "continue_review" SSE with existing-purpose + delta + conflicts. The
//     service holds the pending plan (not generated yet).
//  2. The UI shows the review and calls Approve() or Reject().
//     Approve → generate the held plan (compile-gate + repair as usual).
//     Reject  → discard, nothing staged.
//     With autoApprove=true, step 1 proceeds straight to generation (scripted/CI).
//     Otherwise it always holds for Approve/Reject. The review blocks generation.
//
// Reuses the exact conflict rules the CLI gate uses (planner_v2), so CLI and
// Studio push back identically. Lives in services (generation surface);
// understand-server never links it.
package services

import (
	"fmt"
	"time"

	"github.com/hpdkhoa/gen-system/generator/internal/agent"
	"github.com/hpdkhoa/gen-system/generator/internal/belief"
	gencontext "github.com/hpdkhoa/gen-system/generator/internal/context"
	"github.com/hpdkhoa/gen-system/generator/internal/planner_v2"
)

// PendingContinue holds a planned-but-unapproved continuation between the review
// and the approve/reject call.
type PendingContinue struct {
	Idea      string
	Target    string
	Result    planner_v2.ContinueResult
	Existing  planner_v2.ExistingState
	Conflicts []planner_v2.Conflict
	CreatedAt int64
}

// ContinueReview is the SSE payload the Studio renders for Approve/Reject.
type ContinueReview struct {
	Idea      string                `json:"idea"`
	Target    string                `json:"target"`
	Summary   string                `json:"summary"`
	Existing  []ReviewEntity        `json:"existing"`
	Changed   []string              `json:"changed"`
	NewOps    map[string][]string   `json:"new_ops"`
	Conflicts []planner_v2.Conflict `json:"conflicts"`
	AutoOK    bool                  `json:"auto_ok"` // true → no conflicts, safe to proceed
}

// ReviewEntity is one existing entity in the review.
type ReviewEntity struct {
	Name     string   `json:"name"`
	Fields   int      `json:"fields"`
	Ops      []string `json:"ops"`
	ReadOnly bool     `json:"read_only"`
}

// StartContinue plans a continuation and either proceeds (autoApprove or no
// conflicts) or publishes a review and holds. target empty → server root.
func (s *BuildService) StartContinue(idea, outDir string, autoApprove bool) (*BuildJob, error) {
	s.mu.Lock()
	if s.job != nil && s.job.Status == JobRunning {
		s.mu.Unlock()
		return nil, fmt.Errorf("build already running")
	}
	// v25.8.1: a fresh continuation must never inherit a stale held plan from a
	// previous review that was neither approved nor rejected.
	s.pending = nil
	target := outDir
	if target == "" {
		target = s.root
	}
	job := &BuildJob{
		ID:        fmt.Sprintf("continue-%d", time.Now().UnixMilli()),
		Goal:      "continue: " + idea,
		Status:    JobRunning,
		StartedAt: time.Now().UnixMilli(),
	}
	s.job = job
	s.mu.Unlock()

	s.wireAgentHooks()
	go s.runContinue(job, idea, target, autoApprove)
	return job, nil
}

// runContinue does the plan + review; on auto/approve it generates.
func (s *BuildService) runContinue(job *BuildJob, idea, target string, autoApprove bool) {
	s.broker.Publish("build_started", job)
	emit := func(dot, text string) {
		s.broker.Publish("agent_event", map[string]string{"phase": dot, "dot": dot, "text": text})
	}

	emit("cyan", "Understanding existing code at "+target)
	ctx := gencontext.LoadContext(target)
	existing := planner_v2.SummarizeExisting(&ctx)
	existingSum := planner_v2.RenderExistingSummary(existing)

	// Steer the delta with human belief corrections for this project (session 5):
	// a person's corrections to the machine's understanding are appended to the
	// "what exists" context so the planner honors them. No-op when absent.
	if cc := belief.BuildCorrectionContext(ctx.Root, ctx.Index, ctx.Effects); cc != "" {
		existingSum += "\n\n" + cc
		emit("cyan", "Applied human belief corrections to the plan context")
	}

	existingEntities := planner_v2.RecoverEntitiesFromModels(ctx.Root)
	if len(existingEntities) == 0 {
		// Fall back to IR-derived entities when no model structs are recovered.
		existingEntities = irToSchemaEntities(ctx.IR)
	}

	emit("cyan", "Planning the delta (model call)…")
	res, err := planner_v2.BuildContinuePlan(existingEntities, existingSum, idea, target, false)
	if err != nil {
		s.fail(job, "delta planning failed: "+err.Error())
		return
	}
	if len(res.ChangedSet) == 0 {
		s.mu.Lock()
		job.Status = JobDone
		job.DoneAt = time.Now().UnixMilli()
		s.mu.Unlock()
		emit("green", "Nothing to do. The existing code already satisfies the idea.")
		s.broker.Publish("build_complete", job)
		return
	}

	conflicts := planner_v2.DetectConflicts(existing, res)
	review := buildContinueReview(idea, target, existing, res, conflicts)

	// Always publish the review so the UI can show what will happen.
	s.broker.Publish("continue_review", review)

	// v25.8.2: always hold for the user's decision. The review is a gate that
	// runs before generation. Nothing generates until Approve arrives. The only
	// bypass is an explicit autoApprove (e.g. scripted/CI callers that opt in).
	if !autoApprove {
		s.mu.Lock()
		s.pending = &PendingContinue{
			Idea: idea, Target: target, Result: res,
			Existing: existing, Conflicts: conflicts,
			CreatedAt: time.Now().UnixMilli(),
		}
		s.mu.Unlock()
		if len(conflicts) > 0 {
			emit("amber", fmt.Sprintf("Review ready: %d conflict(s) to confirm. Waiting for your approval.", len(conflicts)))
		} else {
			emit("cyan", "Review ready. Waiting for your approval before generating.")
		}
		return
	}

	s.generateContinue(job, res, target)
}

// ApproveContinue generates the held plan. Returns an error if none is pending.
func (s *BuildService) ApproveContinue() error {
	s.mu.Lock()
	p := s.pending
	job := s.job
	s.pending = nil
	s.mu.Unlock()
	if p == nil {
		return fmt.Errorf("no continuation is awaiting approval")
	}
	if job == nil {
		job = &BuildJob{ID: fmt.Sprintf("continue-%d", time.Now().UnixMilli()), Status: JobRunning, StartedAt: time.Now().UnixMilli()}
		s.mu.Lock()
		s.job = job
		s.mu.Unlock()
	}
	s.broker.Publish("agent_event", map[string]string{"phase": "green", "dot": "green", "text": "Approved. Generating."})
	go s.generateContinue(job, p.Result, p.Target)
	return nil
}

// RejectContinue discards a held continuation.
func (s *BuildService) RejectContinue() error {
	s.mu.Lock()
	p := s.pending
	if p == nil {
		// v26.58 (D089): the old code failed s.job before this test, so a
		// reject with no review pending marked a RUNNING build failed while its
		// goroutine still wrote files, and let a second run start. With no
		// pending plan there is nothing to reject: leave the job alone.
		s.mu.Unlock()
		return fmt.Errorf("no continuation is awaiting approval")
	}
	s.pending = nil
	if s.job != nil {
		s.job.Status = JobFailed
		s.job.Error = "rejected by user at review"
		s.job.DoneAt = time.Now().UnixMilli()
	}
	s.mu.Unlock()
	s.broker.Publish("agent_event", map[string]string{"phase": "red", "dot": "red", "text": "Rejected. Nothing was generated."})
	s.broker.Publish("continue_rejected", map[string]string{"idea": p.Idea})
	return nil
}

// generateContinue runs the agent on the delta plan and finishes the job.
func (s *BuildService) generateContinue(job *BuildJob, res planner_v2.ContinueResult, target string) {
	emit := func(dot, text string) {
		s.broker.Publish("agent_event", map[string]string{"phase": dot, "dot": dot, "text": text})
	}
	emit("cyan", fmt.Sprintf("Running agent (%d entities, %d tasks)…", len(res.Plan.Entities), len(res.Plan.Tasks)))

	if err := agent.RunFromPlan(res.Plan, target); err != nil {
		s.fail(job, "continuation failed: "+err.Error())
		return
	}

	s.mu.Lock()
	job.Status = JobDone
	job.DoneAt = time.Now().UnixMilli()
	job.OutputPath = target
	job.Entities = len(res.Merged.Entities)
	s.mu.Unlock()

	s.broker.Publish("build_complete", job)
	emit("green", "Continuation complete.")

	if s.snapSvc != nil {
		_, _ = s.snapSvc.Create("Continue", "continued from existing code", "agent", "default", false)
	}
}

// buildContinueReview assembles the SSE review payload.
func buildContinueReview(idea, target string, existing planner_v2.ExistingState, res planner_v2.ContinueResult, conflicts []planner_v2.Conflict) ContinueReview {
	rev := ContinueReview{
		Idea: idea, Target: target, Summary: res.Summary,
		NewOps: res.NewOps, Conflicts: conflicts,
		AutoOK: len(conflicts) == 0,
	}
	for _, name := range existing.EntityOrder {
		ops := existing.Ops[name]
		rev.Existing = append(rev.Existing, ReviewEntity{
			Name: name, Fields: len(existing.Entities[name]), Ops: ops,
			ReadOnly: planner_v2.EntityIsReadDominant(ops),
		})
	}
	for name, ch := range res.ChangedSet {
		if ch {
			rev.Changed = append(rev.Changed, name)
		}
	}
	return rev
}
