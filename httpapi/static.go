package httpapi

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var staticFiles embed.FS

// staticFS strips the "static/" embed prefix so /static/app.js on the wire
// maps to static/app.js on disk, not static/static/app.js.
var staticFS = mustSub(staticFiles, "static")

func mustSub(f embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(f, dir)
	if err != nil {
		panic("httpapi: embedded static assets missing: " + err.Error())
	}
	return sub
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	b, err := fs.ReadFile(staticFS, "index.html")
	if err != nil {
		http.Error(w, "index.html not embedded", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(b)
}

// handleFavicon serves the same favicon.svg a page's own <link rel="icon">
// points at, at the conventional /favicon.ico path too - a browser requests
// that path directly regardless of any <link> tag, so without this route
// it 404s even though the icon itself is right there under /static/.
func handleFavicon(w http.ResponseWriter, r *http.Request) {
	b, err := fs.ReadFile(staticFS, "favicon.svg")
	if err != nil {
		http.Error(w, "favicon.svg not embedded", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Write(b)
}
