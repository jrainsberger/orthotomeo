package morph_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jrainsberger/orthotomeo/morph"
	"github.com/jrainsberger/orthotomeo/store"
)

// Mirrors the real TEGMC/TEHMC shape: a "BRIEF LEXICAL" table before the
// marker (must be ignored), the FULL MORPHOLOGY CODES marker, an explanatory
// preamble, the quoted column-header row, four-line code entries (code+
// expansion, phrase, description, example) separated by blank lines, and a
// trailing "<code>\tKJV" appendix that must also be ignored.
const fixtureTEGMC = "BRIEF LEXICAL MORPHOLOGY CODES:\n" +
	"================\n" +
	"G:A\tthoughtful\tGreek Adjective\n" +
	"\n" +
	"FULL MORPHOLOGY CODES:\n" +
	"================\n" +
	"These codes are used in the tagged texts.\n" +
	"=====================================\n" +
	"\n" +
	"\"1 CODE\"\tGroup#1\tspecific Function\n" +
	"\n" +
	"V-IAI-3S\tFunction=Verb; Tense=Imperfect; Voice=Active; Mood=Indicative; Person=3rd; Number=Singular\n" +
	"\tVerb Imperfect Active Indicative 3rd Singular\n" +
	"\tan action that was happening\n" +
	"\t\"he _was teaching_\"\n" +
	"\n" +
	"V-PAI-3S\tFunction=Verb; Tense=Present; Voice=Active; Mood=Indicative; Person=3rd; Number=Singular\n" +
	"\tVerb Present Active Indicative 3rd Singular\n" +
	"\tan action that is happening\n" +
	"\t\"he _teaches_\"\n" +
	"\n" +
	"\n" +
	"HEB\tKJV\n" +
	"S-1PASM\tKJV\n"

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

func TestLoadIgnoresBriefTableAndAppendix(t *testing.T) {
	db := setup(t)

	n, err := morph.Load(db, strings.NewReader(fixtureTEGMC), "grc")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if n != 2 {
		t.Errorf("inserted %d, want 2 (brief table and KJV appendix excluded)", n)
	}

	var bad int
	if err := db.QueryRow(`SELECT COUNT(*) FROM morph_codes WHERE code IN ('G:A', 'HEB', 'S-1PASM', '"1 CODE"')`).Scan(&bad); err != nil {
		t.Fatalf("count bad: %v", err)
	}
	if bad != 0 {
		t.Errorf("%d brief/appendix/header rows leaked into morph_codes", bad)
	}
}

func TestExpandReturnsDescription(t *testing.T) {
	db := setup(t)
	if _, err := morph.Load(db, strings.NewReader(fixtureTEGMC), "grc"); err != nil {
		t.Fatalf("load: %v", err)
	}

	desc, err := morph.Expand(db, "V-IAI-3S")
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	want := "Function=Verb; Tense=Imperfect; Voice=Active; Mood=Indicative; Person=3rd; Number=Singular"
	if desc != want {
		t.Errorf("V-IAI-3S = %q, want %q", desc, want)
	}
}

func TestLoadTagsLanguage(t *testing.T) {
	db := setup(t)
	if _, err := morph.Load(db, strings.NewReader(fixtureTEGMC), "grc"); err != nil {
		t.Fatalf("load: %v", err)
	}

	var lang string
	if err := db.QueryRow(`SELECT language FROM morph_codes WHERE code = 'V-PAI-3S'`).Scan(&lang); err != nil {
		t.Fatalf("query: %v", err)
	}
	if lang != "grc" {
		t.Errorf("language = %q, want grc", lang)
	}
}
