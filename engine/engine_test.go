package engine_test

import (
	"path/filepath"
	"strconv"
	"testing"

	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/engine"
	"github.com/jrainsberger/orthotomeo/retriever"
	"github.com/jrainsberger/orthotomeo/sources"
	"github.com/jrainsberger/orthotomeo/store"
)

// buildFixture writes a small, real DB FILE (not :memory: - engine.Open's
// read-only URI mode needs an actual path to reopen), covering enough of
// Mat.26.28's real shape (G0859/ἄφεσις adjacent to εἰς) to exercise every
// Phase-5 operation through the facade in one pass.
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

	insertWord := func(wordNo int, surface, lemma, dstrong, morphCode string) {
		if _, err := db.Exec(`
			INSERT INTO words (verse_id, source_id, word_no, surface, lemma, dstrong, morph_code, attestation, editions, source_locator)
			VALUES (?, (SELECT id FROM sources WHERE code = 'TAGNT'), ?, ?, ?, ?, ?, 'NKO', 'NA28+TR+Byz', ?)`,
			verseID, wordNo, surface, lemma, dstrong, morphCode, "Mat.26.28#"+strconv.Itoa(wordNo)); err != nil {
			t.Fatalf("insert word: %v", err)
		}
	}
	insertWord(1, "εἰς", "εἰς", "G1519", "PREP")
	insertWord(2, "ἄφεσιν", "ἄφεσις", "G0859", "N-ASF")
	if _, err := db.Exec(`INSERT INTO morph_codes (code, language, description) VALUES ('N-ASF', 'grc', 'Noun, Accusative, Singular, Feminine')`); err != nil {
		t.Fatalf("seed morph_codes: %v", err)
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

func TestEngineReachesEveryPhase5Operation(t *testing.T) {
	path := buildFixture(t)
	e, err := engine.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer e.Close()

	ref := retriever.Ref{Book: "MAT", Chapter: 26, Verse: 28}

	if _, err := e.ResolveRef(ref); err != nil {
		t.Errorf("ResolveRef: %v", err)
	}
	if cs, err := e.GetVerse(ref, []string{"KJV"}); err != nil || len(cs) != 1 {
		t.Errorf("GetVerse: cs=%v err=%v", cs, err)
	}
	rr := retriever.RefRange{Start: ref, End: ref}
	if cs, err := e.GetPassage(rr, []string{"KJV"}); err != nil || len(cs) != 1 {
		t.Errorf("GetPassage: cs=%v err=%v", cs, err)
	}
	if cs, err := e.ConcordLemma("G0859", "TAGNT"); err != nil || len(cs) != 1 {
		t.Errorf("ConcordLemma: cs=%v err=%v", cs, err)
	}
	if cs, err := e.ConcordPhrase([]string{"εἰς", "ἄφεσις"}, "TAGNT", 0); err != nil || len(cs) != 1 {
		t.Errorf("ConcordPhrase: cs=%v err=%v", cs, err)
	}
	if _, err := e.Count("G0859", "TAGNT"); err != nil {
		t.Errorf("Count: %v", err)
	}
	if cs, err := e.Parse(ref, nil, "TAGNT"); err != nil || len(cs) != 2 {
		t.Errorf("Parse: cs=%v err=%v", cs, err)
	}
	if cs, err := e.Lemmatize(ref, "TAGNT"); err != nil || len(cs) != 2 {
		t.Errorf("Lemmatize: cs=%v err=%v", cs, err)
	}
	if cs, err := e.Attestation(ref, nil, "TAGNT"); err != nil || len(cs) != 2 {
		t.Errorf("Attestation: cs=%v err=%v", cs, err)
	}
	if s := e.Cite([]retriever.Citation{{Ref: ref, Edition: "TAGNT", Text: "ἄφεσιν"}}); s == "" {
		t.Error("Cite returned empty for a non-empty input")
	}
}

func TestCountAgreesWithConcordLemmaThroughFacade(t *testing.T) {
	path := buildFixture(t)
	e, err := engine.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer e.Close()

	cs, err := e.ConcordLemma("G0859", "TAGNT")
	if err != nil {
		t.Fatalf("ConcordLemma: %v", err)
	}
	tally, err := e.Count("G0859", "TAGNT")
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if tally.Total != len(cs) {
		t.Errorf("Count.Total = %d, len(ConcordLemma) = %d through the facade - must agree", tally.Total, len(cs))
	}
}

func TestEngineOpenFailsOnMissingFile(t *testing.T) {
	if _, err := engine.Open(filepath.Join(t.TempDir(), "does-not-exist.db")); err == nil {
		t.Fatal("expected an error opening a nonexistent DB read-only")
	}
}
