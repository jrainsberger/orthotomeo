// Command build assembles the derived orthotomeo SQLite database from the
// corpus. It is regenerable: delete the output and re-run. Tables are filled
// per import ticket.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/brentontext"
	"github.com/jrainsberger/orthotomeo/corpus"
	"github.com/jrainsberger/orthotomeo/crossrefs"
	"github.com/jrainsberger/orthotomeo/lexicon"
	"github.com/jrainsberger/orthotomeo/morph"
	"github.com/jrainsberger/orthotomeo/sources"
	"github.com/jrainsberger/orthotomeo/store"
	"github.com/jrainsberger/orthotomeo/tagnt"
	"github.com/jrainsberger/orthotomeo/tahot"
	"github.com/jrainsberger/orthotomeo/verses"
	"github.com/jrainsberger/orthotomeo/versetext"
	"github.com/jrainsberger/orthotomeo/webtext"
)

func main() {
	out := flag.String("out", "data/orthotomeo.db", "output SQLite path")
	// The corpus is split across two parents on this machine (docs/PLAN.md
	// "Corpus locations"): --corpus holds bible-text/ + cross_references.txt,
	// --reference holds STEPBible-Data/ + LXX-Swete-1930/. corpus.Locate tries
	// each root in turn, so either tree may also be symlinked under the other.
	root := flag.String("corpus", `D:/Claude/Bible`, "corpus root (bible-text, cross_references.txt)")
	reference := flag.String("reference", `D:/Reference`, "reference root (STEPBible-Data, LXX-Swete-1930)")
	flag.Parse()

	if err := run(*out, *root, *reference); err != nil {
		log.Fatalf("build: %v", err)
	}
}

func run(out, root, reference string) error {
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	db, err := store.Open(out)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := store.ApplySchema(db); err != nil {
		return err
	}

	nSrc, err := sources.Seed(db)
	if err != nil {
		return err
	}
	nBook, nAlias, err := books.Seed(db)
	if err != nil {
		return err
	}

	roots := []string{root, reference}

	nVerse, err := loadSpine(db, roots)
	if err != nil {
		return err
	}
	nXref, nSkip, err := loadXrefs(db, roots)
	if err != nil {
		return err
	}
	nLex, err := loadLexicon(db, roots)
	if err != nil {
		return err
	}
	nMorph, err := loadMorphCodes(db, roots)
	if err != nil {
		return err
	}
	nText, err := loadVerseText(db, roots)
	if err != nil {
		return err
	}
	nWeb, nWebSkip, nWebBooks, err := loadWebText(db, roots)
	if err != nil {
		return err
	}
	nBrenton, nBrentonFiles, err := loadBrentonText(db, roots)
	if err != nil {
		return err
	}
	nWords, nWordsSkip, nCompound, err := loadTAGNT(db, roots)
	if err != nil {
		return err
	}
	nHebWords, nHebSkip, nUntagged, err := loadTAHOT(db, roots)
	if err != nil {
		return err
	}

	fmt.Printf("seeded %d sources, %d books (%d aliases), %d verses, %d cross-refs (%d skipped), %d lexicon entries, %d morph codes, %d verse texts, %d WEB verses (%d books, %d skipped), %d Brenton verses (%d chapter files), %d TAGNT words (%d skipped, %d compound), %d TAHOT words (%d skipped, %d untagged) -> %s\n",
		nSrc, nBook, nAlias, nVerse, nXref, nSkip, nLex, nMorph, nText, nWeb, nWebBooks, nWebSkip, nBrenton, nBrentonFiles, nWords, nWordsSkip, nCompound, nHebWords, nHebSkip, nUntagged, out)
	return nil
}

// sourceByCode looks up the sources.json row by code; build wiring never
// hard-codes a corpus path itself, only the source code it wants to load.
func sourceByCode(code string) (sources.Source, error) {
	reg, err := sources.Registry()
	if err != nil {
		return sources.Source{}, err
	}
	for _, s := range reg {
		if s.Code == code {
			return s, nil
		}
	}
	return sources.Source{}, fmt.Errorf("source %q not in registry", code)
}

// openSource locates and opens the single file for a sources.json code via
// corpus.LocateOne, the only path-aware step in each loader below.
func openSource(code string, roots []string) (*os.File, error) {
	src, err := sourceByCode(code)
	if err != nil {
		return nil, err
	}
	path, err := corpus.LocateOne(src, roots...)
	if err != nil {
		return nil, fmt.Errorf("locate %s: %w", code, err)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return f, nil
}

func loadSpine(db *sql.DB, roots []string) (int, error) {
	f, err := openSource("KJV", roots)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return verses.BuildSpine(db, f)
}

func loadXrefs(db *sql.DB, roots []string) (inserted, skipped int, err error) {
	f, err := openSource("OpenBible-xref", roots)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	return crossrefs.Load(db, f)
}

// loadLexicon loads TBESG (Greek) and TBESH (Hebrew).
func loadLexicon(db *sql.DB, roots []string) (int, error) {
	greek, err := openSource("TBESG", roots)
	if err != nil {
		return 0, err
	}
	defer greek.Close()
	nGreek, err := lexicon.Load(db, greek, "grc", "Abbott-Smith PD")
	if err != nil {
		return 0, err
	}

	hebrew, err := openSource("TBESH", roots)
	if err != nil {
		return 0, err
	}
	defer hebrew.Close()
	nHebrew, err := lexicon.Load(db, hebrew, "he", "BDB/Online Bible - permission")
	if err != nil {
		return 0, err
	}

	return nGreek + nHebrew, nil
}

// loadVerseText loads KJV and ASV (identical JSON shape).
func loadVerseText(db *sql.DB, roots []string) (int, error) {
	kjv, err := openSource("KJV", roots)
	if err != nil {
		return 0, err
	}
	defer kjv.Close()
	nKJV, err := versetext.Load(db, kjv, "KJV")
	if err != nil {
		return 0, err
	}

	asv, err := openSource("ASV", roots)
	if err != nil {
		return 0, err
	}
	defer asv.Close()
	nASV, err := versetext.Load(db, asv, "ASV")
	if err != nil {
		return 0, err
	}

	return nKJV + nASV, nil
}

// loadWebText loads every WEB USFM file (one per book, plus front matter,
// glossary, and deuterocanon files outside v1 scope - webtext.Load reports
// those as not loaded rather than an error).
func loadWebText(db *sql.DB, roots []string) (inserted, skipped, nBooks int, err error) {
	src, err := sourceByCode("WEB")
	if err != nil {
		return 0, 0, 0, err
	}
	paths, err := corpus.Locate(src, roots...)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("locate WEB: %w", err)
	}

	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("open %s: %w", path, err)
		}
		code, n, skip, loaded, err := webtext.Load(db, f)
		f.Close()
		if err != nil {
			return 0, 0, 0, fmt.Errorf("%s: %w", path, err)
		}
		if !loaded {
			continue
		}
		inserted += n
		skipped += skip
		nBooks++
		if skip > 0 {
			fmt.Printf("  WEB %s: %d verses skipped (unresolved against canonical spine)\n", code, skip)
		}
	}
	return inserted, skipped, nBooks, nil
}

// loadBrentonText loads every Brenton LXX chapter file. Index/TOC pages
// (PSA000.htm, GEN.htm, index.htm), front matter, deuterocanon outside the
// 66-book registry, and the explicitly-skipped combined-book EZR.htm are
// reported as not loaded rather than an error (brentontext.skipBooks; see
// docs/PLAN.md T9 for the open Ezra/Nehemiah book-identity question).
func loadBrentonText(db *sql.DB, roots []string) (inserted, nFiles int, err error) {
	src, err := sourceByCode("Brenton")
	if err != nil {
		return 0, 0, err
	}
	paths, err := corpus.Locate(src, roots...)
	if err != nil {
		return 0, 0, fmt.Errorf("locate Brenton: %w", err)
	}

	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return 0, 0, fmt.Errorf("open %s: %w", path, err)
		}
		_, _, n, loaded, err := brentontext.Load(db, f, filepath.Base(path))
		f.Close()
		if err != nil {
			return 0, 0, fmt.Errorf("%s: %w", path, err)
		}
		if !loaded {
			continue
		}
		inserted += n
		nFiles++
	}
	return inserted, nFiles, nil
}

// loadTAGNT loads both TAGNT TSVs (Mat-Jhn, Act-Rev), both under the single
// "TAGNT" source code.
func loadTAGNT(db *sql.DB, roots []string) (inserted, skipped, compound int, err error) {
	src, err := sourceByCode("TAGNT")
	if err != nil {
		return 0, 0, 0, err
	}
	paths, err := corpus.Locate(src, roots...)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("locate TAGNT: %w", err)
	}

	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("open %s: %w", path, err)
		}
		n, skip, comp, err := tagnt.Load(db, f)
		f.Close()
		if err != nil {
			return 0, 0, 0, fmt.Errorf("%s: %w", path, err)
		}
		inserted += n
		skipped += skip
		compound += comp
	}
	return inserted, skipped, compound, nil
}

// loadTAHOT loads all four TAHOT TSVs (Gen-Deu, Jos-Est, Job-Sng, Isa-Mal),
// all under the single "TAHOT" source code.
func loadTAHOT(db *sql.DB, roots []string) (inserted, skipped, untagged int, err error) {
	src, err := sourceByCode("TAHOT")
	if err != nil {
		return 0, 0, 0, err
	}
	paths, err := corpus.Locate(src, roots...)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("locate TAHOT: %w", err)
	}

	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("open %s: %w", path, err)
		}
		n, skip, unt, err := tahot.Load(db, f)
		f.Close()
		if err != nil {
			return 0, 0, 0, fmt.Errorf("%s: %w", path, err)
		}
		inserted += n
		skipped += skip
		untagged += unt
	}
	return inserted, skipped, untagged, nil
}

// loadMorphCodes loads TEGMC (Greek) and TEHMC (Hebrew).
func loadMorphCodes(db *sql.DB, roots []string) (int, error) {
	greek, err := openSource("TEGMC", roots)
	if err != nil {
		return 0, err
	}
	defer greek.Close()
	nGreek, err := morph.Load(db, greek, "grc")
	if err != nil {
		return 0, err
	}

	hebrew, err := openSource("TEHMC", roots)
	if err != nil {
		return 0, err
	}
	defer hebrew.Close()
	nHebrew, err := morph.Load(db, hebrew, "he")
	if err != nil {
		return 0, err
	}

	return nGreek + nHebrew, nil
}
