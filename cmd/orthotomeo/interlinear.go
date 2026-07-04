package main

import (
	"flag"
	"fmt"

	"github.com/jrainsberger/orthotomeo/interlinear"
	"github.com/jrainsberger/orthotomeo/retriever"
)

// interlinearPayload is the --json envelope for the interlinear subcommand -
// same shape convention as citationsPayload, but Words instead of Citations
// (T35: a display composition, not a Citation itself).
type interlinearPayload struct {
	Words   []interlinear.Word              `json:"words"`
	Sources map[string]retriever.SourceInfo `json:"sources,omitempty"`
}

func runInterlinear(args []string) error {
	fs := flag.NewFlagSet("interlinear", flag.ExitOnError)
	dbPath := fs.String("db", "data/orthotomeo.db", "path to the built orthotomeo DB")
	corpus := fs.String("corpus", "", "word-tagged corpus: TAGNT, TAHOT, Swete, OSS-LXX-lemma (required)")
	word := fs.Int("word", 0, "1-based word_no within the verse; 0 (default) = every word")
	asJSON := fs.Bool("json", false, "emit the interlinearPayload JSON envelope instead of Markdown")
	fs.Parse(args)

	if *corpus == "" {
		return fmt.Errorf("--corpus is required (one of TAGNT, TAHOT, Swete, OSS-LXX-lemma)")
	}
	if fs.NArg() != 1 {
		return errUsage("interlinear <ref> --corpus C [--word N] [--db path] [--json]")
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

	var wordPtr *int
	if *word != 0 {
		wordPtr = word
	}

	words, srcs, err := e.Interlinear(ref, wordPtr, *corpus)
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(interlinearPayload{Words: words, Sources: srcs})
	}
	fmt.Println(interlinear.Render(words))
	return nil
}
