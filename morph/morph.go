// Package morph loads the STEPBible morphology code expansions (TEGMC Greek +
// TEHMC Hebrew) into morph_codes, the human-readable lookup for the codes
// tagged on each word in TAGNT/TAHOT. Ticket 6.
package morph

import (
	"bufio"
	"database/sql"
	"fmt"
	"io"
	"strings"
)

// sectionMarker opens the "FULL MORPHOLOGY CODES" table - the codes actually
// used in the tagged texts (as opposed to the file's earlier "brief lexical"
// table, which describes the same codes more loosely for lexicon entries).
const sectionMarker = "FULL MORPHOLOGY CODES:"

// descriptionPrefix marks a genuine code-expansion line. The file also has a
// trailing appendix of bare "<code>\tKJV" attestation markers after the last
// real entry; filtering on this prefix excludes that appendix without having
// to locate its boundary by line number.
const descriptionPrefix = "Function="

// Load reads one morph-codes file (TEGMC or TEHMC) and inserts its full-code
// entries into morph_codes, tagged with language. Returns the count inserted.
// Runs in one transaction so a partial load never lands.
func Load(db *sql.DB, r io.Reader, language string) (int, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO morph_codes (code, language, description) VALUES (?, ?, ?)`)
	if err != nil {
		return 0, fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	n := 0
	sc := bufio.NewScanner(r)
	inSection := false
	for sc.Scan() {
		line := sc.Text()
		if !inSection {
			if strings.Contains(line, sectionMarker) {
				inSection = true
			}
			continue
		}
		code, desc, ok := codeLine(line)
		if !ok {
			continue
		}
		if _, err := stmt.Exec(code, language, desc); err != nil {
			return 0, fmt.Errorf("insert %s: %w", code, err)
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

// codeLine reports whether line is a "<code>\t<description>" entry row (as
// opposed to a continuation line, the table header, a rule, or appendix
// noise) and returns its parts.
func codeLine(line string) (code, desc string, ok bool) {
	code, desc, found := strings.Cut(line, "\t")
	if !found || code == "" || strings.Contains(code, " ") || strings.HasPrefix(code, `"`) {
		return "", "", false
	}
	if !strings.HasPrefix(desc, descriptionPrefix) {
		return "", "", false
	}
	return code, desc, true
}

// Expand returns the description for a known morphology code.
func Expand(db *sql.DB, code string) (string, error) {
	var desc string
	if err := db.QueryRow(`SELECT description FROM morph_codes WHERE code = ?`, code).Scan(&desc); err != nil {
		return "", fmt.Errorf("expand %s: %w", code, err)
	}
	return desc, nil
}
