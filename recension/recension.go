// Package recension declares which canonical books have a Septuagint witness
// that is a different *recension* - a restructured/reordered edition of the
// text - rather than merely a *renumbering* of the same text.
//
// This distinction is the one thing the verse aligner cannot recover from
// verse counts. The count-based chapter DP (align.AlignWeighted) correctly
// bridges versification divergence - the same text under different numbering
// (Psalms 9/10 merged, a leading-title shift, Joel's chapter boundary) - but
// it cannot tell that apart from recension divergence, where the underlying
// text itself differs. Jeremiah is the paradigm: the Septuagint is a shorter
// recension with the Oracles Against the Nations relocated (MT 46-51 sit at
// LXX 25-32), so a purely size-based alignment threads MT chapters onto
// whichever LXX chapters happen to have similar lengths and manufactures a
// confident-looking but wrong correspondence (see
// docs/known-issue-jeremiah-alignment.md).
//
// Because no count-derived signal separates the two cases (proven against the
// real corpus), the honest behaviour is to *declare* the recension-divergent
// books explicitly and have the aligner refuse to assert a verse-level
// correspondence across the reordered region - stay honest and get out of the
// way - rather than infer divergence from a heuristic that could silently
// suppress a legitimately alignable book. This is an explicit-over-magic
// choice: a short, documented list, not pattern inference.
//
// Scope note: only *reordered* recensions belong here. The Greek additions to
// Esther and Daniel are LXX-only insertions, not reorderings - canonical
// verses in those books still align correctly to their LXX counterparts, and
// the extra Greek material is already handled as unaligned edition-only
// content - so they are deliberately NOT listed.
package recension

// divergentBooks are canonical books (by USFM code - the machine identifier
// both the build and query sides carry) whose Septuagint witness is a
// different recension. Kept deliberately minimal: add a book only after
// confirming the divergence is a genuine reordering, not a renumbering the
// aligner already handles. JER is Jeremiah.
var divergentBooks = map[string]bool{
	"JER": true,
}

// lxxSources are the alignment-keyed Septuagint editions a divergent book can
// diverge against. Non-LXX editions (KJV/ASV/WEB) are the canonical tradition
// itself and never carry recension divergence, so they are excluded.
var lxxSources = map[string]bool{
	"Brenton":       true,
	"Swete":         true,
	"OSS-LXX-lemma": true,
}

// IsDivergent reports whether the canonical book (USFM code) has a
// recension-divergent witness in the given source edition. Both the build
// side (versealign) and the query side (retriever) call this with the same
// (bookCode, sourceCode) pair, so a suppressed alignment and its caveat stay
// in agreement.
func IsDivergent(bookCode, sourceCode string) bool {
	return divergentBooks[bookCode] && lxxSources[sourceCode]
}
