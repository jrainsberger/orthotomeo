// Command build assembles the derived orthotomeo SQLite database from the
// corpus. It is regenerable: delete the output and re-run. Tables are filled
// per import ticket; ticket 1 seeds sources, ticket 2 seeds books.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/sources"
	"github.com/jrainsberger/orthotomeo/store"
)

func main() {
	out := flag.String("out", "data/orthotomeo.db", "output SQLite path")
	flag.Parse()

	if err := run(*out); err != nil {
		log.Fatalf("build: %v", err)
	}
}

func run(out string) error {
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

	fmt.Printf("seeded %d sources, %d books (%d aliases) -> %s\n", nSrc, nBook, nAlias, out)
	return nil
}
