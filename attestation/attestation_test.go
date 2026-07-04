package attestation_test

import (
	"database/sql"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/jrainsberger/orthotomeo/attestation"
	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/retriever"
	"github.com/jrainsberger/orthotomeo/sources"
	"github.com/jrainsberger/orthotomeo/store"
)

// setup mirrors the real Mark 16:9-20 case (Type "KO" - attested in
// Traditional/Other manuscripts, absent from the Nestle-Aland base text)
// alongside an ordinary all-manuscript TAGNT word, a TAHOT Ketiv/Qere row,
// and a Swete row (no attestation apparatus at all - T12), reached via a
// real T4b "renumber" alignment to prove non-exact relations get flagged
// here too, not just in Parse/Concord.
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

	mrkBook := bookID(t, db, "MRK")
	psaBook := bookID(t, db, "PSA")

	mrk1v1 := insertVerse(t, db, "canonical", mrkBook, 1, 1)
	insertWord(t, db, mrk1v1, "TAGNT", 1, "Ἀρχὴ", "ἀρχή", "G0746", "NKO", "NA28+NA27+Tyn+SBL+WH+Treg+TR+Byz")
	insertWord(t, db, mrk1v1, "TAHOT", 1, "בְּ", "בְּ", "H9003", "Q(K)", "")

	mrk16v9 := insertVerse(t, db, "canonical", mrkBook, 16, 9)
	insertWord(t, db, mrk16v9, "TAGNT", 1, "Ἀναστὰς", "ἀνίστημι", "G0450", "KO", "TR+Byz")

	sweteMrk1v1 := insertVerse(t, db, "lxx-swete", mrkBook, 1, 1)
	insertAlignment(t, db, mrk1v1, sweteMrk1v1, "Swete", "exact", 1.0)
	insertSweteWord(t, db, sweteMrk1v1, 1, "Ἀρχή")

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

func insertWord(t *testing.T, db *sql.DB, verseID int64, sourceCode string, wordNo int, surface, lemma, dstrong, attestation, editions string) {
	t.Helper()
	wordSeq++
	if _, err := db.Exec(`
		INSERT INTO words (verse_id, source_id, word_no, surface, lemma, dstrong, morph_code, attestation, editions, source_locator)
		VALUES (?, (SELECT id FROM sources WHERE code = ?), ?, ?, ?, ?, NULL, ?, ?, ?)`,
		verseID, sourceCode, wordNo, surface, lemma, dstrong, attestation, editions, "loc#"+strconv.Itoa(wordSeq)); err != nil {
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

func TestAttestationReturnsTypeAndEditionsAsNeutralData(t *testing.T) {
	db := setup(t)
	nine := 1
	cs, err := attestation.Attestation(db, retriever.Ref{Book: "MRK", Chapter: 16, Verse: 9}, &nine, "TAGNT")
	if err != nil {
		t.Fatalf("attestation: %v", err)
	}
	if len(cs) != 1 {
		t.Fatalf("citations = %d, want 1", len(cs))
	}
	c := cs[0]
	if c.Attestation != "KO" {
		t.Errorf("attestation = %q, want KO (the real Mark 16:9-20 shape)", c.Attestation)
	}
	if c.Manuscripts != "TR+Byz" {
		t.Errorf("manuscripts = %q, want TR+Byz", c.Manuscripts)
	}
	// KO/TR+Byz is reportable, neutral data about which manuscripts carry
	// the word - it is NOT itself a reason to flag the citation (that would
	// be arguing the variant, which T18 explicitly must not do).
	if c.Confidence != retriever.ConfidenceHigh {
		t.Errorf("confidence = %q, want High - a KO attestation is data, not a defect", c.Confidence)
	}
}

// TestAttestationPopulatesTranslit is the direct T32 test: buildCitation
// must forward a words row's transliteration onto the Citation.
func TestAttestationPopulatesTranslit(t *testing.T) {
	db := setup(t)
	if _, err := db.Exec(`UPDATE words SET translit = 'anastas' WHERE dstrong = 'G0450'`); err != nil {
		t.Fatalf("seed translit: %v", err)
	}
	nine := 1
	cs, err := attestation.Attestation(db, retriever.Ref{Book: "MRK", Chapter: 16, Verse: 9}, &nine, "TAGNT")
	if err != nil {
		t.Fatalf("attestation: %v", err)
	}
	if len(cs) != 1 {
		t.Fatalf("citations = %d, want 1", len(cs))
	}
	if cs[0].Translit != "anastas" {
		t.Errorf("translit = %q, want anastas", cs[0].Translit)
	}
}

func TestAttestationOrdinaryWordIsNKO(t *testing.T) {
	db := setup(t)
	cs, err := attestation.Attestation(db, retriever.Ref{Book: "MRK", Chapter: 1, Verse: 1}, nil, "TAGNT")
	if err != nil {
		t.Fatalf("attestation: %v", err)
	}
	if len(cs) != 1 {
		t.Fatalf("citations = %d, want 1", len(cs))
	}
	if cs[0].Attestation != "NKO" {
		t.Errorf("attestation = %q, want NKO", cs[0].Attestation)
	}
}

func TestAttestationTAHOTCarriesKetivQereMarker(t *testing.T) {
	db := setup(t)
	cs, err := attestation.Attestation(db, retriever.Ref{Book: "MRK", Chapter: 1, Verse: 1}, nil, "TAHOT")
	if err != nil {
		t.Fatalf("attestation: %v", err)
	}
	if len(cs) != 1 {
		t.Fatalf("citations = %d, want 1", len(cs))
	}
	if cs[0].Attestation != "Q(K)" {
		t.Errorf("attestation = %q, want Q(K) preserved verbatim", cs[0].Attestation)
	}
}

func TestAttestationSweteFlaggedNoApparatus(t *testing.T) {
	db := setup(t)
	cs, err := attestation.Attestation(db, retriever.Ref{Book: "MRK", Chapter: 1, Verse: 1}, nil, "Swete")
	if err != nil {
		t.Fatalf("attestation: %v", err)
	}
	if len(cs) != 1 {
		t.Fatalf("citations = %d, want 1", len(cs))
	}
	c := cs[0]
	if c.Text != "Ἀρχή" {
		t.Errorf("text = %q, want the real surface form (Swete still HAS the word)", c.Text)
	}
	if c.Confidence != retriever.ConfidenceFlagged || c.Caveat == "" {
		t.Errorf("confidence=%q caveat=%q, want Flagged (Swete carries no attestation apparatus - T12)", c.Confidence, c.Caveat)
	}
}

func TestAttestationAlignmentKeyedNonExactRelationFlagged(t *testing.T) {
	db := setup(t)
	cs, err := attestation.Attestation(db, retriever.Ref{Book: "PSA", Chapter: 9, Verse: 1}, nil, "OSS-LXX-lemma")
	if err != nil {
		t.Fatalf("attestation: %v", err)
	}
	if len(cs) != 1 {
		t.Fatalf("citations = %d, want 1", len(cs))
	}
	if cs[0].Confidence != retriever.ConfidenceFlagged {
		t.Error("a T4b renumber relation must be Flagged, not High")
	}
}

func TestAttestationNoAlignmentReturnsFlaggedPlaceholderNotEmpty(t *testing.T) {
	db := setup(t)
	cs, err := attestation.Attestation(db, retriever.Ref{Book: "PSA", Chapter: 9, Verse: 1}, nil, "Swete")
	if err != nil {
		t.Fatalf("attestation: %v", err)
	}
	if len(cs) != 1 {
		t.Fatalf("citations = %d, want 1 (a placeholder, not a silent empty slice)", len(cs))
	}
	if cs[0].Confidence != retriever.ConfidenceFlagged || cs[0].Caveat == "" {
		t.Error("expected a Flagged placeholder with a caveat, not a bare empty citation")
	}
}

func TestAttestationRejectsNonWordCorpus(t *testing.T) {
	db := setup(t)
	if _, err := attestation.Attestation(db, retriever.Ref{Book: "MRK", Chapter: 1, Verse: 1}, nil, "KJV"); err == nil {
		t.Fatal("expected an error for a verse_text-only corpus")
	}
}
