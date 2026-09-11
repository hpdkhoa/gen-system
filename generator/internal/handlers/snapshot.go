package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (h *Handlers) ListSnapshots(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	if project == "" {
		project = "default"
	}
	writeJSON(w, h.snap.List(project))
}

func (h *Handlers) CreateSnapshot(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Author      string `json:"author"`
		ProjectID   string `json:"project_id"`
		Auto        bool   `json:"auto"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if req.ProjectID == "" {
		req.ProjectID = "default"
	}
	if req.Author == "" {
		req.Author = "you"
	}
	rec, err := h.snap.Create(req.Name, req.Description, req.Author, req.ProjectID, req.Auto)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, rec)
}

func (h *Handlers) UpdateSnapshot(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r, "id")
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Pinned      *bool  `json:"pinned"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	rec := h.snap.Update(id, req.Name, req.Description, req.Pinned)
	if rec == nil {
		http.Error(w, "not found", 404)
		return
	}
	writeJSON(w, rec)
}

func (h *Handlers) DeleteSnapshot(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r, "id")
	rec := h.snap.Find(id)
	if rec == nil {
		http.Error(w, "not found", 404)
		return
	}
	if rec.Tag == "CURRENT" {
		http.Error(w, "cannot delete active snapshot", 400)
		return
	}
	if rec.Pinned {
		http.Error(w, "snapshot is pinned", 400)
		return
	}
	h.snap.Delete(id)
	w.WriteHeader(204)
}

func (h *Handlers) RestoreSnapshot(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r, "id")
	rec := h.snap.Restore(id)
	if rec == nil {
		http.Error(w, "not found", 404)
		return
	}
	writeJSON(w, rec)
}

func (h *Handlers) BranchSnapshot(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r, "id")
	rec := h.snap.Branch(id)
	if rec == nil {
		http.Error(w, "not found", 404)
		return
	}
	writeJSON(w, rec)
}

func (h *Handlers) CompareSnapshots(w http.ResponseWriter, r *http.Request) {
	a, b := r.URL.Query().Get("a"), r.URL.Query().Get("b")
	snapA, snapB := h.snap.Find(a), h.snap.Find(b)
	if snapA == nil || snapB == nil {
		http.Error(w, "snapshot not found", 404)
		return
	}
	writeJSON(w, map[string]interface{}{"left": snapA, "right": snapB})
}

var _ = strings.Contains // suppress unused import if linter fires
