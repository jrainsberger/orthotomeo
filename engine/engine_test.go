package engine_test

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"
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
		INSERT INTO lexicon (dstrong, estrong, ustrong, language, lemma, translit, gloss, definition, def_license)
		VALUES ('G0859', 'G0859', 'G0859', 'grc', 'ἄφεσις', 'aphesis', 'forgiveness', 'release, pardon', 'Abbott-Smith PD')`); err != nil {
		t.Fatalf("seed lexicon: %v", err)
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

// buildCeilingFixture writes a DB holding exactly n TAGNT occurrences of one
// lemma, so the concordance ceiling can be walked across its boundary
// precisely. Kept separate from buildFixture so raising n here can never
// shift a count another test asserts on.
func buildCeilingFixture(t *testing.T, n int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ceiling.db")
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
	for i := 1; i <= n; i++ {
		res, err := db.Exec(`INSERT INTO verses (versification, book_id, chapter, verse) VALUES ('canonical', ?, 1, ?)`, matBook, i)
		if err != nil {
			t.Fatalf("insert verse %d: %v", i, err)
		}
		verseID, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("last insert id %d: %v", i, err)
		}
		if _, err := db.Exec(`
			INSERT INTO words (verse_id, source_id, word_no, surface, lemma, dstrong, morph_code, attestation, editions, source_locator)
			VALUES (?, (SELECT id FROM sources WHERE code = 'TAGNT'), 1, 'εἰς', 'εἰς', 'G1519', 'PREP', 'NKO', 'NA28', ?)`,
			verseID, "Mat.1."+strconv.Itoa(i)+"#1"); err != nil {
			t.Fatalf("insert word %d: %v", i, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close build handle: %v", err)
	}
	return path
}

// The ceiling is deployment policy fixed at Open, so these walk its boundary
// exactly: 5 occurrences against limits either side of 5. The refusal has to
// be a named, branchable error reporting the real count, so a caller can tell
// "too big, here's how big" apart from "no matches".
func TestConcordLemmaRefusesResultSetOverCeiling(t *testing.T) {
	cases := []struct {
		name    string
		opts    []engine.Option
		wantErr bool
	}{
		{name: "unbounded by default", opts: nil, wantErr: false},
		{name: "under the ceiling", opts: []engine.Option{engine.WithMaxResults(10)}, wantErr: false},
		{name: "exactly at the ceiling", opts: []engine.Option{engine.WithMaxResults(5)}, wantErr: false},
		{name: "one over the ceiling", opts: []engine.Option{engine.WithMaxResults(4)}, wantErr: true},
		{name: "zero means unbounded", opts: []engine.Option{engine.WithMaxResults(0)}, wantErr: false},
		{name: "negative means unbounded", opts: []engine.Option{engine.WithMaxResults(-1)}, wantErr: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, err := engine.Open(buildCeilingFixture(t, 5), tc.opts...)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer e.Close()

			cs, err := e.ConcordLemma("εἰς", "TAGNT", "")
			switch {
			case tc.wantErr && err == nil:
				t.Fatalf("ConcordLemma returned %d citations, want ErrResultTooLarge", len(cs))
			case tc.wantErr:
				if !errors.Is(err, engine.ErrResultTooLarge) {
					t.Errorf("error = %v, want ErrResultTooLarge", err)
				}
				if !strings.Contains(err.Error(), "5 occurrences") {
					t.Errorf("error %q does not report the matched count", err)
				}
			case err != nil:
				t.Fatalf("ConcordLemma: %v", err)
			case len(cs) != 5:
				t.Errorf("citations = %d, want 5", len(cs))
			}
		})
	}
}

// ConcordPhrase materializes every occurrence of its anchor token before it
// walks a single chain, so the anchor scan - not the far smaller set of
// matched phrases - is the cost the ceiling has to bound.
func TestConcordPhraseRefusesAnchorScanOverCeiling(t *testing.T) {
	e, err := engine.Open(buildCeilingFixture(t, 5), engine.WithMaxResults(4))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer e.Close()

	if _, err := e.ConcordPhrase([]string{"εἰς", "ἄφεσις"}, "TAGNT", 0); !errors.Is(err, engine.ErrResultTooLarge) {
		t.Fatalf("error = %v, want ErrResultTooLarge", err)
	}
}

// Count reads numbers, never Citations, so it stays available under any
// ceiling - it is how a caller learns how big a refused query actually is.
func TestCountIsNeverBoundedByCeiling(t *testing.T) {
	e, err := engine.Open(buildCeilingFixture(t, 5), engine.WithMaxResults(1))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer e.Close()

	tally, err := e.Count("εἰς", "TAGNT", "")
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if tally.Total != 5 {
		t.Errorf("total = %d, want 5", tally.Total)
	}
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
	if cs, err := e.ConcordLemma("G0859", "TAGNT", ""); err != nil || len(cs) != 1 {
		t.Errorf("ConcordLemma: cs=%v err=%v", cs, err)
	}
	if cs, err := e.ConcordPhrase([]string{"εἰς", "ἄφεσις"}, "TAGNT", 0); err != nil || len(cs) != 1 {
		t.Errorf("ConcordPhrase: cs=%v err=%v", cs, err)
	}
	if _, err := e.Count("G0859", "TAGNT", ""); err != nil {
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
	if entry, err := e.Lookup("G0859"); err != nil || entry.Gloss != "forgiveness" {
		t.Errorf("Lookup: entry=%+v err=%v", entry, err)
	}
}

func TestCountAgreesWithConcordLemmaThroughFacade(t *testing.T) {
	path := buildFixture(t)
	e, err := engine.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer e.Close()

	cs, err := e.ConcordLemma("G0859", "TAGNT", "")
	if err != nil {
		t.Fatalf("ConcordLemma: %v", err)
	}
	tally, err := e.Count("G0859", "TAGNT", "")
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

// TestEngineOpenRejectsStaleSchema covers the real bug this guards against:
// a DB built before schema.sql gained a column (words.translit, then
// sources.homepage_url) opened fine and only failed deep inside a query
// with a cryptic "no such column" error. Simulates a stale DB by stamping
// user_version back down after a normal ApplySchema.
func TestEngineOpenRejectsStaleSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stale.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := store.ApplySchema(db); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	if _, err := db.Exec(`PRAGMA user_version = 0;`); err != nil {
		t.Fatalf("downgrade user_version: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close build handle: %v", err)
	}

	if _, err := engine.Open(path); err == nil {
		t.Fatal("expected Open to reject a DB stamped with an older schema version")
	}
}
