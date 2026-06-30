package swetewords_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/sources"
	"github.com/jrainsberger/orthotomeo/store"
	"github.com/jrainsberger/orthotomeo/swetewords"
)

// Mirrors the real Swete CSVs: a sparse versification file (only verse-
// start word indices) and a dense, sequential word file. Gen.1:1 spans
// indices 1-10 (10 words), Gen.1:2 starts at 11. "Eze.1:1" exercises the
// book-code alias (Swete's "Eze" -> our canonical "Ezk"); "Tob.1:1"
// exercises the deuterocanon skip (no canonical book at all).
const fixtureVersification = "1\tGen.1:1\n" +
	"11\tGen.1:2\n" +
	"13\tEze.1:1\n" +
	"15\tTob.1:1\n"

const fixtureWords = "1\tΕΝ\n" +
	"2\tΑΡΧΗ\n" +
	"3\tἐποίησεν\n" +
	"4\tὁ\n" +
	"5\tθεὸς\n" +
	"6\tτὸν\n" +
	"7\tοὐρανὸν\n" +
	"8\tκαὶ\n" +
	"9\tτὴν\n" +
	"10\tγῆν.\n" +
	"11\tἡ\n" +
	"12\tδὲ\n" +
	"13\tword1\n" +
	"14\tword2\n" +
	"15\tword1\n"

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

func TestLoadBuildsVerseRangesFromSparseIndex(t *testing.T) {
	db := setup(t)

	inserted, skipped, err := swetewords.Load(db, strings.NewReader(fixtureVersification), strings.NewReader(fixtureWords))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// Gen.1:1 (indices 1-10, 10 words) + Gen.1:2 (indices 11-12, 2 words) +
	// Ezk.1:1 (indices 13-14, 2 words) = 14; Tob.1:1 (index 15, the last
	// entry, no upper bound) is deuterocanon and skipped, not inserted.
	if inserted != 14 {
		t.Errorf("inserted = %d, want 14", inserted)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1 (Tob.1:1 deuterocanon)", skipped)
	}
}

func TestLoadPreservesWordOrderAndPosition(t *testing.T) {
	db := setup(t)
	if _, _, err := swetewords.Load(db, strings.NewReader(fixtureVersification), strings.NewReader(fixtureWords)); err != nil {
		t.Fatalf("load: %v", err)
	}

	var surface string
	err := db.QueryRow(`
		SELECT w.surface FROM words w
		JOIN verses v ON v.id = w.verse_id
		JOIN books b ON b.id = v.book_id
		JOIN sources s ON s.id = w.source_id
		WHERE s.code = 'Swete' AND b.full_name = 'Genesis' AND v.chapter = 1 AND v.verse = 1 AND w.word_no = 3`).
		Scan(&surface)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if surface != "ἐποίησεν" {
		t.Errorf("Gen.1.1 word 3 = %q, want ἐποίησεν (epoiesen)", surface)
	}

	var count int
	db.QueryRow(`
		SELECT COUNT(*) FROM words w
		JOIN verses v ON v.id = w.verse_id JOIN books b ON b.id = v.book_id
		JOIN sources s ON s.id = w.source_id
		WHERE s.code = 'Swete' AND b.full_name = 'Genesis' AND v.chapter = 1 AND v.verse = 1`).Scan(&count)
	if count != 10 {
		t.Errorf("Gen.1.1 word count = %d, want 10", count)
	}
}

func TestLoadResolvesBookAliasAndNullsLexicalFields(t *testing.T) {
	db := setup(t)
	if _, _, err := swetewords.Load(db, strings.NewReader(fixtureVersification), strings.NewReader(fixtureWords)); err != nil {
		t.Fatalf("load: %v", err)
	}

	var bookCode string
	var dstrong, lemma, morph sql.NullString
	err := db.QueryRow(`
		SELECT b.code, w.dstrong, w.lemma, w.morph_code FROM words w
		JOIN verses v ON v.id = w.verse_id JOIN books b ON b.id = v.book_id
		JOIN sources s ON s.id = w.source_id
		WHERE s.code = 'Swete' AND w.word_no = 1 AND v.chapter = 1 AND v.verse = 1 AND b.code = 'EZK'`).
		Scan(&bookCode, &dstrong, &lemma, &morph)
	if err != nil {
		t.Fatalf("query (Eze alias to EZK): %v", err)
	}
	if dstrong.Valid || lemma.Valid || morph.Valid {
		t.Errorf("Swete word has lexical fields set: dstrong=%v lemma=%v morph=%v, want all NULL", dstrong, lemma, morph)
	}
}

func TestLoadSkipsDeuterocanonWithoutError(t *testing.T) {
	db := setup(t)
	if _, _, err := swetewords.Load(db, strings.NewReader(fixtureVersification), strings.NewReader(fixtureWords)); err != nil {
		t.Fatalf("load: %v", err)
	}

	var count int
	db.QueryRow(`
		SELECT COUNT(*) FROM words w
		JOIN sources s ON s.id = w.source_id
		WHERE s.code = 'Swete' AND w.source_locator LIKE 'Tob%'`).Scan(&count)
	if count != 0 {
		t.Errorf("Tobit word rows = %d, want 0 (deuterocanon, skipped)", count)
	}
}
