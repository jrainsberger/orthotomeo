// Package tagnt loads the STEPBible Translators Amalgamated Greek New
// Testament (the two TAGNT TSVs) into words, one row per tagged word
// instance. This is the project's complete-or-fail foundation (invariant
// #3): every data row in the source files becomes a words row. Ticket 10.
package tagnt

import (
	"bufio"
	"database/sql"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/jrainsberger/orthotomeo/verses"
)

// sourceCode is the sources.code this loader attributes its rows to.
const sourceCode = "TAGNT"

// refRe matches a data row's ref field: Book.Chapter.Verse#WordNo=Type,
// e.g. "Mat.1.1#01=NKO", "Act.8.37#01=K". This is the only reliable way to
// find data rows - the file repeats its own column-header row and a
// preview block (Greek/English/grammar synopsis lines, "#"-prefixed)
// before every verse, not just once at the top.
//
// A small number of rows (26 across both files: Act.13.39(13.38),
// Act.19.41(19.40), Rom.3.25(3.26), Mrk.12.15(12.14)) carry an optional
// "(Chapter.Verse)" suffix on the verse field, the same edition-versus-
// English-standard cross-reference convention T11's TAHOT loader tolerates
// (see tahot.go's refRe doc for the confirmed source). The number outside
// the parens is the English/standard verse this loader already resolves
// against; before this was tolerated, refRe simply failed to match these
// 26 rows and they were silently dropped, uncounted by any skip/insert
// counter.
var refRe = regexp.MustCompile(`^([A-Za-z0-9]+)\.(\d+)\.(\d+)(?:\(\d+\.\d+\))?#(\d+)=(\S+)$`)

// Load reads one TAGNT TSV (Mat-Jhn or Act-Rev) and inserts every data row
// into words. Verses that fail to resolve against the canonical spine are
// counted as skipped, not a load failure (invariant #4); a compound-tagged
// word (dStrong/lemma containing " + ", one surface token spanning two
// Strong's numbers) gets a NULL dstrong/lemma rather than a guess - both
// are reported counts, not silent. Runs in one transaction so a partial
// load never lands.
func Load(db *sql.DB, r io.Reader) (inserted, skippedVerse, compound int, err error) {
	var sourceID int64
	if err := db.QueryRow(`SELECT id FROM sources WHERE code = ?`, sourceCode).Scan(&sourceID); err != nil {
		return 0, 0, 0, fmt.Errorf("source %q not seeded: %w", sourceCode, err)
	}

	res, err := verses.NewResolver(db, "dotted", verses.Canonical)
	if err != nil {
		return 0, 0, 0, err
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO words (verse_id, source_id, word_no, surface, lemma, dstrong, morph_code, attestation, editions, source_locator)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		fields := strings.Split(sc.Text(), "\t")
		if len(fields) < 6 {
			continue // header / preview / blank line, not a data row
		}
		m := refRe.FindStringSubmatch(fields[0])
		if m == nil {
			continue
		}
		book, chapter, verseNo, wordNo, attestation := m[1], m[2], m[3], m[4], m[5]

		verseID, rerr := res.Resolve(fmt.Sprintf("%s.%s.%s", book, chapter, verseNo))
		if rerr != nil {
			skippedVerse++
			continue
		}

		wordNum, perr := strconv.Atoi(wordNo)
		if perr != nil {
			return 0, 0, 0, fmt.Errorf("bad word number in %q: %w", fields[0], perr)
		}

		surface := surfaceWord(fields[1])
		dstrong, morphCode, lemma, isCompound := dstrongLemma(fields[3], fields[4])
		if isCompound {
			compound++
		}

		if _, err := stmt.Exec(verseID, sourceID, wordNum, surface, lemma, dstrong, morphCode, attestation, fields[5], fields[0]); err != nil {
			return 0, 0, 0, fmt.Errorf("insert %s: %w", fields[0], err)
		}
		inserted++
	}
	if err := sc.Err(); err != nil {
		return 0, 0, 0, fmt.Errorf("scan: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, 0, fmt.Errorf("commit: %w", err)
	}
	return inserted, skippedVerse, compound, nil
}

// surfaceWord strips the trailing " (transliteration)" from the Greek
// column, e.g. "Βίβλος (Biblos)" -> "Βίβλος".
func surfaceWord(field string) string {
	if i := strings.LastIndex(field, " ("); i >= 0 {
		return field[:i]
	}
	return field
}

// dstrongLemma splits the "dStrong=Grammar" and "Lemma=Gloss" columns. A
// compound-tagged word (one surface token spanning two Strong's numbers,
// e.g. μήποτε = "G3361=PRT-N + G4218=PRT" / "μήποτε=lest + πότε=when") has
// no single dStrong or lemma to store and gets SQL NULL for both, reported
// via isCompound rather than guessed at.
func dstrongLemma(dstrongGrammar, lemmaGloss string) (dstrong, morphCode, lemma sql.NullString, isCompound bool) {
	if strings.Contains(dstrongGrammar, " + ") || strings.Contains(lemmaGloss, " + ") {
		return sql.NullString{}, sql.NullString{}, sql.NullString{}, true
	}
	d, m, _ := strings.Cut(dstrongGrammar, "=")
	l, _, _ := strings.Cut(lemmaGloss, "=")
	return sql.NullString{String: d, Valid: true}, sql.NullString{String: m, Valid: true}, sql.NullString{String: l, Valid: true}, false
}
