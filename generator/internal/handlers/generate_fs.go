// generate_fs.go — v25.6: output-folder picker + generated-file browser for the
// three-pane generate.html.
//
// Endpoints (gen-server generation surface ONLY — never mounted on
// understand-server, so the air-gapped enterprise binary keeps zero generation
// routes):
//
//	GET  /api/generate/outdirs        list candidate output folders under the
//	                                  projects base (excluding the current root),
//	                                  each flagged existing/empty/has-manifest.
//	POST /api/generate/mkdir          {name} → create a folder under the base.
//	GET  /api/generate/base           report the base + current root (for the UI).
//	GET  /api/generate/tree?dir=      file tree of a generated output folder.
//	GET  /api/generate/file?path=     read one text file (bounded to the base).
//
// Every path is validated against the projects base via the same rule the build
// uses (BuildService.ValidateOutputDir / a base-descendant check), so the browser
// can never read or create outside the sandbox.
package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// maxFileBytes caps a single file read served to the reviewer pane.
const maxFileBytes = 512 * 1024

// outDirInfo describes one candidate output folder for the picker.
type outDirInfo struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Empty       bool   `json:"empty"`
	HasManifest bool   `json:"has_manifest"`
	HasBackend  bool   `json:"has_backend"`
	IsRoot      bool   `json:"is_root"`
}

// baseAndRoot returns the absolute projects base and root, or an error string
// when the base is not configured.
func (h *Handlers) baseAndRoot() (base, root string, ok bool) {
	base = h.build.ProjectsBase()
	root = h.build.Root()
	return base, root, base != ""
}

// withinBase reports whether abs is the base or a descendant (no ".." escape).
func withinBase(base, abs string) bool {
	b := base
	if r, err := filepath.EvalSymlinks(base); err == nil {
		b = r
	}
	target := abs
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		target = r
	}
	rel, err := filepath.Rel(b, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// GenerateBase handles GET /api/generate/base — the base + root for the UI so it
// can explain the boundary and disable the picker when no base is set.
func (h *Handlers) GenerateBase(w http.ResponseWriter, r *http.Request) {
	base, root, ok := h.baseAndRoot()
	writeJSON(w, map[string]interface{}{
		"base":    base,
		"root":    root,
		"enabled": ok,
	})
}

// GenerateOutdirs handles GET /api/generate/outdirs — immediate subdirectories of
// the base, each flagged so the UI can show which are empty / already generated /
// the current root (which is not selectable).
func (h *Handlers) GenerateOutdirs(w http.ResponseWriter, r *http.Request) {
	base, root, ok := h.baseAndRoot()
	if !ok {
		http.Error(w, "output-folder selection is disabled (no --projects-base configured)", 400)
		return
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		http.Error(w, "cannot read projects base: "+err.Error(), 500)
		return
	}
	var dirs []outDirInfo
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		abs := filepath.Join(base, e.Name())
		info := outDirInfo{Name: e.Name(), Path: abs}
		if r, err := filepath.EvalSymlinks(abs); err == nil {
			info.IsRoot = (r == root)
		} else {
			info.IsRoot = (abs == root)
		}
		sub, _ := os.ReadDir(abs)
		info.Empty = len(sub) == 0
		for _, s := range sub {
			switch s.Name() {
			case "design-manifest":
				info.HasManifest = true
			case "backend", "output":
				info.HasBackend = true
			}
		}
		dirs = append(dirs, info)
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })
	writeJSON(w, map[string]interface{}{"base": base, "root": root, "dirs": dirs})
}

// GenerateMkdir handles POST /api/generate/mkdir {name} — create a new folder
// directly under the base (single path segment, no separators). Returns the
// created absolute path.
func (h *Handlers) GenerateMkdir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", 405)
		return
	}
	base, _, ok := h.baseAndRoot()
	if !ok {
		http.Error(w, "output-folder selection is disabled (no --projects-base configured)", 400)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", 400)
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		http.Error(w, "folder name required", 400)
		return
	}
	// Single clean segment only — no separators, no traversal, no dotfiles.
	if strings.ContainsAny(name, `/\`) || name == "." || name == ".." || strings.HasPrefix(name, ".") {
		http.Error(w, "folder name must be a simple name (no slashes, no leading dot)", 400)
		return
	}
	abs := filepath.Join(base, name)
	if !withinBase(base, abs) {
		http.Error(w, "resolved path escapes the projects base", 400)
		return
	}
	if _, err := os.Stat(abs); err == nil {
		http.Error(w, "a folder with that name already exists", 409)
		return
	}
	if err := os.MkdirAll(abs, 0755); err != nil {
		http.Error(w, "could not create folder: "+err.Error(), 500)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "path": abs, "name": name})
}

// treeNode is one node in the generated-file tree.
type treeNode struct {
	Name     string     `json:"name"`
	Path     string     `json:"path"`
	Dir      bool       `json:"dir"`
	Children []treeNode `json:"children,omitempty"`
}

// GenerateTree handles GET /api/generate/tree?dir= — the file tree of a generated
// output folder, bounded to the base. Skips heavy/noise dirs.
func (h *Handlers) GenerateTree(w http.ResponseWriter, r *http.Request) {
	base, _, ok := h.baseAndRoot()
	if !ok {
		http.Error(w, "disabled (no --projects-base)", 400)
		return
	}
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		http.Error(w, "dir required", 400)
		return
	}
	abs, err := filepath.Abs(dir)
	if err != nil || !withinBase(base, abs) {
		http.Error(w, "dir must be inside the projects base", 400)
		return
	}
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		http.Error(w, "dir is not a directory", 400)
		return
	}
	root := buildTree(abs, 0)
	writeJSON(w, root)
}

// skipDir is the set of directory names not worth showing in the reviewer.
var skipDir = map[string]bool{
	"node_modules": true, ".git": true, ".gen": true, "vendor": true,
	"dist": true, "build": true, ".next": true,
}

// buildTree walks abs to a bounded depth, returning a sorted tree (dirs first).
func buildTree(abs string, depth int) treeNode {
	node := treeNode{Name: filepath.Base(abs), Path: abs, Dir: true}
	if depth > 12 {
		return node
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return node
	}
	var dirs, files []treeNode
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") && e.Name() != ".env.example" {
			continue
		}
		child := filepath.Join(abs, e.Name())
		if e.IsDir() {
			if skipDir[e.Name()] {
				continue
			}
			dirs = append(dirs, buildTree(child, depth+1))
		} else {
			files = append(files, treeNode{Name: e.Name(), Path: child, Dir: false})
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	node.Children = append(dirs, files...)
	return node
}

// GenerateFile handles GET /api/generate/file?path= — read one text file, bounded
// to the base and capped in size. Returns {path, content, truncated}.
func (h *Handlers) GenerateFile(w http.ResponseWriter, r *http.Request) {
	base, _, ok := h.baseAndRoot()
	if !ok {
		http.Error(w, "disabled (no --projects-base)", 400)
		return
	}
	p := r.URL.Query().Get("path")
	if p == "" {
		http.Error(w, "path required", 400)
		return
	}
	abs, err := filepath.Abs(p)
	if err != nil || !withinBase(base, abs) {
		http.Error(w, "path must be inside the projects base", 400)
		return
	}
	fi, err := os.Stat(abs)
	if err != nil || fi.IsDir() {
		http.Error(w, "not a file", 400)
		return
	}
	f, err := os.Open(abs)
	if err != nil {
		http.Error(w, "cannot open file", 500)
		return
	}
	defer f.Close()
	buf := make([]byte, maxFileBytes)
	n, _ := f.Read(buf)
	truncated := fi.Size() > int64(maxFileBytes)
	writeJSON(w, map[string]interface{}{
		"path":      abs,
		"name":      filepath.Base(abs),
		"content":   string(buf[:n]),
		"truncated": truncated,
		"size":      fi.Size(),
	})
}
