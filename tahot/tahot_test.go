package tahot_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/sources"
	"github.com/jrainsberger/orthotomeo/store"
	"github.com/jrainsberger/orthotomeo/tahot"
	"github.com/jrainsberger/orthotomeo/verses"
)

const miniSpine = `{"books":[
  {"name":"Genesis","chapters":[{"chapter":1,"verses":[{"verse":1}]}]},
  {"name":"Isaiah","chapters":[{"chapter":44,"verses":[{"verse":24}]}]}
]}`

// Mirrors the real TAHOT shape: a Hebrew/translation/grammar preview block
// and the column header repeat before every verse. Word #1 carries a
// prefix+root split (dStrongs "H9003/{H7225G}"); word #2 is a plain single
// Strong's already wrapped in braces; Exo.1.1 is not in the mini spine and
// must be skipped. The untagged row mirrors the real Isa.44.24#16 Qere
// reading with no braced segment anywhere.
const fixtureTAHOT = "TAHOT - Translators Amalgamated Hebrew Old Testament\n" +
	"Abbreviations for sources of each word:\n" +
	"\n" +
	"# Gen.1.1\tbe.re.Shit (בְּרֵאשִׁ֖ית)\tba.Ra' (בָּרָ֣א)\n" +
	"#_Translation\tin/ beginning\the created\n" +
	"#_Word+Grammar\tH9003/H7225G=HR/Ncfsa\tH1254A=HVqp3ms\n" +
	"#_Significant variant\n" +
	"\n" +
	"Eng (Heb) Ref & Type\tHebrew\tTransliteration\tTranslation\tdStrongs\tGrammar\tMeaning Variants\tSpelling Variants\tRoot dStrong+Instance\tAlternative Strongs+Instance\tConjoin word\tExpanded Strong tags\n" +
	"Gen.1.1#01=L\tבְּ/רֵאשִׁ֖ית\tbe./re.Shit\tin/ beginning\tH9003/{H7225G}\tHR/Ncfsa\t\t\tH7225G\t\t\tH9003=ב=in/{H7225G=רֵאשִׁית=: beginning»first:1_beginning}\n" +
	"Gen.1.1#02=L\tבָּרָ֣א\tba.Ra'\the created\t{H1254A}\tHVqp3ms\t\t\tH1254A\t\t\t{H1254A=בָּרָא=to create}\n" +
	"\n" +
	"Eng (Heb) Ref & Type\tHebrew\tTransliteration\tTranslation\tdStrongs\tGrammar\tMeaning Variants\tSpelling Variants\tRoot dStrong+Instance\tAlternative Strongs+Instance\tConjoin word\tExpanded Strong tags\n" +
	"Exo.1.1#01=L\tוְ/אֵ֨לֶּה\tve./'El.leh\tand/ these\tH9002/{H0428}\tHC/Pdxcp\t\t\tH0428\t\t\tH9002=ו=and/{H0428=אֵ֫לֶּה=these}\n" +
	"\n" +
	"Eng (Heb) Ref & Type\tHebrew\tTransliteration\tTranslation\tdStrongs\tGrammar\tMeaning Variants\tSpelling Variants\tRoot dStrong+Instance\tAlternative Strongs+Instance\tConjoin word\tExpanded Strong tags\n" +
	"Isa.44.24#16=Q(K)\t\t[ ]\t[ ]\t\t\tK= mi (מִי) \"who [was]?\" (H4310=HPi)\tL= מֵי ¦ ;\t\t\t\t\n"

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

	inserted, skipped, untagged, err := tahot.Load(db, strings.NewReader(fixtureTAHOT))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// Gen.1.1#01, #02, Isa.44.24#16 = 3 rows; Exo.1.1#01 is not in the mini
	// spine and must be skipped.
	if inserted != 3 {
		t.Errorf("inserted = %d, want 3", inserted)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1 (Exo.1.1 not in mini spine)", skipped)
	}
	if untagged != 1 {
		t.Errorf("untagged = %d, want 1 (Isa.44.24#16 has no braced segment)", untagged)
	}
}

func TestLoadExtractsRootNotPrefix(t *testing.T) {
	db := setup(t)
	if _, _, _, err := tahot.Load(db, strings.NewReader(fixtureTAHOT)); err != nil {
		t.Fatalf("load: %v", err)
	}

	var surface, lemma, dstrong, morphCode string
	err := db.QueryRow(`
		SELECT surface, lemma, dstrong, morph_code FROM words
		WHERE source_locator = 'Gen.1.1#01=L'`).
		Scan(&surface, &lemma, &dstrong, &morphCode)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if surface != "בְּ/רֵאשִׁ֖ית" {
		t.Errorf("surface = %q, want the verbatim prefix/root Hebrew", surface)
	}
	if dstrong != "H7225G" {
		t.Errorf("dstrong = %q, want H7225G (the braced root, not the H9003 prefix)", dstrong)
	}
	if morphCode != "Ncfsa" {
		t.Errorf("morph_code = %q, want Ncfsa (positionally matched to the root)", morphCode)
	}
	if lemma != "רֵאשִׁית" {
		t.Errorf("lemma = %q, want רֵאשִׁית", lemma)
	}
}

func TestLoadHandlesPlainSingleStrongWithBraces(t *testing.T) {
	db := setup(t)
	if _, _, _, err := tahot.Load(db, strings.NewReader(fixtureTAHOT)); err != nil {
		t.Fatalf("load: %v", err)
	}

	var dstrong, lemma string
	err := db.QueryRow(`SELECT dstrong, lemma FROM words WHERE source_locator = 'Gen.1.1#02=L'`).
		Scan(&dstrong, &lemma)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if dstrong != "H1254A" {
		t.Errorf("dstrong = %q, want H1254A", dstrong)
	}
	if lemma != "בָּרָא" {
		t.Errorf("lemma = %q, want בָּרָא", lemma)
	}
}

func TestLoadPreservesKetivQereMarkerVerbatim(t *testing.T) {
	db := setup(t)
	if _, _, _, err := tahot.Load(db, strings.NewReader(fixtureTAHOT)); err != nil {
		t.Fatalf("load: %v", err)
	}

	var attestation string
	var dstrong, lemma sql.NullString
	err := db.QueryRow(`
		SELECT attestation, dstrong, lemma FROM words
		WHERE source_locator = 'Isa.44.24#16=Q(K)'`).
		Scan(&attestation, &dstrong, &lemma)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if attestation != "Q(K)" {
		t.Errorf("attestation = %q, want Q(K) (Ketiv marker not collapsed)", attestation)
	}
	if dstrong.Valid || lemma.Valid {
		t.Errorf("untagged row dstrong/lemma = %v/%v, want both NULL", dstrong, lemma)
	}
}
