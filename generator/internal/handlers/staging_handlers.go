// staging_handlers.go (v21): endpoints for propose-before-write evolve.
package handlers

import (
	"encoding/json"
	"net/http"
)

// StageEvolve handles POST /api/evolve/stage {change}. It runs the evolve in a
// staging copy and returns the file diff. Does not modify live files.
func (h *Handlers) StageEvolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
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
	res, err := h.staging.Stage(req.Change)
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	writeJSON(w, res)
}

// StageStatus handles GET /api/evolve/stage and returns the pending proposal or null.
func (h *Handlers) StageStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.staging.Pending())
}

// CommitStage handles POST /api/evolve/commit and writes staged files to live.
func (h *Handlers) CommitStage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	n, err := h.staging.Commit()
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	writeJSON(w, map[string]int{"committed": n})
}

// DiscardStage handles POST /api/evolve/discard and throws away the proposal.
func (h *Handlers) DiscardStage(w http.ResponseWriter, r *http.Request) {
	// v26.58 (D091): POST only, as CommitStage. The route passes every method
	// but OPTIONS, so a GET removed the staging folder and the pending
	// proposal. The Studio sends POST.
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if err := h.staging.Discard(); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
