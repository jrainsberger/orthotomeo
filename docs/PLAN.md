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

### T3 - corpus locator  `NEXT`
**Goal:** one place that maps a source to its absolute file(s), so loaders never
hard-code paths.
- New `corpus` package: `Locate(src sources.Source, root string) ([]string, error)`
  resolving the `source_file` glob against `--corpus` root(s).
- Support the split layout above: accept multiple roots OR a single root with
  expected subdirs (`STEPBible-Data/`, `LXX-Swete-1930/`, `bible-text/`,
  `cross_references.txt`). Recommend documenting one `--corpus` dir and having
  the user symlink the three trees + file under it.
- `cmd/build` gains `--corpus` flag, threaded to loaders.
**Schema:** none.
**Acceptance:** `Locate` returns the right file set for each of the 13 sources
against a temp fixture tree (inline a tiny fake layout); `ErrCorpusMissing`
sentinel when a required tree is absent. Glob expansion is deterministic (sorted).
**Notes:** keep `source_file` patterns logical (already in sources.json); the
locator is the only path-aware code.

### T4 - verses spine + versification_map (TVTMS)  `T4a DONE` · T4b deferred
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

### T7 - verse_text: KJV + ASV (JSON)  `BLOCKED` on T4
**Goal:** English prose, the editions the deliverables quote.
**Scope:** `verse_text` table `(id, verse_id FK, source_id FK, native_ref, text)`.
Parse `bible-text/KJV/KJV.json` and `ASV/ASV.json` (identical shape: books[].chapters[].verses[]).
Resolve book via `name-en`, verse via `verses.Resolve`.
**Schema delta:** `verse_text`.
**Acceptance:** KJV row count == verses count (31,102); Gen.1.1 / John.3.16 text
matches the file verbatim; every row resolves a `verse_id` and carries the right `source_id`.
**Notes:** confirm `name-en` values match KJV.json book strings exactly (e.g.
"Song of Solomon", "Revelation"); reconcile any mismatch into book_names here.

### T8 - verse_text: WEB (USFM)  `BLOCKED` on T7
Parse `bible-text/WEB/*.usfm` (`NN-XXXeng-web.usfm`). Strip `\w word|strong=...\w*`
to the bare word, drop `\f...\f*` footnotes and `\c/\v/\p` markers to recover prose.
Resolve book via `usfm` scheme. Skip front matter / glossary files (00-FRT, 106-GLO).
**Acceptance:** Gen/John prose matches after stripping; chapter/verse counts within
documented WEB versification; no USFM markup leaks into `text`.

### T9 - verse_text: Brenton LXX (HTML)  `BLOCKED` on T4
Parse `bible-text/LXX/eng-Brenton_html/*.htm`. Extract `<span class="verse" id="VN">`
+ following text; strip footnote `<a class="notemark">`/`<span class="popup">` and the
bottom `.footnote` block. Map LXX versification -> canonical via `verses.Resolve`.
**Acceptance:** a known verse extracts clean (no HTML, no footnote markers); LXX
Psalm-offset verse lands on the right canonical `verse_id`; 66-book scope only.

### T10 - words: TAGNT (Greek NT)  `BLOCKED` on T4, T5, T6
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

### T11 - words: TAHOT (Hebrew OT)  `BLOCKED` on T4, T5, T6
Same `words` shape, from `STEPBible-Data/.../TAHOT*.txt`. Hebrew morphology, Aramaic
sections, **Ketiv/Qere** preserved (record both as data, do not collapse). Resolve book
via `dotted`, dstrong via lexicon (Hebrew).
**Acceptance:** counts match; Gen.1.1 word #1 in/be-, dstrong present; a Ketiv/Qere
verse keeps both readings; every row carries `source_id` for TAHOT.

### T12 - words: Swete LXX (Greek surface)  `BLOCKED` on T4
Parse `LXX-Swete-1930/01-Swete_word_with_punctuations.csv` (index -> surface) +
`00-Swete_versification.csv` (word-index -> ref). Build per-word rows with `surface`
only (lemma/dstrong/morph NULL - Swete carries none). Treat Swete text as Public
Domain (cite archive.org origin); do not ship the GPL CSV's transliteration -
regenerate if needed. 66-book scope only (skip deuterocanon).
**Acceptance:** Gen.1.1 has the right surface forms in order (epoiesen at position 3);
word count per verse matches the versification file's deltas; rows carry Swete `source_id`,
NULL lemma.
**Notes:** parallel per-source stream - NOT merged with OSS (see T13).

### T13 - words: OSS LXX lemma  `BLOCKED` on T4
Parse `bible-text/LXX/GreekResources-master/LxxLemmas/<Book>.js` (JSON objects keyed
`Book.C.V` -> array of `{key, lemma}`). Build per-word rows with `lemma` only (surface
NULL). A separate stream from Swete - **do not assume word-position identity** (verified:
exact-count match Gen 74%, Daniel 58%). Cross-source lemma use joins at the *verse* level
until the T22 aligner exists.
**Acceptance:** Gen.1.1 lemma sequence matches the file (en, arche, poieo, ...); rows
carry OSS `source_id`, NULL surface; per-verse lemma counts logged for later alignment.

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
without CCAT.

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
T1 -> T2 -> T3
            T4 (DESIGN) ----> T7 -> T8
            T5 -> T6              T9
            T4,T5,T6 -> T10, T11        T4 -> T12, T13
            T10-13 -> T14 -> Phase 5 (T15..T19) -> T20
            V2: T21, T22, T23, T24 (independent, any time after their deps)
```

Recommended next executable order: **T5, T6** (lexical ref data, no verse dep),
then **T3**, then the **T4 design huddle**, then T7-T13, T14, Phase 5, Phase 6.
