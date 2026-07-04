// Command orthotomeo is the thinnest human/scriptable surface over the
// engine facade (T25) - the facade's first smoke test: if the CLI is
// awkward, the API is awkward. Every subcommand is a direct delegation to
// one engine.Engine method; this package builds no SQL and never triggers
// a build (it opens a prebuilt DB read-only and fails if one isn't there).
// Ticket 26.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/jrainsberger/orthotomeo/engine"
	"github.com/jrainsberger/orthotomeo/retriever"
)

// citationsPayload is the --json wrapper every Citation-bearing subcommand
// emits - an object, not a bare array, and the exact shape T27's HTTP JSON
// responses are specified to reuse byte-for-byte (also matches the MCP
// surface's citationsResult, so the three transports agree on one shape).
// Sources is the T31 per-edition provenance map (file/license/attribution),
// added once per distinct edition actually present - never repeated per
// Citation the way a per-row source file used to be.
type citationsPayload struct {
	Citations []retriever.Citation            `json:"citations"`
	Sources   map[string]retriever.SourceInfo `json:"sources,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "lookup":
		err = runLookup(args)
	case "concord":
		err = runConcord(args)
	case "parse":
		err = runParse(args)
	case "attest":
		err = runAttest(args)
	case "define":
		err = runDefine(args)
	case "interlinear":
		err = runInterlinear(args)
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "orthotomeo: unknown command %q\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "orthotomeo %s: %v\n", cmd, err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `orthotomeo - read-only scripture-study engine CLI

Usage:
  orthotomeo lookup <ref> [--edition KJV,ASV,...] [--db path] [--json]
  orthotomeo concord <lemma|dstrong|surface> --corpus C [--by lemma|dstrong|surface] [--phrase tok1,tok2 [--window N|--adjacent]] [--db path] [--json]
  orthotomeo parse <ref> --corpus C [--word N] [--db path] [--json]
  orthotomeo attest <ref> --corpus C [--word N] [--db path] [--json]
  orthotomeo define <dstrong> [--db path] [--json]
  orthotomeo interlinear <ref> --corpus C [--word N] [--db path] [--json]

ref syntax: BOOK.CHAPTER.VERSE, e.g. MAT.26.28
dstrong syntax: a disambiguated Strong's number, e.g. G0859, H7225G
--db defaults to data/orthotomeo.db; the DB must already be built (cmd/build).`)
}

func openEngine(dbPath string) (*engine.Engine, error) {
	e, err := engine.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open %s (build it first with cmd/build): %w", dbPath, err)
	}
	return e, nil
}

// emit writes citations as Markdown bullets (engine.Cite) by default, or as
// the citationsPayload JSON envelope when asJSON is set.
func emit(e *engine.Engine, citations []retriever.Citation, asJSON bool) error {
	if asJSON {
		srcs, err := retriever.SourcesFor(citations)
		if err != nil {
			return err
		}
		return writeJSON(citationsPayload{Citations: citations, Sources: srcs})
	}
	fmt.Println(e.Cite(citations))
	return nil
}

func writeJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// errUsage reports a subcommand argument-shape error in one place, printing
// the exact form callers get from usage() so a mistyped invocation prints
// the same syntax both times.
func errUsage(form string) error {
	return fmt.Errorf("usage: orthotomeo %s", form)
}
