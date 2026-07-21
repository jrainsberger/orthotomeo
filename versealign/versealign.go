// Package versealign populates verse_alignment: a deterministic, typed
// correspondence between an edition's own verse rows (eg lxx-brenton) and
// the canonical KJV-based verse rows, computed by sequence alignment over
// the parsed verse structures already in the DB - never a TVTMS rule
// engine, never hand-curated, never an LLM (invariant #9). Ticket 4b; see
// docs/PLAN.md's T4 "DECISION" block for the full design rationale and the
// "T4b redesign" note for why this is a two-level algorithm, not the
// original single-pass verse-label design.
//
// Algorithm, per book:
//
//  1. CHAPTER level first. Treat each book's own chapters as a sequence of
//     verse-count "weights" and run align.AlignWeighted (a generalized
//     edit-distance DP: substitute/insert/delete/2:1-merge/1:2-divide,
//     cost = size mismatch). This is the safe anchor signal - chapter
//     SIZE rarely coincides between unrelated chapters, unlike raw
//     (chapter,verse) labels.
//
//     Why not verse-level labels first (the original design): identical
//     (chapter,verse) labels are NOT reliable once any chapter has been
//     renumbered, split, or merged, because both sides' verse numbering
//     restarts/recounts independently and small numbers trivially
//     coincide. Confirmed wrong on real data twice: Brenton's lxx-brenton
//     "10:1" raw-label-matched canonical Psalms 10:1, but is actually
//     Psalm 11:1's content (LXX merges Hebrew Ps9+10 into Greek Ps9,
//     shifting everything after by one chapter); and Joel's canonical 3:1
//     raw-label-matched edition 3:1, but edition's chapter 3 is actually
//     the tail of canonical chapter 2 (Joel splits differently), so
//     canonical 3:1 is unrelated content. Both were confidently labeled
//     "exact" by the old design. Chapter-size DP gets both right: it finds
//     canonical Ps9(20)+Ps10(18) merging into edition's Ps9(39) at cost 1
//     (vs cost 30 for two independent substitutions) and canonical
//     chapter 2(32) dividing into edition chapters 2(27)+3(5) at cost 0
//     (an exact size match) - both verified against the real corpus.
//
//  2. VERSE level, within each chapter-level correspondence established by
//     step 1 - position/count only, via align.FillGap, never verse-number
//     label matching. A 1:1 chapter substitution pairs its two verse
//     ranges directly (align.FillGap: equal counts pair 1:1 in order;
//     unequal counts produce a merge/divide with reduced confidence); a
//     2:1 merge or 1:2 divide proportionally splits the larger side
//     across the smaller's chapters by verse count (align.ProportionalAllocate,
//     the largest-remainder method) and fills each resulting leaf the same
//     way; a chapter-level insert/delete (eg Psalm 151, an entire chapter
//     the other side doesn't have) makes every one of its verses a pure
//     insertion/deletion - no row.
//
//     Label equality is NOT used to find verse-level correspondence, even
//     within an already-established chapter pair - confirmed unsafe at
//     both granularities on real data: Brenton Psalms 5 and 7 (a leading
//     title verse, numbered as verse 1, shifts everything after it within
//     the SAME chapter number - no chapter-level merge involved at all)
//     and Exodus 7/8 (content genuinely crosses the chapter boundary, but
//     the chapter-level DP still chose to substitute 7<->7 and 8<->8 since
//     that was cheaper than a merge/divide). A chapter where sizes happen
//     to match exactly still produces "exact" rows here - not because
//     labels were checked first, but because align.FillGap's equal-count
//     branch pairs position-for-position, and when sizes truly match that
//     pairing's labels are correct by construction.
//
// Known limitation (deferred, not fixed here): a within-chapter insertion
// whose position can't be derived from counts alone (the leading-title
// case above) becomes a low-confidence (0.5) merge/divide on one verse
// rather than "added content, no row" - the rest of the chapter still
// renumbers correctly. This is genuinely underdetermined by verse counts;
// nothing in the counts says the extra verse is a leading title vs a
// trailing addition. The 0.5 confidence already reports this honestly; see
// docs/PLAN.md's T4b "as-built" notes for the future fix (consuming
// TVTMS's title/renumber rows as a deterministic placement authority - its
// Tests are booleans over verse counts already parsed, not a rule engine
// to build).
//
// No TVTMS Tests are evaluated and no TVTMS data is read at all to derive
// the mapping: invariant #9 means deriving this mechanically from the
// verse structure already parsed, not building a second engine to
// reconstruct what TVTMS's Tests encode. Divergence (LXX genuinely
// differing from the Hebrew/KJV spine) is the expected, useful output of
// this aligner, per the Concord spec - the goal is to surface it
// accurately as renumber/merge/divide, never to manufacture agreement.
package versealign

import (
	"database/sql"
	"fmt"

	"github.com/jrainsberger/orthotomeo/align"
	"github.com/jrainsberger/orthotomeo/recension"
)

// Relation values for verse_alignment.relation.
const (
	RelationExact    = "exact"
	RelationRenumber = "renumber"
	RelationMerge    = "merge"
	RelationDivide   = "divide"
)

// Confidence ceiling for a clean positional pairing that wasn't confirmed
// by an identical (chapter,verse) label - lower than an anchor's certainty,
// since it rests on count-based position alone. It is a ceiling, not a flat
// value: classify caps it (and every other relation's confidence) by the
// opConfidence of the chapter-level operation that produced the pairing (the
// weakest-link rule; see producedGroup and classify).
const renumberConfidence = 0.85

type verseRow struct {
	id             int64
	chapter, verse int
}

// chapterSpan is one chapter's contiguous range [start,end) into an ordered
// verseRow slice.
type chapterSpan struct {
	start, end int
}

// Counts summarizes one Align run for a build-time report.
type Counts struct {
	Exact, Renumber, Merge, Divide int
	UnalignedCanonical             int // canonical verses with no row in this edition (eg shorter LXX Jeremiah)
	UnalignedEdition               int // edition verses with no row (eg Psalm 151, Esther/Daniel's Greek additions)
	RecensionSuppressed            int // canonical verses in a recension-divergent book's reordered span, deliberately left unaligned (eg Jeremiah 25-51 vs the LXX)
}

// Align computes and inserts verse_alignment rows between the canonical
// verse rows and one edition's own verse rows, for every book the edition
// has loaded (one book at a time, independently). Deterministic: identical
// inputs produce byte-identical output every run - processing order is
// fixed by SQL ORDER BY, never Go map iteration. Runs in one transaction
// so a partial alignment never lands. group_id values are only meaningful
// in combination with source_id (each call renumbers its own groups from 1;
// two editions' "group 5" are unrelated rows).
func Align(db *sql.DB, versification, sourceCode string) (Counts, error) {
	var sourceID int64
	if err := db.QueryRow(`SELECT id FROM sources WHERE code = ?`, sourceCode).Scan(&sourceID); err != nil {
		return Counts{}, fmt.Errorf("source %q not seeded: %w", sourceCode, err)
	}

	bookIDs, err := queryBookIDs(db, versification)
	if err != nil {
		return Counts{}, err
	}

	tx, err := db.Begin()
	if err != nil {
		return Counts{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO verse_alignment (canonical_verse_id, edition_verse_id, relation, group_id, confidence, source_id)
		VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return Counts{}, fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	var counts Counts
	var groupID int64
	for _, bookID := range bookIDs {
		canonical, err := queryBookVerses(tx, "canonical", bookID)
		if err != nil {
			return Counts{}, err
		}
		edition, err := queryBookVerses(tx, versification, bookID)
		if err != nil {
			return Counts{}, err
		}

		bookCode, err := bookUSFMCode(tx, bookID)
		if err != nil {
			return Counts{}, err
		}
		suppressDivergent := recension.IsDivergent(bookCode, sourceCode)

		groups, suppressed := alignBook(canonical, edition, suppressDivergent)
		counts.RecensionSuppressed += suppressed
		for _, pg := range groups {
			g := pg.g
			relation, confidence := classify(canonical, edition, pg)
			switch {
			case relation == "":
				if len(g.AIdx) == 0 {
					counts.UnalignedEdition++
				} else {
					counts.UnalignedCanonical++
				}
				continue
			case len(g.AIdx) == 1 && len(g.BIdx) == 1:
				if err := insertPair(stmt, canonical[g.AIdx[0]].id, edition[g.BIdx[0]].id, relation, nil, confidence, sourceID); err != nil {
					return Counts{}, err
				}
			default:
				groupID++
				gid := groupID
				for _, ai := range g.AIdx {
					for _, bi := range g.BIdx {
						if err := insertPair(stmt, canonical[ai].id, edition[bi].id, relation, &gid, confidence, sourceID); err != nil {
							return Counts{}, err
						}
					}
				}
			}
			switch relation {
			case RelationExact:
				counts.Exact++
			case RelationRenumber:
				counts.Renumber++
			case RelationMerge:
				counts.Merge++
			case RelationDivide:
				counts.Divide++
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return Counts{}, fmt.Errorf("commit: %w", err)
	}
	return counts, nil
}

func insertPair(stmt *sql.Stmt, canonicalID, editionID int64, relation string, groupID *int64, confidence float64, sourceID int64) error {
	var gid any
	if groupID != nil {
		gid = *groupID
	}
	if _, err := stmt.Exec(canonicalID, editionID, relation, gid, confidence, sourceID); err != nil {
		return fmt.Errorf("insert alignment %d<->%d: %w", canonicalID, editionID, err)
	}
	return nil
}

// classify determines the relation+confidence for a producedGroup. Returns
// relation=="" for a pure insertion/deletion, which gets no row.
//
// Every non-empty relation's confidence is capped by pg.opConfidence, the
// size-agreement of the chapter-level operation that produced this pairing
// (the weakest-link rule): a verse pairing can be no more certain than the
// chapter operation it sits inside. This is what keeps a renumber leaf that
// was position-allocated inside a size-mismatched chapter merge/divide (eg
// Jeremiah's displaced-block region) from claiming the same confidence as a
// renumber from a clean, equal-size 1:1 chapter substitution (eg the Psalms
// or Joel boundary shift, where opConfidence is 1.0 and nothing is capped).
// The cap is purely count-derived - no hand-curation, no per-book table
// (invariant #9). A renumber born from an equal-size *coincidental*
// substitution still reports the full ceiling; distinguishing that case
// needs book-level reorder knowledge the verse counts don't carry.
func classify(canonical, edition []verseRow, pg producedGroup) (relation string, confidence float64) {
	g := pg.g
	switch {
	case len(g.AIdx) == 1 && len(g.BIdx) == 1:
		c, e := canonical[g.AIdx[0]], edition[g.BIdx[0]]
		if c.chapter == e.chapter && c.verse == e.verse {
			return RelationExact, minFloat(1.0, pg.opConfidence)
		}
		return RelationRenumber, minFloat(renumberConfidence, pg.opConfidence)
	case len(g.AIdx) > 1:
		return RelationMerge, minFloat(1.0/float64(len(g.AIdx)), pg.opConfidence)
	case len(g.BIdx) > 1:
		return RelationDivide, minFloat(1.0/float64(len(g.BIdx)), pg.opConfidence)
	default:
		return "", 0
	}
}

// alignBook runs the two-level algorithm described in the package doc:
// chapter-level AlignWeighted first, then a verse-level pass scoped to
// each resulting chapter-level correspondence.
//
// The OpSubstitute case deliberately does NOT attempt verse-number label
// matching to find the within-chapter correspondence, even though the
// chapter pairing is already established. Label equality is not a safe
// discovery signal at the verse level any more than at the chapter level:
// a verse-number coincidence is still a coincidence, and trusting it
// manufactures a confident "exact" claim the data doesn't support.
// Confirmed on the real corpus in two different shapes - Brenton Psalms
// 5/7 (a leading title verse shifts everything after it within the SAME
// chapter number, no chapter-level merge at all) and Exodus 7/8
// (canonical 25/32 vs edition 29/28 - content genuinely crosses the
// chapter boundary, but the chapter-level DP still chose to substitute
// 7<->7 and 8<->8 since that was cheaper than a merge/divide, leaving a
// residual within-chapter mismatch). An earlier "trust labels in this
// direction, not that one" heuristic fixed the first shape and missed the
// second - confirming there is no safe halfway point; verse-label trust
// is unsafe in general, not patchable case by case. A chapter where sizes
// happen to match exactly still produces "exact" rows - not because
// labels were checked first, but because align.FillGap's equal-count
// branch pairs position-for-position, and when sizes truly match that
// pairing's labels are correct by construction. Divergence (LXX genuinely
// differing from the Hebrew/KJV spine) is the expected, useful output of
// this aligner, per the Concord spec - the goal is to surface it
// accurately as renumber/merge/divide, not manufacture false agreement.
// alignBook returns the verse-alignment groups for one book. When
// suppressDivergent is set (a recension-divergent book, eg Jeremiah vs the
// LXX), the groups whose canonical chapter falls in the reordered span are
// dropped rather than emitted: the count-based aligner cannot recover the
// true correspondence across a moved block, so it refuses to assert one
// there instead of manufacturing a confident-looking wrong mapping. The
// second return value is the number of canonical verses so suppressed. The
// span is derived mechanically from the alignment's own structural operations
// (see structuralCanonicalSpan); the clean head and tail where numbering runs
// parallel (eg Jeremiah 1-24 and 52) keep their alignment.
func alignBook(canonical, edition []verseRow, suppressDivergent bool) ([]producedGroup, int) {
	canonChapters := chapterSpans(canonical)
	editChapters := chapterSpans(edition)

	aWeights := make([]int, len(canonChapters))
	for i, c := range canonChapters {
		aWeights[i] = c.end - c.start
	}
	bWeights := make([]int, len(editChapters))
	for i, c := range editChapters {
		bWeights[i] = c.end - c.start
	}

	ops := align.AlignWeighted(aWeights, bWeights)

	var groups []producedGroup
	for _, op := range ops {
		switch op.Kind {
		case align.OpDelete:
			span := canonChapters[op.AIdx[0]]
			// Pure deletion: single-sided groups, no confidence consumed.
			groups = tag(groups, align.FillGap(indexRange(span.start, span.end), nil), 1.0)
		case align.OpInsert:
			span := editChapters[op.BIdx[0]]
			groups = tag(groups, align.FillGap(nil, indexRange(span.start, span.end)), 1.0)
		case align.OpSubstitute:
			cSpan, eSpan := canonChapters[op.AIdx[0]], editChapters[op.BIdx[0]]
			conf := sizeReliability(cSpan.end-cSpan.start, eSpan.end-eSpan.start)
			groups = tag(groups, align.FillGap(indexRange(cSpan.start, cSpan.end), indexRange(eSpan.start, eSpan.end)), conf)
		case align.OpMerge:
			c1, c2 := canonChapters[op.AIdx[0]], canonChapters[op.AIdx[1]]
			eSpan := editChapters[op.BIdx[0]]
			conf := sizeReliability((c1.end-c1.start)+(c2.end-c2.start), eSpan.end-eSpan.start)
			partitions := [][]int{indexRange(c1.start, c1.end), indexRange(c2.start, c2.end)}
			groups = tag(groups, proportionalSplit(partitions, indexRange(eSpan.start, eSpan.end)), conf)
		case align.OpDivide:
			cSpan := canonChapters[op.AIdx[0]]
			e1, e2 := editChapters[op.BIdx[0]], editChapters[op.BIdx[1]]
			conf := sizeReliability(cSpan.end-cSpan.start, (e1.end-e1.start)+(e2.end-e2.start))
			partitions := [][]int{indexRange(e1.start, e1.end), indexRange(e2.start, e2.end)}
			groups = tag(groups, swapGroups(proportionalSplit(partitions, indexRange(cSpan.start, cSpan.end))), conf)
		}
	}

	if suppressDivergent {
		lo, hi, ok := structuralCanonicalSpan(ops, canonChapters, canonical)
		if ok {
			return dropCanonicalSpan(groups, canonical, lo, hi)
		}
	}
	return groups, 0
}

// structuralCanonicalSpan returns the inclusive canonical chapter range
// spanned by the alignment's structural operations - merge, divide, delete:
// anything that is not a clean 1:1 substitution. That range is where an
// edition's numbering stops running parallel to the canonical spine; for a
// recension-divergent book it bounds the reordered block. Clean 1:1
// substitutions before the first and after the last structural op (eg
// Jeremiah 1-24 and 52) are outside it and keep their alignment. ok is false
// when there are no structural ops at all (nothing to suppress).
func structuralCanonicalSpan(ops []align.Op, canonChapters []chapterSpan, canonical []verseRow) (lo, hi int, ok bool) {
	for _, op := range ops {
		if op.Kind == align.OpSubstitute || op.Kind == align.OpInsert {
			continue // OpInsert consumes no canonical chapter
		}
		for _, ai := range op.AIdx {
			chap := canonical[canonChapters[ai].start].chapter
			if !ok {
				lo, hi, ok = chap, chap, true
				continue
			}
			if chap < lo {
				lo = chap
			}
			if chap > hi {
				hi = chap
			}
		}
	}
	return lo, hi, ok
}

// dropCanonicalSpan removes every group whose canonical verses fall in the
// inclusive chapter range [lo,hi], returning the kept groups and the count of
// canonical verses dropped. Groups with no canonical side (edition-only
// insertions) are never dropped - suppression targets false canonical->edition
// claims, not genuine LXX-only content.
func dropCanonicalSpan(groups []producedGroup, canonical []verseRow, lo, hi int) ([]producedGroup, int) {
	kept := groups[:0:0]
	suppressed := 0
	for _, pg := range groups {
		if len(pg.g.AIdx) > 0 && canonical[pg.g.AIdx[0]].chapter >= lo && canonical[pg.g.AIdx[0]].chapter <= hi {
			suppressed += len(pg.g.AIdx)
			continue
		}
		kept = append(kept, pg)
	}
	return kept, suppressed
}

// bookUSFMCode looks up a book's canonical USFM code by id (eg "JER") - the
// key recension.IsDivergent matches on, and the same identifier the retriever
// carries as Ref.Book.
func bookUSFMCode(tx *sql.Tx, bookID int64) (string, error) {
	var code string
	if err := tx.QueryRow(`SELECT code FROM books WHERE id = ?`, bookID).Scan(&code); err != nil {
		return "", fmt.Errorf("book code for id %d: %w", bookID, err)
	}
	return code, nil
}

// producedGroup pairs an align.Group with opConfidence: the confidence
// ceiling imposed by the chapter-level operation that produced it. A verse
// pairing can be no more certain than the chapter operation it sits inside
// (the weakest-link rule). opConfidence is 1.0 for a size-matched chapter
// substitution and drops toward 0 as the operation's verse-count mismatch
// grows; classify caps each leaf's confidence by it.
type producedGroup struct {
	g            align.Group
	opConfidence float64
}

// tag wraps each align.Group produced by one chapter-level operation with
// that operation's opConfidence, appending to dst.
func tag(dst []producedGroup, gs []align.Group, opConfidence float64) []producedGroup {
	for _, g := range gs {
		dst = append(dst, producedGroup{g: g, opConfidence: opConfidence})
	}
	return dst
}

// sizeReliability scores how well two chapter-region verse counts agree, in
// [0,1]: 1.0 for an exact match (the strongest positional signal), falling
// linearly toward 0 as the mismatch approaches the larger side's whole size.
// Purely count-derived - the same signal AlignWeighted minimized, reused
// here to bound how much a downstream verse pairing may claim.
func sizeReliability(aWeight, bWeight int) float64 {
	hi := aWeight
	if bWeight > hi {
		hi = bWeight
	}
	if hi == 0 {
		return 1.0
	}
	diff := aWeight - bWeight
	if diff < 0 {
		diff = -diff
	}
	return 1.0 - float64(diff)/float64(hi)
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// proportionalSplit splits otherSide proportionally across each partition
// in partitions (by partition size, align.ProportionalAllocate), then
// align.FillGap's each partition against its allocated slice of otherSide.
// Returns groups with AIdx=partitions, BIdx=otherSide; callers needing the
// reverse assignment (the divide direction, where the edition side is
// partitioned) use swapGroups.
func proportionalSplit(partitions [][]int, otherSide []int) []align.Group {
	weights := make([]int, len(partitions))
	for i, p := range partitions {
		weights[i] = len(p)
	}
	alloc := align.ProportionalAllocate(len(otherSide), weights)

	var groups []align.Group
	pos := 0
	for i, p := range partitions {
		n := alloc[i]
		groups = append(groups, align.FillGap(p, otherSide[pos:pos+n])...)
		pos += n
	}
	return groups
}

func swapGroups(groups []align.Group) []align.Group {
	out := make([]align.Group, len(groups))
	for i, g := range groups {
		out[i] = align.Group{AIdx: g.BIdx, BIdx: g.AIdx}
	}
	return out
}

// chapterSpans groups an ordered verseRow slice (already sorted by
// chapter, verse) into per-chapter contiguous index ranges.
func chapterSpans(verses []verseRow) []chapterSpan {
	if len(verses) == 0 {
		return nil
	}
	var spans []chapterSpan
	start := 0
	for i := 1; i <= len(verses); i++ {
		if i == len(verses) || verses[i].chapter != verses[start].chapter {
			spans = append(spans, chapterSpan{start: start, end: i})
			start = i
		}
	}
	return spans
}

func indexRange(start, end int) []int {
	if end <= start {
		return nil
	}
	out := make([]int, end-start)
	for i := range out {
		out[i] = start + i
	}
	return out
}

func queryBookIDs(db *sql.DB, versification string) ([]int64, error) {
	rows, err := db.Query(`SELECT DISTINCT book_id FROM verses WHERE versification = ? ORDER BY book_id`, versification)
	if err != nil {
		return nil, fmt.Errorf("query book ids for %s: %w", versification, err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan book id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func queryBookVerses(tx *sql.Tx, versification string, bookID int64) ([]verseRow, error) {
	rows, err := tx.Query(`
		SELECT id, chapter, verse FROM verses
		WHERE versification = ? AND book_id = ?
		ORDER BY chapter, verse`, versification, bookID)
	if err != nil {
		return nil, fmt.Errorf("query verses %s/%d: %w", versification, bookID, err)
	}
	defer rows.Close()
	var out []verseRow
	for rows.Next() {
		var v verseRow
		if err := rows.Scan(&v.id, &v.chapter, &v.verse); err != nil {
			return nil, fmt.Errorf("scan verse: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
