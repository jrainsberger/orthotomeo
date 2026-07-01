// Package parse is the morphology/lemma surface (Concord spec §4C):
// Parse(ref, word?) for dStrong + expanded morphology, Lemmatize(ref) for
// the ordered lemma list. Both operate per-corpus, over the four
// word-tagged sources (TAGNT, TAHOT, Swete, OSS-LXX-lemma). Ticket 17.
package parse

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jrainsberger/orthotomeo/retriever"
	"github.com/jrainsberger/orthotomeo/verses"
)

// corpusLanguage maps a corpus to the morph_codes.language its morph_code
// values expand against (T6: TEGMC=grc, TEHMC=he). Swete/OSS are Greek LXX
// editions but carry no morph_code at all (T12/T13) - the language entry
// still matters for TAGNT/TAHOT, where it does resolve.
var corpusLanguage = map[string]string{
	"TAGNT":         "grc",
	"TAHOT":         "he",
	"Swete":         "grc",
	"OSS-LXX-lemma": "grc",
}

// Parse returns dStrong + expanded morphology for corpus's words at ref -
// every word in the verse, or a single word if word is non-nil (1-based
// word_no). Always returns at least one Citation: a corpus with nothing at
// ref (no T4b alignment, or a requested word_no that doesn't exist there)
// still gets a Flagged placeholder explaining why, never a silent empty
// result indistinguishable from "didn't look."
func Parse(db *sql.DB, ref retriever.Ref, word *int, corpus string) ([]retriever.Citation, error) {
	if !retriever.IsWordCorpus(corpus) {
		return nil, fmt.Errorf("Parse: %q is not a word-tagged corpus (want one of TAGNT, TAHOT, Swete, OSS-LXX-lemma)", corpus)
	}

	targets, err := resolveTargets(db, ref, corpus)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return []retriever.Citation{noDataCitation(ref, corpus,
			fmt.Sprintf("no %s alignment for %s (edition-only content or an unaligned gap - T4b)", corpus, ref))}, nil
	}

	var cites []retriever.Citation
	for _, tgt := range targets {
		rows, err := wordsAt(db, tgt.verseID, corpus, word)
		if err != nil {
			return nil, err
		}
		for _, w := range rows {
			c, err := buildCitation(db, ref, corpus, tgt, w)
			if err != nil {
				return nil, err
			}
			cites = append(cites, c)
		}
	}
	if len(cites) == 0 {
		detail := "no words"
		if word != nil {
			detail = fmt.Sprintf("word #%d", *word)
		}
		return []retriever.Citation{noDataCitation(ref, corpus,
			fmt.Sprintf("%s not found for %s in %s", detail, ref, corpus))}, nil
	}
	return cites, nil
}

// Lemmatize returns the ordered lemma list for ref in corpus: the same
// underlying words Parse(ref, nil, corpus) would return, filtered to those
// that actually carry a lemma. A word with no lemma (a compound-tagged
// TAGNT word, an untagged TAHOT Qere reading, Swete's surface-only rows)
// has nothing to contribute to a lemma list - that's an accurate reflection
// of the source data, not a completeness violation (invariant #3 governs
// not dropping a MATCHING row, not inventing a lemma that isn't there).
func Lemmatize(db *sql.DB, ref retriever.Ref, corpus string) ([]retriever.Citation, error) {
	all, err := Parse(db, ref, nil, corpus)
	if err != nil {
		return nil, err
	}
	out := make([]retriever.Citation, 0, len(all))
	for _, c := range all {
		if c.Lemma != "" {
			out = append(out, c)
		}
	}
	return out, nil
}

// target is one (verse_id, chapter, verse) this ref resolves to in corpus,
// plus the T4b relation/confidence that got it there (relation="exact",
// confidence=1.0 for a canonical-keyed corpus, where there's no alignment
// step at all).
type target struct {
	verseID        int64
	chapter, verse int
	relation       string
	confidence     float64
}

func resolveTargets(db *sql.DB, ref retriever.Ref, corpus string) ([]target, error) {
	canonRes, err := verses.NewResolver(db, "usfm", verses.Canonical)
	if err != nil {
		return nil, err
	}
	canonicalID, err := canonRes.Resolve(fmt.Sprintf("%s.%d.%d", ref.Book, ref.Chapter, ref.Verse))
	if err != nil {
		return nil, fmt.Errorf("Parse %s: %w", ref, err)
	}

	alignmentKeyed, _ := retriever.IsAlignmentKeyed(corpus)
	if !alignmentKeyed {
		return []target{{verseID: canonicalID, chapter: ref.Chapter, verse: ref.Verse, relation: "exact", confidence: 1.0}}, nil
	}

	rows, err := db.Query(`
		SELECT va.edition_verse_id, v.chapter, v.verse, va.relation, va.confidence
		FROM verse_alignment va
		JOIN verses v ON v.id = va.edition_verse_id
		WHERE va.canonical_verse_id = ? AND va.source_id = (SELECT id FROM sources WHERE code = ?)
		ORDER BY va.id`, canonicalID, corpus)
	if err != nil {
		return nil, fmt.Errorf("resolveTargets %s: %w", corpus, err)
	}
	defer rows.Close()

	var targets []target
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.verseID, &t.chapter, &t.verse, &t.relation, &t.confidence); err != nil {
			return nil, fmt.Errorf("resolveTargets scan: %w", err)
		}
		targets = append(targets, t)
	}
	return targets, rows.Err()
}

type wordRow struct {
	wordNo                                                    int
	surface, lemma, dstrong, morphCode, attestation, editions string
	sourceLocator                                             string
}

func wordsAt(db *sql.DB, verseID int64, corpus string, word *int) ([]wordRow, error) {
	query := `
		SELECT word_no, COALESCE(surface,''), COALESCE(lemma,''), COALESCE(dstrong,''),
		       COALESCE(morph_code,''), attestation, editions, source_locator
		FROM words
		WHERE verse_id = ? AND source_id = (SELECT id FROM sources WHERE code = ?)`
	args := []any{verseID, corpus}
	if word != nil {
		query += ` AND word_no = ?`
		args = append(args, *word)
	}
	query += ` ORDER BY word_no`

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("wordsAt: %w", err)
	}
	defer rows.Close()

	var out []wordRow
	for rows.Next() {
		var w wordRow
		if err := rows.Scan(&w.wordNo, &w.surface, &w.lemma, &w.dstrong, &w.morphCode, &w.attestation, &w.editions, &w.sourceLocator); err != nil {
			return nil, fmt.Errorf("wordsAt scan: %w", err)
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func buildCitation(db *sql.DB, ref retriever.Ref, corpus string, tgt target, w wordRow) (retriever.Citation, error) {
	file, err := sourceFile(db, corpus)
	if err != nil {
		return retriever.Citation{}, err
	}

	var caveats []string
	confidence := retriever.ConfidenceHigh
	if tgt.relation != "exact" {
		confidence = retriever.ConfidenceFlagged
		caveats = append(caveats, fmt.Sprintf("T4b alignment: %s (confidence %.2f), not a 1:1 verse match - edition verse %s.%d.%d",
			tgt.relation, tgt.confidence, ref.Book, tgt.chapter, tgt.verse))
	}

	grammar := ""
	switch {
	case w.morphCode == "":
		confidence = retriever.ConfidenceFlagged
		caveats = append(caveats, fmt.Sprintf("%s carries no morph_code for this word (%s)", corpus, noMorphReason(corpus)))
	default:
		desc, resolved, err := expandMorph(db, w.morphCode, corpusLanguage[corpus])
		if err != nil {
			return retriever.Citation{}, err
		}
		if resolved {
			grammar = fmt.Sprintf("%s (%s)", w.morphCode, desc)
		} else {
			grammar = w.morphCode
			confidence = retriever.ConfidenceFlagged
			caveats = append(caveats, fmt.Sprintf("morph_code %q not found in morph_codes (a known, small, documented cross-file gap - T14)", w.morphCode))
		}
	}

	return retriever.Citation{
		Ref: ref, Edition: corpus, Text: displayText(w),
		SourceFile: file, SourceLocator: w.sourceLocator,
		Lemma: w.lemma, DStrong: w.dstrong, Grammar: grammar,
		Attestation: w.attestation, Editions: w.editions,
		Confidence: confidence, Caveat: strings.Join(caveats, "; "),
	}, nil
}

// noMorphReason names what T12/T13's package docs already establish each
// LXX edition actually carries, so the caveat is specific, not generic.
func noMorphReason(corpus string) string {
	switch corpus {
	case "Swete":
		return "Swete is surface-only, T12"
	case "OSS-LXX-lemma":
		return "OSS-LXX-lemma is lemma-only, T13"
	default:
		return "untagged in the source (a compound word or an untagged variant reading)"
	}
}

func expandMorph(db *sql.DB, morphCode, language string) (description string, resolved bool, err error) {
	var desc string
	err = db.QueryRow(`SELECT description FROM morph_codes WHERE code = ? AND language = ?`, morphCode, language).Scan(&desc)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("expandMorph %s: %w", morphCode, err)
	}
	return desc, true, nil
}

func displayText(w wordRow) string {
	if w.surface != "" {
		return w.surface
	}
	return w.lemma
}

func noDataCitation(ref retriever.Ref, corpus, caveat string) retriever.Citation {
	return retriever.Citation{Ref: ref, Edition: corpus, Confidence: retriever.ConfidenceFlagged, Caveat: caveat}
}

func sourceFile(db *sql.DB, code string) (string, error) {
	var f string
	if err := db.QueryRow(`SELECT source_file FROM sources WHERE code = ?`, code).Scan(&f); err != nil {
		return "", fmt.Errorf("sourceFile %s: %w", code, err)
	}
	return f, nil
}
