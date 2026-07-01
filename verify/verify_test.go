package verify_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/sources"
	"github.com/jrainsberger/orthotomeo/store"
	"github.com/jrainsberger/orthotomeo/verify"
)

// setup opens a fresh schema-applied DB seeded with the real sources and
// books registries (so the 39 ot / 27 nt canon and real source codes match
// what DefaultExpectations' queries expect), but no verse/word data - each
// test fills that in deliberately.
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

type bookRow struct {
	id      int64
	code    string
	section string
}

func allBooks(t *testing.T, db *sql.DB) []bookRow {
	t.Helper()
	rows, err := db.Query(`SELECT id, code, section FROM books ORDER BY id`)
	if err != nil {
		t.Fatalf("query books: %v", err)
	}
	defer rows.Close()
	var out []bookRow
	for rows.Next() {
		var b bookRow
		if err := rows.Scan(&b.id, &b.code, &b.section); err != nil {
			t.Fatalf("scan book: %v", err)
		}
		out = append(out, b)
	}
	return out
}

// seedFullCoverage inserts one canonical verse (1:1) per book, plus one
// verse_text row per book for KJV/ASV/WEB and one words row per book for
// TAGNT (nt books) / TAHOT (ot books) - a minimal fixture that satisfies
// every fullCanonCoverage entry. skipSource/skipBook, if non-empty, omits
// exactly one row to simulate a silently dropped book.
func seedFullCoverage(t *testing.T, db *sql.DB, skipSource, skipBook string) {
	t.Helper()
	for _, b := range allBooks(t, db) {
		res, err := db.Exec(`INSERT INTO verses (versification, book_id, chapter, verse) VALUES ('canonical', ?, 1, 1)`, b.id)
		if err != nil {
			t.Fatalf("insert verse %s: %v", b.code, err)
		}
		verseID, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("last insert id: %v", err)
		}

		for _, src := range []string{"KJV", "ASV", "WEB"} {
			if src == skipSource && b.code == skipBook {
				continue
			}
			mustInsertText(t, db, verseID, src, b.code)
		}
		if b.section == "nt" && !(skipSource == "TAGNT" && b.code == skipBook) {
			mustInsertWord(t, db, verseID, "TAGNT", b.code)
		}
		if b.section == "ot" && !(skipSource == "TAHOT" && b.code == skipBook) {
			mustInsertWord(t, db, verseID, "TAHOT", b.code)
		}
	}
}

func mustInsertText(t *testing.T, db *sql.DB, verseID int64, sourceCode, ref string) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO verse_text (verse_id, source_id, native_ref, text)
		VALUES (?, (SELECT id FROM sources WHERE code = ?), ?, 'placeholder')`,
		verseID, sourceCode, ref+" 1:1")
	if err != nil {
		t.Fatalf("insert verse_text %s/%s: %v", sourceCode, ref, err)
	}
}

var wordSeq int

func mustInsertWord(t *testing.T, db *sql.DB, verseID int64, sourceCode, ref string) {
	t.Helper()
	wordSeq++
	_, err := db.Exec(`
		INSERT INTO words (verse_id, source_id, word_no, surface, lemma, dstrong, morph_code, attestation, editions, source_locator)
		VALUES (?, (SELECT id FROM sources WHERE code = ?), 1, 'w', 'w', NULL, NULL, 'N', 'NA28', ?)`,
		verseID, sourceCode, strings.ToLower(ref)+".1.1#01")
	if err != nil {
		t.Fatalf("insert words %s/%s: %v", sourceCode, ref, err)
	}
}

func TestRunPassesOnCleanFixture(t *testing.T) {
	db := setup(t)
	seedFullCoverage(t, db, "", "")

	expect := []verify.CountExpectation{
		{Label: "KJV verse_text", Query: `SELECT COUNT(*) FROM verse_text WHERE source_id = (SELECT id FROM sources WHERE code = ?)`, Args: []any{"KJV"}, Want: 66},
	}
	report, err := verify.Run(db, expect)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !report.OK() {
		t.Fatalf("expected clean report, got issues: %+v", report.Issues)
	}
}

func TestBookCoverageCatchesDroppedBook(t *testing.T) {
	db := setup(t)
	seedFullCoverage(t, db, "TAGNT", "MRK")

	report, err := verify.Run(db, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.OK() {
		t.Fatal("expected a book_coverage issue for the silently dropped MRK/TAGNT row, got none")
	}
	found := false
	for _, iss := range report.Issues {
		if iss.Check == "book_coverage" && strings.Contains(iss.Detail, "MRK") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an issue naming MRK, got: %+v", report.Issues)
	}
}

func TestForeignKeyCheckCatchesDanglingReference(t *testing.T) {
	db := setup(t)
	seedFullCoverage(t, db, "", "")

	// Disable enforcement just long enough to insert a row store.Open's
	// PRAGMA foreign_keys=ON would otherwise reject outright - simulating a
	// DB that somehow ended up corrupted (e.g. a hand edit), which is
	// exactly what a second, independent verify pass exists to catch.
	if _, err := db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatalf("disable fk: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO words (verse_id, source_id, word_no, surface, lemma, dstrong, morph_code, attestation, editions, source_locator)
		VALUES (999999, (SELECT id FROM sources WHERE code = 'TAGNT'), 1, 'x', 'x', NULL, NULL, 'N', 'NA28', 'dangling.1.1#01')`); err != nil {
		t.Fatalf("insert dangling row: %v", err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("re-enable fk: %v", err)
	}

	report, err := verify.Run(db, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.OK() {
		t.Fatal("expected a foreign_key issue for the dangling verse_id, got none")
	}
	found := false
	for _, iss := range report.Issues {
		if iss.Check == "foreign_key" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a foreign_key issue, got: %+v", report.Issues)
	}
}

func TestCheckCountsCatchesDroppedRow(t *testing.T) {
	db := setup(t)
	seedFullCoverage(t, db, "", "")

	expect := []verify.CountExpectation{
		{Label: "KJV verse_text", Query: `SELECT COUNT(*) FROM verse_text WHERE source_id = (SELECT id FROM sources WHERE code = ?)`, Args: []any{"KJV"}, Want: 66},
	}
	if _, err := db.Exec(`DELETE FROM verse_text WHERE source_id = (SELECT id FROM sources WHERE code = 'KJV') LIMIT 1`); err != nil {
		// modernc sqlite may not support LIMIT on DELETE; fall back to a rowid subquery.
		if _, err := db.Exec(`DELETE FROM verse_text WHERE id = (SELECT id FROM verse_text WHERE source_id = (SELECT id FROM sources WHERE code = 'KJV') LIMIT 1)`); err != nil {
			t.Fatalf("delete row: %v", err)
		}
	}

	report, err := verify.Run(db, expect)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.OK() {
		t.Fatal("expected an edition_totals issue after deleting a KJV row, got none")
	}
	found := false
	for _, iss := range report.Issues {
		if iss.Check == "edition_totals" && strings.Contains(iss.Detail, "KJV") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an edition_totals issue naming KJV, got: %+v", report.Issues)
	}
}

func TestDstrongResolutionWithinDocumentedGapIsNoteNotIssue(t *testing.T) {
	db := setup(t)
	seedFullCoverage(t, db, "", "")

	// Insert a couple of unresolved-dstrong rows on top of the clean
	// baseline - well within the documented TAGNT/TBESG gap.
	for i := 0; i < 3; i++ {
		if _, err := db.Exec(`
			INSERT INTO words (verse_id, source_id, word_no, surface, lemma, dstrong, morph_code, attestation, editions, source_locator)
			VALUES ((SELECT id FROM verses LIMIT 1), (SELECT id FROM sources WHERE code = 'TAGNT'), 2, 'x', 'x', 'G99999', NULL, 'N', 'NA28', ?)`,
			"gap.1.1#0"+string(rune('2'+i))); err != nil {
			t.Fatalf("insert gap row: %v", err)
		}
	}

	report, err := verify.Run(db, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, iss := range report.Issues {
		if iss.Check == "dstrong_resolution" {
			t.Errorf("expected the small gap to be a Note, got an Issue: %+v", iss)
		}
	}
	noted := false
	for _, n := range report.Notes {
		if n.Check == "dstrong_resolution" {
			noted = true
		}
	}
	if !noted {
		t.Error("expected a dstrong_resolution Note for the unresolved rows")
	}
}
