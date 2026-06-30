// Package versetext loads verbatim per-edition English prose (KJV, ASV; the
// JSON editions share one shape) into verse_text, FK'd to verses and sources.
// Every row must resolve - these editions define the canonical spine, so an
// unresolvable verse is a corpus inconsistency, not normal skip-and-report
// data quality (contrast crossrefs, whose targets may legitimately fall
// outside the canonical 66-book scope). Ticket 7.
package versetext

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"

	"github.com/jrainsberger/orthotomeo/verses"
)

// doc is the shared shape of the JSON editions (KJV, ASV): books[].chapters[].verses[].
type doc struct {
	Books []struct {
		Name     string `json:"name"`
		Chapters []struct {
			Chapter int `json:"chapter"`
			Verses  []struct {
				Verse int    `json:"verse"`
				Text  string `json:"text"`
			} `json:"verses"`
		} `json:"chapters"`
	} `json:"books"`
}

// Load reads one JSON edition (KJV.json or ASV.json shape) and inserts every
// verse into verse_text, attributed to sourceCode. Returns the row count.
// Runs in one transaction so a partial load never lands.
func Load(db *sql.DB, r io.Reader, sourceCode string) (int, error) {
	var d doc
	if err := json.NewDecoder(r).Decode(&d); err != nil {
		return 0, fmt.Errorf("decode %s: %w", sourceCode, err)
	}

	var sourceID int64
	if err := db.QueryRow(`SELECT id FROM sources WHERE code = ?`, sourceCode).Scan(&sourceID); err != nil {
		return 0, fmt.Errorf("source %q not seeded: %w", sourceCode, err)
	}

	res, err := verses.NewResolver(db, "name-en")
	if err != nil {
		return 0, err
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO verse_text (verse_id, source_id, native_ref, text)
		VALUES (?, ?, ?, ?)`)
	if err != nil {
		return 0, fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	n := 0
	for _, b := range d.Books {
		for _, c := range b.Chapters {
			for _, v := range c.Verses {
				verseID, err := res.Resolve(fmt.Sprintf("%s.%d.%d", b.Name, c.Chapter, v.Verse))
				if err != nil {
					return 0, fmt.Errorf("%s: %w", sourceCode, err)
				}
				nativeRef := fmt.Sprintf("%s %d:%d", b.Name, c.Chapter, v.Verse)
				if _, err := stmt.Exec(verseID, sourceID, nativeRef, v.Text); err != nil {
					return 0, fmt.Errorf("insert %s %s: %w", sourceCode, nativeRef, err)
				}
				n++
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return n, nil
}
