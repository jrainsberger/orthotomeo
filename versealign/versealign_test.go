package versealign_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/sources"
	"github.com/jrainsberger/orthotomeo/store"
	"github.com/jrainsberger/orthotomeo/versealign"
	"github.com/jrainsberger/orthotomeo/verses"
)

// testSourceCode is registered in sources.json as a real LXX source so
// Align's source-lookup succeeds; the fixture data attached to it is
// synthetic, not the real Brenton corpus.
const testSourceCode = "Brenton"
const testVersification = "lxx-test"

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
	return db
}

// putEditionVerses inserts edition verse rows for bookName under
// testVersification, given a flat list of (chapter, verse) pairs.
func putEditionVerses(t *testing.T, db *sql.DB, bookName string, pairs [][2]int) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()
	bookID, err := books.Resolve(tx, "name-en", bookName)
	if err != nil {
		t.Fatalf("resolve %s: %v", bookName, err)
	}
	for _, p := range pairs {
		if _, err := verses.GetOrCreateVerse(tx, testVersification, bookID, p[0], p[1]); err != nil {
			t.Fatalf("get-or-create %s %d:%d: %v", bookName, p[0], p[1], err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// TestAlignBoundaryShift mirrors the real, verified Joel shape: a clean
// chapter-boundary shift where every verse still has a 1:1 counterpart,
// just possibly renumbered - confirmed against the actual built DB before
// writing this test (Joel ch1-2up-to-27 identical, the remainder shifted
// by one chapter, total counts equal on both sides).
func TestAlignBoundaryShift(t *testing.T) {
	db := setup(t)
	const kjv = `{"books":[{"name":"Joel","chapters":[
		{"chapter":1,"verses":[{"verse":1},{"verse":2},{"verse":3}]},
		{"chapter":2,"verses":[{"verse":1},{"verse":2}]}
	]}]}`
	if _, err := verses.BuildSpine(db, strings.NewReader(kjv)); err != nil {
		t.Fatalf("build spine: %v", err)
	}
	// Edition: ch1 identical (anchors), but canonical 2:1/2:2 are shifted
	// to edition 3:1/3:2 (a clean chapter-boundary renumber, like Joel's
	// real KJV-2:28-32 -> LXX-3:1-5 shift).
	putEditionVerses(t, db, "Joel", [][2]int{{1, 1}, {1, 2}, {1, 3}, {3, 1}, {3, 2}})

	counts, err := versealign.Align(db, testVersification, testSourceCode)
	if err != nil {
		t.Fatalf("align: %v", err)
	}
	if counts.Exact != 3 {
		t.Errorf("Exact = %d, want 3", counts.Exact)
	}
	if counts.Renumber != 2 {
		t.Errorf("Renumber = %d, want 2", counts.Renumber)
	}
	if counts.Merge != 0 || counts.Divide != 0 {
		t.Errorf("Merge=%d Divide=%d, want both 0 (clean 1:1 shift, no count mismatch)", counts.Merge, counts.Divide)
	}
	if counts.UnalignedCanonical != 0 || counts.UnalignedEdition != 0 {
		t.Errorf("UnalignedCanonical=%d UnalignedEdition=%d, want both 0", counts.UnalignedCanonical, counts.UnalignedEdition)
	}

	// The renumbered pair must still resolve to the right canonical/edition
	// verse rows, not just the right count.
	var edCh, edV int
	err = db.QueryRow(`
		SELECT ev.chapter, ev.verse FROM verse_alignment va
		JOIN verses cv ON cv.id = va.canonical_verse_id
		JOIN verses ev ON ev.id = va.edition_verse_id
		JOIN books b ON b.id = cv.book_id
		WHERE b.full_name = 'Joel' AND cv.chapter = 2 AND cv.verse = 1`).Scan(&edCh, &edV)
	if err != nil {
		t.Fatalf("query renumbered pair: %v", err)
	}
	if edCh != 3 || edV != 1 {
		t.Errorf("Joel 2:1 aligned to %d:%d, want 3:1", edCh, edV)
	}
}

// TestAlignInsertionGetsNoRow mirrors Psalm 151: edition-only content (an
// entire extra chapter) with no canonical counterpart at all.
func TestAlignInsertionGetsNoRow(t *testing.T) {
	db := setup(t)
	const kjv = `{"books":[{"name":"Jude","chapters":[{"chapter":1,"verses":[{"verse":1},{"verse":2}]}]}]}`
	if _, err := verses.BuildSpine(db, strings.NewReader(kjv)); err != nil {
		t.Fatalf("build spine: %v", err)
	}
	putEditionVerses(t, db, "Jude", [][2]int{{1, 1}, {1, 2}, {2, 1}})

	counts, err := versealign.Align(db, testVersification, testSourceCode)
	if err != nil {
		t.Fatalf("align: %v", err)
	}
	if counts.Exact != 2 {
		t.Errorf("Exact = %d, want 2", counts.Exact)
	}
	if counts.UnalignedEdition != 1 {
		t.Errorf("UnalignedEdition = %d, want 1 (the extra Jude 2:1 chapter)", counts.UnalignedEdition)
	}

	// The inserted verse must still exist as a verse row...
	var id int64
	err = db.QueryRow(`
		SELECT v.id FROM verses v JOIN books b ON b.id = v.book_id
		WHERE b.full_name = 'Jude' AND v.versification = ? AND v.chapter = 2 AND v.verse = 1`, testVersification).Scan(&id)
	if err != nil {
		t.Fatalf("Jude 2:1 should exist as a verse row: %v", err)
	}
	// ...but have no alignment row at all.
	var alignRows int
	db.QueryRow(`SELECT COUNT(*) FROM verse_alignment WHERE edition_verse_id = ?`, id).Scan(&alignRows)
	if alignRows != 0 {
		t.Errorf("Jude 2:1 has %d alignment rows, want 0", alignRows)
	}
}

// TestAlignDeletionGetsNoRow mirrors the shorter LXX Jeremiah: a canonical
// verse the edition simply omits, WITHOUT claiming label-based precision
// about which specific verse is missing. Verse-level correspondence is
// position/count only (align.FillGap), never verse-number label matching
// - confirmed unsafe even within an established chapter pair (see the
// package doc) - so a 3-vs-2 count mismatch resolves as a merge (lower
// confidence), not a guessed-precise "verse 2 specifically is missing."
func TestAlignDeletionGetsNoRow(t *testing.T) {
	db := setup(t)
	const kjv = `{"books":[{"name":"Jude","chapters":[{"chapter":1,"verses":[{"verse":1},{"verse":2},{"verse":3}]}]}]}`
	if _, err := verses.BuildSpine(db, strings.NewReader(kjv)); err != nil {
		t.Fatalf("build spine: %v", err)
	}
	// Edition omits one verse's worth of content; only 2 of canonical's 3
	// verses have an edition counterpart.
	putEditionVerses(t, db, "Jude", [][2]int{{1, 1}, {1, 3}})

	counts, err := versealign.Align(db, testVersification, testSourceCode)
	if err != nil {
		t.Fatalf("align: %v", err)
	}
	// align.FillGap(3 canonical, 2 edition) distributes the remainder to the
	// first group: group0 = 2 canonical verses merged into edition's first
	// verse (merge, confidence 0.5); group1 = canonical verse 3 <-> edition
	// verse 3 (1:1, labels equal -> exact). Every canonical verse gets a
	// row (in a merge group or an exact pair) - none is silently dropped,
	// and none is claimed exact without grounds.
	if counts.Merge != 1 {
		t.Errorf("Merge = %d, want 1", counts.Merge)
	}
	if counts.Exact != 1 {
		t.Errorf("Exact = %d, want 1", counts.Exact)
	}
	if counts.UnalignedCanonical != 0 {
		t.Errorf("UnalignedCanonical = %d, want 0 (the count mismatch is reported as a merge, not silently dropped)", counts.UnalignedCanonical)
	}

	var mergeRows int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM verse_alignment va
		JOIN verses v ON v.id = va.canonical_verse_id
		JOIN books b ON b.id = v.book_id
		WHERE b.full_name = 'Jude' AND va.relation = 'merge'`).Scan(&mergeRows)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if mergeRows != 2 {
		t.Errorf("merge rows = %d, want 2 (the 2 canonical verses bundled into edition's one verse)", mergeRows)
	}
}

// TestAlignMergeSharesGroupID mirrors a within-chapter merge: several
// canonical verses correspond to one edition verse. Both sides use the
// SAME chapter number (9) - with only one chapter on each side, the
// chapter-level DP has no alternative but to substitute them, and the
// verse counts (3 vs 1) then force a verse-level merge. Edition's verse is
// numbered 99 (not 1-3) so no accidental verse-number coincidence could
// steal one of the three into a false "exact" match either way.
func TestAlignMergeSharesGroupID(t *testing.T) {
	db := setup(t)
	const kjv = `{"books":[{"name":"Jude","chapters":[{"chapter":9,"verses":[{"verse":1},{"verse":2},{"verse":3}]}]}]}`
	if _, err := verses.BuildSpine(db, strings.NewReader(kjv)); err != nil {
		t.Fatalf("build spine: %v", err)
	}
	putEditionVerses(t, db, "Jude", [][2]int{{9, 99}})

	counts, err := versealign.Align(db, testVersification, testSourceCode)
	if err != nil {
		t.Fatalf("align: %v", err)
	}
	if counts.Merge != 1 {
		t.Errorf("Merge = %d, want 1 (one merge group)", counts.Merge)
	}

	rows, err := db.Query(`
		SELECT va.group_id, va.confidence FROM verse_alignment va
		JOIN verses v ON v.id = va.canonical_verse_id
		JOIN books b ON b.id = v.book_id
		WHERE b.full_name = 'Jude' AND va.relation = 'merge'`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	var groupIDs []int64
	var confidence float64
	for rows.Next() {
		var gid int64
		var conf float64
		rows.Scan(&gid, &conf)
		groupIDs = append(groupIDs, gid)
		confidence = conf
	}
	if len(groupIDs) != 3 {
		t.Fatalf("merge produced %d rows, want 3 (one per canonical verse)", len(groupIDs))
	}
	for _, g := range groupIDs[1:] {
		if g != groupIDs[0] {
			t.Errorf("merge rows have different group_ids: %v, want all equal", groupIDs)
		}
	}
	if confidence != 1.0/3.0 {
		t.Errorf("merge confidence = %v, want 1/3 (1 / number of merged canonical verses)", confidence)
	}
}

// TestAlignDivideSharesGroupID is the mirror image of the merge case: one
// canonical verse corresponds to several edition verses, both sides using
// the same chapter number for the same reason (forces a substitute, not an
// insert/delete, at the chapter level).
func TestAlignDivideSharesGroupID(t *testing.T) {
	db := setup(t)
	const kjv = `{"books":[{"name":"Jude","chapters":[{"chapter":9,"verses":[{"verse":99}]}]}]}`
	if _, err := verses.BuildSpine(db, strings.NewReader(kjv)); err != nil {
		t.Fatalf("build spine: %v", err)
	}
	putEditionVerses(t, db, "Jude", [][2]int{{9, 1}, {9, 2}, {9, 3}})

	counts, err := versealign.Align(db, testVersification, testSourceCode)
	if err != nil {
		t.Fatalf("align: %v", err)
	}
	if counts.Divide != 1 {
		t.Errorf("Divide = %d, want 1", counts.Divide)
	}

	var n int
	db.QueryRow(`SELECT COUNT(*) FROM verse_alignment WHERE relation = 'divide'`).Scan(&n)
	if n != 3 {
		t.Errorf("divide produced %d rows, want 3 (one per edition verse)", n)
	}
}

// TestAlignProportionalChapterSplit mirrors the real Psalm 9/10 shape:
// a gap whose canonical side spans two of ITS OWN chapters (4 verses +
// 2 verses) against one continuous edition chapter (7 verses, no
// internal break) - the edition's share must be split between the two
// canonical chapters in proportion to their size (4:2), not pooled into
// one undifferentiated blob. Chapter 50 on the edition side guarantees no
// accidental label-equality anchor.
func TestAlignProportionalChapterSplit(t *testing.T) {
	db := setup(t)
	const kjv = `{"books":[{"name":"Jude","chapters":[
		{"chapter":9,"verses":[{"verse":1},{"verse":2},{"verse":3},{"verse":4}]},
		{"chapter":10,"verses":[{"verse":1},{"verse":2}]}
	]}]}`
	if _, err := verses.BuildSpine(db, strings.NewReader(kjv)); err != nil {
		t.Fatalf("build spine: %v", err)
	}
	putEditionVerses(t, db, "Jude", [][2]int{
		{50, 1}, {50, 2}, {50, 3}, {50, 4}, {50, 5}, {50, 6}, {50, 7},
	})

	if _, err := versealign.Align(db, testVersification, testSourceCode); err != nil {
		t.Fatalf("align: %v", err)
	}

	// ProportionalAllocate(7, [4,2]) = [5,2] (largest-remainder method):
	// chapter 9's 4 verses get a 5-verse edition slice, chapter 10's 2
	// verses get a 2-verse edition slice.
	var maxEdVerseForCh9, minEdVerseForCh10 int
	err := db.QueryRow(`
		SELECT MAX(ev.verse) FROM verse_alignment va
		JOIN verses cv ON cv.id = va.canonical_verse_id
		JOIN verses ev ON ev.id = va.edition_verse_id
		JOIN books b ON b.id = cv.book_id
		WHERE b.full_name = 'Jude' AND cv.chapter = 9`).Scan(&maxEdVerseForCh9)
	if err != nil {
		t.Fatalf("query ch9 max: %v", err)
	}
	err = db.QueryRow(`
		SELECT MIN(ev.verse) FROM verse_alignment va
		JOIN verses cv ON cv.id = va.canonical_verse_id
		JOIN verses ev ON ev.id = va.edition_verse_id
		JOIN books b ON b.id = cv.book_id
		WHERE b.full_name = 'Jude' AND cv.chapter = 10`).Scan(&minEdVerseForCh10)
	if err != nil {
		t.Fatalf("query ch10 min: %v", err)
	}
	if maxEdVerseForCh9 != 5 {
		t.Errorf("chapter 9's highest-aligned edition verse = %d, want 5 (proportional 4/6 share of 7)", maxEdVerseForCh9)
	}
	if minEdVerseForCh10 != 6 {
		t.Errorf("chapter 10's lowest-aligned edition verse = %d, want 6 (the remaining slice)", minEdVerseForCh10)
	}
}

// TestAlignAvoidsLeadingTitleInsertionFalseExact mirrors the real,
// confirmed Brenton Psalms 5/7 case: a same-chapter-number pair with a
// leading title verse, no chapter-level merge involved at all. Because
// verse-level correspondence is position/count only (align.FillGap), never
// verse-number label matching, this never gets the chance to coincidentally
// "anchor" canonical verse N to edition verse N for most of the chapter
// (which is what a label-trusting design would do, since edition's range
// becomes a superset of canonical's after the +1 shift) - it is reported
// honestly as a count mismatch (merge/divide, reduced confidence) instead.
// Verified against the real corpus: Brenton Psalms 5 (12->13) and 7
// (17->18) both reproduce this shape; the title itself (edition verse 1)
// has no canonical counterpart, and everything after is shifted by one.
func TestAlignAvoidsLeadingTitleInsertionFalseExact(t *testing.T) {
	db := setup(t)
	const kjv = `{"books":[{"name":"Jude","chapters":[{"chapter":5,"verses":[
		{"verse":1},{"verse":2},{"verse":3},{"verse":4},{"verse":5}
	]}]}]}`
	if _, err := verses.BuildSpine(db, strings.NewReader(kjv)); err != nil {
		t.Fatalf("build spine: %v", err)
	}
	// Edition chapter 5 has 6 verses: a leading title (1) plus 5 verses that
	// would coincidentally number-match canonical's 1-5 if labels were
	// trusted to find the correspondence.
	putEditionVerses(t, db, "Jude", [][2]int{{5, 1}, {5, 2}, {5, 3}, {5, 4}, {5, 5}, {5, 6}})

	counts, err := versealign.Align(db, testVersification, testSourceCode)
	if err != nil {
		t.Fatalf("align: %v", err)
	}
	if counts.Exact != 0 {
		t.Errorf("Exact = %d, want 0 (position/count alignment of a 5-vs-6 mismatch never produces a 1:1 exact pair)", counts.Exact)
	}

	// The whole chapter must become one merge/divide group (verse-count
	// based, not a guessed-confident exact-label chain).
	var rows int
	db.QueryRow(`SELECT COUNT(*) FROM verse_alignment WHERE relation IN ('merge','divide')`).Scan(&rows)
	if rows == 0 {
		t.Error("want at least one merge/divide row for the size-mismatched chapter, got none")
	}
}

// TestAlignCleanRenumberKeepsFullConfidence pins the "no false alarm" side
// of the weakest-link cap: a renumber born from a clean, equal-size 1:1
// chapter substitution (the Joel boundary shift) has opConfidence 1.0, so
// its confidence is NOT capped - it stays at the renumber ceiling (0.85).
// This is the case the cap must leave alone.
func TestAlignCleanRenumberKeepsFullConfidence(t *testing.T) {
	db := setup(t)
	const kjv = `{"books":[{"name":"Joel","chapters":[
		{"chapter":1,"verses":[{"verse":1},{"verse":2},{"verse":3}]},
		{"chapter":2,"verses":[{"verse":1},{"verse":2}]}
	]}]}`
	if _, err := verses.BuildSpine(db, strings.NewReader(kjv)); err != nil {
		t.Fatalf("build spine: %v", err)
	}
	putEditionVerses(t, db, "Joel", [][2]int{{1, 1}, {1, 2}, {1, 3}, {3, 1}, {3, 2}})

	if _, err := versealign.Align(db, testVersification, testSourceCode); err != nil {
		t.Fatalf("align: %v", err)
	}

	rows, err := db.Query(`
		SELECT va.confidence FROM verse_alignment va
		JOIN verses cv ON cv.id = va.canonical_verse_id
		JOIN books b ON b.id = cv.book_id
		WHERE b.full_name = 'Joel' AND va.relation = 'renumber'`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	var n int
	for rows.Next() {
		var conf float64
		if err := rows.Scan(&conf); err != nil {
			t.Fatalf("scan: %v", err)
		}
		n++
		if conf != 0.85 {
			t.Errorf("clean-shift renumber confidence = %v, want 0.85 (equal-size chapter substitution, nothing to cap)", conf)
		}
	}
	if n != 2 {
		t.Fatalf("got %d renumber rows, want 2", n)
	}
}

// TestAlignRenumberInMismatchedRegionIsCapped pins the "catch the real
// problem" side: a renumber whose position was allocated inside a size-
// mismatched chapter-level merge (the shape of Jeremiah's displaced block)
// must NOT keep the full 0.85 ceiling - it is capped by the merge's
// size-agreement.
//
// Canonical Jude 9 (2 verses, labelled 5/6 so nothing coincidentally
// anchors) and Jude 10 (2 verses) merge into edition Jude 1 (5 verses):
// AlignWeighted picks the 2:1 chapter merge (cost 1) over any 1:1 threading.
// sizeReliability(4, 5) = 1 - 1/5 = 0.8, so every leaf under this merge -
// including the 1:1 renumber pairings the proportional split leaves behind -
// is capped at 0.8, below the 0.85 ceiling. The count-derived cap is exactly
// the honesty fix: the pairing is real and flagged, but no longer claims the
// certainty of a clean relabel.
func TestAlignRenumberInMismatchedRegionIsCapped(t *testing.T) {
	db := setup(t)
	const kjv = `{"books":[{"name":"Jude","chapters":[
		{"chapter":9,"verses":[{"verse":5},{"verse":6}]},
		{"chapter":10,"verses":[{"verse":1},{"verse":2}]}
	]}]}`
	if _, err := verses.BuildSpine(db, strings.NewReader(kjv)); err != nil {
		t.Fatalf("build spine: %v", err)
	}
	putEditionVerses(t, db, "Jude", [][2]int{{1, 1}, {1, 2}, {1, 3}, {1, 4}, {1, 5}})

	if _, err := versealign.Align(db, testVersification, testSourceCode); err != nil {
		t.Fatalf("align: %v", err)
	}

	rows, err := db.Query(`
		SELECT va.confidence FROM verse_alignment va
		JOIN verses cv ON cv.id = va.canonical_verse_id
		JOIN books b ON b.id = cv.book_id
		WHERE b.full_name = 'Jude' AND va.relation = 'renumber'`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	var n int
	for rows.Next() {
		var conf float64
		if err := rows.Scan(&conf); err != nil {
			t.Fatalf("scan: %v", err)
		}
		n++
		if conf >= 0.85 {
			t.Errorf("mismatched-region renumber confidence = %v, want < 0.85 (capped by the merge's size-agreement)", conf)
		}
		if conf != 0.8 {
			t.Errorf("renumber confidence = %v, want 0.8 (sizeReliability(4,5))", conf)
		}
	}
	if n == 0 {
		t.Fatal("expected at least one renumber row in the mismatched merge region, got none")
	}
}

func TestAlignIsDeterministic(t *testing.T) {
	db1 := setup(t)
	db2 := setup(t)
	const kjv = `{"books":[{"name":"Joel","chapters":[
		{"chapter":1,"verses":[{"verse":1},{"verse":2},{"verse":3}]},
		{"chapter":2,"verses":[{"verse":1},{"verse":2}]}
	]}]}`
	for _, db := range []*sql.DB{db1, db2} {
		if _, err := verses.BuildSpine(db, strings.NewReader(kjv)); err != nil {
			t.Fatalf("build spine: %v", err)
		}
		putEditionVerses(t, db, "Joel", [][2]int{{1, 1}, {1, 2}, {1, 3}, {3, 1}, {3, 2}})
	}

	c1, err := versealign.Align(db1, testVersification, testSourceCode)
	if err != nil {
		t.Fatalf("align db1: %v", err)
	}
	c2, err := versealign.Align(db2, testVersification, testSourceCode)
	if err != nil {
		t.Fatalf("align db2: %v", err)
	}
	if c1 != c2 {
		t.Errorf("two runs over identical input produced different counts: %+v != %+v", c1, c2)
	}
}
