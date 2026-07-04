// Package tahot loads the STEPBible Translators Amalgamated Hebrew Old
// Testament (the four TAHOT TSVs) into words, one row per tagged word
// instance, alongside T10's TAGNT rows in the same table. Ticket 11.
//
// Hebrew words routinely carry an attached prefix morpheme (preposition,
// article, conjunction - "in", "the", "and") joined to the root by "/" in
// the source columns, e.g. dStrongs "H9009/{H7225G}" for "the/beginning".
// The braces mark which "/"-segment is the root (the lexical entry); the
// prefix has its own Strong's number but no independent lemma. Per
// invariant #5 ("Hebrew may match by root"), this loader stores the
// BRACED segment as dstrong/lemma, not the prefix - confirmed by direct
// audit that every dStrongs/Grammar/Expanded-tags "/"-segment count and
// braced-segment index line up 1:1 across all 283,734 rows in the corpus.
package tahot

import (
	"bufio"
	"database/sql"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/jrainsberger/orthotomeo/lexnorm"
	"github.com/jrainsberger/orthotomeo/verses"
)

// sourceCode is the sources.code this loader attributes its rows to.
const sourceCode = "TAHOT"

// refRe matches a data row's ref field: Book.Chapter.Verse#WordNo=Type,
// e.g. "Gen.1.1#01=L", "Isa.44.24#16=Q(K)". As with TAGNT, this is the only
// reliable way to find data rows - the file repeats its own column-header
// row and a Hebrew/translation/grammar preview block before every verse.
//
// The verse field carries an optional "(Chapter.Verse)" suffix wherever the
// file's own Hebrew-native verse count differs from the English/NRSV verse
// this loader resolves against - e.g. "Psa.9.1(9.2)#01=L" (confirmed by the
// file's own header, line 32: "Ref: Eng (+Heb) ... Bible reference in
// English Bibles ... with Heb refs in brackets when they are different").
// The English number outside the parens is always what's used - it's the
// number this loader already resolved against, unchanged; the parenthetical
// is discarded, not consumed. Before this was tolerated, refRe simply failed
// to match any such row, silently dropping it before it reached the
// resolver or any skip/untagged counter (confirmed: 21,944 rows across all
// four TAHOT files, concentrated in Psalms - Hebrew numbers a psalm's
// superscription as its own verse 1, shifting every later verse by +1
// relative to English, so essentially every verse of an entitled psalm
// carried the annotation). A verse field of "0(N.1)" - the title itself,
// which English versification doesn't number separately (T11's package doc,
// line 33: "Psalm Titles (v.0)") - still correctly fails to resolve against
// the canonical spine (no verse 0 exists there) and is counted as
// skippedVerse, not silently lost.
var refRe = regexp.MustCompile(`^([A-Za-z0-9]+)\.(\d+)\.(\d+)(?:\(\d+\.\d+\))?#(\d+)=(\S+)$`)

// braceRe extracts the content of a "{...}" span - the root/lexical
// segment, as opposed to an unbraced prefix segment. Matches even when the
// source has trailing punctuation-marker junk after the closing brace
// (e.g. a verse-end mark joined with "\"), since it only captures up to
// the first "}".
var braceRe = regexp.MustCompile(`\{([^}]*)\}`)

// Load reads one TAHOT TSV and inserts every data row into words. Verses
// that fail to resolve against the canonical spine are counted as skipped,
// not a load failure (invariant #4). A small number of rows (Qere readings
// with no corresponding Ketiv tagging - confirmed in the source, not a
// parsing gap) have no braced segment at all; dstrong/morph_code/lemma are
// SQL NULL for those rather than guessed at, counted as untagged. Ketiv/
// Qere and other manuscript-variant markers are preserved verbatim in
// attestation (e.g. "Q(K)"), never collapsed to a bare letter. Runs in one
// transaction so a partial load never lands.
func Load(db *sql.DB, r io.Reader) (inserted, skippedVerse, untagged int, err error) {
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
		INSERT INTO words (verse_id, source_id, word_no, surface, lemma, dstrong, morph_code, attestation, editions, source_locator, translit)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		fields := strings.Split(sc.Text(), "\t")
		if len(fields) < 12 {
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

		surface := fields[1]
		dstrong, morphCode, lemma, tagged := rootFields(fields[4], fields[5], fields[11])
		if !tagged {
			untagged++
		}

		if _, err := stmt.Exec(verseID, sourceID, wordNum, surface, lemma, dstrong, morphCode, attestation, "", fields[0], nullIfEmpty(fields[2])); err != nil {
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
	return inserted, skippedVerse, untagged, nil
}

// nullIfEmpty turns "" into SQL NULL rather than storing an empty string -
// "no transliteration" should read the same as "the column doesn't apply
// here" (Swete/OSS-LXX-lemma rows, which never call this at all), not as a
// distinct empty-vs-absent state.
func nullIfEmpty(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// rootFields locates the root (braced) "/"-segment shared by the dStrongs,
// Grammar, and Expanded-Strong-tags columns and extracts dstrong, the
// matching morph code, and the lemma out of it. A word with no braced
// segment anywhere (a handful of untagged Qere readings) returns
// tagged=false and all three values unset.
func rootFields(dstrongsField, grammarField, expandedField string) (dstrong, morphCode, lemma sql.NullString, tagged bool) {
	dParts := strings.Split(dstrongsField, "/")
	idx := -1
	for i, p := range dParts {
		if strings.Contains(p, "{") {
			idx = i
			break
		}
	}
	if idx == -1 {
		return sql.NullString{}, sql.NullString{}, sql.NullString{}, false
	}

	if m := braceRe.FindStringSubmatch(dParts[idx]); m != nil {
		dstrong = sql.NullString{String: m[1], Valid: true}
	}

	gParts := strings.Split(grammarField, "/")
	if idx < len(gParts) {
		morphCode = sql.NullString{String: withLanguagePrefix(gParts, idx), Valid: true}
	}

	eParts := strings.Split(expandedField, "/")
	if idx < len(eParts) {
		if m := braceRe.FindStringSubmatch(eParts[idx]); m != nil {
			// m[1] is "StrongNum=Lemma" or "StrongNum=Lemma=Gloss..."; the
			// lemma is the second "="-delimited field either way.
			if _, rest, ok := strings.Cut(m[1], "="); ok {
				lemmaForm, _, _ := strings.Cut(rest, "=")
				lemma = sql.NullString{String: lexnorm.NFC(lemmaForm), Valid: true}
			}
		}
	}

	return dstrong, morphCode, lemma, dstrong.Valid
}

// withLanguagePrefix returns gParts[idx], re-attaching the H (Hebrew) or A
// (Aramaic) language marker when idx isn't the first "/"-segment. TAHOT's
// Grammar column carries that marker exactly once, on segment 0 - confirmed
// by direct corpus audit of all four TAHOT files: a later ("/"-index > 0)
// segment NEVER itself starts with 'H' (0 occurrences across 152,022 later
// segments), and every apparent "A..." later segment is the POS letter for
// Adjective (e.g. "Acmsc", "Aampa"), not a re-stated Aramaic marker - so the
// prefix must always be prepended unconditionally, never conditionally. The
// T6 TEHMC morph_codes table's own codes are always prefixed (e.g.
// "HNcfsa"), so an un-prefixed later segment like "Ncfsa" would silently
// fail to resolve there without this.
func withLanguagePrefix(gParts []string, idx int) string {
	code := gParts[idx]
	if idx == 0 || code == "" {
		return code
	}
	prefix := gParts[0]
	if prefix == "" {
		return code
	}
	lang := prefix[0]
	if lang != 'H' && lang != 'A' {
		return code
	}
	return string(lang) + code
}
