# Known issue: Jeremiah MT/LXX alignment is wrong

*Filed 2026-07-18. Found while comparing MT and LXX Jeremiah through the MCP tools. Not yet fixed.*

## Summary

The T4b cross-edition alignment for Jeremiah maps canonical (MT) references to the wrong LXX
chapters, and reports `confidence 0.85` while doing so. Jeremiah reorders a whole block between
the two traditions - the oracles against the nations sit at MT 46-51 but around LXX 25-32, which
displaces everything after MT 25 - and the current mapping appears to apply an offset rather than
modelling the moved block.

The flagging behaviour is correct throughout. Every affected row came back
`confidence: Flagged` with `T4b alignment: renumber (0.85), not a 1:1 verse match`, and no offset
text was ever presented as a match. **This is a data/alignment bug, not a flagging bug** - the
Caveat system is what made the problem visible.

## Observed

**Case 1 - wrong chapter entirely.**
`resolve_ref(JER 33:15)` returns `Brenton: renumber of JER.32.27 (confidence 0.85)` and
`Swete: renumber of JER.32.13`. Fetching canonical JER 33:14-17 in Brenton returns the cup-of-wrath
material ("and all the kings from the north...") - that is the counterpart of **MT 25**, not MT 33.
LXX 32 corresponds to MT 25. MT 33 corresponds to **LXX 40**.

**Case 2 - off by two verses.**
`resolve_ref(JER 23:5)` returns `Brenton: renumber of JER.23.3`. Fetching canonical JER 23:5-6
returns Brenton 23:3-4 (shepherds and remnant). The actual counterpart - the righteous Branch
oracle - is at Brenton 23:5-6 and is reachable only by requesting canonical **23:7-8**.

Note that Swete did *not* carry a renumber caveat at JER 23:5, while Brenton did, so the two LXX
editions disagree about the offset in the same chapter.

## Verified correspondences (by reading Brenton locators directly)

Established by walking canonical refs and reading the `source_locator` returned for each:

| Brenton (LXX) | Corresponds to (MT) | How established |
|---|---|---|
| 39:1 | 32:1 | "tenth year of king Sedekias, eighteenth of Nabuchodonosor" |
| 40:1-13 | 33:1-13 | verbatim parallel across all thirteen verses |
| 41:1 | 34:1 | "Nabuchodonosor... warring against Jerusalem... Go to Sedekias" |

Consequence: **LXX Jeremiah 40 ends at verse 13, and MT Jeremiah 33:14-26 has no LXX counterpart.**
That is a real feature of the textual tradition, not a bug - but the aligner currently obscures it
by pointing MT 33 at LXX 32.

## Status

- **T1 (landed 2026-07-21): confidence honesty cap.** `versealign.classify` now caps
  every pairing's confidence at the size-agreement of the chapter-level operation that
  produced it (the weakest-link rule, `producedGroup.opConfidence`). A renumber whose
  position was allocated inside a size-mismatched chapter merge/divide - the shape of the
  displaced-block region - no longer reports the flat `0.85` of a clean relabel; it is
  capped by `1 - |sizeA-sizeB|/max(sizeA,sizeB)`. Purely count-derived, no per-book table
  (invariant #9). Residue left for T2: a renumber born from an *equal-size coincidental*
  substitution, or from a merge whose sizes happen to sum perfectly, still reports the full
  ceiling - counts cannot distinguish those from a real relabel. See the two locking tests
  `TestAlignCleanRenumberKeepsFullConfidence` / `TestAlignRenumberInMismatchedRegionIsCapped`.
- **T2 (landed 2026-07-21): stay honest and get out of the way.** The root cause is that
  one method (count-based chapter DP) is applied to two different kinds of divergence:
  *versification* (same text, renumbered - Psalms/Joel, which it bridges correctly) and
  *recension* (different text - LXX Jeremiah is a shorter, reordered edition). No count-
  derived signal separates them (confirmed against the real corpus: the reordered zone is
  threaded as near-equal-size substitutions, invisible to any size cap - so T1 alone left
  the headline JER 33 -> LXX 32 row at 0.85). The fix declares recension-divergent books
  explicitly (`recension.IsDivergent`, currently just JER; the Greek additions to
  Esther/Daniel are LXX-only insertions, not reorderings, so they are excluded) and, for
  those books, has the aligner refuse to assert a verse correspondence across the reordered
  span rather than manufacture a wrong one. The span is derived mechanically from the
  alignment's own structural ops (`versealign.structuralCanonicalSpan`); the clean head and
  tail keep their alignment. Verified end-to-end on the real DB: JER 1-24 keep 588 Brenton
  rows, JER 25-51 emit **zero** rows, JER 52 keeps 34; `GetVerse(JER 33:15, Brenton)` now
  returns a Flagged citation naming the recension divergence instead of pointing at LXX
  32:27 at 0.85. What this deliberately does NOT do: recover the true in-zone
  correspondences (eg MT 33:1-13 <-> LXX 40). That would need the full external
  versification map and was declined - researchers work in LXX numbering for this book, and
  the tool should get out of their way, not fake a bridge. Tests: `recension` package,
  `versealign` (suppression + negative control), `retriever` (recension caveat + negative
  control).

Original T2 scope (the three items below) is thus resolved by refusal rather than by
modelling the moved block: no distinct `moved` relation is emitted (suppressed verses get
no row), and genuine absence is represented as "no correspondence asserted" for the whole
reordered span.

## What to fix

1. **The Jeremiah mapping.** Model the displaced block rather than applying a chapter/verse offset.
2. **The confidence value.** `0.85` is being reported for mappings that are simply wrong. Whatever
   computes it does not detect this failure class. Consider whether block-reordered books should
   carry a distinct caveat type instead of being scored as ordinary renumbers, so that a consumer
   can tell "renumbered, trust the mapping" from "reordered, verify before use".
3. **Genuine absence should be representable.** MT 33:14-26 is not renumbered, it is *absent* from
   the LXX witness. A consumer needs to be able to distinguish "present elsewhere under another
   number" from "not present in this edition at all". Today both surface as a flagged renumber.

## Why it matters beyond correctness

A user comparing traditions here can very easily conclude that the Branch oracle is missing from
the LXX. It is not - it stands at Brenton 23:5-6 in both witnesses. The wrong mapping plus a
plausible-looking flagged result is exactly the shape of an error that gets published. The flag
prevented it; the mapping should not have required it.
