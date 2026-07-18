package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// clientIP returns the real client IP for a request that reached this
// process through Cloud Run's front end. Cloud Run's GFE appends the
// connecting client's IP as the LAST entry of X-Forwarded-For - any prior
// entries are attacker-controlled (a client can set this header to
// anything it likes), so trusting the first entry would let one attacker
// rotate a fake IP per request and bypass the per-IP limiter entirely.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
			return ip
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// window is a rolling-reset request counter: once start is older than
// period the count resets to zero rather than sliding continuously - a
// deliberate approximation (a true sliding window needs a bucketed
// histogram) that's more than adequate for a cost backstop.
type window struct {
	mu     sync.Mutex
	count  int
	start  time.Time
	limit  int
	period time.Duration
}

func (w *window) allow(now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if now.Sub(w.start) >= w.period {
		w.start = now
		w.count = 0
	}
	if w.count >= w.limit {
		return false
	}
	w.count++
	return true
}

// rateLimiter enforces two independent caps: a per-IP cap (abuse from one
// source) and a global cap (a hard cost ceiling regardless of how
// distributed the traffic is - e.g. requests spread across many IPs still
// trips this one).
type rateLimiter struct {
	mu        sync.Mutex
	perIP     map[string]*window
	ipLimit   int
	ipPeriod  time.Duration
	global    *window
	lastSweep time.Time
}

func newRateLimiter(ipLimit int, ipPeriod time.Duration, globalLimit int, globalPeriod time.Duration) *rateLimiter {
	now := time.Now()
	return &rateLimiter{
		perIP:     make(map[string]*window),
		ipLimit:   ipLimit,
		ipPeriod:  ipPeriod,
		global:    &window{limit: globalLimit, period: globalPeriod, start: now},
		lastSweep: now,
	}
}

// bucketFor returns ip's window, creating it on first use, and
// opportunistically evicts windows untouched for 2x the IP period so a
// long-running process doesn't accumulate one entry per distinct IP
// forever - public traffic means unbounded IP cardinality over the
// process's lifetime.
func (rl *rateLimiter) bucketFor(ip string, now time.Time) *window {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if now.Sub(rl.lastSweep) >= rl.ipPeriod {
		for k, w := range rl.perIP {
			w.mu.Lock()
			stale := now.Sub(w.start) >= 2*rl.ipPeriod
			w.mu.Unlock()
			if stale {
				delete(rl.perIP, k)
			}
		}
		rl.lastSweep = now
	}

	w, ok := rl.perIP[ip]
	if !ok {
		w = &window{limit: rl.ipLimit, period: rl.ipPeriod, start: now}
		rl.perIP[ip] = w
	}
	return w
}

// allow reports whether r may proceed under this limiter, charging both the
// global window and r's per-IP window, and names which cap refused it.
// Split out from middleware so a caller can first decide WHICH budget a
// request should draw on, then charge only that one (see budgets).
func (rl *rateLimiter) allow(r *http.Request) (bool, string) {
	now := time.Now()
	if !rl.global.allow(now) {
		return false, "global request cap reached - try again later"
	}
	if !rl.bucketFor(clientIP(r), now).allow(now) {
		return false, "per-IP request cap reached - try again later"
	}
	return true, ""
}

func (rl *rateLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ok, reason := rl.allow(r); !ok {
			rateLimited(w, reason)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func rateLimited(w http.ResponseWriter, reason string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Retry-After", "3600")
	w.WriteHeader(http.StatusTooManyRequests)
	json.NewEncoder(w).Encode(map[string]string{"error": reason})
}

// costClass says which budget a request draws on. The distinction is WORK,
// not packets. A page load, a favicon, an MCP handshake and a 404 probe all
// cost essentially nothing; a query that reaches the engine costs real
// memory and CPU. Charging them identically is what let liveness bots eat
// a study budget (~780/day from one poller that never invokes a tool), and
// would let the web UI's own static assets do the same - one page load is
// several requests before the visitor has asked anything.
type costClass int

const (
	classFree costClass = iota
	classWork
)

// engineRoutes are the REST paths that reach the engine. Everything else
// this server exposes - the index, /static/, /favicon.ico, /robots.txt,
// /books (autocomplete data the UI fetches on every page load) and any 404
// probe - does no engine work and must never spend study budget.
var engineRoutes = map[string]bool{
	"/verse":       true,
	"/passage":     true,
	"/concord":     true,
	"/parse":       true,
	"/attest":      true,
	"/interlinear": true,
	"/define":      true,
}

// classify decides which budget r draws on. /mcp is judged by its JSON-RPC
// method rather than its path: a liveness poller sending only initialize or
// tools/list does no engine work, while tools/call does.
func classify(r *http.Request) costClass {
	if r.URL.Path == "/mcp" {
		if invokesTool(r) {
			return classWork
		}
		return classFree
	}
	if engineRoutes[r.URL.Path] {
		return classWork
	}
	return classFree
}

// maxRPCPeek bounds how much of an /mcp body is buffered to classify it. A
// JSON-RPC envelope names its method within the first few hundred bytes;
// capping the peek stops a large or hostile body being read into memory
// just to decide what to charge for it.
const maxRPCPeek = 4 << 10

// invokesTool reports whether an /mcp request actually calls a tool. It
// buffers a bounded prefix and restores the body, so the MCP handler
// downstream still reads a complete request.
//
// It fails CLOSED - charging as work - in every uncertain case: a read
// error, or a body longer than the peek in which no tool call was seen.
// Without that, an attacker could push the method field past the peek
// window behind a huge params object and buy unlimited free tool calls.
// Over-charging a cheap request is harmless; under-charging an expensive
// one is the whole vulnerability.
func invokesTool(r *http.Request) bool {
	if r.Body == nil {
		return false
	}
	prefix, err := io.ReadAll(io.LimitReader(r.Body, maxRPCPeek+1))
	if err != nil {
		return true
	}
	r.Body = restoredBody{Reader: io.MultiReader(bytes.NewReader(prefix), r.Body), Closer: r.Body}

	if bytes.Contains(prefix, []byte("tools/call")) {
		return true
	}
	return len(prefix) > maxRPCPeek // truncated and undecided - charge it
}

// restoredBody re-serves the bytes already consumed for classification,
// then the remainder of the original body, while still closing the original.
type restoredBody struct {
	io.Reader
	io.Closer
}

// budgets holds the two independent caps. The work budget is the documented
// study allowance, now charged only for requests that reach the engine. The
// flood guard exists solely to stop someone hammering cheap endpoints and is
// deliberately loose enough that a genuine UI session never trips it.
type budgets struct {
	work  *rateLimiter
	flood *rateLimiter
}

func newBudgets() *budgets {
	return &budgets{
		work:  newRateLimiter(ipWorkLimit, ipWorkWindow, globalWorkLimit, globalWorkWindow),
		flood: newRateLimiter(ipFloodLimit, ipFloodWindow, globalFloodLimit, globalFloodWindow),
	}
}

// middleware charges every request against the flood guard, and only
// engine-touching requests against the study budget.
func (b *budgets) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ok, reason := b.flood.allow(r); !ok {
			rateLimited(w, reason)
			return
		}
		if classify(r) == classWork {
			if ok, reason := b.work.allow(r); !ok {
				rateLimited(w, reason+" - this public instance is cost-bounded; "+
					"run the CLI or desktop build locally for unbounded study")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// securityHeaders adds headers appropriate for a publicly-reachable,
// read-only JSON API plus its static reading-view UI, and answers CORS
// preflight directly - a preflight isn't a real API call, so it's handled
// here, outside the rate limiter, rather than consuming budget from it.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		// The static UI (index.html/app.js/style.css) only ever loads
		// same-origin resources and fetches same-origin JSON endpoints -
		// default-src 'self' costs nothing here and is real defense in
		// depth against XSS if a future change ever introduces one.
		h.Set("Content-Security-Policy", "default-src 'self'")
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// gzipMiddleware compresses API responses. It skips /static/ - the
// embedded file server sets its own Content-Length and supports Range
// requests, both of which a naive gzip wrapper would silently break.
func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/static/") || !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		next.ServeHTTP(gzipResponseWriter{ResponseWriter: w, gz: gz}, r)
	})
}

type gzipResponseWriter struct {
	http.ResponseWriter
	gz *gzip.Writer
}

func (g gzipResponseWriter) Write(b []byte) (int, error) {
	return g.gz.Write(b)
}

// loggingMiddleware writes one line per request to stdout - Cloud Run
// captures stdout as Cloud Logging automatically, no logging client needed.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		log.Printf("%s %s %s %d %s", clientIP(r), r.Method, r.URL.Path, sw.status, time.Since(start))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(status int) {
	sw.status = status
	sw.ResponseWriter.WriteHeader(status)
}
