// handlers.go (v21): + staging service field (propose-before-write evolve).
package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/api/sse"
	"github.com/hpdkhoa/gen-system/generator/internal/services"
)

type Handlers struct {
	broker  *sse.Broker
	snap    *services.SnapshotService
	build   *services.BuildService
	schema  *services.SchemaService
	staging *services.StagingService
}

func New(
	broker *sse.Broker,
	snap *services.SnapshotService,
	build *services.BuildService,
	schema *services.SchemaService,
	staging *services.StagingService,
) *Handlers {
	return &Handlers{broker: broker, snap: snap, build: build, schema: schema, staging: staging}
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// pathParam extracts the last path segment as a named parameter.
// e.g. /api/snapshots/snap-123/restore → for id: "snap-123"
func pathParam(r *http.Request, _ string) string {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 {
		return ""
	}
	// /api/snapshots/{id}         → parts[2]
	// /api/snapshots/{id}/restore → parts[2]
	// /api/entities/{name}/approve → parts[2]
	return parts[2]
}
