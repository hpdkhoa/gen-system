// v23.44: NewServerWithWeb pins the static UI dir (absolute) + no-store on the Studio HTML, so a rebuilt UI is never served stale from CWD-relative http.Dir or the browser cache.
// server.go — v23.9 (Phase 2): the single mux is split into two registration
// functions so the two products can be assembled independently:
//   RegisterUnderstandRoutes — /api/events (SSE) + /api/present/* (the Studio).
//                              This is ALL the enterprise understand-server needs.
//   RegisterGenerateRoutes   — snapshots, schema, entities, build, heal, evolve,
//                              and the gated github routes. Internal/generation.
// NewServer composes both (the all-in-one dev/internal server). The enterprise
// cmd/understand-server calls ONLY RegisterUnderstandRoutes and therefore links
// no generation packages — verified by an acceptance test (go list -deps).
//
// server.go — v23: + presServer.RegisterV23Routes (architecture/search/callgraph/effects).
// server.go — v21: + /api/evolve/stage,commit,discard routes. (v20.1: /api/clean.)
package api

// ─────────────────────────────────────────────────────────────
// server.go — HTTP multiplexer for the gen-system studio server
//
// v15 change: NewServer now accepts an optional *presentation.Server.
// Pass nil to keep v14 behaviour; pass a configured Server to add
// the 5 code-intelligence endpoints under /api/present/.
//
// Patched in v18 (B4): NewServer now also takes a githubEnabled flag.
// The /api/github/* routes (config, pr, webhook) are only mounted when
// the flag is true. The engine itself never calls these — they exist
// only for explicit user-initiated UI flows. Default off matches the
// hard constraint that the engine is offline.
// ─────────────────────────────────────────────────────────────

import (
	"net/http"
	"strings"

	"gen-system/generator/internal/api/sse"
	"gen-system/generator/internal/api/understand"
	"gen-system/generator/internal/handlers"
	"gen-system/generator/internal/presentation"
)

// RegisterUnderstandRoutes mounts the read-only code-intelligence surface. It
// delegates to api/understand (which imports no generation packages) so there is
// a single source of truth for the Studio routes.
func RegisterUnderstandRoutes(mux *http.ServeMux, broker *sse.Broker, presServer *presentation.Server) {
	understand.RegisterRoutes(mux, broker, presServer)
}

// RegisterGenerateRoutes mounts the generation/build surface: snapshots, schema,
// goal mapping, entity review, build, heal, evolve, and (when enabled) github.
// These are driven by *handlers.Handlers and pull in the generation stack. The
// enterprise understand-server does NOT call this.
func RegisterGenerateRoutes(mux *http.ServeMux, h *handlers.Handlers, githubEnabled bool) {
	// ── Snapshots ────────────────────────────────────────────
	mux.HandleFunc("/api/snapshots", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			return
		}
		switch r.Method {
		case http.MethodGet:
			h.ListSnapshots(w, r)
		case http.MethodPost:
			h.CreateSnapshot(w, r)
		default:
			http.Error(w, "method not allowed", 405)
		}
	})

	mux.HandleFunc("/api/snapshots/compare", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			return
		}
		h.CompareSnapshots(w, r)
	})

	mux.HandleFunc("/api/snapshots/", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			return
		}
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, "/restore") && r.Method == http.MethodPost:
			h.RestoreSnapshot(w, r)
		case strings.HasSuffix(path, "/branch") && r.Method == http.MethodPost:
			h.BranchSnapshot(w, r)
		default:
			switch r.Method {
			case http.MethodPut:
				h.UpdateSnapshot(w, r)
			case http.MethodDelete:
				h.DeleteSnapshot(w, r)
			default:
				http.Error(w, "method not allowed", 405)
			}
		}
	})

	// ── Schema / goal ─────────────────────────────────────────
	mux.HandleFunc("/api/goal/map", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			return
		}
		h.MapGoal(w, r)
	})

	mux.HandleFunc("/api/schema/current", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		h.CurrentSchema(w, r)
	})

	mux.HandleFunc("/api/schema/diff", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		h.EntityDiff(w, r)
	})

	// ── Entity review ─────────────────────────────────────────
	mux.HandleFunc("/api/entities/", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			return
		}
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, "/approve"):
			h.ApproveEntity(w, r)
		case strings.HasSuffix(path, "/reject"):
			h.RejectEntity(w, r)
		case strings.HasSuffix(path, "/changes"):
			h.RequestChanges(w, r)
		case strings.HasSuffix(path, "/preview"):
			h.EntityPreview(w, r)
		default:
			http.Error(w, "not found", 404)
		}
	})

	// ── Build ────────────────────────────────────────────────
	mux.HandleFunc("/api/clean", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			return
		}
		h.CleanProject(w, r)
	})

	mux.HandleFunc("/api/build", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			return
		}
		switch r.Method {
		case http.MethodPost:
			h.StartBuild(w, r)
		case http.MethodGet:
			h.BuildStatus(w, r)
		}
	})

	// ── v25.6: output-folder picker + generated-file browser ──────
	// Generation surface only (understand-server never mounts these routes).
	mux.HandleFunc("/api/generate/base", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			return
		}
		h.GenerateBase(w, r)
	})
	mux.HandleFunc("/api/generate/outdirs", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			return
		}
		h.GenerateOutdirs(w, r)
	})
	mux.HandleFunc("/api/generate/mkdir", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			return
		}
		h.GenerateMkdir(w, r)
	})
	mux.HandleFunc("/api/generate/tree", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			return
		}
		h.GenerateTree(w, r)
	})
	mux.HandleFunc("/api/generate/file", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			return
		}
		h.GenerateFile(w, r)
	})

	// ── Agent ────────────────────────────────────────────────
	mux.HandleFunc("/api/heal", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			return
		}
		h.TriggerHeal(w, r)
	})

	mux.HandleFunc("/api/evolve", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			return
		}
		h.EvolveApp(w, r)
	})

	// v25.8: continue (delta against existing source) + Studio review gate.
	mux.HandleFunc("/api/continue", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			return
		}
		h.ContinueApp(w, r)
	})
	mux.HandleFunc("/api/continue/approve", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			return
		}
		h.ContinueApprove(w, r)
	})
	mux.HandleFunc("/api/continue/reject", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			return
		}
		h.ContinueReject(w, r)
	})

	// v21: propose-before-write evolve (stage → review → commit/discard).
	mux.HandleFunc("/api/evolve/stage", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			return
		}
		if r.Method == http.MethodGet {
			h.StageStatus(w, r)
			return
		}
		h.StageEvolve(w, r)
	})
	mux.HandleFunc("/api/evolve/commit", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			return
		}
		h.CommitStage(w, r)
	})
	mux.HandleFunc("/api/evolve/discard", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == http.MethodOptions {
			return
		}
		h.DiscardStage(w, r)
	})

	// ── GitHub HITL CI/CD (Phase 3.1) ────────────────────────
	// v18: only mounted when --github-enabled is set on gen-server.
	// Default behaviour is fully offline — the routes do not exist.
	if githubEnabled {
		mux.HandleFunc("/api/github/config", func(w http.ResponseWriter, r *http.Request) {
			cors(w)
			if r.Method == http.MethodOptions {
				return
			}
			switch r.Method {
			case http.MethodGet:
				h.GetGitHubConfig(w, r)
			case http.MethodPost:
				h.SetGitHubConfig(w, r)
			default:
				http.Error(w, "method not allowed", 405)
			}
		})

		mux.HandleFunc("/api/github/pr", func(w http.ResponseWriter, r *http.Request) {
			cors(w)
			if r.Method == http.MethodOptions {
				return
			}
			h.TriggerPR(w, r)
		})

		mux.HandleFunc("/api/github/webhook", func(w http.ResponseWriter, r *http.Request) {
			h.GitHubWebhook(w, r)
		})
	}
}

// NewServer constructs the all-in-one HTTP handler (generation + understanding +
// static UI). This is the internal/dev server used by cmd/gen-server.
// presServer may be nil (disables /api/present/* endpoints).
// githubEnabled toggles the /api/github/* routes.
func NewServer(
	h *handlers.Handlers,
	broker *sse.Broker,
	presServer *presentation.Server, // v15 addition — pass nil to disable
	githubEnabled bool, // v18 addition — pass false to keep /api/github/* unmounted
) http.Handler {
	// v23.44: default web dir is "web" relative to CWD (legacy behaviour). Use
	// NewServerWithWeb to pin an explicit directory — cmd/gen-server does, so the
	// Studio HTML is served from a known location regardless of launch dir.
	return NewServerWithWeb(h, broker, presServer, githubEnabled, "web")
}

// NewServerWithWeb is NewServer with an explicit static-UI directory. v23.44:
// http.Dir("web") resolved relative to the PROCESS WORKING DIRECTORY, so
// launching gen-server from anywhere but the tree root silently served a stale
// (or missing) studio-v23.html — the "my fix isn't showing up" trap. Pinning an
// absolute path fixes it. Also sets Cache-Control: no-store on the HTML so a
// rebuilt UI is never served from the browser cache.
func NewServerWithWeb(
	h *handlers.Handlers,
	broker *sse.Broker,
	presServer *presentation.Server,
	githubEnabled bool,
	webDir string,
) http.Handler {
	// v24: delegate with no ProjectManager (dynamic-project routes unmounted).
	return NewServerWithProject(h, broker, presServer, githubEnabled, webDir, nil)
}

// NewServerWithProject is NewServerWithWeb plus the dynamic-project endpoints
// (/api/project/*). v24: pass a non-nil pm to let the Studio load/clear/switch
// the analysis root at runtime. pm's routes are registered BEFORE the static
// catch-all so ServeMux resolves them first.
func NewServerWithProject(
	h *handlers.Handlers,
	broker *sse.Broker,
	presServer *presentation.Server,
	githubEnabled bool,
	webDir string,
	pm *presentation.ProjectManager,
) http.Handler {
	mux := http.NewServeMux()

	RegisterGenerateRoutes(mux, h, githubEnabled)
	RegisterUnderstandRoutes(mux, broker, presServer)
	if pm != nil {
		pm.Register(mux)
	}

	// ── Static UI ─────────────────────────────────────────────
	if webDir == "" {
		webDir = "web"
	}
	fileServer := http.FileServer(http.Dir(webDir))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Never let a browser cache the Studio HTML/JS — a rebuilt UI must show
		// up on refresh. (Static assets are small; correctness beats caching.)
		w.Header().Set("Cache-Control", "no-store, must-revalidate")
		fileServer.ServeHTTP(w, r)
	}))

	return mux
}

func cors(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}
