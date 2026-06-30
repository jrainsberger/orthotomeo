// Package webtext loads the WEB (World English Bible) USFM edition into
// verse_text, stripping USFM markup to verbatim prose. Ticket 8.
//
// USFM is markup, not a clean tabular format, so this is more than a tab
// split: footnotes/cross-references are dropped wholesale, word wrappers
// (\w, the words-of-Jesus variant \+w) are stripped to the bare word, and
// front-matter/heading markers (\id, \s1, \d, ...) carry no verse text and
// are skipped. A verse's content can span several physical lines (poetry
// uses \q1/\q2/\q3 line breaks with no new \v), so text is accumulated
// until the next \c or \v, not read off a single line.
package webtext

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/verses"
)

// sourceCode is the sources.code this loader attributes its rows to.
const sourceCode = "WEB"

// nonContentMarkers open lines that carry no canonical verse text - front
// matter, headings, descriptive titles (e.g. Psalm superscriptions). Their
// lines are skipped wholesale, never appended to a verse.
var nonContentMarkers = map[string]bool{
	"id": true, "ide": true, "h": true,
	"toc1": true, "toc2": true, "toc3": true,
	"mt1": true, "mt2": true, "mt3": true,
	"cl": true, "ms1": true, "ms2": true, "is1": true,
	"ip": true, "ili": true, "rem": true, "sp": true,
	"d": true, "s1": true, "s2": true, "cp": true, "periph": true,
}

var (
	// Footnotes and cross-references are dropped wholesale, including any
	// markup nested inside them (e.g. \+wh transliteration glosses).
	footnoteRe = regexp.MustCompile(`(?s)\\f\b.*?\\f\*`)
	xrefRe     = regexp.MustCompile(`(?s)\\x\b.*?\\x\*`)
	// \w word|strong="..."\w* and the words-of-Jesus variant \+w ...\+w*
	// both reduce to the bare word.
	wordRe = regexp.MustCompile(`\\\+?w ([^|\\]*)\|[^\\]*?\\\+?w\*`)
	// Any other inline marker (\wj, \wj*, \qs, \qs*, \bk, \bk*, ...) has no
	// attribute payload - delete the token, keep its enclosed text.
	markerRe = regexp.MustCompile(`\\\+?[A-Za-z][A-Za-z0-9]*\*?`)
	lineRe   = regexp.MustCompile(`^\\([A-Za-z][A-Za-z0-9]*)\s*(.*)$`)
	wsRe     = regexp.MustCompile(`\s+`)
)

// Load reads one WEB USFM book file. If the file's \id book code is not one
// of the 66 canonical USFM codes (front matter, glossary, deuterocanon -
// v1 scope is 66 books only), it is skipped: loaded is false and that is
// not an error. Verses that fail to resolve against the canonical KJV
// spine are counted in skipped, not a load failure - WEB's own front
// matter documents small versification divergences from other editions
// (invariant #4: reconcile at read time, never assume 1:1).
func Load(db *sql.DB, r io.Reader) (code string, inserted, skipped int, loaded bool, err error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return "", 0, 0, false, fmt.Errorf("read: %w", err)
	}
	content := xrefRe.ReplaceAllString(footnoteRe.ReplaceAllString(string(raw), ""), "")

	code = bookCode(content)
	if code == "" {
		return "", 0, 0, false, fmt.Errorf("no \\id line found")
	}

	bookID, err := books.Resolve(db, "usfm", code)
	if err != nil {
		if errors.Is(err, books.ErrUnknownBook) {
			return code, 0, 0, false, nil
		}
		return code, 0, 0, false, err
	}
	var bookName string
	if err := db.QueryRow(`SELECT full_name FROM books WHERE id = ?`, bookID).Scan(&bookName); err != nil {
		return code, 0, 0, false, fmt.Errorf("book name for %s: %w", code, err)
	}

	var sourceID int64
	if err := db.QueryRow(`SELECT id FROM sources WHERE code = ?`, sourceCode).Scan(&sourceID); err != nil {
		return code, 0, 0, false, fmt.Errorf("source %q not seeded: %w", sourceCode, err)
	}

	res, err := verses.NewResolver(db, "usfm", verses.Canonical)
	if err != nil {
		return code, 0, 0, false, err
	}

	tx, err := db.Begin()
	if err != nil {
		return code, 0, 0, false, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO verse_text (verse_id, source_id, native_ref, text)
		VALUES (?, ?, ?, ?)`)
	if err != nil {
		return code, 0, 0, false, fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	chapter, verse := 0, 0
	var buf []string

	flush := func() error {
		if verse == 0 || len(buf) == 0 {
			buf = nil
			return nil
		}
		text := wsRe.ReplaceAllString(strings.Join(buf, " "), " ")
		text = strings.TrimSpace(text)
		buf = nil
		if text == "" {
			return nil
		}
		verseID, rerr := res.Resolve(fmt.Sprintf("%s.%d.%d", code, chapter, verse))
		if rerr != nil {
			skipped++
			return nil
		}
		nativeRef := fmt.Sprintf("%s %d:%d", bookName, chapter, verse)
		if _, err := stmt.Exec(verseID, sourceID, nativeRef, text); err != nil {
			return fmt.Errorf("insert %s: %w", nativeRef, err)
		}
		inserted++
		return nil
	}

	sc := bufio.NewScanner(strings.NewReader(content))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), " \t")
		if line == "" {
			continue
		}
		m := lineRe.FindStringSubmatch(line)
		if m == nil {
			continue // defensive: a line not starting with a marker
		}
		marker, rest := m[1], m[2]

		switch {
		case marker == "c":
			if ferr := flush(); ferr != nil {
				return code, inserted, skipped, true, ferr
			}
			n, perr := strconv.Atoi(strings.TrimSpace(rest))
			if perr != nil {
				return code, inserted, skipped, true, fmt.Errorf("bad chapter %q: %w", rest, perr)
			}
			chapter, verse = n, 0
		case marker == "v":
			if ferr := flush(); ferr != nil {
				return code, inserted, skipped, true, ferr
			}
			numStr, text, _ := strings.Cut(rest, " ")
			n, perr := strconv.Atoi(strings.TrimSpace(numStr))
			if perr != nil {
				return code, inserted, skipped, true, fmt.Errorf("bad verse %q: %w", rest, perr)
			}
			verse = n
			buf = append(buf, stripInline(text))
		case nonContentMarkers[marker]:
			// front matter / heading / title - no verse text, drop the line.
		default:
			// paragraph or poetry marker (\p, \q1, \q2, ...): formatting
			// reset only; any trailing text on the line continues the
			// current verse.
			if verse != 0 && rest != "" {
				buf = append(buf, stripInline(rest))
			}
		}
	}
	if err := sc.Err(); err != nil {
		return code, inserted, skipped, true, fmt.Errorf("scan: %w", err)
	}
	if ferr := flush(); ferr != nil {
		return code, inserted, skipped, true, ferr
	}

	if err := tx.Commit(); err != nil {
		return code, inserted, skipped, true, fmt.Errorf("commit: %w", err)
	}
	return code, inserted, skipped, true, nil
}

// stripInline reduces one line's trailing content to plain text: \w/\+w
// wrappers become their bare word, every other inline marker token is
// deleted while its enclosed text is kept.
func stripInline(s string) string {
	s = wordRe.ReplaceAllString(s, "$1")
	s = markerRe.ReplaceAllString(s, "")
	return s
}

// bookCode extracts the USFM book code from the file's \id line.
func bookCode(content string) string {
	for _, line := range strings.Split(content, "\n") {
		m := lineRe.FindStringSubmatch(strings.TrimRight(line, " \t\r"))
		if m != nil && m[1] == "id" {
			code, _, _ := strings.Cut(m[2], " ")
			return code
		}
	}
	return ""
}
