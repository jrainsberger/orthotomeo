package main

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientIP(t *testing.T) {
	tests := []struct {
		name       string
		xff        string
		remoteAddr string
		want       string
	}{
		{
			name:       "trusts the last XFF entry, not the client-supplied first one",
			xff:        "1.2.3.4, 5.6.7.8",
			remoteAddr: "9.9.9.9:1234",
			want:       "5.6.7.8",
		},
		{
			name:       "single XFF entry",
			xff:        "5.6.7.8",
			remoteAddr: "9.9.9.9:1234",
			want:       "5.6.7.8",
		},
		{
			name:       "no XFF header falls back to RemoteAddr host",
			xff:        "",
			remoteAddr: "9.9.9.9:1234",
			want:       "9.9.9.9",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				r.Header.Set("X-Forwarded-For", tt.xff)
			}
			if got := clientIP(r); got != tt.want {
				t.Errorf("clientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWindowAllow(t *testing.T) {
	now := time.Now()
	w := &window{limit: 2, period: time.Hour, start: now}

	if !w.allow(now) {
		t.Fatal("1st request should be allowed")
	}
	if !w.allow(now) {
		t.Fatal("2nd request should be allowed")
	}
	if w.allow(now) {
		t.Fatal("3rd request should be denied - limit is 2")
	}
	if !w.allow(now.Add(time.Hour + time.Second)) {
		t.Fatal("request after the period elapses should reset and be allowed")
	}
}

func TestRateLimiterPerIP(t *testing.T) {
	rl := newRateLimiter(2, time.Hour, 1000, 24*time.Hour)
	handler := rl.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/verse", nil)
		r.RemoteAddr = "1.2.3.4:5555"
		return r
	}

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req())
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: got status %d, want 200", i+1, w.Code)
		}
	}

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req())
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("3rd request: got status %d, want 429", w.Code)
	}

	// A different IP is unaffected by the first IP's limit.
	other := httptest.NewRequest(http.MethodGet, "/verse", nil)
	other.RemoteAddr = "9.9.9.9:5555"
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, other)
	if w2.Code != http.StatusOK {
		t.Fatalf("other IP: got status %d, want 200", w2.Code)
	}
}

func TestRateLimiterGlobalCap(t *testing.T) {
	rl := newRateLimiter(1000, time.Hour, 1, 24*time.Hour)
	handler := rl.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req1 := httptest.NewRequest(http.MethodGet, "/verse", nil)
	req1.RemoteAddr = "1.1.1.1:1"
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("1st request: got status %d, want 200", w1.Code)
	}

	// A different IP still trips the global cap - it's not per-source.
	req2 := httptest.NewRequest(http.MethodGet, "/verse", nil)
	req2.RemoteAddr = "2.2.2.2:1"
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("2nd request (different IP): got status %d, want 429", w2.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	called := false
	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodGet, "/verse", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if !called {
		t.Error("GET request should reach the wrapped handler")
	}
	for header, want := range map[string]string{
		"X-Content-Type-Options":      "nosniff",
		"Referrer-Policy":             "no-referrer",
		"Content-Security-Policy":     "default-src 'self'",
		"Access-Control-Allow-Origin": "*",
	} {
		if got := w.Header().Get(header); got != want {
			t.Errorf("header %s = %q, want %q", header, got, want)
		}
	}
}

func TestSecurityHeadersHandlesPreflight(t *testing.T) {
	called := false
	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	r := httptest.NewRequest(http.MethodOptions, "/verse", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if called {
		t.Error("OPTIONS preflight should be answered directly, never reaching the wrapped handler")
	}
	if w.Code != http.StatusNoContent {
		t.Errorf("got status %d, want 204", w.Code)
	}
}

func TestGzipMiddlewareCompressesAPIResponses(t *testing.T) {
	const body = `{"hello":"world"}`
	handler := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))

	r := httptest.NewRequest(http.MethodGet, "/verse", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	gr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatalf("response body isn't valid gzip: %v", err)
	}
	defer gr.Close()
	got, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("reading gzip body: %v", err)
	}
	if string(got) != body {
		t.Errorf("decompressed body = %q, want %q", got, body)
	}
}

func TestGzipMiddlewareSkipsStatic(t *testing.T) {
	handler := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("plain"))
	}))

	r := httptest.NewRequest(http.MethodGet, "/static/app.js", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("Content-Encoding = %q, want unset for /static/", got)
	}
	if w.Body.String() != "plain" {
		t.Errorf("body = %q, want unmodified passthrough", w.Body.String())
	}
}

func TestLoggingMiddlewareCapturesStatus(t *testing.T) {
	handler := loggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	r := httptest.NewRequest(http.MethodGet, "/verse", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r) // just exercising the wrapper doesn't panic/deadlock; log output isn't asserted
	if w.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want 400 (logging middleware must not alter the response)", w.Code)
	}
}
