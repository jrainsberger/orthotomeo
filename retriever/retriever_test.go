package retriever_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/retriever"
	"github.com/jrainsberger/orthotomeo/sources"
	"github.com/jrainsberger/orthotomeo/store"
)

// setup seeds the real sources/books registries, then hand-builds a small,
// fully controlled fixture: a canonical Genesis verse present everywhere, a
// canonical Psalm verse that WEB is missing (mirrors T8's real skipped-verse
// case), and a canonical Psalm verse whose Brenton counterpart is a T4b
// "renumber" (mirrors the real Ps9/10 Hebrew/LXX divergence) rather than an
// "exact" match - the two shapes GetVerse/ResolveRef must never paper over.
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

	genBook := bookID(t, db, "GEN")
	psaBook := bookID(t, db, "PSA")

	// Gen.1.1 - present in every edition, all exact.
	genVerse := insertVerse(t, db, "canonical", genBook, 1, 1)
	insertVerseText(t, db, genVerse, "KJV", "Gen.1.1", "In the beginning...")
	insertVerseText(t, db, genVerse, "ASV", "Gen.1.1", "In the beginning...")
	insertVerseText(t, db, genVerse, "WEB", "Gen.1.1", "In the beginning...")
	insertWord(t, db, genVerse, "TAGNT", 1)
	insertWord(t, db, genVerse, "TAHOT", 1)
	brentonGen := insertVerse(t, db, "lxx-brenton", genBook, 1, 1)
	insertVerseText(t, db, brentonGen, "Brenton", "1:1", "In the beginning...")
	insertAlignment(t, db, genVerse, brentonGen, "Brenton", "exact", 1.0)
	sweteGen := insertVerse(t, db, "lxx-swete", genBook, 1, 1)
	insertWord(t, db, sweteGen, "Swete", 1)
	insertAlignment(t, db, genVerse, sweteGen, "Swete", "exact", 1.0)
	ossGen := insertVerse(t, db, "lxx-oss", genBook, 1, 1)
	insertWord(t, db, ossGen, "OSS-LXX-lemma", 1)
	insertAlignment(t, db, genVerse, ossGen, "OSS-LXX-lemma", "exact", 1.0)

	// Ps.2.1 - present in KJV/ASV/Brenton, but NOT WEB (mirrors T8's real
	// skipped-verse case: this edition's own load simply has no row here).
	psa2 := insertVerse(t, db, "canonical", psaBook, 2, 1)
	insertVerseText(t, db, psa2, "KJV", "Ps.2.1", "Why do the heathen rage...")
	insertVerseText(t, db, psa2, "ASV", "Ps.2.1", "Why do the nations rage...")
	brentonPsa2 := insertVerse(t, db, "lxx-brenton", psaBook, 2, 1)
	insertVerseText(t, db, brentonPsa2, "Brenton", "2:1", "Why did the heathen rage...")
	insertAlignment(t, db, psa2, brentonPsa2, "Brenton", "exact", 1.0)

	// Ps.9.1 - canonical Hebrew Ps9:1 aligns to a Brenton chapter 9 verse via
	// "renumber" (confidence 0.85), NOT "exact" - the real Ps9/10 merge
	// shape T4b produces. GetVerse/ResolveRef must surface this, never
	// silently treat it as a 1:1 match.
	psa9 := insertVerse(t, db, "canonical", psaBook, 9, 1)
	insertVerseText(t, db, psa9, "KJV", "Ps.9.1", "I will praise thee, O LORD...")
	brentonPsa9 := insertVerse(t, db, "lxx-brenton", psaBook, 9, 2)
	insertVerseText(t, db, brentonPsa9, "Brenton", "9:2", "I will give thee thanks, O Lord...")
	insertAlignment(t, db, psa9, brentonPsa9, "Brenton", "renumber", 0.85)

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

func insertVerseText(t *testing.T, db *sql.DB, verseID int64, sourceCode, nativeRef, text string) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO verse_text (verse_id, source_id, native_ref, text)
		VALUES (?, (SELECT id FROM sources WHERE code = ?), ?, ?)`,
		verseID, sourceCode, nativeRef, text); err != nil {
		t.Fatalf("insert verse_text %s: %v", sourceCode, err)
	}
}

var wordSeq int

func insertWord(t *testing.T, db *sql.DB, verseID int64, sourceCode string, wordNo int) {
	t.Helper()
	wordSeq++
	if _, err := db.Exec(`
		INSERT INTO words (verse_id, source_id, word_no, surface, lemma, dstrong, morph_code, attestation, editions, source_locator)
		VALUES (?, (SELECT id FROM sources WHERE code = ?), ?, 'w', 'w', NULL, NULL, 'N', 'NA28', ?)`,
		verseID, sourceCode, wordNo, "loc.1.1#0"+string(rune('0'+wordSeq%9+1))); err != nil {
		t.Fatalf("insert word %s: %v", sourceCode, err)
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

func TestResolveRefAllEditionsPresentNoCaveats(t *testing.T) {
	db := setup(t)
	res, err := retriever.ResolveRef(db, retriever.Ref{Book: "GEN", Chapter: 1, Verse: 1})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(res.Addresses) != 8 {
		t.Fatalf("addresses = %d, want 8 (one per edition)", len(res.Addresses))
	}
	for _, a := range res.Addresses {
		if !a.Exists {
			t.Errorf("edition %s: Exists=false, want true", a.Edition)
		}
	}
	if len(res.Caveats) != 0 {
		t.Errorf("caveats = %v, want none", res.Caveats)
	}
}

func TestResolveRefMissingEditionIsAddressedNotSilent(t *testing.T) {
	db := setup(t)
	res, err := retriever.ResolveRef(db, retriever.Ref{Book: "PSA", Chapter: 2, Verse: 1})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	var web retriever.Address
	found := false
	for _, a := range res.Addresses {
		if a.Edition == "WEB" {
			web, found = a, true
		}
	}
	if !found {
		t.Fatal("WEB address missing from Addresses entirely - the whole point is it must still appear")
	}
	if web.Exists {
		t.Error("WEB.Exists = true, want false (no row was inserted)")
	}
	if !containsSubstring(res.Caveats, "WEB") {
		t.Errorf("caveats %v do not mention the missing WEB row", res.Caveats)
	}
}

func TestResolveRefDivergentAlignmentIsCaveated(t *testing.T) {
	db := setup(t)
	res, err := retriever.ResolveRef(db, retriever.Ref{Book: "PSA", Chapter: 9, Verse: 1})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !containsSubstring(res.Caveats, "renumber") {
		t.Errorf("caveats %v do not mention the renumber relation", res.Caveats)
	}
}

func TestResolveRefUnknownRef(t *testing.T) {
	db := setup(t)
	res, err := retriever.ResolveRef(db, retriever.Ref{Book: "GEN", Chapter: 999, Verse: 999})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(res.Addresses) != 0 {
		t.Errorf("addresses = %v, want none for a ref that isn't on the canonical spine", res.Addresses)
	}
	if len(res.Caveats) != 1 || !strings.Contains(res.Caveats[0], "does not exist") {
		t.Errorf("caveats = %v, want a single 'does not exist' caveat", res.Caveats)
	}
}

func TestGetVerseReturnsVerbatimTextWithProvenance(t *testing.T) {
	db := setup(t)
	cs, err := retriever.GetVerse(db, retriever.Ref{Book: "GEN", Chapter: 1, Verse: 1}, []string{"KJV"})
	if err != nil {
		t.Fatalf("get verse: %v", err)
	}
	if len(cs) != 1 {
		t.Fatalf("citations = %d, want 1", len(cs))
	}
	c := cs[0]
	if c.Text != "In the beginning..." {
		t.Errorf("text = %q", c.Text)
	}
	if c.Locator != "Gen.1.1" {
		t.Errorf("locator = %q, want Gen.1.1", c.Locator)
	}
	srcs, err := retriever.SourcesFor(cs)
	if err != nil {
		t.Fatalf("SourcesFor: %v", err)
	}
	if srcs["KJV"].File == "" {
		t.Errorf("SourcesFor[%q].File is empty, want the KJV source file", "KJV")
	}
	if c.Confidence != retriever.ConfidenceHigh {
		t.Errorf("confidence = %q, want High", c.Confidence)
	}
	if c.Caveat != "" {
		t.Errorf("caveat = %q, want none for an exact match", c.Caveat)
	}
}

func TestGetVerseSurfacesDivergenceNotSilentShift(t *testing.T) {
	db := setup(t)
	cs, err := retriever.GetVerse(db, retriever.Ref{Book: "PSA", Chapter: 9, Verse: 1}, []string{"Brenton"})
	if err != nil {
		t.Fatalf("get verse: %v", err)
	}
	if len(cs) != 1 {
		t.Fatalf("citations = %d, want 1", len(cs))
	}
	c := cs[0]
	if c.Text == "" {
		t.Error("text is empty even though a renumbered Brenton row exists")
	}
	if c.Locator != "9:2" {
		t.Errorf("locator = %q, want the Brenton edition's own 9:2 (not a silently reused canonical 9:1)", c.Locator)
	}
	if c.Confidence != retriever.ConfidenceFlagged {
		t.Errorf("confidence = %q, want Flagged for a non-exact alignment", c.Confidence)
	}
	if c.Caveat == "" {
		t.Error("caveat is empty for a renumbered verse")
	}
}

func TestGetVerseMissingEditionRowStillAppears(t *testing.T) {
	db := setup(t)
	cs, err := retriever.GetVerse(db, retriever.Ref{Book: "PSA", Chapter: 2, Verse: 1}, []string{"WEB"})
	if err != nil {
		t.Fatalf("get verse: %v", err)
	}
	if len(cs) != 1 {
		t.Fatalf("citations = %d, want 1 (the absence itself is the row)", len(cs))
	}
	c := cs[0]
	if c.Text != "" {
		t.Errorf("text = %q, want empty", c.Text)
	}
	if c.Confidence != retriever.ConfidenceFlagged || c.Caveat == "" {
		t.Errorf("missing-row citation not flagged: confidence=%q caveat=%q", c.Confidence, c.Caveat)
	}
}

// TestGetVerseRecensionDivergentBookGivesRecensionCaveat covers the T2 query
// side: a canonical Jeremiah verse whose LXX alignment was suppressed (the
// reordered span) must be flagged with a caveat that names the recension
// divergence and does NOT fall back to the generic "canonical-only or gap"
// wording - which would wrongly imply the material is simply absent from the
// LXX when it is present under a different structure.
func TestGetVerseRecensionDivergentBookGivesRecensionCaveat(t *testing.T) {
	db := setup(t)
	jerBook := bookID(t, db, "JER")
	// Canonical Jer 33:15 exists; T2 suppressed its Brenton alignment (no
	// verse_alignment row), so the reordered material asserts no correspondence.
	jer := insertVerse(t, db, "canonical", jerBook, 33, 15)
	insertVerseText(t, db, jer, "KJV", "Jer.33.15", "In those days...")

	cs, err := retriever.GetVerse(db, retriever.Ref{Book: "JER", Chapter: 33, Verse: 15}, []string{"Brenton"})
	if err != nil {
		t.Fatalf("get verse: %v", err)
	}
	if len(cs) != 1 {
		t.Fatalf("citations = %d, want 1", len(cs))
	}
	c := cs[0]
	if c.Confidence != retriever.ConfidenceFlagged {
		t.Errorf("confidence = %v, want Flagged", c.Confidence)
	}
	if !strings.Contains(c.Caveat, "recension") {
		t.Errorf("caveat = %q, want it to name the recension divergence", c.Caveat)
	}
	if strings.Contains(c.Caveat, "canonical-only content") {
		t.Errorf("recension caveat must not fall back to the generic gap wording: %q", c.Caveat)
	}
}

// TestGetVerseNonDivergentGapKeepsGenericCaveat is the negative control: a
// canonical verse with no alignment in a book that is NOT recension-divergent
// keeps the generic gap wording, proving the recension caveat is scoped to
// declared books, not applied to every gap.
func TestGetVerseNonDivergentGapKeepsGenericCaveat(t *testing.T) {
	db := setup(t)
	psaBook := bookID(t, db, "PSA")
	// A canonical Psalm verse with no Brenton alignment row - an ordinary gap.
	psa := insertVerse(t, db, "canonical", psaBook, 42, 1)
	insertVerseText(t, db, psa, "KJV", "Ps.42.1", "As the hart panteth...")

	cs, err := retriever.GetVerse(db, retriever.Ref{Book: "PSA", Chapter: 42, Verse: 1}, []string{"Brenton"})
	if err != nil {
		t.Fatalf("get verse: %v", err)
	}
	if len(cs) != 1 {
		t.Fatalf("citations = %d, want 1", len(cs))
	}
	if strings.Contains(cs[0].Caveat, "recension") {
		t.Errorf("non-divergent gap should not get a recension caveat: %q", cs[0].Caveat)
	}
	if !strings.Contains(cs[0].Caveat, "canonical-only content") {
		t.Errorf("non-divergent gap should keep the generic wording: %q", cs[0].Caveat)
	}
}

func TestGetVerseRejectsWordOnlyEdition(t *testing.T) {
	db := setup(t)
	_, err := retriever.GetVerse(db, retriever.Ref{Book: "GEN", Chapter: 1, Verse: 1}, []string{"TAGNT"})
	if err == nil {
		t.Fatal("expected an error requesting GetVerse text for a word-tagged, non-prose edition")
	}
}

func TestGetPassagePreservesVerseBoundaries(t *testing.T) {
	db := setup(t)
	// Ps2:1 and Ps9:1 in the same book (PSA), requesting KJV only - two
	// verses, two separate Citations, never concatenated into one blob.
	rr := retriever.RefRange{Start: retriever.Ref{Book: "PSA", Chapter: 2, Verse: 1}, End: retriever.Ref{Book: "PSA", Chapter: 9, Verse: 1}}
	cs, err := retriever.GetPassage(db, rr, []string{"KJV"})
	if err != nil {
		t.Fatalf("get passage: %v", err)
	}
	if len(cs) != 2 {
		t.Fatalf("citations = %d, want 2 (one per verse in range)", len(cs))
	}
	if cs[0].Ref.Chapter != 2 || cs[1].Ref.Chapter != 9 {
		t.Errorf("verse order = %v, %v; want chapter 2 then chapter 9", cs[0].Ref, cs[1].Ref)
	}
}

func TestGetPassageRejectsCrossBookRange(t *testing.T) {
	db := setup(t)
	rr := retriever.RefRange{Start: retriever.Ref{Book: "GEN", Chapter: 1, Verse: 1}, End: retriever.Ref{Book: "PSA", Chapter: 2, Verse: 1}}
	if _, err := retriever.GetPassage(db, rr, []string{"KJV"}); err == nil {
		t.Fatal("expected an error for a cross-book range")
	}
}

func containsSubstring(list []string, substr string) bool {
	for _, s := range list {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}

// TestSourcesForDedupsByEdition is the direct test for T31's dedup
// mechanism: many Citations from the same edition collapse to exactly one
// "sources" entry, keyed by Edition - not one per Citation the way a
// per-row source file used to be.
func TestSourcesForDedupsByEdition(t *testing.T) {
	cs := []retriever.Citation{
		{Edition: "TAHOT", Locator: "Lev.17.11#01=L"},
		{Edition: "TAHOT", Locator: "Lev.17.11#02=L"},
		{Edition: "TAHOT", Locator: "Lev.17.11#03=L"},
	}
	srcs, err := retriever.SourcesFor(cs)
	if err != nil {
		t.Fatalf("SourcesFor: %v", err)
	}
	if len(srcs) != 1 {
		t.Fatalf("sources = %d, want exactly 1 (all three Citations share one edition)", len(srcs))
	}
	info, ok := srcs["TAHOT"]
	if !ok {
		t.Fatal(`sources["TAHOT"] missing`)
	}
	if info.File == "" {
		t.Error("File is empty, want the real TAHOT source_file from sources.json")
	}
	if info.License != "CC BY 4.0" {
		t.Errorf("License = %q, want %q (matches sources.json)", info.License, "CC BY 4.0")
	}
	if info.Attribution == "" {
		t.Error("Attribution is empty, want STEPBible.org's attribution string")
	}
}

// TestSourcesForCoversMultipleDistinctEditions confirms the map grows by
// distinct edition, not by Citation count - a get_verse-style call over
// KJV+ASV+WEB gets exactly three entries regardless of how many verses.
func TestSourcesForCoversMultipleDistinctEditions(t *testing.T) {
	cs := []retriever.Citation{
		{Edition: "KJV"}, {Edition: "ASV"}, {Edition: "WEB"},
		{Edition: "KJV"}, {Edition: "ASV"}, {Edition: "WEB"}, // second verse, same editions
	}
	srcs, err := retriever.SourcesFor(cs)
	if err != nil {
		t.Fatalf("SourcesFor: %v", err)
	}
	if len(srcs) != 3 {
		t.Fatalf("sources = %d, want exactly 3 (KJV, ASV, WEB), got %v", len(srcs), srcs)
	}
}

// TestSourcesForSkipsEmptyEditionPlaceholders covers a Citation with no
// Edition at all (a "nothing here" placeholder some paths return) - it
// must be skipped, not treated as an unknown-edition error.
func TestSourcesForSkipsEmptyEditionPlaceholders(t *testing.T) {
	cs := []retriever.Citation{{Edition: "KJV"}, {Edition: ""}}
	srcs, err := retriever.SourcesFor(cs)
	if err != nil {
		t.Fatalf("SourcesFor: %v", err)
	}
	if len(srcs) != 1 {
		t.Fatalf("sources = %d, want exactly 1 (the empty-edition placeholder must not produce an entry)", len(srcs))
	}
}

// TestSourcesForErrorsOnUnknownEdition: a real (non-empty) edition code
// with no sources.json entry means the corpus and the provenance registry
// have drifted - that must raise, never be silently dropped.
func TestSourcesForErrorsOnUnknownEdition(t *testing.T) {
	cs := []retriever.Citation{{Edition: "NOT-A-REAL-EDITION"}}
	if _, err := retriever.SourcesFor(cs); err == nil {
		t.Fatal("expected an error for an edition with no sources.json entry")
	}
}
