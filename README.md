# orthotomeo

A read-only scripture-study **engine**: imports a multi-edition biblical corpus
(STEPBible, public-domain English, Swete/OSS LXX) into a derived SQLite database,
then serves verbatim, provenance-tagged lookups and complete-or-fail concordance
over it via an MCP surface. The engine owns *text*; an LLM client owns *meaning*.

Open-source target: **MIT code + per-source data licenses** (CC-BY / public domain).
Non-redistributable sources (e.g. CCAT-derived Rahlfs LXX) are user-fetched, never bundled.

## Start here

- **[docs/PLAN.md](docs/PLAN.md)** - the full phase/ticket roadmap. Execute tickets
  in order; each is independently testable and leaves the repo green. **Read the
  cross-cutting invariants first.**
- **[docs/erd-v1.svg](docs/erd-v1.svg)** - the data model.
- **`D:\Claude\Conventions\go.md`** - Go style (binding).
- **`D:\Claude\Bible\Teaching\tools\concordance-retriever-spec.md`** - the engine
  thesis (Concord spec), governing Phase 4+.

## Build

```sh
go run ./cmd/build --out data/orthotomeo.db   # regenerate the derived DB
go test ./...                                  # all green
```

The DB is a build artifact (gitignored); the corpus files are the source of truth.

## Status

Done: T1 (sources), T2 (books), T4a (verses spine, 31,102), T21 (cross-references, 344,794),
T5 (lexicon, 22,717), T6 (morph codes, 2,565), T3 (corpus locator), T7 (KJV+ASV text, 62,204),
T8 (WEB text, 31,095/31,102 - 7 documented textual-critical divergences),
T9 (Brenton LXX, 22,690 verses / 920 chapter files, its own `versification='lxx-brenton'` -
not forced onto the KJV spine; canonical correspondence is the separate deterministic
T4b aligner, not a load-time mapping),
T10 (TAGNT, 141,746 words across both Greek-NT TSVs, 0 unresolved verses),
T11 (TAHOT, 305,174 words across all four Hebrew-OT TSVs, 478 documented skips
(Psalm-title verses with no English/standard counterpart) - Hebrew prefix+root
words resolved to the root per invariant #5),
T12 (Swete LXX, 476,937 words, its own `versification='lxx-swete'`, surface-only
- no lemma/dstrong/morph, Swete carries none),
T13 (OSS LXX lemma, 425,299 words across 34 in-scope books, its own
`versification='lxx-oss'`, lemma-only - multi-recension and deuterocanon files
intentionally unloaded, see PLAN.md),
T4b (deterministic verse aligner: chapter-level alignment by verse COUNT
- `align.AlignWeighted` - then verse-level position/count fill - `align.FillGap`
- never verse-number label matching; no TVTMS rule engine, no hand-curation, no
LLM - invariant #9. One documented limitation: a within-chapter leading-title
insertion's exact position is underdetermined by counts alone, reported as
low-confidence rather than guessed; see PLAN.md's T4b "AS-BUILT" notes),
T14 (completeness self-test: `verify` package + `cmd/build --verify`, making
invariant #3 enforceable - source_id/FK integrity, full-canon book coverage,
lemma-count read-agreement, and known per-edition totals, each proven against
a deliberately corrupted fixture. Running it for real found and fixed a
genuine TAHOT `morph_code` parsing bug - see PLAN.md's T14 "AS-BUILT" notes),
T15 (Citation + reference resolution: `retriever` package - `ResolveRef`,
`GetVerse`, `GetPassage` - guaranteeing cross-edition divergence is always a
`Caveat`, never a silent shift. Smoke-testing it against the real DB found a
second, much larger silent-drop bug: both T10 and T11's loaders dropped every
row whose ref carried STEPBible's `(EditionChapter.EditionVerse)` cross-
reference suffix, undetected by any counter - 26 TAGNT rows, 21,440 TAHOT
rows (nearly all of Psalms, where the Hebrew title-as-verse-1 convention
triggers the suffix on almost every verse). Fixed in both loaders; see
PLAN.md's T10/T11 UPDATE and T15 "AS-BUILT" notes),
T16 (concordance - the killer feature: `concord` package - `ConcordLemma`,
`ConcordPhrase`, `Count` - complete-or-fail over the built DB, each raising
rather than returning a silently truncated result if an independent
`COUNT(*)` and a full row scan ever disagree. Validated against the real DB
exactly matching the Concord spec's own worked example: `ConcordLemma`
returns all 17 ἄφεσις rows including the Matt 26:28 control case;
`ConcordPhrase(["εἰς","ἄφεσις"], adjacent)` returns the full 5-occurrence
NT set - see PLAN.md's T16 "AS-BUILT" notes),
T17 (parse/lemmatize: `parse` package - `Parse(ref, word?, corpus)` returns
dStrong + T6-expanded morphology (e.g. `N-ASF (Function=Noun; Case=
Accusative; Number=Singular; Gender=Feminine)`), `Lemmatize(ref, corpus)`
the ordered lemma list. LXX corpora are always `Confidence:Flagged` with a
caveat naming exactly what's missing (Swete: no morph at all - T12;
OSS-LXX-lemma: same - T13) - the word itself is still returned, only the
missing morphology is flagged, never the whole row silently dropped - see
PLAN.md's T17 "AS-BUILT" notes).
Phase 3 (text/word import), T4b, T14, T15, T16, and T17 are now complete.
Next: T18 (attestation). See PLAN.md's T4/T14/T15/T16/T17
"DECISION"/"AS-BUILT" blocks for the full per-edition-versification,
aligner, verify, retriever, concordance, and parse design.
