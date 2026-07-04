package main

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/sources"
	"github.com/jrainsberger/orthotomeo/store"
)

// buildFixture mirrors the same small real-shape fixture httpapi_test.go
// and engine_test.go use.
func buildFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("build open: %v", err)
	}
	if err := store.ApplySchema(db); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if _, err := sources.Seed(db); err != nil {
		t.Fatalf("seed sources: %v", err)
	}
	if _, _, err := books.Seed(db); err != nil {
		t.Fatalf("seed books: %v", err)
	}

	var matBook int64
	if err := db.QueryRow(`SELECT id FROM books WHERE code = 'MAT'`).Scan(&matBook); err != nil {
		t.Fatalf("book lookup: %v", err)
	}
	res, err := db.Exec(`INSERT INTO verses (versification, book_id, chapter, verse) VALUES ('canonical', ?, 26, 28)`, matBook)
	if err != nil {
		t.Fatalf("insert verse: %v", err)
	}
	verseID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO verse_text (verse_id, source_id, native_ref, text)
		VALUES (?, (SELECT id FROM sources WHERE code = 'KJV'), 'Mat.26.28', 'blood of the new testament')`, verseID); err != nil {
		t.Fatalf("insert verse_text: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close build handle: %v", err)
	}
	return path
}

// TestStartServerBindsLoopbackAndServes is T28's core acceptance criterion,
// checked end to end: launching starts a real, working search UI (proven
// here by hitting a real endpoint through it), not just "a listener opens."
func TestStartServerBindsLoopbackAndServes(t *testing.T) {
	srv, err := startServer(buildFixture(t))
	if err != nil {
		t.Fatalf("startServer: %v", err)
	}
	defer srv.shutdown(context.Background())

	addr := srv.ln.Addr().String()
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("listener bound to %q, want a 127.0.0.1 address", addr)
	}

	res, err := http.Get("http://" + addr + "/verse?book=MAT&chapter=26&verse=28&editions=KJV")
	if err != nil {
		t.Fatalf("GET through the started server: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 (the server behind this launcher must actually work)", res.StatusCode)
	}
}

// TestPortMatchesListener confirms port() reports the real OS-assigned
// port, not a stale or default value - srv.url() is built from it and is
// what the browser actually gets opened to.
func TestPortMatchesListener(t *testing.T) {
	srv, err := startServer(buildFixture(t))
	if err != nil {
		t.Fatalf("startServer: %v", err)
	}
	defer srv.shutdown(context.Background())

	wantPort := strconv.Itoa(srv.ln.Addr().(*net.TCPAddr).Port)
	if srv.port() != wantPort {
		t.Errorf("port() = %q, want %q", srv.port(), wantPort)
	}
	if !strings.Contains(srv.url(), wantPort) {
		t.Errorf("url() = %q, want it to contain port %q", srv.url(), wantPort)
	}
	if !strings.HasPrefix(srv.url(), "http://orthotomeo.localhost:") {
		t.Errorf("url() = %q, want the *.localhost host (RFC 6761), not a bare IP", srv.url())
	}
}

// TestShutdownReleasesThePort is T28's other explicit acceptance criterion:
// "port released, no orphan process." Two independent checks: (1) the
// shut-down server refuses new connections outright - no orphan listener
// answering requests; (2) the OS eventually allows rebinding the exact same
// port. (2) is checked with a short bounded retry, not a single immediate
// attempt - Windows can hold a just-closed TCP port in TIME_WAIT for a
// brief moment even after a clean Shutdown, so an instant re-listen can
// transiently fail without the port actually being leaked; retrying for up
// to ~1s distinguishes a real leak (still fails) from that normal OS delay.
func TestShutdownReleasesThePort(t *testing.T) {
	srv, err := startServer(buildFixture(t))
	if err != nil {
		t.Fatalf("startServer: %v", err)
	}
	addr := srv.ln.Addr().String()

	if err := srv.shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	if _, err := http.Get("http://" + addr + "/"); err == nil {
		t.Error("expected the shut-down server to refuse new connections")
	}

	var lastErr error
	for i := 0; i < 10; i++ {
		ln2, err := net.Listen("tcp", addr)
		if err == nil {
			ln2.Close()
			return
		}
		lastErr = err
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("re-listen on %s never succeeded within 1s of shutdown: %v (port was not released)", addr, lastErr)
}

func TestStartServerFailsOnMissingDB(t *testing.T) {
	if _, err := startServer(filepath.Join(t.TempDir(), "does-not-exist.db")); err == nil {
		t.Fatal("expected an error opening a nonexistent DB")
	}
}
