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
	"github.com/jrainsberger/orthotomeo/sources"
	"github.com/jrainsberger/orthotomeo/store"
	"github.com/jrainsberger/orthotomeo/verses"
)

func main() {
	out := flag.String("out", "data/orthotomeo.db", "output SQLite path")
	// TODO(justin, 2026-06): replace with the full corpus locator (T3); for now
	// the KJV.json and cross-reference loads resolve under this single root.
	corpus := flag.String("corpus", `D:/Claude/Bible`, "corpus root")
	flag.Parse()

	if err := run(*out, *corpus); err != nil {
		log.Fatalf("build: %v", err)
	}
}

func run(out, corpus string) error {
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

	fmt.Printf("seeded %d sources, %d books (%d aliases), %d verses, %d cross-refs (%d skipped) -> %s\n",
		nSrc, nBook, nAlias, nVerse, nXref, nSkip, out)
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
