// Package interlinear composes a row-aligned original/transliteration/
// gloss/grammar display over an existing Parse result (Concord spec's
// reading-view need). It is a response-shape ticket, not a new retriever
// capability (T35): every field on Word already comes from a Citation
// (T17/T32) or a lexicon.Lookup (T34) - this package sources no new
// original-language data of its own, it only composes the two.
package interlinear

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jrainsberger/orthotomeo/lexicon"
	"github.com/jrainsberger/orthotomeo/retriever"
)

// Word is one interlinear row: a Parse Citation's own fields, stacked
// alongside (never replacing) a looked-up Gloss. Confidence/Caveat are
// carried through verbatim from the underlying Citation - an interlinear
// render of a Flagged word is still Flagged, not silently upgraded by
// the presence of a gloss.
type Word struct {
	Ref        retriever.Ref        `json:"ref"`
	Edition    string               `json:"edition"`
	Text       string               `json:"text"`
	Translit   string               `json:"translit,omitempty"`
	Lemma      string               `json:"lemma,omitempty"`
	Gloss      string               `json:"gloss,omitempty"`
	DStrong    string               `json:"dstrong,omitempty"`
	Grammar    string               `json:"grammar,omitempty"`
	Confidence retriever.Confidence `json:"confidence"`
	Caveat     string               `json:"caveat,omitempty"`
}

// Build composes one Word per Citation, resolving Gloss via lexicon.Lookup
// keyed by DStrong. A Citation with no DStrong (a compound-tagged word, an
// untagged reading, or a no-data placeholder) gets no gloss - not a
// fabricated one, the same "don't guess" discipline the rest of this corpus
// follows. A DStrong with no matching lexicon row (a known, small,
// documented cross-file gap - T14) is likewise left gloss-less rather than
// failing the whole render over one missing dictionary entry; any other
// lookup failure (a broken DB) still propagates, not swallowed.
func Build(db *sql.DB, citations []retriever.Citation) ([]Word, error) {
	out := make([]Word, len(citations))
	for i, c := range citations {
		w := Word{
			Ref: c.Ref, Edition: c.Edition, Text: c.Text,
			Translit: c.Translit, Lemma: c.Lemma, DStrong: c.DStrong,
			Grammar: c.Grammar, Confidence: c.Confidence, Caveat: c.Caveat,
		}
		if c.DStrong != "" {
			entry, err := lexicon.Lookup(db, c.DStrong)
			switch {
			case err == nil:
				w.Gloss = entry.Gloss
			case errors.Is(err, sql.ErrNoRows):
				// no lexicon entry for this dStrong - a documented gap
				// (T14), not a reason to fail the render.
			default:
				return nil, fmt.Errorf("interlinear.Build: %w", err)
			}
		}
		out[i] = w
	}
	return out, nil
}

// Render is Build's Markdown counterpart to cite.Cite - one line per Word,
// same "every field present only if the word actually carries it" honesty
// cite.Cite already follows. Kept in this package (not cite itself) since
// Word is a display composition, not a Citation - cite.Cite's signature
// stays exactly the Concord-spec Cite([]Citation) string it always was.
func Render(words []Word) string {
	if len(words) == 0 {
		return ""
	}
	lines := make([]string, len(words))
	for i, w := range words {
		var b strings.Builder
		fmt.Fprintf(&b, "- **%s** (%s)", w.Ref, w.Edition)
		if w.Text != "" {
			fmt.Fprintf(&b, " %q", w.Text)
		}
		if w.Translit != "" {
			fmt.Fprintf(&b, " [%s]", w.Translit)
		}
		if w.Gloss != "" {
			fmt.Fprintf(&b, " — %s", w.Gloss)
		}
		if w.Grammar != "" {
			fmt.Fprintf(&b, " (%s)", w.Grammar)
		}
		if w.Caveat != "" {
			fmt.Fprintf(&b, " *(%s)*", w.Caveat)
		}
		lines[i] = b.String()
	}
	return strings.Join(lines, "\n")
}
