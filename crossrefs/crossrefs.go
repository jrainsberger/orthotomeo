// Package crossrefs loads the OpenBible.info / Treasury of Scripture Knowledge
// cross-reference dataset (CC-BY) into cross_references. Links are deterministic
// data with a signed community vote weight; they are never LLM-synthesized.
// Ticket 21.
package crossrefs

import (
	"bufio"
	"database/sql"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/jrainsberger/orthotomeo/verses"
)

// sourceCode is the sources.code this loader attributes its rows to.
const sourceCode = "OpenBible-xref"

// Load reads the TSV cross-reference file (From, To[+range], Votes; OSIS refs)
// and inserts resolvable rows into cross_references, in one transaction.
// Returns (#inserted, #skipped). Unresolvable refs are counted as skipped and
// reported, never silently dropped; a skip is data quality, not a crash.
func Load(db *sql.DB, r io.Reader) (inserted, skipped int, err error) {
	var sourceID int64
	if err := db.QueryRow(`SELECT id FROM sources WHERE code = ?`, sourceCode).Scan(&sourceID); err != nil {
		return 0, 0, fmt.Errorf("source %q not seeded: %w", sourceCode, err)
	}

	res, err := verses.NewResolver(db, "osis")
	if err != nil {
		return 0, 0, err
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, 0, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO cross_references (from_verse, to_verse, to_verse_end, votes, source_id)
		VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, 0, fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Split(sc.Text(), "\t")
		if len(fields) < 3 {
			continue
		}
		votes, verr := strconv.Atoi(strings.TrimSpace(fields[2]))
		if verr != nil {
			continue // header line or comment ("Votes", trailing "#...")
		}

		fromID, ferr := res.Resolve(fields[0])
		toID, toEnd, terr := resolveTarget(res, fields[1])
		if ferr != nil || terr != nil {
			skipped++
			continue
		}

		if _, err := stmt.Exec(fromID, toID, toEnd, votes, sourceID); err != nil {
			return 0, 0, fmt.Errorf("insert %s->%s: %w", fields[0], fields[1], err)
		}
		inserted++
	}
	if err := sc.Err(); err != nil {
		return 0, 0, fmt.Errorf("scan: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("commit: %w", err)
	}
	return inserted, skipped, nil
}

// resolveTarget resolves a To field that may be a single verse or a range
// ("Col.1.16-Col.1.17"). The end is NULL for a single verse.
func resolveTarget(res *verses.Resolver, field string) (start int64, end sql.NullInt64, err error) {
	lo, hi, ranged := strings.Cut(field, "-")
	start, err = res.Resolve(lo)
	if err != nil {
		return 0, sql.NullInt64{}, err
	}
	if ranged {
		e, eerr := res.Resolve(hi)
		if eerr != nil {
			return 0, sql.NullInt64{}, eerr
		}
		end = sql.NullInt64{Int64: e, Valid: true}
	}
	return start, end, nil
}
