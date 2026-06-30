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
T4b aligner, not a load-time mapping).
Next: T10/T11 (TAGNT/TAHOT words - the complete-or-fail foundation), then T12/T13
(Swete/OSS LXX words), then T4b (the deterministic verse aligner: sequence alignment
over parsed data, no TVTMS rule engine, no hand-curation, no LLM - invariant #9), then
T14. See PLAN.md's T4 "DECISION" block for the full per-edition-versification design.
