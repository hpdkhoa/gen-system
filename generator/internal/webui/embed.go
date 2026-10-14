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
	"bytes"
	"embed"
	"io/fs"
	"net/http"
)

//go:embed studio-v23.html
var assets embed.FS

// FS returns the embedded UI file system.
func FS() fs.FS { return assets }

// readOnlyStyle hides the parts of the Studio that belong to the generation
// half. v26.68: understand-server only serves this embedded copy, links no
// generation code, and does not embed legacy/generate.html, so its "Build from
// idea" link led nowhere and its Self-healing panel could never fill. gen-server
// serves its own copy from web/ and keeps both.
const readOnlyStyle = `<style id="read-only-studio">
a[href="legacy/generate.html"] { display: none !important; }
.panel:has(#heal-log) { display: none !important; }
</style>
`

// studioPage returns the embedded Studio with readOnlyStyle added to its head.
func studioPage() ([]byte, error) {
	page, err := assets.ReadFile("studio-v23.html")
	if err != nil {
		return nil, err
	}
	return bytes.Replace(page, []byte("</head>"), []byte(readOnlyStyle+"</head>"), 1), nil
}

// Handler returns an http.Handler that serves the embedded Studio. A request to
// "/" (or "/index.html") is rewritten to studio-v23.html so the appliance opens
// directly into the Studio, and the page goes out with readOnlyStyle.
func Handler() http.Handler {
	fileServer := http.FileServer(http.FS(assets))
	page, pageErr := studioPage()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/", "/index.html", "/studio-v23.html":
			if pageErr != nil {
				http.Error(w, pageErr.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(page)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}
