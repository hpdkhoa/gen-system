// generator/internal/webui/embed.go (v23.9, Phase 3 step 2): embeds the
// enterprise Studio UI into the binary so the understand-server ships as a
// single self-contained, tamper-evident file (no loose web/ directory required
// on the customer's box). Only studio-v23.html is embedded. The legacy
// web/legacy/studio.html and web/legacy/generate.html are internal/dev assets, and
// they stay out of the enterprise artifact on purpose.
//
// The embedded FS serves "/" -> studio-v23.html so the appliance opens straight
// into the Studio with no path needed.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed studio-v23.html
var assets embed.FS

// FS returns the embedded UI file system.
func FS() fs.FS { return assets }

// Handler returns an http.Handler that serves the embedded Studio. A request to
// "/" (or "/index.html") is rewritten to studio-v23.html so the appliance opens
// directly into the Studio.
func Handler() http.Handler {
	fileServer := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			r.URL.Path = "/studio-v23.html"
		}
		fileServer.ServeHTTP(w, r)
	})
}
