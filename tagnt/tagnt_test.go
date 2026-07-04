package tagnt_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/sources"
	"github.com/jrainsberger/orthotomeo/store"
	"github.com/jrainsberger/orthotomeo/tagnt"
	"github.com/jrainsberger/orthotomeo/verses"
)

const miniSpine = `{"books":[
  {"name":"Matthew","chapters":[{"chapter":1,"verses":[{"verse":1}]},{"chapter":4,"verses":[{"verse":6}]}]},
  {"name":"John","chapters":[{"chapter":1,"verses":[{"verse":1},{"verse":2}]}]}
]}`

// Mirrors the real TAGNT shape: the "Word & Type" column header and a
// Greek/English/grammar preview block repeat before EVERY verse (not just
// once at the top of the file) - the loader must key off the ref field's
// own shape, not line position. Includes a normal verse (Mat.1.1), a
// compound-tagged word (Mat.4.6, one surface token spanning two Strong's
// numbers), and a verse not in the mini spine (Mark.1.1, must be skipped).
const fixtureTAGNT = "TAGNT - Translators Amalgamated Greek New Testament\n" +
	"Editions\tByz=Byzantine...\n" +
	"\n" +
	"# Mat.1.1\tΒίβλος \tγενέσεως \n" +
	"#_Translation\t[The] book\tof [the] genealogy\n" +
	"#_Word=Grammar\tG0976=N-NSF\tG1078=N-GSF\n" +
	"#_Significant variant\n" +
	"\n" +
	"Word & Type\tGreek\tEnglish translation\tdStrongs = Grammar\tDictionary form =  Gloss\teditions\n" +
	"Mat.1.1#01=NKO\tΒίβλος (Biblos)\t[The] book\tG0976=N-NSF\tβίβλος=book\tNA28+NA27+Tyn+SBL+WH+Treg+TR+Byz\n" +
	"Mat.1.1#02=NKO\tγενέσεως (geneseōs)\tof [the] genealogy\tG1078=N-GSF\tγένεσις=origin\tNA28+NA27+Tyn+SBL+WH+Treg+TR+Byz\n" +
	"\n" +
	"Word & Type\tGreek\tEnglish translation\tdStrongs = Grammar\tDictionary form =  Gloss\teditions\n" +
	"Mat.4.6#26=NKO\tμήποτε (mēpote)\totherwise\tG3361=PRT-N + G4218=PRT\tμήποτε=lest + πότε=when\tNA28+NA27+Tyn+SBL+WH+Treg+TR+Byz\n" +
	"\n" +
	"Word & Type\tGreek\tEnglish translation\tdStrongs = Grammar\tDictionary form =  Gloss\teditions\n" +
	"Jhn.1.1#02=NKO\tἀρχῇ (archē)\tbeginning\tG0746=N-DSF\tἀρχή=beginning\tNA28+NA27+Tyn+SBL+WH+Treg+TR+Byz\n" +
	"\n" +
	"Word & Type\tGreek\tEnglish translation\tdStrongs = Grammar\tDictionary form =  Gloss\teditions\n" +
	"Mark.1.1#01=K\tἀρχὴ (archē)\tbeginning\tG0746=N-NSF\tἀρχή=beginning\tTR\n" +
	"\n" +
	// A real dual-numbered ref shape (e.g. Rom.3.25(3.26) in the actual
	// corpus): English/standard verse 2 outside the parens, an edition's own
	// differing verse split (here 1.3, arbitrary) inside. Before refRe
	// tolerated this, the row matched nothing and vanished uncounted.
	"Word & Type\tGreek\tEnglish translation\tdStrongs = Grammar\tDictionary form =  Gloss\teditions\n" +
	"Jhn.1.2(1.3)#01=NKO\tκαι (kai)\tand\tG2532=CONJ\tκαί=and\tNA28+NA27+Tyn+SBL+WH+Treg+TR+Byz\n"

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

func TestLoadSkipsHeaderAndPreviewBlocks(t *testing.T) {
	db := setup(t)

	inserted, skipped, compound, err := tagnt.Load(db, strings.NewReader(fixtureTAGNT))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// 5 real data rows (Mat.1.1#01, #02, Mat.4.6#26, Jhn.1.1#02,
	// Jhn.1.2(1.3)#01); Mark.1.1#01 is not in the mini spine and must be
	// skipped, not inserted.
	if inserted != 5 {
		t.Errorf("inserted = %d, want 5", inserted)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1 (Mark.1.1 not in mini spine)", skipped)
	}
	if compound != 1 {
		t.Errorf("compound = %d, want 1 (Mat.4.6#26)", compound)
	}

	var rows int
	db.QueryRow(`SELECT COUNT(*) FROM words`).Scan(&rows)
	if rows != 5 {
		t.Errorf("words table has %d rows, want 5", rows)
	}
}

func TestLoadToleratesDualNumberedRef(t *testing.T) {
	db := setup(t)
	if _, _, _, err := tagnt.Load(db, strings.NewReader(fixtureTAGNT)); err != nil {
		t.Fatalf("load: %v", err)
	}

	var ch, v int
	err := db.QueryRow(`
		SELECT verse.chapter, verse.verse FROM words w
		JOIN verses verse ON verse.id = w.verse_id
		WHERE w.source_locator = 'Jhn.1.2(1.3)#01=NKO'`).Scan(&ch, &v)
	if err != nil {
		t.Fatalf("query Jhn.1.2(1.3)#01: %v (the row was silently dropped)", err)
	}
	if ch != 1 || v != 2 {
		t.Errorf("resolved to %d:%d, want 1:2 (the English/standard number outside the parens)", ch, v)
	}
}

func TestLoadParsesWordFields(t *testing.T) {
	db := setup(t)
	if _, _, _, err := tagnt.Load(db, strings.NewReader(fixtureTAGNT)); err != nil {
		t.Fatalf("load: %v", err)
	}

	var surface, lemma, dstrong, attestation, editions string
	var wordNo int
	err := db.QueryRow(`
		SELECT word_no, surface, lemma, dstrong, attestation, editions
		FROM words WHERE source_locator = 'Jhn.1.1#02=NKO'`).
		Scan(&wordNo, &surface, &lemma, &dstrong, &attestation, &editions)
	if err != nil {
		t.Fatalf("query Jhn.1.1#02: %v", err)
	}
	if wordNo != 2 {
		t.Errorf("word_no = %d, want 2", wordNo)
	}
	if surface != "ἀρχῇ" {
		t.Errorf("surface = %q, want ἀρχῇ (translit stripped)", surface)
	}
	if lemma != "ἀρχή" {
		t.Errorf("lemma = %q, want ἀρχή", lemma)
	}
	if dstrong != "G0746" {
		t.Errorf("dstrong = %q, want G0746", dstrong)
	}
	if attestation != "NKO" {
		t.Errorf("attestation = %q, want NKO", attestation)
	}
	if editions != "NA28+NA27+Tyn+SBL+WH+Treg+TR+Byz" {
		t.Errorf("editions = %q, want the full edition list", editions)
	}
}

// TestLoadExtractsTransliteration is the direct T32 test: the Greek column
// carries "SURFACE (translit)" - the loader must split it, storing the
// clean surface form (already covered by TestLoadParsesWordFields) AND the
// transliteration, not silently discard the parenthetical as it did before.
func TestLoadExtractsTransliteration(t *testing.T) {
	db := setup(t)
	if _, _, _, err := tagnt.Load(db, strings.NewReader(fixtureTAGNT)); err != nil {
		t.Fatalf("load: %v", err)
	}

	var translit string
	err := db.QueryRow(`SELECT translit FROM words WHERE source_locator = 'Jhn.1.1#02=NKO'`).Scan(&translit)
	if err != nil {
		t.Fatalf("query Jhn.1.1#02: %v", err)
	}
	if translit != "archē" {
		t.Errorf("translit = %q, want archē", translit)
	}
}

// TestLoadExtractsTransliterationForCompoundWord confirms transliteration is
// captured even when dstrong/lemma are NULL (a compound-tagged word) - the
// two are independent: a word can have no single dStrong yet still carry a
// real, storable transliteration for its whole surface span.
func TestLoadExtractsTransliterationForCompoundWord(t *testing.T) {
	db := setup(t)
	if _, _, _, err := tagnt.Load(db, strings.NewReader(fixtureTAGNT)); err != nil {
		t.Fatalf("load: %v", err)
	}

	var translit string
	err := db.QueryRow(`SELECT translit FROM words WHERE source_locator = 'Mat.4.6#26=NKO'`).Scan(&translit)
	if err != nil {
		t.Fatalf("query Mat.4.6#26: %v", err)
	}
	if translit != "mēpote" {
		t.Errorf("translit = %q, want mēpote (transliteration survives even for a compound-tagged word)", translit)
	}
}

func TestLoadVariantVerseIsAllK(t *testing.T) {
	db := setup(t)
	if _, _, _, err := tagnt.Load(db, strings.NewReader(fixtureTAGNT)); err != nil {
		t.Fatalf("load: %v", err)
	}

	var attestation, editions string
	err := db.QueryRow(`SELECT attestation, editions FROM words WHERE source_locator = 'Mat.4.6#26=NKO'`).
		Scan(&attestation, &editions)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if attestation != "NKO" || editions != "NA28+NA27+Tyn+SBL+WH+Treg+TR+Byz" {
		t.Errorf("attestation=%q editions=%q", attestation, editions)
	}
}

func TestLoadNullsCompoundWordDstrongAndLemma(t *testing.T) {
	db := setup(t)
	if _, _, _, err := tagnt.Load(db, strings.NewReader(fixtureTAGNT)); err != nil {
		t.Fatalf("load: %v", err)
	}

	var dstrong, lemma sql.NullString
	err := db.QueryRow(`SELECT dstrong, lemma FROM words WHERE source_locator = 'Mat.4.6#26=NKO'`).
		Scan(&dstrong, &lemma)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if dstrong.Valid || lemma.Valid {
		t.Errorf("compound word dstrong/lemma = %v/%v, want both NULL", dstrong, lemma)
	}
}

func TestLoadSourceLocatorIsTheRowKey(t *testing.T) {
	db := setup(t)
	if _, _, _, err := tagnt.Load(db, strings.NewReader(fixtureTAGNT)); err != nil {
		t.Fatalf("load: %v", err)
	}

	// Two distinct words in the same verse (Mat.1.1) share a verse_id and
	// source_id but must NOT collide - source_locator is the unique key,
	// not (verse_id, source_id, word_no).
	var count int
	db.QueryRow(`
		SELECT COUNT(*) FROM words w
		JOIN verses v ON v.id = w.verse_id
		JOIN books b ON b.id = v.book_id
		WHERE b.full_name = 'Matthew' AND v.chapter = 1 AND v.verse = 1`).Scan(&count)
	if count != 2 {
		t.Errorf("Mat.1.1 word count = %d, want 2", count)
	}
}
