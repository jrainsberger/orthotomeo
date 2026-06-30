// Package osswords loads the Open Scriptures LXX lemma index into words,
// under its own versification ("lxx-oss") per the T4b decision - never
// forced onto the canonical KJV spine at load time. OSS carries lemma text
// only: surface, dstrong, and morph_code are NULL for every row. This is a
// separate stream from Swete (T12) - word-position identity between the two
// is NOT assumed (per-verse word counts diverge: confirmed exact-count
// match only ~74% of verses in Genesis, ~58% in Daniel); joining them is
// alignment work (T22), not load work. Ticket 13.
package osswords

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"

	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/verses"
)

// sourceCode is the sources.code this loader attributes its rows to.
const sourceCode = "OSS-LXX-lemma"

// Versification is this edition's own verse-identity tag (never canonical).
const Versification = "lxx-oss"

// bookAlias maps the book token used in a verse key (eg "Gen" in
// "Gen.1.1") to the canonical dotted code, for every book this loader
// covers. This map IS the v1 scope allow-list: a key whose token is not
// here is skipped (counted), not an error - covers two kinds of file this
// loader intentionally does not load:
//
//   - Multi-recension witnesses with no single canonical text to pick
//     without guessing (JoshA/JoshB, JudgA/JudgB, DanOG/DanTh - confirmed
//     by direct inspection: JudgA/JudgB are near-complete parallel full
//     texts of the same book, 617 of 618 verse keys overlap; JoshA is a
//     96-verse fragment documenting divergent readings against JoshB's
//     complete 659-verse text - genuinely different situations, but
//     neither has a deterministic "pick this one" rule, so both stay
//     unloaded per invariant #9 rather than hand-picked).
//   - Deuterocanon/extra-biblical books outside the 66-book registry
//     (1/2/3/4 Macc, Bar, EpJer, Jdt, Odes, PsSol, Sir, Sus/Bel OG+Th,
//     Tob, Wis) and the combined-book 1Esd/2Esd (2Esd = LXX's combined
//     Ezra+Nehemiah, the same "2 Esdras" identity question left open in
//     T9's Brenton EZR.htm skip).
//
// Two tokens differ from their source filename: Eccl.js's keys are
// "Qoh.*" (Qoheleth) and Song.js's keys are "Cant.*" (Canticles) -
// confirmed by direct check that every other loaded file's key token
// equals its filename stem.
var bookAlias = map[string]string{
	"Gen": "Gen", "Exod": "Exo", "Lev": "Lev", "Num": "Num", "Deut": "Deu",
	"Ruth": "Rut", "1Sam": "1Sa", "2Sam": "2Sa", "1Kgs": "1Ki", "2Kgs": "2Ki",
	"1Chr": "1Ch", "2Chr": "2Ch", "Esth": "Est", "Job": "Job", "Ps": "Psa",
	"Prov": "Pro", "Qoh": "Ecc", "Cant": "Sng", "Isa": "Isa", "Jer": "Jer",
	"Lam": "Lam", "Ezek": "Ezk", "Hos": "Hos", "Joel": "Jol", "Amos": "Amo",
	"Obad": "Oba", "Jonah": "Jon", "Mic": "Mic", "Nah": "Nam", "Hab": "Hab",
	"Zeph": "Zep", "Hag": "Hag", "Zech": "Zec", "Mal": "Mal",
}

// keyRe matches a verse key: Book.Chapter.Verse, with an optional trailing
// lowercase letter marking a lettered sub-verse (eg Greek Esther's heavy
// use of lettered addition verses: "Esth.1.1a".."Esth.1.1s"). A key that
// doesn't match this shape (eg the single confirmed anomaly "Jer.7.27/28",
// a combined-verse-range key) is reported as malformed, not guessed at.
var keyRe = regexp.MustCompile(`^([A-Za-z0-9]+)\.(\d+)\.(\d+)([a-z]?)$`)

// lemmaEntry is one element of a verse's word array in the source JSON.
type lemmaEntry struct {
	Key   string `json:"key"`
	Lemma string `json:"lemma"`
}

// Load reads one LxxLemmas/<Book>.js file and inserts every word of every
// in-scope verse into words. Lettered sub-verse keys sharing a base verse
// number (eg "Esth.1.1a", "Esth.1.1b") are concatenated, in letter order,
// into one verse row - the same merge the Brenton HTML loader (T9) applies
// to its lettered doublets, for the same reason: this schema has no
// sub-verse addressing. Returns inserted word count, the count of rows
// skipped because their book token is outside v1 scope (bookAlias), and
// the count of malformed keys (reported, not silently dropped). Runs in
// one transaction so a partial load never lands.
func Load(db *sql.DB, r io.Reader) (inserted, skippedBook, malformed int, err error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("read: %w", err)
	}
	var doc map[string][]lemmaEntry
	if err := json.Unmarshal(raw, &doc); err != nil {
		return 0, 0, 0, fmt.Errorf("decode: %w", err)
	}

	var sourceID int64
	if err := db.QueryRow(`SELECT id FROM sources WHERE code = ?`, sourceCode).Scan(&sourceID); err != nil {
		return 0, 0, 0, fmt.Errorf("source %q not seeded: %w", sourceCode, err)
	}

	// A verse's content may be split across several lettered keys (eg
	// "Esth.1.1a".."Esth.1.1s"); group by the base (book, chapter, verse)
	// and keep each letter's lemma list as a separate ordered part, merged
	// below once parts are sorted into manuscript (letter) order.
	type part struct {
		letter string
		lemmas []string
	}
	type verseRef struct {
		book           string
		chapter, verse int
	}
	parts := map[verseRef][]part{}
	for key, entries := range doc {
		m := keyRe.FindStringSubmatch(key)
		if m == nil {
			malformed++
			continue
		}
		book, chStr, vStr, letter := m[1], m[2], m[3], m[4]
		chapter, _ := strconv.Atoi(chStr)
		verse, _ := strconv.Atoi(vStr)
		ref := verseRef{book, chapter, verse}

		lemmas := make([]string, len(entries))
		for i, e := range entries {
			lemmas[i] = e.Lemma
		}
		parts[ref] = append(parts[ref], part{letter, lemmas})
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO words (verse_id, source_id, word_no, surface, lemma, dstrong, morph_code, attestation, editions, source_locator)
		VALUES (?, ?, ?, NULL, ?, NULL, NULL, '', '', ?)`)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	var refs []verseRef
	for ref := range parts {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		a, b := refs[i], refs[j]
		if a.book != b.book {
			return a.book < b.book
		}
		if a.chapter != b.chapter {
			return a.chapter < b.chapter
		}
		return a.verse < b.verse
	})

	for _, ref := range refs {
		code, ok := bookAlias[ref.book]
		if !ok {
			skippedBook++
			continue
		}
		bookID, berr := books.Resolve(tx, "dotted", code)
		if berr != nil {
			if errors.Is(berr, books.ErrUnknownBook) {
				skippedBook++
				continue
			}
			return 0, 0, 0, berr
		}
		verseID, verr := verses.GetOrCreateVerse(tx, Versification, bookID, ref.chapter, ref.verse)
		if verr != nil {
			return 0, 0, 0, verr
		}

		ps := parts[ref]
		sort.Slice(ps, func(i, j int) bool { return ps[i].letter < ps[j].letter })

		wordNo := 1
		for _, p := range ps {
			for _, lemma := range p.lemmas {
				locator := fmt.Sprintf("%s.%d.%d%s#%02d", ref.book, ref.chapter, ref.verse, p.letter, wordNo)
				if _, err := stmt.Exec(verseID, sourceID, wordNo, lemma, locator); err != nil {
					return 0, 0, 0, fmt.Errorf("insert %s: %w", locator, err)
				}
				inserted++
				wordNo++
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, 0, fmt.Errorf("commit: %w", err)
	}
	return inserted, skippedBook, malformed, nil
}
