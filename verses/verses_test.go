package verses_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/store"
	"github.com/jrainsberger/orthotomeo/verses"
)

// miniKJV is an inline KJV.json-shaped fixture: two books using the actual
// JSON naming (Roman numerals), so the test proves name-en resolution too.
const miniKJV = `{
  "translation": "test",
  "books": [
    {"name": "I Samuel", "chapters": [
      {"chapter": 1, "verses": [{"verse": 1}, {"verse": 2}]},
      {"chapter": 2, "verses": [{"verse": 1}]}
    ]},
    {"name": "Mark", "chapters": [
      {"chapter": 1, "verses": [{"verse": 1}]}
    ]}
  ]
}`

func newSpine(t *testing.T) (*sql.DB, int) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.ApplySchema(db); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if _, _, err := books.Seed(db); err != nil {
		t.Fatalf("seed books: %v", err)
	}
	n, err := verses.BuildSpine(db, strings.NewReader(miniKJV))
	if err != nil {
		t.Fatalf("build spine: %v", err)
	}
	return db, n
}

func TestBuildSpineCount(t *testing.T) {
	db, n := newSpine(t)
	if n != 4 {
		t.Errorf("BuildSpine returned %d, want 4", n)
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM verses`).Scan(&rows); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 4 {
		t.Errorf("verses table has %d rows, want 4", rows)
	}
}

func TestResolveAcrossSchemes(t *testing.T) {
	db, _ := newSpine(t)

	// The same canonical verse is reachable from any scheme's book code.
	osis, err := verses.NewResolver(db, "osis", verses.Canonical)
	if err != nil {
		t.Fatalf("osis resolver: %v", err)
	}
	dotted, err := verses.NewResolver(db, "dotted", verses.Canonical)
	if err != nil {
		t.Fatalf("dotted resolver: %v", err)
	}

	idOSIS, err := osis.Resolve("1Sam.1.2")
	if err != nil {
		t.Fatalf("resolve osis 1Sam.1.2: %v", err)
	}
	idDotted, err := dotted.Resolve("1Sa.1.2")
	if err != nil {
		t.Fatalf("resolve dotted 1Sa.1.2: %v", err)
	}
	if idOSIS != idDotted {
		t.Errorf("1Sam.1.2 (osis) = %d but 1Sa.1.2 (dotted) = %d", idOSIS, idDotted)
	}
}

func TestResolveUnknownVerse(t *testing.T) {
	db, _ := newSpine(t)
	r, err := verses.NewResolver(db, "osis", verses.Canonical)
	if err != nil {
		t.Fatalf("resolver: %v", err)
	}

	cases := []string{
		"1Sam.99.1", // chapter out of range
		"Gen.1.1",   // book not in this mini-spine
		"Mark.1",    // malformed
	}
	for _, ref := range cases {
		if _, err := r.Resolve(ref); !errors.Is(err, verses.ErrUnknownVerse) {
			t.Errorf("Resolve(%q) err = %v, want ErrUnknownVerse", ref, err)
		}
	}
}

func TestResolverScopedToVersification(t *testing.T) {
	db, _ := newSpine(t)
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	bookID, err := books.Resolve(tx, "osis", "1Sam")
	if err != nil {
		t.Fatalf("resolve book: %v", err)
	}
	// A different versification can carry a DIFFERENT verse count for the
	// same (book, chapter) without colliding with the canonical row.
	if _, err := verses.GetOrCreateVerse(tx, "lxx-brenton", bookID, 1, 1); err != nil {
		t.Fatalf("get-or-create: %v", err)
	}
	if _, err := verses.GetOrCreateVerse(tx, "lxx-brenton", bookID, 1, 99); err != nil {
		t.Fatalf("get-or-create: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	canonical, err := verses.NewResolver(db, "osis", verses.Canonical)
	if err != nil {
		t.Fatalf("canonical resolver: %v", err)
	}
	if _, err := canonical.Resolve("1Sam.1.99"); !errors.Is(err, verses.ErrUnknownVerse) {
		t.Errorf("canonical resolver sees lxx-brenton-only verse 1Sam.1.99: err = %v", err)
	}

	brenton, err := verses.NewResolver(db, "osis", "lxx-brenton")
	if err != nil {
		t.Fatalf("brenton resolver: %v", err)
	}
	if _, err := brenton.Resolve("1Sam.1.99"); err != nil {
		t.Errorf("brenton resolver should see its own verse 1Sam.1.99: %v", err)
	}
	if _, err := brenton.Resolve("Mark.1.1"); !errors.Is(err, verses.ErrUnknownVerse) {
		t.Errorf("brenton resolver sees canonical-only verse Mark.1.1: err = %v", err)
	}
}

func TestGetOrCreateVerseIsIdempotent(t *testing.T) {
	db, _ := newSpine(t)
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()
	bookID, err := books.Resolve(tx, "osis", "1Sam")
	if err != nil {
		t.Fatalf("resolve book: %v", err)
	}

	id1, err := verses.GetOrCreateVerse(tx, "lxx-brenton", bookID, 1, 1)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	id2, err := verses.GetOrCreateVerse(tx, "lxx-brenton", bookID, 1, 1)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if id1 != id2 {
		t.Errorf("GetOrCreateVerse returned different ids on repeat calls: %d != %d", id1, id2)
	}
}
