// Package brentontext loads the Brenton LXX (Septuagint in English) HTML
// edition into verse_text, one verse_id per book/chapter/verse in Brenton's
// OWN versification ("lxx-brenton") - never forced onto the canonical KJV
// spine at load time (invariant #4; see docs/PLAN.md's T4b decision). Every
// verse extracted gets a row; there is no "unresolvable verse" here because
// verses.GetOrCreateVerse always succeeds. Ticket 9.
package brentontext

import (
	"database/sql"
	"errors"
	"fmt"
	"html"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/verses"
)

// sourceCode is the sources.code this loader attributes its rows to.
const sourceCode = "Brenton"

// Versification is this edition's own verse-identity tag (never canonical).
const Versification = "lxx-brenton"

// fileBookAlias maps a Brenton file's book code to the canonical USFM code
// when the two differ. Brenton ships Daniel and Esther only as their
// Greek-expanded editions (DAG/ESG), with the deuterocanonical additions
// integrated directly into the text rather than appended separately.
var fileBookAlias = map[string]string{
	"DAG": "DAN",
	"ESG": "EST",
}

// skipBooks are Brenton file codes with no clean 1:1 canonical book
// identity, intentionally left unloaded rather than hand-resolved.
// EZR.htm is the LXX's combined Ezra+Nehemiah ("2 Esdras" in TVTMS
// terms) - roughly its first third corresponds to canonical Ezra and the
// rest to canonical Nehemiah, but the exact split is a book-identity
// question for the T4b aligner, not something to guess at in this loader.
// Brenton's separate NEH.htm files (standalone Nehemiah) ARE loaded
// normally under canonical NEH.
var skipBooks = map[string]bool{
	"EZR": true,
}

// fileNameRe extracts (book code, chapter) from a Brenton chapter filename,
// e.g. "GEN01.htm" -> ("GEN", "01"), "1CH05.htm" -> ("1CH", "05"). The code
// group is lazy so it stops at the first run of trailing digits rather than
// greedily swallowing them (book codes never end in a digit in this corpus).
var fileNameRe = regexp.MustCompile(`^([A-Z0-9]+?)(\d+)\.htm$`)

var (
	// The verse number lives in the span's own displayed text (group 1),
	// NOT its id="VN" attribute: where Brenton prints a lettered doublet
	// (e.g. 1Ki.2's "35a"/"35b" Miscellanies block), the id attribute is
	// just a sequential HTML anchor and does not match the printed number.
	verseSpanRe    = regexp.MustCompile(`<span class="verse" id="V\d+">([^<]*)</span>`)
	verseLabelRe   = regexp.MustCompile(`^(\d+)([a-z]?)`)
	footnoteDivRe  = regexp.MustCompile(`<div class="footnote">`)
	trailingNavRe  = regexp.MustCompile(`(?s)<ul class='tnav'>.*?</ul>`)
	chapterLabelRe = regexp.MustCompile(`(?s)<div class='chapterlabel'[^>]*>.*?</div>`)
	notemarkRe     = regexp.MustCompile(`(?s)<a[^>]*class="notemark"[^>]*>.*?</a>`)
	tagRe          = regexp.MustCompile(`<[^>]+>`)
	wsRe           = regexp.MustCompile(`\s+`)
)

// Load reads one Brenton chapter file (book/chapter derived from filename,
// e.g. "GEN01.htm") and inserts every verse it contains into verse_text
// under versification Versification. If the file's book code has no
// canonical 66-book identity (front matter, index/TOC pages, deuterocanon,
// or an explicitly skipped book - see skipBooks), loaded is false and that
// is not an error. Runs in one transaction so a partial chapter never lands.
func Load(db *sql.DB, r io.Reader, filename string) (code string, chapter, inserted int, loaded bool, err error) {
	m := fileNameRe.FindStringSubmatch(filename)
	if m == nil {
		// Not a chapter file: book-index pages (GEN.htm), front matter
		// (index.htm, copr.htm, copyright.htm) - not an error, just nothing
		// to load here.
		return "", 0, 0, false, nil
	}
	code = m[1]
	chapter, err = strconv.Atoi(m[2])
	if err != nil {
		return code, 0, 0, false, fmt.Errorf("bad chapter in %q: %w", filename, err)
	}

	if skipBooks[code] {
		return code, chapter, 0, false, nil
	}
	resolvedCode := code
	if alias, ok := fileBookAlias[code]; ok {
		resolvedCode = alias
	}

	bookID, err := books.Resolve(db, "usfm", resolvedCode)
	if err != nil {
		if errors.Is(err, books.ErrUnknownBook) {
			return code, chapter, 0, false, nil
		}
		return code, chapter, 0, false, err
	}
	var bookName string
	if err := db.QueryRow(`SELECT full_name FROM books WHERE id = ?`, bookID).Scan(&bookName); err != nil {
		return code, chapter, 0, false, fmt.Errorf("book name for %s: %w", resolvedCode, err)
	}

	var sourceID int64
	if err := db.QueryRow(`SELECT id FROM sources WHERE code = ?`, sourceCode).Scan(&sourceID); err != nil {
		return code, chapter, 0, false, fmt.Errorf("source %q not seeded: %w", sourceCode, err)
	}

	raw, err := io.ReadAll(r)
	if err != nil {
		return code, chapter, 0, false, fmt.Errorf("read: %w", err)
	}
	content := mainContent(string(raw))
	if content == "" {
		return code, chapter, 0, false, nil // index/TOC page: no verse spans at all
	}

	tx, err := db.Begin()
	if err != nil {
		return code, chapter, 0, false, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO verse_text (verse_id, source_id, native_ref, text)
		VALUES (?, ?, ?, ?)`)
	if err != nil {
		return code, chapter, 0, false, fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for _, vt := range extractVerses(content) {
		verseID, err := verses.GetOrCreateVerse(tx, Versification, bookID, chapter, vt.num)
		if err != nil {
			return code, chapter, inserted, true, err
		}
		nativeRef := fmt.Sprintf("%s %d:%d", bookName, chapter, vt.num)
		if _, err := stmt.Exec(verseID, sourceID, nativeRef, vt.text); err != nil {
			return code, chapter, inserted, true, fmt.Errorf("insert %s: %w", nativeRef, err)
		}
		inserted++
	}

	if err := tx.Commit(); err != nil {
		return code, chapter, inserted, true, fmt.Errorf("commit: %w", err)
	}
	return code, chapter, inserted, true, nil
}

// mainContent slices out the verse-bearing content of a chapter file: from
// just after the opening `<div class="main">` up to the trailing
// `<div class="footnote">` block (or end of string if the file has none),
// with the trailing nav list and the chapter-label div removed. Returns ""
// if the file has no verse spans at all (index/TOC pages like PSA000.htm,
// or the bare book-index "GEN.htm" pages).
func mainContent(raw string) string {
	mainIdx := strings.Index(raw, `<div class="main">`)
	if mainIdx == -1 {
		return ""
	}
	body := raw[mainIdx:]
	if loc := footnoteDivRe.FindStringIndex(body); loc != nil {
		body = body[:loc[0]]
	}
	body = trailingNavRe.ReplaceAllString(body, "")
	body = chapterLabelRe.ReplaceAllString(body, "")
	if !verseSpanRe.MatchString(body) {
		return ""
	}
	return body
}

// extractVerses splits cleaned chapter content into ordered (verse, text)
// pairs, stripping inline footnote markers, remaining HTML tags, and
// decoding entities. Lettered doublets sharing a base verse number (Brenton
// prints some passages, e.g. 1Ki.2's "Miscellanies" block, as "35a"/"35b")
// are concatenated in document order into one row for that verse - this
// loader has no sub-verse addressing, so the full text is kept, not split.
func extractVerses(content string) []verseText {
	content = notemarkRe.ReplaceAllString(content, "")

	locs := verseSpanRe.FindAllStringSubmatchIndex(content, -1)
	order := make([]int, 0, len(locs))
	byNum := map[int]*strings.Builder{}
	for i, loc := range locs {
		label := content[loc[2]:loc[3]]
		lm := verseLabelRe.FindStringSubmatch(strings.TrimSpace(label))
		if lm == nil {
			continue // malformed label, e.g. a chapter-label leftover
		}
		num, _ := strconv.Atoi(lm[1])

		start := loc[1]
		end := len(content)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		segment := content[start:end]
		segment = tagRe.ReplaceAllString(segment, "")
		segment = html.UnescapeString(segment)
		segment = strings.TrimSpace(wsRe.ReplaceAllString(segment, " "))
		if segment == "" {
			continue
		}

		b, seen := byNum[num]
		if !seen {
			b = &strings.Builder{}
			byNum[num] = b
			order = append(order, num)
		} else {
			b.WriteByte(' ')
		}
		b.WriteString(segment)
	}

	out := make([]verseText, 0, len(order))
	for _, num := range order {
		out = append(out, verseText{num, byNum[num].String()})
	}
	return out
}

type verseText struct {
	num  int
	text string
}
