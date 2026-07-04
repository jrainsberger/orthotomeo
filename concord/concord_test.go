package concord_test

import (
	"database/sql"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/concord"
	"github.com/jrainsberger/orthotomeo/retriever"
	"github.com/jrainsberger/orthotomeo/sources"
	"github.com/jrainsberger/orthotomeo/store"
)

// setup builds a fixture shaped like the spec's own worked example (§6):
// G0859/ἄφεσις occurs in TAGNT at Mat.26.28 (the control case: Christ's
// blood poured out εἰς ἄφεσιν - a causal "because of" reading is
// structurally impossible there) and at Act.2.38 (the ambiguous case). A
// third TAGNT row (Mat.1.1) matches neither query, proving ConcordLemma
// doesn't just return everything. One TAHOT row and one alignment-keyed
// OSS-LXX-lemma row (via a real T4b "renumber" alignment, mirroring Ps9/10)
// round out coverage of both edition-reaching strategies.
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

	matBook := bookID(t, db, "MAT")
	actBook := bookID(t, db, "ACT")
	psaBook := bookID(t, db, "PSA")

	// Mat.1.1 - noise, must never appear in a G0859/ἄφεσις result.
	mat1v1 := insertVerse(t, db, "canonical", matBook, 1, 1)
	insertWord(t, db, mat1v1, "TAGNT", 1, "Βίβλος", "βίβλος", "G0976", "N-NSF")

	// Mat.26.28 - "...εἰς ἄφεσιν ἁμαρτιῶν", adjacent εἰς/ἄφεσις, the control
	// case (blood poured out εἰς ἄφεσιν - can't be "because of").
	mat26v28 := insertVerse(t, db, "canonical", matBook, 26, 28)
	insertWord(t, db, mat26v28, "TAGNT", 1, "τὸ", "ὁ", "G3588", "T-ASN")
	insertWord(t, db, mat26v28, "TAGNT", 2, "εἰς", "εἰς", "G1519", "PREP")
	insertWord(t, db, mat26v28, "TAGNT", 3, "ἄφεσιν", "ἄφεσις", "G0859", "N-ASF")
	insertWord(t, db, mat26v28, "TAGNT", 4, "ἁμαρτιῶν", "ἁμαρτία", "G0266", "N-GPF")

	// Act.2.38 - the ambiguous case, same adjacency.
	act2v38 := insertVerse(t, db, "canonical", actBook, 2, 38)
	insertWord(t, db, act2v38, "TAGNT", 1, "εἰς", "εἰς", "G1519", "PREP")
	insertWord(t, db, act2v38, "TAGNT", 2, "ἄφεσιν", "ἄφεσις", "G0859", "N-ASF")

	// A non-adjacent εἰς...ἄφεσις pair (one word of gap) - must be excluded
	// at window=0 (strictly adjacent) but included at window=1.
	act10v43 := insertVerse(t, db, "canonical", actBook, 10, 43)
	insertWord(t, db, act10v43, "TAGNT", 1, "εἰς", "εἰς", "G1519", "PREP")
	insertWord(t, db, act10v43, "TAGNT", 2, "τὸ", "ὁ", "G3588", "T-ASN")
	insertWord(t, db, act10v43, "TAGNT", 3, "ἄφεσιν", "ἄφεσις", "G0859", "N-ASF")

	// TAHOT row - canonical-keyed corpus, different match column exercise.
	insertWord(t, db, mat1v1, "TAHOT", 1, "בְּ", "בְּ", "H9003", "HR")

	// Ps.9.1 canonical aligns to OSS-LXX-lemma via a real T4b "renumber"
	// relation (mirrors the real Ps9/10 Hebrew/LXX divergence) - proves
	// ConcordLemma surfaces the divergence via Caveat, not a silent shift,
	// even for an alignment-keyed corpus.
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
	if _, err := db.Exec(`
		INSERT INTO words (verse_id, source_id, word_no, surface, lemma, dstrong, morph_code, attestation, editions, source_locator)
		VALUES (?, (SELECT id FROM sources WHERE code = ?), ?, ?, ?, ?, ?, 'N', 'NA28', ?)`,
		verseID, sourceCode, wordNo, surface, lemma, dstrong, morphCode, "loc#"+strconv.Itoa(wordSeq)); err != nil {
		t.Fatalf("insert word %s: %v", sourceCode, err)
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

func TestConcordLemmaByDStrongReturnsAllOccurrencesInclControlCase(t *testing.T) {
	db := setup(t)
	cs, err := concord.ConcordLemma(db, "G0859", "TAGNT", "")
	if err != nil {
		t.Fatalf("concord: %v", err)
	}
	if len(cs) != 3 {
		t.Fatalf("citations = %d, want 3 (Mat.26.28, Act.2.38, Act.10.43)", len(cs))
	}
	var sawControl bool
	for _, c := range cs {
		if c.Ref.Book == "MAT" && c.Ref.Chapter == 26 && c.Ref.Verse == 28 {
			sawControl = true
		}
		if c.DStrong != "G0859" {
			t.Errorf("citation %v has dstrong %q, want G0859", c.Ref, c.DStrong)
		}
	}
	if !sawControl {
		t.Error("Matt 26:28 control case missing from the result set")
	}
}

func TestConcordLemmaNeverReturnsNonMatchingRows(t *testing.T) {
	db := setup(t)
	cs, err := concord.ConcordLemma(db, "G0859", "TAGNT", "")
	if err != nil {
		t.Fatalf("concord: %v", err)
	}
	for _, c := range cs {
		if c.Ref.Book == "MAT" && c.Ref.Chapter == 1 && c.Ref.Verse == 1 {
			t.Error("Mat.1.1 (G0976, unrelated word) leaked into a G0859 query")
		}
	}
}

func TestConcordLemmaByPlainLemmaText(t *testing.T) {
	db := setup(t)
	cs, err := concord.ConcordLemma(db, "ἄφεσις", "TAGNT", "")
	if err != nil {
		t.Fatalf("concord: %v", err)
	}
	if len(cs) != 3 {
		t.Fatalf("citations = %d, want 3 (same set as the G0859 dstrong query)", len(cs))
	}
}

// TestConcordLemmaPopulatesTranslit is the direct T32 test: a words row
// carrying a transliteration must surface it on the returned Citation, not
// silently drop it the way SourceFile used to be a per-row field before T31
// moved it out - Translit is genuine per-row data, so it stays on Citation
// itself, unlike SourceFile/HomepageURL.
func TestConcordLemmaPopulatesTranslit(t *testing.T) {
	db := setup(t)
	// Update by dstrong+verse, not source_locator - the package-level
	// wordSeq counter that generates "loc#N" isn't reset between tests, so
	// its exact value here depends on test execution order.
	if _, err := db.Exec(`
		UPDATE words SET translit = 'aphesin'
		WHERE dstrong = 'G0859' AND verse_id = (
			SELECT v.id FROM verses v JOIN books b ON b.id = v.book_id
			WHERE b.code = 'MAT' AND v.chapter = 26 AND v.verse = 28)`); err != nil {
		t.Fatalf("seed translit: %v", err)
	}
	cs, err := concord.ConcordLemma(db, "G0859", "TAGNT", "")
	if err != nil {
		t.Fatalf("concord: %v", err)
	}
	var saw bool
	for _, c := range cs {
		if c.Ref.Book == "MAT" && c.Ref.Chapter == 26 && c.Ref.Verse == 28 {
			saw = true
			if c.Translit != "aphesin" {
				t.Errorf("translit = %q, want aphesin", c.Translit)
			}
		}
	}
	if !saw {
		t.Fatal("Mat.26.28 missing from result set")
	}
}

func TestCountAgreesWithConcordLemma(t *testing.T) {
	db := setup(t)
	cs, err := concord.ConcordLemma(db, "G0859", "TAGNT", "")
	if err != nil {
		t.Fatalf("concord: %v", err)
	}
	tally, err := concord.Count(db, "G0859", "TAGNT", "")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if tally.Total != len(cs) {
		t.Errorf("Count.Total = %d, len(ConcordLemma) = %d - must agree", tally.Total, len(cs))
	}
	if tally.ByBook["MAT"] != 1 || tally.ByBook["ACT"] != 2 {
		t.Errorf("ByBook = %v, want MAT:1 ACT:2", tally.ByBook)
	}
}

func TestConcordPhraseAdjacentFindsEisAphesis(t *testing.T) {
	db := setup(t)
	cs, err := concord.ConcordPhrase(db, []string{"εἰς", "ἄφεσις"}, "TAGNT", 0)
	if err != nil {
		t.Fatalf("concord phrase: %v", err)
	}
	if len(cs) != 2 {
		t.Fatalf("citations = %d, want 2 (Mat.26.28, Act.2.38 - Act.10.43 has a word between and must be excluded at window=0)", len(cs))
	}
	for _, c := range cs {
		if c.Text != "εἰς ἄφεσιν" {
			t.Errorf("text = %q, want the verbatim two-word span", c.Text)
		}
	}
}

func TestConcordPhraseWindowAllowsGap(t *testing.T) {
	db := setup(t)
	cs, err := concord.ConcordPhrase(db, []string{"εἰς", "ἄφεσις"}, "TAGNT", 1)
	if err != nil {
		t.Fatalf("concord phrase: %v", err)
	}
	if len(cs) != 3 {
		t.Fatalf("citations = %d, want 3 (window=1 also picks up Act.10.43's one-word gap)", len(cs))
	}
}

// TestConcordPhraseJoinsTranslitWhenEveryWordHasOne and its sibling below
// are the direct tests for chainTranslit's all-or-nothing join: Mat.26.28's
// εἰς/ἄφεσιν pair both get a translit seeded, Act.2.38's pair gets only one -
// the joined field must appear for the fully-seeded chain and stay empty for
// the partially-seeded one, never a join with a blank gap in it.
func TestConcordPhraseJoinsTranslitWhenEveryWordHasOne(t *testing.T) {
	db := setup(t)
	matVerse := `(SELECT v.id FROM verses v JOIN books b ON b.id = v.book_id WHERE b.code = 'MAT' AND v.chapter = 26 AND v.verse = 28)`
	if _, err := db.Exec(`UPDATE words SET translit = 'eis' WHERE lemma = 'εἰς' AND verse_id = ` + matVerse); err != nil {
		t.Fatalf("seed translit: %v", err)
	}
	if _, err := db.Exec(`UPDATE words SET translit = 'aphesin' WHERE lemma = 'ἄφεσις' AND verse_id = ` + matVerse); err != nil {
		t.Fatalf("seed translit: %v", err)
	}

	cs, err := concord.ConcordPhrase(db, []string{"εἰς", "ἄφεσις"}, "TAGNT", 0)
	if err != nil {
		t.Fatalf("concord phrase: %v", err)
	}
	for _, c := range cs {
		if c.Ref.Chapter == 26 && c.Ref.Verse == 28 {
			if c.Translit != "eis aphesin" {
				t.Errorf("translit = %q, want %q (both words seeded)", c.Translit, "eis aphesin")
			}
		}
		if c.Ref.Chapter == 2 && c.Ref.Verse == 38 {
			if c.Translit != "" {
				t.Errorf("translit = %q, want empty (Act.2.38 has no seeded translit at all)", c.Translit)
			}
		}
	}
}

func TestConcordPhraseLeavesTranslitEmptyWhenAnyWordIsMissingOne(t *testing.T) {
	db := setup(t)
	matVerse := `(SELECT v.id FROM verses v JOIN books b ON b.id = v.book_id WHERE b.code = 'MAT' AND v.chapter = 26 AND v.verse = 28)`
	// Seed only the εἰς word, not ἄφεσιν - a partial chain.
	if _, err := db.Exec(`UPDATE words SET translit = 'eis' WHERE lemma = 'εἰς' AND verse_id = ` + matVerse); err != nil {
		t.Fatalf("seed translit: %v", err)
	}

	cs, err := concord.ConcordPhrase(db, []string{"εἰς", "ἄφεσις"}, "TAGNT", 0)
	if err != nil {
		t.Fatalf("concord phrase: %v", err)
	}
	for _, c := range cs {
		if c.Ref.Chapter == 26 && c.Ref.Verse == 28 && c.Translit != "" {
			t.Errorf("translit = %q, want empty - a partial join must not appear as if complete", c.Translit)
		}
	}
}

func TestConcordLemmaAlignmentKeyedCorpusSurfacesDivergence(t *testing.T) {
	db := setup(t)
	cs, err := concord.ConcordLemma(db, "ἐξομολογήσομαι", "OSS-LXX-lemma", "")
	if err != nil {
		t.Fatalf("concord: %v", err)
	}
	if len(cs) != 1 {
		t.Fatalf("citations = %d, want 1", len(cs))
	}
	c := cs[0]
	if c.Ref.Book != "PSA" || c.Ref.Chapter != 9 || c.Ref.Verse != 1 {
		t.Errorf("ref = %v, want the canonical PSA.9.1 this OSS verse aligns to (not its own OSS chapter/verse numbering)", c.Ref)
	}
	if c.Confidence != retriever.ConfidenceFlagged || c.Caveat == "" {
		t.Errorf("confidence=%q caveat=%q, want Flagged with a caveat for a non-exact T4b relation", c.Confidence, c.Caveat)
	}
}

func TestConcordLemmaRejectsNonWordCorpus(t *testing.T) {
	db := setup(t)
	if _, err := concord.ConcordLemma(db, "G0859", "KJV", ""); err == nil {
		t.Fatal("expected an error querying a verse_text-only corpus for word concordance")
	}
}

func TestConcordLemmaUnknownDStrongReturnsEmptyNotError(t *testing.T) {
	db := setup(t)
	cs, err := concord.ConcordLemma(db, "G9999", "TAGNT", "")
	if err != nil {
		t.Fatalf("concord: %v", err)
	}
	if len(cs) != 0 {
		t.Errorf("citations = %d, want 0 (a real query with zero matches is not an error)", len(cs))
	}
}

func TestConcordPhraseRequiresAtLeastTwoTokens(t *testing.T) {
	db := setup(t)
	if _, err := concord.ConcordPhrase(db, []string{"εἰς"}, "TAGNT", 0); err == nil {
		t.Fatal("expected an error for a single-token phrase query")
	}
}

// oxiaBaptizo/tonosBaptizo are the two Unicode forms of the same word
// (baptizo): oxiaBaptizo uses the Greek Extended "oxia" accent (U+1F77) -
// the raw form STEPBible's TAGNT source files actually use before
// lexnorm.NFC (T10's loader now normalizes on ingest, so a freshly-built DB
// stores the NFC form - tonosBaptizo, U+03AF - instead). Visually
// identical, byte-different, canonically equivalent under NFC.
const (
	oxiaBaptizo  = "βαπτίζω"
	tonosBaptizo = "βαπτίζω"
)

func TestConcordLemmaMatchesAcrossPolytonicAndMonotonicUnicodeForms(t *testing.T) {
	db := setup(t)
	lukBook := bookID(t, db, "LUK")
	v := insertVerse(t, db, "canonical", lukBook, 99, 1)
	insertWord(t, db, v, "TAGNT", 1, "βαπτίζων", tonosBaptizo, "G0907", "V-PAP-NSM")

	cs, err := concord.ConcordLemma(db, oxiaBaptizo, "TAGNT", "")
	if err != nil {
		t.Fatalf("concord: %v", err)
	}
	if len(cs) != 1 {
		t.Fatalf("citations = %d, want 1 - a monotonic-typed query must match a polytonic-stored lemma (lexnorm.NFC)", len(cs))
	}
}

// TestConcordLemmaBySurfaceMatchesExactInflectedForm is the direct T33 test:
// Mat.1.1's word has surface "Βίβλος" (capitalized, as it appears in the
// verse) but lemma "βίβλος" (lowercase dictionary form) - two different
// strings. by="surface" must match against the literal surface text, not
// silently fall back to lemma.
func TestConcordLemmaBySurfaceMatchesExactInflectedForm(t *testing.T) {
	db := setup(t)
	cs, err := concord.ConcordLemma(db, "Βίβλος", "TAGNT", "surface")
	if err != nil {
		t.Fatalf("concord: %v", err)
	}
	if len(cs) != 1 {
		t.Fatalf("citations = %d, want 1 (Mat.1.1's exact surface form)", len(cs))
	}
	if cs[0].Ref.Book != "MAT" || cs[0].Ref.Chapter != 1 || cs[0].Ref.Verse != 1 {
		t.Errorf("ref = %v, want MAT.1.1", cs[0].Ref)
	}
}

// TestConcordLemmaByExplicitLemmaDoesNotMatchSurfaceText confirms an
// explicit by="lemma" restricts the search to the lemma column only - the
// same surface-vs-lemma string ("Βίβλος" vs "βίβλος") must NOT match when
// the caller explicitly asked for lemma, proving by isn't a hint that also
// widens the search, it's a hard column selector.
func TestConcordLemmaByExplicitLemmaDoesNotMatchSurfaceText(t *testing.T) {
	db := setup(t)
	cs, err := concord.ConcordLemma(db, "Βίβλος", "TAGNT", "lemma")
	if err != nil {
		t.Fatalf("concord: %v", err)
	}
	if len(cs) != 0 {
		t.Errorf("citations = %d, want 0 - by=\"lemma\" must not match the capitalized surface form", len(cs))
	}
}

// TestConcordLemmaRejectsUnknownByValue: an invalid by value is a caller
// error, not silently treated as auto-detect.
func TestConcordLemmaRejectsUnknownByValue(t *testing.T) {
	db := setup(t)
	if _, err := concord.ConcordLemma(db, "Βίβλος", "TAGNT", "bogus"); err == nil {
		t.Fatal("expected an error for an unknown by value")
	}
}

// TestCountRespectsByOverride confirms Count's by parameter runs the
// identical WHERE clause ConcordLemma would, same as the query/corpus
// agreement TestCountAgreesWithConcordLemma already covers for the default
// auto-detect path.
func TestCountRespectsByOverride(t *testing.T) {
	db := setup(t)
	cs, err := concord.ConcordLemma(db, "Βίβλος", "TAGNT", "surface")
	if err != nil {
		t.Fatalf("concord: %v", err)
	}
	tally, err := concord.Count(db, "Βίβλος", "TAGNT", "surface")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if tally.Total != len(cs) {
		t.Errorf("Count.Total = %d, len(ConcordLemma) = %d - must agree", tally.Total, len(cs))
	}
}

func TestConcordPhraseMatchesAcrossPolytonicAndMonotonicUnicodeForms(t *testing.T) {
	db := setup(t)
	lukBook := bookID(t, db, "LUK")
	// Mirrors the real bug: a corpus row stored in the oxia form (as
	// STEPBible's TAGNT actually is) sat adjacent to an already-tonos-typed
	// word, and a tonos-typed phrase query found nothing - not because the
	// words weren't adjacent, but because the byte comparison silently
	// failed. window=0 (strict adjacency) must still find this pair once
	// both sides are normalized to the same form.
	v := insertVerse(t, db, "canonical", lukBook, 99, 2)
	insertWord(t, db, v, "TAGNT", 1, "βαπτισθεὶς", tonosBaptizo, "G0907", "V-APP-NSM")
	insertWord(t, db, v, "TAGNT", 2, "ἄφεσιν", "ἄφεσις", "G0859", "N-ASF")

	cs, err := concord.ConcordPhrase(db, []string{oxiaBaptizo, "ἄφεσις"}, "TAGNT", 0)
	if err != nil {
		t.Fatalf("concord phrase: %v", err)
	}
	if len(cs) != 1 {
		t.Fatalf("citations = %d, want 1 - adjacency must be found even though the DB stores the oxia form and the query used the tonos form", len(cs))
	}
}
