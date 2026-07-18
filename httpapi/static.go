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

// staticCacheControl lets a browser reuse an embedded asset for an hour
// without asking again. Deliberately modest: these filenames are not
// content-hashed, so a long max-age would strand visitors on stale JS/CSS
// after a deploy with no way to bust it. An hour removes nearly every repeat
// request within a session - which is the actual cost being addressed, since
// each one otherwise wakes the container - while a redeploy is still picked
// up promptly. Revisit only alongside content-hashed filenames.
const staticCacheControl = "public, max-age=3600"

// cacheStatic sets that policy on the embedded asset routes. Without it every
// revisit issues a conditional request: cheap individually, but it is the
// single largest source of avoidable traffic once a UI is published.
func cacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", staticCacheControl)
		next.ServeHTTP(w, r)
	})
}

// robotsTxt denies every well-behaved crawler the entire surface. This is a
// study engine, not a site to index: crawling it spends the public
// instance's finite request budget and surfaces nothing a search engine
// should hold. Held as a constant rather than an embedded file so the route
// has no "not embedded" failure branch - a policy this small should not be
// able to 500.
//
// It is advisory only. A crawler that ignores robots.txt, and the
// vulnerability scanners that probe this host, are entirely unaffected -
// this is hygiene and load reduction, not a security control.
const robotsTxt = "User-agent: *\nDisallow: /\n"

// handleRobots serves robotsTxt at the conventional path. Registered on both
// the full UI Handler and the JSON-only APIHandler: the public deployment
// runs the latter, which is precisely the surface crawlers were reaching
// (observed live as GET /robots.txt 404).
func handleRobots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(robotsTxt))
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
