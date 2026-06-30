// Command build assembles the derived orthotomeo SQLite database from the
// corpus. It is regenerable: delete the output and re-run. Tables are filled
// per import ticket; ticket 1 seeds the provenance registry.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/jrainsberger/orthotomeo/internal/sources"
	"github.com/jrainsberger/orthotomeo/internal/store"
)

func main() {
	out := flag.String("out", "data/orthotomeo.db", "output SQLite path")
	flag.Parse()

	if err := run(*out); err != nil {
		log.Fatalf("build: %v", err)
	}
}

func run(out string) error {
	if err := os.MkdirAll(dir(out), 0o755); err != nil {
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

	n, err := sources.Seed(db)
	if err != nil {
		return err
	}
	fmt.Printf("seeded %d sources -> %s\n", n, out)
	return nil
}

// dir returns the directory portion of a path, or "." if none.
func dir(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[:i]
		}
	}
	return "."
}
