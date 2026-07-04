package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/sources"
	"github.com/jrainsberger/orthotomeo/store"
)

// buildFixture writes a real DB file shaped like the spec's own worked
// example: G0859/ἄφεσις at Mat.26.28 (the control case) and Act.2.38, plus
// KJV verse text for the same verse - enough to exercise all four
// subcommands against real data, not mocks.
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

// captureStdout runs fn with os.Stdout redirected to a pipe and returns
// whatever it wrote - the same text a real terminal invocation would see.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	return string(out)
}

func TestConcordFindsControlCaseByDStrong(t *testing.T) {
	dbPath := buildFixture(t)
	var runErr error
	out := captureStdout(t, func() {
		runErr = runConcord([]string{"--corpus", "TAGNT", "--db", dbPath, "G0859"})
	})
	if runErr != nil {
		t.Fatalf("concord: %v", runErr)
	}
	if !strings.Contains(out, "MAT.26.28") {
		t.Errorf("output missing the Matt 26:28 control case: %q", out)
	}
}

func TestConcordJSONMatchesCitationsPayloadShape(t *testing.T) {
	dbPath := buildFixture(t)
	var runErr error
	out := captureStdout(t, func() {
		runErr = runConcord([]string{"--corpus", "TAGNT", "--db", dbPath, "--json", "G0859"})
	})
	if runErr != nil {
		t.Fatalf("concord: %v", runErr)
	}
	var payload citationsPayload
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, out)
	}
	if len(payload.Citations) != 1 || payload.Citations[0].DStrong != "G0859" {
		t.Errorf("citations = %+v, want one G0859 citation", payload.Citations)
	}
}

func TestConcordPhraseAdjacentFindsEisAphesis(t *testing.T) {
	dbPath := buildFixture(t)
	var runErr error
	out := captureStdout(t, func() {
		runErr = runConcord([]string{"--corpus", "TAGNT", "--db", dbPath, "--phrase", "εἰς,ἄφεσις", "--adjacent"})
	})
	if runErr != nil {
		t.Fatalf("concord phrase: %v", runErr)
	}
	if !strings.Contains(out, "MAT.26.28") {
		t.Errorf("phrase output missing MAT.26.28: %q", out)
	}
}

func TestLookupReturnsVerbatimText(t *testing.T) {
	dbPath := buildFixture(t)
	var runErr error
	out := captureStdout(t, func() {
		runErr = runLookup([]string{"--db", dbPath, "--edition", "KJV", "MAT.26.28"})
	})
	if runErr != nil {
		t.Fatalf("lookup: %v", runErr)
	}
	if !strings.Contains(out, "blood of the new testament") {
		t.Errorf("output missing verbatim KJV text: %q", out)
	}
}

// TestLookupAcceptsBookNameVariants covers the reported bug: the USFM code
// (MAT) worked but the full English name in any case (Matthew/MATTHEW/
// matthew) did not, because parseRef only upper-cased the token instead of
// resolving it against the registry.
func TestLookupAcceptsBookNameVariants(t *testing.T) {
	dbPath := buildFixture(t)
	for _, book := range []string{"Matthew", "MATTHEW", "matthew", "mat"} {
		t.Run(book, func(t *testing.T) {
			var runErr error
			out := captureStdout(t, func() {
				runErr = runLookup([]string{"--db", dbPath, "--edition", "KJV", book + ".26.28"})
			})
			if runErr != nil {
				t.Fatalf("lookup: %v", runErr)
			}
			if !strings.Contains(out, "blood of the new testament") {
				t.Errorf("output missing verbatim KJV text: %q", out)
			}
		})
	}
}

func TestParseReturnsMorphologyForOneWord(t *testing.T) {
	dbPath := buildFixture(t)
	var runErr error
	out := captureStdout(t, func() {
		runErr = runParse([]string{"--corpus", "TAGNT", "--db", dbPath, "--word", "2", "MAT.26.28"})
	})
	if runErr != nil {
		t.Fatalf("parse: %v", runErr)
	}
	if !strings.Contains(out, "G0859") {
		t.Errorf("output missing G0859: %q", out)
	}
}

func TestAttestReturnsManuscriptData(t *testing.T) {
	dbPath := buildFixture(t)
	var runErr error
	out := captureStdout(t, func() {
		runErr = runAttest([]string{"--corpus", "TAGNT", "--db", dbPath, "MAT.26.28"})
	})
	if runErr != nil {
		t.Fatalf("attest: %v", runErr)
	}
	if !strings.Contains(out, "NKO") {
		t.Errorf("output missing Type=NKO attestation: %q", out)
	}
}

func TestDefineReturnsGlossAndDefinition(t *testing.T) {
	dbPath := buildFixture(t)
	var runErr error
	out := captureStdout(t, func() {
		runErr = runDefine([]string{"--db", dbPath, "G0859"})
	})
	if runErr != nil {
		t.Fatalf("define: %v", runErr)
	}
	if !strings.Contains(out, "forgiveness") || !strings.Contains(out, "release, pardon") {
		t.Errorf("output missing gloss/definition: %q", out)
	}
}

func TestInterlinearIncludesGlossFromLexiconLookup(t *testing.T) {
	dbPath := buildFixture(t)
	var runErr error
	out := captureStdout(t, func() {
		runErr = runInterlinear([]string{"--corpus", "TAGNT", "--db", dbPath, "MAT.26.28"})
	})
	if runErr != nil {
		t.Fatalf("interlinear: %v", runErr)
	}
	if !strings.Contains(out, "ἄφεσιν") || !strings.Contains(out, "forgiveness") {
		t.Errorf("output missing text/gloss: %q", out)
	}
}

func TestInterlinearJSONMatchesInterlinearPayloadShape(t *testing.T) {
	dbPath := buildFixture(t)
	var runErr error
	out := captureStdout(t, func() {
		runErr = runInterlinear([]string{"--corpus", "TAGNT", "--db", dbPath, "--json", "MAT.26.28"})
	})
	if runErr != nil {
		t.Fatalf("interlinear: %v", runErr)
	}
	var payload interlinearPayload
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, out)
	}
	if len(payload.Words) != 2 {
		t.Fatalf("words = %d, want 2", len(payload.Words))
	}
	if payload.Sources["TAGNT"].File == "" {
		t.Error("Sources[TAGNT].File is empty, want it populated")
	}
}

func TestConcordRejectsMissingCorpus(t *testing.T) {
	dbPath := buildFixture(t)
	if err := runConcord([]string{"--db", dbPath, "G0859"}); err == nil {
		t.Fatal("expected an error when --corpus is omitted")
	}
}

func TestLookupRejectsMalformedRef(t *testing.T) {
	dbPath := buildFixture(t)
	if err := runLookup([]string{"--db", dbPath, "not-a-ref"}); err == nil {
		t.Fatal("expected an error for a malformed ref")
	}
}

func TestOpenEngineFailsOnMissingDB(t *testing.T) {
	if err := runLookup([]string{"--db", filepath.Join(t.TempDir(), "missing.db"), "MAT.26.28"}); err == nil {
		t.Fatal("expected an error opening a nonexistent DB")
	}
}
