// Package swetewords loads the Swete LXX (1909-1930) Greek surface-form
// word stream into words, under its own versification ("lxx-swete") per
// the T4b decision - never forced onto the canonical KJV spine at load
// time. Swete carries surface text only: lemma, dstrong, and morph_code
// are NULL for every row (Swete has no lexical tagging). This is a
// parallel stream from the OSS lemma data (T13), NOT merged with it -
// joining them is alignment work (T22), not load work. Ticket 12.
package swetewords

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/verses"
)

// sourceCode is the sources.code this loader attributes its rows to.
const sourceCode = "Swete"

// Versification is this edition's own verse-identity tag (never canonical).
const Versification = "lxx-swete"

// bookAlias maps a Swete versification-file book code to the canonical
// dotted code when the two abbreviations differ (eg "Eze" vs our "Ezk").
// Codes not listed here and not already a canonical dotted code are
// deuterocanon/extra-biblical (1 Enoch, Maccabees, Tobit, ...) - out of
// v1's 66-book scope, skipped rather than guessed at.
var bookAlias = map[string]string{
	"Eze": "Ezk",
	"Joe": "Jol",
	"Nah": "Nam",
	"Sol": "Sng",
}

// verseStart is one row of the versification file: the word index where a
// verse begins.
type verseStart struct {
	index          int
	book           string
	chapter, verse int
}

// Load reads the versification CSV (word-index -> ref) and the
// with-punctuation word CSV (word-index -> Greek surface) and inserts every
// word of every canonical-66 verse into words. Deuterocanonical books are
// skipped (counted), not an error. Returns inserted word count and skipped
// verse count (deuterocanon). Runs in one transaction so a partial load
// never lands.
func Load(db *sql.DB, versificationCSV, wordsCSV io.Reader) (inserted, skippedVerses int, err error) {
	starts, err := readVersification(versificationCSV)
	if err != nil {
		return 0, 0, err
	}
	words, err := readWords(wordsCSV)
	if err != nil {
		return 0, 0, err
	}

	var sourceID int64
	if err := db.QueryRow(`SELECT id FROM sources WHERE code = ?`, sourceCode).Scan(&sourceID); err != nil {
		return 0, 0, fmt.Errorf("source %q not seeded: %w", sourceCode, err)
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, 0, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO words (verse_id, source_id, word_no, surface, lemma, dstrong, morph_code, attestation, editions, source_locator)
		VALUES (?, ?, ?, ?, NULL, NULL, NULL, '', '', ?)`)
	if err != nil {
		return 0, 0, fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for i, vs := range starts {
		end := len(words)
		if i+1 < len(starts) {
			end = starts[i+1].index - 1
		}
		if end < vs.index || vs.index < 1 || end > len(words) {
			return 0, 0, fmt.Errorf("bad word range for %s.%d:%d: [%d,%d]", vs.book, vs.chapter, vs.verse, vs.index, end)
		}

		code := vs.book
		if alias, ok := bookAlias[code]; ok {
			code = alias
		}
		bookID, berr := books.Resolve(tx, "dotted", code)
		if berr != nil {
			if errors.Is(berr, books.ErrUnknownBook) {
				skippedVerses++
				continue
			}
			return 0, 0, berr
		}

		verseID, verr := verses.GetOrCreateVerse(tx, Versification, bookID, vs.chapter, vs.verse)
		if verr != nil {
			return 0, 0, verr
		}

		for wordNo, idx := 1, vs.index; idx <= end; wordNo, idx = wordNo+1, idx+1 {
			locator := fmt.Sprintf("%s.%d:%d#%02d", vs.book, vs.chapter, vs.verse, wordNo)
			if _, err := stmt.Exec(verseID, sourceID, wordNo, words[idx-1], locator); err != nil {
				return 0, 0, fmt.Errorf("insert %s: %w", locator, err)
			}
			inserted++
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("commit: %w", err)
	}
	return inserted, skippedVerses, nil
}

// readVersification parses "index\tBook.C:V" rows, in file order (which is
// index order - confirmed ascending in the source).
func readVersification(r io.Reader) ([]verseStart, error) {
	var out []verseStart
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		idxStr, ref, ok := strings.Cut(line, "\t")
		if !ok {
			return nil, fmt.Errorf("malformed versification row %q", line)
		}
		idx, err := strconv.Atoi(idxStr)
		if err != nil {
			return nil, fmt.Errorf("bad index in %q: %w", line, err)
		}
		book, chapter, verse, err := parseRef(ref)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", line, err)
		}
		out = append(out, verseStart{idx, book, chapter, verse})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan versification: %w", err)
	}
	return out, nil
}

// parseRef parses "Book.C:V", eg "Gen.1:1".
func parseRef(ref string) (book string, chapter, verse int, err error) {
	book, rest, ok := strings.Cut(ref, ".")
	if !ok {
		return "", 0, 0, fmt.Errorf("malformed ref %q", ref)
	}
	chStr, vStr, ok := strings.Cut(rest, ":")
	if !ok {
		return "", 0, 0, fmt.Errorf("malformed ref %q", ref)
	}
	chapter, err = strconv.Atoi(chStr)
	if err != nil {
		return "", 0, 0, fmt.Errorf("bad chapter in %q: %w", ref, err)
	}
	verse, err = strconv.Atoi(vStr)
	if err != nil {
		return "", 0, 0, fmt.Errorf("bad verse in %q: %w", ref, err)
	}
	return book, chapter, verse, nil
}

// readWords parses "index\tsurface" rows into a 0-indexed slice where
// words[i-1] is the surface form at word index i - safe because the source
// file's index column is confirmed sequential with no gaps.
func readWords(r io.Reader) ([]string, error) {
	var out []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		_, word, ok := strings.Cut(line, "\t")
		if !ok {
			return nil, fmt.Errorf("malformed word row %q", line)
		}
		out = append(out, word)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan words: %w", err)
	}
	return out, nil
}
