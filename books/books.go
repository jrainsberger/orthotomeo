// Package books holds the canonical book registry and the per-scheme name
// aliases every verse/text load resolves through. The registry is checked-in
// data (books.json); resolution is by (scheme, value) because naming schemes
// (OSIS, USFM, dotted, English) are shared across sources. Ticket 2.
package books

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrUnknownBook means a (scheme, value) pair did not resolve to any book.
// It is a named, branchable failure so loaders can distinguish "this edition
// uses a book name we have not registered" from a real I/O error.
var ErrUnknownBook = errors.New("unknown book name")

// Book is one registry row plus its aliases. Field tags mirror books.json.
type Book struct {
	Order   int    `json:"order"`   // 1..66, canonical ordering
	Code    string `json:"code"`    // canonical USFM code (also books.code), e.g. MRK
	OSIS    string `json:"osis"`    // OSIS abbreviation, e.g. Mark (used by the xref file)
	Dotted  string `json:"dotted"`  // STEPBible dotted-ref code, e.g. Mrk
	Name    string `json:"name"`    // English name (KJV/ASV form), e.g. Mark
	Section string `json:"section"` // ot | nt
	Canon   string `json:"canon"`   // protestant
}

//go:embed books.json
var registryJSON []byte

// Registry returns the declared books, decoded from books.json.
func Registry() ([]Book, error) {
	var b []Book
	if err := json.Unmarshal(registryJSON, &b); err != nil {
		return nil, fmt.Errorf("decode books.json: %w", err)
	}
	return b, nil
}

// Seed inserts the registry into books and its aliases into book_names, in a
// single transaction. Each book contributes one alias per scheme
// (usfm, osis, dotted, name-en) so every source's native book name resolves
// through one path. Returns (#books, #aliases).
func Seed(db *sql.DB) (books, aliases int, err error) {
	reg, err := Registry()
	if err != nil {
		return 0, 0, err
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, 0, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	insBook, err := tx.Prepare(`
		INSERT INTO books (code, full_name, section, canon, sort_order)
		VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, 0, fmt.Errorf("prepare books: %w", err)
	}
	defer insBook.Close()

	insName, err := tx.Prepare(`
		INSERT INTO book_names (book_id, scheme, value) VALUES (?, ?, ?)`)
	if err != nil {
		return 0, 0, fmt.Errorf("prepare book_names: %w", err)
	}
	defer insName.Close()

	for _, b := range reg {
		res, err := insBook.Exec(b.Code, b.Name, b.Section, b.Canon, b.Order)
		if err != nil {
			return 0, 0, fmt.Errorf("insert book %q: %w", b.Code, err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return 0, 0, fmt.Errorf("book id %q: %w", b.Code, err)
		}
		for scheme, value := range map[string]string{
			"usfm":    b.Code,
			"osis":    b.OSIS,
			"dotted":  b.Dotted,
			"name-en": b.Name,
		} {
			if _, err := insName.Exec(id, scheme, value); err != nil {
				return 0, 0, fmt.Errorf("insert alias %s/%s: %w", scheme, value, err)
			}
			aliases++
		}
		books++
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("commit: %w", err)
	}
	return books, aliases, nil
}

// Querier is the subset of *sql.DB and *sql.Tx that Resolve needs, so it can
// run standalone or inside a caller's transaction. The pool is single-connection
// (see store.Open), so querying the *sql.DB while a transaction is open would
// deadlock; callers inside a transaction must pass the *sql.Tx.
type Querier interface {
	QueryRow(query string, args ...any) *sql.Row
}

// Resolve returns the books.id for a native book name under the given scheme
// (usfm | osis | dotted | name-en). It wraps ErrUnknownBook when the pair is
// not registered, so callers can branch on a missing book vs. a query error.
func Resolve(q Querier, scheme, value string) (int64, error) {
	var id int64
	err := q.QueryRow(
		`SELECT book_id FROM book_names WHERE scheme = ? AND value = ?`,
		scheme, value,
	).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return 0, fmt.Errorf("%w: %s/%q", ErrUnknownBook, scheme, value)
	case err != nil:
		return 0, fmt.Errorf("resolve %s/%q: %w", scheme, value, err)
	}
	return id, nil
}

// ResolveCode turns free-form book input a person would actually type - a
// USFM code or the full English name, in any case - into the canonical USFM
// code retriever.Ref.Book requires. It tries the usfm scheme uppercased
// first (LUK, luk, Luk), then falls back to a case-insensitive match on
// name-en (Luke, LUKE, luke). This is the one place that normalization
// happens; every transport (CLI, HTTP, MCP) calls it before constructing a
// Ref instead of each re-deriving its own book-matching rules.
func ResolveCode(q Querier, raw string) (string, error) {
	var code string
	err := q.QueryRow(`
		SELECT b.code FROM book_names n JOIN books b ON b.id = n.book_id
		WHERE (n.scheme = 'usfm' AND n.value = ?)
		   OR (n.scheme = 'name-en' AND n.value = ? COLLATE NOCASE)
		LIMIT 1`,
		strings.ToUpper(raw), raw,
	).Scan(&code)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", fmt.Errorf("%w: %q", ErrUnknownBook, raw)
	case err != nil:
		return "", fmt.Errorf("resolve book %q: %w", raw, err)
	}
	return code, nil
}
