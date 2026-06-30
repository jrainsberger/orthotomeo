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

Governing design doc (Phase 4+): `D:\Claude\Bible\Teaching\tools\concordance-retriever-spec.md`
("Concord spec"). Data model: `docs/erd-v1.svg`. Go style: `D:\Claude\Conventions\go.md`.

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

## Corpus locations (this machine)

The builder takes `--corpus <root>` (default to a documented dev path). Sources
resolve their `source_file` glob (in `sources/sources.json`) against the corpus.
Current on-disk layout (split across two parents - Ticket 3 reconciles this):

| Tree | Path |
|---|---|
| STEPBible-Data | `D:\Reference\STEPBible-Data` |
| LXX-Swete-1930 | `D:\Reference\LXX-Swete-1930` |
| bible-text (KJV/ASV/WEB/Brenton/OSS) | `D:\Claude\Bible\bible-text` |
| cross_references.txt (OpenBible/TSK) | `D:\Claude\Bible\cross_references.txt` |

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

---

# Phase 4 - Integrity

### T14 - completeness self-test  `BLOCKED` on T10-T13
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

# Phase 5 - Retriever engine (the Concord tool surface)

Read-only. Every call returns `Citation`-bearing results (Concord spec §5). Implement
against the built DB; the engine is the deterministic reference monitor.

### T15 - Citation + reference resolution  `BLOCKED` on Phase 3
`Citation` type (ref, edition, verbatim text, source_file+locator, lemma?, dstrong?,
morph?, attestation?, confidence, caveat). `ResolveRef`, `GetVerse`, `GetPassage`.
**Acceptance:** GetVerse returns verbatim per-edition text with provenance; a ref that
diverges across editions returns per-edition addresses + caveats, never a silent shift.

### T16 - concordance (the killer feature)  `BLOCKED` on T15
`ConcordLemma(lemma|dstrong)`, `ConcordPhrase(tokens, {adjacent|window:N})`, `Count`.
**Complete-or-fail**: return every matching word row or raise. `Count` and `Concord`
over the same query MUST agree (built-in completeness check).
**Acceptance:** `ConcordLemma(G0859)` returns all aphesis rows incl. the Matt 26:28
control case; `ConcordPhrase(["eis","aphesis"], adjacent)` returns the full NT set;
`Count == len(Concord)` for every tested query; a forced partial read raises.

### T17 - parse / lemmatize  `BLOCKED` on T15
`Parse(ref, word?)` (dstrong + expanded morph), `Lemmatize(ref)` (ordered lemma list).
**Acceptance:** Parse returns morph_code expansion via T6; LXX parse flagged (Swete has none).

### T18 - attestation  `BLOCKED` on T15
`Attestation(ref, word?)` -> the Type/Editions columns as neutral text-critical data
(e.g. Mark 16:9-20 = KO). No argument, just data.

### T19 - Cite renderer  `BLOCKED` on T15-T18
`Cite([]Citation) -> string` in the `Teaching/Studies/*-references.md` format - the only
sanctioned bridge from engine output to a study deliverable.

---

# Phase 6 - MCP server

### T20 - MCP surface  `BLOCKED` on Phase 5
Expose the engine tools over MCP (read-only). Natural-language queries are allowed but
MUST route to the deterministic tools (ConcordLemma/ConcordPhrase), never generate raw
SQL (the lexica anti-pattern). The MCP server is the engine; the LLM client is the
analysis layer. Tool definitions: provider = latest Claude per `D:\Claude\Bible` API
guidance if a client is built.
**Acceptance:** each tool callable over MCP returns Citation-bearing JSON; completeness
guarantees hold across the boundary.

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

---

## Dependency summary

```
DONE: T1 -> T2 -> T3, T4a, T4b, T5 -> T6, T7 -> T8, T9, T10, T11, T12, T13, T21
T4a (verses spine) -> T9 (Brenton, per-edition, DONE), T12 (Swete, DONE), T13 (OSS, DONE)
T4a,T5,T6 -> T10 (TAGNT, DONE), T11 (TAHOT, DONE)
T9,T12,T13 -> T4b (deterministic verse aligner, DONE - the align package's
     AlignWeighted/FillGap core is reusable for T22)
T10-T13 -> T14 -> Phase 5 (T15..T19) -> T20
V2 after deps: T22 (word align, can reuse align package), T23, T24
```

Recommended next executable order: **T14** (integrity - the complete-or-fail
self-test over T10-T13's words rows and T4b's verse_alignment), then Phase 5 and
6. All of Phase 3 (text/word import) and T4b are now DONE.
