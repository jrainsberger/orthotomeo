package webtext_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/sources"
	"github.com/jrainsberger/orthotomeo/store"
	"github.com/jrainsberger/orthotomeo/verses"
	"github.com/jrainsberger/orthotomeo/webtext"
)

const miniSpine = `{"books":[
  {"name":"Genesis","chapters":[{"chapter":1,"verses":[{"verse":1}]}]},
  {"name":"Psalms","chapters":[{"chapter":3,"verses":[{"verse":1},{"verse":2}]}]}
]}`

// Mirrors real WEB USFM: front matter (skipped), a footnote and a \w/strong
// wrapper on one verse, a words-of-Jesus \+w span, and a poetry verse split
// across \q1/\q2 lines with no new \v (verse 2 also carries a \qs Selah).
const fixtureGEN = "\\id GEN World English Bible (WEB)\n" +
	"\\ide UTF-8\n" +
	"\\h Genesis\n" +
	"\\toc1 The First Book of Moses\n" +
	"\\mt1 Genesis\n" +
	"\\c 1\n" +
	"\\p\n" +
	"\\v 1 \\w In|strong=\"H8064\"\\w* \\w the|strong=\"H1254\"\\w* \\w beginning|strong=\"H7225\"\\w*, " +
	"\\w God|strong=\"H8064\"\\w*\\f + \\fr 1:1 \\ft footnote text dropped entirely.\\f* " +
	"\\w created|strong=\"H1254\"\\w* \\w the|strong=\"H1254\"\\w* \\w heavens|strong=\"H8064\"\\w*.\n"

const fixturePSA = "\\id PSA World English Bible (WEB)\n" +
	"\\c 3\n" +
	"\\d A Psalm by David, when he fled from Absalom his son.\n" +
	"\\q1\n" +
	"\\v 1 \\w Yahweh|strong=\"H3068\"\\w*, how my adversaries have increased!\n" +
	"\\q2 Many are those who rise up against me.\n" +
	"\\q1\n" +
	"\\v 2 Many there are who say of my soul, \\wj “There is no help for him in God.”\\wj* \\qs Selah.\\qs*\n"

// Front matter file: \id is FRT, not a canonical 66-book code, so it must
// be skipped (loaded=false), not an error.
const fixtureFRT = "\\id FRT World English Bible (WEB)\n\\h Front Matter\n\\ip Some preface text.\n"

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

func TestLoadStripsMarkupAndFootnotes(t *testing.T) {
	db := setup(t)

	code, inserted, skipped, loaded, err := webtext.Load(db, strings.NewReader(fixtureGEN))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !loaded {
		t.Fatal("loaded = false, want true for canonical book GEN")
	}
	if code != "GEN" {
		t.Errorf("code = %q, want GEN", code)
	}
	if inserted != 1 || skipped != 0 {
		t.Errorf("inserted=%d skipped=%d, want 1/0", inserted, skipped)
	}

	var text string
	if err := db.QueryRow(`SELECT text FROM verse_text WHERE native_ref = 'Genesis 1:1'`).Scan(&text); err != nil {
		t.Fatalf("query: %v", err)
	}
	want := "In the beginning, God created the heavens."
	if text != want {
		t.Errorf("text = %q, want %q", text, want)
	}
}

func TestLoadJoinsPoetryLinesAndKeepsSelah(t *testing.T) {
	db := setup(t)

	_, inserted, skipped, loaded, err := webtext.Load(db, strings.NewReader(fixturePSA))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !loaded || inserted != 2 || skipped != 0 {
		t.Fatalf("loaded=%v inserted=%d skipped=%d, want true/2/0", loaded, inserted, skipped)
	}

	var v1, v2 string
	db.QueryRow(`SELECT text FROM verse_text WHERE native_ref = 'Psalms 3:1'`).Scan(&v1)
	db.QueryRow(`SELECT text FROM verse_text WHERE native_ref = 'Psalms 3:2'`).Scan(&v2)

	wantV1 := "Yahweh, how my adversaries have increased! Many are those who rise up against me."
	if v1 != wantV1 {
		t.Errorf("v1 = %q, want %q", v1, wantV1)
	}
	wantV2 := "Many there are who say of my soul, “There is no help for him in God.” Selah."
	if v2 != wantV2 {
		t.Errorf("v2 = %q, want %q", v2, wantV2)
	}

	// The \d superscription has no \v of its own and must not leak into v1.
	if strings.Contains(v1, "Absalom") {
		t.Error("descriptive title text leaked into verse 1")
	}
}

func TestLoadSkipsNonCanonicalFiles(t *testing.T) {
	db := setup(t)

	code, inserted, skipped, loaded, err := webtext.Load(db, strings.NewReader(fixtureFRT))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded {
		t.Error("loaded = true for front matter, want false")
	}
	if code != "FRT" {
		t.Errorf("code = %q, want FRT", code)
	}
	if inserted != 0 || skipped != 0 {
		t.Errorf("inserted=%d skipped=%d, want 0/0", inserted, skipped)
	}

	var rows int
	db.QueryRow(`SELECT COUNT(*) FROM verse_text`).Scan(&rows)
	if rows != 0 {
		t.Errorf("verse_text has %d rows, want 0", rows)
	}
}

func TestLoadCountsUnresolvableVerseAsSkipped(t *testing.T) {
	db := setup(t)
	// Genesis 1:2 is not in the mini spine (only 1:1 is) - a documented
	// WEB/KJV versification divergence must be skipped, not fail the load.
	const fixture = "\\id GEN World English Bible (WEB)\n\\c 1\n" +
		"\\v 1 In the beginning.\n\\v 2 Unresolvable extra verse.\n"

	_, inserted, skipped, loaded, err := webtext.Load(db, strings.NewReader(fixture))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !loaded || inserted != 1 || skipped != 1 {
		t.Errorf("loaded=%v inserted=%d skipped=%d, want true/1/1", loaded, inserted, skipped)
	}
}
