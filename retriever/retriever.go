// Package retriever is the read-only reference-resolution and citation
// surface for the built DB (Concord spec §3-5). It never decides what a
// text means - it guarantees a Ref maps to a Citation with real,
// re-fetchable provenance, and that cross-edition divergence is surfaced as
// data (a Caveat), never a silent shift (invariant #4). Ticket 15.
package retriever

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/jrainsberger/orthotomeo/verses"
)

// Ref is a canonical, edition-neutral reference: a USFM book code
// (books.code, e.g. "GEN", "PSA") plus chapter and verse on the canonical
// (KJV-based) spine. Every retriever call takes a canonical Ref and maps it
// OUT to each edition's own address - never the reverse (invariant #4:
// reconcile at read time, never assume 1:1 across editions).
type Ref struct {
	Book    string
	Chapter int
	Verse   int
}

func (r Ref) dotted() string {
	return fmt.Sprintf("%s.%d.%d", r.Book, r.Chapter, r.Verse)
}

// String renders the dotted form (e.g. "PSA.9.1") used in caveats and logs.
func (r Ref) String() string { return r.dotted() }

// RefRange is a contiguous span within a single book, inclusive of both
// ends. GetPassage rejects a range spanning two books - "the same book"
// isn't well-defined across editions with different canons/versification
// (invariant #4), so a caller wanting a cross-book span must issue separate
// calls.
type RefRange struct {
	Start, End Ref
}

// Address is where a Ref lives in one edition, or a documented absence.
// Exists:false is itself data (T4b: canonical-only or edition-only content
// is not a bug), never an error.
type Address struct {
	Edition string
	File    string
	Locator string
	Exists  bool
}

// Resolution is the full cross-edition picture for one Ref.
type Resolution struct {
	Ref       Ref
	Addresses []Address
	Caveats   []string
}

// Confidence marks whether a Citation's provenance is a direct row (High)
// or reached through T4b's deterministic-but-imperfect verse alignment
// (Flagged - see Caveat for why).
type Confidence string

const (
	ConfidenceHigh    Confidence = "High"
	ConfidenceFlagged Confidence = "Flagged"
)

// Citation is one verbatim, provenance-bearing result row - the unit every
// retriever call returns (Concord spec §5). Lemma/DStrong/Grammar/
// Attestation/Editions are populated only by the tickets that carry them
// (T17/T18); T15 leaves them zero-valued.
type Citation struct {
	Ref           Ref
	Edition       string
	Text          string
	SourceFile    string
	SourceLocator string
	Lemma         string
	DStrong       string
	Grammar       string
	Attestation   string
	Editions      string
	Confidence    Confidence
	Caveat        string
}

// editionInfo describes how one sources.code reaches a canonical Ref.
// canonicalKeyed editions (alignment=false) were loaded by resolving
// directly against the canonical verses spine (T7/T8/T10/T11), so their
// verse_id IS the canonical verse's id. alignment=true editions own their
// own versification (T9/T12/T13) and are related to canonical only through
// T4b's verse_alignment table - never a verse-number guess.
type editionInfo struct {
	sourceCode string
	table      string // "verse_text" | "words"
	alignment  bool
}

// verseTextEditions is the subset GetVerse/GetPassage serve: sources that
// carry continuous verbatim prose (verse_text). TAGNT/TAHOT/Swete/OSS are
// word-tagged streams, not prose, and are out of GetVerse's scope by design
// (T17 Parse/Lemmatize is their surface).
var verseTextEditions = map[string]editionInfo{
	"KJV":     {"KJV", "verse_text", false},
	"ASV":     {"ASV", "verse_text", false},
	"WEB":     {"WEB", "verse_text", false},
	"Brenton": {"Brenton", "verse_text", true},
}

// allEditions is every per-verse content edition ResolveRef reports on, in
// a fixed, deterministic order (never range over the map - invariant #9).
var allEditions = map[string]editionInfo{
	"KJV":           {"KJV", "verse_text", false},
	"ASV":           {"ASV", "verse_text", false},
	"WEB":           {"WEB", "verse_text", false},
	"Brenton":       {"Brenton", "verse_text", true},
	"TAGNT":         {"TAGNT", "words", false},
	"TAHOT":         {"TAHOT", "words", false},
	"Swete":         {"Swete", "words", true},
	"OSS-LXX-lemma": {"OSS-LXX-lemma", "words", true},
}

var editionOrder = []string{"KJV", "ASV", "WEB", "Brenton", "TAGNT", "TAHOT", "Swete", "OSS-LXX-lemma"}

// IsAlignmentKeyed reports whether sourceCode's verse rows are reached only
// through T4b's verse_alignment (true - Brenton/Swete/OSS) or share the
// canonical verses spine directly (false - KJV/ASV/WEB/TAGNT/TAHOT). known
// is false for a sourceCode this package doesn't recognize as a per-verse
// content edition. Exported so other Phase 5 tickets (T16 concordance) that
// also need to map a words/verse_text row back to a canonical Ref don't
// have to re-derive this table.
func IsAlignmentKeyed(sourceCode string) (alignmentKeyed, known bool) {
	info, ok := allEditions[sourceCode]
	if !ok {
		return false, false
	}
	return info.alignment, true
}

// IsWordCorpus reports whether sourceCode carries word-tagged rows (the
// `words` table) rather than continuous prose (`verse_text`) - the four
// corpora T16 (concord), T17 (parse), and T18 (attestation) operate over:
// TAGNT, TAHOT, Swete, OSS-LXX-lemma. Exported as the single source of
// truth so those packages don't each maintain their own copy of this table.
func IsWordCorpus(sourceCode string) bool {
	info, ok := allEditions[sourceCode]
	return ok && info.table == "words"
}

// ResolveRef reports, for every per-verse content edition, whether ref has
// a counterpart there and where - never silently omitting an edition that
// lacks one (Concord spec §3: "the analysis is never handed a silently-
// shifted verse").
func ResolveRef(db *sql.DB, ref Ref) (Resolution, error) {
	res := Resolution{Ref: ref}

	canonRes, err := verses.NewResolver(db, "usfm", verses.Canonical)
	if err != nil {
		return res, err
	}
	canonicalID, err := canonRes.Resolve(ref.dotted())
	if err != nil {
		res.Caveats = append(res.Caveats, fmt.Sprintf("%s does not exist on the canonical spine", ref))
		return res, nil
	}

	for _, code := range editionOrder {
		info := allEditions[code]
		if !info.alignment {
			addr, err := canonicalAddress(db, canonicalID, info)
			if err != nil {
				return res, err
			}
			res.Addresses = append(res.Addresses, addr)
			if !addr.Exists {
				res.Caveats = append(res.Caveats, fmt.Sprintf("%s: no row for %s", code, ref))
			}
			continue
		}
		addrs, caveats, err := alignedAddresses(db, canonicalID, ref, info)
		if err != nil {
			return res, err
		}
		res.Addresses = append(res.Addresses, addrs...)
		res.Caveats = append(res.Caveats, caveats...)
	}
	return res, nil
}

func canonicalAddress(db *sql.DB, canonicalID int64, info editionInfo) (Address, error) {
	file, err := sourceFile(db, info.sourceCode)
	if err != nil {
		return Address{}, err
	}

	switch info.table {
	case "verse_text":
		var locator string
		err := db.QueryRow(`
			SELECT native_ref FROM verse_text
			WHERE verse_id = ? AND source_id = (SELECT id FROM sources WHERE code = ?)`,
			canonicalID, info.sourceCode).Scan(&locator)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return Address{Edition: info.sourceCode, File: file, Exists: false}, nil
		case err != nil:
			return Address{}, fmt.Errorf("canonicalAddress %s: %w", info.sourceCode, err)
		}
		return Address{Edition: info.sourceCode, File: file, Locator: locator, Exists: true}, nil
	case "words":
		var n int
		if err := db.QueryRow(`
			SELECT COUNT(*) FROM words
			WHERE verse_id = ? AND source_id = (SELECT id FROM sources WHERE code = ?)`,
			canonicalID, info.sourceCode).Scan(&n); err != nil {
			return Address{}, fmt.Errorf("canonicalAddress %s: %w", info.sourceCode, err)
		}
		return Address{Edition: info.sourceCode, File: file, Exists: n > 0}, nil
	default:
		return Address{}, fmt.Errorf("canonicalAddress: unknown table %q", info.table)
	}
}

func alignedAddresses(db *sql.DB, canonicalID int64, ref Ref, info editionInfo) ([]Address, []string, error) {
	file, err := sourceFile(db, info.sourceCode)
	if err != nil {
		return nil, nil, err
	}

	rows, err := db.Query(`
		SELECT v.chapter, v.verse, va.relation, va.confidence
		FROM verse_alignment va
		JOIN verses v ON v.id = va.edition_verse_id
		WHERE va.canonical_verse_id = ? AND va.source_id = (SELECT id FROM sources WHERE code = ?)
		ORDER BY va.id`, canonicalID, info.sourceCode)
	if err != nil {
		return nil, nil, fmt.Errorf("alignedAddresses %s: %w", info.sourceCode, err)
	}
	defer rows.Close()

	var addrs []Address
	var caveats []string
	for rows.Next() {
		var ch, v int
		var relation string
		var confidence float64
		if err := rows.Scan(&ch, &v, &relation, &confidence); err != nil {
			return nil, nil, fmt.Errorf("alignedAddresses %s scan: %w", info.sourceCode, err)
		}
		locator := (Ref{Book: ref.Book, Chapter: ch, Verse: v}).dotted()
		addrs = append(addrs, Address{Edition: info.sourceCode, File: file, Locator: locator, Exists: true})
		if relation != "exact" {
			caveats = append(caveats, fmt.Sprintf("%s: %s of %s (confidence %.2f)", info.sourceCode, relation, locator, confidence))
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if len(addrs) == 0 {
		addrs = append(addrs, Address{Edition: info.sourceCode, File: file, Exists: false})
		caveats = append(caveats, fmt.Sprintf("%s: no aligned verse for %s (canonical-only content or an unaligned gap - T4b)", info.sourceCode, ref))
	}
	return addrs, caveats, nil
}

// GetVerse returns one Citation per requested edition (more than one for an
// edition where T4b recorded a merge/divide). An edition that legitimately
// has no counterpart still produces a Citation - empty Text, Flagged, with
// a Caveat - so its absence is data in the result set, not a gap the caller
// has to separately detect.
func GetVerse(db *sql.DB, ref Ref, editions []string) ([]Citation, error) {
	canonRes, err := verses.NewResolver(db, "usfm", verses.Canonical)
	if err != nil {
		return nil, err
	}
	canonicalID, err := canonRes.Resolve(ref.dotted())
	if err != nil {
		return nil, fmt.Errorf("GetVerse %s: %w", ref, err)
	}

	var out []Citation
	for _, edition := range editions {
		info, ok := verseTextEditions[edition]
		if !ok {
			return nil, fmt.Errorf("GetVerse: %q is not a verse-text edition (KJV, ASV, WEB, Brenton)", edition)
		}
		if !info.alignment {
			c, err := canonicalCitation(db, canonicalID, ref, info)
			if err != nil {
				return nil, err
			}
			out = append(out, c)
			continue
		}
		cs, err := alignedCitations(db, canonicalID, ref, info)
		if err != nil {
			return nil, err
		}
		out = append(out, cs...)
	}
	return out, nil
}

func canonicalCitation(db *sql.DB, canonicalID int64, ref Ref, info editionInfo) (Citation, error) {
	file, err := sourceFile(db, info.sourceCode)
	if err != nil {
		return Citation{}, err
	}

	var text, native string
	err = db.QueryRow(`
		SELECT text, native_ref FROM verse_text
		WHERE verse_id = ? AND source_id = (SELECT id FROM sources WHERE code = ?)`,
		canonicalID, info.sourceCode).Scan(&text, &native)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Citation{
			Ref: ref, Edition: info.sourceCode, SourceFile: file,
			Confidence: ConfidenceFlagged,
			Caveat:     fmt.Sprintf("no %s verse_text row for %s", info.sourceCode, ref),
		}, nil
	case err != nil:
		return Citation{}, fmt.Errorf("canonicalCitation %s: %w", info.sourceCode, err)
	}
	return Citation{
		Ref: ref, Edition: info.sourceCode, Text: text,
		SourceFile: file, SourceLocator: native, Confidence: ConfidenceHigh,
	}, nil
}

func alignedCitations(db *sql.DB, canonicalID int64, ref Ref, info editionInfo) ([]Citation, error) {
	file, err := sourceFile(db, info.sourceCode)
	if err != nil {
		return nil, err
	}

	rows, err := db.Query(`
		SELECT va.edition_verse_id, va.relation, va.confidence
		FROM verse_alignment va
		WHERE va.canonical_verse_id = ? AND va.source_id = (SELECT id FROM sources WHERE code = ?)
		ORDER BY va.id`, canonicalID, info.sourceCode)
	if err != nil {
		return nil, fmt.Errorf("alignedCitations %s: %w", info.sourceCode, err)
	}
	defer rows.Close()

	type aligned struct {
		editionVerseID int64
		relation       string
		confidence     float64
	}
	var pairs []aligned
	for rows.Next() {
		var a aligned
		if err := rows.Scan(&a.editionVerseID, &a.relation, &a.confidence); err != nil {
			return nil, fmt.Errorf("alignedCitations %s scan: %w", info.sourceCode, err)
		}
		pairs = append(pairs, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(pairs) == 0 {
		return []Citation{{
			Ref: ref, Edition: info.sourceCode, SourceFile: file,
			Confidence: ConfidenceFlagged,
			Caveat:     fmt.Sprintf("no %s alignment for %s (canonical-only content or an unaligned gap - T4b)", info.sourceCode, ref),
		}}, nil
	}

	var out []Citation
	for _, a := range pairs {
		var text, native string
		if err := db.QueryRow(`
			SELECT text, native_ref FROM verse_text
			WHERE verse_id = ? AND source_id = (SELECT id FROM sources WHERE code = ?)`,
			a.editionVerseID, info.sourceCode).Scan(&text, &native); err != nil {
			return nil, fmt.Errorf("alignedCitations %s text: %w", info.sourceCode, err)
		}
		c := Citation{Ref: ref, Edition: info.sourceCode, Text: text, SourceFile: file, SourceLocator: native}
		if a.relation == "exact" {
			c.Confidence = ConfidenceHigh
		} else {
			c.Confidence = ConfidenceFlagged
			c.Caveat = fmt.Sprintf("T4b alignment: %s (confidence %.2f), not a 1:1 verse match", a.relation, a.confidence)
		}
		out = append(out, c)
	}
	return out, nil
}

// GetPassage returns GetVerse's result for every canonical verse in rr, in
// order - one verse's Citations never bleed into the next (verse
// boundaries are preserved, per the Concord spec).
func GetPassage(db *sql.DB, rr RefRange, editions []string) ([]Citation, error) {
	if rr.Start.Book != rr.End.Book {
		return nil, fmt.Errorf("GetPassage: start and end must be the same book, got %s and %s", rr.Start.Book, rr.End.Book)
	}
	refs, err := versesInRange(db, rr)
	if err != nil {
		return nil, err
	}

	var out []Citation
	for _, ref := range refs {
		cs, err := GetVerse(db, ref, editions)
		if err != nil {
			return nil, err
		}
		out = append(out, cs...)
	}
	return out, nil
}

func versesInRange(db *sql.DB, rr RefRange) ([]Ref, error) {
	rows, err := db.Query(`
		SELECT v.chapter, v.verse FROM verses v
		JOIN books b ON b.id = v.book_id
		WHERE v.versification = 'canonical' AND b.code = ?
		ORDER BY v.chapter, v.verse`, rr.Start.Book)
	if err != nil {
		return nil, fmt.Errorf("versesInRange: %w", err)
	}
	defer rows.Close()

	var refs []Ref
	for rows.Next() {
		var ch, v int
		if err := rows.Scan(&ch, &v); err != nil {
			return nil, fmt.Errorf("versesInRange scan: %w", err)
		}
		if before(ch, v, rr.Start.Chapter, rr.Start.Verse) || after(ch, v, rr.End.Chapter, rr.End.Verse) {
			continue
		}
		refs = append(refs, Ref{Book: rr.Start.Book, Chapter: ch, Verse: v})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return nil, fmt.Errorf("GetPassage: no canonical verses between %s and %s", rr.Start, rr.End)
	}
	return refs, nil
}

func before(ch, v, refCh, refV int) bool { return ch < refCh || (ch == refCh && v < refV) }
func after(ch, v, refCh, refV int) bool  { return ch > refCh || (ch == refCh && v > refV) }

func sourceFile(db *sql.DB, code string) (string, error) {
	var f string
	if err := db.QueryRow(`SELECT source_file FROM sources WHERE code = ?`, code).Scan(&f); err != nil {
		return "", fmt.Errorf("sourceFile %s: %w", code, err)
	}
	return f, nil
}
