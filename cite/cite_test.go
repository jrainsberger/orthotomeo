package cite_test

import (
	"strings"
	"testing"

	"github.com/jrainsberger/orthotomeo/cite"
	"github.com/jrainsberger/orthotomeo/retriever"
)

func TestCiteEmptySlice(t *testing.T) {
	if got := cite.Cite(nil); got != "" {
		t.Errorf("Cite(nil) = %q, want empty string", got)
	}
}

func TestCiteVerseTextCitation(t *testing.T) {
	c := retriever.Citation{
		Ref: retriever.Ref{Book: "GEN", Chapter: 1, Verse: 1}, Edition: "KJV",
		Text:       "In the beginning God created the heaven and the earth.",
		SourceFile: "bible-text/KJV/KJV.json", SourceLocator: "Gen.1.1",
		Confidence: retriever.ConfidenceHigh,
	}
	got := cite.Cite([]retriever.Citation{c})
	want := `- **GEN.1.1** (KJV) — "In the beginning God created the heaven and the earth." (source: bible-text/KJV/KJV.json Gen.1.1)`
	if got != want {
		t.Errorf("Cite = %q\nwant  = %q", got, want)
	}
}

func TestCiteWordCitationIncludesMetadataInFixedOrder(t *testing.T) {
	c := retriever.Citation{
		Ref: retriever.Ref{Book: "MAT", Chapter: 26, Verse: 28}, Edition: "TAGNT",
		Text:       "ἄφεσιν",
		SourceFile: "STEPBible-Data/.../TAGNT Mat-Jhn....txt", SourceLocator: "Mat.26.28#16=NKO",
		Lemma: "ἄφεσις", DStrong: "G0859", Grammar: "N-ASF (Function=Noun; Case=Accusative)",
		Attestation: "NKO", Editions: "NA28+NA27+Tyn+SBL+WH+Treg+TR+Byz",
		Confidence: retriever.ConfidenceHigh,
	}
	got := cite.Cite([]retriever.Citation{c})
	want := `- **MAT.26.28** (TAGNT) — "ἄφεσιν" [G0859, ἄφεσις, N-ASF (Function=Noun; Case=Accusative), Type=NKO, NA28+NA27+Tyn+SBL+WH+Treg+TR+Byz] (source: STEPBible-Data/.../TAGNT Mat-Jhn....txt Mat.26.28#16=NKO)`
	if got != want {
		t.Errorf("Cite = %q\nwant  = %q", got, want)
	}
}

func TestCiteFlaggedCitationShowsCaveat(t *testing.T) {
	c := retriever.Citation{
		Ref: retriever.Ref{Book: "PSA", Chapter: 9, Verse: 1}, Edition: "Brenton",
		Text: "Εἰς τὸ τέλος", SourceFile: "bible-text/LXX/.../PSA09.htm", SourceLocator: "9:2",
		Confidence: retriever.ConfidenceFlagged,
		Caveat:     "T4b alignment: divide (confidence 0.50), not a 1:1 verse match",
	}
	got := cite.Cite([]retriever.Citation{c})
	if !strings.HasSuffix(got, "*(T4b alignment: divide (confidence 0.50), not a 1:1 verse match)*") {
		t.Errorf("Cite = %q, want a trailing italicized caveat", got)
	}
}

func TestCiteNoDataPlaceholderStillRendersACompleteLine(t *testing.T) {
	c := retriever.Citation{
		Ref: retriever.Ref{Book: "PSA", Chapter: 9, Verse: 1}, Edition: "Swete",
		Confidence: retriever.ConfidenceFlagged,
		Caveat:     "no Swete alignment for PSA.9.1 (edition-only content or an unaligned gap - T4b)",
	}
	got := cite.Cite([]retriever.Citation{c})
	want := `- **PSA.9.1** (Swete) *(no Swete alignment for PSA.9.1 (edition-only content or an unaligned gap - T4b))*`
	if got != want {
		t.Errorf("Cite = %q\nwant  = %q", got, want)
	}
}

func TestCiteMultipleCitationsOneLineEach(t *testing.T) {
	cs := []retriever.Citation{
		{Ref: retriever.Ref{Book: "MAT", Chapter: 26, Verse: 28}, Edition: "TAGNT", Text: "ἄφεσιν"},
		{Ref: retriever.Ref{Book: "ACT", Chapter: 2, Verse: 38}, Edition: "TAGNT", Text: "ἄφεσιν"},
	}
	got := cite.Cite(cs)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2, got: %q", len(lines), got)
	}
	if !strings.Contains(lines[0], "MAT.26.28") || !strings.Contains(lines[1], "ACT.2.38") {
		t.Errorf("lines out of order or wrong content: %v", lines)
	}
}
