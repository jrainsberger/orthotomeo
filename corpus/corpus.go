// Package corpus is the only path-aware code in the importer: it resolves a
// sources.Source's logical source_file glob to absolute file(s) on disk.
// Loaders never hard-code or join corpus paths themselves. Ticket 3.
package corpus

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/jrainsberger/orthotomeo/sources"
)

// ErrCorpusMissing means a source's tree was not found under any given root.
var ErrCorpusMissing = errors.New("corpus tree missing")

// Locate resolves src.SourceFile (a glob relative to a corpus root, e.g.
// "bible-text/KJV/KJV.json" or "STEPBible-Data/Lexicons/TBESG*.txt") against
// each root in turn and returns the matches from the first root where any
// are found. This is how the on-disk split between corpus trees (bible-text
// and cross_references.txt under one parent; STEPBible-Data and
// LXX-Swete-1930 under another) is supported without requiring the caller
// to symlink everything under a single directory: pass each tree's parent
// as one root. Matches are sorted for deterministic glob expansion.
func Locate(src sources.Source, roots ...string) ([]string, error) {
	for _, root := range roots {
		pattern := filepath.Join(root, filepath.FromSlash(src.SourceFile))
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("glob %s: %w", pattern, err)
		}
		if len(matches) > 0 {
			sort.Strings(matches)
			return matches, nil
		}
	}
	return nil, fmt.Errorf("%w: %s under %v", ErrCorpusMissing, src.SourceFile, roots)
}

// LocateOne resolves src.SourceFile like Locate, but requires exactly one
// match - for sources whose source_file names a single file (KJV.json,
// TBESG*.txt, ...). A glob matching zero or many files is an error: the
// caller asked for one file and the corpus does not agree.
func LocateOne(src sources.Source, roots ...string) (string, error) {
	matches, err := Locate(src, roots...)
	if err != nil {
		return "", err
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("%s: want 1 match, got %d: %v", src.SourceFile, len(matches), matches)
	}
	return matches[0], nil
}
