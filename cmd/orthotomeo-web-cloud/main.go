// Command orthotomeo-web-cloud is the publicly-reachable, MCP-first cloud
// deployment: a remote MCP endpoint (Streamable HTTP, T43) at /mcp for any
// LLM client to use over the network - no local binary, no local DB - plus
// the same JSON REST API cmd/orthotomeo-web serves locally, mounted
// alongside it for anything that just wants plain HTTP. It deliberately
// binds 0.0.0.0:$PORT (Cloud Run's contract) instead of loopback, and
// wraps both surfaces with rate limiting, timeouts, and the other
// hardening a loopback-only local tool doesn't need. cmd/orthotomeo-web's
// own doc comment says repurposing it to listen beyond loopback needs a
// fresh security design, not a flag - this binary is that fresh design,
// kept entirely separate so the local tool's threat model is never
// touched.
//
// The DB is baked into the image at build time (see Dockerfile.cloud) -
// this binary reads it from a fixed path, no --db flag, since a publicly
// reachable container has no host filesystem to mount one from anyway.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jrainsberger/orthotomeo/engine"
	"github.com/jrainsberger/orthotomeo/httpapi"
	"github.com/jrainsberger/orthotomeo/mcpserver"
)

const (
	dbPath      = "/data/orthotomeo.db"
	defaultPort = "8080" // Cloud Run's own documented default when $PORT is unset

	requestTimeout = 10 * time.Second

	// The study budget: charged only for requests that reach the engine
	// (see classify). The numbers are unchanged - what changed is that a
	// liveness ping, a page load, or a 404 probe no longer spends them.
	ipWorkLimit      = 60 // engine queries per IP
	ipWorkWindow     = time.Hour
	globalWorkLimit  = 2000 // engine queries, all sources - the hard cost ceiling
	globalWorkWindow = 24 * time.Hour

	// The flood guard: every request, including the free ones (static
	// assets, index, favicon, robots.txt, MCP handshakes, probes). Its only
	// job is to stop someone hammering cheap endpoints, so it is sized well
	// above real use - one UI page load is roughly six requests, so 600/hr
	// leaves room for ~100 page loads an hour from a single visitor.
	ipFloodLimit      = 600
	ipFloodWindow     = time.Hour
	globalFloodLimit  = 50000 // current real traffic is ~2,700/day
	globalFloodWindow = 24 * time.Hour
)

// newHandler builds the full routed, middleware-wrapped handler: /mcp
// (Streamable HTTP, T43) alongside the REST API, both behind the same
// rate-limiting/timeout/security hardening. Factored out from main so a
// test can exercise the real routing/middleware chain end-to-end, not just
// main's wiring by inspection.
func newHandler(e *engine.Engine) http.Handler {
	mcpSrv := mcp.NewServer(&mcp.Implementation{Name: "orthotomeo", Version: "0.1.0"}, nil)
	mcpserver.RegisterTools(mcpSrv, e)
	// Stateless + JSONResponse: every call is one JSON-RPC request/response
	// over a single HTTP POST, same shape as the REST API - no session
	// cookies, no long-lived SSE stream held open. That keeps /mcp inside
	// the same timeout/rate-limit assumptions as the rest of this file
	// (which are sized for quick request/response, not a persistent
	// connection) without needing separate handling for this one path.
	// getServer may return the same *mcp.Server for every request (see the
	// SDK's own doc comment on NewStreamableHTTPHandler) - RegisterTools'
	// tools are stateless engine reads, so one shared server is safe.
	mcpHandler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return mcpSrv },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
	)

	// API-only for now: the web UI is still under active development and
	// isn't ready to publish - see httpapi.APIHandler's doc comment. Switch
	// back to .Handler() once the UI and its own frontend tests are ready.
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpHandler)
	mux.Handle("/", httpapi.New(e).APIHandler())

	handler := gzipMiddleware(http.Handler(mux))
	handler = http.TimeoutHandler(handler, requestTimeout, `{"error":"request timed out"}`)
	handler = newBudgets().middleware(handler)
	handler = securityHeaders(handler)
	handler = loggingMiddleware(handler)
	return handler
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	// Bound concordance result size for the public instance. The rate
	// limiter bounds how OFTEN a caller can ask; this bounds how EXPENSIVE
	// any single ask may be - without it, one unauthenticated request for a
	// common lemma (TAGNT ὁ matches 20705 rows) can allocate tens of
	// megabytes inside a 256Mi container that serves 20 requests at a time.
	// Fixed here, at wiring time: the Engine exposes no setter, so nothing
	// downstream can raise or remove it. A caller who genuinely needs every
	// occurrence uses the CLI or desktop build, which is unbounded.
	e, err := engine.Open(dbPath, engine.WithMaxResults(engine.DefaultPublicMaxResults))
	if err != nil {
		log.Fatalf("open engine: %v", err)
	}
	defer e.Close()

	handler := newHandler(e)

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()

	log.Printf("orthotomeo-web-cloud listening on :%s (public)", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("serve: %v", err)
	}
}
