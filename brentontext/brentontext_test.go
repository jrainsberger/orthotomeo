package brentontext_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/brentontext"
	"github.com/jrainsberger/orthotomeo/sources"
	"github.com/jrainsberger/orthotomeo/store"
)

// Mirrors the real Brenton chapter-file shape: a <div class="main"> body
// with verse spans, an inline footnote marker (notemark + popup, dropped),
// a trailing nav list and footnote block (both excluded from the verse
// text), and a chapterlabel div (id="V0", not a real verse).
const fixtureGEN01 = `<!DOCTYPE html><html><body>
<ul class='tnav'><li><a href='index.htm'>Genesis</a></li></ul>
<div class="main">
<div class='chapterlabel' id="V0"> 1</div><div class='p'> <span class="verse" id="V1">1&#160;</span>In the beginning God made the heaven and the earth.   <span class="verse" id="V2">2&#160;</span>But the earth was unsightly<a href="#FN1" class="notemark">*<span class="popup">Gr. note text.</span></a> and unfurnished.   </div>
<ul class='tnav'><li><a href='index.htm'>Genesis</a></li></ul>
<div class="footnote">
<p class="f" id="FN1"><span class="notemark">*</span>Gr. note text.</p>
</div>
</body></html>`

// 1Ki.2's real "Miscellanies" doublet: Brenton prints "35a"/"35b" as the
// visible verse label, but the span's id="VN" attribute is just a
// sequential HTML anchor (id="V36" for label "35a", id="V37" for "35b")
// that does NOT match the printed number - confirmed against the real
// corpus file. The next real verse 36 then reuses id="V36" again.
const fixtureDoublet = `<!DOCTYPE html><html><body>
<div class="main">
<div class='chapterlabel' id="V0"> 2</div><div class='p'> <span class="verse" id="V36">35a&#160;</span>first half.   <span class="verse" id="V37">35b&#160;</span>second half.   </div><div class='p'> <span class="verse" id="V36">36&#160;</span>the real verse 36.   </div>
<div class="footnote"></div>
</body></html>`

// A book-index page (e.g. the real "PSA000.htm") has no verse spans at all.
const fixtureIndexPage = `<!DOCTYPE html><html><body>
<div class="main">
<div class="toc"><a href="PSA001.htm">Psalms</a></div>
</div>
</body></html>`

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
	return db
}

func TestLoadStripsMarkupAndFootnotes(t *testing.T) {
	db := setup(t)

	code, chapter, inserted, loaded, err := brentontext.Load(db, strings.NewReader(fixtureGEN01), "GEN01.htm")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !loaded || code != "GEN" || chapter != 1 || inserted != 2 {
		t.Fatalf("loaded=%v code=%q chapter=%d inserted=%d, want true/GEN/1/2", loaded, code, chapter, inserted)
	}

	var text string
	if err := db.QueryRow(`SELECT text FROM verse_text WHERE native_ref = 'Genesis 1:1'`).Scan(&text); err != nil {
		t.Fatalf("query: %v", err)
	}
	want := "In the beginning God made the heaven and the earth."
	if text != want {
		t.Errorf("Gen.1.1 = %q, want %q", text, want)
	}

	var v2 string
	db.QueryRow(`SELECT text FROM verse_text WHERE native_ref = 'Genesis 1:2'`).Scan(&v2)
	if strings.Contains(v2, "Gr. note text") || strings.ContainsAny(v2, "<>") {
		t.Errorf("Gen.1.2 = %q, footnote/markup leaked", v2)
	}
}

func TestLoadUsesItsOwnVersification(t *testing.T) {
	db := setup(t)
	if _, _, _, _, err := brentontext.Load(db, strings.NewReader(fixtureGEN01), "GEN01.htm"); err != nil {
		t.Fatalf("load: %v", err)
	}

	var versification string
	err := db.QueryRow(`
		SELECT v.versification FROM verses v
		JOIN verse_text vt ON vt.verse_id = v.id
		WHERE vt.native_ref = 'Genesis 1:1'`).Scan(&versification)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if versification != brentontext.Versification {
		t.Errorf("versification = %q, want %q", versification, brentontext.Versification)
	}

	// A canonical-spine verse with the same (book, chapter, verse) does NOT
	// exist yet (BuildSpine was never called) - proving Brenton built its
	// own row rather than colliding with or depending on the canonical one.
	var canonicalCount int
	db.QueryRow(`SELECT COUNT(*) FROM verses WHERE versification = 'canonical'`).Scan(&canonicalCount)
	if canonicalCount != 0 {
		t.Errorf("canonical verse rows = %d, want 0 (none built in this test)", canonicalCount)
	}
}

func TestLoadMergesLetteredDoublets(t *testing.T) {
	db := setup(t)

	_, _, inserted, loaded, err := brentontext.Load(db, strings.NewReader(fixtureDoublet), "1KI02.htm")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// Three spans (35a, 35b, 36) collapse to two verse rows (35, 36).
	if !loaded || inserted != 2 {
		t.Fatalf("loaded=%v inserted=%d, want true/2", loaded, inserted)
	}

	var v35, v36 string
	db.QueryRow(`SELECT text FROM verse_text WHERE native_ref = 'I Kings 2:35'`).Scan(&v35)
	db.QueryRow(`SELECT text FROM verse_text WHERE native_ref = 'I Kings 2:36'`).Scan(&v36)
	if v35 != "first half. second half." {
		t.Errorf("v35 (merged doublet) = %q, want %q", v35, "first half. second half.")
	}
	if v36 != "the real verse 36." {
		t.Errorf("v36 = %q, want %q", v36, "the real verse 36.")
	}
}

func TestLoadResolvesBookAliases(t *testing.T) {
	db := setup(t)

	code, _, inserted, loaded, err := brentontext.Load(db, strings.NewReader(fixtureGEN01), "DAG01.htm")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !loaded || code != "DAG" || inserted != 2 {
		t.Fatalf("loaded=%v code=%q inserted=%d, want true/DAG/2", loaded, code, inserted)
	}

	var bookCode string
	err = db.QueryRow(`
		SELECT b.code FROM verse_text vt
		JOIN verses v ON v.id = vt.verse_id
		JOIN books b ON b.id = v.book_id
		WHERE vt.native_ref = 'Daniel 1:1'`).Scan(&bookCode)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if bookCode != "DAN" {
		t.Errorf("DAG01.htm loaded under book code %q, want DAN", bookCode)
	}
}

func TestLoadSkipsEzraCombinedBook(t *testing.T) {
	db := setup(t)

	code, _, inserted, loaded, err := brentontext.Load(db, strings.NewReader(fixtureGEN01), "EZR01.htm")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded {
		t.Error("loaded = true for EZR.htm, want false (combined-book identity unresolved)")
	}
	if code != "EZR" {
		t.Errorf("code = %q, want EZR", code)
	}
	if inserted != 0 {
		t.Errorf("inserted = %d, want 0", inserted)
	}

	var rows int
	db.QueryRow(`SELECT COUNT(*) FROM verse_text`).Scan(&rows)
	if rows != 0 {
		t.Errorf("verse_text has %d rows, want 0", rows)
	}
}

func TestLoadSkipsNonChapterAndIndexFiles(t *testing.T) {
	db := setup(t)

	tests := []struct {
		name     string
		filename string
		content  string
	}{
		{"bare book-index filename (no chapter digits)", "GEN.htm", fixtureGEN01},
		{"front matter / copyright page", "copyright.htm", fixtureGEN01},
		{"chapter-shaped filename but no verse spans", "PSA000.htm", fixtureIndexPage},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, inserted, loaded, err := brentontext.Load(db, strings.NewReader(tc.content), tc.filename)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if loaded || inserted != 0 {
				t.Errorf("loaded=%v inserted=%d, want false/0", loaded, inserted)
			}
		})
	}
}
