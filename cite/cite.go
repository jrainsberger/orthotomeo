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
	"github.com/jrainsberger/orthotomeo/sources"
)

// Cite renders citations as a Markdown bullet list, one line per Citation,
// in ref order as given (callers control ordering, e.g. by book/chapter/
// verse or by discovery order - Cite doesn't re-sort). Returns "" for an
// empty slice.
//
// Citation no longer carries its own source file (T31 - moved to the
// top-level "sources" map every transport wraps around its Citations, since
// it was byte-identical across every row of a single-corpus result). Cite's
// Markdown output is unaffected by that: it's a flat string for human/LLM
// reading, not a nested structure a client re-parses against a separate
// map, so Cite still inlines the file per bullet exactly as it always has -
// resolving it here from sources.Registry() (an embedded, in-memory lookup,
// no DB, no I/O) rather than from the Citation itself.
func Cite(citations []retriever.Citation) string {
	if len(citations) == 0 {
		return ""
	}
	files := sourceFiles()
	lines := make([]string, len(citations))
	for i, c := range citations {
		lines[i] = citeOne(c, files[c.Edition])
	}
	return strings.Join(lines, "\n")
}

// sourceFiles maps every sources.json edition code to its source_file.
// sources.Registry() decodes an embedded JSON file compiled into the
// binary - failure here means a broken build, not a runtime data gap, so
// it panics rather than threading an error through Cite's fixed, DB-free
// signature (matches the Concord spec's own Cite(citations) -> string
// signature exactly - the same class of "can't happen at runtime" panic
// already used for schemaFor in cmd/orthotomeo-mcp).
func sourceFiles() map[string]string {
	reg, err := sources.Registry()
	if err != nil {
		panic(fmt.Sprintf("cite: sources.Registry(): %v (embedded sources.json is broken)", err))
	}
	m := make(map[string]string, len(reg))
	for _, s := range reg {
		m[s.Code] = s.SourceFile
	}
	return m
}

// citeOne renders a single Citation as one Markdown bullet:
//
//   - **REF** (Edition) — "verbatim text" [metadata] (source: file locator) *(caveat)*
//
// Every clause after the ref/edition is present only if the Citation
// carries that data - a placeholder "nothing here" Citation (empty Text,
// Flagged, Caveat-only) still renders a complete, honest line rather than
// an empty or malformed one.
func citeOne(c retriever.Citation, file string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "- **%s** (%s)", c.Ref, c.Edition)
	if c.Text != "" {
		fmt.Fprintf(&b, " — %q", c.Text)
	}
	if meta := metadata(c); meta != "" {
		fmt.Fprintf(&b, " [%s]", meta)
	}
	if prov := provenance(c, file); prov != "" {
		fmt.Fprintf(&b, " (source: %s)", prov)
	}
	if c.Caveat != "" {
		fmt.Fprintf(&b, " *(%s)*", c.Caveat)
	}
	return b.String()
}

// metadata joins whichever of Translit/DStrong/Lemma/Grammar/Attestation/
// Manuscripts the Citation actually carries, in that fixed order, so the
// same field always lands in the same position across a whole reference
// list. Translit (T32) goes first - right after the quoted original-
// language Text - since pronunciation is what a reader wants immediately
// alongside the text itself, before the more technical dStrong/grammar data.
func metadata(c retriever.Citation) string {
	var parts []string
	if c.Translit != "" {
		parts = append(parts, c.Translit)
	}
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
	if c.Manuscripts != "" {
		parts = append(parts, c.Manuscripts)
	}
	return strings.Join(parts, ", ")
}

// provenance joins the resolved source file (looked up by edition - T31)
// and Locator, the re-fetchable pointer back to the exact row this
// Citation came from.
func provenance(c retriever.Citation, file string) string {
	switch {
	case file != "" && c.Locator != "":
		return file + " " + c.Locator
	case file != "":
		return file
	default:
		return ""
	}
}
