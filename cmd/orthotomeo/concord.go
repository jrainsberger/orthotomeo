package main

import (
	"flag"
	"fmt"
	"strings"
)

func runConcord(args []string) error {
	fs := flag.NewFlagSet("concord", flag.ExitOnError)
	dbPath := fs.String("db", "data/orthotomeo.db", "path to the built orthotomeo DB")
	corpus := fs.String("corpus", "", "word-tagged corpus: TAGNT, TAHOT, Swete, OSS-LXX-lemma (required)")
	phrase := fs.String("phrase", "", "comma-separated ordered lemma tokens - switches to ConcordPhrase, ignoring the positional query")
	window := fs.Int("window", 0, "max words between consecutive phrase tokens (0 = strictly adjacent)")
	adjacent := fs.Bool("adjacent", false, "shorthand for --window 0 (the default already, provided for explicitness)")
	by := fs.String("by", "", "match column override: lemma, dstrong, or surface (default: auto-detect dStrong shape, else lemma - surface must be requested explicitly)")
	asJSON := fs.Bool("json", false, "emit the citationsPayload JSON envelope instead of Markdown")
	fs.Parse(args)

	if *corpus == "" {
		return fmt.Errorf("--corpus is required (one of TAGNT, TAHOT, Swete, OSS-LXX-lemma)")
	}

	e, err := openEngine(*dbPath)
	if err != nil {
		return err
	}
	defer e.Close()

	if *phrase != "" {
		w := *window
		if *adjacent {
			w = 0
		}
		tokens := strings.Split(*phrase, ",")
		cs, err := e.ConcordPhrase(tokens, *corpus, w)
		if err != nil {
			return err
		}
		return emit(e, cs, *asJSON)
	}

	if fs.NArg() != 1 {
		return errUsage("concord <lemma|dstrong|surface> --corpus C [--by lemma|dstrong|surface] [--phrase tok1,tok2 [--window N|--adjacent]] [--db path] [--json]")
	}
	cs, err := e.ConcordLemma(fs.Arg(0), *corpus, *by)
	if err != nil {
		return err
	}
	return emit(e, cs, *asJSON)
}
