package interlinear_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/interlinear"
	"github.com/jrainsberger/orthotomeo/retriever"
	"github.com/jrainsberger/orthotomeo/sources"
	"github.com/jrainsberger/orthotomeo/store"
)

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
	if _, err := db.Exec(`
		INSERT INTO lexicon (dstrong, estrong, ustrong, language, lemma, translit, gloss, definition, def_license)
		VALUES ('G0859', 'G0859', 'G0859', 'grc', 'ἄφεσις', 'aphesis', 'forgiveness', 'release, pardon', 'Abbott-Smith PD')`); err != nil {
		t.Fatalf("seed lexicon: %v", err)
	}
	return db
}

// TestBuildResolvesGlossByDStrong is the direct T35 test: a Citation
// carrying a DStrong that has a real lexicon row must come back with that
// row's Gloss attached, alongside (not replacing) its own Text/Translit.
func TestBuildResolvesGlossByDStrong(t *testing.T) {
	db := setup(t)
	cs := []retriever.Citation{
		{
			Ref: retriever.Ref{Book: "MAT", Chapter: 26, Verse: 28}, Edition: "TAGNT",
			Text: "ἄφεσιν", Translit: "aphesin", Lemma: "ἄφεσις", DStrong: "G0859",
			Grammar: "N-ASF", Confidence: retriever.ConfidenceHigh,
		},
	}
	words, err := interlinear.Build(db, cs)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(words) != 1 {
		t.Fatalf("words = %d, want 1", len(words))
	}
	w := words[0]
	if w.Gloss != "forgiveness" {
		t.Errorf("gloss = %q, want forgiveness", w.Gloss)
	}
	if w.Text != "ἄφεσιν" || w.Translit != "aphesin" {
		t.Errorf("text/translit = %q/%q, want the Citation's own values preserved", w.Text, w.Translit)
	}
	if w.Confidence != retriever.ConfidenceHigh {
		t.Errorf("confidence = %q, want High carried through from the Citation", w.Confidence)
	}
}

// TestBuildLeavesGlossEmptyWhenNoDStrong covers a compound-tagged/untagged
// word (no DStrong at all) - Build must not guess a gloss, just skip it.
func TestBuildLeavesGlossEmptyWhenNoDStrong(t *testing.T) {
	db := setup(t)
	cs := []retriever.Citation{
		{Ref: retriever.Ref{Book: "MAT", Chapter: 4, Verse: 6}, Edition: "TAGNT",
			Text: "μήποτε", Confidence: retriever.ConfidenceHigh},
	}
	words, err := interlinear.Build(db, cs)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if words[0].Gloss != "" {
		t.Errorf("gloss = %q, want empty - no DStrong to look up", words[0].Gloss)
	}
}

// TestBuildLeavesGlossEmptyWhenDStrongUnknownToLexicon covers the
// documented small T14 gap: a DStrong the words table carries but the
// lexicon table doesn't. Must not fail the whole render over one missing
// dictionary row.
func TestBuildLeavesGlossEmptyWhenDStrongUnknownToLexicon(t *testing.T) {
	db := setup(t)
	cs := []retriever.Citation{
		{Ref: retriever.Ref{Book: "MAT", Chapter: 1, Verse: 1}, Edition: "TAGNT",
			Text: "Βίβλος", DStrong: "G9999", Confidence: retriever.ConfidenceHigh},
	}
	words, err := interlinear.Build(db, cs)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if words[0].Gloss != "" {
		t.Errorf("gloss = %q, want empty - G9999 has no lexicon row (a T14 gap, not an error)", words[0].Gloss)
	}
}

// TestBuildPreservesCaveatAndOrder confirms a multi-word result keeps its
// order and each word's own Caveat, not just the gloss-bearing fields.
func TestBuildPreservesCaveatAndOrder(t *testing.T) {
	db := setup(t)
	cs := []retriever.Citation{
		{Ref: retriever.Ref{Book: "MAT", Chapter: 1, Verse: 1}, Edition: "TAGNT", Text: "first"},
		{Ref: retriever.Ref{Book: "MAT", Chapter: 26, Verse: 28}, Edition: "TAGNT", Text: "second",
			DStrong: "G0859", Confidence: retriever.ConfidenceFlagged, Caveat: "a test caveat"},
	}
	words, err := interlinear.Build(db, cs)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(words) != 2 || words[0].Text != "first" || words[1].Text != "second" {
		t.Fatalf("order not preserved: %+v", words)
	}
	if words[1].Caveat != "a test caveat" {
		t.Errorf("caveat = %q, want it carried through", words[1].Caveat)
	}
}

func TestRenderEmptySlice(t *testing.T) {
	if got := interlinear.Render(nil); got != "" {
		t.Errorf("Render(nil) = %q, want empty string", got)
	}
}

func TestRenderOneLinePerWordWithGloss(t *testing.T) {
	words := []interlinear.Word{
		{Ref: retriever.Ref{Book: "MAT", Chapter: 26, Verse: 28}, Edition: "TAGNT",
			Text: "ἄφεσιν", Translit: "aphesin", Gloss: "forgiveness", Grammar: "N-ASF",
			Confidence: retriever.ConfidenceHigh},
	}
	got := interlinear.Render(words)
	want := `- **MAT.26.28** (TAGNT) "ἄφεσιν" [aphesin] — forgiveness (N-ASF)`
	if got != want {
		t.Errorf("Render = %q\nwant   = %q", got, want)
	}
}

func TestRenderShowsCaveatForFlaggedWord(t *testing.T) {
	words := []interlinear.Word{
		{Ref: retriever.Ref{Book: "PSA", Chapter: 9, Verse: 1}, Edition: "Swete",
			Confidence: retriever.ConfidenceFlagged, Caveat: "no Swete alignment"},
	}
	got := interlinear.Render(words)
	if !strings.HasSuffix(got, `*(no Swete alignment)*`) {
		t.Errorf("Render = %q, want a trailing italicized caveat", got)
	}
}
