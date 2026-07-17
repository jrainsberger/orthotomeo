package main

import (
	"compress/gzip"
	"encoding/json"
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

func (rl *rateLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		if !rl.global.allow(now) {
			rateLimited(w, "global request cap reached - try again later")
			return
		}
		ip := clientIP(r)
		if !rl.bucketFor(ip, now).allow(now) {
			rateLimited(w, "per-IP request cap reached - try again later")
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
