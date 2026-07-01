package engine

import (
	"path/filepath"
	"testing"

	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/sources"
	"github.com/jrainsberger/orthotomeo/store"
)

// TestOpenRejectsWrites is a white-box test (package engine, not
// engine_test) specifically to reach the unexported db field - the public
// API never exposes it, so this is the only place a write attempt can be
// made at all, proving Open's two independent read-only guards actually
// hold rather than just trusting the URI parameter.
func TestOpenRejectsWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	build, err := store.Open(path)
	if err != nil {
		t.Fatalf("build open: %v", err)
	}
	if err := store.ApplySchema(build); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if _, err := sources.Seed(build); err != nil {
		t.Fatalf("seed sources: %v", err)
	}
	if _, _, err := books.Seed(build); err != nil {
		t.Fatalf("seed books: %v", err)
	}
	if err := build.Close(); err != nil {
		t.Fatalf("close build handle: %v", err)
	}

	e, err := Open(path)
	if err != nil {
		t.Fatalf("engine.Open: %v", err)
	}
	defer e.Close()

	_, err = e.db.Exec(`INSERT INTO sources (code, full_name, language, type, license, shippable) VALUES ('X','X','en','translation','X',1)`)
	if err == nil {
		t.Fatal("expected a write attempt to fail against a read-only engine connection")
	}
}
