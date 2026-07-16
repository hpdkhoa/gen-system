// generator/internal/api/understand/routes.go — v23.9 (Phase 2/3): the
// understand-only route registration, deliberately in its OWN package so it
// imports nothing but sse + presentation. The parent `api` package imports
// `handlers` (for the generation routes), which transitively pulls in the whole
// generation stack; anything importing `api` inherits that. By living here,
// cmd/understand-server can mount the Studio API without importing `api` at all,
// keeping the enterprise binary free of generation packages (enforced by
// archtest/boundary_test.go).
package understand

import (
	"net/http"

	"gen-system/generator/internal/api/sse"
	"gen-system/generator/internal/presentation"
)

// RegisterRoutes mounts the read-only code-intelligence surface: the SSE stream
// and the /api/present/* + /api/present/v23/* endpoints the Studio consumes.
func RegisterRoutes(mux *http.ServeMux, broker *sse.Broker, presServer *presentation.Server) {
	mux.Handle("/api/events", broker)
	if presServer != nil {
		presServer.RegisterRoutes(mux)
		presServer.RegisterV23Routes(mux)
	}
}
