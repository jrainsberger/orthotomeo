# orthotomeo - build plan

The full phase/ticket roadmap. Execute tickets in order. Each is independently
testable and leaves the repo green (build + vet + tests pass, builder runs).

> Name: orthotomeo ( orthotomeo, "rightly dividing the word of truth", 2 Tim 2:15).

## What this is

A read-only scripture-study **engine**: it imports a multi-edition biblical
corpus into a derived SQLite database, then serves verbatim, provenance-tagged
lookups and concordance over it via an MCP surface. It is the **determinate
spine** of a study system - the engine owns *text* (verses, words, morphology),
the LLM client owns *meaning* (interpretation). The engine never interprets;
the analysis never quotes original-language text it did not get from the engine.

Governing design doc (Phase 4+): the "Concord spec" - an external design document
not included in this repo; its invariants are summarized operatively below and its
section numbers (e.g. "§4B", "§10") are cited throughout this file and the codebase
as provenance for a decision, not as a live link. Data model: `docs/erd-v1.svg`.
Go style: standard (`gofmt`, `go vet` clean).

## Cross-cutting invariants (apply to EVERY ticket)

1. **SQLite is a derived build artifact.** The corpus files are the source of
   truth and are read-only. `cmd/build` regenerates the DB from scratch; never
   hand-edit the DB, never reformat a corpus file.
2. **Every text/word row carries `source_id`.** Provenance is the spine: a row
   without a traceable source is an assertion, not a citation. This is also how
   "can I ship this byte?" is answered (`sources.shippable`).
3. **Complete or fail loudly.** Any concordance/sweep returns *every* matching
   row or raises - never a silent subset. Partial truncation is the cardinal
   failure (Concord spec invariant #3).
4. **Reconcile at read time; never assume 1:1 across editions.** Editions
   disagree on versification and canon. A reference resolves to a per-edition
   address; surface the disagreement as data.
5. **Match the lemma, not the root** (Greek); Hebrew may match by root. The
   lemma->Strong's bridge is `words.dstrong -> lexicon.dstrong`.
6. **Go conventions**: follow `go.md`. `internal/` is for harnesses/fakes only;
   concern packages live at the repo root. Exported items get godoc comments
   opening with the name and citing the ticket. Named failures are exported
   sentinels matched with `errors.Is`. `gofmt`/`go vet` are non-negotiable.
7. **Tests**: `go test`, native harness, black-box `package x_test`. File-backed
   DB in `t.TempDir()`. Table-driven, named subtests, `t.Helper()` in helpers,
   `t.Fatalf` on setup / `t.Errorf` on independent assertions. Inline test data;
   never read a corpus file from a unit test (load-path tests may, scoped small).
8. **No em-dashes in docs** (hyphens only). House output style.
9. **Imports are deterministic and LLM-free.** Every import is a pure, re-runnable
   function of the corpus files (plus CC-BY seed tables) - same inputs, identical
   DB every run. No LLM in the build path, and **no hand-curated content**. If a
   mapping cannot be derived mechanically from the data, it is surfaced as a typed
   relation or a loud, counted skip - never authored by hand and never inferred by
   a model. (This invariant is the reason the T4b verse-alignment design below
   exists: a prior session, told "no hand-curation," over-reached into building a
   TVTMS conditional-rule *engine*. The correct reading is the opposite - derive
   the mapping deterministically from data we already parse, so neither curation
   nor an inference engine is needed.)

## Definition of done (every ticket)

- `gofmt -l .` silent, `go vet ./...` clean, `go test ./...` green.
- `go run ./cmd/build` succeeds and prints the new counts.
- New exported surface has godoc + tests mapping to the ticket's acceptance
  criteria.
- One commit, conventional message, citing the ticket.

## Corpus locations

`cmd/build` takes `--corpus <root>` and `--reference <root>` (both required, no
default - these are external inputs, not part of this repo). Sources resolve their
`source_file` glob (in `sources/sources.json`) against whichever root they live
under. Expected layout, split across the two roots (Ticket 3 reconciles this):

| Tree | Root | Relative path |
|---|---|---|
| STEPBible-Data | `--reference` | `STEPBible-Data` |
| LXX-Swete-1930 | `--reference` | `LXX-Swete-1930` |
| bible-text (KJV/ASV/WEB/Brenton/OSS) | `--corpus` | `bible-text` |
| cross_references.txt (OpenBible/TSK) | `--corpus` | `cross_references.txt` |

## Status legend

`DONE` shipped & committed · `NEXT` ready to start · `BLOCKED` waiting on a dep ·
`DESIGN` needs a decision before coding (flagged inline) · `V2` deferred.

---

# Phase 0 - Scaffold & provenance

### T1 - sources registry  `DONE` (commit cadfb1a)
Provenance registry: `sources` table + checked-in `sources/sources.json` + `Seed`.
13 sources with per-row license/shippable. Done.

---

# Phase 1 - Canonical reference frame

### T2 - books + book_names  `DONE` (commit a0aac5b)
Canonical 66-book registry + per-scheme aliases (usfm/osis/dotted/name-en),
scheme-keyed `Resolve(scheme, value) -> book_id` with `ErrUnknownBook`. Done.

### T3 - corpus locator  `DONE`
**Goal:** one place that maps a source to its absolute file(s), so loaders never
hard-code paths.
- New `corpus` package: `Locate(src sources.Source, roots ...string) ([]string, error)`
  resolving the `source_file` glob against each root in turn, returning the
  first root's matches; `LocateOne` additionally requires exactly one match.
- `cmd/build` gains `--corpus` (bible-text/, cross_references.txt) and
  `--reference` (STEPBible-Data/, LXX-Swete-1930/) flags, both threaded to
  `corpus.Locate` as an ordered root list. Loaders now resolve a path only via
  `sourceByCode(code)` + `corpus.LocateOne`, never a hard-coded join.
**Schema:** none.
**Acceptance:** `Locate` returns the right file set for each tree against a
temp fixture tree (inline a tiny fake two-root layout, mirroring the real
split); `ErrCorpusMissing` sentinel when a required tree is absent under every
root. Glob expansion is deterministic (sorted).
**Notes (as built):** chose the "accept multiple roots" option over the
single-root-plus-symlinks recommendation - the real layout is already split
across two parents (`D:\Claude\Bible`, `D:\Reference`) and symlinking would
mean writing into the read-only `bible-text/` corpus directory's parent for
no functional gain. `Locate`'s variadic roots signature differs from the
ticket's literal `Locate(src, root string)` for the same reason. Full
rebuild verified unchanged: 22,717 lexicon entries, 2,565 morph codes, same
verse/xref counts as T5/T6.

### T4 - verses spine + verse alignment  `T4a DONE` · `T4b DONE`
**Status:** T4a (verses spine) DONE - canonical = KJV/English, enumerated from
KJV.json (31,102 verses / 1,189 chapters), `verses.BuildSpine` + scheme-aware
`verses.Resolver` + `ErrUnknownVerse`. `name-en` aliases corrected to the KJV/ASV
JSON form (Roman numerals, "Revelation of John"). **T4b** (populate
`versification_map` from TVTMS for LXX divergences) is deferred to when the LXX
loaders (T9/T12/T13) need it; the table exists, empty.
**Original goal / design (kept for T4b):**
**Decided:**
- Canonical versification basis. **Recommended:** standard KJV/English Protestant
  versification, enumerated from `KJV.json` (it contains every book/chapter/verse;
  ~31,102 verses, 1,189 chapters, 66 books). The English editions then map 1:1;
  LXX maps via TVTMS.
- TVTMS modeling: store only the *divergences* (identity mappings are implicit).
**Scope:**
- `verses` table: `id, book_id (FK), chapter, verse`, unique `(book_id, chapter, verse)`.
- Build the spine by enumerating KJV.json through `books.Resolve("name-en", ...)`.
- Parse TVTMS (`STEPBible-Data/Versification/TVTMS*.txt`) into `versification_map`
  `(id, source_id, native_book, native_chapter, native_verse, verse_id)` for the
  rows where an edition's native ref differs from canonical (LXX Psalm offsets etc.).
- `verses.Resolve(scheme, native_ref) -> verse_id` helper used by all later loaders
  (identity when no map row, mapped otherwise).
**Schema delta:** `verses`, `versification_map` (per `erd-v1.svg`).
**Acceptance:** verse count == KJV total (assert exact, document the number);
chapter count 1,189; every KJV verse resolves; a TVTMS spot-check resolves an LXX
Psalm-offset ref to the right canonical verse; unknown ref -> `ErrUnknownVerse`.
**Notes:** v1 = 66 books only; deuterocanon TVTMS rows are skipped (log count).
**Re-checked:** confirmed T4b still unstarted - no TVTMS parser/package exists,
`versification_map` is schema-only (empty). Decision stands: defer to T9/T12/T13
rather than build it with no consumer to verify against.

**T4b scope audit (2026-06-30), triggered by attempting T9:**

TVTMS's machine-usable "Expanded Version" is **not a static lookup table** -
it is a conditional rules table. Each data row has a `SourceType` (the
versification *tradition* the row applies to - not one "Greek" value but
several: `Greek`, `Greek2`, `GreekUndivided`, `GreekIntegrated`,
`GrkTitleSeparate`, plus combination types), a `SourceRef` -> `StandardRef`
pair, an `Action`, and `Tests` (conditions like `Gen.32:33=Last` that must be
evaluated against *the specific edition's own chapter/verse structure*, not
looked up from an external authority - though they are mechanically
computable from the edition's own parsed verse counts).

`Action` is not uniformly an ID remap. Of the ~22,874 Expanded-version data
rows (all SourceTypes): `Renumber verse` 51.7%, `Keep verse` 36.7% - these
two are simple 1:1 remaps, 88.4% combined. The remaining ~11.6% need real
text handling beyond a `native_ref -> verse_id` row: `Concatenation` 2.9%
(two source verses merge into one standard verse - requires combining
*text*), `MergedPrev`/`MergedNext` 2.5%, `DividedPrev`/`DividedNext` 2.6%
(one source verse's text splits across two standard verses), `IfEmpty`
1.1%, `Renumber title` 1.1%, `CopiedFrom` 0.7%, `Psalm title` 0.5%,
`MovedFrom` 0.3%. The current `versification_map` schema
(`native_book/chapter/verse -> verse_id`, one row in, one row out) cannot
represent Concatenation/Merge/Divide - those need loader-level text
merge/split logic, not just a bigger map.

**Direct chapter/verse-count audit, Brenton LXX vs KJV** (all 39 OT books,
verse spans counted from the actual HTML, chapter-label/index pages
excluded - `cmd/build` source not yet committed, audit script was scratch):

| Book | KJV ch | LXX ch | KJV vrs | LXX vrs | Note |
|---|---|---|---|---|---|
| PSA | 150 | 151 | 2,461 | 2,535 | classic Ps 9/10 merge offset + Ps 151 (deuterocanon, no KJV equivalent) |
| EST | 10 | 10 | 167 | 252 | Brenton ships Esther only as `ESG` (Greek text with the deuterocanonical additions folded in) |
| DAN | 12 | 12 | 357 | 422 | Brenton ships Daniel only as `DAG` (Susanna/Bel/Song of Three integrated) |
| EZR | 10 | 23 | 280 | 669 | Brenton's `EZR` is the LXX's combined Ezra+Nehemiah as one book (matches TVTMS's separate `2Es` book token, not `Ezr`/`Neh` - a book-identity question, not a verse-remap question); `NEH` *also* exists as a separate 13-chapter file with its own (smaller) divergence (389 vs 406) - relationship between the two unconfirmed, open question |
| JER | 52 | 52 | 1,364 | 1,299 | LXX Jeremiah is a genuinely shorter recension (~5% fewer verses), not just reordered |
| EXO | 40 | 40 | 1,213 | 1,166 | |
| 1SA | 31 | 31 | 810 | 792 | |
| PRO | 31 | 31 | 915 | 938 | |
| 1CH | 29 | 29 | 942 | 931 | |
| 1KI | 22 | 22 | 816 | 823 | |
| JOB | 42 | 42 | 1,070 | 1,082 | |
| 2CH | 36 | 36 | 822 | 832 | |
| EZK | 48 | 48 | 1,273 | 1,265 | |
| ISA | 66 | 66 | 1,292 | 1,289 | |
| NEH | 13 | 13 | 406 | 389 | see EZR note |
| 2KI | 25 | 25 | 719 | 723 | |
| GEN | 50 | 50 | 1,533 | 1,532 | |
| JOS | 24 | 24 | 658 | 659 | |
| 2SA | 24 | 24 | 695 | 697 | |
| DEU | 34 | 34 | 959 | 958 | |
| LAM | 5 | 5 | 154 | 153 | |
| JOL | 3 | 4 | 73 | 73 | pure chapter-boundary shift, verse count matches exactly - no content gain/loss |
| MAL | 4 | 3 | 55 | 55 | pure chapter-boundary shift (opposite direction), verse count matches exactly |
| AMO, ECC, HAB, HAG, HOS, JDG, LEV, MIC, NAM, NUM, OBA, RUT, SNG, ZEC, ZEP | - | - | - | - | exact match, no divergence (15 of 39 books) |

24 of 39 OT books diverge from KJV in chapter and/or verse count; 15 match
exactly. The divergences split into two different *kinds* of problem:

1. **Pure renumbering/boundary shifts** (JOL, MAL, and most of the small
   +/-1 to +/-20 verse cases like GEN, JOS, 2SA, DEU, LAM, ISA, EZK, 2KI,
   1KI, 1CH) - every verse has real KJV-equivalent content, just at a
   different address. This is what `versification_map` was designed for.
2. **Genuine added/removed content** (PSA's Ps 151, EST's and DAN's Greek
   additions, JER's shorter recension, EZR's combined-book structure) -
   some LXX verses have **no KJV verse to map to at all**. No TVTMS engine
   fixes this; the existing skip-and-report pattern (proven in T8 for WEB's
   7 unresolvable verses) is the correct handling, not a bug to solve.

**DECISION (2026-06-30, approved by Justin): per-edition verse identity +
deterministic verse_alignment. Do NOT build a TVTMS rule engine; do NOT
hand-curate verses; do NOT force the LXX onto the KJV spine.**

*Root cause of the difficulty:* the original model forced every edition onto the
single KJV spine through a 1:1 `versification_map`. That fights the data (24 of 39
OT books diverge; some LXX content has no KJV target at all) and violates invariant
#4 ("per-edition address; never assume 1:1"). The fix is to stop forcing it, and to
keep canonical formatting from being imposed on the LXX.

**Schema change (this SUPERSEDES `versification_map`):**
- `verses` gains `versification TEXT NOT NULL DEFAULT 'canonical'`; the unique key
  becomes `(versification, book_id, chapter, verse)`. KJV/ASV/WEB and crossrefs keep
  using `versification='canonical'` rows exactly as today - no migration of existing
  rows beyond the new column's default.
- **DROP `versification_map`** (it is empty / schema-only). Replace it with:
- `verse_alignment (id INTEGER PK, canonical_verse_id INTEGER FK verses,
  edition_verse_id INTEGER FK verses, relation TEXT NOT NULL, group_id INTEGER NULL,
  confidence REAL NOT NULL, source_id INTEGER FK sources)`. A typed, many-to-many
  relation between an edition's OWN verse rows and the canonical rows.
  `relation` in {exact, renumber, merge, divide, title, moved}. `group_id` ties the
  members of an n:1 merge or 1:n divide. **LXX-only content** (Ps 151, the Esther/
  Daniel Greek additions, extra verses) has **no alignment row** - it is fully present
  as its own verse row, by design. **Canonical-only content** (KJV verses absent from
  the shorter LXX Jeremiah) likewise has no alignment row. Neither is a skip or a bug;
  absence of an alignment row IS the data.
- Update `docs/erd-v1.svg` (currently shows `versification_map`) when convenient -
  it is stale on this one table.

**Loader model for the LXX (T9/T12/T13):** each LXX loader GET-OR-CREATEs its own
verse rows under its versification tag (`lxx-brenton`, `lxx-swete`) and FKs its
text/word rows to those. `verse_id` stays NOT NULL everywhere - no nullable column.
There is NO canonical resolution at load time; the LXX is loaded completely in its
own versification. Book-identity that differs per edition (Brenton `EZR` = LXX 2
Esdras = combined Ezra+Nehemiah; `DAN`->`DAG`, `EST`->`ESG`) is handled at the book
token, recorded once, not re-litigated per verse.

**T4b = the deterministic verse aligner (new ticket spec, separate from T9):**
*This is the determinism guarantee (invariant #9) applied. It needs no TVTMS engine
and no curated verse, because we already parse both editions' real structures and can
compute directly what TVTMS's Tests were reconstructing without that data.*
- Input: the canonical verse rows + an edition's own verse rows, both already in the DB.
- Per (edition-book <-> canonical-book) pair, **sequence-align the two verse lists**
  (LCS / Needleman-Wunsch) - the SAME machinery as the T22 Swete<->OSS word aligner;
  build them to share code.
- **Seed/cross-check from TVTMS's simple rows only** (`Keep verse` + `Renumber verse`
  = 88.4%, CC-BY) as high-confidence anchors where they apply - NOT as a required rule
  engine, and the conditional Tests are NOT evaluated; the sequence alignment over real
  data is the source of truth, TVTMS is corroboration.
- Emit typed `verse_alignment` rows: 1:1 -> exact (same number) or renumber (shifted);
  n:1 -> merge (shared `group_id`); 1:n -> divide; unaligned edition verse -> no row
  (added content); unaligned canonical verse -> no row (absent in this edition).
- **Re-runnable, identical every run, zero curated verses, no LLM.** Confidence is a
  number from the alignment, not a judgment call.
- Acceptance: deterministic (two runs byte-identical); JOL/MAL boundary shifts produce
  exact 1:1 alignments with matching verse counts; the Ps 9/10 offset produces the
  right renumber chain; Ps 151 and the Esther/Daniel additions produce edition verse
  rows with no alignment (assert they exist as verses, assert no alignment row); a
  known merge/divide produces a shared `group_id`.

**T4b AS-BUILT (2026-06-30):** the spec above called for verse-LABEL-based sequence
alignment, optionally cross-checked against TVTMS's simple rows. Implementing it
surfaced that verse-label equality is **not safe as the primary signal** - confirmed
wrong on real data in two distinct shapes before landing on the actual design below.
Two runs at this are documented for the record (the second run is what shipped):

1. **First attempt (verse-label LIS + gap-fill), found unsound.** Identical
   `(chapter,verse)` labels were trusted as anchors, found via patience-diff-style
   LIS. This is wrong whenever a chapter has been renumbered, split, or merged,
   because both sides' verse numbering then restarts/recounts independently and
   small numbers trivially coincide. Confirmed wrong twice: Brenton `lxx-brenton`
   "10:1" label-matched canonical Psalms 10:1, but is actually Psalm 11:1's content
   (the Hebrew Ps9+10 merge into Greek Ps9 shifts everything after by one chapter -
   exactly the mechanism Justin named); and Joel's canonical 3:1 label-matched
   edition 3:1, but edition's chapter 3 is actually the tail of canonical chapter 2
   (Joel's chapters split differently) - canonical 3:1 is unrelated content. Both
   were confidently mislabeled `exact`.
2. **A "trust labels in this direction, not that one" patch, also found unsound.**
   Reasoning: genuine omissions (edition same size or smaller) preserve surrounding
   verse numbers; insertions (edition larger, eg a Psalm title counted as verse 1)
   cascade-renumber everything after. This fixed Brenton Psalms 5/7 (the title
   case) but NOT Exodus 7/8 (canonical 25/32 vs edition 29/28 - content genuinely
   crosses the chapter boundary, but the chapter-level decision below still chose
   to substitute 7<->7 and 8<->8 since that was cheaper, leaving a residual
   within-chapter mismatch the directional rule didn't catch). Iterating
   per-discovered-case like this is itself a form of hand-curation (tuning the
   algorithm against specific verses read by eye) - flagged as such mid-session and
   abandoned in favor of one clean, general rule with no case-by-case exceptions.

**Shipped design - two levels, both content-free:**
1. **Chapter level first** (`align.AlignWeighted`): treat each book's own chapters
   as a sequence of verse-COUNT weights (not labels) and run a generalized
   edit-distance DP - substitute / insert / delete / 2:1-merge / 1:2-divide, cost =
   `|weight difference|` (0 for an exact size match). Chapter size is a far harder
   signal to coincidentally collide on than raw verse labels. This finds the real
   structure directly from parsed data with no lookup table: canonical Ps9(20)+
   Ps10(18) merging into edition's Ps9(39) costs 1 (the title verse) vs cost 30 for
   two independent substitutions; canonical chapter 2(32) dividing into edition
   chapters 2(27)+3(5) costs 0 (an exact size match) - both verified against the
   real corpus, matching the historically-documented Hebrew/LXX Psalm-numbering
   pattern Justin described, with no need to encode it as a literal table.
2. **Verse level, within each chapter-level correspondence - position/count only**
   (`align.FillGap`), **never verse-number label matching, even within an
   established chapter pair.** Equal counts pair 1:1 in order; unequal counts
   produce a merge/divide with confidence `1/groupSize`; a chapter-level
   insert/delete (eg Psalm 151) makes every verse in it a pure insertion/deletion -
   no row. `exact` vs `renumber` is a post-hoc label comparison on an
   ALREADY-DETERMINED pairing (informational), never the signal used to find the
   pairing.
- Schema (`relation`) narrowed from the original `{exact, renumber, merge, divide,
  title, moved}` to `{exact, renumber, merge, divide}` - `title` and `moved`
  require a signal (eg TVTMS's annotated Action types) this design doesn't have;
  the column still permits them for a future enhancement.
- **TVTMS is not read or consumed to derive the mapping at all** (a stronger
  position than the original spec's "seed/cross-check from TVTMS's simple rows").
  Once the chapter-size DP existed, TVTMS's corroboration role had nothing left to
  add to the *derivation*; it is used only as an independent, read-only scoring
  oracle post-hoc (see Validation below) - reading it to score agreement is not the
  same as consuming it to build the mapping.
- **Known, documented limitation (deferred, not fixed):** a within-chapter
  insertion whose position can't be derived from counts alone (the leading
  Psalm-title case) becomes a low-confidence (0.5) merge/divide bundling one verse
  with its neighbor, rather than "added content, no row" - the rest of the chapter
  still renumbers correctly. This is genuinely underdetermined by verse counts:
  nothing in the counts says the extra verse is a leading title vs a trailing
  addition. The 0.5 confidence reports this honestly; it is not a bug to chase
  with more heuristics. **Future fix:** consume TVTMS's `Psalm title`/`Renumber
  title` Action rows as a deterministic placement authority for this one pattern
  specifically (its Tests are booleans over verse counts already parsed - reading
  them as data for this narrow purpose, not building a general rule engine).
- **Validation (2026-06-30 real build, `lxx-*` vs canonical):**

  | Edition | Exact | Renumber | Merge | Divide | Canonical-only | Edition-only |
  |---|---|---|---|---|---|---|
  | lxx-brenton | 16,314 | 5,453 | 395 | 246 | 53 | 36 |
  | lxx-swete | 18,033 | 4,474 | 194 | 226 | 24 | 40 |
  | lxx-oss | 14,611 | 5,202 | 365 | 221 | 53 | 7 |

  Deterministic: two builds from identical input produce byte-identical
  `verse_alignment` rows (asserted in `versealign_test.go`'s
  `TestAlignIsDeterministic`, and confirmed by re-running the full build).
  **Independent oracle cross-check:** scored (not consumed) against TVTMS's own
  `SourceType=Greek` `Keep verse`/`Renumber verse` simple rows (a scratch,
  uncommitted script - reading TVTMS to score, not to build) - 269 of 521 rows
  were comparable (both refs in our loaded 66-book scope), with 96 agreeing
  (35.7% overall). This split sharply by book, which is itself informative:
  **Psalms agreed 83/87 (95.4%)** - strong independent confirmation on the book
  this design was hardest-won for. Several other books (Leviticus, Deuteronomy,
  Daniel, most of Exodus) showed near-zero agreement against this baseline -
  consistent with, not contradicting, the documented reason TVTMS was never
  consumed as a rule engine: TVTMS records SEVERAL distinct Greek sub-traditions
  per book (`Greek`, `Greek2`, `GreekUndivided`, `GreekIntegrated`,
  `GrkTitleSeparate`, ...), and this comparison scored against only the single
  `Greek` SourceType as a baseline; a book where Brenton follows a different
  TVTMS sub-tradition will show low agreement against the wrong baseline without
  our alignment being wrong. Reported as flagged data for future investigation,
  not chased into a per-book correction.
- Tests: `align/align_test.go` (`AlignWeighted`, `Anchors`, `FillGap`,
  `ProportionalAllocate` - generic, structural, synthetic) and
  `versealign/versealign_test.go` (boundary shift, insertion/deletion, merge/divide
  group_id sharing, the proportional chapter split, the leading-title-insertion
  regression case, determinism) - all synthetic fixtures shaped like the real
  patterns found, asserting aggregate counts/relations/confidence, never
  per-verse-content checks (that would itself be a step toward curation).

---

# Phase 2 - Lexical reference data (independent of verses)

### T5 - lexicon (TBESG + TBESH)  `DONE`
**Goal:** the Strong's lemma/definition dictionary the bridge joins to.
**Scope:**
- `lexicon` table: `dstrong (PK), estrong, ustrong, language, lemma, translit,
  gloss, definition, def_license`.
- Parse `STEPBible-Data/Lexicons/TBESG*.txt` (Greek) and `TBESH*.txt` (Hebrew):
  data rows are `eStrong | dStrong | uStrong | <lemma> | translit | morph | gloss | meaning`.
  One `lexicon` table for both, distinguished by `language`.
- Set `def_license` per row: Greek = "Abbott-Smith PD"; Hebrew = "BDB/Online Bible -
  permission" (the flagged sub-license; gloss is clean, long definition is flagged).
**Schema delta:** `lexicon` (PK `dstrong`; `ustrong` self-reference column for the synonym layer).
**Acceptance:** counts match non-header data rows of each file; `Lookup(dstrong)`
returns lemma+gloss; spot-check G0746=arche="beginning", a Hebrew entry; every row
has a `language` and a `def_license`; `ustrong` populated.
**Notes:** strip the long header (data starts well below). `uStrong` enables the
deterministic synonym layer later - do not collapse it.

### T6 - morph_codes (TEGMC + TEHMC)  `DONE`
**Goal:** human-readable expansions of morphology codes.
**Scope:** `morph_codes` table `(code PK, language, description)`; parse
`STEPBible-Data/Morphology codes/TEGMC*.txt` and `TEHMC*.txt`.
**Acceptance:** counts match; `Expand(code)` returns the description for a known
Greek (`V-IAI-3S`) and Hebrew (`Ncfsa`) code.
**Notes (as built):** the "FULL MORPHOLOGY CODES" table is the section actually
used by tagged texts (the file also has an earlier "BRIEF LEXICAL" table for
lexicon entries, skipped). Bare `Ncfsa` does not occur in TEHMC; codes there
carry a language prefix (`HNcfsa`, `ANcfsa`) - the acceptance example was
illustrative, not literal. 1,644 Greek + 921 Hebrew = 2,565 codes loaded.

---

# Phase 3 - Text import (FK to verses + sources)

### T7 - verse_text: KJV + ASV (JSON)  `DONE`
**Goal:** English prose, the editions the deliverables quote.
**Scope:** `verse_text` table `(id, verse_id FK, source_id FK, native_ref, text)`.
Parse `bible-text/KJV/KJV.json` and `ASV/ASV.json` (identical shape: books[].chapters[].verses[]).
Resolve book via `name-en`, verse via `verses.Resolve`.
**Schema delta:** `verse_text`.
**Acceptance:** KJV row count == verses count (31,102); Gen.1.1 / John.3.16 text
matches the file verbatim; every row resolves a `verse_id` and carries the right `source_id`.
**Notes (as built):** ASV.json's book names, chapter count, and verse count are
byte-identical to KJV.json's (verified: same 66 names, same 1,189 chapters, same
31,102 verses) - no book_names reconciliation was needed. Unlike crossrefs,
an unresolvable verse here is a hard error, not a skip: KJV/ASV define the
canonical spine, so a miss would mean the spine itself is wrong. 31,102 KJV +
31,102 ASV = 62,204 rows; Gen.1.1/John.3.16 spot-checked verbatim against the
built DB for both editions.

### T8 - verse_text: WEB (USFM)  `DONE`
Parse `bible-text/WEB/*.usfm` (`NN-XXXeng-web.usfm`). Strip `\w word|strong=...\w*`
to the bare word, drop `\f...\f*` footnotes and `\c/\v/\p` markers to recover prose.
Resolve book via `usfm` scheme. Skip front matter / glossary files (00-FRT, 106-GLO).
**Acceptance:** Gen/John prose matches after stripping; chapter/verse counts within
documented WEB versification; no USFM markup leaks into `text`.
**Notes (as built):** USFM needed more than the three markers named above to avoid
silent data loss. Poetry books (Psalms, Job, Proverbs, ...) use `\q1/\q2/\q3` line
breaks with no new `\v` - a verse's text is accumulated until the next `\c`/`\v`,
not read off one line, or every poetry verse after the first line would be
truncated. Also handled: `\+w`/`\+w*` (the words-of-Jesus word-wrapper variant,
used inside `\wj...\wj*`), `\qs...\qs*` (Selah, kept as text), `\x...\x*`
(cross-references, dropped like footnotes), and a `nonContentMarkers` skip-list
(`\id`, `\s1`, `\d` Psalm superscriptions, `\ip`, etc.) for lines with no verse
text. `\d` superscriptions (138 in Psalms) carry real canonical text but no `\v`
of their own in this edition - out of v1 scope, dropped rather than guessing a
verse-0 attachment.
Of 83 `*.usfm` files, 17 are front matter/glossary/deuterocanon and are skipped
(not an error) via `books.Resolve("usfm", code)` failing; all 66 canonical codes
present. Unlike T7, an unresolvable verse is counted as skipped, not a load
failure (invariant #4: WEB's own front matter documents real versification
divergences from the KJV-based canonical spine). Verified against the actual
divergences found: Luke 17:36, Acts 8:37/15:34/24:7 (Textus-Receptus-only
readings WEB relegates to footnotes) and Romans 16:25-27 (WEB relocates this
content to 14:24-26) - all 7 cross-checked against the front matter's own
textual notes, not bugs. 31,095 of 31,102 canonical verses loaded across all 66
books; zero markup leakage verified across every row; Gen.1.1/John.3.16/Psalm 3
(superscription + poetry + Selah) spot-checked against the built DB.

### T9 - verse_text: Brenton LXX (HTML)  `DONE`
Parse `bible-text/LXX/eng-Brenton_html/*.htm`. Extract `<span class="verse" id="VN">`
+ following text; strip footnote `<a class="notemark">`/`<span class="popup">` and the
bottom `.footnote` block.
**Load model (per the T4b decision above - this is what unblocked it):** load EVERY
Brenton verse into its OWN versification. Get-or-create
`verses(versification='lxx-brenton', book_id, chapter, verse)` and FK each `verse_text`
row to that row. Do NOT map to canonical here, and do NOT skip anything for lacking a
KJV target. `verse_id` stays NOT NULL (it points at the lxx-brenton row). Canonical
alignment is the separate deterministic T4b aligner, not this ticket.
**HTML extraction (confirmed format, salvaged from the paused attempt):**
`<div class="main">` ... `<div class="footnote">` boundary; strip inline
`<a class="notemark">` footnote markers; exclude `<div class='chapterlabel' id="V0">`;
book code from filename `XXXNN.htm`; book aliases `DAN`->`DAG`, `EST`->`ESG` (Brenton
ships only the Greek-expanded Daniel/Esther). Load DAG/ESG under the DAN/EST `book_id`
with `versification='lxx-brenton'`; their extra (deuterocanonical-addition) verses are
just lxx-brenton verse rows - added content, aligned later or never.
**Open question (do NOT block on it, do NOT hand-resolve it):** Brenton `EZR` is the
LXX's combined Ezra+Nehemiah (2 Esdras), and a separate `NEH` file also exists with its
own smaller divergence. Load Brenton EZR under its own book handling and leave the
2Es <-> {Ezr, Neh} identity to the T4b aligner / a book-identity note.
**Acceptance:** a known verse extracts clean (no HTML, no footnote markers); EVERY
Brenton verse is loaded as an lxx-brenton verse row (per-book counts match the audit
table's "LXX vrs" column - use those as expected totals); **zero skips**; every row
carries the Brenton `source_id` and `versification='lxx-brenton'`. No canonical mapping
is asserted here (that is T4b).
**Notes (as built):** the audit table's "LXX vrs" counts (and TVTMS's own row counts)
turned out to be inflated by something the audit didn't account for: **lettered
verse doublets**. Brenton prints some passages as split sub-verses (e.g. 1Ki.2's
"Miscellanies" insertion is labeled "35a"/"35b" in the rendered text) - and
critically, the `id="VN"` HTML attribute on those spans is just a sequential
anchor ID, NOT the printed verse number (`id="V36"` displays "35a", `id="V37"`
displays "35b", then `id="V36"` is reused for the real verse 36). Using the id
attribute as the verse number (the original plan) caused duplicate-key insert
failures on first run. Fixed by parsing the verse number from the span's own
displayed text instead, and concatenating same-numbered lettered parts into one
row (no sub-verse addressing in this schema) - confirmed deterministic and
mechanical, not a judgment call. This affects 68 of ~1,117 real chapter files
(~6%), heavily concentrated in Esther's Greek additions (ESG: audit's raw-span
count was 252, actual distinct-verse count is 164 - a difference entirely
explained by lettered doublets, not data loss; spot-checked file by file).
EZR.htm (the LXX's combined Ezra+Nehemiah) is skipped per the open question
above, via a `skipBooks` set - not re-litigated, exactly as scoped. Final
build: 22,690 Brenton verses across 920 chapter files, zero skips, zero markup
leakage (checked across every row), zero duplicate (verse_id, source_id) pairs;
Gen.1.1, the 1Ki.2 doublet merge, the DAG/ESG book aliasing, and Psalm 151
(loaded with no canonical counterpart, as designed) all spot-checked against the
built DB.

### T10 - words: TAGNT (Greek NT)  `DONE`
**Goal:** the workhorse tagged text and the foundation of complete-or-fail.
**Scope:** `words` table `(id, verse_id FK, source_id FK, word_no, surface, lemma,
dstrong FK, morph_code FK, attestation, editions, source_locator)`.
Parse the two `STEPBible-Data/.../TAGNT*.txt` TSVs (data starts ~line 95). Columns:
ref `Book.C.V#NN=Type` | Greek(+translit) | English | `dStrong=Morph` | `Lemma=Gloss` |
Editions | ... Resolve book via `dotted`, verse via `verses.Resolve`, dstrong via lexicon.
**Schema delta:** `words` (see variant note). **Do NOT** make `(verse_id, source_id,
word_no)` unique - variant readings share positions; the row key is `source_locator`.
Store `attestation` (N/K/O Type) AND `editions` (the NA28+...+TR+Byz list).
**Acceptance:** total word rows == data-row count of both files; a known variant
verse (Acts 8:37) loads as all-`K`/`TR` rows; John.1.1 word #2 = arche, lemma arche,
dstrong G0746; every word resolves verse + dstrong (or flags unresolved as data).
**Notes:** this ticket establishes the complete-or-fail contract used by T14 and Phase 4.
**Notes (as built):** "data starts ~line 95" undersold it - the file repeats its own
"Word & Type" column-header row AND a Greek/English/grammar preview block before
**every verse**, not just once at the top. The only reliable data-row filter is the
ref field's own shape (`Book.C.V#NN=Type`), checked per line throughout the whole
file, not a fixed skip-N-lines-then-read approach. `dstrong`/`morph_code` are plain
TEXT, not hard FKs (see schema comment): 5 of 5,575 distinct TAGNT dStrongs (315 word
rows) are absent from TBESG - a real STEPBible cross-file gap, confirmed not a parsing
bug. A rarer case (273 rows, 0.19%) is a single surface token spanning two Strong's
numbers (eg μήποτε = "G3361=PRT-N + G4218=PRT" / "μήποτε=lest + πότε=when") - both
columns get SQL NULL rather than a guessed split, counted and reported, not silent.
Verse resolution against the canonical KJV-based spine: **zero skips** across the
full NT (unlike WEB/T8, TAGNT's Greek-NT versification matches KJV's exactly).
Final: 141,720 words (66,931 Mat-Jhn + 74,789 Act-Rev, exact match to source data-row
counts), 273 compound, 0 unresolved verses. Acts.8.37 spot-checked as 23 all-K/TR
rows; John.1.1 word #2 spot-checked as surface=ἀρχῇ, lemma=ἀρχή, dstrong=G0746.

**T10 UPDATE (2026-06-30, found via T15 smoke-testing):** `refRe` required the ref
field's verse to be pure digits immediately before `#`, so any row whose verse field
carried a `(Chapter.Verse)` suffix - the file's own convention for where an edition's
verse split differs from the English/NRSV-standard one it's tagged against (e.g.
`Rom.3.25(3.26)`) - matched nothing and was dropped **before reaching any skip/insert
counter**, so `0 skipped` was true but incomplete: some rows were never counted at
all. 26 rows across both files (`Act.13.39(13.38)`, `Act.19.41(19.40)`,
`Rom.3.25(3.26)`, `Mrk.12.15(12.14)`). Fixed by tolerating an optional
`(?:\(\d+\.\d+\))?` before `#` and discarding it - the number outside the parens
(what the loader already resolved against) is unchanged. New final: **141,746**
words, still 0 skipped.

**T10 UPDATE (2026-06-30, found via live `concord_phrase` testing over MCP):**
STEPBible's TAGNT source stores lemma text in Greek Extended "oxia" accent form
(e.g. U+1F77), not the ordinary monotonic "tonos" form (U+03AF) a keyboard or LLM
normally produces - visually identical, byte-different, but canonically equivalent
under Unicode NFC. `concord.ConcordPhrase`'s exact-match `lemma = ?` comparison
silently returned zero rows for a real, adjacent, provably-correct phrase match
(Mrk.16.16's βαπτισθεὶς σωθήσεται) whenever the query was typed in the tonos form -
not an error, just an empty result indistinguishable from "doesn't occur." Fixed
with a new `lexnorm` package (`lexnorm.NFC`, wrapping `golang.org/x/text/unicode/norm`,
**approved dependency**) applied at two points: `dstrongLemma` normalizes `lemma`
before it's stored (this loader), and `concord.ConcordLemma`/`Count`/`ConcordPhrase`
normalize their query/token input before comparing. Both sides landing on the same
NFC form is what makes the comparison work regardless of which form either side
originally used. DB rebuilt with the fixed loader: same **141,746** words (a byte
-normalization, not a row-count, fix). Regression tests in `concord/concord_test.go`
(`TestConcordLemmaMatchesAcrossPolytonicAndMonotonicUnicodeForms`,
`TestConcordPhraseMatchesAcrossPolytonicAndMonotonicUnicodeForms`) and
`lexnorm/lexnorm_test.go` cover it directly.

### T11 - words: TAHOT (Hebrew OT)  `DONE`
Same `words` shape, from `STEPBible-Data/.../TAHOT*.txt`. Hebrew morphology, Aramaic
sections, **Ketiv/Qere** preserved (record both as data, do not collapse). Resolve book
via `dotted`, dstrong via lexicon (Hebrew).
**Acceptance:** counts match; Gen.1.1 word #1 in/be-, dstrong present; a Ketiv/Qere
verse keeps both readings; every row carries `source_id` for TAHOT.
**Notes (as built):** TAHOT's per-row shape is structurally different from TAGNT's,
not just a Hebrew re-skin. Hebrew words routinely carry an attached prefix morpheme
(preposition/article/conjunction - "in", "the", "and") joined to the root by "/" in
the dStrongs, Grammar, AND Expanded-Strong-tags columns alike, e.g. dStrongs
`H9009/{H7225G}` for "the/beginning" - braces mark which segment is the root (the
real lexical entry); the prefix has its own Strong's number but no independent
lemma. Per invariant #5 ("Hebrew may match by root"), this loader stores the
**braced** segment as dstrong/lemma, not the prefix - verified by direct audit
that the "/"-segment count and the braced-segment index line up 1:1 across all
283,734 rows in the corpus (zero mismatches), so the extraction is mechanical, not
a guess. There is no `editions` equivalent in TAHOT (no NA28/TR/Byz-style
edition-list concept for the Hebrew OT manuscript tradition) - left as empty
string, not NULL (the column stays `NOT NULL`).
**Ketiv/Qere, concretely:** confirmed it is NOT two separate rows per word - one row
carries the Qere reading with the Ketiv variant described in the Meaning Variants
text column, and the Type marker preserves the relationship verbatim (eg `Q(K)`,
never collapsed to a bare `Q`). 13 rows (out of 283,734) have no braced segment at
all - genuine untagged Qere readings confirmed in the source (an elided word with no
Ketiv-side lexical entry), not a parsing gap; dstrong/morph_code/lemma are SQL NULL
for those, counted and reported. 365 word rows reference a dStrong absent from
TBESH - the same kind of small, real STEPBible cross-file gap as T10's 5/315
(dstrong/morph_code stay plain TEXT, not hard FKs, for the same reason).
Final: 283,734 words (76,490+75,051+29,983+102,210, exact match to source data-row
counts across all four files), 13 untagged, 0 unresolved verses. Gen.1.1 word #1
spot-checked as surface=בְּ/רֵאשִׁ֖ית (verbatim), lemma=רֵאשִׁית, dstrong=H7225G (the
root, not the H9003 prefix), morph=Ncfsa; Gen.27.3's Ketiv/Qere verse spot-checked
in the built DB.

**T11 UPDATE (2026-06-30, found via T15 smoke-testing GetVerse/ResolveRef against
the real DB - Ps.9.1 showed `TAHOT: exists=false`, which is wrong; Psalms is a
covered OT book):** the same `refRe` gap as T10's update, at far larger scale. Every
entitled psalm's Hebrew verse-count runs one ahead of the English/standard verse it's
tagged against (the title is its own Hebrew verse 1, not separately numbered in
English - T11's own header, line 33: "Psalm Titles (v.0)"), so nearly every verse of
nearly every entitled psalm carried a `(Chapter.Verse)` suffix and matched nothing:
**21,440 rows dropped, uncounted, across all four TAHOT files** - `0 skipped` was
true but incomplete, the same way as T10, just three orders of magnitude larger.
Fixed with the identical regex tolerance. The title rows themselves (verse `0`, e.g.
`Psa.9.0(9.1)`) still correctly fail to resolve against the canonical spine (English
versification has no verse 0) and are now counted as skippedVerse, not silently
lost - `skipped` went from 0 to 478 (all Psalm-title rows). New final: **305,174**
words, 478 skipped (documented, not a regression), 14 untagged.

This is the strongest evidence yet for invariant #3/T14: a completeness self-test
proves rows aren't dropped only as far as its own checks reach. T14's book-level
`checkBookCoverage` correctly reported TAHOT covering Psalms (chapters 1, 2, 10, 11...
all had rows) and missed that chapter 9 specifically had zero - a chapter-level gap,
not a book-level one. Caught instead by T15's retriever surfacing a concrete,
individually-wrong answer (`Ps.9.1` "not present in TAHOT") during ordinary use, not
by a completeness assertion. Recorded as a known verify limitation, not fixed now
(scope discipline - see the closing note under T15 below): `checkBookCoverage`
currently proves no book is silently *empty*; it does not prove no *chapter* is.

**T11 UPDATE (2026-06-30):** `rootFields`'s extracted lemma now passes through
`lexnorm.NFC` before storage - same Unicode-normalization fix as T10's UPDATE above
(Hebrew source text has its own analogous polytonic/precomposed accent-and-pointing
variance); see T10's entry for the full rationale. Row counts unchanged.

### T12 - words: Swete LXX (Greek surface)  `DONE`
Parse `LXX-Swete-1930/01-Swete_word_with_punctuations.csv` (index -> surface) +
`00-Swete_versification.csv` (word-index -> ref). Build per-word rows with `surface`
only (lemma/dstrong/morph NULL - Swete carries none). Treat Swete text as Public
Domain (cite archive.org origin); do not ship the GPL CSV's transliteration -
regenerate if needed. 66-book scope only (skip deuterocanon).
**Acceptance:** Gen.1.1 has the right surface forms in order (epoiesen at position 3);
word count per verse matches the versification file's deltas; rows carry Swete `source_id`,
NULL lemma.
**Notes:** parallel per-source stream - NOT merged with OSS (see T13). Per the T4b
decision, Swete loads into its OWN versification (`versification='lxx-swete'`,
get-or-create verse rows, `verse_id` NOT NULL); no canonical mapping at load.
**Notes (as built):** the versification CSV only lists VERSE-START word indices
(sparse), not one row per word - a verse's word range is `[this row's index, next
row's index - 1]`, with the last entry running to end-of-file. Both CSVs are
confirmed index-ordered with no gaps (`NR == index` holds for the word file; the
versification file's indices are strictly ascending), so this is a mechanical
range computation, not a guess. Four Swete book codes don't match our canonical
`dotted` scheme (`Eze`->`Ezk`, `Joe`->`Jol`, `Nah`->`Nam`, `Sol`->`Sng`) - aliased
the same way T9's `DAG`->`DAN`/`ESG`->`EST` were; the other ~21 distinct codes in
the versification file are deuterocanon/extra-biblical (1 Enoch, Maccabees, Tobit,
Susanna, Odes, ...) and fall through `books.ErrUnknownBook` to a counted skip.
**Caught a self-deadlock during testing, not present in any prior loader**:
`store.Open` pins the connection pool to 1 (`SetMaxOpenConns(1)`), and this loader
calls `books.Resolve` inside the per-verse loop, AFTER `tx := db.Begin()` has
already claimed the single connection - the original code called
`books.Resolve(db, ...)` (against the pool) instead of `books.Resolve(tx, ...)`
(against the open transaction), which blocks forever waiting for a connection the
transaction is already holding. Fixed by passing `tx`. T9-T11 never hit this
because they resolve every book ONCE before opening the transaction; T12 resolves
book per verse-range row, inside the loop, which is what exposed it. Worth
auditing T13 for the same pattern before it's written.
Final: 476,937 words across the 66-book canonical scope (476,937 + 132,585
deuterocanon-skipped = 609,522, the word file's exact total - confirmed no word is
silently dropped, only correctly routed to "out of v1 scope"); 7,340 deuterocanon
verses skipped. Gen.1.1 word #3 spot-checked as ἐποίησεν (epoiesen); every Swete
row confirmed NULL lemma/dstrong/morph_code and `versification='lxx-swete'`.

### T13 - words: OSS LXX lemma  `DONE`
Parse `bible-text/LXX/GreekResources-master/LxxLemmas/<Book>.js` (JSON objects keyed
`Book.C.V` -> array of `{key, lemma}`). Build per-word rows with `lemma` only (surface
NULL). A separate stream from Swete - **do not assume word-position identity** (verified:
exact-count match Gen 74%, Daniel 58%). Cross-source lemma use joins at the *verse* level
until the T22 aligner exists.
**Acceptance:** Gen.1.1 lemma sequence matches the file (en, arche, poieo, ...); rows
carry OSS `source_id`, NULL surface; per-verse lemma counts logged for later alignment.
**Notes:** per the T4b decision, OSS loads into its OWN versification
(`versification='lxx-oss'`, get-or-create verse rows, `verse_id` NOT NULL). Each LXX
source (brenton/swete/oss) keeps its own versification tag; relating them is alignment
work, not load work.
**Notes (as built):** the 59-file `LxxLemmas/*.js` set turned out to need a real
scoping decision, not just a parse. Two recurring patterns required the same "don't
guess, skip and document" handling already established in T9's Brenton EZR skip:
- **Multi-recension witnesses** (`JoshA`/`JoshB`, `JudgA`/`JudgB`, `DanOG`/`DanTh`):
  direct inspection confirmed `JudgA`/`JudgB` are near-complete parallel full texts
  of Judges (617 of 618 verse keys overlap - two real competing witnesses), while
  `JoshA` is only a 96-verse fragment of divergent readings against `JoshB`'s
  complete 659-verse text - genuinely different situations, but neither has a
  mechanical "pick this one" rule. Both stay unloaded (invariant #9: no
  hand-curation) rather than silently choosing one recension as "the" text.
- **Deuterocanon/extra-biblical** (1-4 Maccabees, Baruch, Epistle of Jeremiah,
  Judith, Odes, Psalms of Solomon, Sirach, Susanna/Bel OG+Th, Tobit, Wisdom) and
  the combined-book `1Esd`/`2Esd` (`2Esd` = LXX's combined Ezra+Nehemiah, the
  same "2 Esdras" identity question T9 left open for Brenton's `EZR.htm`).
A `bookAlias` map of the 34 in-scope book tokens doubles as the scope allow-list:
anything not in it is a counted skip, not an error - so the 25 out-of-scope files
need no separate file-level skip logic, the per-key book-token lookup handles it
uniformly. Two tokens differ from their filename (`Eccl.js` keys are `Qoh.*`,
`Song.js` keys are `Cant.*` - confirmed by direct check that every other file's
key token equals its filename stem).
**Lettered sub-verse keys** (eg Greek Esther's heavy use of addition verses,
`Esth.1.1a`..`Esth.1.1s`) needed the same merge T9 applied to Brenton's lettered
doublets - concatenated in letter order into one verse row, since this schema has
no sub-verse addressing. Confirmed via direct corpus scan this pattern appears in
15 of the 34 loaded files (1Kgs, 1Sam, 2Chr, 2Kgs, 2Sam, Exod, Job, JoshB, Prov, Ps,
and others), not just Esther - genuinely common, not a one-off. Go's JSON decode
into a map does not preserve source key order, so letter order is reconstructed by
explicit string sort (`"" < "a" < "b"...`), not insertion order.
**One real anomaly surfaced and is reported, not guessed at**: `Jer.7.27/28` (a
combined-verse-range key, the only one in any of the 34 loaded files) does not
match the `Book.Chapter.Verse[letter]` shape and is counted as malformed rather
than parsed by a guessed split. A full-corpus scan confirmed exactly 45 such
malformed keys exist across all 59 files (mostly `Sir.Prolog.*` and similar in the
already-out-of-scope deuterocanon files) - the parser's malformed count matched
this hand-verified total exactly, confirming nothing is being silently misparsed.
**Caught the same self-deadlock class T12 found** (book resolution must run
against the open transaction `tx`, not the connection pool `db`, since
`store.Open` pins the pool to 1 connection) - fixed before it shipped this time,
by writing the resolve-inside-the-verse-loop code with `tx` from the start.
Final: 425,299 words across the 34 in-scope books; 9,699 out-of-scope rows
(multi-recension + deuterocanon, by design); 45 malformed keys (exact match to
the independently-verified full-corpus count). Gen.1.1 lemma sequence
spot-checked verbatim against the file (ἐν, ἀρχή, ποιέω, ...); Esther 1:1's 18
lettered addition-verse parts merged correctly in letter order.

**T13 UPDATE (2026-06-30):** each lemma passes through `lexnorm.NFC` before storage -
same Unicode-normalization fix as T10's UPDATE. Row counts unchanged.

---

# Phase 4 - Integrity

### T14 - completeness self-test  `DONE`
**Goal:** make invariant #3 enforceable, not aspirational.
**Scope:** a `verify` package + `cmd/build --verify` (or a `go test` integration tag)
that asserts over a freshly built DB:
- every `words`/`verse_text` row has a non-null `source_id`;
- every `verse_id`/`book_id`/`dstrong`/`morph_code` FK resolves (no orphans);
- no book in scope is silently empty;
- `Count(lemma) == len(ConcordLemma(lemma))` for a sample of lemmas (the agreement check);
- known per-edition verse totals match documented expectations.
**Acceptance:** the suite passes on a full build and fails loudly when a row is dropped
(prove with a deliberately corrupted fixture).

---

### T14 AS-BUILT (2026-06-30)

Shipped as the `verify` package (`Run(db, expectations) (Report, error)`) plus a
`cmd/build --verify` flag that runs it after a build and exits non-zero on failure.
`Report` separates hard `Issues` (fail the build) from informational `Notes` (small,
documented, pre-existing gaps that are reported but not failures) - matching invariant
#9's "surface the disagreement as data, don't smooth it over."

Checks implemented, each independently testable (see `verify/verify_test.go`, all five
tests corrupt a clean fixture and assert the loud failure the acceptance criterion
requires):
- `checkSourceIDs` - `words`/`verse_text` rows with a NULL `source_id` (belt-and-suspenders
  over the schema's own `NOT NULL`).
- `checkForeignKeys` - runs SQLite's own `PRAGMA foreign_key_check` over the whole DB, a
  second independent pass distinct from the insert-time `PRAGMA foreign_keys=ON` in
  `store.Open`.
- `checkBookCoverage` - for the sources documented as full-canon (KJV/ASV/WEB verse_text,
  TAGNT words over all 27 nt books, TAHOT words over all 39 ot books), asserts every book
  has at least one row. Brenton/Swete/OSS are edition-scoped subsets by design (T9/T12/T13)
  and are intentionally NOT held to full-canon coverage here.
- `checkLemmaAgreement` - T15/T16 (`ConcordLemma`/`Count`) don't exist yet, so this checks
  the same failure mode one level down: for a deterministic sample of `dstrong`s actually
  present (`ORDER BY dstrong LIMIT 25`, not random - invariant #9), an aggregate
  `COUNT(*)` must agree with a full row-scan count for the identical `WHERE` clause. Once
  T16 exists, that ticket should extend (not replace) this with the real `Concord`
  functions.
- `checkDstrongMorphResolution` - `words.dstrong`/`morph_code` are intentionally plain
  TEXT, not hard FKs (T10/T11 doc). Counts DISTINCT unresolved values (not row
  occurrences - one bad term can tag hundreds of word instances) against
  `lexicon`/`morph_codes`; up to `maxKnownDstrongGap` (40) is a Note, more is a failing
  Issue.
- `checkCounts` - `DefaultExpectations` records the real full-build totals (verses,
  cross_references, lexicon, morph_codes, and each edition's verse_text/words count) so a
  future dropped row shrinks a number and fails loudly instead of silently passing.

**A real bug T14 found and fixed (not curation - a general, deterministic parsing fix):**
running `--verify` against the real corpus surfaced 109,026 TAHOT `words` rows whose
`morph_code` didn't resolve against `morph_codes` (T6/TEHMC), while TAGNT was clean. Direct
corpus audit (all four TAHOT TSVs) showed the cause: TAHOT's Grammar column states the H
(Hebrew)/A (Aramaic) language marker exactly once, on the first `"/"`-segment of a
multi-segment field (e.g. `HR/Ncfsa` for a prefix+root word) - never restated on a later
segment (confirmed: 0 of 152,022 later segments start with `H`). `tahot.go`'s `rootFields`
picked the root's segment positionally but never re-attached that marker, so a root at
`idx>0` (the common case for any prefixed word) produced a bare code like `Ncfsa` that
TEHMC's own table only ever stores as `HNcfsa`. Fixed with a new `withLanguagePrefix`
helper that unconditionally prepends segment 0's language letter to any later segment
(an earlier draft tried to skip re-prefixing when the later segment "already looked
prefixed" - that was itself a second bug, since TEHMC's Adjective POS letter is also `A`,
so an unprefixed Adjective code like `Aampa` was misread as an already-Aramaic-prefixed
code; corpus audit confirmed 0 real re-prefixed later segments exist, so the rule is
unconditional, not conditional). After the fix, 0 TAHOT rows are morph-unresolved; a
table-driven unit test (`tahot/morphprefix_test.go`) pins both the Hebrew/Aramaic and the
Adjective-ambiguity cases. This is a parsing correctness fix discovered by a mechanical,
general completeness check - not per-verse hand-curation.

The `dstrong` gap is genuine, small, and matches T10's already-documented figure exactly
once measured correctly (by distinct term, not row count): 5 distinct TAGNT dStrongs (T10's
documented number) + 20 distinct TAHOT dStrongs absent from lexicon, reported as a Note.

**Validated real-build Counts (2026-06-30, run twice, byte-identical):**
```
verify NOTE [dstrong_resolution]: 25 distinct dstrong values are absent from lexicon (within documented gap)
verify: OK (all completeness checks passed)
```

---

# Phase 5 - Retriever engine (the Concord tool surface)

Read-only. Every call returns `Citation`-bearing results (Concord spec §5). Implement
against the built DB; the engine is the deterministic reference monitor.

**Phase 5 is the single query seam.** T15-T19 are the engine's public API, and T25
(below) aggregates them into one facade package. Every access surface in Phase 6
(MCP, CLI, HTTP+web, desktop) imports that facade and nothing lower - no transport
touches `store`, `verses`, `words`, or raw SQL directly. That keeps complete-or-fail
(invariant #3) and provenance (#2) enforced in exactly one place, and blocks the
lexica anti-pattern (a transport building its own SQL) structurally, not by
convention. The engine is the reference monitor; the transports are dumb pipes.

### T15 - Citation + reference resolution  `DONE`
`Citation` type (ref, edition, verbatim text, source_file+locator, lemma?, dstrong?,
morph?, attestation?, confidence, caveat). `ResolveRef`, `GetVerse`, `GetPassage`.
**Acceptance:** GetVerse returns verbatim per-edition text with provenance; a ref that
diverges across editions returns per-edition addresses + caveats, never a silent shift.

### T15 AS-BUILT (2026-06-30)

Shipped as the `retriever` package: `Ref` (USFM book code + chapter + verse, always
canonical/edition-neutral), `Citation`, `Address`, `Resolution`, `Confidence`
(`High`/`Flagged`), and `ResolveRef` / `GetVerse` / `GetPassage`.

**Two edition-reaching strategies, matching how each source was actually loaded:**
canonical-keyed editions (KJV/ASV/WEB verse_text, TAGNT/TAHOT words) were loaded by
resolving directly against the canonical spine (T7/T8/T10/T11), so a canonical Ref's
`verses.id` IS their key - no lookup needed beyond `WHERE verse_id = ?`.
Alignment-keyed editions (Brenton verse_text, Swete/OSS words - T9/T12/T13) own their
own versification entirely and are reached only through T4b's `verse_alignment` table,
never a verse-number guess. `ResolveRef` reports on all 8; `GetVerse`/`GetPassage`
serve only the 4 verse_text (prose) editions - TAGNT/TAHOT/Swete/OSS are word-tagged
streams, not prose, and are T17's (Parse/Lemmatize) surface, not GetVerse's.

**"Never a silent shift" (the acceptance criterion), concretely:** an edition with no
counterpart for a Ref - whether it's simply missing (a WEB skip) or has no T4b
alignment (a genuine LXX/Hebrew divergence) - still produces one `Address`/`Citation`
for that edition, `Exists:false` / `Confidence:Flagged`, with a `Caveat` naming why.
Nothing is silently omitted from the result set. A T4b `merge`/`divide`/`renumber`
relation produces `Confidence:Flagged` with a `Caveat` naming the relation and
confidence - `exact` is the only relation that yields `Confidence:High` with no
caveat. Proven with a fixture shaped like the real Ps9/10 Hebrew/LXX divergence
(`retriever_test.go`): canonical Ps.9.1 aligned to Brenton via a `divide`/`renumber`
relation (not `exact`) returns a real, correctly-addressed Brenton verse, Flagged,
with the relation named in the Caveat - never a coincidentally-matching wrong verse
presented as if it were exact.

**A real bug T15 found (not a T15 bug - a T10/T11 loader bug), the same shape twice:**
smoke-testing `ResolveRef(PSA.9.1)` against the real built DB showed `TAHOT:
exists=false` - wrong, Psalms is a fully in-scope OT book. Traced to `refRe` in both
`tagnt.go` and `tahot.go`: the ref field's verse must be pure digits immediately
before `#`, but STEPBible's own convention appends an optional
`(EditionChapter.EditionVerse)` cross-reference whenever a file's own verse split
differs from the English/NRSV-standard verse it's tagged against - a row carrying
that suffix matched nothing and was silently dropped **before reaching any
skip/insert counter**, so both loaders' `0 skipped` claims were true but incomplete.
TAGNT: 26 rows (tiny - occasional edition-split differences like `Rom.3.25(3.26)`).
TAHOT: 21,440 rows (huge - Hebrew numbers a psalm's superscription as its own verse
1, so nearly every verse of nearly every entitled psalm carried the suffix). Fixed
identically in both: `refRe` now tolerates `(?:\(\d+\.\d+\))?` before `#` and
discards it, keeping the English/standard number outside the parens unchanged (see
T10/T11 UPDATE blocks above for the full trace and new totals). This is why T15
matters as a real integration test, not just new code: it exercised real cross-
edition joins T10/T11's and T14's own unit/completeness tests never happened to hit.

**Known verify limitation surfaced, deliberately not fixed here:** T14's
`checkBookCoverage` proves no book is silently *empty*; it does not prove no
*chapter* within a covered book is. Psalm 9 had zero TAHOT rows while the book-level
check passed clean (other Psalm chapters had rows). A chapter-level coverage check
would have caught this without needing T15 to stumble into it. Recorded as a T14
follow-up, not built now - scope discipline: T15's job was the retriever, not
re-opening T14.

### T16 - concordance (the killer feature)  `DONE`
`ConcordLemma(lemma|dstrong)`, `ConcordPhrase(tokens, {adjacent|window:N})`, `Count`.
**Complete-or-fail**: return every matching word row or raise. `Count` and `Concord`
over the same query MUST agree (built-in completeness check).
**Acceptance:** `ConcordLemma(G0859)` returns all aphesis rows incl. the Matt 26:28
control case; `ConcordPhrase(["eis","aphesis"], adjacent)` returns the full NT set;
`Count == len(Concord)` for every tested query; a forced partial read raises.

### T16 AS-BUILT (2026-06-30)

Shipped as the `concord` package: `ConcordLemma`, `ConcordPhrase`, `Count`, all
returning/tallying `retriever.Citation`/`Tally` over one `corpus` (a sources.code
restricted to the four word-tagged sources: TAGNT, TAHOT, Swete, OSS-LXX-lemma -
KJV/ASV/WEB/Brenton carry verse_text, not words, and are rejected with a clear error
rather than silently returning zero matches).

**`lemma | strongs` auto-detection:** `ConcordLemma`'s single `query` argument is
classified by shape (`^[GH]\d{2,5}[A-Za-z]{0,2}$` = dStrong, e.g. `G0859`; anything
else = a literal `lemma` match) rather than a second parameter - matches the spec's
`ConcordLemma(lemma | strongs, corpus)` signature exactly. Verified both paths return
the identical set for the same underlying word (`ConcordLemma("G0859", "TAGNT")` and
`ConcordLemma("ἄφεσις", "TAGNT")` agree).

**Complete-or-fail, concretely:** both `ConcordLemma` and `ConcordPhrase` run an
independent `COUNT(*)` before scanning rows, then call `checkComplete` to compare the
counted total against what was actually scanned - a real driver-level partial read
(not just "zero matches", which is a legitimate answer) raises instead of returning a
truncated slice. `Count` shares the identical `WHERE` clause, so `Count(...).Total ==
len(ConcordLemma(...))` holds by construction; `checkComplete`'s guard logic itself is
unit-tested directly (`concord/complete_test.go`) since forcing a real SQLite driver
to truncate mid-scan isn't a reproducible test scenario - the guard existing and
firing correctly on disagreement is what's provable and proven.

**`ConcordPhrase`'s window semantics:** one `window int` parameter, not a separate
adjacent/window mode - `window=0` means strictly adjacent (consecutive `word_no`,
the spec's `{adjacent}` case), `window=N` allows up to `N` intervening words. Phrase
matching never crosses a verse boundary (`word_no` is verse-relative in the source
data itself - T10/T11's own package docs). Ties are broken deterministically: the
nearest subsequent token match is taken, not enumerated as multiple branches.

**Two edition-reaching strategies, reused from T15:** canonical-keyed corpora
(TAGNT/TAHOT) attach a word's own verse chapter/verse directly as the Citation's
`Ref`. Alignment-keyed corpora (Swete/OSS-LXX-lemma) resolve `Ref` through T4b's
`verse_alignment` (`retriever.IsAlignmentKeyed`, exported from T15 for this reuse) -
`exact` relations get `Confidence:High`, anything else gets `Confidence:Flagged` with
a `Caveat` naming the relation, and a `merge`-target edition verse (multiple canonical
verses collapsing into one edition verse) picks the first canonical verse
deterministically and says so in the `Caveat`, since word-level alignment (T22) that
would resolve which canonical verse a given WORD belongs to doesn't exist yet and
isn't guessed at - the same "report low-confidence, don't guess" discipline as T4b's
own residual limitation.

**Validated against the real DB, matching the spec's own worked example (§6) exactly:**
`ConcordLemma("G0859", "TAGNT")` returns **17** citations (the spec's own stated
count) including `MAT.26.28` (the control case: Christ's blood poured out εἰς ἄφεσιν -
a causal "because of" reading is structurally impossible there); `Count("G0859",
"TAGNT").Total == 17` agrees. `ConcordPhrase(["εἰς","ἄφεσις"], "TAGNT", 0)` (adjacent)
returns 5 matches: `Mat.26.28`, `Mrk.1.4`, `Luk.3.3`, `Act.2.38`, `Luk.24.47` - the
full adjacent-occurrence set.

**T16 UPDATE (2026-06-30, found via live MCP `concord_phrase` testing against real
scripture-study questions):** `ConcordLemma`/`Count`'s `query` and `ConcordPhrase`'s
`tokens` are now passed through `lexnorm.NFC` before comparison, matching the
now-NFC-normalized `words.lemma` column (T10/T11/T13 UPDATEs above). Before this fix,
a lemma-text query typed in the ordinary monotonic Unicode form (what a keyboard or
LLM produces) silently returned zero rows against the corpus's polytonic-accented
storage form - not an error, and not distinguishable from a real "doesn't occur"
result, which is exactly the kind of silent gap invariant #3 exists to prevent
everywhere else. Confirmed live: `ConcordPhrase(["βαπτίζω","σῴζω"], "TAGNT", 40)`
(typed in the ordinary tonos form) now correctly finds the sole NT verse where these
two lemma families co-occur (Mrk.16.16, adjacent, `βαπτισθεὶς σωθήσεται`), and
`ConcordPhrase(["σῴζω","βάπτισμα"], "TAGNT", 40)` finds the only other one
(1Pe.3.21, `σῴζει βάπτισμα`) - both zero-result before the fix. Regression coverage:
`concord/concord_test.go`'s `TestConcordLemmaMatchesAcrossPolytonicAndMonotonicUnicodeForms`
/ `TestConcordPhraseMatchesAcrossPolytonicAndMonotonicUnicodeForms`, plus the new
`lexnorm` package's own tests.

### T17 - parse / lemmatize  `DONE`
`Parse(ref, word?)` (dstrong + expanded morph), `Lemmatize(ref)` (ordered lemma list).
**Acceptance:** Parse returns morph_code expansion via T6; LXX parse flagged (Swete has none).

### T17 AS-BUILT (2026-06-30)

Shipped as the `parse` package: `Parse(db, ref, word *int, corpus)` and
`Lemmatize(db, ref, corpus)`, both over the same four word-tagged corpora as T16
(reusing `retriever.IsWordCorpus`/`IsAlignmentKeyed` - no third copy of that table).
`word` is a `*int` (nil = every word in the verse, non-nil = one 1-based `word_no`),
matching the spec's `word?` optional-argument shape in an idiomatic Go way.

**Morph expansion, concretely:** for each word, `morph_code` is looked up in T6's
`morph_codes` table (scoped by corpus's language: `grc` for TAGNT/Swete/OSS, `he` for
TAHOT) and `Citation.Grammar` is set to `"<code> (<description>)"` - both the raw
code and its expansion, so nothing is lost by expanding it. An unresolved code (T14's
documented small cross-file gap) falls back to the raw code alone, `Confidence:
Flagged`, with a `Caveat` naming it as a known gap rather than a silent success.

**"LXX parse flagged," concretely:** Swete carries no `morph_code` at all (T12,
surface-only) and OSS-LXX-lemma carries no `morph_code` either (T13, lemma-only) - a
word from either always comes back `Confidence:Flagged` with a `Caveat` naming
exactly what that edition doesn't carry and why (not a generic "missing data"
message). The word itself (surface or lemma, whichever the corpus actually has) is
still returned - Parse never omits real data because ancillary data is absent.

**Reused, not reinvented, T15/T16's alignment handling:** alignment-keyed corpora
(Swete/OSS) resolve via `verse_alignment` the SAME direction T16's Parse-shaped case
needed (canonical -> edition, not the reverse ambiguity T16's `ConcordLemma` had to
navigate) - a canonical ref under a `divide` relation maps to multiple edition
verses, and Parse correctly returns the union of all their words in order, each
tagged with its own edition chapter/verse in the Caveat. A non-`exact` relation
(`renumber`/`merge`/`divide`) is `Confidence:Flagged` regardless of morph
availability, same discipline as T15/T16.

**`Lemmatize` as a filtered view, not a separate query:** `Lemmatize(ref, corpus)`
calls `Parse(ref, nil, corpus)` and returns only the Citations that carry a lemma -
documented as intentional (a word truly has nothing to contribute to a lemma list
without one, e.g. a TAGNT compound word, an untagged TAHOT Qere reading, or any
Swete row), not a completeness violation of invariant #3 (which governs not dropping
a MATCHING row, not inventing data that isn't there).

**Validated against the real DB:** `Parse(MAT.26.28, nil, "TAGNT")` returns all 17
words with full dStrong + expanded grammar (e.g. `N-ASF (Function=Noun; Case=
Accusative; Number=Singular; Gender=Feminine)` for ἄφεσιν), all `Confidence:High`.
`Lemmatize` on the same ref returns the ordered lemma list. `Parse(PSA.9.1, nil,
"Swete")` returns real Greek words (Εἰς, τὸ, τέλος, ...) each `Confidence:Flagged`
with a caveat naming BOTH the T4b `divide` relation AND Swete's lack of morph_code.
`Parse(GEN.1.1, &3, "TAHOT")` resolves the third word's Hebrew morph_code cleanly
(`HNcmpa`) via the Hebrew-language T6 table.

### T18 - attestation  `DONE`
`Attestation(ref, word?)` -> the Type/Editions columns as neutral text-critical data
(e.g. Mark 16:9-20 = KO). No argument, just data.

### T18 AS-BUILT (2026-06-30)

Shipped as the `attestation` package: `Attestation(db, ref, word *int, corpus)`,
same per-corpus/optional-word shape as T17's `Parse`. Populates only
`Citation.Attestation` (the Type marker: `NKO`, `KO`, `Q(K)`, etc.) and
`Citation.Editions` (`NA28+TR+Byz`-style edition list) - deliberately does NOT touch
`Grammar`/morph expansion (that's T17's concern, kept separate so a caller asking
purely "what manuscripts carry this word" isn't handed morphology noise).

**No argument, just data, concretely:** a `KO` Type (Traditional/Other manuscripts,
absent from the Nestlé-Aland base text - exactly Mark 16:9-20's real shape) is
`Confidence:High` with no `Caveat` - the variant is neutral, reportable data, not a
defect to flag. The only things that DO get flagged here are structural: a non-exact
T4b alignment relation (same discipline as T15-T17), or a corpus that carries no
attestation apparatus at all (Swete/OSS-LXX-lemma, T12/T13 - neither has a
Greek-manuscript-tradition concept, so `attestation` is always empty by design; that
absence is itself named in the `Caveat`, not silently returned as an empty string
with no explanation).

**A third Rule-of-Three extraction, done proactively:** T18 is the third ticket
(after T15, T17) needing "map a canonical Ref to an edition's own verse(s) via T4b's
verse_alignment, forward direction." Rather than write a fourth private copy,
`retriever.ResolveEditionVerses`/`retriever.AlignedVerse` were extracted from T17's
now-removed `parse.resolveTargets`/`parse.target` into `retriever.go` as the shared,
exported primitive, and T17's `parse.go` was refactored to call it too (verified:
all of T15-T17's existing tests still pass unchanged after the refactor). T15's own
`GetVerse`/`ResolveRef` internals were deliberately left as-is - they predate this
extraction and are already shipped/tested; refactoring them for symmetry alone was
judged not worth the regression risk.

**Validated against the real DB, matching the acceptance criterion exactly:**
`Attestation(MRK.16.9, nil, "TAGNT")` and `Attestation(MRK.16.20, nil, "TAGNT")` -
every word in both verses is Type `KO`, `Confidence:High`, no caveat. An ordinary
verse (`MRK.1.1`) is predominantly `NKO` by contrast, confirming the distinction is
real, not an artifact of the query.

### T19 - Cite renderer  `DONE`
`Cite([]Citation) -> string` in the `Teaching/Studies/*-references.md` format - the only
sanctioned bridge from engine output to a study deliverable.

### T19 AS-BUILT (2026-06-30)

**Scope correction, made before writing any code:** a real `Teaching/Studies/
*-references.md` (`baptism-salvation-references.md` read in full) is a hand-composed
document - thematic headings, "take-away" summary lines, an analytical comparison
table, selective bolding of the words that matter to the argument. None of that is
mechanically derivable from a `Citation` slice; the spec's own §7 ("Deliberately not
in the retriever") says exactly this is the analysis layer's job. `Cite`'s real,
narrower, buildable scope - matching §4E precisely - is rendering EACH `Citation`
into one quoted, fully-attributed Markdown bullet, the raw material a human/LLM then
arranges into a document like that one. `Cite` does not compose the document; it is
what a document-composer pastes in.

Shipped as the `cite` package: `Cite(citations []retriever.Citation) string`, exactly
the spec's signature (no DB argument - purely a string transform over data already
fetched). One line per Citation:

```
- **REF** (Edition) — "verbatim text" [metadata] (source: file locator) *(caveat)*
```

Every clause is conditional on the Citation actually carrying that data - metadata
(`DStrong, Lemma, Grammar, Type=Attestation, Editions`, in that fixed order, only the
non-empty ones) never appears for a bare verse_text Citation; a placeholder "nothing
here" Citation (T15-T18's `Confidence:Flagged`+`Caveat`-only rows) still renders one
complete, honest line, never a blank or malformed one. `Ref.String()` (the dotted
USFM form, e.g. `PSA.9.1`) is used as-is - `Citation` doesn't carry a full English
book name, and `Cite` takes no DB argument to look one up; renaming books for prose
is exactly the kind of decision left to the analysis layer composing the final
document.

**Validated against the real DB, chained through T16's own concordance:**
`Cite(ConcordPhrase(["εἰς","ἄφεσις"], "TAGNT", 0))` renders the full 5-occurrence
adjacent set as five ready-to-paste bullets, each with its real Greek text, lemma
metadata, and exact source file + locator - a direct, working example of the
retriever→citation→deliverable pipeline the whole engine exists to guarantee.

**Phase 5 is now complete (T15-T19).**

---

# Phase 6 - Access surfaces (shared seam + transports)

Every transport here is a thin adapter over the T25 facade. None owns query logic,
SQL, or completeness enforcement - that all lives in Phase 5. Build the seam (T25)
first; MCP / CLI / HTTP / desktop then fall out cheaply and identically.

### T25 - `engine` facade (the shared seam)  `DONE`
**Goal:** one read-only Go package that every transport imports - the single seam,
so a front-end adds a UI, never a second copy of the query rules.
**Scope:**
- New `engine` package: `Open(dbPath string) (*Engine, error)` opens the built DB
  **read-only** (`store.Open` in a read-only mode; reject writes at the connection).
- Methods delegate 1:1 to Phase 5: `ResolveRef`, `GetVerse`, `GetPassage`,
  `ConcordLemma`, `ConcordPhrase`, `Count`, `Parse`, `Lemmatize`, `Attestation`,
  `Cite`. All return the Phase-5 `Citation`-bearing types unchanged.
- The facade is the **only** symbol transports import. `store`/`verses`/`words` etc.
  stay internal to it; a transport that needs SQL is a design failure, not a feature.
**Schema:** none.
**Acceptance:** every Phase-5 operation is reachable through `engine` alone;
`Count(q) == len(Concord(q))` holds *through the facade* (not just under it); a write
attempted on the opened DB fails; the package imports no transport and exposes no
`*sql.DB`. A `grep` for `database/sql` or `store.` outside `engine`/loaders is empty.

### T25 AS-BUILT (2026-06-30)

Shipped as the `engine` package: `Open(dbPath) (*Engine, error)`, `Close()`, and one
method per Phase 5 operation (`ResolveRef`, `GetVerse`, `GetPassage`, `ConcordLemma`,
`ConcordPhrase`, `Count`, `Parse`, `Lemmatize`, `Attestation`, `Cite`), each a direct
1:1 delegation to its Phase 5 function with `e.db` filled in. `Engine.db` is
unexported and has no accessor - the type system, not just convention, prevents a
transport from reaching a `*sql.DB`.

**Read-only in two independent layers, confirmed empirically before writing the
package (not assumed from docs):** the SQLite URI `mode=ro` parameter
(`file:<path>?mode=ro`) refuses the connection outright if the file needs write
access, and `PRAGMA query_only = ON` is a second, statement-level guard. Verified
against the real corpus DB: an `INSERT` through either layer alone fails with
`attempt to write a readonly database (8)`. `TestOpenRejectsWrites` (a white-box
test in package `engine`, the only place that can legally reach the unexported `db`
field) proves this holds through `Open`'s actual code path, not just the modernc.org/
sqlite driver in isolation.

**Acceptance validated:**
- Every Phase-5 operation reachable through `engine` alone - `TestEngineReaches
  EveryPhase5Operation` calls all ten.
- `Count(q) == len(ConcordLemma(q))` holds through the facade -
  `TestCountAgreesWithConcordLemmaThroughFacade`, and confirmed again against the
  real DB (`ConcordLemma("G0859","TAGNT")` = 17, `Count(...).Total` = 17, both
  reached only through `Engine`, never `retriever`/`concord` directly).
- A write attempt fails - `TestOpenRejectsWrites`.
- No `*sql.DB` escapes the package - enforced by Go's own unexported-field rule, not
  a lint check.

The `grep for database/sql or store. outside engine/loaders` acceptance line is a
standing discipline for T20/T26/T27/T28 (none exist yet, so there's nothing to check
today) rather than a test built here - each transport ticket must self-verify this as
it's written, not defer it to a later audit.

### T20 - MCP surface  `DONE`
Expose the `engine` facade tools over MCP (read-only). Natural-language queries are
allowed but MUST route to the deterministic facade methods
(`ConcordLemma`/`ConcordPhrase`), never generate raw SQL (the lexica anti-pattern).
The MCP server is the engine; the LLM client is the analysis layer. Tool definitions:
provider = latest Claude per `D:\Claude\Bible` API guidance if a client is built.
**Acceptance:** each tool callable over MCP returns Citation-bearing JSON; completeness
guarantees hold across the boundary; the server imports `engine` only.

### T20 AS-BUILT (2026-06-30)

**Dependency (user-approved before writing any code):** the official
`github.com/modelcontextprotocol/go-sdk` (v1.6.1, Anthropic + Google), chosen over
the community `mark3labs/mcp-go` and hand-rolling the protocol - offered as a
3-option choice, user picked the official SDK.

Shipped as `cmd/orthotomeo-mcp`: `main.go` opens the built DB via `engine.Open` and
runs an `mcp.Server` on `mcp.StdioTransport`; `tools.go` registers one
`mcp.AddTool[In, Out]` per `Engine` method (`resolve_ref`, `get_verse`,
`get_passage`, `concord_lemma`, `concord_phrase`, `count`, `parse`, `lemmatize`,
`attestation`, `cite`) - ten tools, every Phase-5 operation reachable, none building
SQL. Each handler is a one-line delegation: unmarshal typed args, call the matching
`Engine` method, return the result - the SDK's generic `ToolHandlerFor` auto-
validates input against the inferred JSON Schema (from `jsonschema` struct tags) and
auto-populates both `StructuredContent` and human-readable JSON text from a non-nil
`Out` value, so no handler builds `CallToolResult` by hand.

**"Server imports engine only," verified, not assumed:** `grep -rn "database/sql\|orthotomeo/store" cmd/orthotomeo-mcp/` returns nothing outside a doc comment.

**A real MCP-spec constraint discovered while wiring this up:** the spec requires an
object-typed output schema; a bare `[]retriever.Citation` is a JSON array, and
`mcp.AddTool` panics at registration (`"output schema must have type object"`) if
`Out` isn't a map or struct. Every tool returning a Citation slice wraps it in a
one-field `citationsResult{Citations []retriever.Citation}` instead. Caught and
fixed by actually registering the tools and running the test suite, not by reading
the SDK source first - the panic message was immediate and exact.

**JSON field names:** `retriever.Ref/Address/Resolution/Citation` and
`concord.Tally` gained `json:"..."` struct tags (snake_case, `omitempty` on the
Citation fields that are legitimately absent for many rows - `Lemma`, `DStrong`,
`Grammar`, `Attestation`, `Editions`, `Caveat`) as part of this ticket - the first
time these types cross an external JSON boundary, so the first time their wire
shape mattered. Purely additive; no existing Go code depended on the previous
(unset, default Go-field-name) JSON marshaling.

**Validated three ways, escalating in realism:**
1. In-process: `mcp.NewInMemoryTransports()` wiring a real `mcp.Client` to a real
   `mcp.Server` (this package's actual `registerTools`) in one test binary - 7 tests
   in `cmd/orthotomeo-mcp/mcp_test.go`, including `Count(q) == len(concord_lemma(q))`
   held *across the wire* (JSON round-tripped, not just Go values compared
   in-process) and a `cite` call chained from a live `concord_lemma` result.
2. Real subprocess: built the actual `orthotomeo-mcp` binary, launched it via
   `mcp.CommandTransport` (`exec.Command`) exactly as a real MCP host would, against
   the real corpus DB - `tools/list` returned all 10 tools; `concord_lemma("G0859",
   "TAGNT")` returned the real 17-citation JSON set over a live stdio pipe.
3. Input validation: `parse` with `word=0` (invalid, 1-based) is rejected as a tool
   error (`IsError:true`) rather than silently matching nothing.

Standing discipline carried forward from T25: future transports (T26 CLI, T27
HTTP+web, T28 desktop) must self-verify the same `engine`-only import constraint as
they're written.

**T20 UPDATE (2026-07-01, found via real-world use through Claude Desktop):**
`get_verse`, `get_passage`, `concord_phrase`, and `cite` each take a required Go
slice argument (`Editions`/`Tokens`/`Citations []retriever.Citation`). `jsonschema-go`'s
default reflected schema for a slice field is a nullable union - `"type": ["null",
"array"]`, so a nil Go slice also validates - but the real MCP client this server
was registered with (Claude Desktop/Code) doesn't parse a `"type"` that's an array of
strings: it silently treated the property as untyped and rejected a real array
argument, with no error at server-registration time to catch it (`mcp.AddTool` only
panics on a structurally invalid schema, not a spec-valid one an external client
happens not to support). Fixed with a new `schemaFor[T]()` helper (`tools.go`) that
computes the reflected schema, then collapses any such union down to plain `"array"`
- a required field never needs to also accept a literal JSON `null`, since `required`
already demands the property be present with a real value - and wires the result in
via each affected tool's `InputSchema` field (bypassing `mcp.AddTool`'s automatic
reflection, which has no hook to customize it). Regression coverage:
`TestArraySchemaFieldsAdvertiseArrayNotUnionType` in `mcp_test.go` asserts, over a
real `ListTools` call (not just that our own SDK-based test client tolerates the
union - it does, since it's the same library on both ends), that every one of these
four properties advertises a plain string `"type":"array"` with `"items"` present -
verified to fail with the pre-fix schema by temporarily reverting one `InputSchema`
override and confirming the test catches it.

### T26 - CLI adapter  `DONE`
**Goal:** the thinnest human/scriptable surface, and the facade's first smoke test -
if the CLI is awkward, the API is awkward.
**Scope:** `cmd/orthotomeo` with subcommands over `engine`: `lookup <ref> [--edition]`,
`concord <lemma|dstrong> [--phrase <tokens> --window N|--adjacent]`, `parse <ref>`,
`attest <ref>`. Stdlib `flag` only (no cobra dep). Text output by default, `--json`
emits the exact `Citation` payload the HTTP surface (T27) reuses. Opens a prebuilt DB
read-only; never triggers a build.
**Acceptance:** `orthotomeo concord G0859` prints every aphesis row (incl. the Matt
26:28 control) with citations; `--json` output is byte-identical to T27's for the same
query; a forced partial read exits non-zero (complete-or-fail surfaces as an error,
not a short list).

### T26 AS-BUILT (2026-07-01)

Shipped as `cmd/orthotomeo`: four subcommands (`lookup`, `concord`, `parse`,
`attest`), each a direct delegation to one `engine.Engine` method, stdlib `flag`
only - one `flag.NewFlagSet` per subcommand, dispatched by `os.Args[1]` in `main.go`.
No cobra, no third-party CLI framework.

**Text vs `--json` output, one shared path:** every Citation-bearing subcommand
calls a single `emit` helper - default output is `engine.Cite(citations)` (Markdown
bullets, reusing T19's renderer rather than building a second formatter), `--json`
marshals a `citationsPayload{Citations []retriever.Citation}` wrapper. The wrapper
(object, not a bare array) is the same shape `cmd/orthotomeo-mcp/tools.go`'s
`citationsResult` already uses for the identical reason (MCP's object-typed-output
constraint) - T27's HTTP JSON is specified to reuse this shape byte-for-byte, so
fixing it here now means T27 has one less design decision to make, not a new one.

**Ref syntax:** a small `parseRef` (`cmd/orthotomeo/ref.go`) parses the CLI's
"BOOK.CHAPTER.VERSE" argument (e.g. `MAT.26.28`) into a `retriever.Ref` - the same
dotted shape `retriever.Ref.String()` already produces, so a ref printed by one
command round-trips as input to another. Book code is upper-cased for typing
convenience; chapter/verse must be plain integers or the command errors out (not a
guessed parse).

**stdlib `flag` ordering caveat, worth documenting since it surprised manual
testing:** flags must precede the positional argument (`concord --corpus TAGNT
G0859`, not `concord G0859 --corpus TAGNT`) - `flag.Parse` stops interpreting `-`
tokens once it hits the first non-flag argument. Not a bug, just how the stdlib
package works; noted here so it isn't rediscovered as one later.

**Complete-or-fail reaches the exit code, not just the error message:** every
subcommand's `run*` function returns `error`; `main` is the only place that calls
`os.Exit`, printing the error and exiting 1 - so an engine error (unknown corpus,
malformed ref, a genuine partial-read failure) always surfaces as a non-zero exit,
never a truncated success. Because `run*` returns rather than exits, the subcommand
logic itself is directly unit-testable without a subprocess.

**Validated against the real DB, matching the acceptance criterion exactly:**
`orthotomeo concord --corpus TAGNT G0859` prints all 17 aphesis rows including
`MAT.26.28` (the control case); `orthotomeo concord --corpus TAGNT --phrase
"εἰς,ἄφεσις" --adjacent` reproduces the full 5-occurrence adjacent set
(`Mat.26.28`, `Mrk.1.4`, `Luk.3.3`, `Act.2.38`, `Luk.24.47`) exactly matching T16's
own worked example. `cmd/orthotomeo/orthotomeo_test.go` covers all four subcommands
(text and `--json` modes), a missing-corpus error, a malformed ref, and a missing-DB
open failure, using a real built fixture DB (no mocks) per the project's own
testing convention.

### T27 - HTTP + local web UI  `DONE`
**Goal:** the browser-facing seam. The browser renders polytonic Greek and RTL Hebrew
correctly for free - the reason a web UI beats a native-toolkit renderer here.
**Scope:**
- `httpapi` package, stdlib `net/http` only (no framework dep). Read-only **GET**
  endpoints delegating to `engine`: `/verse`, `/passage`, `/concord`, `/parse`,
  `/attest`; every response is Citation-bearing JSON (same shape as T26 `--json`).
- Static web UI served from the same binary via `embed.FS`: a search box + results
  table that renders Greek/Hebrew in the browser. No JS framework unless brought for
  approval; plain fetch + templates is the default.
- **Security (dual mindset, baked into the ticket):** bind `127.0.0.1` only, never
  `0.0.0.0`; GET-only, no mutation surface, no raw-SQL passthrough (goes through
  `engine`); a distributed build serves only `sources.shippable=1` text (gate
  non-shippable editions like a fetched Rahlfs LXX out of the served set); no secrets,
  no auth needed for a loopback read-only server, but document the loopback assumption
  so nobody re-binds it to a LAN.
- **Print support (2026-07-01 decision):** `@media print` stylesheet only, no new
  route/endpoint - a study reference list is exactly the kind of output someone prints
  or exports to PDF via the browser's own print dialog. Hides the search bar and any
  interactive chrome (buttons, selects), forces plain black-on-white (confidence
  badges must not rely on color alone - add a border/outline so they survive
  grayscale printing), and keeps table rows from splitting across a page break
  (`tr { page-break-inside: avoid; }`) while still allowing the table itself to
  paginate. The T31 sources/attribution footer must always print - it's the
  license/attribution surfacing that CC BY sources require to be visible "in the
  work as served," and a printed page is part of that surface.
**Acceptance:** `GET /concord?dstrong=G0859` returns the full set as JSON;
`Count == len(Concord)` across the HTTP boundary; the web UI renders one Greek and one
Hebrew result legibly; the listener is loopback-only (assert the bind address);
a non-shippable edition is absent from a shippable-mode response; printing the page
(browser print preview) hides the search chrome, keeps the results table and the
sources/attribution footer, and shows no color-only confidence indicators.

### T27 AS-BUILT (2026-07-03)

Built as scoped, plus two endpoints the original scope predates: **`/interlinear`**
(T35) and **`/define`** (T34), since both landed earlier this session and the
ticket's own "recommended next executable order" note said T27 should draw on them
rather than retrofit later. Final endpoint set: `/verse`, `/passage`, `/concord`
(routes to `ConcordLemma` or `ConcordPhrase` depending on whether a `phrase` param is
present, mirroring T26 CLI's own `--phrase` branch), `/parse`, `/attest`,
`/interlinear`, `/define` - 7 GET routes, `net/http`'s Go 1.22+ method-prefixed
`ServeMux` patterns (`"GET /verse"`), stdlib only, no framework.

**Response shape parity, not a new shape:** every Citation-bearing endpoint returns
the identical `{citations, sources}` envelope T26's CLI `--json` and T20's MCP tools
already use; `/interlinear` mirrors it with `{words, sources}`; `/define` returns
`lexicon.Entry` directly (no wrapper needed - `Definition` is already `omitempty`).
A client that knows one transport's JSON shape knows all three.

**Security, each part actually enforced, not just documented:**
- **GET-only** is structural, not a middleware check - every route is registered as
  `"GET /path"`; Go's own `ServeMux` returns 405 for any other method with zero
  handler code involved (verified: `TestOnlyGETIsAllowed` POSTs to all 7 routes).
- **Loopback-only** lives in `cmd/orthotomeo-web`, not `httpapi` itself -
  deliberately **no `--host` flag exists at all**, only `--port`, so there is no
  flag combination that could ever bind to `0.0.0.0` or a LAN interface. The bind
  logic is factored into `listenLoopback(port)` specifically so a test could assert
  the real `net.Listener`'s address, not just read the source
  (`TestListenLoopbackBindsOnlyToLoopback`, `cmd/orthotomeo-web/main_test.go`).
- **Shippable-only content:** `httpapi/security.go`'s `shippableEditions`/
  `requireShippable` take the registry as a parameter specifically so a test could
  fabricate a non-shippable source and prove the gate actually drops/rejects it -
  today's real `sources.json` has zero non-shippable rows (there's nothing
  non-shippable until T23's Rahlfs LXX exists), so this couldn't otherwise be tested
  against real data at all. `shippableEditions` silently drops a non-shippable
  edition from a multi-edition list (`/verse`, `/passage`); `requireShippable`
  errors on a *known* non-shippable single corpus (`/concord`, `/parse`, `/attest`,
  `/interlinear`) but deliberately passes an *unknown* code through unjudged - that
  validation belongs to `engine`, not a duplicated worse-message copy of it here.
- **No raw SQL, ever:** confirmed by construction - `httpapi` imports only `engine`,
  `interlinear`, `lexicon`, `retriever`, `sources` (registry only, not DB), never
  `database/sql` or `orthotomeo/store`, same discipline `orthotomeo-mcp` already
  follows.

**Static UI** (`httpapi/static/`, `embed.FS`): a single-page vanilla-JS app
(`index.html` + `app.js` + `style.css`, no framework) with a mode selector (verse /
passage / concord / parse / attest / interlinear / define) that shows only the
relevant input fields per mode, fetches the matching endpoint, and renders a results
table plus a sources/attribution footer (T31 file/license/attribution, T36
`homepage_url` as a clickable link when present). Original-language table cells use
`dir="auto"` so the browser's own bidi algorithm handles RTL Hebrew correctly without
hardcoding direction - this is the entire reason a web UI beats a native-toolkit
renderer here, and it required zero special-case code, exactly as the ticket's Goal
predicted.

**Print stylesheet:** `@media print` block in `style.css` per the decided scope -
hides the search form and print button, forces black-on-white, gives `.badge`
elements a real border so a confidence badge survives grayscale printing (never
color-only), keeps `tr { page-break-inside: avoid; }`, and gives the sources footer
its own `page-break-inside: avoid` with a heavier border so it reads as a distinct,
intact block on a printed page - it's the CC BY attribution surface, so it must not
get silently orphaned mid-page-break.

**Validated against the real corpus, not just fixtures:** rebuilt a scratch DB from
the actual `bible-text`/`STEPBible-Data` roots (full `--verify` passed), started the
real `orthotomeo-web` binary, and hit it with real HTTP requests: `GET /interlinear`
for Lev.17.11 (TAHOT) returned real Hebrew text/translit/gloss; `GET /concord` with
`phrase=εἰς,ἄφεσις&corpus=TAGNT` returned both real NT occurrences with `translit:
"eis aphesin"`; `GET /` served the HTML shell (200, `text/html`); `GET
/static/app.js` served (200); a `POST` to every route returned 405; the `sources`
map on a live response carried `homepage_url`. (One process-management note, not a
code issue: the smoke-test server was started detached from this session's shell,
so it couldn't be torn down automatically afterward - `taskkill` is blocked by a
standing user deny-rule - left running on loopback for Justin to close manually;
harmless, read-only, not a security concern, just noted so a stray listener on
127.0.0.1:8421 doesn't look mysterious later.)

New test files: `httpapi/httpapi_test.go` (20 tests: one per endpoint against a real
DB fixture, GET-only enforcement, static asset serving, index page), `httpapi/
security_test.go` (the shippable-gate tests described above), `cmd/orthotomeo-web/
main_test.go` (the loopback-bind test). Full suite green across every package.

**E2E addendum (2026-07-04): `httpapi/e2e_test.go`, chromedp not Playwright.**
`httpapi_test.go`'s handler-level tests can't prove the actual served page works -
they never load `index.html` in a real browser or run `app.js`'s mode-switching/
fetch/render logic. Considered Playwright (Node-based `@playwright/test` and the
community `playwright-go` binding) vs `chromedp` (`github.com/chromedp/chromedp`);
chose chromedp - it's genuinely pure-Go (talks the Chrome DevTools Protocol
directly, no Node process anywhere, unlike `playwright-go` which still shells out
to Playwright's own Node driver under the hood) and runs inside plain `go test`,
keeping this repo on one toolchain - the same reasoning that already picked Gio
over Fyne for T28. Real tradeoff, accepted deliberately: Chrome/Chromium only, no
cross-browser coverage - acceptable for a local, single-user, read-only tool.
Bumped `go.mod`'s `go` directive 1.25.0 -> 1.26 (chromedp's latest requires it;
matches the toolchain already installed, not a forced download).

**A real chromedp lifecycle quirk found and fixed, not worked around:** an initial
design ran a short "probe" `chromedp.Run` first (navigate to `about:blank`, confirm
a browser actually launched) so a missing-Chrome machine would skip cleanly, then
ran the real test actions in a second `chromedp.Run` call on a fresh child context.
Every real action then failed with `context canceled` - reproduced in isolation
(a 6-line repro outside the test suite) and confirmed: chromedp ties a browser
tab's lifetime to whichever context first navigated it, so cancelling even a
short-lived **child** `context.WithTimeout` used only for the probe tears down the
whole tab, breaking later calls on the same base context. Fixed by dropping the
separate probe entirely - one context, one `chromedp.Run` call per test - and
classifying the action-chain's own error instead (`skipIfNoBrowser`: a
browser-launch failure like "executable file not found" skips, any other error
still fails the test for real).

Two tests: `TestE2EIndexPageLoadsSearchForm` (baseline - the embedded `index.html`
actually renders a working form, checked via page `<title>`) and
`TestE2EParseSearchRendersResultsTable` (drives the real UI: dispatches a real
`change` event on the mode `<select>` so `app.js`'s own listener reveals the
corpus/word fields - not simulated by directly toggling CSS classes - fills the
ref/corpus inputs, clicks submit, waits for `#results table`, and confirms the
real fetched-and-rendered text contains `G0859`/`MAT.26.28`). Both pass against
the actual Chrome install on this machine. Full suite still green (`cmd/
orthotomeo-desktop`'s tests included - unaffected).

**UI-design port addendum (2026-07-04): lightwater theme + real dropdowns.**
Ported the 7 chat-reviewed mockups (verse/passage/concord/parse/attest/
interlinear/define, one per `httpapi` endpoint) into the actual shipped
`httpapi/static/` - not just a visual reskin: `editions` (verse/passage) became a
real `<select multiple>` and `corpus`/`by` (concord/parse/attest/interlinear)
became real `<select>` dropdowns, replacing free-text inputs. `app.js`'s submit
handler gained explicit multi-select handling (`input.multiple` ->
`selectedOptions`, not `.value`, which only ever returns the first selected
option) - proven by a new E2E test, not just code review:
`TestE2EVerseSearchReadsMultiSelectEditions` selects a second edition (ASV,
alongside KJV's default) via a real DOM interaction and confirms **both** come
back in the fetched results, not just the first.

`style.css` rebuilt on the same `t-lightwater` palette as `Baptism/design/
prototype-full.html` (Spectral serif for original-language text, the gold accent,
blue-gray ink/rule tones) rather than the earlier CDS-neutral placeholder theme.

**Two real bugs found only by actually screenshotting the live page** (chromedp's
`FullScreenshot`, not just re-reading the CSS) **against the real corpus**, not
caught by any of the passing unit/E2E tests, since none of them assert on layout
or rendered HTML structure:
1. The `book`/`chapter`/`verse`/`word` input boxes rendered ~3x their intended
   height. Cause: a blanket `.field.active { flex-direction: column }` rule meant
   only for the labeled `editions` field was applying to every field. Fixed by
   scoping the stacked layout to `.field.active:has(.field-label)` instead of
   every active field.
2. The Abbott-Smith lexicon definition (`define` mode) rendered its own embedded
   markup (`<b>`, `<BR/>`, custom `<ref>` tags - present in the STEPBible TBESG
   source file itself, meant to render) as literal visible angle-bracket text,
   since `app.js` set it via `textContent`. **Explicitly decided, not assumed:**
   asked whether to render as HTML - confirmed safe specifically because this
   string only ever originates from bundled, static corpus data (never a request
   parameter or any other caller-controllable input), so there's no injection
   surface `innerHTML` would open here that `textContent` was actually guarding
   against. Switched `entry-definition` to `innerHTML`, documented inline in
   `app.js` why this specific case is safe (a blanket "always use innerHTML"
   habit elsewhere would NOT be safe - this is a narrow, justified exception).

Both fixes verified visually via real chromedp screenshots against a freshly
rebuilt DB from the actual corpus (Lev.17.11 TAHOT interlinear view, Jhn.1.1
KJV+ASV multi-select verse lookup, G0859 Abbott-Smith definition card) before and
after each fix - not just re-run tests, actually looked at the rendered pixels.
Full suite green throughout.

**Second addendum (2026-07-04): saved mockups as files + a real editions bug found
by direct comparison, not a stale build.** All 7 mockups saved as standalone files
(`design/t27-{verse,passage,concord,parse,attest,interlinear,define}.html`), same
convention as the earlier two `t27-web-ui-lightwater*.html` files. Justin reported
the live app didn't match the design (screenshot showed WEB alone highlighted in
the editions list, and wider-than-expected `book`/`chapter`/`verse` boxes) after
rebuilding `orthotomeo-desktop.exe` fresh - ruling out the "stale embedded assets"
explanation from T28's earlier note. Investigated by chromedp-screenshotting the
real app's untouched default state (via `httptest`, straight from source, no
compiled binary involved) side by side with the saved `design/t27-verse.html` file
(`file://` navigation, same chromedp tooling) - direct comparison, not
re-reading the CSS and guessing.

**Finding: the default state was already correct** (KJV pre-checked, consistent
box widths) - the reported mismatch traced to a real, separate problem: `editions`
was a native `<select multiple>`, and **a plain click on one option silently
deselects every other selected option** (standard HTML behavior - only a
Ctrl/Cmd+click adds to a selection instead of replacing it). Clicking "WEB" to try
the field out would deselect KJV and leave only WEB checked, exactly matching the
screenshot - not a stale build, not a rendering bug, a real interaction trap with
no affordance telling the user a modifier key was needed. Confirmed the diagnosis
with Justin before fixing rather than assuming it.

**Fixed by replacing the `<select multiple>` with 4 independent checkboxes**
(`index.html`, new `.checkbox-group` styling in `style.css`), each toggling on its
own click with no modifier-key requirement - closer to what "pick several" actually
means to a user. `app.js`'s submit handler updated to gather every *checked* box
sharing `name="editions"` (`querySelectorAll` + `.checked` filter) instead of
reading `selectedOptions` off a single multi-select element.
`TestE2EVerseSearchReadsMultiSelectEditions` updated to click the real ASV
checkbox (a real user interaction, not a JS property set) and still confirms both
KJV and ASV round-trip through a live fetch. Both affected `design/*.html` mockups
(`t27-verse.html`, `t27-passage.html` - `editions` is one shared field config
across both modes) updated to match. Re-screenshotted the real app post-fix to
confirm the checkboxes render and toggle independently. Full suite green.

**Third addendum (2026-07-04): a real re-theme, via `/frontend-design`.** The
"lightwater" palette was borrowed wholesale from an unrelated project (Baptism, a
devotional thesis book) - a real, non-generic aesthetic, but not one derived from
orthotomeo's own subject matter. Re-derived the visual identity from what
orthotomeo actually is: a critical-apparatus / interlinear-lexicon tool (manuscript
sigla, Strong's numbering, morphological parse codes, Ketiv/Qere), not a devotional
reading surface. Ran the design through a brainstorm/plan/critique pass before
touching code (per the `/frontend-design` skill's own process) and checked the
result against the three current AI-design clichés (warm-cream+terracotta,
near-black+acid, broadsheet-hairlines) before building, to confirm it wasn't
defaulting into one of them.

**New tokens:** paper `#F5F2EA` / ink `#1C1B18` / rule `#D6D0C0` / rubric (Greek/NT
accent, critical-edition red) `#8A2E1C` / indigo (Hebrew/OT accent) `#26314C` /
gilt (confidence badges only) `#9C7A2E` - the Greek/Hebrew color split isn't
decorative, it does real work disambiguating LTR vs RTL content at a glance.
Type: **STIX Two Text** (display+body - built for scholarly typesetting, real
academic pedigree, distinct from Spectral/Playfair/Fraunces defaults), **Noto
Serif** / **Noto Serif Hebrew** (original-language text, robust polytonic/niqqud
coverage), **Fragment Mono** (apparatus codes - dStrong, morph codes, refs).

**Signature: the apparatus row + the colophon.** Every result row's citation
number is styled as a footnote mark (superscript ¹²³via a `SUPERSCRIPT_DIGITS`
map in `app.js`, not a plain digit column) against a rubric-red left rule
(`td.ordinal` in `style.css`) - "structure is information": the number IS a
footnote reference in a critical edition, so it's spelled that way. The `#sources`
block (unchanged element ID, so nothing downstream broke) is reframed as a
**colophon** - the scribal note naming a manuscript's origin at its close, the
same provenance information a generic "sources" list carries, in the vocabulary
of the subject matter itself (an asterism `⁂` mark prefixes the heading via
CSS `::before`).

**Attribution, explicitly placed per Justin's answer, not assumed:** asked
whether the credit line should live in the per-result colophon (only visible
after a search) or persist regardless of search state - persistent won. Added
`<footer class="credit">orthotomeo, an engine by Justin Rainsberger</footer>` in
`index.html`, always rendered, independent of `#results`/`#sources` state.

**Validated against the real corpus**, not just the color-swatch specimen shown
for approval first: rebuilt a fresh DB, screenshotted `interlinear` (Lev.17.11,
TAHOT - real Hebrew renders correctly in Noto Serif Hebrew, apparatus rows and
colophon both correct) and `define` (G0859 - Greek headword in indigo, gloss in
rubric, Abbott-Smith definition in STIX Two Text) via chromedp against the actual
running server. `orthotomeo-desktop.exe` rebuilt with the new theme baked in.
Existing E2E tests (`httpapi/e2e_test.go`) needed no changes - they assert on
text content and element structure (`#results table`, checkbox values), not the
specific CSS/type tokens that changed. Full suite green.

**Not yet done, flagged not silently skipped:** the 9 saved `design/*.html`
mockup files (the 7 per-tool-type ones plus the 2 earlier lightwater ones) still
reflect the old palette/type and haven't been refreshed to the apparatus theme -
a real follow-up if they're going to keep being used for comparison-driven
debugging the way they were earlier this session.

**Fourth addendum (2026-07-04): navigation/cross-linking UX pass.** Justin asked
for a review of navigation/input/display for ease of use, flagging a concrete
example: a Strong's number (`dstrong`) rendered as plain text in a results table
is useless on its own without a path to what it actually means. Reviewed the
whole surface, not just that cell, and found three real gaps:
1. `dstrong`, `lemma`, and `ref` were all dead text across every results table -
   no way to go from "I see G0859" to its gloss, from "I see this lemma" to its
   other occurrences, or from "I see this ref" to its full interlinear reading,
   without manually retyping into a different mode.
2. Cross-linking creates its own problem: no browser-history integration (this
   is a client-side-fetch single-page app, the URL bar never changes), so
   without a fix, clicking into a definition from a result row would strand the
   reader with no way back except re-running the original search.
3. **A separate, real display bug found in passing**: `renderCitations` never
   rendered `Attestation`/`Manuscripts` at all, and `renderWords` never rendered
   `Lemma`/`DStrong` - both fields the backend already sends (`omitempty` JSON,
   present whenever populated). `attest` mode's entire purpose is showing
   Type/Manuscripts data, and it was silently invisible. Fixed alongside the
   cross-linking work since it's the same "display what the API already sends"
   concern, not scope creep - flagged explicitly rather than silently bundled in.

**Decisions, asked not assumed:** confirmed whether dStrong linking should be
click-through-only or also invest in a hover preview (chose to build both -
click-through to full `/define` view, plus a debounced, cached hover tooltip
showing just the gloss without leaving the results); confirmed which other
cross-links to build (`lemma` -> `concord`, `ref` -> `interlinear`, both scoped
to word-tagged results only, not verse/passage's plain-text editions where
there's no corpus to jump to); confirmed the back-link and title-tooltip
additions rather than assuming scope.

**Implementation:** `app.js` refactored around shared `gatherParams`/
`applyParams`/`setFieldValue` helpers (previously inlined in the submit
handler) so cross-link navigation and "back" restoration use the exact same
code path a normal search does, not a parallel one. `historyStack` is pushed
only on a cross-link jump (not every search) - "restores the prior mode,
fields, and results" scoped to undoing a jump, not a full search log. The
hover preview (`scheduleGlossPreview`/`fetchGlossPreview`) debounces 250ms and
caches by dStrong (`definitionCache`) so a fast pass over several cells never
fires more than one fetch per distinct dStrong. Cross-links are real `<a>`
elements (`xlink()` helper), not divs with an onclick - keyboard tab order and
Enter-to-activate work without extra code, and a `:focus-visible` outline was
added both generically (`:root`) and specifically for `.xlink`, since keyboard
navigation into a results table is exactly the case this skill's "quality
floor" (visible keyboard focus) calls out. `attestationTitle()` gives the
Type-code badge a native tooltip explaining N/K/O in plain language; confidence
badges got a `title` too (a caveat's own text when Flagged, a generic
explanation when High).

**Validated for real, not just via the new E2E tests:** rebuilt the DB from the
actual corpus and screenshotted a concord search for G0859 (17 real NT
occurrences, full 10-column table, hover tooltip correctly showing "forgiveness
(aphesis)" under the dStrong link), a click-through-then-back round trip
(define view showing the `‹ back` link, confirmed it returns to the original
concord results), and `attest` on the real Mark 16:9-20 longer-ending case
(Type=KO now visible across all 15 words, previously invisible entirely).
`orthotomeo-desktop.exe` rebuilt with the pass baked in.

**New tests** (`httpapi/e2e_test.go`): `TestE2EDStrongLinkNavigatesToDefine`,
`TestE2ELemmaLinkNavigatesToConcord`, `TestE2EBackLinkRestoresPriorResults`,
`TestE2EDStrongHoverShowsGlossTooltip` - each drives the real DOM (a real click,
a real dispatched `mouseenter`, not a direct function call), against the same
`buildFixture` other httpapi tests already share. One real test-selector bug
caught and fixed before these tests passed: `a.xlink:nth-of-type(2)` does not
mean "the second cross-link in the row" - `nth-of-type` counts siblings under
the same parent, and each `xlink` is the sole child of its own `<td>`, so it's
always `nth-of-type(1)` relative to its own parent. Fixed by targeting column
position (`td:nth-child(N) a.xlink`) instead, and by scoping each test's parse
request to a single word (`?word=2`) so the target row is unambiguous rather
than relying on selector ordering across multiple rows. Full suite green.

**Fifth addendum (2026-07-04): book-field autocomplete.** Justin asked what I
thought of making `book` a dropdown. Recommended a `<datalist>`-backed
autocomplete instead of a `<select>`: this is a single fluent user's own tool,
and he already types book codes from memory - a full dropdown would slow down
the common case to fix a rare one (a forgotten/mistyped code), where a
datalist gets typo-catching and discoverability without giving up free typing.
Traded off explicitly: unlike a `<select>`, a `<datalist>` doesn't strictly
enforce a valid choice.

**Data source, not hand-typed:** rather than writing 66 `<option>` elements
into `index.html` (a second, hand-maintained copy of the canonical book list
that could silently drift from the real one - exactly what this project's own
"single source of truth" discipline exists to prevent), added `GET /books`
(`httpapi/handlers.go`) serving `books.Registry()` directly - the same
embedded, checked-in `books.json` every loader already treats as ground truth
(T2), zero DB access, zero new data. `app.js` fetches it once on page load and
populates `<datalist id="books">`; a failed fetch just leaves the field as a
plain text input (autocomplete is additive, never load-bearing).

**Validated for real:** confirmed against the real corpus via chromedp - 66
options, in canonical order, `GEN`/`Genesis` first. New tests: `TestBooksEndpoint`
(the endpoint itself, 66 books, Genesis first), `TestOnlyGETIsAllowed` extended
to cover `/books`, `TestE2EBookDatalistPopulatesFromRealRegistry` (confirms the
real page-load fetch actually populates the DOM, not just that the endpoint
works in isolation). `orthotomeo-desktop.exe` rebuilt. Full suite green.

**Sixth addendum (2026-07-04): book-name resolution bug.** Justin reported
"LUK" worked but "LUKE"/"Luke"/"luke" didn't. Root cause: `retriever.Ref.Book`
is documented as requiring the canonical USFM code, and every transport
resolved it via `verses.NewResolver(db, "usfm", ...)`, an exact, case-sensitive
match against the `usfm` scheme only - `book_names` already has a `name-en`
alias per book (Justin's own `books.Seed`, T2), but nothing outside the loader
path ever queried it, and no path folded case at all. The HTTP handlers
(`queryRef`/`handlePassage`) passed the raw query param straight through with
zero normalization; the CLI's `parseRef` upper-cased but never tried the
`name-en` scheme.

**Fix, at the one seam, not per-transport:** added `books.ResolveCode(q, raw)`
- tries `usfm` uppercased first, falls back to `name-en` case-insensitively
(`COLLATE NOCASE`) - and exposed it as `Engine.ResolveBookCode`, so the
normalization logic exists exactly once regardless of which transport calls
it (`engine.go`'s own stated invariant: "no transport ever sees `*sql.DB`...
a transport that needs SQL is a design failure"). Wired into both HTTP
(`queryRef`, `handlePassage`) and the CLI (`parseRef`, which now takes
`*engine.Engine` and resolves the book token before the dot-split parse still
handles chapter/verse). Left the MCP tools' `ref()` helper alone - its schema
already documents "canonical USFM book code" as the contract for an LLM
client, a different situation from a human free-typing into a text field.

**Validated:** new `books.TestResolveCode`/`TestResolveCodeUnknown` (usfm/
name-en, every case, multi-word names, unknown-book still errors),
`httpapi.TestVerseEndpointBookNameVariants`/`TestVerseEndpointUnknownBook`,
CLI `TestLookupAcceptsBookNameVariants`. Also re-verified against the real
corpus DB (not just the fixture) via a scratch script hitting `/verse` for
LUK/luk/Luke/LUKE/luke (all 200) and a nonsense book (400). Full suite green;
`orthotomeo-desktop.exe` rebuilt.

**Goal:** an offline, no-browser-juggling native launcher, matching the Footsteps
desktop pattern (desktop app starts the web server, opens the browser, and stops the
server on close).
**Scope:** `cmd/orthotomeo-desktop`, a native GUI app that:
1. starts the T27 HTTP server on an ephemeral **loopback** port;
2. opens the default browser to `http://orthotomeo.localhost:<port>/` (the
   `*.localhost` host resolves to 127.0.0.1 per RFC 6761; matches the house
   `*.localhost` test-domain convention and Footsteps' `footsteps.localhost`);
3. shows a minimal status window / tray (running, port, quit);
4. on window close / quit, cancels the server context, waits for a clean shutdown
   (port released, no orphan process), and exits.
- **The GUI toolkit renders NO scripture** - it is a lifecycle shell only; the
  browser owns all Greek/Hebrew rendering. This is what keeps the "thin wrapper"
  actually thin and sidesteps any GUI toolkit's complex-script/RTL weaknesses.
**Dependency:** originally `fyne.io/fyne/v2`, approved 2026-06-30 on the assumption
(unverified at the time) that it would keep the whole build - engine, HTTP, and
desktop - C-toolchain-free like `modernc.org/sqlite` already does. **Corrected
2026-07-03 (see AS-BUILT): that assumption was wrong** - Fyne's default desktop
driver requires cgo. Built on `gioui.org` instead, which actually delivers the
cgo-free property this ticket wanted.
**Acceptance:** launching the binary opens the working search UI in the browser;
quitting stops the server (port freed, process exits, verified no orphan); the GUI
toolkit itself draws no verse text; run on Windows (primary) at minimum.

### T28 AS-BUILT (2026-07-03)

**The dependency changed mid-ticket, for a real, verified reason - not a preference
swap.** Built with Fyne first (as originally approved), then discovered its default
desktop driver pulls in `go-gl/glfw`, which is cgo-based: `CGO_ENABLED=0 go build
./cmd/orthotomeo-desktop` failed outright (`build constraints exclude all Go files`
on `github.com/go-gl/gl`). This directly contradicted PLAN.md's own recorded
assumption that the whole build would stay C-toolchain-free - that assumption was
simply inaccurate when written, not a newly-introduced regression (Footsteps, the
cited precedent, already ships Fyne+cgo successfully, so cgo itself isn't new risk -
but it wasn't the *documented* design either). Surfaced this to Justin directly with
the actual landscape of alternatives (Gio: true pure-Go via OS syscalls, less mature,
more manual API; `lxn/walk`: pure-Go but Windows-only; nothing else avoids cgo the
way `modernc.org/sqlite` does) rather than quietly picking one. **Decision: switch to
Gio** - confirmed by rebuilding with `fyne.io/fyne/v2` fully removed
(`go mod tidy`) and `gioui.org` added instead; `CGO_ENABLED=0 go build
./cmd/orthotomeo-desktop` now succeeds, the property originally wanted.

**Scope split across two files**, deliberately: `server.go` is the non-GUI lifecycle
logic (`startServer`/`shutdown`/`port`/`url`/`openBrowser`) - fully unit-testable
without a display, which `go test` in CI doesn't have. `main.go` is purely the Gio
window (status label, "Open in browser" button, "Quit" button) using Gio's own
idiomatic `for { switch w.Event().(type) { ... } }` loop, not a callback API -
confirmed via `go doc` against the actually-installed `gioui.org v0.10.1` rather than
assumed from general Gio familiarity, since Gio's window/event API has changed
shape across versions.

Matches T27's own "no --host flag" discipline: `startServer` hardcodes
`127.0.0.1` with an ephemeral port (`:0`) - there is no parameter anywhere in this
binary that could bind beyond loopback. `url()` builds the `http://
orthotomeo.localhost:<port>/` address per the ticket's own RFC 6761 reasoning
(matches Footsteps' `footsteps.localhost` precedent) rather than a bare IP.

**Validated:** `TestStartServerBindsLoopbackAndServes` (a real HTTP GET through the
started server, not just "a listener opened"), `TestPortMatchesListener`,
`TestShutdownReleasesThePort` (two independent checks: the shut-down server refuses
new connections, AND the OS eventually allows rebinding the identical port - the
second check uses a short bounded retry, not a single immediate attempt, after a
real flake surfaced: Windows can hold a just-closed TCP port in TIME_WAIT briefly
even after a clean `Shutdown`, so an instant re-listen attempt failed transiently on
a genuinely-working shutdown path - fixed the test's timing assumption, not the
shutdown code, which was already correct), `TestStartServerFailsOnMissingDB`.
**Not independently visually verified** (window actually renders, browser actually
opens) - a prior smoke-test process from this session was left running because
`taskkill` is blocked by a standing deny-rule, so a second GUI-launching smoke test
was skipped to avoid leaving another orphaned window; the underlying HTTP/lifecycle
logic every button/action calls is fully tested, and every Gio API call was checked
against the real installed package via `go doc` rather than guessed, but the actual
window paint/click path itself wasn't clicked through by a human or a screenshot
this session. Worth a manual run before considering this fully proven, not just
tested.

### T30 - Public remote MCP deployment (Streamable HTTP + GCP)  `NEXT` (design decided 2026-07-01)
**Goal:** make the MCP server reachable by anyone as a claude.ai custom connector, at
zero/near-zero cost, without requiring any login on either side.
**Decisions made (2026-07-01 session):**
- **Transport:** stdio (current, local-only) does not satisfy this - per the MCP spec,
  a remote client can only reach a server over **Streamable HTTP** (single endpoint,
  POST+GET, `Mcp-Session-Id` session header). This is a real new entry point, not a
  config flag - `cmd/orthotomeo-mcp` needs a second command (or a `--transport` flag)
  using the go-sdk's Streamable HTTP handler alongside the existing stdio one. Origin
  header validation is a spec **MUST** for this transport (DNS-rebinding protection) -
  implement it regardless of the auth decision below.
- **Auth: none, by design decision, not by default.** The MCP spec itself makes
  authorization OPTIONAL for HTTP transports - there is no protocol requirement forcing
  OAuth. Given this server's data (public-domain/CC-licensed biblical text, read-only,
  no user data, no writes), "no auth" is a defensible choice for the *access-control*
  risk, unlike it would be for a write-capable or PII-bearing service. What auth would
  have controlled (cost exposure to anonymous traffic) is instead controlled directly:
- **Cost/abuse controls replace auth as the actual safety mechanism:**
  - Cloud Run `--max-instances=1` (explicit user decision - never scale beyond one
    instance, full stop, regardless of traffic).
  - Rate limiting and request limits, enforced **in-app** (not just at the GCP/Cloud
    Armor layer) - this is real code to write, not just a deploy flag.
  - A GCP budget alert as a second, independent backstop.
  - Origin validation (above) is a security control, not a cost control, but ships in
    the same pass since it's a spec **MUST** either way.
- **Packaging:** a **separate new executable** (not a mode flag on the existing
  `cmd/orthotomeo-mcp` stdio binary used by Claude Desktop) - keeps the locally-run
  stdio path and the publicly-exposed HTTP path as genuinely separate binaries with
  separate blast radii, rolled into one Docker image for Cloud Run deployment. The
  built DB is baked into the image at build time (`cmd/build` runs at image-build time,
  not at container startup) - matches the existing "DB is a regenerable build artifact"
  discipline, just relocated to the image build step.
- **Monitoring:** usage monitoring is a stated requirement, not yet designed - Cloud
  Run's built-in request logs are the starting point; whether anything more
  purpose-built is needed is an open question, not decided.
**Was blocked on T31 (attribution/license surfacing) - T31 is now `DONE`** (2026-07-01):
served output now carries `sources.<edition>.license`/`.attribution` for every response,
closing the compliance gap that existed before real (non-repo-reading) traffic. T30
itself is unblocked; nothing in its own scope has been built yet.
**Not yet decided:** exact rate/request-limit numbers; whether to also pursue listing
in Anthropic's public connector directory (a separate, unresearched process - see
chat) versus only publishing the URL for people to add as their own custom connector.

### T31 - Citation attribution/license surfacing  `DONE`
**Goal:** close a real CC BY 4.0 compliance gap found while scoping T30: served tool
output currently carries no human-readable attribution or license text at all.
**Finding (2026-07-01 session, verified against the code, not assumed):**
- `retriever.Citation` (`retriever/retriever.go`) has `SourceFile` (a glob path, e.g.
  `STEPBible-Data/Translators Amalgamated OT+NT/TAGNT*.txt`) and `SourceLocator`, but
  **no `Attribution` or `License` field**. `sources.json` already records the correct
  `license`/`attribution` string per source (the T1 sources registry) - it's just never
  joined into what a caller of `concord_lemma`/`get_verse`/etc. actually receives.
  CC BY 4.0 requires crediting the creator and linking the license **in the work as
  served** - a value recorded in a repo file nobody using the live service reads does
  not satisfy that. Fine as-is for a locally-run personal tool; a real gap once this is
  a public service showing tool output to end users (T30).
- Affects every CC BY 4.0 source: TAGNT, TAHOT, TBESG, TBESH (gloss only - see below),
  TEGMC, TEHMC, OSS-LXX-lemma, OpenBible-xref. Public Domain sources (KJV, ASV, WEB,
  Brenton, Swete) have no legal attribution requirement - crediting them is courtesy,
  not compliance.
- **TBESH carries a stricter sub-license, already correctly flagged in `sources.json`**:
  `"CC BY 4.0 (definitions: abridged BDB via Online Bible - permission required before
  applying definitions)"`. Verified directly against the code: the flagged
  `lexicon.definition` column (the long BDB-derived Hebrew definitions, as opposed to
  the short CC-BY `gloss`) is loaded by the T5 loader but **never read by any
  query-serving path** - `grep`ed `parse`, `concord`, `attestation`, `retriever`,
  `cite`, `engine`, `cmd/orthotomeo-mcp`, `cmd/orthotomeo`: zero hits on
  `lexicon.definition`. So today this permission-gated content is inert from a serving
  standpoint - not currently a violation, but two things to keep true going forward:
  (a) never expose `lexicon.definition` through any future feature without actually
  resolving the permission question, and (b) never make the raw `.db` file itself
  downloadable (only query results) - a raw DB download would distribute the flagged
  column even though no current tool response surfaces it.
**Design revised (2026-07-01, before any code was written) - do not build the naive
per-Citation-field version.** A live example surfaced the problem directly: a full-verse
`parse`/`attestation` call over an 18-word verse (Lev.17.11, TAHOT) returns 18 Citations,
and **`source_file` is the byte-identical string 18 times** in that one response - real,
measured token waste, not a hypothetical. Adding `License`/`Attribution` as two more
per-Citation fields would have made the exact same redundancy worse, not better, since
`source_file`/`license`/`attribution` are never row-specific - they're **constants of the
edition**, and every Citation in a single-corpus call (`concord_lemma`, `concord_phrase`,
`parse`, `attestation`) shares one edition already.
**Corrected shape:** move `SourceFile` (and the new `License`/`Attribution`) OUT of
`Citation` entirely, into a small top-level map on the JSON envelope, keyed by the
`edition` string already present on every Citation. **This is a net field removal, not a
swap** - verified every Citation-construction site (`retriever.go`, `parse.go`,
`attestation.go`, `concord.go`): `Edition` is unconditionally set to `info.sourceCode` /
`corpus`, i.e. it is *already, always* the exact `sources.json` join key. No new per-row
field (no numeric index, no separate `source` key) is added at all - `SourceFile` is
simply dropped from each row, and `edition` (already present) does the pointing:
```json
{
  "sources": {
    "TAHOT": {"file": "STEPBible-Data/.../TAHOT*.txt", "license": "CC BY 4.0", "attribution": "STEPBible.org / Tyndale House Cambridge"}
  },
  "citations": [{"edition": "TAHOT", "ref": {...}, "source_locator": "Lev.17.11#01=L", ...}]
}
```
`source_locator` (renamed `locator` - see below) stays inline on each Citation - unlike
`source_file`, it's genuinely row-specific (e.g. `Lev.17.11#01=L` differs per word) and
there's nothing to dedupe. `get_verse`/`get_passage` (which can span multiple editions in
one call, e.g. KJV+ASV+WEB) get more benefit still: today they repeat each edition's
`source_file` once per verse; with this shape each distinct edition appears exactly once
in `sources` regardless of how many verses/editions are requested.

**Two field renames decided in the same pass (2026-07-01), since both are naming
problems the `source_file` fix exposed, not separate work:**
- **`source_locator` -> `locator`.** Once `source_file` no longer sits on the row,
  "source_locator" reads as if it still means "the source's locator" when "source" no
  longer appears as a value at that level at all - `locator` alone is unambiguous next
  to `edition`.
- **`editions` -> `manuscripts`.** A real, pre-existing naming collision, independent of
  the dedup work: `edition` (singular - the corpus/source name, e.g. `"TAHOT"`) and
  `editions` (plural - the manuscript-attestation list, e.g.
  `"NA28+NA27+Tyn+WH+Treg+TR+Byz"`) are two unrelated concepts differing only by an "s" -
  easy to misread one for the other. Renamed to `manuscripts` (matches how the Concord
  spec/WHNT itself talks about textual witnesses) to remove the ambiguity outright, not
  just to save bytes.

**Raised, not decided - a distinct, lower-confidence idea for later:** in an
`attestation` call over a whole passage, `manuscripts` (nee `editions`) and
`attestation` (the Type code) are *also* often identical across every word - verified
directly against the real Mark 16:9-20 example, where all 9 words in the verse carry the
exact same `manuscripts` string. This is a different kind of redundancy than
`source_file` was: `edition -> source_file` is a *static* 1:1 mapping already recorded in
`sources.json`, so reusing the already-present `edition` field as a pointer was free.
`manuscripts`/`attestation` are genuine per-word *data* that only *usually* happens to
repeat within a passage - deduping that properly needs an actual symbol table with
indices (the exact complexity the `source_file` fix specifically avoided needing).
Bigger design lift, less certain payoff - on record for a future pass, not scoped now.

**Scope (not yet built):**
1. Remove `Citation.SourceFile`; add the top-level `sources` map, populated from
   `sources.json` at the one existing join point (`SourceFile` lookups already happen
   in one place per the Rule-of-Three precedent - `retriever.ResolveEditionVerses`).
2. Rename `Citation.SourceLocator` (JSON `source_locator`) -> `Locator` (JSON `locator`).
3. Rename `Citation.Editions` (JSON `editions`) -> `Manuscripts` (JSON `manuscripts`).
4. Every tool/CLI response wrapper (`citationsPayload` in both `cmd/orthotomeo-mcp` and
   `cmd/orthotomeo`) gains the `sources` field alongside `citations`.
5. **`cite.Cite`'s rendered Markdown is unaffected** - this dedup is a JSON-transport
   optimization only. Markdown output is a flat string for human/LLM reading, not a
   nested structure a client re-parses, so `Cite` still resolves edition to file
   internally and inlines "(source: FILE LOCATOR)" per bullet exactly as it does today.
6. Existing tests asserting exact Citation JSON shapes will need updating - expected,
   not a regression signal.

### T31 AS-BUILT (2026-07-01)

Built exactly to the design above, no deviations. `retriever.Citation` lost `SourceFile`
entirely; `SourceLocator`/`Editions` renamed to `Locator`/`Manuscripts`. New
`retriever.SourceInfo` + `retriever.SourcesFor(citations) (map[string]SourceInfo, error)`
- the one shared construction point, backed by `sources.Registry()` (the existing
embedded-JSON decode, zero DB, zero I/O) - is the only place a `Citation.Edition` is
joined against `sources.json`'s `license`/`attribution`/`source_file`. Every
`sourceFile(db, ...)` DB lookup that existed **solely** to populate `Citation.SourceFile`
was deleted outright (`concord.go`, `parse.go`, `attestation.go` each had one) - not just
unused, genuinely removed, so ConcordLemma/ConcordPhrase/Parse/Attestation now make one
fewer DB round trip per call than before. `retriever.go`'s own `sourceFile(db, code)`
helper survives unchanged - `canonicalAddress`/`alignedAddresses` (which build
`Address`, not `Citation`) still need it, and `Address.File` was never the redundant
per-row repeat `Citation.SourceFile` was (one `Address` per edition already, never one
per word).

`cite.Cite` needed a real code change, not just "confirmed unaffected" as originally
assumed: since `Citation` no longer carries a file, `Cite` now resolves it internally via
`sources.Registry()` (its own edition->file map, computed once per `Cite` call) so the
rendered Markdown bullet is byte-identical to before - confirmed directly against the
real DB (`orthotomeo attest MRK.16.16 --word 4` still renders
`(source: STEPBible-Data/.../TAGNT*.txt Mrk.16.16#04=KO)`).

Both wrapper types (`citationsResult` in `cmd/orthotomeo-mcp`, `citationsPayload` in
`cmd/orthotomeo`) gained the `sources` field; `cmd/orthotomeo-mcp` centralized the
per-tool construction into one `toCitationsResult(cs, err)` helper (7 call sites, one
shared point) rather than repeating `SourcesFor` at each.

**Validated against the real DB, matching the exact motivating example:**
`orthotomeo parse --corpus TAHOT LEV.17.11 --json` - the 18-word verse that started this
ticket - now emits `source_file` **zero** times (down from 18) and exactly one
`sources.TAHOT` entry with `file`/`license`/`attribution` populated from the real
`sources.json` row. New direct tests for `SourcesFor` cover the dedup itself
(many-Citations-one-edition -> one entry), multi-edition coverage, the empty-Edition
skip, and the unknown-edition error - plus every existing test asserting the old
`SourceFile`/`SourceLocator`/`Editions` shape was updated, not disabled. Full suite green.

### T32 - per-word transliteration  `DONE`
**Goal:** surface a pronunciation guide for the actual inflected word in a verse, not
just its dictionary lemma - readers of Greek but not Hebrew (or vice versa, or neither)
are a real, explicitly named audience (2026-07-01 session).
**Context:** `lexicon.translit` (T5) already stores a transliteration, but only one per
`dStrong` (the citation form) - CC BY 4.0, no permission caveat, already loaded for both
Greek (TBESG) and Hebrew (TBESH). Separately, the source files TAGNT/TAHOT ingest from
*do* carry a per-word transliteration column, but both ingesters (`tagnt/tagnt.go:117`,
`tahot/tahot.go`) currently strip and discard it - it's parsed only to isolate the
surface form, never stored.
**Decision:** store the real per-occurrence transliteration, not the lemma-level one.
A dictionary-form transliteration would silently paper over real inflection (Hebrew
construct/pausal vowel changes, Greek case endings) - the exact "label without
derivation" failure mode T24's output-footprint guard exists to catch, so it's worth
avoiding at the source rather than filtering downstream.
**Scope:**
- Add a `translit TEXT` column to `words` (nullable - Swete/OSS rows have no
  transliteration column in their source files and should store NULL, not a
  lemma-level fallback that would misrepresent itself as per-word).
- Stop discarding the transliteration field in `tagnt.go`/`tahot.go`; store it
  alongside `surface`.
- Add `Citation.Translit string \`json:"translit,omitempty"\`` (retriever.go); populate
  at every construction site that already reads `w.surface`/`w.lemma` (same sites T31
  touched: `retriever.go`, `concord.go`, `parse.go`, `attestation.go`).
- `cite.Cite`: add transliteration to the bracketed metadata clause (order TBD when
  built - likely right after the original-language `Text`).
**Acceptance:** a TAGNT/TAHOT parse result carries a real, verse-specific
transliteration per word; Swete/OSS rows carry none (NULL/omitted, not a
misleading substitute); existing tests for the four touched packages updated, not
disabled.

### T32 AS-BUILT (2026-07-03)

Built as scoped. `words.translit TEXT` (nullable) added to the schema. The two
ingesters now capture it instead of discarding it:
- **TAGNT** (`tagnt.go`): the Greek column is `"SURFACE (translit)"` in one field -
  the old `surfaceWord` (which threw the parenthetical away) was replaced by
  `surfaceAndTranslit` (a regex split, `^(.*) \(([^()]*)\)$`), returning both. Confirmed
  transliteration survives even for a compound-tagged word (dstrong/lemma NULL, but
  the whole span's transliteration is still real, storable data - "μήποτε (mēpote)"
  stores `translit="mēpote"` despite having no single dStrong).
- **TAHOT** (`tahot.go`): simpler - transliteration is already its own column
  (`fields[2]`, confirmed against the real header: `Eng (Heb) Ref & Type / Hebrew /
  Transliteration / Translation / dStrongs / ...`), just never read before. Now
  inserted verbatim, including the literal `"[ ]"` placeholder the source itself uses
  for an untagged Qere reading with no real transliteration - preserved as-is, not
  filtered or reinterpreted (same "verbatim, not fabricated" discipline the rest of
  this loader already follows for Ketiv/Qere markers).
- Both loaders share a tiny `nullIfEmpty` helper (duplicated, not extracted to a
  shared package - two three-line copies in independent loader packages isn't a
  Rule-of-Three violation) so a blank transliteration becomes SQL NULL, not an
  empty string standing in for "not applicable."

`retriever.Citation` gained `Translit string \`json:"translit,omitempty"\``.
Populated at the three real word-level construction sites - `concord.go`
(`ConcordLemma` directly from `w.translit`; `ConcordPhrase` via a new
`chainTranslit` helper), `parse.go`, `attestation.go`. **`retriever.go` itself
needed no change beyond the struct field** - confirmed by inspection that
`canonicalCitation`/`alignedCitations` (GetVerse/GetPassage) only ever query
`verse_text` tables (KJV/ASV/WEB/Brenton), never `words`, so there is no
per-word Citation built there to carry a transliteration in the first place.

**`chainTranslit`'s all-or-nothing join** (not in the original scope bullets,
decided while building): `ConcordPhrase` already joins each chain word's surface
text into one `Text` string, so transliteration should join the same way - but
only when *every* word in the chain actually has one. A partial join (real
translit for word 1, blank for word 2) would misrepresent the phrase's
pronunciation rather than honestly reporting "not available for this phrase,"
so any missing word blanks the whole joined field.

`cite.Cite`'s `metadata()` gained `Translit` as the **first** bracketed field -
right after the quoted `Text`, before `DStrong`/`Lemma`/`Grammar` - since
pronunciation is what a reader wants immediately alongside the text itself, ahead
of the more technical dStrong/grammar data.

**Validated against the real corpus**, not just the mini-fixtures: rebuilt the
full DB from the actual `bible-text`/`STEPBible-Data` roots (`go run ./cmd/build
... --verify` - full verify passed, all completeness checks green) and confirmed
real transliteration renders for both languages - `orthotomeo parse --corpus
TAGNT MAT.26.28` shows `[touto, ...]`, `[gar, ...]`, etc.; `orthotomeo parse
--corpus TAHOT LEV.17.11` - the exact 18-word verse that motivated T31's
citation-shape redesign - shows `[ki, H3588A, ...]`, `[Ne.fesh, H5315H, ...]`,
etc., and the `--json` form carries `"translit": "ki"` in the expected position.
New direct tests: `TestLoadExtractsTransliteration` +
`TestLoadExtractsTransliterationForCompoundWord` (tagnt),
`TestLoadStoresTransliterationColumnVerbatim` +
`TestLoadPreservesPlaceholderTransliterationVerbatim` (tahot),
`TestConcordLemmaPopulatesTranslit` +
`TestConcordPhraseJoinsTranslitWhenEveryWordHasOne` +
`TestConcordPhraseLeavesTranslitEmptyWhenAnyWordIsMissingOne` (concord),
`TestParsePopulatesTranslit` (parse), `TestAttestationPopulatesTranslit`
(attestation); `TestCiteWordCitationIncludesMetadataInFixedOrder` updated for the
new bracket position. Full suite green; scratch DB and CLI binary cleaned up
after verification.

### T33 - concord: surface-word matching  `DONE`
**Goal:** let a search match the exact word as it appears in the verse (the inflected/
vocalized `surface` form), not only its dictionary `lemma` or `dStrong` - raised
alongside T32 as the same audience gap (someone who can read the word on the page but
doesn't know its lemma).
**Decided (2026-07-01):** add `surface` as a third auto-detected match column;
**do not** widen `concord_lemma`/`Count` to span multiple corpora in one call - lemma
and dStrong meaning is corpus/language-specific (a Greek dStrong and a Hebrew dStrong
share nothing), so one-corpus-per-call stays the design; callers already loop per
corpus for a cross-language search.
**Scope:** `concord/concord.go`'s `matchColumn`/`columnExpr` currently choose only
`dstrong` (regex-shaped) or `lemma` (default). Extend so a query matching neither the
dStrong shape nor an existing lemma also tries `w.surface` - or add an explicit `--by
{lemma,dstrong,surface}` override so callers aren't at the mercy of a guess. Exact
match only (no fuzzy/diacritic-insensitive matching in this ticket - that's a
separate, harder problem given combining-diacritic variance already noted in the
corpus's own working conventions).
**Acceptance:** searching the literal surface text of a known word (e.g. the exact
pointed Hebrew or accented Greek form from a specific verse) returns that occurrence
via `concord_lemma`/`Count`, without requiring the caller to already know the lemma.

### T33 AS-BUILT (2026-07-03)

Built as the **explicit override**, not the auto-detected fallback the ticket offered
as an alternative - matching this codebase's own "explicit over magic: declared over
inferred, never infer from names/positions/patterns" principle. A dStrong number has
a fixed, unambiguous shape (`dstrongRe`), so auto-detecting it from the query string is
safe; a surface form and its lemma cannot be reliably told apart from text alone, so a
"try lemma, fall back to surface on zero rows" auto-detect would silently guess which
column the caller meant - the same class of magic this project avoids elsewhere.

`concord.ConcordLemma`/`Count` both gained a `by string` parameter (`""` = the
original two-way auto-detect, unchanged; `"lemma"`/`"dstrong"`/`"surface"` = explicit
override). `columnExpr` gained the `surface` -> `w.surface` case; an unknown `by`
value fails loudly via the existing "unknown match column" error, not a silent
fallback. `ConcordPhrase` was **not** touched - out of this ticket's scope, still
lemma-only for its per-token matching. Threaded through `engine.Engine`
(ConcordLemma/Count), the MCP tools (`concord_lemma`/`count` gained an optional
`by` arg), and the CLI (`orthotomeo concord --by lemma|dstrong|surface`).

**Validated against the real DB:** `orthotomeo concord ἄφεσις --corpus TAGNT` (lemma,
default) returns all 17 inflected NT occurrences; `orthotomeo concord ἄφεσιν --corpus
TAGNT --by surface` returns only the 12 sharing that exact accusative-singular
spelling - a real, meaningful narrowing, not a no-op. `--by bogus` fails with
`columnExpr: unknown match column "bogus"` rather than silently matching nothing.
New tests: `TestConcordLemmaBySurfaceMatchesExactInflectedForm` (surface text with a
different lemma spelling - Mat.1.1's "Βίβλος"/"βίβλος" case-difference fixture),
`TestConcordLemmaByExplicitLemmaDoesNotMatchSurfaceText` (proves `by` is a hard column
selector, not a widened search), `TestConcordLemmaRejectsUnknownByValue`,
`TestCountRespectsByOverride`. Every pre-existing `ConcordLemma`/`Count` call site
(9 in `concord_test.go`, 4 in `engine_test.go`) updated to pass `""` for unchanged
auto-detect behavior - no behavior change for any existing caller. Full suite green.

### T34 - lexicon / Strong's lookup  `DONE`
**Goal:** resolve a `dStrong` (already present on every word `Citation`) to its
lexicon entry - gloss and, where licensing allows, a fuller definition. Currently
`lexicon.gloss`/`lexicon.definition` are loaded (T5) but read by **no** query path
anywhere in the codebase.
**License split (checked 2026-07-01 against `sources.json`):**
- **TBESG (Greek):** `"CC BY 4.0 (definitions: Abbott-Smith 1922, Public Domain)"` -
  gloss and definition both clear to expose.
- **TBESH (Hebrew):** gloss is clear, but `definition` is flagged
  `"abridged BDB via Online Bible - permission required before applying definitions"`.
  **Explicitly out of scope for this ticket** (per this session's decision) - the
  Hebrew `definition` column stays dormant until permission is resolved separately;
  do not expose it as a side effect of building the Greek side.
**Scope:** new `lexicon` package (or a function on `retriever`), `Lookup(db, dStrong)
(Entry, error)` returning `{DStrong, Language, Lemma, Translit, Gloss, Definition
*string}` - `Definition` is `nil` for any TBESH row (gloss-only), populated for TBESG.
Expose as a new MCP tool / CLI subcommand / (later) HTTP endpoint, consistent with the
existing 5-tool/5-endpoint surface (`get_verse`, `get_passage`, `concord_lemma`,
`concord_phrase`, `parse`, `attestation` already there - this adds one more primitive,
not a rework of them).
**Acceptance:** looking up a Greek dStrong returns gloss + definition; looking up a
Hebrew dStrong returns gloss only, with `Definition == nil`, not an empty string
standing in for "not returned" (an empty string would be ambiguous with a genuinely
empty upstream field).

### T34 AS-BUILT (2026-07-03)

Built as scoped, replacing rather than shadowing the old `lexicon.Lookup(db,
dstrong) (lemma, gloss string, err error)` - confirmed via a full-repo grep that its
only callers were its own package's tests, so widening its signature outright (new
`Entry` type: `DStrong`, `Language`, `Lemma`, `Translit`, `Gloss`,
`Definition *string`) was a clean replacement, not a second near-duplicate lookup
function living alongside the first. The license gate is `e.Language != "he"`, not a
per-row license-string parse - `def_license` already carries the full flagged text
(`"BDB/Online Bible - permission"` vs `"Abbott-Smith PD"`, set at load time in
`cmd/build/main.go`), but language is the simpler, equally-correct partition since
the whole ticket's scope is literally TBESG-vs-TBESH.

**A real gap the tests catch explicitly:** the Hebrew fixture's `definition` column
is genuinely non-empty in the test data (`"father of an individual"`) - the test
(`TestLookupWithholdsDefinitionForHebrew`) asserts the raw column really does hold a
value AND that `Lookup` still returns `Definition == nil`, so the withholding is
proven to be a real gate, not an accident of empty test data.

Exposed as: `engine.Engine.Lookup(dstrong) (lexicon.Entry, error)`; MCP tool
**`lexicon_lookup`** (deliberately not named `lookup` - the CLI's `orthotomeo
lookup` subcommand already means something different, GetVerse's verse-text
retrieval; the MCP tool description also cross-references this so an LLM client
doesn't confuse the two); CLI subcommand **`orthotomeo define <dstrong> [--db
path] [--json]`** (`cmd/orthotomeo/define.go`, new file - renders one Markdown
line via a local `defineLine`, or the raw `lexicon.Entry` as JSON with
`definition` using `omitempty` so a Hebrew lookup's JSON has no `definition` key
at all, not a `null`).

**Validated against the real DB:** `orthotomeo define G0859` returns the full
Abbott-Smith definition text (Public Domain); `orthotomeo define H7225G` returns
gloss only, and its `--json` output has no `definition` key whatsoever (confirmed
directly, not assumed from `omitempty`'s general behavior); `orthotomeo define
G9999999` (unknown dStrong) fails with a clear `sql: no rows in result set`
wrapped error rather than a misleadingly "successful" empty result.

New tests: `TestLookupPopulatesDefinitionForGreek`,
`TestLookupWithholdsDefinitionForHebrew` (the real-column-not-empty proof above),
`TestLookupUnknownDStrongReturnsError` (lexicon); `engine_test.go`'s shared fixture
gained a seeded `lexicon` row and `TestEngineReachesEveryPhase5Operation` now also
calls `e.Lookup`. Full suite green.

### T35 - interlinear view  `DONE`
**Goal:** a row-aligned original/transliteration/gloss/morphology-under-translation
display - the composed reading view, not new engine data.
**Decided:** this is a response-*shape* ticket, not a new retriever capability. It
composes existing/planned primitives (`parse` for the word list, T32's `Translit` on
each `Citation`, T34's `Lookup` for gloss) rather than adding its own DB queries.
**Scope:** a rendering function (`engine`-level or a `cite`-adjacent package) that
takes a `parse` result plus per-word `lexicon.Lookup` calls and produces a stacked
per-word view: original text / transliteration / gloss / grammar, aligned under (not
replacing) the corpus's own text. Exposed wherever T27's web UI needs it - most
naturally as a display mode over the existing `/parse` response, not a new engine
method.
**Acceptance:** given a verse reference and corpus, renders one interlinear block per
word using only data T32/T34 already expose - no new original-language data sourced
for this ticket.

### T35 AS-BUILT (2026-07-03)

Built as scoped: new `interlinear` package (not folded into `cite` or `engine`
directly), `Build(db, citations) ([]Word, error)` - one `Word` per `Citation`, Gloss
resolved via `lexicon.Lookup` keyed by `DStrong`. No new DB queries beyond the
lookup itself; every other field is a straight carry-through from the Citation
(Text, Translit, Lemma, Grammar, Confidence, Caveat) - confirming the ticket's own
framing that this sources no new original-language data.

**Two failure-mode decisions made while building, not pre-specified:**
- A Citation with no `DStrong` (compound-tagged word, untagged reading, no-data
  placeholder) gets no gloss - skipped, not guessed.
- A `DStrong` with no lexicon row at all (the small, T14-documented cross-file gap)
  is *also* left gloss-less rather than failing the whole render - `Build` only
  propagates a lookup error that isn't `sql.ErrNoRows`, so a single missing
  dictionary entry never blocks an otherwise-good interlinear view.

Added `interlinear.Render([]Word) string` as `cite.Cite`'s counterpart for this new
shape (kept in `interlinear`, not added onto `cite.Cite` itself, since `Word` is a
display composition over a Citation, not a Citation - `cite.Cite`'s signature stays
exactly the Concord spec's `Cite([]Citation) string`).

Exposed three ways, mirroring T34's pattern:
- `engine.Engine.Interlinear(ref, word, corpus) ([]interlinear.Word,
  map[string]retriever.SourceInfo, error)` - also returns the T31 sources map,
  computed internally from the underlying Citations (the caller never needs to
  re-run Parse just to get provenance).
- MCP tool **`interlinear`** (reuses `wordScopedArgs`, same shape as `parse`/
  `attestation`; new `interlinearResult{Words, Sources}` wrapper).
- CLI **`orthotomeo interlinear <ref> --corpus C [--word N] [--json]`**
  (`cmd/orthotomeo/interlinear.go`, new file).

**Validated against the real corpus, not just fixtures:** the checked-in
`data/orthotomeo.db` predates T32's `translit` column (confirmed: querying it
directly fails with `no such column: translit`, since `CREATE TABLE IF NOT EXISTS`
never retroactively alters an existing table) - `data/*.db` is gitignored, a local
build artifact, not tracked, so this is expected, not a regression. Rebuilt a fresh
DB from the real `bible-text`/`STEPBible-Data` roots (full `--verify` passed) and
confirmed real interlinear output for both languages: `orthotomeo interlinear
--corpus TAGNT MAT.26.28` shows `[touto] — this/he/she/it`, `[gar] — for`, etc.;
`orthotomeo interlinear --corpus TAHOT --word 2 --json LEV.17.11` shows
`"translit": "Ne.fesh", "gloss": "soul: life"` with the T31 sources map (including
T36's `homepage_url`) attached.

New tests: `TestBuildResolvesGlossByDStrong`, `TestBuildLeavesGlossEmptyWhenNoDStrong`,
`TestBuildLeavesGlossEmptyWhenDStrongUnknownToLexicon`,
`TestBuildPreservesCaveatAndOrder`, `TestRenderEmptySlice`,
`TestRenderOneLinePerWordWithGloss`, `TestRenderShowsCaveatForFlaggedWord`
(interlinear package); `TestInterlinearOverMCP`,
`TestInterlinearRejectsInvalidWordNumberOverMCP`, `TestLexiconLookupOverMCP` (a real
end-to-end MCP round trip, not just the Go function - this also caught that
`lexicon_lookup` hadn't had its own over-the-wire test until now); CLI
`TestDefineReturnsGlossAndDefinition`, `TestInterlinearIncludesGlossFromLexiconLookup`,
`TestInterlinearJSONMatchesInterlinearPayloadShape`. Full suite green.

### T36 - source homepage/reference URLs  `DONE`
**Goal:** attach a human-facing reference link to each `sources.json` entry - "where
this project/data lives" (STEPBible's GitHub, eBible.org, etc.) - so attribution
surfaced by T31 (and shown in T27's print/sources footer) can link out, not just name
the license and local file path.
**Not the same field as `fetch_url`:** `fetch_url` is reserved for the actual download
endpoint of a non-shippable, user-fetched source (T23's Rahlfs LXX pattern) and is
empty for every shippable source today. This ticket adds a separate field for a
reference/homepage link that every source can carry, shippable or not.
**Scope:**
- Add `homepage_url TEXT` to the `sources` table (`store/schema.sql`) and
  `HomepageURL string \`json:"homepage_url,omitempty"\`` to `sources.Source`
  (`sources/sources.go`), threading it through `Registry()`/`Seed()`'s insert alongside
  the existing 10 columns.
- Populate `sources.json` per entry. Confirmed/high-confidence today:
  - `TAGNT`, `TAHOT`, `TBESG`, `TBESH`, `TEGMC`, `TEHMC`, `TVTMS` (7 STEPBible-Data
    sources): `https://github.com/STEPBible/STEPBible-Data`
  - `OpenBible-xref`: `https://www.openbible.info/labs/cross-references/`
  - `OSS-LXX-lemma`: `https://github.com/openscriptures/GreekResources` (matches the
    vendored `GreekResources-master/` directory name)
  - `KJV`, `ASV` (confirmed by Justin, 2026-07-02):
    `https://github.com/scrollmapper/bible_databases`
  - `WEB`, `Brenton` (confirmed by Justin, 2026-07-02): `https://ebible.org/`
  - `Swete`'s existing `attribution` already says "via archive.org" - resolve to the
    specific archive.org item identifier used, not the bare archive.org domain.
- `cite.Cite` / T27's sources footer: render `homepage_url` as a link when present;
  a source with no confirmed URL yet renders exactly as it does today (license +
  local file path only) - never a placeholder link to nowhere.
**Acceptance:** every source with a confirmed URL surfaces it in `SourcesFor`'s output
and, where applicable, as a clickable link in the web UI/print view; sources still
pending confirmation are unaffected (field omitted, not null-guessed).

### T36 AS-BUILT (2026-07-02)

Built as scoped, with one deliberate deviation from the original scope bullet.
`homepage_url TEXT` added to the `sources` table and `Source.HomepageURL` (both
`store/schema.sql` and `sources/sources.go`), threaded through `Registry()`/`Seed()`'s
insert. All 15 entries in `sources.json` got a real, confirmed URL except **Swete**
(still open - "via archive.org" isn't specific enough to be a real reference link;
left absent rather than guessed): 7 STEPBible-Data sources and OpenBible-xref and
OSS-LXX-lemma were already high-confidence; KJV/ASV (`scrollmapper/bible_databases`)
and WEB/Brenton (`ebible.org`) were confirmed directly by Justin (2026-07-02).

**Deviation:** the ticket's scope said "`cite.Cite` ... render `homepage_url` as a
link when present." Building it, this turned out to reintroduce exactly the
redundancy T31 exists to prevent - `homepage_url` is per-*edition*, not per-row, so
inlining it on every `cite.Cite` bullet would repeat the identical URL once per
Citation in a multi-word result (an 18-word verse would print the same link 18
times). Kept it **only** on `retriever.SourceInfo` (the once-per-edition map T31
already established), not on `cite.Cite`'s Markdown line - consistent with, not a
regression from, the existing precedent. `cite.Cite`'s rendered output is therefore
unchanged by this ticket.

**Validated against the real DB:** `orthotomeo parse --corpus TAHOT --db
data/orthotomeo.db --json LEV.17.1` now emits `"sources":{"TAHOT":{...,
"homepage_url":"https://github.com/STEPBible/STEPBible-Data"}}`. No DB rebuild was
needed for this - confirmed `SourcesFor` resolves purely from the embedded
`sources.json` via `sources.Registry()`, never queries the DB's `sources` table, so
the checked-in `data/orthotomeo.db` (built before this schema change) still works
correctly; only a future `cmd/build` run would also carry `homepage_url` into the
DB's own `sources` table (used for the FK relationship, not for citation output).
New tests: `TestHomepageURLPopulatedExceptSwete` (registry-level spot check) and
`TestSeedRoundTripsHomepageURL` (confirms the column actually reaches the DB via a
real `Seed` insert, not just the in-memory `Registry()`). Full suite green.

---

# Phase 7 - Deferred / v2

### T21 - cross_references (OpenBible / TSK)  `DONE`
Loaded: `crossrefs.Load` reads `cross_references.txt` (CC-BY), resolves OSIS via the
`verses.Resolver`, inserts to `cross_references` with `to_verse_end` (ranges) and signed
`votes`. Full build: 344,794 inserted / 5 skipped (reported, not dropped). `OpenBible-xref`
source added; `crossref` added to allowed types. Original spec below.

### T21 (original spec) - cross_references (OpenBible / TSK)
Load `cross_references.txt` (TSV `From | To(+range) | Votes`, OSIS refs, CC-BY, ~344,799
rows). Schema: `cross_references (id, from_verse FK, to_verse FK, to_verse_end FK NULL,
votes, source_id, kind)`. Add `crossref` to `sources.type`; add an `OpenBible-xref`
source row. Resolve OSIS via book_names. Keep negative-voted rows (data, filter at query).
Do NOT LLM-synthesize cross-refs. Phrase-anchored pure-Torrey's TSK = later upgrade.

### T22 - word_alignment (Swete <-> OSS aligner)  `V2`
Per-verse sequence alignment (LCS / Needleman-Wunsch) linking Swete surface rows to OSS
lemma rows where they correspond (~74-83% free, rest via the aligner). Table
`word_alignment (word_a FK, word_b FK, confidence)`. Enables word-level surface+lemma
without CCAT. **Shares the sequence-alignment core with the T4b verse aligner** - build
one generic deterministic aligner and apply it at verse granularity (T4b) and word
granularity (T22). Same determinism guarantee (invariant #9): re-runnable, no LLM.

### T23 - Rahlfs LXX user-fetch  `V2`
Optional `cmd/build --fetch-lxx` step: fetch eliranwong/LXX-Rahlfs-1935 (CC-BY-NC-SA,
CCAT-derived) to the user's machine under explicit license acceptance; load as `words`
with `source.shippable=0`. NEVER bundled. Gives LXX parse + Strong's join.

### T24 - output footprint guard  `V2`
Concord spec §10: dumb external gates over a generated study doc - grounding (every
original-language claim carries a Citation), completeness (cited set == Concord set),
label-without-derivation, commentary/conclusion register. Flags, never rewrites.

### T29 - Hebrew<->Greek heuristic bridge without the LXX  `V2`
**Goal:** answer "what's the Hebrew/Greek equivalent of this word" for common
vocabulary (not just proper names) in cases where the LXX itself isn't the right
tool - or as a second, independent check alongside it.
**Context (2026-07-01 session):** investigated live via `ὀρθοτομέω` (only 3
corpus occurrences: 2Ti.2.15, and its own LXX translation of Hebrew `יָשַׁר` at
Prov.3.6/11.5). Confirmed what does and doesn't already bridge the two languages:
- `lexicon.ustrong` (T5, already loaded) DOES link Greek and Hebrew rows sharing a
  `ustrong` value, but only for **proper names/named entities** (verified:
  `H0085`/`G0011` group to "Abraham," `H0054`/`G0008` to "Abiathar") - it comes
  from STEPBible's own `TBESG`/`TBESH` files, not a separate resource. Does not
  help for common words - `ὀρθοτομέω`'s own `ustrong` is just itself, ungrouped.
- `cross_references` (T21, 344,794 rows loaded) could in principle carry
  "NT verse directly quotes this OT verse" links, which would let a Greek NT
  word be compared straight against its Hebrew Vorlage with no LXX involved -
  but every currently-loaded row has `kind = "thematic"` (undifferentiated
  OpenBible/TSK topical links). A curated NT-quotation-specific list is a
  **different dataset** than what's loaded, not something already sitting
  unused in this data.
- STEPBible-Data has two more unloaded resources, checked directly on disk:
  - `Proper Nouns/TIPNR` ("Translators Individualised Proper Names with all
    References") - richer than `TBESG`/`TBESH`'s built-in name-grouping, but
    still proper-names-only. Doesn't solve the general-vocabulary case either.
  - `Tagged-Bibles/TTESV` ("Tyndale Translation tags for ESV") - tags the ESV's
    English wording to underlying Strong's numbers across **both** Testaments.
    Closest real lead: matching where the ESV renders the same English word/
    phrase in both an OT and NT verse gives a candidate Hebrew-Greek link
    without going through the LXX at all - grounded in actual whole-Bible
    translator decisions, not a two-word dictionary gloss. Still a heuristic,
    not a certainty (English polysemy can produce false positives - a lead to
    verify against usage, never a lookup to trust directly). **License note:**
    CC BY-NC - more restrictive than every currently-bundled source. If ever
    added, follow the **same pattern as T23's Rahlfs LXX** (`sources.shippable=0`,
    user-fetched, never bundled in the repo) rather than treating "this project
    won't be sold" as sufficient - an NC term restricts commercial use by anyone
    downstream of a public repo, not just the original publisher's own intent.
- Ruled out: `Older Formats/TOTHT` is a legacy predecessor of the already-loaded
  `TAHOT` (superseded, no new capability). `Versification/TVTMS` tracks verse-
  *numbering* differences across traditions, not word-sense equivalence (and
  T4b already deliberately doesn't use a TVTMS rule engine).
- Also available with **zero new data**: heuristic candidate-generation by
  matching `TBESG`/`TBESH`'s existing `gloss` text between languages (e.g. both
  glossed "straight"/"make straight"). Same caveat as TTESV - a lead, not a
  lookup.
**Not scoped further** - this is a research note, not a committed design. Any
future ticket built from it should keep whatever it returns clearly labeled as
a candidate/heuristic match (Confidence:Flagged-equivalent), never presented
with the same certainty as a real LXX-mediated or cross-reference-mediated link.

---

### Post-T36 fixes (2026-07-05): review pass, schema-drift guard, concordance false negative

A broad self-review (schema-drift risk, book-resolution edge cases, web UI
completeness, project health) plus one piece of real usage feedback produced
five fixes, each committed separately:

1. **Schema-version guard.** Hit twice in one session: `CREATE TABLE IF NOT
   EXISTS` never retrofits an existing table, so a DB built before
   `schema.sql` gained a column (`words.translit`, then
   `sources.homepage_url`) opened fine and only failed deep inside a query
   with `no such column`. `store.ApplySchema` now stamps `PRAGMA
   user_version` with a `store.SchemaVersion` constant (bumped whenever
   `schema.sql` changes); `engine.Open` checks it and fails fast with an
   actionable "delete it and rebuild" error instead.
2. **MCP book-name resolution.** The earlier LUK/Luke case-insensitivity fix
   deliberately left `cmd/orthotomeo-mcp`'s `ref()` helper alone, on the
   assumption the schema's "canonical USFM book code" wording would keep an
   LLM client in line - nothing enforces that. `ref()` now resolves through
   `Engine.ResolveBookCode` like every other transport; schema doc comments
   updated to state both accepted forms.
3. **Print-CSS gap.** `.status-row`/`#backLink` weren't in `@media print`'s
   hidden-elements list, so a printed page could show loading/error text and
   the "‹ back" link. One-line fix.
4. **README drift.** MCP tools list said "ten," missing `interlinear` and
   `lexicon_lookup` (real count: twelve); no HTTP API or desktop-app section
   existed despite both being real, tested transports; CLI/library examples
   were stale. Brought current.
5. **Concordance false negative (real usage report, the significant one).**
   `concord_phrase ["ὕδωρ","πνεῦμα"]` against TAGNT came back empty even
   though John 3:5 plainly has both, in order, in range. Root cause:
   STEPBible's TAGNT source gives some irregular nouns a compound citation
   form - nominative and genitive joined by ", " (`"ὕδωρ, ὕδατος"`, since
   ὕδωρ's genitive stem isn't predictable from its nominative) - stored
   verbatim as one `words.lemma` string. Every match site (`ConcordLemma`,
   `Count`, `ConcordPhrase`'s anchor and chain-extension steps) did exact
   string equality against the whole column, so a bare-form query silently
   missed every compound-tagged occurrence. Not a one-off: 51 distinct
   compound lemma strings / 3306 word rows in TAGNT alone, covering common
   words (Δαυείδ/David, Μωϋσῆς/Moses, Ναζαρέθ/Nazareth, τρεῖς/"three"). This
   is exactly invariant #3's failure mode in the wrong direction: it failed
   to find rather than failing loudly, indistinguishable from a real empty
   result unless a caller cross-checked. Fixed in the matching predicate
   (`concord.matchClause`), not the loader - rewriting the stored lemma would
   lose real lexical information (the compound form documents the actual
   citation-form convention). `matchClause` now matches a query against any
   single `", "`-delimited component of a `lemma` column too (`dstrong`/
   `surface` untouched, they never take this shape); the LIKE patterns
   require the exact `", "` boundary on each side, so a partial substring
   (e.g. `"ὕδα"`) can never false-positive match, and wildcard characters in
   the query are escaped defensively. Validated against the real corpus DB
   (read-only): the phrase query now finds John 3:5, plus Acts 8:39 - a
   second real match equally invisible before.

All five: full suite green, `orthotomeo-desktop.exe` rebuilt where static
assets changed.

**Sixth fix (2026-07-06), from the same real-usage feedback thread: case-
sensitive lemma matching.** Querying a proper noun's lemma (e.g. lowercase,
correctly-accented "ιησους"/Jesus) returned 0 results even though the corpus
carries 988 real occurrences of the capitalized `Ἰησοῦς` - `lexnorm.NFC`
only normalizes Unicode composition, never case. 579 of TAGNT's 5407
distinct lemma strings start capitalized (proper nouns), so this was broad,
not an edge case.

**Design decision (asked, not assumed):** case-insensitive lemma matching is
now the default - ancient manuscripts had no letter-case distinction at all;
capitalizing proper nouns in a printed citation form is a modern editorial
convention, not something the text itself distinguishes, so folding it is
textually faithful, not a loosened rule. This does create one real
ambiguity: 6 TAGNT lemmas are genuinely different words spelled identically
apart from case (e.g. `Στέφανος` "Stephen" vs `στέφανος` "crown" - 25 real
occurrences split between both senses). Decided: return the complete union
(never guess which sense was meant), but every Citation whose own
`morph_code` carries STEPBible's `Name type=` tag (a proper-noun marker
already present in the loaded `morph_codes` data) gets an automatic caveat -
driven by the word's actual morphology tag, not its surface capitalization,
so it fires correctly even where the source's own capitalization is
inconsistent (verified live: Acts 8:2's `στέφανος` is stored lowercase in
the corpus but still correctly caveated as Stephen).

`concord.resolveLemmaVariants` resolves a lemma query to every real stored
lemma string that case-insensitively names the same word (composing with
the ὕδωρ/ὕδατος compound-form fix - `matchClause` now ORs a clause per
resolved variant); `nameTypeCaveat`/`appendCaveat` compose the new note onto
whatever caveat a word already carried, rather than overwriting it.
`dstrong`/`surface` matching is untouched - the case convention is
lemma-specific.

Validated against the real corpus DB (read-only): lowercase `Ἰησοῦς` query
returns all 988 occurrences with the name-type caveat; `στέφανος` query
returns all 25 real occurrences (both senses), caveat present on every
Stephen row, absent from every crown row. Full suite green.

---

## Dependency summary

```
DONE: T1 -> T2 -> T3, T4a, T4b, T5 -> T6, T7 -> T8, T9, T10, T11, T12, T13, T14, T21
T4a (verses spine) -> T9 (Brenton, per-edition, DONE), T12 (Swete, DONE), T13 (OSS, DONE)
T4a,T5,T6 -> T10 (TAGNT, DONE), T11 (TAHOT, DONE)
T9,T12,T13 -> T4b (deterministic verse aligner, DONE - the align package's
     AlignWeighted/FillGap core is reusable for T22)
T10-T13 -> T14 (completeness self-test, DONE) -> Phase 5 (T15-T19 ALL DONE)
Phase 5 (T15..T19, DONE) -> T25 (engine facade / the seam, DONE) -> {T20 MCP DONE, T26 CLI DONE, T27 HTTP+web DONE}
T27 (HTTP+web, DONE) -> T28 (desktop launcher, DONE - Footsteps pattern, Gio not Fyne)
T20 (MCP, DONE) -> T31 (attribution/license surfacing, DONE) -> T30 (public remote MCP:
     Streamable HTTP + GCP Cloud Run, single instance, in-app rate limiting)
T5 (lexicon, DONE) -> T32 (per-word transliteration, DONE), T34 (Strong's/lexicon
     lookup, DONE - Hebrew definition excluded pending permission)
T16 (concordance, DONE) -> T33 (concord: surface-word matching, DONE)
T32, T34 (both DONE) -> T35 (interlinear view, DONE) -> T27 (consumed by /interlinear)
T31 (attribution/license surfacing, DONE) -> T36 (source homepage/reference URLs, DONE
     - Swete's specific archive.org identifier still open, all other 14 confirmed)
V2 after deps: T22 (word align, can reuse align package), T23, T24, T29 (research
     note only, not yet scoped)
```

Recommended next executable order: **T30** (public remote MCP over Streamable HTTP
on GCP, unblocked since T31 is done) is the only remaining unblocked ticket outside
V2 - **T28** (desktop launcher) is now DONE too. All of Phase 3 (text/word import),
T4b, T14, all of Phase 5 (T15-T19), T25, T20, T26, T27, T28, T31, T32, T33, T34,
T35, and T36 are now DONE.
