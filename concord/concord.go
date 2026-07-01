// Package concord is the concordance surface (Concord spec §4B, invariant
// #3: complete-or-fail). ConcordLemma and ConcordPhrase each internally
// verify their own row count against an independent COUNT(*) before
// returning - a partial read raises rather than silently handing back a
// truncated result set. Ticket 16.
package concord

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jrainsberger/orthotomeo/lexnorm"
	"github.com/jrainsberger/orthotomeo/retriever"
)

// Tally is an occurrence count: the total plus a per-book breakdown.
type Tally struct {
	Total  int            `json:"total"`
	ByBook map[string]int `json:"by_book"`
}

// dstrongRe recognizes a disambiguated Strong's number (e.g. "G0859",
// "H7225G", "G3700H") as opposed to a plain lemma string - the shape every
// dstrong value in the corpus actually takes (letter, 2-5 digits, up to 2
// disambiguation letters).
var dstrongRe = regexp.MustCompile(`^[GH]\d{2,5}[A-Za-z]{0,2}$`)

func matchColumn(query string) string {
	if dstrongRe.MatchString(query) {
		return "dstrong"
	}
	return "lemma"
}

func columnExpr(col string) (string, error) {
	switch col {
	case "dstrong":
		return "w.dstrong", nil
	case "lemma":
		return "w.lemma", nil
	default:
		return "", fmt.Errorf("columnExpr: unknown match column %q", col)
	}
}

// ConcordLemma returns every words row in corpus whose lemma or dStrong
// (auto-detected from query's shape) matches - the complete set or an
// error, never a silent partial read.
func ConcordLemma(db *sql.DB, query, corpus string) ([]retriever.Citation, error) {
	sourceID, err := validateCorpus(db, corpus)
	if err != nil {
		return nil, err
	}
	query = lexnorm.NFC(query)
	col := matchColumn(query)

	want, err := countMatches(db, col, query, sourceID)
	if err != nil {
		return nil, err
	}
	rows, err := wordsMatching(db, col, query, sourceID)
	if err != nil {
		return nil, err
	}
	if err := checkComplete("ConcordLemma", want, len(rows)); err != nil {
		return nil, err
	}

	file, err := sourceFile(db, corpus)
	if err != nil {
		return nil, err
	}

	cites := make([]retriever.Citation, 0, len(rows))
	for _, w := range rows {
		ref, confidence, caveat, err := resolveCanonicalRef(db, w, corpus)
		if err != nil {
			return nil, err
		}
		cites = append(cites, retriever.Citation{
			Ref: ref, Edition: corpus, Text: displayText(w),
			SourceFile: file, SourceLocator: w.sourceLocator,
			Lemma: w.lemma, DStrong: w.dstrong, Grammar: w.morphCode,
			Attestation: w.attestation, Editions: w.editions,
			Confidence: confidence, Caveat: caveat,
		})
	}
	return cites, nil
}

// Count returns the occurrence tally for the same query ConcordLemma would
// match - a caller who only needs numbers doesn't have to materialize every
// Citation. Count(query, corpus).Total always equals
// len(ConcordLemma(query, corpus)) by construction (both run the identical
// WHERE clause).
func Count(db *sql.DB, query, corpus string) (Tally, error) {
	sourceID, err := validateCorpus(db, corpus)
	if err != nil {
		return Tally{}, err
	}
	query = lexnorm.NFC(query)
	col := matchColumn(query)

	total, err := countMatches(db, col, query, sourceID)
	if err != nil {
		return Tally{}, err
	}
	byBook, err := countMatchesByBook(db, col, query, sourceID)
	if err != nil {
		return Tally{}, err
	}
	return Tally{Total: total, ByBook: byBook}, nil
}

// ConcordPhrase finds every occurrence, within one verse, of tokens (lemma
// strings) appearing in order with at most window intervening words between
// each consecutive pair - window=0 means strictly adjacent (the εἰς ἄφεσιν
// query). It does not search across verse boundaries: word_no is verse-
// relative in the source data (T10/T11), so "adjacent" only means anything
// within a single verse.
func ConcordPhrase(db *sql.DB, tokens []string, corpus string, window int) ([]retriever.Citation, error) {
	if len(tokens) < 2 {
		return nil, fmt.Errorf("ConcordPhrase: need at least 2 tokens, got %d", len(tokens))
	}
	if window < 0 {
		return nil, fmt.Errorf("ConcordPhrase: window must be >= 0, got %d", window)
	}
	sourceID, err := validateCorpus(db, corpus)
	if err != nil {
		return nil, err
	}
	normTokens := make([]string, len(tokens))
	for i, t := range tokens {
		normTokens[i] = lexnorm.NFC(t)
	}
	tokens = normTokens

	anchorWant, err := countMatches(db, "lemma", tokens[0], sourceID)
	if err != nil {
		return nil, err
	}
	anchors, err := wordsMatching(db, "lemma", tokens[0], sourceID)
	if err != nil {
		return nil, err
	}
	if err := checkComplete("ConcordPhrase anchor scan", anchorWant, len(anchors)); err != nil {
		return nil, err
	}

	var chains [][]wordRow
	for _, anchor := range anchors {
		chain, ok, err := extendChain(db, anchor, tokens[1:], sourceID, window)
		if err != nil {
			return nil, err
		}
		if ok {
			chains = append(chains, chain)
		}
	}

	file, err := sourceFile(db, corpus)
	if err != nil {
		return nil, err
	}

	cites := make([]retriever.Citation, 0, len(chains))
	for _, chain := range chains {
		ref, confidence, caveat, err := resolveCanonicalRef(db, chain[0], corpus)
		if err != nil {
			return nil, err
		}
		parts := make([]string, len(chain))
		for i, w := range chain {
			parts[i] = displayText(w)
		}
		cites = append(cites, retriever.Citation{
			Ref: ref, Edition: corpus, Text: strings.Join(parts, " "),
			SourceFile: file, SourceLocator: chain[0].sourceLocator,
			Lemma:      strings.Join(tokens, " "),
			Confidence: confidence, Caveat: caveat,
		})
	}
	return cites, nil
}

// extendChain walks forward from anchor matching each of tokens in order,
// each within window intervening words of the previous match, all within
// anchor's own verse. Returns ok=false if any token in the chain isn't
// found - a broken chain, not a completeness failure (most anchors don't
// start a real phrase match; that's expected, not an error).
func extendChain(db *sql.DB, anchor wordRow, tokens []string, sourceID int64, window int) ([]wordRow, bool, error) {
	chain := []wordRow{anchor}
	cur := anchor
	for _, tok := range tokens {
		next, found, err := nextTokenInVerse(db, cur, tok, sourceID, window)
		if err != nil {
			return nil, false, err
		}
		if !found {
			return nil, false, nil
		}
		chain = append(chain, next)
		cur = next
	}
	return chain, true, nil
}

func nextTokenInVerse(db *sql.DB, after wordRow, lemma string, sourceID int64, window int) (wordRow, bool, error) {
	maxWordNo := after.wordNo + 1 + window
	row := db.QueryRow(`
		SELECT w.id, w.verse_id, v.book_id, v.chapter, v.verse, w.word_no,
		       COALESCE(w.surface,''), COALESCE(w.lemma,''), COALESCE(w.dstrong,''),
		       COALESCE(w.morph_code,''), w.attestation, w.editions, w.source_locator
		FROM words w JOIN verses v ON v.id = w.verse_id
		WHERE w.verse_id = ? AND w.lemma = ? AND w.source_id = ? AND w.word_no > ? AND w.word_no <= ?
		ORDER BY w.word_no LIMIT 1`,
		after.verseID, lemma, sourceID, after.wordNo, maxWordNo)
	w, err := scanWordRow(row)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return wordRow{}, false, nil
	case err != nil:
		return wordRow{}, false, fmt.Errorf("nextTokenInVerse: %w", err)
	}
	return w, true, nil
}

// checkComplete is the invariant #3 guard: an independently-counted total
// must equal what was actually scanned, or the caller gets an error instead
// of a silently truncated result.
func checkComplete(op string, want, got int) error {
	if want != got {
		return fmt.Errorf("%s: partial read - COUNT()=%d but scan returned %d rows (invariant #3 violation)", op, want, got)
	}
	return nil
}

func validateCorpus(db *sql.DB, corpus string) (int64, error) {
	if !retriever.IsWordCorpus(corpus) {
		return 0, fmt.Errorf("concord: %q is not a word-tagged corpus (want one of TAGNT, TAHOT, Swete, OSS-LXX-lemma)", corpus)
	}
	var id int64
	if err := db.QueryRow(`SELECT id FROM sources WHERE code = ?`, corpus).Scan(&id); err != nil {
		return 0, fmt.Errorf("validateCorpus %s: %w", corpus, err)
	}
	return id, nil
}

// wordRow is one words table row plus its containing verse's identity -
// the raw material Citations are built from.
type wordRow struct {
	id                     int64
	verseID, bookID        int64
	chapter, verse, wordNo int
	surface, lemma         string
	dstrong, morphCode     string
	attestation, editions  string
	sourceLocator          string
}

func wordsMatching(db *sql.DB, col, value string, sourceID int64) ([]wordRow, error) {
	expr, err := columnExpr(col)
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(`
		SELECT w.id, w.verse_id, v.book_id, v.chapter, v.verse, w.word_no,
		       COALESCE(w.surface,''), COALESCE(w.lemma,''), COALESCE(w.dstrong,''),
		       COALESCE(w.morph_code,''), w.attestation, w.editions, w.source_locator
		FROM words w JOIN verses v ON v.id = w.verse_id
		WHERE %s = ? AND w.source_id = ?
		ORDER BY v.chapter, v.verse, w.word_no`, expr)
	rows, err := db.Query(query, value, sourceID)
	if err != nil {
		return nil, fmt.Errorf("wordsMatching: %w", err)
	}
	defer rows.Close()

	var out []wordRow
	for rows.Next() {
		w, err := scanWordRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func countMatches(db *sql.DB, col, value string, sourceID int64) (int, error) {
	expr, err := columnExpr(col)
	if err != nil {
		return 0, err
	}
	var n int
	err = db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM words w WHERE %s = ? AND w.source_id = ?`, expr), value, sourceID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("countMatches: %w", err)
	}
	return n, nil
}

func countMatchesByBook(db *sql.DB, col, value string, sourceID int64) (map[string]int, error) {
	expr, err := columnExpr(col)
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(`
		SELECT b.code, COUNT(*) FROM words w
		JOIN verses v ON v.id = w.verse_id
		JOIN books b ON b.id = v.book_id
		WHERE %s = ? AND w.source_id = ?
		GROUP BY b.code`, expr)
	rows, err := db.Query(query, value, sourceID)
	if err != nil {
		return nil, fmt.Errorf("countMatchesByBook: %w", err)
	}
	defer rows.Close()

	m := map[string]int{}
	for rows.Next() {
		var code string
		var n int
		if err := rows.Scan(&code, &n); err != nil {
			return nil, err
		}
		m[code] = n
	}
	return m, rows.Err()
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanWordRow(r rowScanner) (wordRow, error) {
	var w wordRow
	err := r.Scan(&w.id, &w.verseID, &w.bookID, &w.chapter, &w.verse, &w.wordNo,
		&w.surface, &w.lemma, &w.dstrong, &w.morphCode, &w.attestation, &w.editions, &w.sourceLocator)
	return w, err
}

// displayText is the Text a Citation shows for one word: the verbatim
// surface form when the source carries one, else the lemma (OSS-LXX-lemma
// is lemma-only - T13 - so surface is always empty there; falling back to
// lemma is honest, not fabricated, since it's still a real column value
// from the source file, just not the inflected surface form).
func displayText(w wordRow) string {
	if w.surface != "" {
		return w.surface
	}
	return w.lemma
}

// resolveCanonicalRef maps a word's own verse back to a canonical Ref.
// canonicalKeyed corpora (TAGNT/TAHOT) share the canonical verses row
// directly. alignmentKeyed corpora (Swete/OSS-LXX-lemma) are related only
// through T4b's verse_alignment; a merge-target edition verse can map from
// MULTIPLE canonical verses, and word-level alignment (T22) doesn't exist
// yet to say which canonical verse THIS word belongs to - rather than
// guess, this picks the first (deterministic, by verse_alignment.id) and
// says so in Caveat, exactly the "report low-confidence, don't guess"
// discipline T4b itself follows for its own residual limitation.
func resolveCanonicalRef(db *sql.DB, w wordRow, corpus string) (retriever.Ref, retriever.Confidence, string, error) {
	alignmentKeyed, known := retriever.IsAlignmentKeyed(corpus)
	if !known {
		return retriever.Ref{}, "", "", fmt.Errorf("resolveCanonicalRef: unknown corpus %q", corpus)
	}

	if !alignmentKeyed {
		var bookCode string
		if err := db.QueryRow(`SELECT code FROM books WHERE id = ?`, w.bookID).Scan(&bookCode); err != nil {
			return retriever.Ref{}, "", "", fmt.Errorf("resolveCanonicalRef: %w", err)
		}
		return retriever.Ref{Book: bookCode, Chapter: w.chapter, Verse: w.verse}, retriever.ConfidenceHigh, "", nil
	}

	rows, err := db.Query(`
		SELECT v.chapter, v.verse, b.code, va.relation, va.confidence
		FROM verse_alignment va
		JOIN verses v ON v.id = va.canonical_verse_id
		JOIN books b ON b.id = v.book_id
		WHERE va.edition_verse_id = ? AND va.source_id = (SELECT id FROM sources WHERE code = ?)
		ORDER BY va.id`, w.verseID, corpus)
	if err != nil {
		return retriever.Ref{}, "", "", fmt.Errorf("resolveCanonicalRef %s: %w", corpus, err)
	}
	defer rows.Close()

	type cref struct {
		ch, v      int
		book       string
		relation   string
		confidence float64
	}
	var crefs []cref
	for rows.Next() {
		var c cref
		if err := rows.Scan(&c.ch, &c.v, &c.book, &c.relation, &c.confidence); err != nil {
			return retriever.Ref{}, "", "", fmt.Errorf("resolveCanonicalRef scan: %w", err)
		}
		crefs = append(crefs, c)
	}
	if err := rows.Err(); err != nil {
		return retriever.Ref{}, "", "", err
	}

	switch len(crefs) {
	case 0:
		return retriever.Ref{}, retriever.ConfidenceFlagged,
			fmt.Sprintf("no canonical alignment for this %s verse (edition-only content or an unaligned gap - T4b)", corpus), nil
	case 1:
		c := crefs[0]
		ref := retriever.Ref{Book: c.book, Chapter: c.ch, Verse: c.v}
		if c.relation == "exact" {
			return ref, retriever.ConfidenceHigh, "", nil
		}
		return ref, retriever.ConfidenceFlagged,
			fmt.Sprintf("T4b alignment: %s (confidence %.2f), not a 1:1 verse match", c.relation, c.confidence), nil
	default:
		c := crefs[0]
		ref := retriever.Ref{Book: c.book, Chapter: c.ch, Verse: c.v}
		return ref, retriever.ConfidenceFlagged, fmt.Sprintf(
			"this word's containing %s verse was merged from %d canonical verses (T4b relation=%s) - the exact canonical verse for THIS WORD is undetermined without word-level alignment (T22, deferred); showing the first of %d: %s",
			corpus, len(crefs), c.relation, len(crefs), ref.String()), nil
	}
}

func sourceFile(db *sql.DB, code string) (string, error) {
	var f string
	if err := db.QueryRow(`SELECT source_file FROM sources WHERE code = ?`, code).Scan(&f); err != nil {
		return "", fmt.Errorf("sourceFile %s: %w", code, err)
	}
	return f, nil
}
