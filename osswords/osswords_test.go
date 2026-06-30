package osswords_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/osswords"
	"github.com/jrainsberger/orthotomeo/sources"
	"github.com/jrainsberger/orthotomeo/store"
)

// Mirrors the real OSS LxxLemmas/<Book>.js shape. Gen.1.1 is a normal
// single-key verse; Esth.1.1a/b are lettered sub-verses that must merge
// into one verse 1 row, in letter order; "Jer.7.27/28" mirrors the single
// confirmed real-corpus anomaly (a combined-verse-range key) and must be
// reported as malformed, not guessed at; "JoshA.1.1" mirrors a
// multi-recension book outside the bookAlias allow-list.
const fixtureOSS = `{
"Gen.1.1": [
	{"key": "en", "lemma": "ἐν"},
	{"key": "arche", "lemma": "ἀρχή"},
	{"key": "poieo", "lemma": "ποιέω"}
],
"Esth.1.1b": [
	{"key": "kai", "lemma": "καί"}
],
"Esth.1.1a": [
	{"key": "etos", "lemma": "ἔτος"}
],
"Jer.7.27/28": [
	{"key": "lego", "lemma": "λέγω"}
],
"JoshA.1.1": [
	{"key": "meta", "lemma": "μετά"}
]
}`

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

func TestLoadCountsAndSkips(t *testing.T) {
	db := setup(t)

	inserted, skippedBook, malformed, err := osswords.Load(db, strings.NewReader(fixtureOSS))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// Gen.1.1 (3 words) + Esth.1.1a+b merged (2 words) = 5; JoshA.1.1 is
	// out of scope (1 skipped-book row); Jer.7.27/28 is malformed (1 row).
	if inserted != 5 {
		t.Errorf("inserted = %d, want 5", inserted)
	}
	if skippedBook != 1 {
		t.Errorf("skippedBook = %d, want 1 (JoshA outside bookAlias)", skippedBook)
	}
	if malformed != 1 {
		t.Errorf("malformed = %d, want 1 (Jer.7.27/28)", malformed)
	}
}

func TestLoadPreservesLemmaOrderAndNullsSurface(t *testing.T) {
	db := setup(t)
	if _, _, _, err := osswords.Load(db, strings.NewReader(fixtureOSS)); err != nil {
		t.Fatalf("load: %v", err)
	}

	var lemma string
	var surface, dstrong sql.NullString
	err := db.QueryRow(`
		SELECT lemma, surface, dstrong FROM words w
		JOIN verses v ON v.id = w.verse_id
		JOIN books b ON b.id = v.book_id
		JOIN sources s ON s.id = w.source_id
		WHERE s.code = 'OSS-LXX-lemma' AND b.full_name = 'Genesis' AND v.chapter = 1 AND v.verse = 1 AND w.word_no = 2`).
		Scan(&lemma, &surface, &dstrong)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if lemma != "ἀρχή" {
		t.Errorf("Gen.1.1 word 2 lemma = %q, want ἀρχή", lemma)
	}
	if surface.Valid || dstrong.Valid {
		t.Errorf("OSS word has surface/dstrong set: %v/%v, want both NULL", surface, dstrong)
	}
}

func TestLoadMergesLetteredSubVersesInOrder(t *testing.T) {
	db := setup(t)
	if _, _, _, err := osswords.Load(db, strings.NewReader(fixtureOSS)); err != nil {
		t.Fatalf("load: %v", err)
	}

	rows, err := db.Query(`
		SELECT w.word_no, w.lemma FROM words w
		JOIN verses v ON v.id = w.verse_id
		JOIN books b ON b.id = v.book_id
		JOIN sources s ON s.id = w.source_id
		WHERE s.code = 'OSS-LXX-lemma' AND b.full_name = 'Esther' AND v.chapter = 1 AND v.verse = 1
		ORDER BY w.word_no`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	var lemmas []string
	for rows.Next() {
		var n int
		var l string
		rows.Scan(&n, &l)
		lemmas = append(lemmas, l)
	}
	// "a" sorts before "b" - ἔτος (1a) must come before καί (1b), even
	// though 1b appeared first in the source JSON object.
	want := []string{"ἔτος", "καί"}
	if len(lemmas) != 2 || lemmas[0] != want[0] || lemmas[1] != want[1] {
		t.Errorf("Esth.1.1 merged lemmas = %v, want %v (letter order, not key order)", lemmas, want)
	}
}

func TestLoadUsesItsOwnVersification(t *testing.T) {
	db := setup(t)
	if _, _, _, err := osswords.Load(db, strings.NewReader(fixtureOSS)); err != nil {
		t.Fatalf("load: %v", err)
	}

	var versification string
	err := db.QueryRow(`
		SELECT DISTINCT v.versification FROM verses v
		JOIN words w ON w.verse_id = v.id
		JOIN sources s ON s.id = w.source_id
		WHERE s.code = 'OSS-LXX-lemma'`).Scan(&versification)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if versification != osswords.Versification {
		t.Errorf("versification = %q, want %q", versification, osswords.Versification)
	}
}
