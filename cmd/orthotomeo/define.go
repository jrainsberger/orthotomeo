package main

import (
	"flag"
	"fmt"

	"github.com/jrainsberger/orthotomeo/lexicon"
)

func runDefine(args []string) error {
	fs := flag.NewFlagSet("define", flag.ExitOnError)
	dbPath := fs.String("db", "data/orthotomeo.db", "path to the built orthotomeo DB")
	asJSON := fs.Bool("json", false, "emit the lexicon.Entry as JSON instead of Markdown")
	fs.Parse(args)

	if fs.NArg() != 1 {
		return errUsage("define <dstrong> [--db path] [--json]")
	}

	e, err := openEngine(*dbPath)
	if err != nil {
		return err
	}
	defer e.Close()

	entry, err := e.Lookup(fs.Arg(0))
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(entry)
	}
	fmt.Println(defineLine(entry))
	return nil
}

// defineLine renders one lexicon.Entry as a single Markdown line, mirroring
// cite.Cite's bullet style. Definition is appended only when present (a
// Hebrew entry never carries one - lexicon.Entry's doc comment, T34).
func defineLine(e lexicon.Entry) string {
	line := fmt.Sprintf("- **%s** (%s, %s) — %s", e.DStrong, e.Lemma, e.Translit, e.Gloss)
	if e.Definition != nil {
		line += fmt.Sprintf(": %s", *e.Definition)
	}
	return line
}
