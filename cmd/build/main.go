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
	"github.com/jrainsberger/orthotomeo/crossrefs"
	"github.com/jrainsberger/orthotomeo/lexicon"
	"github.com/jrainsberger/orthotomeo/sources"
	"github.com/jrainsberger/orthotomeo/store"
	"github.com/jrainsberger/orthotomeo/verses"
)

func main() {
	out := flag.String("out", "data/orthotomeo.db", "output SQLite path")
	// TODO(justin, 2026-06): replace with the full corpus locator (T3); for now
	// the KJV.json and cross-reference loads resolve under this single root.
	corpus := flag.String("corpus", `D:/Claude/Bible`, "corpus root")
	stepbible := flag.String("stepbible", `D:/Reference/STEPBible-Data`, "STEPBible-Data root")
	flag.Parse()

	if err := run(*out, *corpus, *stepbible); err != nil {
		log.Fatalf("build: %v", err)
	}
}

func run(out, corpus, stepbible string) error {
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

	nVerse, err := loadSpine(db, filepath.Join(corpus, "bible-text", "KJV", "KJV.json"))
	if err != nil {
		return err
	}
	nXref, nSkip, err := loadXrefs(db, filepath.Join(corpus, "cross_references.txt"))
	if err != nil {
		return err
	}
	nLex, err := loadLexicon(db, stepbible)
	if err != nil {
		return err
	}

	fmt.Printf("seeded %d sources, %d books (%d aliases), %d verses, %d cross-refs (%d skipped), %d lexicon entries -> %s\n",
		nSrc, nBook, nAlias, nVerse, nXref, nSkip, nLex, out)
	return nil
}

func loadSpine(db *sql.DB, path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open KJV.json: %w", err)
	}
	defer f.Close()
	return verses.BuildSpine(db, f)
}

func loadXrefs(db *sql.DB, path string) (inserted, skipped int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, fmt.Errorf("open cross_references.txt: %w", err)
	}
	defer f.Close()
	return crossrefs.Load(db, f)
}

// loadLexicon loads TBESG (Greek) and TBESH (Hebrew), each glob-matched under
// stepbible/Lexicons since the filenames carry a long descriptive suffix.
func loadLexicon(db *sql.DB, stepbible string) (int, error) {
	greek, err := loadLexiconFile(db, stepbible, "TBESG*.txt", "grc", "Abbott-Smith PD")
	if err != nil {
		return 0, err
	}
	hebrew, err := loadLexiconFile(db, stepbible, "TBESH*.txt", "he", "BDB/Online Bible - permission")
	if err != nil {
		return 0, err
	}
	return greek + hebrew, nil
}

func loadLexiconFile(db *sql.DB, stepbible, glob, language, defLicense string) (int, error) {
	matches, err := filepath.Glob(filepath.Join(stepbible, "Lexicons", glob))
	if err != nil {
		return 0, fmt.Errorf("glob %s: %w", glob, err)
	}
	if len(matches) != 1 {
		return 0, fmt.Errorf("glob %s: want 1 match, got %d", glob, len(matches))
	}
	f, err := os.Open(matches[0])
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", matches[0], err)
	}
	defer f.Close()
	return lexicon.Load(db, f, language, defLicense)
}
