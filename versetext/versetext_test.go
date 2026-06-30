package versetext_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/sources"
	"github.com/jrainsberger/orthotomeo/store"
	"github.com/jrainsberger/orthotomeo/verses"
	"github.com/jrainsberger/orthotomeo/versetext"
)

const miniSpine = `{"books":[
  {"name":"Genesis","chapters":[{"chapter":1,"verses":[{"verse":1},{"verse":2}]}]},
  {"name":"John","chapters":[{"chapter":3,"verses":[{"verse":16}]}]}
]}`

const miniKJV = `{"books":[
  {"name":"Genesis","chapters":[{"chapter":1,"verses":[
    {"verse":1,"text":"In the beginning God created the heaven and the earth."},
    {"verse":2,"text":"And the earth was without form, and void."}
  ]}]},
  {"name":"John","chapters":[{"chapter":3,"verses":[
    {"verse":16,"text":"For God so loved the world..."}
  ]}]}
]}`

func setup(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.ApplySchema(db); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if _, err := sources.Seed(db); err != nil {
		t.Fatalf("seed sources: %v", err)
	}
	if _, _, err := books.Seed(db); err != nil {
		t.Fatalf("seed books: %v", err)
	}
	if _, err := verses.BuildSpine(db, strings.NewReader(miniSpine)); err != nil {
		t.Fatalf("build spine: %v", err)
	}
	return db
}

func TestLoadInsertsEveryVerseVerbatim(t *testing.T) {
	db := setup(t)

	n, err := versetext.Load(db, strings.NewReader(miniKJV), "KJV")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if n != 3 {
		t.Errorf("inserted %d, want 3", n)
	}

	var text string
	err = db.QueryRow(`
		SELECT vt.text FROM verse_text vt
		JOIN verses v ON v.id = vt.verse_id
		JOIN books b ON b.id = v.book_id
		WHERE b.full_name = 'John' AND v.chapter = 3 AND v.verse = 16`).Scan(&text)
	if err != nil {
		t.Fatalf("query John 3:16: %v", err)
	}
	want := "For God so loved the world..."
	if text != want {
		t.Errorf("John.3.16 = %q, want %q", text, want)
	}
}

func TestLoadCarriesSourceAndNativeRef(t *testing.T) {
	db := setup(t)
	if _, err := versetext.Load(db, strings.NewReader(miniKJV), "KJV"); err != nil {
		t.Fatalf("load: %v", err)
	}

	var bad int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM verse_text vt
		JOIN sources s ON s.id = vt.source_id
		WHERE s.code <> 'KJV'`).Scan(&bad); err != nil {
		t.Fatalf("provenance check: %v", err)
	}
	if bad != 0 {
		t.Errorf("%d rows not attributed to KJV", bad)
	}

	var ref string
	if err := db.QueryRow(`SELECT native_ref FROM verse_text WHERE text LIKE 'In the beginning%'`).Scan(&ref); err != nil {
		t.Fatalf("query native_ref: %v", err)
	}
	if ref != "Genesis 1:1" {
		t.Errorf("native_ref = %q, want %q", ref, "Genesis 1:1")
	}
}

func TestLoadFailsOnUnresolvableVerse(t *testing.T) {
	db := setup(t)
	// Exodus is not in the mini spine - must fail loudly, not skip silently
	// (KJV/ASV define the canonical spine, unlike crossrefs targets).
	const badDoc = `{"books":[{"name":"Exodus","chapters":[{"chapter":1,"verses":[{"verse":1,"text":"x"}]}]}]}`

	if _, err := versetext.Load(db, strings.NewReader(badDoc), "KJV"); err == nil {
		t.Fatal("load with unresolvable verse: want error, got nil")
	}

	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM verse_text`).Scan(&rows); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 0 {
		t.Errorf("rows = %d, want 0 (failed load must not partially land)", rows)
	}
}
