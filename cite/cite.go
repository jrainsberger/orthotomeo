// Package cite is the only sanctioned bridge from engine output to a study
// deliverable (Concord spec §4E): Cite renders Citations as quoted,
// fully-attributed Markdown blocks in the Teaching/Studies/*-references.md
// house style. It is purely mechanical - every field a Citation carries is
// rendered verbatim, nothing selected or interpreted (Concord spec §7: sense
// disambiguation, which occurrence matters, and how citations get arranged
// into a document's sections/argument are the analysis layer's job, not
// this package's). Ticket 19.
package cite

import (
	"fmt"
	"strings"

	"github.com/jrainsberger/orthotomeo/retriever"
)

// Cite renders citations as a Markdown bullet list, one line per Citation,
// in ref order as given (callers control ordering, e.g. by book/chapter/
// verse or by discovery order - Cite doesn't re-sort). Returns "" for an
// empty slice.
func Cite(citations []retriever.Citation) string {
	if len(citations) == 0 {
		return ""
	}
	lines := make([]string, len(citations))
	for i, c := range citations {
		lines[i] = citeOne(c)
	}
	return strings.Join(lines, "\n")
}

// citeOne renders a single Citation as one Markdown bullet:
//
//   - **REF** (Edition) — "verbatim text" [metadata] (source: file locator) *(caveat)*
//
// Every clause after the ref/edition is present only if the Citation
// carries that data - a placeholder "nothing here" Citation (empty Text,
// Flagged, Caveat-only) still renders a complete, honest line rather than
// an empty or malformed one.
func citeOne(c retriever.Citation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "- **%s** (%s)", c.Ref, c.Edition)
	if c.Text != "" {
		fmt.Fprintf(&b, " — %q", c.Text)
	}
	if meta := metadata(c); meta != "" {
		fmt.Fprintf(&b, " [%s]", meta)
	}
	if prov := provenance(c); prov != "" {
		fmt.Fprintf(&b, " (source: %s)", prov)
	}
	if c.Caveat != "" {
		fmt.Fprintf(&b, " *(%s)*", c.Caveat)
	}
	return b.String()
}

// metadata joins whichever of DStrong/Lemma/Grammar/Attestation/Editions
// the Citation actually carries, in that fixed order, so the same field
// always lands in the same position across a whole reference list.
func metadata(c retriever.Citation) string {
	var parts []string
	if c.DStrong != "" {
		parts = append(parts, c.DStrong)
	}
	if c.Lemma != "" {
		parts = append(parts, c.Lemma)
	}
	if c.Grammar != "" {
		parts = append(parts, c.Grammar)
	}
	if c.Attestation != "" {
		parts = append(parts, "Type="+c.Attestation)
	}
	if c.Editions != "" {
		parts = append(parts, c.Editions)
	}
	return strings.Join(parts, ", ")
}

// provenance joins SourceFile and SourceLocator - the re-fetchable pointer
// back to the exact row this Citation came from.
func provenance(c retriever.Citation) string {
	switch {
	case c.SourceFile != "" && c.SourceLocator != "":
		return c.SourceFile + " " + c.SourceLocator
	case c.SourceFile != "":
		return c.SourceFile
	default:
		return ""
	}
}
