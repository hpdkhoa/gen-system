package handlers

import (
	"encoding/json"
	"net/http"
)

func (h *Handlers) MapGoal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Goal string `json:"goal"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	entities := h.schema.CurrentEntities()
	h.broker.Publish("agent_event", map[string]string{
		"phase": "map", "dot": "cyan",
		"text": "Parsing goal: \"" + req.Goal + "\"",
	})
	writeJSON(w, map[string]interface{}{"goal": req.Goal, "entities": entities})
}

func (h *Handlers) CurrentSchema(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.schema.CurrentEntities())
}

func (h *Handlers) EntityDiff(w http.ResponseWriter, r *http.Request) {
	entity := r.URL.Query().Get("entity")
	if entity == "" {
		entity = pathParam(r, "name")
	}
	writeJSON(w, h.schema.EntityDiff(entity))
}

func (h *Handlers) ApproveEntity(w http.ResponseWriter, r *http.Request) {
	name := pathParam(r, "name")
	h.broker.Publish("entity_approved", map[string]string{"entity": name})
	writeJSON(w, map[string]string{"entity": name, "status": "approved"})
}

func (h *Handlers) RejectEntity(w http.ResponseWriter, r *http.Request) {
	name := pathParam(r, "name")
	var req struct{ Comment string `json:"comment"` }
	_ = json.NewDecoder(r.Body).Decode(&req)
	h.broker.Publish("entity_rejected", map[string]string{"entity": name, "comment": req.Comment})
	writeJSON(w, map[string]string{"entity": name, "status": "rejected"})
}

func (h *Handlers) RequestChanges(w http.ResponseWriter, r *http.Request) {
	name := pathParam(r, "name")
	var req struct{ Comment string `json:"comment"` }
	_ = json.NewDecoder(r.Body).Decode(&req)
	h.broker.Publish("entity_changes_requested", map[string]string{"entity": name, "comment": req.Comment})
	writeJSON(w, map[string]string{"entity": name, "status": "changes_requested"})
}

// v18.3: the previous file declared EntityPreview twice and shipped a
// dead helper entityFilePreview() that the surviving handler does not
// call. The first declaration delegated to a hard-coded path list; the
// second (kept below) delegates to SchemaService.PreviewEntity which
// validates the entity exists in the proposed-or-current schema and
// reports per-file on-disk status. The hard-coded version and its
// helper are removed entirely.

// EntityPreview returns the list of files that would be written to disk
// for a given entity during the next build. Powers the Studio UI
// "Files to be written" preview modal (Phase 1.3).
//
// GET /api/entities/:name/preview
func (h *Handlers) EntityPreview(w http.ResponseWriter, r *http.Request) {
	name := pathParam(r, "name")
	if name == "" {
		http.Error(w, "entity name required", 400)
		return
	}
	files, err := h.schema.PreviewEntity(name)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	writeJSON(w, map[string]interface{}{
		"entity": name,
		"files":  files,
		"count":  len(files),
	})
}
