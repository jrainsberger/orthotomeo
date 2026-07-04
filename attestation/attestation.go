// Package attestation is the text-critical surface (Concord spec §4D):
// Attestation(ref, word?) surfaces the Type/Editions manuscript-tradition
// columns as neutral data - which Greek editions carry a word (e.g. Mark
// 16:9-20 = Type "KO": Traditional/Other manuscripts, absent from the
// Nestle-Aland base text) - with no argument for or against a variant.
// Ticket 18.
package attestation

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/jrainsberger/orthotomeo/retriever"
)

// Attestation returns corpus's Type/Editions data for every word at ref, or
// a single word if word is non-nil (1-based word_no). Always returns at
// least one Citation: a corpus with nothing at ref (no T4b alignment, or a
// requested word_no that doesn't exist there) still gets a Flagged
// placeholder explaining why, never a silent empty result.
func Attestation(db *sql.DB, ref retriever.Ref, word *int, corpus string) ([]retriever.Citation, error) {
	if !retriever.IsWordCorpus(corpus) {
		return nil, fmt.Errorf("Attestation: %q is not a word-tagged corpus (want one of TAGNT, TAHOT, Swete, OSS-LXX-lemma)", corpus)
	}

	targets, err := retriever.ResolveEditionVerses(db, ref, corpus)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return []retriever.Citation{noDataCitation(ref, corpus,
			fmt.Sprintf("no %s alignment for %s (edition-only content or an unaligned gap - T4b)", corpus, ref))}, nil
	}

	var cites []retriever.Citation
	for _, tgt := range targets {
		rows, err := wordsAt(db, tgt.VerseID, corpus, word)
		if err != nil {
			return nil, err
		}
		for _, w := range rows {
			cites = append(cites, buildCitation(ref, corpus, tgt, w))
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

func buildCitation(ref retriever.Ref, corpus string, tgt retriever.AlignedVerse, w wordRow) retriever.Citation {
	var caveats []string
	confidence := retriever.ConfidenceHigh
	if tgt.Relation != "exact" {
		confidence = retriever.ConfidenceFlagged
		caveats = append(caveats, fmt.Sprintf("T4b alignment: %s (confidence %.2f), not a 1:1 verse match - edition verse %s.%d.%d",
			tgt.Relation, tgt.Confidence, ref.Book, tgt.Chapter, tgt.Verse))
	}
	// attestation (the Type marker) is required by TAGNT/TAHOT's own row
	// shape and never empty there; it's empty for every Swete/OSS-LXX-lemma
	// row by design (T12/T13: neither carries a Greek-editions apparatus at
	// all) - that absence is itself the data point worth flagging, not a
	// dropped value.
	if w.attestation == "" {
		confidence = retriever.ConfidenceFlagged
		caveats = append(caveats, fmt.Sprintf("%s carries no Type/Editions attestation for this word (%s)", corpus, noAttestationReason(corpus)))
	}

	return retriever.Citation{
		Ref: ref, Edition: corpus, Text: displayText(w),
		Locator: w.sourceLocator,
		Lemma:   w.lemma, Translit: w.translit, DStrong: w.dstrong,
		Attestation: w.attestation, Manuscripts: w.editions,
		Confidence: confidence, Caveat: strings.Join(caveats, "; "),
	}
}

// noAttestationReason names what T12/T13's package docs already establish,
// so the caveat is specific, not generic.
func noAttestationReason(corpus string) string {
	switch corpus {
	case "Swete":
		return "Swete carries no manuscript-tradition apparatus - T12"
	case "OSS-LXX-lemma":
		return "OSS-LXX-lemma carries no manuscript-tradition apparatus - T13"
	default:
		return "not recorded in the source for this word"
	}
}

type wordRow struct {
	surface, lemma, dstrong, attestation, editions string
	sourceLocator                                  string
	translit                                       string
}

func wordsAt(db *sql.DB, verseID int64, corpus string, word *int) ([]wordRow, error) {
	query := `
		SELECT COALESCE(surface,''), COALESCE(lemma,''), COALESCE(dstrong,''), attestation, editions, source_locator,
		       COALESCE(translit,'')
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
		if err := rows.Scan(&w.surface, &w.lemma, &w.dstrong, &w.attestation, &w.editions, &w.sourceLocator,
			&w.translit); err != nil {
			return nil, fmt.Errorf("wordsAt scan: %w", err)
		}
		out = append(out, w)
	}
	return out, rows.Err()
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
