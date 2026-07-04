package parse_test

import (
	"database/sql"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/parse"
	"github.com/jrainsberger/orthotomeo/retriever"
	"github.com/jrainsberger/orthotomeo/sources"
	"github.com/jrainsberger/orthotomeo/store"
)

// setup builds a fixture covering: a fully-tagged TAGNT verse (2 words,
// morph_code resolving cleanly), a TAGNT word with an unresolved morph_code
// (the small documented T14 gap), a TAHOT word (Hebrew language expansion),
// a Swete word (surface-only - no morph at all, reached via a real T4b
// "exact" alignment), and an OSS-LXX-lemma word reached via a "renumber"
// alignment (mirrors Ps9/10) to prove non-exact relations get flagged.
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
	if _, err := db.Exec(`INSERT INTO morph_codes (code, language, description) VALUES ('N-ASF', 'grc', 'Noun, Accusative, Singular, Feminine')`); err != nil {
		t.Fatalf("seed morph_codes: %v", err)
	}

	matBook := bookID(t, db, "MAT")
	psaBook := bookID(t, db, "PSA")

	mat1v1 := insertVerse(t, db, "canonical", matBook, 1, 1)
	insertWord(t, db, mat1v1, "TAGNT", 1, "εἰς", "εἰς", "G1519", "PREP")        // unresolved morph (PREP not seeded)
	insertWord(t, db, mat1v1, "TAGNT", 2, "ἄφεσιν", "ἄφεσις", "G0859", "N-ASF") // resolves cleanly
	insertWord(t, db, mat1v1, "TAHOT", 1, "בְּ", "בְּ", "H9003", "")            // no morph at all (untagged)

	brentonMat1v1 := insertVerse(t, db, "lxx-brenton", matBook, 1, 1)
	insertAlignment(t, db, mat1v1, brentonMat1v1, "Brenton", "exact", 1.0)

	sweteMat1v1 := insertVerse(t, db, "lxx-swete", matBook, 1, 1)
	insertAlignment(t, db, mat1v1, sweteMat1v1, "Swete", "exact", 1.0)
	insertSweteWord(t, db, sweteMat1v1, 1, "εἰς")

	psa9 := insertVerse(t, db, "canonical", psaBook, 9, 1)
	ossPsa9 := insertVerse(t, db, "lxx-oss", psaBook, 9, 2)
	insertAlignment(t, db, psa9, ossPsa9, "OSS-LXX-lemma", "renumber", 0.85)
	insertOSSWord(t, db, ossPsa9, 1, "ἐξομολογήσομαι")

	return db
}

func bookID(t *testing.T, db *sql.DB, code string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(`SELECT id FROM books WHERE code = ?`, code).Scan(&id); err != nil {
		t.Fatalf("book %s: %v", code, err)
	}
	return id
}

func insertVerse(t *testing.T, db *sql.DB, versification string, bookID int64, chapter, verse int) int64 {
	t.Helper()
	res, err := db.Exec(`INSERT INTO verses (versification, book_id, chapter, verse) VALUES (?, ?, ?, ?)`,
		versification, bookID, chapter, verse)
	if err != nil {
		t.Fatalf("insert verse: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	return id
}

var wordSeq int

func insertWord(t *testing.T, db *sql.DB, verseID int64, sourceCode string, wordNo int, surface, lemma, dstrong, morphCode string) {
	t.Helper()
	wordSeq++
	var morph any
	if morphCode != "" {
		morph = morphCode
	}
	if _, err := db.Exec(`
		INSERT INTO words (verse_id, source_id, word_no, surface, lemma, dstrong, morph_code, attestation, editions, source_locator)
		VALUES (?, (SELECT id FROM sources WHERE code = ?), ?, ?, ?, ?, ?, 'N', 'NA28', ?)`,
		verseID, sourceCode, wordNo, surface, lemma, dstrong, morph, "loc#"+strconv.Itoa(wordSeq)); err != nil {
		t.Fatalf("insert word %s: %v", sourceCode, err)
	}
}

func insertSweteWord(t *testing.T, db *sql.DB, verseID int64, wordNo int, surface string) {
	t.Helper()
	wordSeq++
	if _, err := db.Exec(`
		INSERT INTO words (verse_id, source_id, word_no, surface, lemma, dstrong, morph_code, attestation, editions, source_locator)
		VALUES (?, (SELECT id FROM sources WHERE code = 'Swete'), ?, ?, NULL, NULL, NULL, '', '', ?)`,
		verseID, wordNo, surface, "swete-loc#"+strconv.Itoa(wordSeq)); err != nil {
		t.Fatalf("insert Swete word: %v", err)
	}
}

func insertOSSWord(t *testing.T, db *sql.DB, verseID int64, wordNo int, lemma string) {
	t.Helper()
	wordSeq++
	if _, err := db.Exec(`
		INSERT INTO words (verse_id, source_id, word_no, surface, lemma, dstrong, morph_code, attestation, editions, source_locator)
		VALUES (?, (SELECT id FROM sources WHERE code = 'OSS-LXX-lemma'), ?, NULL, ?, NULL, NULL, '', '', ?)`,
		verseID, wordNo, lemma, "oss-loc#"+strconv.Itoa(wordSeq)); err != nil {
		t.Fatalf("insert OSS word: %v", err)
	}
}

func insertAlignment(t *testing.T, db *sql.DB, canonicalID, editionID int64, sourceCode, relation string, confidence float64) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO verse_alignment (canonical_verse_id, edition_verse_id, relation, confidence, source_id)
		VALUES (?, ?, ?, ?, (SELECT id FROM sources WHERE code = ?))`,
		canonicalID, editionID, relation, confidence, sourceCode); err != nil {
		t.Fatalf("insert alignment %s: %v", sourceCode, err)
	}
}

func TestParseReturnsAllWordsInVerse(t *testing.T) {
	db := setup(t)
	cs, err := parse.Parse(db, retriever.Ref{Book: "MAT", Chapter: 1, Verse: 1}, nil, "TAGNT")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cs) != 2 {
		t.Fatalf("citations = %d, want 2", len(cs))
	}
}

func TestParseExpandsMorphCodeViaT6(t *testing.T) {
	db := setup(t)
	two := 2
	cs, err := parse.Parse(db, retriever.Ref{Book: "MAT", Chapter: 1, Verse: 1}, &two, "TAGNT")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cs) != 1 {
		t.Fatalf("citations = %d, want 1", len(cs))
	}
	c := cs[0]
	if c.Grammar != "N-ASF (Noun, Accusative, Singular, Feminine)" {
		t.Errorf("grammar = %q, want the code plus its T6 expansion", c.Grammar)
	}
	if c.DStrong != "G0859" {
		t.Errorf("dstrong = %q, want G0859", c.DStrong)
	}
	if c.Confidence != retriever.ConfidenceHigh {
		t.Errorf("confidence = %q, want High for a fully resolved word", c.Confidence)
	}
}

// TestParsePopulatesTranslit is the direct T32 test: buildCitation must
// forward a words row's transliteration onto the Citation, the same as it
// already does for Lemma/DStrong/Grammar.
func TestParsePopulatesTranslit(t *testing.T) {
	db := setup(t)
	if _, err := db.Exec(`UPDATE words SET translit = 'aphesin' WHERE dstrong = 'G0859'`); err != nil {
		t.Fatalf("seed translit: %v", err)
	}
	two := 2
	cs, err := parse.Parse(db, retriever.Ref{Book: "MAT", Chapter: 1, Verse: 1}, &two, "TAGNT")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cs) != 1 {
		t.Fatalf("citations = %d, want 1", len(cs))
	}
	if cs[0].Translit != "aphesin" {
		t.Errorf("translit = %q, want aphesin", cs[0].Translit)
	}
}

func TestParseFlagsUnresolvedMorphCode(t *testing.T) {
	db := setup(t)
	one := 1
	cs, err := parse.Parse(db, retriever.Ref{Book: "MAT", Chapter: 1, Verse: 1}, &one, "TAGNT")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cs) != 1 {
		t.Fatalf("citations = %d, want 1", len(cs))
	}
	c := cs[0]
	if c.Grammar != "PREP" {
		t.Errorf("grammar = %q, want the raw unresolved code", c.Grammar)
	}
	if c.Confidence != retriever.ConfidenceFlagged || c.Caveat == "" {
		t.Errorf("confidence=%q caveat=%q, want Flagged with a caveat for an unresolved morph_code", c.Confidence, c.Caveat)
	}
}

func TestParseTAHOTExpandsHebrewMorph(t *testing.T) {
	db := setup(t)
	cs, err := parse.Parse(db, retriever.Ref{Book: "MAT", Chapter: 1, Verse: 1}, nil, "TAHOT")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cs) != 1 {
		t.Fatalf("citations = %d, want 1", len(cs))
	}
	if cs[0].DStrong != "H9003" {
		t.Errorf("dstrong = %q, want H9003", cs[0].DStrong)
	}
	// No morph_code was tagged on this fixture row, so it must be flagged,
	// not silently given an empty Grammar with High confidence.
	if cs[0].Confidence != retriever.ConfidenceFlagged {
		t.Error("untagged morph should be Flagged, not High")
	}
}

func TestParseSweteFlaggedNoMorph(t *testing.T) {
	db := setup(t)
	cs, err := parse.Parse(db, retriever.Ref{Book: "MAT", Chapter: 1, Verse: 1}, nil, "Swete")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cs) != 1 {
		t.Fatalf("citations = %d, want 1", len(cs))
	}
	c := cs[0]
	if c.Text != "εἰς" {
		t.Errorf("text = %q, want the real surface form (Swete still HAS the word, just not morph)", c.Text)
	}
	if c.Confidence != retriever.ConfidenceFlagged || c.Caveat == "" {
		t.Errorf("confidence=%q caveat=%q, want Flagged (Swete carries no morph_code - T12)", c.Confidence, c.Caveat)
	}
}

func TestParseAlignmentKeyedNonExactRelationFlagged(t *testing.T) {
	db := setup(t)
	cs, err := parse.Parse(db, retriever.Ref{Book: "PSA", Chapter: 9, Verse: 1}, nil, "OSS-LXX-lemma")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cs) != 1 {
		t.Fatalf("citations = %d, want 1", len(cs))
	}
	c := cs[0]
	if c.Lemma != "ἐξομολογήσομαι" {
		t.Errorf("lemma = %q", c.Lemma)
	}
	if c.Confidence != retriever.ConfidenceFlagged {
		t.Error("a T4b renumber relation must be Flagged, not High")
	}
}

func TestParseNoAlignmentReturnsFlaggedPlaceholderNotEmpty(t *testing.T) {
	db := setup(t)
	// PSA.9.1 canonical has no Swete alignment in this fixture at all.
	cs, err := parse.Parse(db, retriever.Ref{Book: "PSA", Chapter: 9, Verse: 1}, nil, "Swete")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cs) != 1 {
		t.Fatalf("citations = %d, want 1 (a placeholder, not a silent empty slice)", len(cs))
	}
	if cs[0].Confidence != retriever.ConfidenceFlagged || cs[0].Caveat == "" {
		t.Error("expected a Flagged placeholder with a caveat, not a bare empty citation")
	}
}

func TestParseRejectsNonWordCorpus(t *testing.T) {
	db := setup(t)
	if _, err := parse.Parse(db, retriever.Ref{Book: "MAT", Chapter: 1, Verse: 1}, nil, "KJV"); err == nil {
		t.Fatal("expected an error for a verse_text-only corpus")
	}
}

func TestLemmatizeReturnsOrderedLemmaListSkippingUntagged(t *testing.T) {
	db := setup(t)
	cs, err := parse.Lemmatize(db, retriever.Ref{Book: "MAT", Chapter: 1, Verse: 1}, "TAGNT")
	if err != nil {
		t.Fatalf("lemmatize: %v", err)
	}
	if len(cs) != 2 {
		t.Fatalf("citations = %d, want 2", len(cs))
	}
	if cs[0].Lemma != "εἰς" || cs[1].Lemma != "ἄφεσις" {
		t.Errorf("lemmas = %q, %q, want εἰς then ἄφεσις in word order", cs[0].Lemma, cs[1].Lemma)
	}
}

func TestLemmatizeSkipsWordsWithNoLemma(t *testing.T) {
	db := setup(t)
	// Swete's row has surface "εἰς" but no lemma - Lemmatize must exclude
	// it, not fabricate a lemma from the surface form.
	cs, err := parse.Lemmatize(db, retriever.Ref{Book: "MAT", Chapter: 1, Verse: 1}, "Swete")
	if err != nil {
		t.Fatalf("lemmatize: %v", err)
	}
	if len(cs) != 0 {
		t.Errorf("citations = %d, want 0 (Swete carries no lemma at all)", len(cs))
	}
}
