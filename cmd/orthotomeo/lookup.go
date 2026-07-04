package main

import (
	"flag"
	"strings"
)

// defaultLookupEditions is used when --edition isn't given - every
// per-verse content edition GetVerse knows how to reach, not a silent
// narrowing to one.
var defaultLookupEditions = []string{"KJV", "ASV", "WEB", "Brenton"}

func runLookup(args []string) error {
	fs := flag.NewFlagSet("lookup", flag.ExitOnError)
	dbPath := fs.String("db", "data/orthotomeo.db", "path to the built orthotomeo DB")
	editionsFlag := fs.String("edition", "", "comma-separated editions (default: all of KJV,ASV,WEB,Brenton)")
	asJSON := fs.Bool("json", false, "emit the citationsPayload JSON envelope instead of Markdown")
	fs.Parse(args)

	if fs.NArg() != 1 {
		return errUsage("lookup <ref> [--edition KJV,ASV,...] [--db path] [--json]")
	}
	e, err := openEngine(*dbPath)
	if err != nil {
		return err
	}
	defer e.Close()

	ref, err := parseRef(e, fs.Arg(0))
	if err != nil {
		return err
	}

	editions := defaultLookupEditions
	if *editionsFlag != "" {
		editions = strings.Split(*editionsFlag, ",")
	}

	cs, err := e.GetVerse(ref, editions)
	if err != nil {
		return err
	}
	return emit(e, cs, *asJSON)
}
