// Package verses builds the canonical verse spine and resolves native
// references to a verses.id. Canonical versification is KJV/English Protestant,
// enumerated from KJV.json (31,102 verses); other editions map onto it (LXX via
// versification_map, T4b). Ticket 4.
package verses

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/jrainsberger/orthotomeo/books"
)

// ErrUnknownVerse means a reference did not resolve to any canonical verse.
var ErrUnknownVerse = errors.New("unknown verse")

// kjvDoc is the minimal shape of KJV.json needed to enumerate the spine.
type kjvDoc struct {
	Books []struct {
		Name     string `json:"name"`
		Chapters []struct {
			Chapter int `json:"chapter"`
			Verses  []struct {
				Verse int `json:"verse"`
			} `json:"verses"`
		} `json:"chapters"`
	} `json:"books"`
}

// BuildSpine enumerates the canonical verse spine from a KJV.json reader,
// resolving each book through the name-en scheme. Returns the verse count.
// Runs in one transaction so a partial spine never lands.
func BuildSpine(db *sql.DB, r io.Reader) (int, error) {
	var doc kjvDoc
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return 0, fmt.Errorf("decode KJV.json: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO verses (book_id, chapter, verse) VALUES (?, ?, ?)`)
	if err != nil {
		return 0, fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	n := 0
	for _, b := range doc.Books {
		bookID, err := books.Resolve(tx, "name-en", b.Name)
		if err != nil {
			return 0, fmt.Errorf("spine: %w", err)
		}
		for _, c := range b.Chapters {
			for _, v := range c.Verses {
				if _, err := stmt.Exec(bookID, c.Chapter, v.Verse); err != nil {
					return 0, fmt.Errorf("insert %s %d:%d: %w", b.Name, c.Chapter, v.Verse, err)
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

// Resolver caches book-name and verse lookups for one scheme so bulk loaders
// (cross-references, future text/word imports) resolve in memory instead of
// per-row queries.
type Resolver struct {
	scheme string
	books  map[string]int64 // native book value -> book_id
	verses map[verseKey]int64
}

type verseKey struct {
	book           int64
	chapter, verse int
}

// NewResolver preloads every (scheme) book alias and every canonical verse.
func NewResolver(db *sql.DB, scheme string) (*Resolver, error) {
	r := &Resolver{scheme: scheme, books: map[string]int64{}, verses: map[verseKey]int64{}}

	bookRows, err := db.Query(`SELECT value, book_id FROM book_names WHERE scheme = ?`, scheme)
	if err != nil {
		return nil, fmt.Errorf("load book_names(%s): %w", scheme, err)
	}
	defer bookRows.Close()
	for bookRows.Next() {
		var value string
		var id int64
		if err := bookRows.Scan(&value, &id); err != nil {
			return nil, fmt.Errorf("scan book_name: %w", err)
		}
		r.books[value] = id
	}
	if err := bookRows.Err(); err != nil {
		return nil, err
	}

	verseRows, err := db.Query(`SELECT id, book_id, chapter, verse FROM verses`)
	if err != nil {
		return nil, fmt.Errorf("load verses: %w", err)
	}
	defer verseRows.Close()
	for verseRows.Next() {
		var id, bookID int64
		var ch, v int
		if err := verseRows.Scan(&id, &bookID, &ch, &v); err != nil {
			return nil, fmt.Errorf("scan verse: %w", err)
		}
		r.verses[verseKey{bookID, ch, v}] = id
	}
	return r, verseRows.Err()
}

// Resolve maps a dotted reference (Book.Chapter.Verse, book in the resolver's
// scheme) to its canonical verses.id, wrapping ErrUnknownVerse when the book or
// verse is not registered.
func (r *Resolver) Resolve(ref string) (int64, error) {
	book, ch, v, err := splitRef(ref)
	if err != nil {
		return 0, err
	}
	bookID, ok := r.books[book]
	if !ok {
		return 0, fmt.Errorf("%w: %s (%s)", ErrUnknownVerse, ref, r.scheme)
	}
	id, ok := r.verses[verseKey{bookID, ch, v}]
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrUnknownVerse, ref)
	}
	return id, nil
}

// splitRef parses "Book.Chapter.Verse"; the book token may contain digits
// (1Cor, 2Sam) but never a dot, so splitting on "." is unambiguous.
func splitRef(ref string) (book string, chapter, verse int, err error) {
	parts := strings.Split(ref, ".")
	if len(parts) != 3 {
		return "", 0, 0, fmt.Errorf("%w: malformed ref %q", ErrUnknownVerse, ref)
	}
	chapter, err = strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, 0, fmt.Errorf("%w: bad chapter in %q", ErrUnknownVerse, ref)
	}
	verse, err = strconv.Atoi(parts[2])
	if err != nil {
		return "", 0, 0, fmt.Errorf("%w: bad verse in %q", ErrUnknownVerse, ref)
	}
	return parts[0], chapter, verse, nil
}
