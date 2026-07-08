# orthotomeo

**ὀρθοτομέω** — "to cut straight," from 2 Timothy 2:15: *"Be diligent to present
yourself approved to God, a worker who does not need to be ashamed, cutting
straight the word of truth."*

orthotomeo is a read-only scripture-study **engine**. It imports a multi-edition
biblical corpus (Greek New Testament, Hebrew Old Testament, Septuagint, and
public-domain English translations) into a derived SQLite database, then serves
verbatim, provenance-tagged lookups and complete-or-fail concordance over it -
through an MCP server, a CLI, or as a Go library.

## Why it's built this way

Three texts shape the engine's design, not just its name:

- **Romans 3:4** - *"let God be true, and every man a liar."* Human sources -
  lexicons, grammars, commentaries, an LLM's own training - yield to the text
  itself whenever the two conflict. The engine has no lexicon-category shortcut
  for settling a word's sense; it returns the raw text and lets every occurrence
  speak.
- **1 Peter 4:11** - *"if any man speak, let him speak as the oracles of God."*
  Speech *about* scripture is accountable to scripture as its own oracle. A
  citation the engine returns must be traceable to an actual row in an actual
  source file - never a summary, a paraphrase, or a claim from memory.
- **2 Timothy 2:15** - the commission this project is named for, not just framed
  by. "Cutting straight" isn't a metaphor for careful reading in general; it's
  an explicit charge to handle the text with exactly this kind of precision -
  which is why the engine's core operation is complete-or-fail concordance
  (**every** matching occurrence, or an error - never a silent partial answer)
  rather than a best-effort search.

Practically, this means: **the engine owns *text*, the LLM client owns
*meaning*.** It never interprets, never argues a position, and never quotes
original-language text it didn't get from its own database. Given a query, it
either returns the complete, provenance-tagged result set, or it raises an
error - it does not silently truncate, guess, or paraphrase.

## Design principles

- **The content itself is never curated by an LLM.** Every word, verse, and
  lemma a caller receives comes straight from the checked-in corpus files
  (STEPBible, Open Scriptures, public-domain translations) through a
  deterministic SQL query - no model ever selects, summarizes, paraphrases,
  or decides what's "relevant" before it reaches you. An LLM client can
  reason *about* what the engine returns; it never touches what the engine
  *is*. The one place an LLM's own judgment enters is display formatting it
  builds on top (e.g. how it phrases an answer using the returned Citations)
  - never the underlying text, and the tools exist precisely so that
  judgment is checkable against the real data, not offered on its own
  authority.
- **Complete-or-fail, never a silent partial answer.** A concordance query
  returns *every* matching occurrence or raises an error - it never
  truncates, samples, or guesses at "close enough."
- **Provenance always.** Every citation traces to a real row in a real
  source file (edition, license, attribution, locator) - never a summary,
  a paraphrase, or a claim from memory.
- **Reconcile at read time, never assume 1:1 across editions.** The five
  text traditions disagree on versification and canon; cross-edition
  divergence is surfaced as data (a Caveat) at the moment it's found, never
  hidden or silently forced onto one edition's numbering.
- **Textually faithful defaults, not modern-convention defaults.** Where a
  modern editorial convention doesn't reflect anything in the manuscripts
  themselves (e.g. letter case, which ancient Greek/Hebrew manuscripts
  didn't have at all), the engine doesn't treat it as load-bearing by
  default - but flags it via a Caveat wherever the convention *does* carry
  real disambiguating information (e.g. a proper name spelled identically
  to a common noun apart from case).
- **Read-only, and the database is disposable.** The engine never writes;
  the derived SQLite database is a regenerable build artifact, never
  checked in and never the source of truth - the corpus files are, and the
  database can be deleted and rebuilt from them at any time.

## What's inside

- A derived **SQLite database** built from the corpus (`cmd/build`) - a
  regenerable artifact, not checked in. The corpus files are the source of
  truth; the DB is just an index over them.
- A Go **engine** package (the single read-only seam) exposing reference
  resolution, verbatim verse/passage lookup, lemma/phrase concordance,
  parse/lemmatize, manuscript attestation, and a citation renderer.
- Five transports over that one engine: an **MCP server**
  (`cmd/orthotomeo-mcp`) for use from an LLM chat client, a **CLI**
  (`cmd/orthotomeo`) for terminal use, an **HTTP API + local web UI**
  (`httpapi`, served by `cmd/orthotomeo-web`), a **desktop launcher**
  (`cmd/orthotomeo-desktop`) that starts the web server and opens a browser to
  it, and the engine package itself if you want to embed it directly in Go.

See [docs/PLAN.md](docs/PLAN.md) for the full design (read its cross-cutting
invariants first) and [docs/erd-v1.svg](docs/erd-v1.svg) for the data model.
[docs/STATUS.md](docs/STATUS.md) has the detailed ticket-by-ticket build log.

## The corpus

**This repo does not ship the corpus.** The source files are external inputs
you supply yourself - each is separately licensed, and `cmd/build` refuses to
run without both roots explicitly given (no default path). What you need:

| Edition | What it is | Format | License / attribution |
|---|---|---|---|
| TAGNT | Translators Amalgamated Greek New Testament | tab-delimited TSV, one row per Greek word (lemma, disambiguated Strong's, morphology, manuscript attestation) | CC BY 4.0, STEPBible.org / Tyndale House Cambridge |
| TAHOT | Translators Amalgamated Hebrew Old Testament | same TSV shape, Hebrew/Aramaic | CC BY 4.0, STEPBible.org / Tyndale House Cambridge |
| TEGMC / TEHMC | Greek/Hebrew morphology-code expansion tables | plain TSV | CC BY 4.0, STEPBible.org / Tyndale House Cambridge |
| TBESG / TBESH | Brief lexicon of Extended Strong's (Greek/Hebrew) | plain TSV | CC BY 4.0 (Hebrew definitions require checking terms before use - see `sources/sources.json`) |
| Swete | Swete's 1909-1930 Cambridge Septuagint, Greek surface text | CSV | Public Domain, via archive.org |
| OSS-LXX-lemma | Open Scriptures LXX lemma index | JS files, one per book, `{key, lemma}` per word | CC BY 4.0, Open Scriptures |
| KJV / ASV | King James (1769) / American Standard (1901) | single JSON file, `books[].chapters[].verses[]` | Public Domain |
| WEB | World English Bible (2024) | USFM, one file per book, Strong's numbers inline | Public Domain, eBible.org |
| Brenton | Brenton's 1851 English Septuagint | HTML, one file per chapter | Public Domain |
| OpenBible-xref | Cross-references (from the Treasury of Scripture Knowledge) | TSV | CC BY 4.0, OpenBible.info |

The authoritative list - exact `source_file` glob, format, license, and
attribution for every source - is checked in at
[`sources/sources.json`](sources/sources.json); treat that file as ground truth
over this table.

Each edition keeps its **own versification and canon** (the LXX numbers Psalms
differently and includes deuterocanonical books; the five text traditions
disagree on where some verses start). The engine never assumes a 1:1 mapping
across editions - see PLAN.md's cross-cutting invariant #4.

**Directory layout `cmd/build` expects**, split across two roots:

| Root | Contains |
|---|---|
| `--reference` | `STEPBible-Data/` (TAGNT, TAHOT, lexicons, morphology codes), `LXX-Swete-1930/` |
| `--corpus` | `bible-text/` (KJV, ASV, WEB, Brenton, OSS-LXX-lemma), `cross_references.txt` |

STEPBible-Data is published by STEPBible.org / Tyndale House Cambridge; the
Swete CSV is the archive.org digitization of the 1930 Cambridge edition; the
public-domain English translations and Open Scriptures lemma files are
available from their respective publishers (eBible.org, Open Scriptures). None
of these are fetched automatically yet - `sources/sources.json`'s `fetch_url`
field is a placeholder for that, not yet built.

## Building the database

```sh
go run ./cmd/build --corpus <path-to-bible-text-root> --reference <path-to-STEPBible-Data-root> --out data/orthotomeo.db --verify
go test ./...   # all green
```

`--verify` runs the completeness self-test (invariant #3 made enforceable) -
source/FK integrity, full-canon book coverage, and known per-edition word
counts, each checked against the real build. The DB is regenerable: delete it
and re-run the build at any time; the corpus files, never the DB, are the
source of truth.

## Using it

### As an MCP server

Build the binary and register it with an MCP host (e.g. Claude Desktop):

```sh
go build -o orthotomeo-mcp ./cmd/orthotomeo-mcp
```

```json
{
  "mcpServers": {
    "orthotomeo": {
      "command": "/path/to/orthotomeo-mcp",
      "args": ["--db", "/path/to/data/orthotomeo.db"]
    }
  }
}
```

Twelve tools are exposed: `resolve_ref`, `get_verse`, `get_passage`,
`concord_lemma`, `concord_phrase`, `count`, `parse`, `lemmatize`,
`attestation`, `interlinear`, `lexicon_lookup`, `cite`. Every `book` argument
accepts a USFM code or the full English book name, in any case (`MAT`,
`mat`, `Matthew`, `MATTHEW` all resolve to the same book).

### As a CLI

```sh
go build -o orthotomeo ./cmd/orthotomeo
orthotomeo concord --corpus TAGNT G0859               # every occurrence of a lemma/Strong's number
orthotomeo concord --corpus TAGNT --phrase "εἰς,ἄφεσις" --adjacent
orthotomeo lookup --edition KJV,ASV,WEB MAT.26.28      # verbatim verse text
orthotomeo parse --corpus TAGNT --word 2 MRK.16.16     # dStrong + expanded morphology
orthotomeo attest --corpus TAGNT MRK.16.16             # manuscript attestation (Type/Editions)
orthotomeo define G0859                                # lexicon/Strong's lookup
orthotomeo interlinear --corpus TAGNT MRK.16.16         # row-aligned original/translit/gloss/grammar
```

Every subcommand also takes `--json` for machine-readable output. A ref's
book token (e.g. the `MAT` in `MAT.26.28`) accepts a USFM code or the full
English name, in any case.

### As an HTTP API + local web UI

```sh
go build -o orthotomeo-web ./cmd/orthotomeo-web
./orthotomeo-web --db data/orthotomeo.db   # loopback only, default port 8420
```

Open `http://127.0.0.1:8420/` for the web UI, or hit the GET-only JSON
endpoints directly: `/verse`, `/passage`, `/concord`, `/parse`, `/attest`,
`/interlinear`, `/define`, `/books` (the canonical 66-book registry, used by
the UI's book-field autocomplete).

```sh
curl "http://127.0.0.1:8420/verse?book=JHN&chapter=3&verse=16&editions=KJV"
```
```json
{
  "citations": [
    {
      "ref": {"book": "JHN", "chapter": 3, "verse": 16},
      "edition": "KJV",
      "text": "For God so loved the world, that he gave his only begotten Son, that whosoever believeth in him should not perish, but have everlasting life.",
      "locator": "John 3:16",
      "confidence": "High"
    }
  ],
  "sources": {
    "KJV": {"file": "bible-text/KJV/KJV.json", "license": "Public Domain", "homepage_url": "https://github.com/scrollmapper/bible_databases"}
  }
}
```

```sh
curl "http://127.0.0.1:8420/concord?query=G0859&corpus=TAGNT"
```
```json
{
  "citations": [
    {
      "ref": {"book": "MAT", "chapter": 26, "verse": 28},
      "edition": "TAGNT",
      "text": "ἄφεσιν",
      "locator": "Mat.26.28#16=NKO",
      "lemma": "ἄφεσις",
      "translit": "aphesin",
      "dstrong": "G0859",
      "grammar": "N-ASF",
      "attestation": "NKO",
      "manuscripts": "NA28+NA27+Tyn+SBL+WH+Treg+TR+Byz",
      "confidence": "High"
    },
    { "...": "16 more occurrences, one per real match - complete-or-fail, never a sample" }
  ],
  "sources": {
    "TAGNT": {"file": "STEPBible-Data/Translators Amalgamated OT+NT/TAGNT*.txt", "license": "CC BY 4.0", "attribution": "STEPBible.org / Tyndale House Cambridge", "homepage_url": "https://github.com/STEPBible/STEPBible-Data"}
  }
}
```

### As a desktop app

```sh
go build -ldflags -H=windowsgui -o orthotomeo-desktop.exe ./cmd/orthotomeo-desktop
```

A native GUI shell (renders no scripture itself) that starts the HTTP
server on an ephemeral loopback port, opens your browser to it, and shuts
the server down cleanly on close.

### As a Go library

```go
e, err := engine.Open("data/orthotomeo.db") // opens read-only
if err != nil { ... }
defer e.Close()

citations, err := e.ConcordLemma("G0859", "TAGNT", "") // by: "" (auto-detect), "lemma", "dstrong", or "surface"
fmt.Println(e.Cite(citations)) // Markdown-formatted, fully cited
```

## Example prompt

A sample of the discipline this engine is built to serve - the kind of prompt
you'd give an LLM client with orthotomeo's MCP tools registered:

> You have access to the orthotomeo MCP tools over a real biblical-text corpus.
> When I ask a scripture question, answer using the concordance method: pull
> **every** relevant occurrence via `concord_lemma`/`concord_phrase` - not a
> remembered sample - and let the unambiguous cases fix the ambiguous ones.
> Work from what the tools return, not from recall; if a grammatical or lexical
> claim isn't something you can point to in the returned citations, say so
> rather than asserting it. Stay strictly with the biblical text itself - no
> commentaries, no modern theological arguments, no ancient/extra-biblical
> sources unless I explicitly ask for those as a separate, clearly-marked
> follow-up. Report manuscript attestation (via `attestation`) as neutral
> data - a textual variant is something for me to weigh, not something for you
> to argue for or against. Give me a reference list with brief lexical notes I
> can take into my own study, not a finished conclusion.

## License

Code: MIT (LICENSE file pending). Data is **not** relicensed by this
project - each source keeps its own license as recorded in
`sources/sources.json` (a mix of CC BY 4.0 and Public Domain; see that file for
the exact terms and attribution per source). Non-redistributable sources (e.g.
a CCAT-derived Rahlfs LXX, if added later) would be user-fetched, never
bundled.
