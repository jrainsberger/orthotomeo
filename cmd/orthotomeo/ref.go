package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jrainsberger/orthotomeo/engine"
	"github.com/jrainsberger/orthotomeo/retriever"
)

// parseRef parses the CLI's "BOOK.CHAPTER.VERSE" ref syntax (the same
// dotted shape retriever.Ref.String() produces, e.g. "MAT.26.28") into a
// retriever.Ref. The book token is resolved through e.ResolveBookCode, so a
// USFM code or the full English name works in any case (MAT, mat, Matthew,
// matthew); chapter/verse must be plain positive integers.
func parseRef(e *engine.Engine, s string) (retriever.Ref, error) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return retriever.Ref{}, fmt.Errorf("ref %q: want BOOK.CHAPTER.VERSE (e.g. MAT.26.28)", s)
	}
	code, err := e.ResolveBookCode(parts[0])
	if err != nil {
		return retriever.Ref{}, fmt.Errorf("ref %q: %w", s, err)
	}
	chapter, err := strconv.Atoi(parts[1])
	if err != nil {
		return retriever.Ref{}, fmt.Errorf("ref %q: chapter %q is not a number", s, parts[1])
	}
	verse, err := strconv.Atoi(parts[2])
	if err != nil {
		return retriever.Ref{}, fmt.Errorf("ref %q: verse %q is not a number", s, parts[2])
	}
	return retriever.Ref{Book: code, Chapter: chapter, Verse: verse}, nil
}
