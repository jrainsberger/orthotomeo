// Package lexicon loads the STEPBible Strong's lemma/definition dictionary
// (TBESG Greek + TBESH Hebrew) into the lexicon table, the dictionary the
// words.dstrong -> lexicon.dstrong bridge joins to (invariant #5). Ticket 5.
package lexicon

import (
	"bufio"
	"database/sql"
	"fmt"
	"io"
	"strings"
)

// headerMarker is the unique substring of the real column-header row in both
// TBESG and TBESH; everything before it is front-matter prose that must not
// be parsed as data, and the row itself is not data either.
const headerMarker = "dStrong\tuStrong"

// Load reads one lexicon file (TBESG or TBESH; same eStrong/dStrong/uStrong/
// lemma/translit/morph/gloss/definition shape) and inserts its data rows into
// lexicon, tagging every row with language and defLicense. Returns the count
// inserted. Runs in one transaction so a partial load never lands.
func Load(db *sql.DB, r io.Reader, language, defLicense string) (int, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO lexicon (dstrong, estrong, ustrong, language, lemma, translit, gloss, definition, def_license)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	n := 0
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	pastHeader := false
	for sc.Scan() {
		line := sc.Text()
		if !pastHeader {
			if strings.Contains(line, headerMarker) {
				pastHeader = true
			}
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || isRule(trimmed) {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 8 {
			continue // not a data row (stray prose between header and table)
		}

		eStrong := fields[0]
		dStrong := firstToken(fields[1])
		uStrong := strings.TrimRight(strings.TrimSpace(fields[2]), ", ")
		lemma, translit, gloss, definition := fields[3], fields[4], fields[6], fields[7]

		if _, err := stmt.Exec(dStrong, eStrong, uStrong, language, lemma, translit, gloss, definition, defLicense); err != nil {
			return 0, fmt.Errorf("insert %s: %w", dStrong, err)
		}
		n++
	}
	if err := sc.Err(); err != nil {
		return 0, fmt.Errorf("scan: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return n, nil
}

// isRule reports whether a trimmed line is a "===..." rule, the only other
// non-data line shape that can appear between the header and the table body.
func isRule(s string) bool {
	for _, r := range s {
		if r != '=' {
			return false
		}
	}
	return true
}

// firstToken returns the leading whitespace-delimited token of a dStrong
// field, which carries a relation annotation after it (e.g. "G0002 = the
// Greek of"; "G0001G ="). The token alone is the lexicon.dstrong key.
func firstToken(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// Lookup returns the lemma and gloss for a dstrong key.
func Lookup(db *sql.DB, dstrong string) (lemma, gloss string, err error) {
	err = db.QueryRow(`SELECT lemma, gloss FROM lexicon WHERE dstrong = ?`, dstrong).Scan(&lemma, &gloss)
	if err != nil {
		return "", "", fmt.Errorf("lookup %s: %w", dstrong, err)
	}
	return lemma, gloss, nil
}
