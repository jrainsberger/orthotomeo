// Package verify makes invariant #3 (complete-or-fail, never a silent
// partial read) enforceable against a built database, not just aspirational.
// It runs read-only checks over an already-built DB; it never repairs data -
// a failure means the build is wrong and must be re-run or investigated, not
// patched by verify itself (invariant #9: no hand-curation).
package verify

import (
	"database/sql"
	"fmt"
)

// Issue is one failed assertion.
type Issue struct {
	Check  string
	Detail string
}

// Note is informational: a known, documented, small discrepancy that is not
// itself a failure (e.g. the 5 TAGNT dStrongs absent from TBESG, T10's
// doc'd gap) but is worth surfacing rather than silently swallowing.
type Note struct {
	Check  string
	Detail string
}

// Report is the result of Run. A report with no Issues means the DB passed
// every completeness assertion; Notes never fail the build.
type Report struct {
	Issues []Issue
	Notes  []Note
}

// OK reports whether the build passed every check.
func (r Report) OK() bool { return len(r.Issues) == 0 }

func (r *Report) fail(check, format string, args ...any) {
	r.Issues = append(r.Issues, Issue{Check: check, Detail: fmt.Sprintf(format, args...)})
}

func (r *Report) note(check, format string, args ...any) {
	r.Notes = append(r.Notes, Note{Check: check, Detail: fmt.Sprintf(format, args...)})
}

// CountExpectation asserts that Query (a single-column COUNT query taking
// Args as placeholders) returns exactly Want. Used for known per-edition
// totals (T14's "known per-edition verse totals match documented
// expectations" bullet) - the numbers are what a full real build actually
// produced, recorded here so a future dropped row fails loudly instead of
// silently shrinking the corpus.
type CountExpectation struct {
	Label string
	Query string
	Args  []any
	Want  int
}

// sourceCount builds the standard "rows in table T for source code C" query.
func sourceCount(table string) string {
	return fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE source_id = (SELECT id FROM sources WHERE code = ?)`, table)
}

// DefaultExpectations are the totals a full real build (cmd/build, default
// --corpus/--reference roots) produces today. Update these only when a
// corpus source or loader genuinely changes scope - never to make a
// regression pass.
var DefaultExpectations = []CountExpectation{
	{"canonical verse spine", `SELECT COUNT(*) FROM verses WHERE versification = 'canonical'`, nil, 31102},
	{"cross_references", `SELECT COUNT(*) FROM cross_references`, nil, 344794},
	{"lexicon entries", `SELECT COUNT(*) FROM lexicon`, nil, 22717},
	{"morph_codes", `SELECT COUNT(*) FROM morph_codes`, nil, 2565},
	{"KJV verse_text", sourceCount("verse_text"), []any{"KJV"}, 31102},
	{"ASV verse_text", sourceCount("verse_text"), []any{"ASV"}, 31102},
	{"WEB verse_text", sourceCount("verse_text"), []any{"WEB"}, 31095},
	{"Brenton verse_text", sourceCount("verse_text"), []any{"Brenton"}, 22690},
	{"TAGNT words", sourceCount("words"), []any{"TAGNT"}, 141720},
	{"TAHOT words", sourceCount("words"), []any{"TAHOT"}, 283734},
	{"Swete words", sourceCount("words"), []any{"Swete"}, 476937},
	{"OSS words", sourceCount("words"), []any{"OSS-LXX-lemma"}, 425299},
}

// bookCoverage lists the (source, table, section) triples expected to cover
// every book in that section with at least one row. section == "" means
// every book (ot + nt). A book with zero rows here means a loader silently
// dropped an entire book, not just a few verses (T8's 3-verse WEB skip is
// fine; a 66th-book-sized hole is not).
type bookCoverage struct {
	sourceCode, table, section string
}

var fullCanonCoverage = []bookCoverage{
	{"KJV", "verse_text", ""},
	{"ASV", "verse_text", ""},
	{"WEB", "verse_text", ""},
	{"TAGNT", "words", "nt"},
	{"TAHOT", "words", "ot"},
}

// maxKnownDstrongGap bounds the number of DISTINCT dstrong/morph_code
// values (not row occurrences - one missing term can tag hundreds of word
// instances) that may fail to resolve before it's treated as a regression
// rather than a known cross-file gap. T10's package doc confirms 5 distinct
// TAGNT dStrongs are absent from TBESG; a live audit of TAHOT found 20
// distinct dStrongs absent from TBESH. 40 leaves headroom for both without
// masking a real, much larger break.
const maxKnownDstrongGap = 40

// Run executes the full completeness self-test over a built DB.
func Run(db *sql.DB, expectations []CountExpectation) (Report, error) {
	var r Report
	if err := checkSourceIDs(db, &r); err != nil {
		return r, err
	}
	if err := checkForeignKeys(db, &r); err != nil {
		return r, err
	}
	if err := checkBookCoverage(db, &r); err != nil {
		return r, err
	}
	if err := checkLemmaAgreement(db, &r); err != nil {
		return r, err
	}
	if err := checkDstrongMorphResolution(db, &r); err != nil {
		return r, err
	}
	if err := checkCounts(db, expectations, &r); err != nil {
		return r, err
	}
	return r, nil
}

// checkSourceIDs asserts every words/verse_text row carries a non-null
// source_id. The schema already declares NOT NULL, so this catches a schema
// regression or a hand-edited DB, not a normal loader bug.
func checkSourceIDs(db *sql.DB, r *Report) error {
	for _, table := range []string{"words", "verse_text"} {
		var n int
		if err := db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE source_id IS NULL`, table)).Scan(&n); err != nil {
			return fmt.Errorf("checkSourceIDs %s: %w", table, err)
		}
		if n > 0 {
			r.fail("source_id", "%d rows in %s have a NULL source_id", n, table)
		}
	}
	return nil
}

// checkForeignKeys runs SQLite's own foreign-key integrity check across
// every declared FK in the schema (verses.book_id, verse_text.verse_id,
// words.verse_id, verse_alignment's two verse FKs, cross_references, etc).
// This works even though store.Open already sets PRAGMA foreign_keys=ON at
// insert time - it is a second, independent pass over the DB as it actually
// sits on disk.
func checkForeignKeys(db *sql.DB, r *Report) error {
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("checkForeignKeys: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var table string
		var rowid sql.NullInt64
		var parent string
		var fkid int
		if err := rows.Scan(&table, &rowid, &parent, &fkid); err != nil {
			return fmt.Errorf("checkForeignKeys scan: %w", err)
		}
		r.fail("foreign_key", "%s row %v: dangling FK #%d into %s", table, rowid, fkid, parent)
	}
	return rows.Err()
}

// checkBookCoverage asserts no book in scope is silently empty for the
// sources documented as full-canon (fullCanonCoverage).
func checkBookCoverage(db *sql.DB, r *Report) error {
	for _, c := range fullCanonCoverage {
		expected, err := expectedBooks(db, c.section)
		if err != nil {
			return err
		}
		present, err := presentBooks(db, c.table, c.sourceCode)
		if err != nil {
			return err
		}
		for _, code := range expected {
			if !present[code] {
				r.fail("book_coverage", "%s: %s has zero rows in %s (source %s)", c.sourceCode, code, c.table, c.sourceCode)
			}
		}
	}
	return nil
}

func expectedBooks(db *sql.DB, section string) ([]string, error) {
	query := `SELECT code FROM books`
	var args []any
	if section != "" {
		query += ` WHERE section = ?`
		args = append(args, section)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("expectedBooks: %w", err)
	}
	defer rows.Close()
	var codes []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	return codes, rows.Err()
}

func presentBooks(db *sql.DB, table, sourceCode string) (map[string]bool, error) {
	query := fmt.Sprintf(`
		SELECT DISTINCT b.code
		FROM %s t
		JOIN verses v ON v.id = t.verse_id
		JOIN books b ON b.id = v.book_id
		WHERE t.source_id = (SELECT id FROM sources WHERE code = ?)`, table)
	rows, err := db.Query(query, sourceCode)
	if err != nil {
		return nil, fmt.Errorf("presentBooks %s/%s: %w", table, sourceCode, err)
	}
	defer rows.Close()
	present := map[string]bool{}
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		present[code] = true
	}
	return present, rows.Err()
}

// checkLemmaAgreement guards the retriever contract Phase 5 will build on
// (Concord spec: Count(lemma) == len(Concord(lemma)) for every query, never
// a silently truncated result). T15/T16 don't exist yet, so this checks the
// same failure mode one level down: for a deterministic sample of dStrongs
// actually present, an aggregate COUNT(*) must agree with the number of rows
// a full row scan actually yields for the identical WHERE clause.
func checkLemmaAgreement(db *sql.DB, r *Report) error {
	sample, err := db.Query(`SELECT DISTINCT dstrong FROM words WHERE dstrong IS NOT NULL ORDER BY dstrong LIMIT 25`)
	if err != nil {
		return fmt.Errorf("checkLemmaAgreement sample: %w", err)
	}
	var dstrongs []string
	for sample.Next() {
		var d string
		if err := sample.Scan(&d); err != nil {
			sample.Close()
			return err
		}
		dstrongs = append(dstrongs, d)
	}
	if err := sample.Err(); err != nil {
		return err
	}
	sample.Close()

	for _, d := range dstrongs {
		var want int
		if err := db.QueryRow(`SELECT COUNT(*) FROM words WHERE dstrong = ?`, d).Scan(&want); err != nil {
			return fmt.Errorf("checkLemmaAgreement count %s: %w", d, err)
		}
		rows, err := db.Query(`SELECT id FROM words WHERE dstrong = ?`, d)
		if err != nil {
			return fmt.Errorf("checkLemmaAgreement scan %s: %w", d, err)
		}
		got := 0
		for rows.Next() {
			got++
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if got != want {
			r.fail("lemma_agreement", "dstrong %s: COUNT(*)=%d but row scan returned %d", d, want, got)
		}
	}
	return nil
}

// checkDstrongMorphResolution reports (informationally, not as a failure
// unless it exceeds the documented gap) words rows whose non-null
// dstrong/morph_code doesn't resolve in lexicon/morph_codes. These columns
// are intentionally plain TEXT, not hard FKs (T10's package doc): a small,
// confirmed number of cross-file gaps exist in the source data itself.
func checkDstrongMorphResolution(db *sql.DB, r *Report) error {
	var n int
	if err := db.QueryRow(`
		SELECT COUNT(DISTINCT w.dstrong) FROM words w
		WHERE w.dstrong IS NOT NULL
		AND NOT EXISTS (SELECT 1 FROM lexicon l WHERE l.dstrong = w.dstrong)`).Scan(&n); err != nil {
		return fmt.Errorf("checkDstrongMorphResolution dstrong: %w", err)
	}
	if n > 0 {
		if n > maxKnownDstrongGap {
			r.fail("dstrong_resolution", "%d distinct dstrong values are absent from lexicon (documented gap is <=%d)", n, maxKnownDstrongGap)
		} else {
			r.note("dstrong_resolution", "%d distinct dstrong values are absent from lexicon (within documented gap)", n)
		}
	}

	var m int
	if err := db.QueryRow(`
		SELECT COUNT(DISTINCT w.morph_code) FROM words w
		WHERE w.morph_code IS NOT NULL
		AND NOT EXISTS (SELECT 1 FROM morph_codes c WHERE c.code = w.morph_code)`).Scan(&m); err != nil {
		return fmt.Errorf("checkDstrongMorphResolution morph_code: %w", err)
	}
	if m > 0 {
		if m > maxKnownDstrongGap {
			r.fail("morph_resolution", "%d distinct morph_code values are absent from morph_codes (documented gap is <=%d)", m, maxKnownDstrongGap)
		} else {
			r.note("morph_resolution", "%d distinct morph_code values are absent from morph_codes (within documented gap)", m)
		}
	}
	return nil
}

// checkCounts asserts every expectation's query returns exactly Want.
func checkCounts(db *sql.DB, expectations []CountExpectation, r *Report) error {
	for _, e := range expectations {
		var got int
		if err := db.QueryRow(e.Query, e.Args...).Scan(&got); err != nil {
			return fmt.Errorf("checkCounts %s: %w", e.Label, err)
		}
		if got != e.Want {
			r.fail("edition_totals", "%s: got %d, want %d", e.Label, got, e.Want)
		}
	}
	return nil
}
