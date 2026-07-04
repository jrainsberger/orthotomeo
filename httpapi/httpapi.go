// Package httpapi is the browser-facing seam (T27): read-only GET endpoints
// over engine.Engine, plus the static reading-view UI served from the same
// binary. It exists because the browser renders polytonic Greek and RTL
// Hebrew correctly for free - the reason this transport beats a native-
// toolkit renderer for this corpus.
//
// Security posture (dual mindset, baked into the ticket, not bolted on):
//   - GET-only. There is no mutation surface at all - every handler reads
//     through engine, never touches SQL directly (Concord spec invariant #2).
//   - Loopback-only by convention: Serve's caller is responsible for binding
//     127.0.0.1, never 0.0.0.0 (see cmd/orthotomeo-web) - this package itself
//     has no network-binding code, so there's nothing here to misconfigure,
//     but the assumption is documented at every layer that does bind.
//   - A distributed build serves only sources.shippable=1 text: shippable()
//     filters/validates every edition or corpus code a request names against
//     the live sources.Registry() before it ever reaches engine, so a
//     non-shippable edition (e.g. a future user-fetched Rahlfs LXX, T23)
//     never appears in a served response even if a caller asks for it by name.
package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/jrainsberger/orthotomeo/engine"
	"github.com/jrainsberger/orthotomeo/interlinear"
	"github.com/jrainsberger/orthotomeo/lexicon"
	"github.com/jrainsberger/orthotomeo/retriever"
)

// Server wraps the one engine.Engine every handler delegates through - no
// handler ever imports database/sql or builds SQL itself.
type Server struct {
	e *engine.Engine
}

// New wraps e. The caller owns e's lifecycle (Open/Close) - Server never
// closes it.
func New(e *engine.Engine) *Server {
	return &Server{e: e}
}

// Handler returns the routed http.Handler: the API endpoints under their
// own paths, plus the embedded static UI at "/" for everything else.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /verse", s.handleVerse)
	mux.HandleFunc("GET /passage", s.handlePassage)
	mux.HandleFunc("GET /concord", s.handleConcord)
	mux.HandleFunc("GET /parse", s.handleParse)
	mux.HandleFunc("GET /attest", s.handleAttest)
	mux.HandleFunc("GET /interlinear", s.handleInterlinear)
	mux.HandleFunc("GET /define", s.handleDefine)
	mux.HandleFunc("GET /books", handleBooks)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticFS)))
	mux.HandleFunc("GET /{$}", handleIndex)
	return mux
}

// citationsResponse is the JSON envelope for every Citation-bearing
// endpoint - the same shape T26's CLI --json and T20's MCP tools already
// use (citationsPayload/citationsResult), so a client that already knows
// one transport's shape knows this one too. Sources is T31's per-edition
// provenance map (file/license/attribution/T36 homepage_url), added once
// per distinct edition actually present.
type citationsResponse struct {
	Citations []retriever.Citation            `json:"citations"`
	Sources   map[string]retriever.SourceInfo `json:"sources,omitempty"`
}

// interlinearResponse mirrors citationsResponse for T35's Word shape.
type interlinearResponse struct {
	Words   []interlinear.Word              `json:"words"`
	Sources map[string]retriever.SourceInfo `json:"sources,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func citationsPayload(cs []retriever.Citation) (citationsResponse, error) {
	srcs, err := retriever.SourcesFor(cs)
	if err != nil {
		return citationsResponse{}, err
	}
	return citationsResponse{Citations: cs, Sources: srcs}, nil
}

// lookupPayload is define's response - lexicon.Entry itself already has
// the exact shape wanted (Definition nil-omitted for a Hebrew entry, T34),
// no wrapper needed.
type lookupPayload = lexicon.Entry
