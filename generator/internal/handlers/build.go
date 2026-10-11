// build.go (v20.1): + CleanProject handler (POST /api/clean) for a clean start.
package handlers

import (
	"encoding/json"
	"net/http"
)

// CleanProject handles POST /api/clean. It removes .gen and the generated
// layer directories so the user can start fresh. Returns the CleanResult
// summary (what was removed vs. absent).
func (h *Handlers) CleanProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	res, err := h.build.Clean()
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	writeJSON(w, res)
}

func (h *Handlers) StartBuild(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Goal     string `json:"goal"`
		RootPath string `json:"root_path"`
		OutDir   string `json:"out_dir"` // v25.6: chosen output folder (optional)
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if req.Goal == "" {
		http.Error(w, "goal required", 400)
		return
	}
	// v25.6: if the UI supplied an output folder, validate it (inside
	// projects-base, not the current root) before generating into it. Empty
	// out_dir keeps the legacy behaviour (generate into the server root).
	outDir := ""
	if req.OutDir != "" {
		safe, err := h.build.ValidateOutputDir(req.OutDir)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		outDir = safe
	}
	job, err := h.build.StartInDir(req.Goal, outDir)
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	w.WriteHeader(202)
	writeJSON(w, job)
}

func (h *Handlers) BuildStatus(w http.ResponseWriter, r *http.Request) {
	job := h.build.Current()
	if job == nil {
		writeJSON(w, map[string]string{"status": "idle"})
		return
	}
	writeJSON(w, job)
}

func (h *Handlers) TriggerHeal(w http.ResponseWriter, r *http.Request) {
	h.broker.Publish("agent_event", map[string]string{
		"phase": "heal", "dot": "amber",
		"text": "CLASSIFY: scanning the last build output for error patterns",
	})
	h.broker.Publish("heal_started", map[string]string{"status": "running"})
	w.WriteHeader(202)
	writeJSON(w, map[string]string{"status": "heal triggered"})
}

// EvolveApp handles POST /api/evolve.
//
// Phase 2.3: instead of just emitting an SSE event (old stub), this now
// calls BuildService.Evolve() which runs evolve_frontend tasks for every
// entity in the current IR, using marker-safe EvolveForm/EvolveList.
//
// Request body: {"change": "add search bar to user list"}
// Response: 202 Accepted + job object (same shape as /api/build)
func (h *Handlers) EvolveApp(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}

	var req struct {
		Change string `json:"change"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if req.Change == "" {
		http.Error(w, "change description required", 400)
		return
	}

	job, err := h.build.Evolve(req.Change)
	if err != nil {
		// 409 if a build/evolve is already running
		http.Error(w, err.Error(), 409)
		return
	}

	w.WriteHeader(202)
	writeJSON(w, job)
}

// ── v25.8: continue (delta against existing source) + review gate ─────

// ContinueApp handles POST /api/continue: plan a delta against existing source.
// Body: {"idea": "...", "out_dir": "...", "auto_approve": false}. When conflicts
// are found and auto_approve is false, the server publishes a "continue_review"
// SSE and holds; the UI then calls /api/continue/approve or /reject.
func (h *Handlers) ContinueApp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req struct {
		Idea        string `json:"idea"`
		OutDir      string `json:"out_dir"`
		AutoApprove bool   `json:"auto_approve"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if req.Idea == "" {
		http.Error(w, "idea required", 400)
		return
	}
	outDir := ""
	if req.OutDir != "" {
		safe, err := h.build.ValidateOutputDir(req.OutDir)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		outDir = safe
	}
	job, err := h.build.StartContinue(req.Idea, outDir, req.AutoApprove)
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	w.WriteHeader(202)
	writeJSON(w, job)
}

// ContinueApprove handles POST /api/continue/approve: generate a held plan.
func (h *Handlers) ContinueApprove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if err := h.build.ApproveContinue(); err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// ContinueReject handles POST /api/continue/reject: discard a held plan.
func (h *Handlers) ContinueReject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if err := h.build.RejectContinue(); err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
