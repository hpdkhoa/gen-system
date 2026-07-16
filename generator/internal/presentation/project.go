// project.go — v24: dynamic project loading for the Studio. Lets the UI plug in
// one source codebase at a time (hard swap), clear it, and plug another —
// without restarting gen-server. Security: every requested root must resolve
// (symlinks cleaned) to a subdirectory of the configured projectsBase; absolute
// escapes and ".." traversal are rejected. The heavy work (parse + swap) is
// delegated to a caller-provided loader so this package keeps no dependency on
// internal/context (which imports presentation-adjacent packages); gen-server
// wires the loader in.
package presentation

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ProjectLoader parses a root and returns a swap closure that applies the new
// analysis context to the Server (via ReplaceContext), plus the resolved root
// for reporting. Implemented in gen-server over context.LoadContextWithOpts, so
// this package keeps no dependency on internal/context. Returns an error the
// handler surfaces verbatim.
type ProjectLoader func(root string, opts ProjectLoadOpts) (loadedRoot string, swap func(*Server), err error)

// ProjectLoadOpts are the per-load knobs the UI can set.
type ProjectLoadOpts struct {
	CopybookPath string `json:"copybooks"`
	Stdlib       string `json:"stdlib"`
	StdlibLang   string `json:"stdlibLang"`
	Live         bool   `json:"live"` // start the file watcher on the new root
}

// WatcherControl lets the project endpoints re-point the file watcher when the
// root changes. gen-server supplies an implementation. Repoint(root, live)
// stops watching the old root and — only if live — starts watching the new one.
type WatcherControl interface {
	Repoint(root string, live bool)
}

// ProjectManager holds the dynamic-project state and serves the endpoints.
type ProjectManager struct {
	srv          *Server
	projectsBase string // absolute; requested roots must live under this
	loader       ProjectLoader
	watcher      WatcherControl

	mu      sync.Mutex
	current string // absolute root currently loaded ("" = cleared)
	live    bool   // whether the watcher is live for the current project
}

// NewProjectManager wires the manager. projectsBase is made absolute; if empty,
// the caller should pass the parent of the initial --root (gen-server does).
func NewProjectManager(srv *Server, projectsBase string, loader ProjectLoader, watcher WatcherControl) *ProjectManager {
	abs := projectsBase
	if a, err := filepath.Abs(projectsBase); err == nil {
		abs = a
	}
	return &ProjectManager{srv: srv, projectsBase: abs, loader: loader, watcher: watcher}
}

// SetCurrent records the initially-loaded project (called once at startup so
// /api/project/current is accurate before any dynamic load).
func (pm *ProjectManager) SetCurrent(root string, live bool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if a, err := filepath.Abs(root); err == nil {
		pm.current = a
	} else {
		pm.current = root
	}
	pm.live = live
}

// Register mounts the project endpoints on the mux.
func (pm *ProjectManager) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/project/load", pm.handleLoad)
	mux.HandleFunc("/api/project/clear", pm.handleClear)
	mux.HandleFunc("/api/project/current", pm.handleCurrent)
	mux.HandleFunc("/api/project/base", pm.handleBase)
	mux.HandleFunc("/api/project/list", pm.handleList) // v25.8.2: folder picker
}

// handleList returns the immediate subdirectories of the projects base so the
// Studio can offer a folder PICKER instead of a raw absolute-path text box.
// Each entry is flagged with whether it looks like a source project (contains
// recognizable code / a .gen marker), so the user can tell projects from noise.
func (pm *ProjectManager) handleList(w http.ResponseWriter, r *http.Request) {
	base := pm.projectsBase
	if strings.TrimSpace(base) == "" {
		writeProjectJSON(w, http.StatusOK, map[string]interface{}{"base": "", "enabled": false, "dirs": []interface{}{}})
		return
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		writeProjectJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "cannot read projects base: " + err.Error()})
		return
	}
	type dirInfo struct {
		Name     string `json:"name"`
		Path     string `json:"path"`
		IsSource bool   `json:"is_source"`
	}
	var dirs []dirInfo
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		abs := filepath.Join(base, e.Name())
		dirs = append(dirs, dirInfo{Name: e.Name(), Path: abs, IsSource: looksLikeProject(abs)})
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })
	writeProjectJSON(w, http.StatusOK, map[string]interface{}{"base": base, "enabled": true, "dirs": dirs})
}

// looksLikeProject reports whether dir contains something worth loading: a .gen
// marker, a go.mod, a backend/ dir, or any recognizable source file near the top.
func looksLikeProject(dir string) bool {
	for _, marker := range []string{".gen", "go.mod", "backend", "src"} {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return true
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch filepath.Ext(e.Name()) {
		case ".go", ".java", ".py", ".ts", ".tsx", ".cob", ".cbl":
			return true
		}
	}
	return false
}

// validateRoot resolves req to an absolute, symlink-cleaned path and confirms it
// is projectsBase itself or a descendant. Returns the safe absolute path or an
// error suitable for a 400.
func (pm *ProjectManager) validateRoot(req string) (string, error) {
	if strings.TrimSpace(req) == "" {
		return "", fmt.Errorf("root is required")
	}
	abs, err := filepath.Abs(req)
	if err != nil {
		return "", fmt.Errorf("cannot resolve path: %v", err)
	}
	// Clean symlinks on the deepest existing ancestor so a symlinked dir can't
	// escape the base. EvalSymlinks fails if the path doesn't exist yet; for a
	// project root we require it to exist anyway.
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("path does not exist or is unreadable: %v", err)
	}
	base := pm.projectsBase
	if b, err := filepath.EvalSymlinks(base); err == nil {
		base = b
	}
	// Descendant check via relative path (no ".." components).
	rel, err := filepath.Rel(base, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("root must be inside the projects base (%s)", pm.projectsBase)
	}
	fi, err := os.Stat(resolved)
	if err != nil || !fi.IsDir() {
		return "", fmt.Errorf("root must be an existing directory")
	}
	return resolved, nil
}

func writeProjectJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// handleLoad: POST {root, copybooks?, stdlib?, stdlibLang?, live?} → hard-swap
// the analysis context to the new project.
func (pm *ProjectManager) handleLoad(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Root string `json:"root"`
		ProjectLoadOpts
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	safeRoot, err := pm.validateRoot(body.Root)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if pm.loader == nil {
		http.Error(w, "project loading not available", http.StatusServiceUnavailable)
		return
	}
	loadedRoot, swap, err := pm.loader(safeRoot, body.ProjectLoadOpts)
	if err != nil {
		http.Error(w, fmt.Sprintf("load failed: %v", err), http.StatusUnprocessableEntity)
		return
	}
	// Apply the swap (mutex-guarded inside ReplaceContext via the closure).
	swap(pm.srv)
	pm.srv.SetRoot(loadedRoot, body.CopybookPath, "")

	pm.mu.Lock()
	pm.current = loadedRoot
	pm.live = body.Live
	pm.mu.Unlock()

	if pm.watcher != nil {
		pm.watcher.Repoint(loadedRoot, body.Live)
	}
	if pm.srv.broker != nil {
		pm.srv.broker.Publish("project_loaded", map[string]interface{}{
			"root": loadedRoot,
			"live": body.Live,
		})
	}
	writeProjectJSON(w, http.StatusOK, map[string]interface{}{
		"ok":   true,
		"root": loadedRoot,
		"live": body.Live,
	})
}

// handleClear: POST → drop the current project (empty state).
func (pm *ProjectManager) handleClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	pm.srv.ReplaceContext(nil, nil, nil) // empty index/cfgs/efx
	pm.srv.SetRoot("", "", "")
	pm.mu.Lock()
	pm.current = ""
	pm.live = false
	pm.mu.Unlock()
	if pm.watcher != nil {
		pm.watcher.Repoint("", false) // stop watching
	}
	if pm.srv.broker != nil {
		pm.srv.broker.Publish("project_cleared", map[string]interface{}{})
	}
	writeProjectJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "root": ""})
}

// handleCurrent: GET → the active root + live flag.
func (pm *ProjectManager) handleCurrent(w http.ResponseWriter, r *http.Request) {
	pm.mu.Lock()
	cur, live := pm.current, pm.live
	pm.mu.Unlock()
	writeProjectJSON(w, http.StatusOK, map[string]interface{}{
		"root":   cur,
		"live":   live,
		"loaded": cur != "",
	})
}

// handleBase: GET → the projects base, so the UI can constrain its path input.
func (pm *ProjectManager) handleBase(w http.ResponseWriter, r *http.Request) {
	writeProjectJSON(w, http.StatusOK, map[string]interface{}{"base": pm.projectsBase})
}
