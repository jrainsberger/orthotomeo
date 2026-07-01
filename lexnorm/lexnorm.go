// Package lexnorm normalizes lemma text to Unicode NFC. Corpus source files
// (STEPBible TAGNT/TAHOT, Open Scriptures LXX lemmas) and query input typed
// or generated separately don't reliably land on the same codepoint
// sequence for the same accented letter - e.g. Greek polytonic oxia
// (U+1F77) vs monotonic tonos (U+03AF) render identically but compare
// unequal as raw bytes, even though they're canonically equivalent under
// Unicode NFC. Every exact-match lemma lookup (concord.ConcordLemma,
// ConcordPhrase) depends on both sides of the comparison having gone
// through the same normalization - this package is that one shared point,
// used by both the loaders that write words.lemma and the concord package
// that queries it.
package lexnorm

import "golang.org/x/text/unicode/norm"

// NFC returns s in Unicode Normalization Form C. Empty input returns
// unchanged - normalizing "" is a no-op but the check avoids an allocation
// on the overwhelmingly common empty-lemma case (words with no lemma).
func NFC(s string) string {
	if s == "" {
		return s
	}
	return norm.NFC.String(s)
}
