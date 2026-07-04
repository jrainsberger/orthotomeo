package lexicon_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jrainsberger/orthotomeo/lexicon"
	"github.com/jrainsberger/orthotomeo/store"
)

// Mirrors the real TBESG shape: front-matter prose (including a decoy
// "eStrong#" mention), the real header row, a "===" rule, a blank line, then
// tab-delimited data rows. dStrong carries a relation annotation after the
// key; uStrong sometimes has a trailing ", ".
const fixtureTBESG = "$========== PERSON(s)\n" +
	"- Named	Herod@Mat.2.1	G2264G«G2264=Ἡρώδης	Herod	https://...\n" +
	"* Gloss = a meaning (eStrong# mentioned here is not a header)\n" +
	"eStrong	dStrong	uStrong	Greek	Transliteration	Morph	Gloss	Abbott-Smith lexicon (AS)\n" +
	"===============================================================\n" +
	"\n" +
	"G0746\tG0746 =\tG0746\tἀρχή\tarchē\tG:N-F\tbeginning\t<b>ἀρχή</b>, -ῆς, ἡ beginning, origin\n" +
	"G0007\tG0007H = the Greek of\tH0029L, \tἈβιά\tAbia\tN:N-M-P\tAbijah\tson of Rehoboam\n"

const fixtureTBESH = "eStrong#\tdStrong\tuStrong\tHebrew\tTransliteration\tMorph\tGloss\tMeaning\n" +
	"\n" +
	"H0001\tH0001G =\tH0001G\tאָב\tav\tH:N-M\tfather\tfather of an individual\n"

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
	return db
}

func TestLoadSkipsFrontMatterAndExtractsDStrong(t *testing.T) {
	db := setup(t)

	n, err := lexicon.Load(db, strings.NewReader(fixtureTBESG), "grc", "Abbott-Smith PD")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if n != 2 {
		t.Errorf("inserted %d, want 2", n)
	}

	e, err := lexicon.Lookup(db, "G0746")
	if err != nil {
		t.Fatalf("lookup G0746: %v", err)
	}
	if e.Lemma != "ἀρχή" || e.Gloss != "beginning" {
		t.Errorf("G0746 = %q/%q, want ἀρχή/beginning", e.Lemma, e.Gloss)
	}
}

// TestLookupPopulatesDefinitionForGreek is the direct T34 test: a Greek
// (TBESG) entry's definition is Abbott-Smith 1922, Public Domain - clear to
// expose, so Lookup must return it, not silently withhold it too.
func TestLookupPopulatesDefinitionForGreek(t *testing.T) {
	db := setup(t)
	if _, err := lexicon.Load(db, strings.NewReader(fixtureTBESG), "grc", "Abbott-Smith PD"); err != nil {
		t.Fatalf("load: %v", err)
	}
	e, err := lexicon.Lookup(db, "G0746")
	if err != nil {
		t.Fatalf("lookup G0746: %v", err)
	}
	if e.Definition == nil {
		t.Fatal("Definition = nil, want the Abbott-Smith text for a Greek entry")
	}
	if !strings.Contains(*e.Definition, "beginning, origin") {
		t.Errorf("Definition = %q, want it to contain the fixture's Abbott-Smith text", *e.Definition)
	}
	if e.Translit != "archē" {
		t.Errorf("translit = %q, want archē", e.Translit)
	}
}

// TestLookupWithholdsDefinitionForHebrew is the direct T34 license-gate
// test: a Hebrew (TBESH) entry's definition column IS present in the source
// data (loaded, not blank), but Lookup must return Definition == nil anyway -
// the gate is the license (BDB via Online Bible, permission required), not
// whether the column happens to be empty.
func TestLookupWithholdsDefinitionForHebrew(t *testing.T) {
	db := setup(t)
	if _, err := lexicon.Load(db, strings.NewReader(fixtureTBESH), "he", "BDB/Online Bible - permission"); err != nil {
		t.Fatalf("load: %v", err)
	}
	e, err := lexicon.Lookup(db, "H0001G")
	if err != nil {
		t.Fatalf("lookup H0001G: %v", err)
	}
	if e.Gloss != "father" {
		t.Errorf("gloss = %q, want father (gloss is always clear to expose)", e.Gloss)
	}
	if e.Definition != nil {
		t.Errorf("Definition = %q, want nil - Hebrew definitions are withheld pending permission (T34)", *e.Definition)
	}

	var rawDefinition string
	if err := db.QueryRow(`SELECT definition FROM lexicon WHERE dstrong = 'H0001G'`).Scan(&rawDefinition); err != nil {
		t.Fatalf("query raw definition: %v", err)
	}
	if rawDefinition != "father of an individual" {
		t.Fatalf("test fixture assumption broken: raw definition = %q, want a non-empty value so this test actually proves withholding, not just an empty column", rawDefinition)
	}
}

// TestLookupUnknownDStrongReturnsError confirms a miss is a real error, not
// a zero-value Entry indistinguishable from "found, but empty."
func TestLookupUnknownDStrongReturnsError(t *testing.T) {
	db := setup(t)
	if _, err := lexicon.Load(db, strings.NewReader(fixtureTBESG), "grc", "Abbott-Smith PD"); err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, err := lexicon.Lookup(db, "G9999"); err == nil {
		t.Fatal("expected an error for an unknown dStrong")
	}
}

func TestLoadStripsDStrongAnnotationAndUStrongTrailer(t *testing.T) {
	db := setup(t)
	if _, err := lexicon.Load(db, strings.NewReader(fixtureTBESG), "grc", "Abbott-Smith PD"); err != nil {
		t.Fatalf("load: %v", err)
	}

	var dstrong, ustrong string
	err := db.QueryRow(`SELECT dstrong, ustrong FROM lexicon WHERE estrong = 'G0007'`).Scan(&dstrong, &ustrong)
	if err != nil {
		t.Fatalf("query G0007: %v", err)
	}
	if dstrong != "G0007H" {
		t.Errorf("dstrong = %q, want G0007H (annotation stripped)", dstrong)
	}
	if ustrong != "H0029L" {
		t.Errorf("ustrong = %q, want H0029L (trailing comma trimmed)", ustrong)
	}
}

func TestLoadTagsLanguageAndDefLicense(t *testing.T) {
	db := setup(t)
	if _, err := lexicon.Load(db, strings.NewReader(fixtureTBESG), "grc", "Abbott-Smith PD"); err != nil {
		t.Fatalf("load greek: %v", err)
	}
	if _, err := lexicon.Load(db, strings.NewReader(fixtureTBESH), "he", "BDB/Online Bible - permission"); err != nil {
		t.Fatalf("load hebrew: %v", err)
	}

	var bad int
	if err := db.QueryRow(`SELECT COUNT(*) FROM lexicon WHERE language = '' OR def_license = '' OR ustrong = ''`).Scan(&bad); err != nil {
		t.Fatalf("count bad: %v", err)
	}
	if bad != 0 {
		t.Errorf("%d rows missing language/def_license/ustrong", bad)
	}

	var heLicense string
	if err := db.QueryRow(`SELECT def_license FROM lexicon WHERE dstrong = 'H0001G'`).Scan(&heLicense); err != nil {
		t.Fatalf("query H0001G: %v", err)
	}
	if heLicense != "BDB/Online Bible - permission" {
		t.Errorf("Hebrew def_license = %q, want BDB/Online Bible - permission", heLicense)
	}

	var total int
	if err := db.QueryRow(`SELECT COUNT(*) FROM lexicon`).Scan(&total); err != nil {
		t.Fatalf("count total: %v", err)
	}
	if total != 3 {
		t.Errorf("total rows = %d, want 3", total)
	}
}
